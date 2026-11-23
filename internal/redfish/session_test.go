package redfish

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeTokenBMC refuses basic auth and accepts only session tokens, like a
// BMC with basic auth switched off. legacyLinks serves the root in iLO 4's
// links.Sessions.href form instead of Links.Sessions.
type fakeTokenBMC struct {
	mu          sync.Mutex
	tokens      map[string]bool
	logins      int
	deleted     []string
	posted      string
	legacyLinks bool
}

func (f *fakeTokenBMC) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/redfish/v1/" && r.Method == http.MethodGet:
			if f.legacyLinks {
				_, _ = w.Write([]byte(`{"links":{"Sessions":{"href":"/redfish/v1/SessionService/Sessions/"}}}`))
			} else {
				_, _ = w.Write([]byte(`{"Links":{"Sessions":{"@odata.id":"/redfish/v1/SessionService/Sessions/"}}}`))
			}
			return
		case r.URL.Path == "/redfish/v1/SessionService/Sessions/" && r.Method == http.MethodPost:
			var c struct{ UserName, Password string }
			_ = json.NewDecoder(r.Body).Decode(&c)
			if c.UserName != "admin" || c.Password != "pw" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			f.logins++
			tok := "tok" + string(rune('0'+f.logins))
			f.tokens[tok] = true
			w.Header().Set("X-Auth-Token", tok)
			w.Header().Set("Location", "/redfish/v1/SessionService/Sessions/"+tok)
			w.WriteHeader(http.StatusCreated)
			return
		case strings.HasPrefix(r.URL.Path, "/redfish/v1/SessionService/Sessions/") && r.Method == http.MethodDelete:
			f.deleted = append(f.deleted, r.URL.Path)
			delete(f.tokens, r.Header.Get("X-Auth-Token"))
			return
		}
		if _, _, basic := r.BasicAuth(); basic || !f.tokens[r.Header.Get("X-Auth-Token")] {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Method == http.MethodPost {
			b, _ := io.ReadAll(r.Body)
			f.posted = string(b)
			return
		}
		_, _ = w.Write([]byte(`{"PowerState":"On"}`))
	})
}

func newTokenBMC(t *testing.T, legacy bool) (*fakeTokenBMC, string) {
	f := &fakeTokenBMC{tokens: map[string]bool{}, legacyLinks: legacy}
	srv := httptest.NewTLSServer(f.handler(t))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { _ = CloseSessions() })
	return f, srv.URL
}

func TestSessionFallback(t *testing.T) {
	f, base := newTokenBMC(t, false)
	c := httpClient()

	for i := 0; i < 2; i++ {
		got, err := fetchJSON(c, base+"/redfish/v1/Systems/1", "admin", "pw")
		if err != nil || got["PowerState"] != "On" {
			t.Fatalf("GET %d: %v %v", i, got, err)
		}
	}
	if f.logins != 1 {
		t.Fatalf("logins %d, want one session reused", f.logins)
	}
	// A POST body survives the basic-auth attempt and the retry.
	if err := doPost(c, base+"/redfish/v1/Systems/1/Actions/ComputerSystem.Reset", "admin", "pw", []byte(`{"ResetType":"On"}`)); err != nil || f.posted != `{"ResetType":"On"}` {
		t.Fatalf("POST: %v body %q", err, f.posted)
	}
	// Token expiry: the BMC forgets it; the next call logs in again.
	f.mu.Lock()
	f.tokens = map[string]bool{}
	f.mu.Unlock()
	if _, err := fetchJSON(c, base+"/redfish/v1/Systems/1", "admin", "pw"); err != nil || f.logins != 2 {
		t.Fatalf("after expiry: %v, logins %d", err, f.logins)
	}
	// Wrong password: the BMC's own 401 comes back.
	if _, err := fetchJSON(c, base+"/redfish/v1/Systems/1", "admin", "nope"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("wrong password: %v", err)
	}
	if err := CloseSessions(); err != nil || len(f.deleted) != 1 {
		t.Fatalf("close: %v deleted %v", err, f.deleted)
	}
}

func TestSessionLegacyLinks(t *testing.T) {
	f, base := newTokenBMC(t, true)
	if _, err := fetchJSON(httpClient(), base+"/redfish/v1/Systems/1", "admin", "pw"); err != nil || f.logins != 1 {
		t.Fatalf("iLO 4 links form: %v logins %d", err, f.logins)
	}
}

// TestBasicAuthUnchanged checks BMCs that accept basic auth never get a
// session.
func TestBasicAuthUnchanged(t *testing.T) {
	var sawToken bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Auth-Token") != "" || r.Method == http.MethodPost {
			sawToken = true
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if _, err := fetchJSON(httpClient(), srv.URL+"/redfish/v1/", "u", "p"); err != nil || sawToken {
		t.Fatalf("err %v, session used %v", err, sawToken)
	}
}
