// colourmylife.go
//
// "Colour My Life" Named after the M-People track.
//
// Theme engine for Fyrtaarn.
//
// "Renaissance" is the default theme set:
// partly because of art, partly because of another M People reference.
//
// Yes, this is intentional.

package tui

import (
  "fmt"
  "strings"

  "github.com/charmbracelet/lipgloss"
)

type Theme struct {
  Name string

  Background string
  Foreground string

  Accent    string
  Highlight string
  Warning   string
  Error     string

  Border    string
  StatusBar string
}

var CurrentTheme = GetTheme("renaissance")

func GetTheme(name string) Theme {

  switch strings.ToLower(name) {

  case "british":
    return Theme{
      Name:       "british",
      Background: "#0000AA",
      Foreground: "#FFFFFF",
      Accent:     "#FF0000",
      Highlight:  "#FFFFFF",
      Warning:    "#FFFF00",
      Error:      "#FF4444",
      Border:     "#FFFFFF",
      StatusBar:  "#AA0000",
    }

  case "matrix":
    return Theme{
      Name:       "matrix",
      Background: "#000000",
      Foreground: "#00FF00",
      Accent:     "#00AA00",
      Highlight:  "#AAFFAA",
      Warning:    "#FFFF00",
      Error:      "#FF0000",
      Border:     "#00AA00",
      StatusBar:  "#003300",
    }

  case "teletext":
    return Theme{
      Name:       "teletext",
      Background: "#0000AA",
      Foreground: "#FFFFFF",
      Accent:     "#FFFF00",
      Highlight:  "#00FFFF",
      Warning:    "#FFAA00",
      Error:      "#FF5555",
      Border:     "#FFFFFF",
      StatusBar:  "#000000",
    }

  case "dsb":
    return Theme{
      Name:       "dsb",
      Background: "#AA0000",
      Foreground: "#FFFFFF",
      Accent:     "#FFFFFF",
      Highlight:  "#0000AA",
      Warning:    "#FFFF00",
      Error:      "#FF4444",
      Border:     "#FFFFFF",
      StatusBar:  "#770000",
    }

  case "scotrail":
    return Theme{
      Name:       "scotrail",
      Background: "#0044AA",
      Foreground: "#FFFFFF",
      Accent:     "#FFFF00",
      Highlight:  "#FFFFFF",
      Warning:    "#FFFF00",
      Error:      "#FF6666",
      Border:     "#FFFF00",
      StatusBar:  "#002266",
    }

  case "intercity-swallow":
    return Theme{
      Name:       "intercity-swallow",
      Background: "#222222",
      Foreground: "#FFFFFF",
      Accent:     "#CC0000",
      Highlight:  "#FFFFFF",
      Warning:    "#FFFF00",
      Error:      "#FF4444",
      Border:     "#777777",
      StatusBar:  "#550000",
    }

  case "irn-bru":
    return Theme{
      Name:       "irn-bru",
      Background: "#FF6600",
      Foreground: "#FFFFFF",
      Accent:     "#0000FF",
      Highlight:  "#FFFF00",
      Warning:    "#FFFF00",
      Error:      "#FF0000",
      Border:     "#FFFF00",
      StatusBar:  "#CC5500",
    }

  case "ns":
    return Theme{
      Name:       "ns",
      Background: "#FFFF00",
      Foreground: "#000000",
      Accent:     "#0033AA",
      Highlight:  "#FFFFFF",
      Warning:    "#FF8800",
      Error:      "#CC0000",
      Border:     "#0033AA",
      StatusBar:  "#0033AA",
    }

  case "network-southeast":
    return Theme{
      Name:       "network-southeast",
      Background: "#FFFFFF",
      Foreground: "#000000",
      Accent:     "#0000AA",
      Highlight:  "#CC0000",
      Warning:    "#FF8800",
      Error:      "#CC0000",
      Border:     "#0000AA",
      StatusBar:  "#CC0000",
    }

  case "viarail":
    return Theme{
      Name:       "viarail",
      Background: "#FFFF00",
      Foreground: "#000000",
      Accent:     "#003399",
      Highlight:  "#FFFFFF",
      Warning:    "#FF8800",
      Error:      "#CC0000",
      Border:     "#003399",
      StatusBar:  "#003399",
    }

  case "procomm":
    return Theme{
      Name:       "procomm",
      Background: "#000000",
      Foreground: "#FFFFFF",
      Accent:     "#0000FF",
      Highlight:  "#FFFF00",
      Warning:    "#FFFF00",
      Error:      "#FF0000",
      Border:     "#0000FF",
      StatusBar:  "#222222",
    }

  case "renaissance":
    fallthrough

  default:
    return Theme{
      Name:       "renaissance",
      Background: "#1E1E1E",
      Foreground: "#D0D0D0",
      Accent:     "#5FD7FF",
      Highlight:  "#FFFFFF",
      Warning:    "#FFD75F",
      Error:      "#FF5F5F",
      Border:     "#5F87AF",
      StatusBar:  "#303030",
    }
  }
}

func SetTheme(name string) {
  CurrentTheme = GetTheme(name)
}

func HeaderStyle() lipgloss.Style {
  return lipgloss.NewStyle().
    Bold(true).
    Foreground(
      lipgloss.Color(CurrentTheme.Accent),
    )
}

func MenuStyle(selected bool) lipgloss.Style {

  style := lipgloss.NewStyle().
    Foreground(
      lipgloss.Color(CurrentTheme.Foreground),
    ).
    Background(
      lipgloss.Color(CurrentTheme.Background),
    ).
    Padding(0, 1)

  if selected {

    style = style.
      Bold(true).
      Foreground(
        lipgloss.Color(CurrentTheme.Highlight),
      ).
      Background(
        lipgloss.Color(CurrentTheme.Accent),
      )
  }

  return style
}

func StatusStyle() lipgloss.Style {
  return lipgloss.NewStyle().
    Foreground(
      lipgloss.Color(CurrentTheme.Foreground),
    ).
    Background(
      lipgloss.Color(CurrentTheme.StatusBar),
    ).
    Padding(0, 1)
}

func BorderStyle() lipgloss.Style {
  return lipgloss.NewStyle().
    Border(lipgloss.RoundedBorder()).
    BorderForeground(
      lipgloss.Color(CurrentTheme.Border),
    ).
    Padding(1, 2)
}

func ThemeList() []string {
  return []string{
    "renaissance",
    "teletext",
    "british",
    "matrix",
    "dsb",
    "scotrail",
    "intercity-swallow",
    "irn-bru",
    "ns",
    "network-southeast",
    "viarail",
    "procomm",
  }
}

func ThemePreview() string {

  out := "Available Themes\n\n"

  for _, theme := range ThemeList() {

    t := GetTheme(theme)

    line := lipgloss.NewStyle().
      Foreground(
        lipgloss.Color(t.Foreground),
      ).
      Background(
        lipgloss.Color(t.Background),
      ).
      Padding(0, 1).
      Render(
        fmt.Sprintf(
          " %s ",
          t.Name,
        ),
      )

    out += line + "\n"
  }

  return out
}

