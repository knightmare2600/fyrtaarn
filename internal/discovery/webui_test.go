package discovery

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
