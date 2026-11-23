package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/knightmare2600/fyrtaarn/internal/ptyproc"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/knightmare2600/fyrtaarn/internal/discovery"
	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
)

func discoveryHost(ip string) discovery.HostResult {
	return discovery.HostResult{IP: ip, IsBMC: true}
}

// These tests use ordinary processes as stand-ins for ipmitool to pin down
// the pty teardown behaviour that broke SOL reconnect: closing the pty while
// listenSOL has a Read in flight did nothing, so ipmitool never exited.

func shortSOLTimeouts(t *testing.T) {
	t.Helper()
	e, s, k := solEscapeWait, solSigintWait, solKillWait
	solEscapeWait, solSigintWait, solKillWait = 300*time.Millisecond, time.Second, time.Second
	t.Cleanup(func() { solEscapeWait, solSigintWait, solKillWait = e, s, k })
}

func startTestPane(t *testing.T, name string, args ...string) *solPane {
	t.Helper()
	p, err := startSolProcess(exec.Command(name, args...), 80, 24)
	if err != nil {
		t.Fatalf("start %s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = ptyproc.KillNow(p.con)
	})
	return p
}

// blockedReader mirrors listenSOL: a Read that is in flight when F10 lands.
func blockedReader(p *solPane) chan struct{} {
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := p.con.Read(buf); err != nil {
				close(done)
				return
			}
		}
	}()
	return done
}

func assertGone(t *testing.T, p *solPane, reader chan struct{}) {
	t.Helper()
	select {
	case <-p.exited:
	default:
		t.Fatal("child still running after shutdown")
	}
	select {
	case <-reader:
	case <-time.After(2 * time.Second):
		t.Fatal("pty Read still blocked after shutdown")
	}
}

func TestShutdownIdleChildWithBlockedRead(t *testing.T) {
	shortSOLTimeouts(t)
	p := startTestPane(t, "sleep", "300")
	reader := blockedReader(p)
	time.Sleep(100 * time.Millisecond)

	if !p.shutdown() {
		t.Error("signalled child: want needDeactivate=true")
	}
	assertGone(t, p, reader)
}

func TestShutdownCleanEscapeExit(t *testing.T) {
	shortSOLTimeouts(t)
	// Exits 0 once it has read the two-byte escape sequence, like ipmitool.
	p := startTestPane(t, "sh", "-c", "stty raw -echo; head -c 2 >/dev/null; exit 0")
	reader := blockedReader(p)
	time.Sleep(200 * time.Millisecond)

	if p.shutdown() {
		t.Error("clean escape exit: want needDeactivate=false")
	}
	assertGone(t, p, reader)
}

func TestShutdownNoEscapeMidLine(t *testing.T) {
	shortSOLTimeouts(t)
	out := filepath.Join(t.TempDir(), "input")
	p := startTestPane(t, "sh", "-c", "stty raw -echo; exec cat > "+out)
	reader := blockedReader(p)
	time.Sleep(200 * time.Millisecond)

	p.write([]byte("abc")) // not at start of line: escape would reach the console
	time.Sleep(100 * time.Millisecond)
	if !p.shutdown() {
		t.Error("signalled child: want needDeactivate=true")
	}
	assertGone(t, p, reader)

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "abc" {
		t.Errorf("console received %q, want %q", got, "abc")
	}
	if strings.IndexByte(string(got), ipmi.SOLEscapeChar) >= 0 {
		t.Error("escape character sent mid-line")
	}
}

func TestShutdownEscalatesToKill(t *testing.T) {
	shortSOLTimeouts(t)
	// Ignored signal dispositions survive exec, so sleep ignores INT and TERM.
	p := startTestPane(t, "sh", "-c", "trap '' INT TERM; exec sleep 300")
	reader := blockedReader(p)
	time.Sleep(200 * time.Millisecond)

	if !p.shutdown() {
		t.Error("killed child: want needDeactivate=true")
	}
	assertGone(t, p, reader)
}

func TestShutdownAlreadyExited(t *testing.T) {
	shortSOLTimeouts(t)
	p := startTestPane(t, "true")
	if !p.waitExit(2 * time.Second) {
		t.Fatal("true did not exit")
	}
	if p.shutdown() {
		t.Error("process ended on its own: want needDeactivate=false")
	}
}

func TestStaleSOLReadDiscarded(t *testing.T) {
	shortSOLTimeouts(t)
	p := startTestPane(t, "sleep", "300")
	// On the SOL screen the mouse is already released (syncMouse).
	a := &App{solPane: p, solGeneration: 2, currentScreen: screenSOL, mouseReleased: true}

	_, cmd := a.Update(solReadMsg{Data: []byte("stale"), Gen: 1})
	if cmd != nil {
		t.Error("stale read re-armed a reader on the current pane")
	}
	if a.solPane.hasContent() {
		t.Error("stale read was ingested into the current pane")
	}
	p.shutdown()
}

