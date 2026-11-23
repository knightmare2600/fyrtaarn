package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
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

// TestMouseReleasedForDialogsAndSOL checks mouse reporting is switched off
// while a dialog or SOL is up, so the terminal's own select-to-copy and
// right-click paste work there, and back on afterwards for the menu bar.
func TestMouseReleasedForDialogsAndSOL(t *testing.T) {
	a := &App{currentScreen: screenResults}
	if cmd := a.syncMouse(); cmd != nil || a.mouseReleased {
		t.Fatal("results screen: mouse should stay captured with no command")
	}
	a.activeDialog = NewSOLTakeoverDialog("h")
	if cmd := a.syncMouse(); cmd == nil || !a.mouseReleased {
		t.Fatal("dialog: mouse not released")
	}
	if cmd := a.syncMouse(); cmd != nil {
		t.Fatal("no change, but a command was sent again")
	}
	a.activeDialog = nil
	if cmd := a.syncMouse(); cmd == nil || a.mouseReleased {
		t.Fatal("dialog closed: mouse not captured again")
	}
	a.currentScreen = screenSOL
	if cmd := a.syncMouse(); cmd == nil || !a.mouseReleased {
		t.Fatal("SOL: mouse not released")
	}
	a.currentScreen = screenMCInfo
	a.activeFavDialog = &FavouritesDialog{}
	if cmd := a.syncMouse(); cmd != nil || !a.mouseReleased {
		t.Fatal("favourites dialog: mouse should stay released")
	}
}

// TestClipboardReplyReachesInput checks the message Ctrl+V's clipboard read
// produces reaches the focused input box. Dialogs were only sent key
// presses, so it was dropped and Ctrl+V never pasted. Without a clipboard
// (no display, as in CI) the reply is an error, recorded in the input's
// Err; with one, the text is inserted. Either proves the routing.
func TestClipboardReplyReachesInput(t *testing.T) {
	reply := textinput.Paste() // what Ctrl+V's command returns

	a := &App{activeDialog: NewConnectBMCDialog("", "")}
	before := a.activeDialog.inputs[0].Value()
	a.Update(reply)
	in := a.activeDialog.inputs[0]
	if in.Err == nil && in.Value() == before {
		t.Fatal("clipboard reply did not reach the Connect dialog's input")
	}

	f := NewFavouritesDialog(nil, nil)
	f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}) // open the add form
	a = &App{activeFavDialog: f}
	a.Update(reply)
	if f.nameInput.Err == nil && f.nameInput.Value() == "" {
		t.Fatal("clipboard reply did not reach the Favourites name field")
	}
}
