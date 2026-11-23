// Package webproxy serves a BMC's HTTPS web interface on a local plain-HTTP
// port, so any modern browser can use it even when the BMC only speaks TLS
// that browsers have long since dropped (see internal/legacytls).
//
// It listens on 127.0.0.1 only: the hop from the browser to the proxy never
// leaves the machine, and the hop from the proxy to the BMC is whatever TLS
// the BMC can manage.
package webproxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/knightmare2600/fyrtaarn/internal/legacytls"
)

// Proxy is a running local proxy for one BMC.
type Proxy struct {
	Host string         // BMC address
	URL  string         // http://127.0.0.1:<port>/
	Port int            // local port, for SSH forwarding hints
	TLS  legacytls.Info // what the first connection negotiated
	srv  *http.Server
}

// Close stops the listener and drops open connections.
func (p *Proxy) Close() error { return p.srv.Close() }

// Start checks the BMC answers TLS on port 443, then serves it on a free
// local port. The check makes a bad host fail here, with the TLS error,
// rather than as a blank browser tab.
func Start(host string) (*Proxy, error) {
	return start(host, net.JoinHostPort(host, "443"))
}

func start(host, upstream string) (*Proxy, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	info, err := legacytls.Probe(ctx, upstream)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", upstream, err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("could not open a local port: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	local := "http://" + ln.Addr().String()

	p := &Proxy{
		Host: host,
		URL:  local + "/",
		Port: port,
		TLS:  info,
		srv: &http.Server{
			Handler:           handler(upstream, local),
			ReadHeaderTimeout: 30 * time.Second,
		},
	}
	go func() { _ = p.srv.Serve(ln) }()
	return p, nil
}

// urlHost is addr as it appears in an https URL: without :443, IPv6 in
// brackets.
func urlHost(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port != "443" {
		return addr
	}
	if strings.Contains(host, ":") {
		return "[" + host + "]"
	}
	return host
}

// remotePattern matches absolute URLs pointing back at the BMC: https with
// or without :443, or plain http (which old BMCs redirect to https anyway).
// The trailing group stops 10.0.0.4 matching inside 10.0.0.40; replacements
// put it back with ${1}.
func remotePattern(addr string) *regexp.Regexp {
	alt := regexp.QuoteMeta(addr)
	if h := urlHost(addr); h != addr {
		alt = regexp.QuoteMeta(h) + `(?::443|:80)?`
	}
	return regexp.MustCompile(`(?i)https?://(?:` + alt + `)([^\w.:-]|$)`)
}

func handler(upstream, local string) http.Handler {
	host := urlHost(upstream)
	target := &url.URL{Scheme: "https", Host: host}
	remote := remotePattern(upstream)
	repl := local + "${1}"
	localRE := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(local) + `([^\w.:-]|$)`)

	tr := legacytls.Transport()
	tr.ResponseHeaderTimeout = 60 * time.Second

	return &httputil.ReverseProxy{
		Transport: tr,
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Host = host
			// AMI MegaRAC pages tell the BMC their own address in EXTRNIP
			// when fetching the Java console launch file; from a browser on
			// the proxy that is 127.0.0.1, which would point JViewer's KVM
			// and media connections at this machine instead of the BMC.
			if q := r.Out.URL.Query(); q.Has("EXTRNIP") {
				q.Set("EXTRNIP", host)
				r.Out.URL.RawQuery = q.Encode()
			}
			// Ask for an uncompressed body so links in it can be rewritten.
			r.Out.Header.Del("Accept-Encoding")
			// Embedded servers that check these for CSRF expect their own
			// origin, not the proxy's.
			for _, h := range []string{"Origin", "Referer"} {
				if v := r.Out.Header.Get(h); v != "" {
					r.Out.Header.Set(h, localRE.ReplaceAllString(v, target.String()+"${1}"))
				}
			}
		},
		ModifyResponse: func(resp *http.Response) error {
			if loc := resp.Header.Get("Location"); loc != "" {
				resp.Header.Set("Location", remote.ReplaceAllString(loc, repl))
			}
			if cs := resp.Header.Values("Set-Cookie"); len(cs) > 0 {
				resp.Header.Del("Set-Cookie")
				for _, c := range cs {
					resp.Header.Add("Set-Cookie", localCookie(c))
				}
			}
			return rewriteBody(resp, remote, repl)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "fyrtaarn proxy: "+host+": "+err.Error(), http.StatusBadGateway)
		},
	}
}

// localCookie makes a BMC cookie usable on http://127.0.0.1: browsers drop
// Secure cookies on plain HTTP, reject a Domain that is not 127.0.0.1, and
// reject SameSite=None without Secure.
func localCookie(c string) string {
	parts := strings.Split(c, ";")
	out := parts[:1]
	for _, a := range parts[1:] {
		k, v, _ := strings.Cut(strings.TrimSpace(a), "=")
		switch strings.ToLower(k) {
		case "secure", "domain":
			continue
		case "samesite":
			if strings.EqualFold(v, "none") {
				continue
			}
		}
		out = append(out, a)
	}
	return strings.Join(out, ";")
}

// rewriteBody replaces absolute links back to the BMC in text responses.
// Binary responses (Java applets, images, firmware) pass through untouched.
func rewriteBody(resp *http.Response, remote *regexp.Regexp, repl string) error {
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	text := strings.HasPrefix(ct, "text/") ||
		strings.Contains(ct, "javascript") ||
		strings.Contains(ct, "json") ||
		strings.Contains(ct, "xml") ||
		strings.Contains(ct, "x-java-jnlp-file")
	if !text || resp.Header.Get("Content-Encoding") != "" {
		return nil
	}
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return err
	}
	b = remote.ReplaceAll(b, []byte(repl))
	resp.Body = io.NopCloser(bytes.NewReader(b))
	resp.ContentLength = int64(len(b))
	resp.Header.Set("Content-Length", strconv.Itoa(len(b)))
	return nil
}
