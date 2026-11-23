package redfish

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeNP is a Redfish BMC with one manager and a NetworkProtocol resource,
// shaped like iLO 5's (Managers/1/, NetworkProtocol/ links with trailing
// slashes, @odata.etag on the resource).
type fakeNP struct {
	mu        sync.Mutex
	ipmi      map[string]any // nil: resource has no IPMI entry
	ignore    bool           // accept PATCH but keep the old value
	refuse    bool           // answer PATCH with a Redfish error
	needMatch bool           // reject PATCH without the right If-Match
	patches   int
}

func (f *fakeNP) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/redfish/v1/Managers", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Managers/1/"}]}`))
	})
	mux.HandleFunc("/redfish/v1/Managers/1/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"NetworkProtocol":{"@odata.id":"/redfish/v1/Managers/1/NetworkProtocol/"}}`))
	})
	mux.HandleFunc("/redfish/v1/Managers/1/NetworkProtocol/", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if u, p, _ := r.BasicAuth(); u != "admin" || p != "pw" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method {
		case http.MethodGet:
			doc := map[string]any{"@odata.etag": `W/"abc"`, "HTTPS": map[string]any{"ProtocolEnabled": true, "Port": 443}}
			if f.ipmi != nil {
				doc["IPMI"] = f.ipmi
			}
			_ = json.NewEncoder(w).Encode(doc)
		case http.MethodPatch:
			f.patches++
			if f.needMatch && r.Header.Get("If-Match") != `W/"abc"` {
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			if f.refuse {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":{"code":"Base.1.4.GeneralError","message":"See ExtendedInfo","@Message.ExtendedInfo":[{"MessageId":"Base.1.4.PropertyNotWritable","Message":"The property IPMI is read only."}]}}`))
				return
			}
			var body struct {
				IPMI struct{ ProtocolEnabled bool }
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("PATCH body: %v", err)
			}
			if !f.ignore {
				f.ipmi["ProtocolEnabled"] = body.IPMI.ProtocolEnabled
			}
			_, _ = w.Write([]byte(`{"error":{"@Message.ExtendedInfo":[{"MessageId":"iLO.2.14.ResetRequired"}]}}`))
		}
	})
	return mux
}

func startNP(t *testing.T, f *fakeNP) string {
	t.Helper()
	srv := httptest.NewTLSServer(f.handler(t))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://")
}

func TestIPMIOverLAN(t *testing.T) {
	f := &fakeNP{ipmi: map[string]any{"ProtocolEnabled": false, "Port": 623}, needMatch: true}
	host := startNP(t, f)

	st, err := GetIPMIOverLAN(host, "admin", "pw")
	if err != nil || st.Enabled || st.Port != 623 || !strings.HasSuffix(st.URL, "/Managers/1/NetworkProtocol/") {
		t.Fatalf("get: %+v %v", st, err)
	}
	if err := SetIPMIOverLAN(host, "admin", "pw", true); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if st, _ := GetIPMIOverLAN(host, "admin", "pw"); !st.Enabled {
		t.Fatal("still off after enable")
	}
	// Already in the requested state: no PATCH sent.
	before := f.patches
	if err := SetIPMIOverLAN(host, "admin", "pw", true); err != nil || f.patches != before {
		t.Fatalf("no-op enable: err=%v patches %d->%d", err, before, f.patches)
	}
	if err := SetIPMIOverLAN(host, "admin", "pw", false); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := GetIPMIOverLAN(host, "admin", "wrong"); err == nil {
		t.Fatal("wrong password accepted")
	}
}

func TestIPMIOverLANIgnored(t *testing.T) {
	f := &fakeNP{ipmi: map[string]any{"ProtocolEnabled": false}, ignore: true}
	err := SetIPMIOverLAN(startNP(t, f), "admin", "pw", true)
	if err == nil || !strings.Contains(err.Error(), "still reports IPMI over LAN off") || !strings.Contains(err.Error(), "reset") {
		t.Fatalf("err = %v", err)
	}
}

func TestIPMIOverLANRefused(t *testing.T) {
	f := &fakeNP{ipmi: map[string]any{"ProtocolEnabled": false}, refuse: true}
	err := SetIPMIOverLAN(startNP(t, f), "admin", "pw", true)
	if err == nil || !strings.Contains(err.Error(), "HTTP 400: The property IPMI is read only.") {
		t.Fatalf("err = %v", err)
	}
}

func TestIPMIOverLANAbsent(t *testing.T) {
	_, err := GetIPMIOverLAN(startNP(t, &fakeNP{}), "admin", "pw")
	if !errors.Is(err, ErrNoIPMIProtocol) {
		t.Fatalf("err = %v", err)
	}
}
