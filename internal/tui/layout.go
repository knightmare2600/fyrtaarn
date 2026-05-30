package tui

import (
  "strings"

  "github.com/charmbracelet/lipgloss"
)

var (
  dimStyle = lipgloss.NewStyle().
    Foreground(lipgloss.Color("240"))

  centerStyle = lipgloss.NewStyle().
    Align(lipgloss.Center)
)

// StatusBar renders a full-width bar with left content left-aligned and right
// content right-aligned, both using the current theme colours.
func StatusBar(width int, left, right string) string {
  if width == 0 {
    return ""
  }

  base := lipgloss.NewStyle().
    Background(lipgloss.Color(CurrentTheme.Chrome)).
    Foreground(lipgloss.Color(CurrentTheme.ChromeFg))

  l := " " + left
  r := right + " "

  lw := lipgloss.Width(l)
  rw := lipgloss.Width(r)

  gap := width - lw - rw
  if gap < 0 {
    gap = 0
  }

  content := l + strings.Repeat(" ", gap) + r
  return base.Width(width).Render(content)
}

// center vertically + horizontally in a fixed viewport.
func center(width, height int, content string) string {
  lines := strings.Split(content, "\n")
  block := lipgloss.JoinVertical(lipgloss.Left, lines...)
  return centerStyle.
    Width(width).
    Height(height).
    Render(block)
}

// modal wraps content in the themed rounded border box.
func modal(_ int, _ int, content string) string {
  return BorderStyle().Render(content)
}

// dim wraps content in a dimmed style (used for background layers).
func dim(content string) string {
  return dimStyle.Render(content)
}
