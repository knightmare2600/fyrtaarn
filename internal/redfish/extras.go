package redfish

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/knightmare2600/fyrtaarn/internal/quirks"
)

// Event log, user accounts and firmware inventory over Redfish, for BMCs
// used without IPMI. Built to the DMTF schemas (LogService/LogEntry,
// AccountService/ManagerAccount, UpdateService/SoftwareInventory); not yet
// checked against captures of real BMCs, which scripts/fy-capture.sh now
// collects.

// maxLogEntries caps entries per log service: only the first page of each
// Entries collection is read (iDRAC's Lifecycle log alone runs to
// thousands of entries).
const maxLogEntries = 200

// LogEntry is one event, from any log service.
type LogEntry struct {
	Log      string // log service name, e.g. "IML", "SEL"
	ID       string
	Created  string
	Severity string // "OK", "Warning", "Critical"
	Message  string
}

// ErrILO4Logs is returned instead of reading log entries on iLO 4.
// The cause is in GetEventLogs' comment; the message is kept short for the
// status bar.
var ErrILO4Logs = errors.New("skipped on iLO 4 (reading it stalled an iLO 4's web server); use [L] on BMC Info (IPMI)")

// observeManagers records the managers' firmware strings ("iLO 4 v2.54")
// with internal/quirks, so it can be asked about host.
func observeManagers(c *http.Client, host, base, user, pass string) {
	col, err := fetchJSON(c, base+"/redfish/v1/Managers", user, pass)
	if err != nil {
		return
	}
	for _, p := range collectionPaths(col) {
		if m, err := fetchJSON(c, base+p, user, pass); err == nil {
			if fw := firstStr(m, "FirmwareVersion"); fw != "" {
				quirks.Observe(host, quirks.Identity{RedfishFirmware: fw})
			}
		}
	}
}

