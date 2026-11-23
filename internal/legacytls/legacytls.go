// Package legacytls connects to BMC web servers that only speak TLS 1.0 or
// 1.1 with ciphers modern TLS stacks have dropped, without needing an old
// OpenSSL anywhere.
//
// Dial first tries Go's crypto/tls with TLS 1.0 and every cipher suite Go
// still implements switched back on (CBC, 3DES, RC4-SHA, RSA key exchange).
// If that handshake fails, it falls back to a minimal client in this package
// that speaks TLS 1.0/1.1 with RC4-128-MD5 or RC4-128-SHA. Go never
// implemented RC4-MD5, and some 2008-era AMI MegaRAC firmware offers nothing
// else that is usable (its full list is NULL-MD5, two 40-bit export suites
// and RC4-MD5).
//
// None of this is secure against an active attacker. It exists because the
// alternative is keeping an old OS around just to reach the BMC; callers show
// the negotiated Info so nobody mistakes it for a sound connection.
// Certificates are never verified, as with every other BMC connection
// fyrtaarn makes: BMCs ship self-signed certificates.
package legacytls

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/knightmare2600/fyrtaarn/internal/quirks"
)

// Info describes a negotiated connection.
type Info struct {
	Version uint16 // tls.VersionTLS10 etc.
	Suite   uint16 // IANA cipher suite ID
	// Fallback is true when this package's own RC4 client was used because
	// Go's crypto/tls could not complete the handshake.
	Fallback bool
}

// Legacy reports whether the connection uses a protocol or cipher that
// modern clients refuse: anything below TLS 1.2, or any RC4/3DES/CBC-SHA1
// suite Go lists as insecure.
func (i Info) Legacy() bool {
	if i.Fallback || i.Version < tls.VersionTLS12 {
		return true
	}
	for _, s := range tls.InsecureCipherSuites() {
		if s.ID == i.Suite {
			return true
		}
	}
	return false
}

func (i Info) String() string {
	s := tls.VersionName(i.Version) + " " + suiteName(i.Suite)
	if i.Fallback {
		s += " (fyrtaarn legacy client)"
	}
	return s
}

func suiteName(id uint16) string {
	switch id {
	case suiteRC4MD5:
		return "TLS_RSA_WITH_RC4_128_MD5"
	case suiteRC4SHA:
		return "TLS_RSA_WITH_RC4_128_SHA"
	}
	return tls.CipherSuiteName(id)
}

// Conn is what Dial returns: a *tls.Conn or this package's own client, both
// carrying the negotiated Info.
type Conn interface {
	net.Conn
	Info() Info
}

type nativeConn struct{ *tls.Conn }

func (c nativeConn) Info() Info {
	st := c.ConnectionState()
	return Info{Version: st.Version, Suite: st.CipherSuite}
}

// nativeConfig enables everything Go's crypto/tls still implements. Setting
// MinVersion and CipherSuites explicitly overrides the Go 1.22+ defaults
// that dropped TLS 1.0/1.1 and RSA key exchange from clients; the list has
// no effect on TLS 1.3, which still negotiates normally with modern BMCs.
func nativeConfig(serverName string) *tls.Config {
	var ids []uint16
	for _, s := range tls.CipherSuites() {
		ids = append(ids, s.ID)
	}
	for _, s := range tls.InsecureCipherSuites() {
		ids = append(ids, s.ID)
	}
	return &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // BMCs use self-signed certificates
		MinVersion:         tls.VersionTLS10,
		CipherSuites:       ids,
		ServerName:         serverName,
	}
}

// fallbackHosts remembers addresses where only the fallback client works, so
// later connections (a browser opens many) skip the doomed native attempt.
var fallbackHosts sync.Map

// forceFallback is set by tests to exercise the fallback client against a
// Go server that would otherwise be reached natively.
var forceFallback bool

// Dial connects to addr ("host:port") and completes a TLS handshake, falling
// back to the legacy client as described in the package comment.
func Dial(ctx context.Context, network, addr string) (Conn, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	d := &net.Dialer{Timeout: 10 * time.Second}

	_, knownLegacy := fallbackHosts.Load(addr)
	var nativeErr error
	if !knownLegacy && !forceFallback {
		raw, err := d.DialContext(ctx, network, addr)
		if err != nil {
			return nil, err
		}
		tc := tls.Client(raw, nativeConfig(host))
		if nativeErr = tc.HandshakeContext(ctx); nativeErr == nil {
			return nativeConn{tc}, nil
		}
		_ = raw.Close()
		if ctx.Err() != nil {
			return nil, nativeErr
		}
	}

	raw, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(dl)
	}
	c, err := client(raw)
	if err != nil {
		_ = raw.Close()
		if nativeErr != nil {
			return nil, fmt.Errorf("TLS handshake failed: %w (legacy client: %v)", nativeErr, err)
		}
		return nil, fmt.Errorf("legacy TLS handshake failed: %w", err)
	}
	_ = raw.SetDeadline(time.Time{})
	fallbackHosts.Store(addr, struct{}{})
	quirks.Learn(host, quirks.LegacyTLSOnly)
	return c, nil
}

// Transport returns an http.Transport whose HTTPS connections go through
// Dial. Plain-HTTP requests are unaffected.
func Transport() *http.Transport {
	return &http.Transport{
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return Dial(ctx, network, addr)
		},
		// The fallback client cannot negotiate ALPN, and BMC web servers of
		// any age are fine with HTTP/1.1.
		ForceAttemptHTTP2: false,
		IdleConnTimeout:   30 * time.Second,
	}
}

// Probe makes one connection to addr and reports what was negotiated.
func Probe(ctx context.Context, addr string) (Info, error) {
	c, err := Dial(ctx, "tcp", addr)
	if err != nil {
		return Info{}, err
	}
	defer c.Close()
	return c.Info(), nil
}
