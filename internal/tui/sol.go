package tui

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/creack/pty"
	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
)

/* ---------------- MESSAGES ---------------- */

type solPaneReadyMsg struct {
	pane *solPane
}

type solReadMsg struct {
	Data []byte
	Err  error
}

type solDoneMsg struct{}

/* ---------------- PANE ---------------- */

const solMaxLines = 1000

type solPane struct {
	ptmx      *os.File
	lines     []string
	partial   string
	pendingCR bool
	scrollUp  int // 0 = pinned to bottom; N = scrolled N lines up
}

func (s *solPane) resize(cols, rows int) {
	_ = pty.Setsize(s.ptmx, &pty.Winsize{
		Rows: uint16(max(rows, 1)),
		Cols: uint16(max(cols, 1)),
	})
}

func (s *solPane) close() {
	_ = s.ptmx.Close()
}

func (s *solPane) write(b []byte) {
	_, _ = s.ptmx.Write(b)
}

// ingest strips ANSI escapes, handles CR/LF/backspace, and appends to the
// line buffer. pendingCR tracks bare \r (overwrites current line) vs \r\n
// (normal line ending).
func (s *solPane) ingest(raw []byte) {
	for _, b := range stripANSI(raw) {
		switch {
		case b == '\r':
			s.pendingCR = true

		case b == '\n':
			s.pendingCR = false
			s.lines = append(s.lines, s.partial)
			s.partial = ""
			if len(s.lines) > solMaxLines {
				s.lines = s.lines[len(s.lines)-solMaxLines:]
			}

		case b == '\b':
			s.pendingCR = false
			runes := []rune(s.partial)
			if len(runes) > 0 {
				s.partial = string(runes[:len(runes)-1])
			}

		default:
			if s.pendingCR {
				s.pendingCR = false
				s.partial = "" // bare CR — overwrite current line
			}
			if b >= 0x20 {
				s.partial += string(rune(b))
			}
		}
	}
}

/* ---------------- COMMANDS ---------------- */

// startSOLPane forks ipmitool sol activate inside a pty sized to (cols×rows).
// Returns solPaneReadyMsg on success, solReadMsg{Err} on failure.
func startSOLPane(host, user, pass string, cols, rows int) tea.Cmd {
	return func() tea.Msg {
		if runtime.GOOS == "windows" {
			return solReadMsg{Err: fmt.Errorf("built-in SOL pane not supported on Windows")}
		}
		cmd := ipmi.SOLCmd(host, user, pass)
		ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
			Rows: uint16(max(rows, 24)),
			Cols: uint16(max(cols, 80)),
		})
		if err != nil {
			return solReadMsg{Err: err}
		}
		return solPaneReadyMsg{pane: &solPane{ptmx: ptmx}}
	}
}

// listenSOL blocks on one pty read and returns the result as a tea.Msg.
// Re-issue this command after each solReadMsg to keep the read loop running.
func listenSOL(ptmx *os.File) tea.Cmd {
	return func() tea.Msg {
		buf := make([]byte, 4096)
		n, err := ptmx.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			return solReadMsg{Data: data}
		}
		if err != nil {
			if err == io.EOF {
				return solDoneMsg{}
			}
			return solDoneMsg{}
		}
		return solReadMsg{} // zero-byte read, no error — retry
	}
}

/* ---------------- KEY MAPPING ---------------- */

