package webproxy

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeBMC is a TLS 1.0-only server behaving like an old embedded web UI:
// absolute redirects to itself, Secure cookies, and absolute links in pages.
func fakeBMC(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	var self string
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, self+"/login.html", http.StatusFound)
	})
	mux.HandleFunc("/login.html", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "SessionCookie", Value: "abc", Path: "/",
			Domain: "bmc.example", Secure: true, SameSite: http.SameSiteNoneMode})
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<a href="`+self+`/page/main.html">main</a> <a href="`+self+`0/x">other host</a>`)
	})
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		// A response header, not the body: text bodies are rewritten.
		w.Header().Set("X-Echo", "host="+r.Host+" origin="+r.Header.Get("Origin")+" cookie="+r.Header.Get("Cookie"))
	})
	mux.HandleFunc("/Java/jviewer.jnlp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo", r.URL.RawQuery)
	})
	mux.HandleFunc("/bin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, self)
	})
	srv := httptest.NewUnstartedServer(mux)
	srv.TLS = &tls.Config{MaxVersion: tls.VersionTLS10, MinVersion: tls.VersionTLS10,
		CipherSuites: []uint16{tls.TLS_RSA_WITH_3DES_EDE_CBC_SHA}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	self = srv.URL
	return srv, strings.TrimPrefix(srv.URL, "https://")
}

func TestProxy(t *testing.T) {
	_, addr := fakeBMC(t)
	p, err := start("bmc", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if !strings.HasPrefix(p.URL, "http://127.0.0.1:") || p.TLS.Version != tls.VersionTLS10 {
		t.Fatalf("proxy %+v", p)
	}
	local := strings.TrimSuffix(p.URL, "/")

	noFollow := &http.Client{Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	t.Run("redirect", func(t *testing.T) {
		resp, err := noFollow.Get(p.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("Location"); got != local+"/login.html" {
			t.Fatalf("Location %q", got)
		}
	})

	t.Run("cookie and links", func(t *testing.T) {
		resp, err := noFollow.Get(local + "/login.html")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		c := resp.Header.Get("Set-Cookie")
		if strings.Contains(c, "Secure") || strings.Contains(c, "Domain") || strings.Contains(c, "SameSite=None") ||
			!strings.HasPrefix(c, "SessionCookie=abc") || !strings.Contains(c, "Path=/") {
			t.Fatalf("Set-Cookie %q", c)
		}
		want := `<a href="` + local + `/page/main.html">main</a> <a href="https://` + addr + `0/x">other host</a>`
		if string(body) != want {
			t.Fatalf("body\n got %s\nwant %s", body, want)
		}
		if resp.ContentLength != int64(len(want)) {
			t.Fatalf("Content-Length %d, body %d", resp.ContentLength, len(want))
		}
	})

	t.Run("request headers", func(t *testing.T) {
		req, _ := http.NewRequest("GET", local+"/echo", nil)
		req.Header.Set("Origin", local)
		req.Header.Set("Cookie", "SessionCookie=abc")
		resp, err := noFollow.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		want := "host=" + addr + " origin=https://" + addr + " cookie=SessionCookie=abc"
		if got := resp.Header.Get("X-Echo"); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})

	t.Run("EXTRNIP", func(t *testing.T) {
		resp, err := noFollow.Get(local + "/Java/jviewer.jnlp?EXTRNIP=127.0.0.1&JNLPSTR=JViewer")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		q, err := url.ParseQuery(resp.Header.Get("X-Echo"))
		if err != nil || q.Get("EXTRNIP") != addr || q.Get("JNLPSTR") != "JViewer" {
			t.Fatalf("query %q", resp.Header.Get("X-Echo"))
		}
	})

	t.Run("binary untouched", func(t *testing.T) {
		resp, err := noFollow.Get(local + "/bin")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != "https://"+addr {
			t.Fatalf("got %q", body)
		}
	})
}

func TestRemotePatternDefaultPort(t *testing.T) {
	re := remotePattern("10.0.0.4:443")
	repl := "http://127.0.0.1:9" + "${1}"
	cases := map[string]string{
		`"https://10.0.0.4/a"`:     `"http://127.0.0.1:9/a"`,
		`"https://10.0.0.4:443/a"`: `"http://127.0.0.1:9/a"`,
		`"http://10.0.0.4/a"`:      `"http://127.0.0.1:9/a"`,
		`"HTTPS://10.0.0.4"`:       `"http://127.0.0.1:9"`,
		`https://10.0.0.4`:         `http://127.0.0.1:9`,
		`"https://10.0.0.40/a"`:    `"https://10.0.0.40/a"`,
		`"https://10.0.0.4:8443/"`: `"https://10.0.0.4:8443/"`,
		`"https://10.0.0.4.nip/"`:  `"https://10.0.0.4.nip/"`,
	}
	for in, want := range cases {
		if got := re.ReplaceAllString(in, repl); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
	if got := urlHost("[fe80::1]:443"); got != "[fe80::1]" {
		t.Errorf("urlHost IPv6: %s", got)
	}
}

func TestStartNotTLS(t *testing.T) {
	// A port that answers with something other than TLS: Start reports the
	// error instead of serving a dead proxy.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = io.WriteString(c, "SSH-2.0-OpenSSH_9.9\r\n")
			_ = c.Close()
		}
	}()
	if _, err := start("bmc", ln.Addr().String()); err == nil {
		t.Fatal("want error")
	}
}
