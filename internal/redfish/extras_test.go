package redfish

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeExtras is a BMC shaped like the captured iLO 5 (trailing slashes,
// log services under both Systems and Managers), with DMTF LogEntry,
// ManagerAccount and SoftwareInventory documents.
func fakeExtras(t *testing.T, withUpdate bool) string {
	docs := map[string]string{
		"/redfish/v1/":                                     `{"AccountService":{"@odata.id":"/redfish/v1/AccountService/"}` + map[bool]string{true: `,"UpdateService":{"@odata.id":"/redfish/v1/UpdateService/"}`, false: ""}[withUpdate] + `}`,
		"/redfish/v1/Systems":                              `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/"}]}`,
		"/redfish/v1/Systems/1/":                           `{"LogServices":{"@odata.id":"/redfish/v1/Systems/1/LogServices/"}}`,
		"/redfish/v1/Systems/1/LogServices/":               `{"Members":[{"@odata.id":"/redfish/v1/Systems/1/LogServices/IML/"}]}`,
		"/redfish/v1/Systems/1/LogServices/IML/":           `{"Id":"IML","Name":"Integrated Management Log","Entries":{"@odata.id":"/redfish/v1/Systems/1/LogServices/IML/Entries/"}}`,
		"/redfish/v1/Systems/1/LogServices/IML/Entries/":   `{"Members":[{"Id":"1","Created":"2026-01-16T09:54:31Z","Severity":"Warning","Message":"Drive failed"},{"@odata.id":"/redfish/v1/Systems/1/LogServices/IML/Entries/2/"}]}`,
		"/redfish/v1/Systems/1/LogServices/IML/Entries/2/": `{"Id":"2","Created":"2026-02-01T08:00:00Z","Severity":"OK","Message":"POST complete"}`,
		"/redfish/v1/Managers":                             `{"Members":[{"@odata.id":"/redfish/v1/Managers/1/"}]}`,
		"/redfish/v1/Managers/1/":                          `{}`,
		"/redfish/v1/AccountService/":                      `{"Accounts":{"@odata.id":"/redfish/v1/AccountService/Accounts/"}}`,
		"/redfish/v1/AccountService/Accounts/":             `{"Members":[{"@odata.id":"/redfish/v1/AccountService/Accounts/1/"},{"@odata.id":"/redfish/v1/AccountService/Accounts/2/"}]}`,
		"/redfish/v1/AccountService/Accounts/1/":           `{"Id":"1","UserName":"Administrator","RoleId":"Administrator","Enabled":true,"Locked":false}`,
		"/redfish/v1/AccountService/Accounts/2/":           `{"Id":"2","UserName":"","Enabled":false}`,
		"/redfish/v1/UpdateService/":                       `{"FirmwareInventory":{"@odata.id":"/redfish/v1/UpdateService/FirmwareInventory/"}}`,
		"/redfish/v1/UpdateService/FirmwareInventory/":     `{"Members":[{"@odata.id":"/redfish/v1/UpdateService/FirmwareInventory/1/"}]}`,
		"/redfish/v1/UpdateService/FirmwareInventory/1/":   `{"Id":"1","Name":"iLO 5","Version":"1.40 Jan 14 2019"}`,
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, ok := docs[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(d))
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://")
}

func TestRedfishExtras(t *testing.T) {
	host := fakeExtras(t, true)

	logs, err := GetEventLogs(host, "u", "p")
	if err != nil || len(logs) != 2 || logs[0].Message != "POST complete" || logs[1].Severity != "Warning" || logs[0].Log != "Integrated Management Log" {
		t.Fatalf("logs %+v %v (want newest first, linked entry fetched)", logs, err)
	}
	acc, err := GetAccounts(host, "u", "p")
	if err != nil || len(acc) != 1 || acc[0].UserName != "Administrator" || !acc[0].Enabled || acc[0].Role != "Administrator" {
		t.Fatalf("accounts %+v %v (empty slots left out)", acc, err)
	}
	fw, err := GetFirmware(host, "u", "p")
	if err != nil || len(fw) != 1 || fw[0].Version != "1.40 Jan 14 2019" {
		t.Fatalf("firmware %+v %v", fw, err)
	}

	if _, err := GetFirmware(fakeExtras(t, false), "u", "p"); err == nil || !strings.Contains(err.Error(), "has no UpdateService") {
		t.Fatalf("missing UpdateService: %v", err)
	}
}

// TestAccountState covers the standard fields and iLO 5's shape (no
// Enabled/Locked; Oem.Hpe.Privileges.LoginPriv instead).
func TestAccountState(t *testing.T) {
	cases := []struct {
		doc          map[string]any
		enabled, lck bool
	}{
		{map[string]any{"Enabled": false, "Locked": true}, false, true},
		{map[string]any{"Oem": map[string]any{"Hpe": map[string]any{"Privileges": map[string]any{"LoginPriv": true}}}}, true, false},
		{map[string]any{"Oem": map[string]any{"Hpe": map[string]any{"Privileges": map[string]any{"LoginPriv": false}}}}, false, false},
		{map[string]any{"UserName": "x"}, true, false},
	}
	for i, c := range cases {
		if en, lk := accountState(c.doc); en != c.enabled || lk != c.lck {
			t.Errorf("case %d: got %v,%v", i, en, lk)
		}
	}
}

// TestEventLogsSkipILO4 checks no log entries are requested from an iLO 4.
func TestEventLogsSkipILO4(t *testing.T) {
	var entriesAsked bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redfish/v1/Managers":
			_, _ = w.Write([]byte(`{"Members":[{"@odata.id":"/redfish/v1/Managers/1/"}]}`))
		case "/redfish/v1/Managers/1/":
			_, _ = w.Write([]byte(`{"FirmwareVersion":"iLO 4 v2.54","LogServices":{"@odata.id":"/redfish/v1/Managers/1/LogServices/"}}`))
		default:
			if strings.Contains(r.URL.Path, "Entries") {
				entriesAsked = true
			}
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	_, err := GetEventLogs(strings.TrimPrefix(srv.URL, "https://"), "u", "p")
	if !errors.Is(err, ErrILO4Logs) || entriesAsked {
		t.Fatalf("err %v, entries requested %v", err, entriesAsked)
	}
}
