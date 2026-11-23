package discovery

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/knightmare2600/fyrtaarn/internal/quirks"
)

// The Quanta's GoAhead-Webs front page answers / with a 302 to
// /index.html, served gzip-compressed whatever the request asked for.
func TestProbeWebUI(t *testing.T) {
	page := "<!--\n;**** AMI copyright ****;\n-->\n<html>\n<head>\n\t<title>Megarac SP</title>\n"
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	_, _ = zw.Write([]byte(page))
	_ = zw.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			http.Redirect(w, r, "/index.html", http.StatusFound)
		case "/index.html":
			w.Header().Set("Content-Encoding", "gzip")
			_, _ = w.Write(gz.Bytes())
		}
	}))
	defer srv.Close()
	if got := probeWebUI(strings.TrimPrefix(srv.URL, "https://")); got != "AMI MegaRAC SP" {
		t.Fatalf("got %q", got)
	}

	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><title>Some Router</title></html>"))
	}))
	defer other.Close()
	if got := probeWebUI(strings.TrimPrefix(other.URL, "https://")); got != "" {
		t.Fatalf("unknown title gave %q", got)
	}
}

// TestFingerprintAtScan checks a scan recognises the iLO 4's firmware from
// its front page ETag, before any login.
func TestFingerprintAtScan(t *testing.T) {
	quirks.Reset()
	t.Cleanup(quirks.Reset)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("ETag", `"00ce4159"`)
			_, _ = w.Write([]byte("<html><title>iLO 4</title></html>"))
			return
		}
		http.NotFound(w, r) // no Redfish
	}))
	defer srv.Close()
	ip := strings.TrimPrefix(srv.URL, "https://")
	rs := EnrichResults([]HostResult{{IP: ip, IsBMC: true}})
	if rs[0].Fingerprint != "HP iLO 4, firmware 2.54" || !quirks.Has(ip, quirks.NoCipherSuiteList) {
		t.Fatalf("fingerprint %q, quirks %v", rs[0].Fingerprint, quirks.For(ip))
	}
}