func TestSOLInUseOffersTakeover(t *testing.T) {
	shortSOLTimeouts(t)
	// Stand-in for ipmitool refusing activation (completion code 0x80).
	p := startTestPane(t, "sh", "-c", "exit 1")
	if !p.waitExit(2 * time.Second) {
		t.Fatal("stand-in did not exit")
	}
	p.ingest([]byte("Info: SOL payload already active on another session\r\n"))

	a := &App{solPane: p, solGeneration: 1, currentScreen: screenSOL}
	a.results = append(a.results, discoveryHost("10.0.0.20"))
	a.Update(solDoneMsg{gen: 1, exitErr: p.exitErr})
	if a.activeDialog != nil {
		t.Fatal("takeover dialog shown before teardown finished")
	}
	a.Update(solClosedMsg{host: "10.0.0.20"})
	d := a.activeDialog
	if d == nil || d.Title != "SOL Console In Use" {
		t.Fatalf("want takeover dialog, got %+v", d)
	}
	if d.buttons[d.focus].Action != "cancel" {
		t.Errorf("takeover dialog defaults to %q, want cancel", d.buttons[d.focus].Action)
	}
}

func TestSOLOtherFailureNoTakeover(t *testing.T) {
	shortSOLTimeouts(t)
	p := startTestPane(t, "sh", "-c", "exit 1")
	p.waitExit(2 * time.Second)
	p.ingest([]byte("Error: Unable to establish IPMI v2 / RMCP+ session\r\n"))

	a := &App{solPane: p, solGeneration: 1, currentScreen: screenSOL}
	a.results = append(a.results, discoveryHost("10.0.0.21"))
	a.Update(solDoneMsg{gen: 1, exitErr: p.exitErr})
	a.Update(solClosedMsg{host: "10.0.0.21"})
	if a.activeDialog != nil {
		t.Errorf("non-in-use failure offered takeover: %+v", a.activeDialog)
	}
}

func TestMouseIgnoredDuringSOL(t *testing.T) {
	a := &App{currentScreen: screenSOL}
	a.Update(tea.MouseMsg{X: 1, Y: 0, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if a.currentScreen != screenSOL || a.activeDialog != nil {
		t.Errorf("menu click acted during SOL: screen %v dialog %+v", a.currentScreen, a.activeDialog)
	}
}

func TestSOLUnsupportedExplainsUpFront(t *testing.T) {
	old := solPaneSupported
	solPaneSupported = false
	t.Cleanup(func() { solPaneSupported = old })

	a := &App{currentScreen: screenMCInfo, username: "root"}
	a.results = append(a.results, discoveryHost("10.0.0.30"))
	// update, not Update: opening the dialog also releases the mouse
	// (syncMouse), which is a command too but not a chassis check.
	_, cmd := a.update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	d := a.activeDialog
	if d == nil || d.Title != "SOL Console Not Available" {
		t.Fatalf("want unsupported dialog, got %+v", d)
	}
	if cmd != nil || a.ipmiLoading {
		t.Error("started a chassis check despite SOL being unavailable")
	}
	for _, want := range []string{
		"-H 10.0.0.30 -U root -E sol activate",
		"set IPMITOOL_PASSWORD=",   // Command Prompt
		"$env:IPMITOOL_PASSWORD='", // PowerShell
	} {
		if !strings.Contains(d.Body, want) {
			t.Errorf("dialog lacks %q: %q", want, d.Body)
		}
	}
}

// TestKeyToBytesText checks typed and pasted text reaches the BMC as the
// characters themselves: '+' used to be dropped (key names containing '+'
// were taken for combinations like ctrl+x) and pastes arrived as "[text]".
func TestKeyToBytesText(t *testing.T) {
	cases := []struct {
		name string
		msg  tea.KeyMsg
		want string
	}{
		{"plus", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}}, "+"},
		{"letter", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, "a"},
		{"utf8", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("æøå")}, "æøå"},
		{"paste", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ls -l a+b"), Paste: true}, "ls -l a+b"},
		{"paste lines", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("one\ntwo\r\nthree"), Paste: true}, "one\rtwo\rthree"},
		{"alt", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}, Alt: true}, "\x1bx"},
		{"space", tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, " "},
		{"enter", tea.KeyMsg{Type: tea.KeyEnter}, "\r"},
		{"insert sends nothing, not its name", tea.KeyMsg{Type: tea.KeyInsert}, ""},
		{"ctrl+c", tea.KeyMsg{Type: tea.KeyCtrlC}, "\x03"},
	}
	for _, c := range cases {
		if got := string(keyToBytes(c.msg)); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}
