package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestRenderModalBorderAligned checks the top border's corner lines up with
// the side borders below it. lipgloss.Place centres each line on its own
// width, so lines of unequal width get unequal left padding — the top border
// (no shadow cell) used to land one column right of the body.
func TestRenderModalBorderAligned(t *testing.T) {
	for _, screenW := range []int{80, 81, 100, 101, 120, 121} {
		for _, body := range []string{"x", "xy", "Connect to BMC body text", "odd width body!"} {
			out := ansi.Strip(renderModal("Title", body, screenW, 30))
			var left, right []int
			for _, line := range strings.Split(out, "\n") {
				r := []rune(line)
				for i, c := range r {
					if c == '╭' || c == '│' || c == '╰' {
						left = append(left, i)
						for j := len(r) - 1; j > i; j-- {
							if r[j] == '╮' || r[j] == '│' || r[j] == '╯' {
								right = append(right, j)
								break
							}
						}
						break
					}
				}
			}
			if len(left) < 3 {
				t.Fatalf("w=%d body=%q: border not found in\n%s", screenW, body, out)
			}
			for i := range left {
				if left[i] != left[0] || right[i] != right[0] {
					t.Errorf("w=%d body=%q: border line %d at cols %d..%d, top at %d..%d",
						screenW, body, i, left[i], right[i], left[0], right[0])
					break
				}
			}
		}
	}
}

// TestDialogButtonsWrap checks a long button row wraps instead of running
// past an 80-column screen (Redfish can offer six power actions).
func TestDialogButtonsWrap(t *testing.T) {
	d := &Dialog{Title: "Power Control", Body: "Host: h"}
	for _, l := range []string{"Power On", "Graceful Shutdown", "Press Power Button", "Force Restart", "Power Cycle", "Force Off", "Cancel"} {
		d.buttons = append(d.buttons, DialogButton{Label: l, Action: l})
	}
	out := ansi.Strip(d.Render(80, 30))
	for _, line := range strings.Split(out, "\n") {
		if w := len([]rune(line)); w > 80 {
			t.Fatalf("line %d columns wide:\n%s", w, out)
		}
	}
	for _, l := range []string{"Graceful Shutdown", "Force Off", "Cancel"} {
		if !strings.Contains(out, "[ "+l+" ]") {
			t.Fatalf("button %q missing:\n%s", l, out)
		}
	}
}

// TestStatusBarLongMessage checks an over-long status is cut to one line,
// keeping its start, instead of wrapping and showing only its tail.
func TestStatusBarLongMessage(t *testing.T) {
	msg := "/home/someone/a/very/long/path/to/an/image/fyrtaarn-test.img attached to 192.168.139.4 as a virtual floppy; it stays attached until Eject or exit"
	out := ansi.Strip(StatusBar(80, msg, "[F9] Menu"))
	if strings.Contains(out, "\n") || len([]rune(out)) != 80 {
		t.Fatalf("bar is %d columns, %d lines:\n%s", len([]rune(out)), strings.Count(out, "\n")+1, out)
	}
	if !strings.HasPrefix(out, " /home/someone") || !strings.Contains(out, "…") || !strings.HasSuffix(out, "[F9] Menu ") {
		t.Fatalf("got %q", out)
	}
}
