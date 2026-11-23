package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// fileBrowser is a full-screen picker for a local image, opened from the
// Virtual Media dialog's Browse... button. It is a screen rather than a
// dialog so that mouse rows map to entries at fixed positions (content
// starts on screen row 1, under the menu bar) and so mouse reporting stays
// on (dialogs release the mouse for copy and paste).
//
// Paths use the platform's own separators (path/filepath). On Windows,
// going up from a drive's root lists the drives.

// imageExts are the files shown unless "all files" is on: what Mount and
// Upload to BMC take (CD images; disk and floppy images).
var imageExts = map[string]bool{".iso": true, ".img": true, ".ima": true, ".vfd": true}

type fbEntry struct {
	name  string // shown; ".." for the parent; "C:\" for a drive
	path  string // full path to open or choose
	isDir bool
	size  int64
}

type fileBrowser struct {
	dir        string // "" while listing Windows drives
	entries    []fbEntry
	cursor     int
	offset     int
	showHidden bool
	showAll    bool
	err        string // last listing error, shown under the header

	// goos and drives are runtime.GOOS and listDrives, replaceable by tests
	// so the Windows behaviour is tested on any platform.
	goos   string
	drives func() []string
}

// fbHeaderRows is how many content rows come before the first entry: the
// header, the path line and a rule. Mouse rows are counted from there.
const fbHeaderRows = 3

func newFileBrowser(start string) *fileBrowser {
	fb := &fileBrowser{goos: runtime.GOOS, drives: listDrives}
	fb.open(startDir(start))
	return fb
}

// startDir picks where to start: the folder of the path already typed if
// it exists, else the home folder, else the working folder.
func startDir(typed string) string {
	if typed = strings.TrimSpace(expandHome(typed)); typed != "" {
		for _, d := range []string{typed, filepath.Dir(typed)} {
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				return d
			}
		}
	}
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return string(filepath.Separator)
}

