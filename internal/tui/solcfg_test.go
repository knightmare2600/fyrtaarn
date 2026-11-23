package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
)

func TestSOLSettingsDialog(t *testing.T) {
	actions := func(d *Dialog) string {
		var a []string
		for _, b := range d.buttons {
			a = append(a, b.Action)
		}
		return strings.Join(a, ",")
	}
	on := NewSOLSettingsDialog("10.0.0.9", "admin", ipmi.SOLSettings{Enabled: true, BitRate: "115.2", NVBitRate: "115.2", Privilege: "ADMINISTRATOR", UserID: 2, UserPayload: "enabled"}, "")
	if actions(on) != "solcfg-disable,cancel" || on.buttons[on.focus].Action != "cancel" {
		t.Fatalf("SOL on: %s focus %d", actions(on), on.focus)
	}
	off := NewSOLSettingsDialog("10.0.0.9", "ops", ipmi.SOLSettings{UserID: 3, UserPayload: "disabled"}, "Change applied")
	if actions(off) != "solcfg-enable,solcfg-allow-user,cancel" {
		t.Fatalf("SOL off, user denied: %s", actions(off))
	}
	out := ansi.Strip(off.Render(80, 40))
	for _, want := range []string{"SOL on 10.0.0.9: OFF", "SOL access for ops (user 3): disabled", "serial port", "Change applied"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if len([]rune(line)) > 80 {
			t.Fatalf("wider than 80 columns:\n%s", out)
		}
	}
}
