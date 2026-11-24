package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/knightmare2600/fyrtaarn/internal/discovery"
	"github.com/knightmare2600/fyrtaarn/internal/fleet"
	"github.com/knightmare2600/fyrtaarn/internal/session"
)

func fleetApp() *App {
	a := &App{
		sessionCache: session.NewCache(),
		results: []discovery.HostResult{
			{IP: "10.0.0.1", IsBMC: true}, {IP: "10.0.0.2", IsBMC: true}, {IP: "10.0.0.3"},
		},
		currentScreen: screenResults,
		width:         80,
		contentH:      30,
	}
	a.sessionCache.Set("10.0.0.1", "root", "pw")
	return a
}

func TestFleetAuditDialogAndNoLogin(t *testing.T) {
	a := fleetApp()
	a.openFleetAudit()
	body := ansi.Strip(a.activeDialog.Render(80, 40))
	if !strings.Contains(body, "Audit 2 BMCs") || !strings.Contains(body, "Saved logins (1 of 2)") {
		t.Fatalf("dialog:\n%s", body)
	}
	// No typed login, and only 10.0.0.1 saved: one target, the other left out.
	// Clear the saved one too: nothing to audit.
	a.sessionCache.Delete("10.0.0.1")
	a.activeDialog.inputs[0].SetValue("")
	a.startFleetAudit()
	if a.currentScreen == screenFleet || !strings.Contains(a.status, "no BMC has a login") {
		t.Fatalf("screen %v status %q", a.currentScreen, a.status)
	}
	a2 := &App{sessionCache: session.NewCache(), results: []discovery.HostResult{{IP: "10.0.0.9"}}}
	a2.openFleetAudit()
	if a2.activeDialog != nil || !strings.Contains(a2.status, "No BMCs") {
		t.Fatalf("no BMCs: %q", a2.status)
	}
}

func TestFleetScreen(t *testing.T) {
	a := fleetApp()
	a.fleetReturn = screenResults
	a.currentScreen = screenFleet
	a.fleet = &fleetState{gen: 1, running: true, results: []fleet.Result{{IP: "10.0.0.1"}, {IP: "10.0.0.2"}}}
	a.handleFleetProgress(fleetProgressMsg{gen: 1, r: fleet.Result{IP: "10.0.0.2", Err: "timeout"}})
	a.handleFleetProgress(fleetProgressMsg{gen: 7, r: fleet.Result{IP: "10.0.0.1", Err: "stale"}}) // other run: ignored
	if a.fleet.done != 1 || a.fleet.results[0].Err != "" {
		t.Fatalf("progress: %+v", a.fleet)
	}
	a.handleFleetDone(fleetDoneMsg{gen: 1, rs: []fleet.Result{
		{IP: "10.0.0.1", Manufacturer: "Quanta Computer Inc.", Firmware: "7.54", Power: "off", Suite0: "CALLBACK",
			Admins: []string{"admin"}, Findings: []fleet.Finding{
				{Severity: fleet.Critical, Text: `logs in with the AMI MegaRAC factory default password (user "admin")`},
				{Severity: fleet.Note, Text: "cipher suite 0 is enabled but capped at CALLBACK, below USER"}}},
		{IP: "10.0.0.2", Err: "timeout"},
	}})
	out := ansi.Strip(a.renderFleet())
	if !strings.Contains(out, "1 critical, 1 note") || !strings.Contains(out, "not reached") ||
		!strings.Contains(out, "1 BMC audited, 1 with critical findings, 1 not reached") {
		t.Fatalf("screen:\n%s", out)
	}
	for _, l := range strings.Split(out, "\n") {
		if len([]rune(l)) > 80 {
			t.Fatalf("wider than 80: %q", l)
		}
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if d := ansi.Strip(a.activeDialog.Render(80, 40)); !strings.Contains(d, "CRITICAL") || !strings.Contains(d, "Cipher suite 0: CALLBACK") {
		t.Fatalf("detail:\n%s", d)
	}
	a.activeDialog = nil
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if a.activeDialog == nil || a.activeDialog.Title != "Export Fleet Audit" {
		t.Fatal("E did not open the export dialog")
	}
	a.activeDialog.inputs[0].SetValue(t.TempDir() + "/audit.csv")
	a.exportFleet(false)
	if !strings.Contains(a.status, "written to") {
		t.Fatalf("export: %q", a.status)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if a.currentScreen != screenResults {
		t.Fatalf("Esc: screen %v", a.currentScreen)
	}
}