// keyToBytes converts a bubbletea KeyMsg to the byte sequence the pty expects.
// Returns nil for keys that should not be forwarded (e.g. the disconnect key).
func keyToBytes(msg tea.KeyMsg) []byte {
	switch msg.String() {
	case "enter":
		return []byte{'\r'}
	case "backspace":
		return []byte{'\x7f'}
	case "tab":
		return []byte{'\t'}
	case "up":
		return []byte{'\x1b', '[', 'A'}
	case "down":
		return []byte{'\x1b', '[', 'B'}
	case "right":
		return []byte{'\x1b', '[', 'C'}
	case "left":
		return []byte{'\x1b', '[', 'D'}
	case "ctrl+a":
		return []byte{'\x01'}
	case "ctrl+b":
		return []byte{'\x02'}
	case "ctrl+c":
		return []byte{'\x03'}
	case "ctrl+d":
		return []byte{'\x04'}
	case "ctrl+e":
		return []byte{'\x05'}
	case "ctrl+f":
		return []byte{'\x06'}
	case "ctrl+g":
		return []byte{'\x07'}
	case "ctrl+h":
		return []byte{'\x08'}
	case "ctrl+k":
		return []byte{'\x0b'}
	case "ctrl+l":
		return []byte{'\x0c'}
	case "ctrl+n":
		return []byte{'\x0e'}
	case "ctrl+o":
		return []byte{'\x0f'}
	case "ctrl+p":
		return []byte{'\x10'}
	case "ctrl+q":
		return []byte{'\x11'}
	case "ctrl+r":
		return []byte{'\x12'}
	case "ctrl+s":
		return []byte{'\x13'}
	case "ctrl+t":
		return []byte{'\x14'}
	case "ctrl+u":
		return []byte{'\x15'}
	case "ctrl+v":
		return []byte{'\x16'}
	case "ctrl+w":
		return []byte{'\x17'}
	case "ctrl+x":
		return []byte{'\x18'}
	case "ctrl+y":
		return []byte{'\x19'}
	case "ctrl+z":
		return []byte{'\x1a'}
	case "esc":
		return []byte{'\x1b'}
	case "home":
		return []byte{'\x1b', '[', 'H'}
	case "end":
		return []byte{'\x1b', '[', 'F'}
	case "delete":
		return []byte{'\x1b', '[', '3', '~'}
	case "f1":
		return []byte{'\x1b', 'O', 'P'}
	case "f2":
		return []byte{'\x1b', 'O', 'Q'}
	case "f3":
		return []byte{'\x1b', 'O', 'R'}
	case "f4":
		return []byte{'\x1b', 'O', 'S'}
	case "f5":
		return []byte{'\x1b', '[', '1', '5', '~'}
	case "f6":
		return []byte{'\x1b', '[', '1', '7', '~'}
	case "f7":
		return []byte{'\x1b', '[', '1', '8', '~'}
	case "f8":
		return []byte{'\x1b', '[', '1', '9', '~'}
	case "f9":
		return []byte{'\x1b', '[', '2', '0', '~'}
	case "f10":
		return nil // disconnect key — never forward
	case "f11":
		return []byte{'\x1b', '[', '2', '3', '~'}
	case "f12":
		return []byte{'\x1b', '[', '2', '4', '~'}
	case "space":
		return []byte{' '}
	}
	// Plain printable rune(s): msg.String() returns the character directly.
	s := msg.String()
	if !strings.Contains(s, "+") && s != "" {
		return []byte(s)
	}
	return nil
}

/* ---------------- ANSI STRIPPER ---------------- */

// stripANSI removes ANSI/VT100 escape sequences so raw terminal output can be
// stored and rendered as plain text in the pane.
func stripANSI(b []byte) []byte {
	out := make([]byte, 0, len(b))
	i := 0
	for i < len(b) {
		if b[i] != 0x1b {
			out = append(out, b[i])
			i++
			continue
		}
		i++ // consume ESC
		if i >= len(b) {
			break
		}
		switch b[i] {
		case '[': // CSI — parameters until final byte 0x40–0x7e
			i++
			for i < len(b) && !(b[i] >= 0x40 && b[i] <= 0x7e) {
				i++
			}
			if i < len(b) {
				i++
			}
		case ']': // OSC — until BEL or ST (ESC \)
			i++
			for i < len(b) {
				if b[i] == 0x07 {
					i++
					break
				}
				if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		case 'P', 'X', '^', '_': // DCS / SOS / PM / APC — until ST
			i++
			for i < len(b) {
				if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		default: // simple two-byte ESC sequence
			i++
		}
	}
	return out
}
