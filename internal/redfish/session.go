package redfish

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/knightmare2600/fyrtaarn/internal/legacytls"
)

// Session login. Every Redfish request is sent with HTTP basic auth, as
// before; when a BMC refuses that (401), fyrtaarn creates a Redfish
// session (POST to the Sessions collection), keeps its X-Auth-Token per host
// and user, and retries. BMCs can turn basic auth off, and iLO 5/6 prefer
// sessions. Sessions are deleted on exit (CloseSessions).

type rfSession struct {
	token    string
	location string // the session's URL, for logging out
	base     string // scheme://host
}

var sessions sync.Map // sessionKey -> *rfSession

// sessionKey ties a session to host, user and password: a request with a
// different password must not ride on a session another one opened. The
// password is hashed, not kept.
func sessionKey(host, user, pass string) string {
	h := sha256.Sum256([]byte(pass))
	return host + "|" + user + "|" + hex.EncodeToString(h[:])
}

// sessionTransport wraps the legacy-TLS transport with the session fallback.
type sessionTransport struct{ base http.RoundTripper }

func newTransport() http.RoundTripper { return &sessionTransport{base: legacytls.Transport()} }

func (t *sessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	user, pass, basic := req.BasicAuth()
	if !basic {
		return t.base.RoundTrip(req)
	}
	key := sessionKey(req.URL.Host, user, pass)

	if v, ok := sessions.Load(key); ok {
		resp, err := t.withToken(req, v.(*rfSession).token)
		if err != nil || resp.StatusCode != http.StatusUnauthorized {
			return resp, err
		}
		// Expired or revoked: start again below.
		resp.Body.Close()
		sessions.Delete(key)
	}

	resp, err := t.base.RoundTrip(cloneWithBody(req))
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	s, serr := t.createSession(req, user, pass)
	if serr != nil {
		return resp, nil // keep the BMC's own 401
	}
	resp.Body.Close()
	sessions.Store(key, s)
	return t.withToken(req, s.token)
}

// withToken sends req with the session token instead of basic auth.
func (t *sessionTransport) withToken(req *http.Request, token string) (*http.Response, error) {
	r := cloneWithBody(req)
	r.Header.Del("Authorization")
	r.Header.Set("X-Auth-Token", token)
	return t.base.RoundTrip(r)
}

// cloneWithBody copies req with a fresh body, so it can be sent again.
func cloneWithBody(req *http.Request) *http.Request {
	r := req.Clone(req.Context())
	if req.GetBody != nil {
		if b, err := req.GetBody(); err == nil {
			r.Body = b
		}
	}
	return r
}

// sessionsURL finds the Sessions collection from the service root, which
// needs no auth: Links.Sessions (DMTF; iDRAC9, iLO 5) or links.Sessions.href
// (iLO 4), else the DMTF default path.
func (t *sessionTransport) sessionsURL(base string) string {
	req, _ := http.NewRequest(http.MethodGet, base+"/redfish/v1/", nil)
	req.Header.Set("Accept", "application/json")
	if resp, err := t.base.RoundTrip(req); err == nil {
		defer resp.Body.Close()
		var root struct {
			Links struct {
				Sessions struct {
					ID string `json:"@odata.id"`
				}
			}
			Legacy struct {
				Sessions struct{ Href string } `json:"Sessions"`
			} `json:"links"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&root) == nil {
			if root.Links.Sessions.ID != "" {
				return base + root.Links.Sessions.ID
			}
			if root.Legacy.Sessions.Href != "" {
				return base + root.Legacy.Sessions.Href
			}
		}
	}
	return base + "/redfish/v1/SessionService/Sessions"
}

func (t *sessionTransport) createSession(orig *http.Request, user, pass string) (*rfSession, error) {
	base := orig.URL.Scheme + "://" + orig.URL.Host
	body, _ := json.Marshal(map[string]string{"UserName": user, "Password": pass})
	req, err := http.NewRequestWithContext(orig.Context(), http.MethodPost, t.sessionsURL(base), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OData-Version", "4.0")
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	tok := resp.Header.Get("X-Auth-Token")
	if resp.StatusCode >= 300 || tok == "" {
		return nil, fmt.Errorf("session login refused: HTTP %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if loc != "" && loc[0] == '/' {
		loc = base + loc
	}
	return &rfSession{token: tok, location: loc, base: base}, nil
}

// CloseSessions logs out every session fyrtaarn opened (on exit).
func CloseSessions() error {
	var errs []error
	t := legacytls.Transport()
	sessions.Range(func(k, v any) bool {
		s := v.(*rfSession)
		sessions.Delete(k)
		if s.location == "" {
			return true
		}
		req, err := http.NewRequest(http.MethodDelete, s.location, nil)
		if err != nil {
			errs = append(errs, err)
			return true
		}
		req.Header.Set("X-Auth-Token", s.token)
		if resp, err := t.RoundTrip(req); err != nil {
			errs = append(errs, err)
		} else {
			resp.Body.Close()
		}
		return true
	})
	return errors.Join(errs...)
}
