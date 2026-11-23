package tui

import (
	"strings"
	"testing"
	"time"
)

// TestStatusShownThenHints checks a new status message replaces the key
// hints in the status bar, and the hints come back once it has been shown
// for statusShowFor. Before this, BMC Info never showed a.status at all, so
// mount, eject and power results and their errors were invisible.
func TestStatusShownThenHints(t *testing.T) {
	a := &App{currentScreen: screenMCInfo, contentH: 30}
	hints := a.screenStatusHint()
	if !strings.Contains(hints, "[V] Virtual Media") {
		t.Fatalf("no status yet: got %q", hints)
	}
	a.status = "Virtual media error: boom"
	if got := a.screenStatusHint(); got != a.status {
		t.Fatalf("new status: got %q", got)
	}
	a.statusSeenAt = time.Now().Add(-statusShowFor - time.Second)
	if got := a.screenStatusHint(); got != hints {
		t.Fatalf("after %v: got %q", statusShowFor, got)
	}
	a.status = "ISO mounted via Redfish"
	if got := a.screenStatusHint(); got != a.status {
		t.Fatalf("second status: got %q", got)
	}
	a.activeDialog = NewSOLTakeoverDialog("h")
	if got := a.screenStatusHint(); !strings.HasPrefix(got, "[Tab] Next") {
		t.Fatalf("dialog hints must win: got %q", got)
	}
}
