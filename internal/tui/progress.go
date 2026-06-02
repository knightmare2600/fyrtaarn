package tui

import (
	"fmt"
	"strings"
)

type Progress struct {
	Total   int
	Current int
	Label   string
}

// Render draws a full-width progress bar for use in the loading screen.
func (p Progress) Render(width int) string {
	if p.Total == 0 {
		return p.Label + " ..."
	}
	ratio := float64(p.Current) / float64(p.Total)
	barWidth := width - 20
	if barWidth < 1 {
		barWidth = 1
	}
	filled := int(ratio * float64(barWidth))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	return fmt.Sprintf("%s [%s] %d/%d", p.Label, bar, p.Current, p.Total)
}

// RenderCompact draws a narrow progress bar suitable for the status bar.
// barWidth controls only the inner fill characters, not the brackets or counts.
func (p Progress) RenderCompact(barWidth int) string {
	if p.Total == 0 || barWidth < 1 {
		return fmt.Sprintf("%d/%d", p.Current, p.Total)
	}
	ratio := float64(p.Current) / float64(p.Total)
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(barWidth))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	return fmt.Sprintf("[%s] %d/%d", bar, p.Current, p.Total)
}
