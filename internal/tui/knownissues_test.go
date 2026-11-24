package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/knightmare2600/fyrtaarn/internal/discovery"
	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
	"github.com/knightmare2600/fyrtaarn/internal/quirks"
	"github.com/knightmare2600/fyrtaarn/internal/session"
)

// TestKnownIssuesQuanta uses the Quanta's real mc info identity.
func TestKnownIssuesQuanta(t *testing.T) {
	// A successful connect saves Recent Connections to config.json.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	quirks.Reset()
	t.Cleanup(quirks.Reset)
	a := &App{
		sessionCache:  session.NewCache(),
		detailsCache:  map[string]*ipmi.HostDetails{},
		results:       []discovery.HostResult{{IP: "192.168.139.4"}},
		currentScreen: screenMCInfo,
		contentH:      40,
	}
	info := &ipmi.MCInfo{FirmwareRevision: "7.54", ManufacturerID: quirks.MfrQuanta, ProductID: 0x3252}
	a.Update(mcInfoMsg{Info: info})
	quirks.Learn("192.168.139.4", quirks.LegacyTLSOnly) // as the TLS fallback does
	if !strings.Contains(a.renderMCInfo(), "Known issues:            6  [K] details") {
		t.Fatalf("BMC Info:\n%s", a.renderMCInfo())
	}
	a.updateMCInfo(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if a.activeDialog == nil {
		t.Fatal("[K] opened nothing")
	}
	out := ansi.Strip(a.activeDialog.Render(80, 60))
	for _, want := range []string{"Known Issues — 192.168.139.4", "no matching cipher suite", "EXTRNIP", "status -1"} {
		if !strings.Contains(out, want) {
			t.Errorf("dialog lacks %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if len([]rune(line)) > 80 {
			t.Fatalf("wider than 80 columns:\n%s", out)
		}
	}

	// A host with no known issues has no line.
	if knownIssuesLine("10.1.1.1") != "" {
		t.Fatal("line shown for an unknown host")
	}
}

// TestDialogBodyScrolls: a body taller than the screen is cut to a window
// that fits, with its position shown, and moves with PgDn, arrows and End.
func TestDialogBodyScrolls(t *testing.T) {
	var lines []string
	for i := 1; i <= 60; i++ {
		lines = append(lines, fmt.Sprintf("line %02d", i))
	}
	d := &Dialog{Title: "Long", Body: strings.Join(lines, "\n"), buttons: []DialogButton{{Label: "Close", Action: "cancel"}}}
	for _, h := range []int{24, 40} {
		out := ansi.Strip(d.Render(80, h))
		if n := strings.Count(out, "\n") + 1; n > h {
			t.Fatalf("h=%d: rendered %d rows", h, n)
		}
		if !strings.Contains(out, "line 01") || strings.Contains(out, "line 60") || !strings.Contains(out, "of 60") {
			t.Fatalf("h=%d: first window wrong:\n%s", h, out)
		}
	}
	d.Render(80, 24)
	key := func(k tea.KeyType) { d.Update(tea.KeyMsg{Type: k}) }
	key(tea.KeyPgDown)
	if out := ansi.Strip(d.Render(80, 24)); strings.Contains(out, "line 01") || !strings.Contains(out, fmt.Sprintf("line %02d", d.bodyRows+1)) {
		t.Fatalf("PgDn:\n%s", out)
	}
	key(tea.KeyEnd)
	if out := ansi.Strip(d.Render(80, 24)); !strings.Contains(out, "line 60") {
		t.Fatalf("End:\n%s", out)
	}
	key(tea.KeyDown) // past the end: stays
	if out := ansi.Strip(d.Render(80, 24)); !strings.Contains(out, "line 60") {
		t.Fatal("Down past the end lost the last line")
	}
	key(tea.KeyHome)
	if out := ansi.Strip(d.Render(80, 24)); !strings.Contains(out, "line 01") {
		t.Fatal("Home")
	}
	if a, _, _ := d.Update(tea.KeyMsg{Type: tea.KeyEsc}); a != "cancel" {
		t.Fatal("Esc")
	}
	// A short body is untouched.
	short := &Dialog{Title: "Short", Body: "one\ntwo", buttons: []DialogButton{{Label: "Close", Action: "cancel"}}}
	if out := ansi.Strip(short.Render(80, 24)); strings.Contains(out, "scroll") || short.bodyRows != 0 {
		t.Fatal("short body scrolled")
	}
}
