package redfish

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeSystem answers like the captured iLO 4/5 systems: inline
// ResetType@Redfish.AllowableValues, no GracefulShutdown.
func fakeSystem(t *testing.T, got *[]string) string {
	mux := http.NewServeMux()
	mux.HandleFunc("/redfish/v1/Systems", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Systems/1/"}]}`))
	})
	mux.HandleFunc("/redfish/v1/Systems/1/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"PowerState":"On","Actions":{"#ComputerSystem.Reset":{"target":"/redfish/v1/Systems/1/Actions/ComputerSystem.Reset/","ResetType@Redfish.AllowableValues":["On","ForceOff","ForceRestart","Nmi","PushPowerButton"]}}}`))
	})
	mux.HandleFunc("/redfish/v1/Systems/1/Actions/ComputerSystem.Reset/", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ ResetType string }
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil || r.Method != http.MethodPost {
			t.Errorf("reset request: %v %s", err, r.Method)
		}
		*got = append(*got, b.ResetType)
	})
	srv := httptest.NewTLSServer(mux)
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://")
}

func TestRedfishPower(t *testing.T) {
	var got []string
	host := fakeSystem(t, &got)
	info, err := GetPowerInfo(host, "u", "p")
	if err != nil || info.State != "On" {
		t.Fatalf("%+v %v", info, err)
	}
	var labels []string
	for _, c := range info.Choices() {
		labels = append(labels, c.Type)
	}
	// Order fixed by fyrtaarn, limited to what the BMC allows, never Nmi.
	if strings.Join(labels, ",") != "On,PushPowerButton,ForceRestart,ForceOff" {
		t.Fatalf("choices %v", labels)
	}
	if err := ResetSystem(host, "u", "p", "ForceRestart"); err != nil || strings.Join(got, ",") != "ForceRestart" {
		t.Fatalf("reset: %v %v", err, got)
	}
	if err := ResetSystem(host, "u", "p", "GracefulShutdown"); err == nil || len(got) != 1 {
		t.Fatalf("disallowed type was sent: %v %v", err, got)
	}
}

func TestRedfishPowerChoicesUnlisted(t *testing.T) {
	if n := len((PowerInfo{}).Choices()); n != len(resetLabels) {
		t.Fatalf("BMC that lists nothing: %d choices", n)
	}
}

func TestChassisSensorFields(t *testing.T) {
	th := map[string]any{
		"Temperatures": []any{
			map[string]any{"Name": "CPU1 Temp", "ReadingCelsius": 42.0, "Status": map[string]any{"Health": "OK", "State": "Enabled"}},
			map[string]any{"Name": "12-CPU 2", "ReadingCelsius": 0.0, "Status": map[string]any{"State": "Absent"}},
		},
		"Fans": []any{
			// iLO 4 shape: FanName, CurrentReading, Units.
			map[string]any{"FanName": "Fan 1", "CurrentReading": 5.0, "Units": "Percent", "Status": map[string]any{"Health": "OK"}},
			map[string]any{"Name": "Fan2", "Reading": 1920.0, "ReadingUnits": "RPM", "Status": map[string]any{"Health": "Warning"}},
		},
	}
	ss := thermalSensors(th)
	want := []ChassisSensor{
		{"CPU1 Temp", "42 degrees C", "ok"},
		{"Fan 1", "5 percent", "ok"},
		{"Fan2", "1920 RPM", "warning"},
	}
	if len(ss) != len(want) {
		t.Fatalf("got %+v", ss)
	}
	for i := range want {
		if ss[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, ss[i], want[i])
		}
	}
	pw := powerSensors(map[string]any{
		"Voltages":      []any{map[string]any{"Name": "PS1 Voltage", "ReadingVolts": 230.0}},
		"PowerControl":  []any{map[string]any{"PowerConsumedWatts": 89.0}},
		"PowerSupplies": []any{map[string]any{"Name": "PSU1", "LastPowerOutputWatts": 120.0, "Status": map[string]any{"Health": "OK"}}},
	})
	if len(pw) != 3 || pw[0].Value != "230 Volts" || pw[1].Name != "Power Consumption 1" || pw[1].Value != "89 Watts" || pw[2].Name != "PSU1 Output" {
		t.Fatalf("power %+v", pw)
	}
}

// TestResetManager uses the shapes of the captured iDRAC9 (only
// GracefulRestart allowed) and iLO 5 (no list, HPE OEM actions beside it).
func TestResetManager(t *testing.T) {
	for _, c := range []struct {
		name, mgr, wantType, wantBody string
	}{
		{"idrac", `{"Actions":{"#Manager.Reset":{"target":"/redfish/v1/Managers/1/Actions/Manager.Reset","ResetType@Redfish.AllowableValues":["GracefulRestart"]}}}`, "GracefulRestart", `{"ResetType":"GracefulRestart"}`},
		{"ilo", `{"Actions":{"#Manager.Reset":{"target":"/redfish/v1/Managers/1/Actions/Manager.Reset"}},"Oem":{"Hpe":{"Actions":{"#HpeiLO.ResetToFactoryDefaults":{"target":"/redfish/v1/Managers/1/Actions/Oem/Hpe/HpeiLO.ResetToFactoryDefaults"}}}}}`, "", `{}`},
	} {
		var posts []string
		var body string
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/redfish/v1/Managers":
				_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Managers/1"}]}`))
			case r.URL.Path == "/redfish/v1/Managers/1":
				_, _ = w.Write([]byte(c.mgr))
			case r.Method == http.MethodPost:
				posts = append(posts, r.URL.Path)
				b, _ := io.ReadAll(r.Body)
				body = string(b)
			}
		}))
		got, err := ResetManager(strings.TrimPrefix(srv.URL, "https://"), "u", "p")
		srv.Close()
		if err != nil || got != c.wantType || body != c.wantBody || len(posts) != 1 || posts[0] != "/redfish/v1/Managers/1/Actions/Manager.Reset" {
			t.Errorf("%s: type %q body %s posts %v err %v", c.name, got, body, posts, err)
		}
	}
}
