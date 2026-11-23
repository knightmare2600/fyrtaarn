package tui

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/knightmare2600/fyrtaarn/internal/discovery"
	"github.com/knightmare2600/fyrtaarn/internal/session"
)

// fbTree makes:  root/{sub/, a.iso, b.IMG, notes.txt, .hidden.iso}
// and, where symlinks work, root/link -> sub.
func fbTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.iso", "b.IMG", "notes.txt", ".hidden.iso", filepath.Join("sub", "inner.vfd")} {
		if err := os.WriteFile(filepath.Join(root, f), make([]byte, 2048), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if runtime.GOOS != "windows" {
		_ = os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "link"))
	}
	return root
}

func names(fb *fileBrowser) string {
	var n []string
	for _, e := range fb.entries {
		n = append(n, e.name)
	}
	return strings.Join(n, ",")
}

const fbH = 30 // content height used by the tests

func TestFileBrowserListing(t *testing.T) {
	root := fbTree(t)
	fb := newFileBrowser(root)
	want := "..,link,sub,a.iso,b.IMG" // folders first, images only, no dotfiles
	if runtime.GOOS == "windows" {
		want = "..,sub,a.iso,b.IMG"
	}
	if fb.dir != root || names(fb) != want {
		t.Fatalf("dir %s entries %s", fb.dir, names(fb))
	}
	fb.key("a", fbH)
	if !strings.Contains(names(fb), "notes.txt") {
		t.Fatalf("all files: %s", names(fb))
	}
	fb.key(".", fbH)
	if !strings.Contains(names(fb), ".hidden.iso") {
		t.Fatalf("hidden: %s", names(fb))
	}
	// Starting from a typed file path opens its folder.
	if got := newFileBrowser(filepath.Join(root, "a.iso")).dir; got != root {
		t.Fatalf("start from a file path: %s", got)
	}
}

func TestFileBrowserKeys(t *testing.T) {
	root := fbTree(t)
	fb := newFileBrowser(root)
	for fb.entries[fb.cursor].name != "sub" {
		fb.key("down", fbH)
	}
	fb.key("enter", fbH)
	if fb.dir != filepath.Join(root, "sub") || names(fb) != "..,inner.vfd" {
		t.Fatalf("into sub: %s %s", fb.dir, names(fb))
	}
	fb.key("backspace", fbH)
	if fb.dir != root || fb.entries[fb.cursor].name != "sub" {
		t.Fatalf("back up: %s cursor on %s", fb.dir, fb.entries[fb.cursor].name)
	}
	fb.key("end", fbH)
	chosen, done := fb.key("enter", fbH)
	if !done || chosen != filepath.Join(root, "b.IMG") {
		t.Fatalf("choose: %q %v", chosen, done)
	}
	if _, done := fb.key("esc", fbH); !done {
		t.Fatal("esc did not close")
	}
}

func TestFileBrowserMouse(t *testing.T) {
	root := fbTree(t)
	fb := newFileBrowser(root)
	click := func(row int) (string, bool) {
		return fb.mouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}, fbHeaderRows+row, fbH)
	}
	// The a.iso row, whichever entries precede it.
	row := strings.Index(","+names(fb)+",", ",a.iso,")
	row = strings.Count(("," + names(fb) + ",")[:row], ",")
	if _, done := click(row); done || fb.entries[fb.cursor].name != "a.iso" {
		t.Fatalf("first click should select a.iso, cursor on %s", fb.entries[fb.cursor].name)
	}
	if chosen, done := click(row); !done || chosen != filepath.Join(root, "a.iso") {
		t.Fatalf("second click should choose: %q %v", chosen, done)
	}
	// Clicks on the header do nothing; the wheel moves the cursor.
	fb.cursor = 0
	if _, done := fb.mouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}, 0, fbH); done || fb.cursor != 0 {
		t.Fatal("header click acted")
	}
	fb.mouse(tea.MouseMsg{Button: tea.MouseButtonWheelDown}, 5, fbH)
	if fb.cursor != min(3, len(fb.entries)-1) {
		t.Fatalf("wheel: cursor %d", fb.cursor)
	}
}

// TestFileBrowserWindowsDrives runs the Windows behaviour anywhere: going
// up from a drive root lists the drives, and the drive left is selected.
func TestFileBrowserWindowsDrives(t *testing.T) {
	fb := newFileBrowser(fbTree(t))
	fb.goos = "windows"
	fb.drives = func() []string { return []string{`C:\`, `D:\`} }
	fb.dir = string(filepath.Separator) // a root, as C:\ is on Windows
	fb.up()
	if fb.dir != "" || names(fb) != `C:\,D:\` {
		t.Fatalf("drives: dir %q entries %s", fb.dir, names(fb))
	}
	if !strings.Contains(fb.render(80, fbH), "Drives") {
		t.Fatal("drive list not labelled")
	}
}

func TestFileBrowserRenderWidth(t *testing.T) {
	root := fbTree(t)
	long := strings.Repeat("x", 120) + ".iso"
	if err := os.WriteFile(filepath.Join(root, long), nil, 0o644); err != nil {
		t.Skip("filesystem refuses long names")
	}
	out := ansi.Strip(newFileBrowser(root).render(80, fbH))
	for _, line := range strings.Split(out, "\n") {
		if len([]rune(line)) > 80 {
			t.Fatalf("line wider than 80 columns: %q", line)
		}
	}
}

// TestBrowseFromVirtualMediaDialog drives the whole round trip in the app.
func TestBrowseFromVirtualMediaDialog(t *testing.T) {
	root := fbTree(t)
	a := &App{
		sessionCache:  session.NewCache(),
		results:       []discovery.HostResult{{IP: "10.0.0.8", HasRedfish: true}},
		currentScreen: screenMCInfo,
		contentH:      fbH,
	}
	a.activeDialog = NewVirtualMediaDialog(true)
	a.activeDialog.inputs[0].SetValue(root)
	a.handleDialogAction("vm-browse")
	if a.currentScreen != screenFileBrowser || a.fb.dir != root || a.activeDialog != nil {
		t.Fatalf("browser not open on %s", root)
	}
	for a.fb.entries[a.fb.cursor].name != "a.iso" {
		a.Update(tea.KeyMsg{Type: tea.KeyDown})
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	d := a.activeDialog
	if a.currentScreen != screenMCInfo || d == nil || d.InputValue(0) != filepath.Join(root, "a.iso") ||
		d.buttons[d.focus-len(d.inputs)].Action != "vm-mount" {
		t.Fatalf("back in the dialog: screen %v value %q", a.currentScreen, d.InputValue(0))
	}
	// Esc from the browser returns to an empty dialog.
	a.handleDialogAction("vm-browse")
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if a.currentScreen != screenMCInfo || a.activeDialog == nil || a.activeDialog.InputValue(0) != "" {
		t.Fatal("Esc did not return to the dialog")
	}
}
