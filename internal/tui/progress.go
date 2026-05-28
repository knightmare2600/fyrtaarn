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

func (p Progress) Render(width int) string {

  if p.Total == 0 {
    return p.Label + " ..."
  }

  ratio := float64(p.Current) / float64(p.Total)
  barWidth := width - 20

  filled := int(ratio * float64(barWidth))

  bar := strings.Repeat("█", filled) +
    strings.Repeat("░", barWidth-filled)

  return fmt.Sprintf(
    "%s [%s] %d/%d",
    p.Label,
    bar,
    p.Current,
    p.Total,
  )
}
