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
