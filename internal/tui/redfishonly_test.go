package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
	"github.com/knightmare2600/fyrtaarn/internal/redfish"
)

func TestRedfishOnlyMode(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // watching a sensor saves config.json
	a := ipmiLanApp(true)
	a.contentH = 30

	// Offered from a failed connect with IPMI over LAN off.
	a.handleIPMILanMsg(ipmiLanMsg{Host: "10.0.0.5", State: redfish.IPMIOverLAN{}, FromConnect: true, ConnectErr: rmcpErr})
	var actions []string
	for _, b := range a.activeDialog.buttons {
		actions = append(actions, b.Action)
	}
	if strings.Join(actions, ",") != "ipmilan-on,redfish-view,cancel" || a.activeDialog.buttons[a.activeDialog.focus].Action != "cancel" {
		t.Fatalf("buttons %v focus %d", actions, a.activeDialog.focus)
	}

	// Choosing it reads the system over Redfish without IPMI.
	_, cmd := a.handleDialogAction("redfish-view")
	if cmd == nil || !a.redfishOnly || !a.ipmiLoading {
		t.Fatal("Redfish view did not start")
	}
	a.ipmiLoading = false
	a.currentScreen = screenRedfish

	// [S]: Redfish sensors fill the Sensors screen; Esc comes back here.
	if _, cmd := a.updateRedfishEnum(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}); cmd == nil {
		t.Fatal("[S] did nothing")
	}
	a.handleRFSensors(rfSensorsMsg{Sensors: []redfish.ChassisSensor{{Name: "CPU1 Temp", Value: "42 degrees C", Status: "ok"}}})
	if a.currentScreen != screenSensor || !a.sensorsFromRedfish || len(a.sensors) != 1 || a.sensors[0].Value != "42 degrees C" {
		t.Fatalf("sensors %+v", a.sensors)
	}
	// Space + D: a Redfish-sourced dashboard, read straight away (no
	// repository dump), its watch list kept apart from the IPMI one.
	a.updateSensor(key(" "))
	if got := a.dashSensors["redfish:10.0.0.5"]; len(got) != 1 || got[0] != "CPU1 Temp" || len(a.dashSensors["10.0.0.5"]) != 0 {
		t.Fatalf("watch lists %v", a.dashSensors)
	}
	_, cmd = a.updateSensor(key("d"))
	if cmd == nil || a.currentScreen != screenDashboard || !a.dash.redfish || a.ipmiLoading {
		t.Fatalf("Redfish dashboard did not start: screen %v status %q", a.currentScreen, a.status)
	}
	if !strings.Contains(a.renderDashboard(), "(Redfish)") {
		t.Fatal("dashboard does not say its source")
	}
	a.updateDashboard(tea.KeyMsg{Type: tea.KeyEsc})
	a.updateSensor(tea.KeyMsg{Type: tea.KeyEsc})
	if a.currentScreen != screenRedfish {
		t.Fatal("Esc from Redfish sensors should return to the Redfish screen")
	}

	// [P]: only the BMC's allowed types, never Nmi, Cancel focused.
	a.handleRFPowerInfo(rfPowerInfoMsg{Host: "10.0.0.5", Info: redfish.PowerInfo{State: "On", Allowed: []string{"On", "ForceOff", "Nmi", "PushPowerButton"}}})
	d := a.activeDialog
	actions = actions[:0]
	for _, b := range d.buttons {
		actions = append(actions, b.Action)
	}
	if strings.Join(actions, ",") != "rfpower:On,rfpower:PushPowerButton,rfpower:ForceOff,cancel" || d.buttons[d.focus].Action != "cancel" {
		t.Fatalf("power buttons %v focus %d", actions, d.focus)
	}
	if _, cmd := a.handleDialogAction("rfpower:ForceOff"); cmd == nil || !strings.Contains(a.status, "ForceOff") {
		t.Fatalf("reset not sent: %q", a.status)
	}
	a.handleRFReset(rfResetMsg{Host: "10.0.0.5", Type: "ForceOff", Err: errors.New("HTTP 400")})
	if !strings.Contains(a.status, "failed: HTTP 400") {
		t.Fatalf("status %q", a.status)
	}

	// Esc from the Redfish screen goes to the host list: there is no BMC Info.
	a.activeDialog = nil
	a.currentScreen = screenRedfish
	a.updateRedfishEnum(tea.KeyMsg{Type: tea.KeyEsc})
	if a.currentScreen != screenResults {
		t.Fatal("Esc should return to the host list in Redfish-only mode")
	}
}

func TestRedfishViewNeedsRedfishAndCreds(t *testing.T) {
	a := ipmiLanApp(false)
	if _, cmd := a.openRedfishView(); cmd != nil || !strings.Contains(a.status, "does not offer Redfish") {
		t.Fatalf("no-Redfish host: %q", a.status)
	}
	a = ipmiLanApp(true)
	a.username, a.password = "", ""
	if _, cmd := a.openRedfishView(); cmd != nil || !strings.Contains(a.status, "No credentials") {
		t.Fatalf("no credentials: %q", a.status)
	}
}

// TestRedfishAccountsReadOnly checks Redfish accounts are never acted on
// with IPMI user commands, even when the Redfish view was opened from an
// IPMI session, and that Esc returns to the Redfish view.
func TestRedfishAccountsReadOnly(t *testing.T) {
	a := ipmiLanApp(true)
	a.contentH = 30
	a.currentScreen = screenRedfish // reached with [R] from BMC Info: not Redfish-only
	a.handleRFUsers(rfUsersMsg{Accounts: []redfish.Account{{ID: "2", UserName: "Administrator", Role: "Administrator", Enabled: true}}})
	if a.currentScreen != screenUsers || a.redfishOnly {
		t.Fatal("setup")
	}
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEnter}, {Type: tea.KeyRunes, Runes: []rune{'n'}}} {
		a.activeDialog = nil
		a.updateUsers(k)
		if a.activeDialog != nil || !strings.Contains(a.status, "read-only") {
			t.Fatalf("%v on a Redfish account opened %v (status %q)", k, a.activeDialog, a.status)
		}
	}
	a.updateUsers(tea.KeyMsg{Type: tea.KeyEsc})
	if a.currentScreen != screenRedfish {
		t.Fatalf("Esc went to %v, want the Redfish view", a.currentScreen)
	}
	// An IPMI user list afterwards is actionable again and returns to BMC Info.
	a.Update(usersMsg{Entries: []ipmi.UserEntry{{ID: 2, Name: "root", Enabled: true}}})
	if a.usersFromRedfish || a.backToRedfish {
		t.Fatal("IPMI user list still marked as Redfish")
	}
}