// GetEventLogs reads the first page of every log service under Systems
// and Managers, newest first. Not on iLO 4: the first request for its IML
// entries stalled an iLO 4 2.54 (ProLiant ML310e Gen8 v2) so that its web
// server answered nothing for minutes, on a production box.
func GetEventLogs(host, user, pass string) ([]LogEntry, error) {
	c := httpClient()
	base := "https://" + host
	observeManagers(c, host, base, user, pass)
	if quirks.Has(host, quirks.RedfishLogEntriesStall) {
		return nil, ErrILO4Logs
	}
	var out []LogEntry
	found := false
	for _, col := range []string{"/redfish/v1/Systems", "/redfish/v1/Managers"} {
		cl, err := fetchJSON(c, base+col, user, pass)
		if err != nil {
			continue
		}
		for _, p := range collectionPaths(cl) {
			res, err := fetchJSON(c, base+p, user, pass)
			if err != nil {
				continue
			}
			ls := link(res, "LogServices")
			if ls == "" {
				continue
			}
			svcCol, err := fetchJSON(c, base+ls, user, pass)
			if err != nil {
				continue
			}
			for _, sp := range collectionPaths(svcCol) {
				found = true
				out = append(out, logEntries(c, base, sp, user, pass)...)
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("no Redfish log services on this BMC")
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Created > out[j].Created })
	return out, nil
}

func logEntries(c *http.Client, base, svcPath, user, pass string) []LogEntry {
	svc, err := fetchJSON(c, base+svcPath, user, pass)
	if err != nil {
		return nil
	}
	name := firstStr(svc, "Name", "Id")
	ep := link(svc, "Entries")
	if ep == "" {
		return nil
	}
	col, err := fetchJSON(c, base+ep, user, pass)
	if err != nil {
		return nil
	}
	var out []LogEntry
	for _, m := range items(col, "Members") {
		if len(out) >= maxLogEntries {
			break
		}
		// Members are usually expanded entries; fetch any that are links.
		if _, ok := m["Message"]; !ok {
			if p, _ := m["@odata.id"].(string); p != "" {
				if e, err := fetchJSON(c, base+p, user, pass); err == nil {
					m = e
				}
			}
		}
		out = append(out, LogEntry{
			Log:      name,
			ID:       firstStr(m, "Id"),
			Created:  firstStr(m, "Created"),
			Severity: firstStr(m, "Severity"),
			Message:  strings.TrimSpace(firstStr(m, "Message")),
		})
	}
	return out
}

// rootLink follows a link from the service root, since paths differ (iLO
// ends them in "/"). An absent service is reported as such: the iLO 4 in
// the captures (FW 2.54) has no UpdateService at all.
func rootLink(c *http.Client, base, user, pass, key string) (string, error) {
	root, err := fetchJSON(c, base+"/redfish/v1/", user, pass)
	if err != nil {
		return "", err
	}
	if l := link(root, key); l != "" {
		return l, nil
	}
	return "", fmt.Errorf("this BMC's Redfish has no %s", key)
}

// Account is a BMC user account.
type Account struct {
	ID       string
	UserName string
	Role     string
	Enabled  bool
	Locked   bool
}

// GetAccounts lists AccountService accounts with a user name (BMCs list
// their empty slots too).
func GetAccounts(host, user, pass string) ([]Account, error) {
	c := httpClient()
	base := "https://" + host
	sp, err := rootLink(c, base, user, pass, "AccountService")
	if err != nil {
		return nil, err
	}
	svc, err := fetchJSON(c, base+sp, user, pass)
	if err != nil {
		return nil, err
	}
	ap := link(svc, "Accounts")
	if ap == "" {
		return nil, fmt.Errorf("no Redfish accounts collection on this BMC")
	}
	col, err := fetchJSON(c, base+ap, user, pass)
	if err != nil {
		return nil, err
	}
	var out []Account
	for _, p := range collectionPaths(col) {
		a, err := fetchJSON(c, base+p, user, pass)
		if err != nil {
			continue
		}
		name := firstStr(a, "UserName")
		if name == "" {
			continue
		}
		en, lk := accountState(a)
		out = append(out, Account{ID: firstStr(a, "Id"), UserName: name, Role: firstStr(a, "RoleId"), Enabled: en, Locked: lk})
	}
	return out, nil
}

// Firmware is one firmware inventory item.
type Firmware struct {
	ID, Name, Version string
}

// GetFirmware reads UpdateService's firmware inventory.
func GetFirmware(host, user, pass string) ([]Firmware, error) {
	c := httpClient()
	base := "https://" + host
	sp, err := rootLink(c, base, user, pass, "UpdateService")
	if err != nil {
		return nil, err
	}
	svc, err := fetchJSON(c, base+sp, user, pass)
	if err != nil {
		return nil, err
	}
	fp := link(svc, "FirmwareInventory")
	if fp == "" {
		return nil, fmt.Errorf("no Redfish firmware inventory on this BMC")
	}
	col, err := fetchJSON(c, base+fp, user, pass)
	if err != nil {
		return nil, err
	}
	var out []Firmware
	for _, p := range collectionPaths(col) {
		f, err := fetchJSON(c, base+p, user, pass)
		if err != nil {
			continue
		}
		out = append(out, Firmware{ID: firstStr(f, "Id"), Name: firstStr(f, "Name"), Version: firstStr(f, "Version")})
	}
	return out, nil
}

// accountState reads whether an account is enabled and locked. iLO 5 (FW
// 1.40) sends neither Enabled nor Locked; whether the account may log in is
// HPE's Oem.Hpe.Privileges.LoginPriv. With neither, a named account is
// taken as enabled: BMCs list named accounts because they exist.
func accountState(a map[string]any) (enabled, locked bool) {
	locked, _ = a["Locked"].(bool)
	if en, ok := a["Enabled"].(bool); ok {
		return en, locked
	}
	if oem, ok := a["Oem"].(map[string]any); ok {
		if hpe, ok := oem["Hpe"].(map[string]any); ok {
			if pr, ok := hpe["Privileges"].(map[string]any); ok {
				if login, ok := pr["LoginPriv"].(bool); ok {
					return login, locked
				}
			}
		}
	}
	return true, locked
}
