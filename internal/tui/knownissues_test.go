package tui

import (
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
	if !strings.Contains(a.renderMCInfo(), "Known issues:            4  [K] details") {
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