// listDrives returns the Windows drive roots that exist, e.g. "C:\".
func listDrives() []string {
	var out []string
	for c := 'A'; c <= 'Z'; c++ {
		root := string(c) + `:\`
		if _, err := os.Stat(root); err == nil {
			out = append(out, root)
		}
	}
	return out
}

// isRoot reports whether dir has no parent folder.
func isRoot(dir string) bool { return filepath.Dir(dir) == dir }

// open lists dir; on error the current listing stays and the error shows.
func (fb *fileBrowser) open(dir string) {
	if dir == "" && fb.goos == "windows" {
		fb.listDriveRoots()
		return
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		fb.err = err.Error()
		if fb.entries == nil && dir != "" && !isRoot(dir) {
			fb.open(filepath.Dir(dir)) // first open failed: try the parent
		}
		return
	}
	var dirs, files []fbEntry
	for _, de := range des {
		name := de.Name()
		if !fb.showHidden && strings.HasPrefix(name, ".") {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := os.Stat(full) // follows symlinks to folders
		if err != nil {
			continue // dangling link or no permission: leave it out
		}
		if info.IsDir() {
			dirs = append(dirs, fbEntry{name: name, path: full, isDir: true})
			continue
		}
		if !fb.showAll && !imageExts[strings.ToLower(filepath.Ext(name))] {
			continue
		}
		files = append(files, fbEntry{name: name, path: full, size: info.Size()})
	}
	byName := func(es []fbEntry) {
		sort.Slice(es, func(i, j int) bool { return strings.ToLower(es[i].name) < strings.ToLower(es[j].name) })
	}
	byName(dirs)
	byName(files)
	var entries []fbEntry
	if !isRoot(dir) || fb.goos == "windows" {
		parent := filepath.Dir(dir)
		if isRoot(dir) {
			parent = "" // Windows: up from a drive root lists the drives
		}
		entries = append(entries, fbEntry{name: "..", path: parent, isDir: true})
	}
	prev := fb.dir
	fb.dir, fb.err = dir, ""
	fb.entries = append(append(entries, dirs...), files...)
	fb.cursor, fb.offset = 0, 0
	// Coming up from a child folder: put the cursor on it.
	for i, e := range fb.entries {
		if e.isDir && e.path == prev && e.name != ".." {
			fb.cursor = i
		}
	}
}

func (fb *fileBrowser) listDriveRoots() {
	prev := fb.dir
	fb.dir, fb.err = "", ""
	fb.entries = nil
	for _, d := range fb.drives() {
		fb.entries = append(fb.entries, fbEntry{name: d, path: d, isDir: true})
	}
	fb.cursor, fb.offset = 0, 0
	for i, e := range fb.entries {
		if strings.EqualFold(e.path, filepath.VolumeName(prev)+`\`) {
			fb.cursor = i
		}
	}
}

// activate opens a folder or chooses a file; chosen is the file's path.
func (fb *fileBrowser) activate() (chosen string) {
	if len(fb.entries) == 0 {
		return ""
	}
	e := fb.entries[fb.cursor]
	if e.isDir {
		fb.open(e.path)
		return ""
	}
	return e.path
}

func (fb *fileBrowser) up() {
	switch {
	case fb.dir == "":
		// already listing drives
	case isRoot(fb.dir) && fb.goos == "windows":
		fb.open("")
	case !isRoot(fb.dir):
		fb.open(filepath.Dir(fb.dir))
	}
}

// visibleRows is how many entries fit in a content area of height h.
func fbVisibleRows(h int) int { return max(h-fbHeaderRows-1, 1) }

func (fb *fileBrowser) move(delta, h int) {
	if len(fb.entries) == 0 {
		return
	}
	fb.cursor = min(max(fb.cursor+delta, 0), len(fb.entries)-1)
	fb.scrollTo(h)
}

func (fb *fileBrowser) scrollTo(h int) {
	rows := fbVisibleRows(h)
	if fb.cursor < fb.offset {
		fb.offset = fb.cursor
	}
	if fb.cursor >= fb.offset+rows {
		fb.offset = fb.cursor - rows + 1
	}
}

// key handles a key; done is true when the browser closes, with chosen
// set if a file was picked.
func (fb *fileBrowser) key(k string, h int) (chosen string, done bool) {
	rows := fbVisibleRows(h)
	switch k {
	case "esc", "q":
		return "", true
	case "up", "k":
		fb.move(-1, h)
	case "down", "j":
		fb.move(1, h)
	case "pgup":
		fb.move(-rows, h)
	case "pgdown", " ":
		fb.move(rows, h)
	case "home", "g":
		fb.move(-len(fb.entries), h)
	case "end", "G":
		fb.move(len(fb.entries), h)
	case "enter", "right", "l":
		if p := fb.activate(); p != "" {
			return p, true
		}
	case "backspace", "left", "h":
		fb.up()
	case "~":
		if home, err := os.UserHomeDir(); err == nil {
			fb.open(home)
		}
	case ".":
		fb.showHidden = !fb.showHidden
		fb.open(fb.dir)
	case "a":
		fb.showAll = !fb.showAll
		fb.open(fb.dir)
	}
	return "", false
}

// mouse handles a mouse event at content row y (screen row minus the menu
// bar): a click selects an entry, a click on the selected entry opens or
// chooses it, the wheel scrolls.
func (fb *fileBrowser) mouse(m tea.MouseMsg, y, h int) (chosen string, done bool) {
	switch m.Button {
	case tea.MouseButtonWheelUp:
		fb.move(-3, h)
		return "", false
	case tea.MouseButtonWheelDown:
		fb.move(3, h)
		return "", false
	case tea.MouseButtonLeft:
		if m.Action != tea.MouseActionPress {
			return "", false
		}
	default:
		return "", false
	}
	i := fb.offset + y - fbHeaderRows
	if y < fbHeaderRows || i < 0 || i >= len(fb.entries) || i >= fb.offset+fbVisibleRows(h) {
		return "", false
	}
	if i != fb.cursor {
		fb.cursor = i
		return "", false
	}
	if p := fb.activate(); p != "" {
		return p, true
	}
	return "", false
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func (fb *fileBrowser) render(w, h int) string {
	var b strings.Builder
	where := fb.dir
	if where == "" {
		where = "Drives"
	}
	filter := ".iso .img .ima .vfd"
	if fb.showAll {
		filter = "all files"
	}
	b.WriteString(HeaderStyle().Render("SELECT IMAGE") + "  " + filter + "\n")
	if fb.err != "" {
		b.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#CB4B16")).Render(fb.err) + "\n")
	} else {
		b.WriteString("  " + where + "\n")
	}
	b.WriteString("  " + strings.Repeat("─", max(min(w-4, 76), 10)) + "\n")
	if len(fb.entries) == 0 {
		b.WriteString("  (no folders or images here; [A] shows all files)\n")
		return b.String()
	}
	sel := lipgloss.NewStyle().Reverse(true)
	nameW := max(min(w-20, 60), 12)
	rows := fbVisibleRows(h)
	for i := fb.offset; i < len(fb.entries) && i < fb.offset+rows; i++ {
		e := fb.entries[i]
		name, size := e.name, ""
		if e.isDir && e.name != ".." && fb.dir != "" {
			name += string(filepath.Separator)
		}
		if !e.isDir {
			size = humanSize(e.size)
		}
		if r := []rune(name); len(r) > nameW {
			name = string(r[:nameW-1]) + "…"
		}
		line := fmt.Sprintf("%-*s %10s", nameW, name, size)
		if i == fb.cursor {
			line = sel.Render(line)
		}
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}
