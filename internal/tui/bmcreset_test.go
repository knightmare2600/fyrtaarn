package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestBMCResetConfirmation(t *testing.T) {
	a := ipmiLanApp(true)
	a.handleMenuAction("bmc-reset")
	d := a.activeDialog
	if d == nil || d.focus != 0 || len(d.inputs) != 1 {
		t.Fatalf("dialog %+v", d)
	}
	out := ansi.Strip(d.Render(80, 50))
	for _, want := range []string{"management controller ONLY", "server it manages keeps running", "not a server reset and not a factory reset"} {
		if !strings.Contains(out, want) {
			t.Errorf("dialog lacks %q", want)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if len([]rune(line)) > 80 {
			t.Fatalf("wider than 80 columns:\n%s", out)
		}
	}

	// Enter in the address box moves to Cancel, never straight to Reset.
	action, _, _ := d.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if action != "" || d.buttons[d.focus-len(d.inputs)].Action != "cancel" {
		t.Fatalf("after Enter: action %q focus %d", action, d.focus)
	}

	// Wrong address: nothing sent.
	d.inputs[0].SetValue("10.0.0.6")
	if _, cmd := a.handleDialogAction("bmc-reset-confirm"); cmd != nil || !strings.Contains(a.status, "did not match") {
		t.Fatalf("wrong address: cmd %v status %q", cmd != nil, a.status)
	}

	// Exact address: the reset is sent.
	a.handleMenuAction("bmc-reset")
	a.activeDialog.inputs[0].SetValue("10.0.0.5")
	if _, cmd := a.handleDialogAction("bmc-reset-confirm"); cmd == nil || !a.ipmiLoading {
		t.Fatalf("matching address: no reset sent (status %q)", a.status)
	}
	a.handleBMCReset(bmcResetMsg{Host: "10.0.0.5", Via: "IPMI mc reset cold"})
	if !strings.Contains(a.status, "is restarting") {
		t.Fatalf("status %q", a.status)
	}
}
