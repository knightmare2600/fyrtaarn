package legacytls

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestKeys checks the TLS 1.0 PRF and key expansion against vectors that
// were generated with GnuTLS (gnutls-cli -d 9), copied from Go's
// src/crypto/tls/prf_test.go (testKeysFromTests, TLS_RSA_WITH_RC4_128_SHA:
// 20-byte MAC keys, 16-byte RC4 keys).
func TestKeys(t *testing.T) {
	tests := []struct {
		pms, cr, sr, master, cMAC, sMAC, cKey, sKey string
	}{
		{
			"0302cac83ad4b1db3b9ab49ad05957de2a504a634a386fc600889321e1a971f57479466830ac3e6f468e87f5385fa0c5",
			"4ae66303755184a3917fcb44880605fcc53baa01912b22ed94473fc69cebd558",
			"4ae663020ec16e6bb5130be918cfcafd4d765979a3136a5d50c593446e4e44db",
			"3d851bab6e5556e959a16bc36d66cfae32f672bfa9ecdef6096cbb1b23472df1da63dbbd9827606413221d149ed08ceb",
			"805aaa19b3d2c0a0759a4b6c9959890e08480119",
			"2d22f9fe519c075c16448305ceee209fc24ad109",
			"d50b5771244f850cd8117a9ccafe2cf1",
			"e076e33206b30507a85c32855acd0919",
		},
		{
			"03023f7527316bc12cbcd69e4b9e8275d62c028f27e65c745cfcddc7ce01bd3570a111378b63848127f1c36e5f9e4890",
			"4ae66364b5ea56b20ce4e25555aed2d7e67f42788dd03f3fee4adae0459ab106",
			"4ae66363ab815cbf6a248b87d6b556184e945e9b97fbdf247858b0bdafacfa1c",
			"7d64be7c80c59b740200b4b9c26d0baaa1c5ae56705acbcf2307fe62beb4728c19392c83f20483801cce022c77645460",
			"97742ed60a0554ca13f04f97ee193177b971e3b0",
			"37068751700400e03a8477a5c7eec0813ab9e0dc",
			"207cddbc600d2a200abac6502053ee5c",
			"df3f94f6e1eacc753b815fe16055cd43",
		},
		{
			// 128-byte pre-master secret: exercises the even-length split.
			"832d515f1d61eebb2be56ba0ef79879efb9b527504abb386fb4310ed5d0e3b1f220d3bb6b455033a2773e6d8bdf951d278a187482b400d45deb88a5d5a6bb7d6a7a1decc04eb9ef0642876cd4a82d374d3b6ff35f0351dc5d411104de431375355addc39bfb1f6329fb163b0bc298d658338930d07d313cd980a7e3d9196cac1",
			"4ae663b2ee389c0de147c509d8f18f5052afc4aaf9699efe8cb05ece883d3a5e",
			"4ae664d503fd4cff50cfc1fb8fc606580f87b0fcdac9554ba0e01d785bdf278e",
			"1aff2e7a2c4279d0126f57a65a77a8d9d0087cf2733366699bec27eb53d5740705a8574bb1acc2abbe90e44f0dd28d6c",
			"3c7647c93c1379a31a609542aa44e7f117a70085",
			"0d73102994be74a575a3ead8532590ca32a526d4",
			"ac7581b0b6c10d85bbd905ffbf36c65e",
			"ff07edde49682b45466bd2e39464b306",
		},
	}
	unhex := func(s string) []byte {
		b, err := hex.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for i, tc := range tests {
		master, cMAC, sMAC, cKey, sKey := keys(unhex(tc.pms), unhex(tc.cr), unhex(tc.sr), 20)
		got := []string{hex.EncodeToString(master), hex.EncodeToString(cMAC), hex.EncodeToString(sMAC),
			hex.EncodeToString(cKey), hex.EncodeToString(sKey)}
		want := []string{tc.master, tc.cMAC, tc.sMAC, tc.cKey, tc.sKey}
		for j := range got {
			if got[j] != want[j] {
				t.Errorf("vector %d field %d: got %s, want %s", i, j, got[j], want[j])
			}
		}
	}
}

// oldServer starts an HTTPS server limited to one TLS version and the given
// suites, standing in for an old BMC.
func oldServer(t *testing.T, vers uint16, suites ...uint16) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello from "+r.URL.Path+strings.Repeat(".", 40000))
	}))
	srv.TLS = &tls.Config{MinVersion: vers, MaxVersion: vers, CipherSuites: suites}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, url string) string {
	t.Helper()
	c := &http.Client{Transport: Transport(), Timeout: 10 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

func probe(t *testing.T, srv *httptest.Server) Info {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	info, err := Probe(ctx, strings.TrimPrefix(srv.URL, "https://"))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	return info
}

// TestNative covers old servers Go's own crypto/tls can still reach once
// TLS 1.0/1.1 and the insecure suites are switched back on.
func TestNative(t *testing.T) {
	cases := []struct {
		vers  uint16
		suite uint16
	}{
		{tls.VersionTLS10, tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA},
		{tls.VersionTLS10, tls.TLS_RSA_WITH_AES_128_CBC_SHA},
		{tls.VersionTLS11, tls.TLS_RSA_WITH_AES_256_CBC_SHA},
		{tls.VersionTLS11, tls.TLS_RSA_WITH_RC4_128_SHA},
	}
	for _, tc := range cases {
		t.Run(tls.VersionName(tc.vers)+"/"+tls.CipherSuiteName(tc.suite), func(t *testing.T) {
			srv := oldServer(t, tc.vers, tc.suite)
			info := probe(t, srv)
			if info.Fallback || info.Version != tc.vers || info.Suite != tc.suite || !info.Legacy() {
				t.Fatalf("got %+v (%s)", info, info)
			}
			if body := get(t, srv.URL+"/x"); !strings.HasPrefix(body, "hello from /x") || len(body) != 40013 {
				t.Fatalf("body: %.40q (%d bytes)", body, len(body))
			}
		})
	}
}

// TestFallback runs this package's RC4 client against Go's server, which is
// an independent TLS implementation, at both versions it supports.
func TestFallback(t *testing.T) {
	forceFallback = true
	t.Cleanup(func() { forceFallback = false })
	for _, vers := range []uint16{tls.VersionTLS10, tls.VersionTLS11} {
		t.Run(tls.VersionName(vers), func(t *testing.T) {
			srv := oldServer(t, vers, tls.TLS_RSA_WITH_RC4_128_SHA)
			info := probe(t, srv)
			if !info.Fallback || info.Version != vers || info.Suite != suiteRC4SHA {
				t.Fatalf("got %+v", info)
			}
			// 40 kB body spans several records; two requests check the
			// connection survives reuse.
			for _, p := range []string{"/a", "/b"} {
				if body := get(t, srv.URL+p); !strings.HasPrefix(body, "hello from "+p) || len(body) != 40013 {
					t.Fatalf("body: %.40q (%d bytes)", body, len(body))
				}
			}
		})
	}
}

// TestModernUnaffected checks a TLS 1.3 server still negotiates TLS 1.3.
func TestModernUnaffected(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	t.Cleanup(srv.Close)
	info := probe(t, srv)
	if info.Version != tls.VersionTLS13 || info.Legacy() {
		t.Fatalf("got %+v", info)
	}
}

// TestFallbackRemembered checks an address that needed the fallback skips
// the native attempt next time.
func TestFallbackRemembered(t *testing.T) {
	srv := oldServer(t, tls.VersionTLS10, tls.TLS_RSA_WITH_RC4_128_SHA)
	addr := strings.TrimPrefix(srv.URL, "https://")
	fallbackHosts.Store(addr, struct{}{})
	t.Cleanup(func() { fallbackHosts.Delete(addr) })
	if info := probe(t, srv); !info.Fallback {
		t.Fatalf("got %+v, want fallback", info)
	}
}
