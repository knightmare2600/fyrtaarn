package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
)

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
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
	})
	return p
}

// blockedReader mirrors listenSOL: a Read that is in flight when F10 lands.
func blockedReader(p *solPane) chan struct{} {
	done := make(chan struct{})
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := p.ptmx.Read(buf); err != nil {
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
	a := &App{solPane: p, solGeneration: 2, currentScreen: screenSOL}

	_, cmd := a.Update(solReadMsg{Data: []byte("stale"), Gen: 1})
	if cmd != nil {
		t.Error("stale read re-armed a reader on the current pane")
	}
	if a.solPane.hasContent() {
		t.Error("stale read was ingested into the current pane")
	}
	p.shutdown()
}
