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
//
// Colour schemes are derived from the companion PowerShell DSA-TUI project
// (github.com/knightmare2600/wintools). The PS1 uses ANSI-16 Terminal.Gui
// colour names; these are rendered here as tasteful true-colour hex
// approximations so bright-background themes look right on modern terminals.

package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Theme defines the colour palette for a visual theme.
//
// Chrome covers the menu bar and status bar.
// ChromeFg is the text colour on those bars — this must contrast with Chrome
// independently of Foreground, because light-chrome themes (Pan Am, NSE,
// Network Southeast) need dark text on their chrome while keeping light text
// in the content area.
//
// Background is the main content area and dropdown menu fill.
// Foreground is default text in the content area.
type Theme struct {
	Name string

	Background string // content area background
	Foreground string // content area text

	Chrome   string // menu bar + status bar background
	ChromeFg string // menu bar + status bar text

	Accent    string // header text colour; focused/selected element background
	Highlight string // text drawn on top of Accent background

	Warning string
	Error   string
	Border  string
}

var CurrentTheme = GetTheme("dsb")

func GetTheme(name string) Theme {

	switch strings.ToLower(name) {

	// ── British Rail ────────────────────────────────────────────────────────────
	// Red/white/blue — Union Jack. Chrome = BR red, content = Prussian blue.
	// PS1: ScriptCs White/Blue chrome, mainWindow White/Blue content, focus Red/White.
	case "british":
		return Theme{
			Name:       "british",
			Background: "#003087",
			Foreground: "#FFFFFF",
			Chrome:     "#C60C30",
			ChromeFg:   "#FFFFFF",
			Accent:     "#FFFFFF",
			Highlight:  "#003087",
			Warning:    "#FFFF00",
			Error:      "#FF4444",
			Border:     "#FFFFFF",
		}

	// ── InterCity Class 91 ──────────────────────────────────────────────────────
	// Executive white + red. White content, black chrome, blue selection.
	// PS1: ScriptCs White/Black, mainWindow BrightRed/White.
	case "class91":
		return Theme{
			Name:       "class91",
			Background: "#FFFFFF",
			Foreground: "#CC0000",
			Chrome:     "#000000",
			ChromeFg:   "#FFFFFF",
			Accent:     "#5555FF",
			Highlight:  "#FFFFFF",
			Warning:    "#FF8800",
			Error:      "#CC0000",
			Border:     "#CC0000",
		}

	// ── Dark ────────────────────────────────────────────────────────────────────
	// Classic dark-on-green. Green content, black chrome, red focus.
	// PS1: ScriptCs Gray/Black, mainWindow Gray/Green, focus Black/Red.
	case "dark":
		return Theme{
			Name:       "dark",
			Background: "#006600",
			Foreground: "#AAAAAA",
			Chrome:     "#000000",
			ChromeFg:   "#AAAAAA",
			Accent:     "#AA0000",
			Highlight:  "#000000",
			Warning:    "#FFFF00",
			Error:      "#FF4444",
			Border:     "#AAAAAA",
		}

	// ── Deutsche Bundesbahn 1980s ────────────────────────────────────────────────
	// Orientrot era: warm grey with red accents.
	// PS1: ScriptCs Black/Gray, mainWindow Black/Gray, focus White/Red.
	case "db-1980s":
		return Theme{
			Name:       "db-1980s",
			Background: "#666666",
			Foreground: "#000000",
			Chrome:     "#888888",
			ChromeFg:   "#000000",
			Accent:     "#CC0000",
			Highlight:  "#FFFFFF",
			Warning:    "#FFFF00",
			Error:      "#FF4444",
			Border:     "#CC0000",
		}

	// ── DSB (Danske Statsbaner) ──────────────────────────────────────────────────
	// DSB red throughout. Chrome = bright red, content = darker red, blue selection.
	// PS1: ScriptCs White/Red, mainWindow White/Red, focus White/Blue.
	case "dsb":
		return Theme{
			Name:       "dsb",
			Background: "#880000",
			Foreground: "#FFFFFF",
			Chrome:     "#CC0000",
			ChromeFg:   "#FFFFFF",
			Accent:     "#0033AA",
			Highlight:  "#FFFFFF",
			Warning:    "#FFD700",
			Error:      "#FF6666",
			Border:     "#FF6666",
		}

	// ── Gemstones ───────────────────────────────────────────────────────────────
	// Bold jewel tones. White chrome, bright green content, magenta selection.
	// PS1: ScriptCs Green/White, mainWindow White/BrightGreen, focus BrightYellow/BrightMagenta.
	case "gemstones":
		return Theme{
			Name:       "gemstones",
			Background: "#44BB44",
			Foreground: "#FFFFFF",
			Chrome:     "#FFFFFF",
			ChromeFg:   "#006600",
			Accent:     "#BB00BB",
			Highlight:  "#FFFF44",
			Warning:    "#FFAA00",
			Error:      "#FF4444",
			Border:     "#FFFFFF",
		}

	// ── InterCity Swallow ───────────────────────────────────────────────────────
	// BR Executive charcoal. Dark grey throughout, red accent.
	// PS1: ScriptCs White/DarkGray, mainWindow White/DarkGray, focus Black/Red.
	case "intercity-swallow":
		return Theme{
			Name:       "intercity-swallow",
			Background: "#555555",
			Foreground: "#FFFFFF",
			Chrome:     "#333333",
			ChromeFg:   "#FFFFFF",
			Accent:     "#CC0000",
			Highlight:  "#000000",
			Warning:    "#FFFF00",
			Error:      "#FF4444",
			Border:     "#CC0000",
		}

	// ── IRN-BRU ─────────────────────────────────────────────────────────────────
	// Orange and blue. True-colour orange content (PS1 used BrightRed as closest
	// ANSI to the IRN-BRU brand orange). Blue chrome, yellow selection.
	// PS1: ScriptCs White/BrightRed chrome, focus Blue/BrightYellow, mainWindow White/BrightRed.
	case "irn-bru":
		return Theme{
			Name:       "irn-bru",
			Background: "#FF6600",
			Foreground: "#FFFFFF",
			Chrome:     "#FF6600",
			ChromeFg:   "#FFFFFF",
			Accent:     "#003399",
			Highlight:  "#FFFF44",
			Warning:    "#FFFF00",
			Error:      "#CC0000",
			Border:     "#003399",
		}

	// ── Light ───────────────────────────────────────────────────────────────────
	// Cyan chrome and content, dark text, blue selection.
	// PS1: ScriptCs Black/Cyan, mainWindow Black/Cyan, focus White/Blue.
	case "light":
		return Theme{
			Name:       "light",
			Background: "#00AAAA",
			Foreground: "#000000",
			Chrome:     "#007777",
			ChromeFg:   "#000000",
			Accent:     "#0000AA",
			Highlight:  "#FFFFFF",
			Warning:    "#FF8800",
			Error:      "#CC0000",
			Border:     "#000000",
		}

	// ── Matrix ──────────────────────────────────────────────────────────────────
	// Black background throughout. Chrome = green text, content = yellow text.
	// PS1: ScriptCs Green/Black, mainWindow BrightYellow/Black, focus BrightYellow/Gray.
	case "matrix":
		return Theme{
			Name:       "matrix",
			Background: "#000000",
			Foreground: "#CCCC00",
			Chrome:     "#000000",
			ChromeFg:   "#00CC00",
			Accent:     "#555555",
			Highlight:  "#CCCC00",
			Warning:    "#FFFF00",
			Error:      "#FF0000",
			Border:     "#00CC00",
		}

	// ── NS (Nederlandse Spoorwegen) ─────────────────────────────────────────────
	// Yellow throughout, NS blue selection.
	// PS1: ScriptCs Black/BrightYellow, mainWindow Black/BrightYellow, focus White/Blue.
	case "ns":
		return Theme{
			Name:       "ns",
			Background: "#FFFF44",
			Foreground: "#000000",
			Chrome:     "#CCCC00",
			ChromeFg:   "#000000",
			Accent:     "#003082",
			Highlight:  "#FFFFFF",
			Warning:    "#FF8800",
			Error:      "#CC0000",
			Border:     "#003082",
		}

	// ── Network SouthEast ───────────────────────────────────────────────────────
	// "Toothpaste" livery — white throughout, blue/red accents.
	// PS1: ScriptCs Black/White, mainWindow Black/White, focus White/Blue & Black/Red.
	case "network-southeast":
		return Theme{
			Name:       "network-southeast",
			Background: "#FFFFFF",
			Foreground: "#000000",
			Chrome:     "#E0E0E0",
			ChromeFg:   "#000000",
			Accent:     "#0000AA",
			Highlight:  "#FFFFFF",
			Warning:    "#FF8800",
			Error:      "#CC0000",
			Border:     "#0000AA",
		}

	// ── Pan Am ──────────────────────────────────────────────────────────────────
	// White chrome, Pan Am blue content, blue/white alternating selection.
	// PS1: ScriptCs BrightBlue/White chrome, mainWindow White/BrightBlue content.
	case "pan-am":
		return Theme{
			Name:       "pan-am",
			Background: "#4466CC",
			Foreground: "#FFFFFF",
			Chrome:     "#FFFFFF",
			ChromeFg:   "#0044CC",
			Accent:     "#FFFFFF",
			Highlight:  "#0044CC",
			Warning:    "#FFD700",
			Error:      "#FF5555",
			Border:     "#FFFFFF",
		}

	// ── ProComm ─────────────────────────────────────────────────────────────────
	// Red chrome with bright yellow text — classic ProComm Plus colours.
	// Black content. PS1: ScriptCs BrightYellow/Red, mainWindow White/Black.
	case "procomm":
		return Theme{
			Name:       "procomm",
			Background: "#000000",
			Foreground: "#FFFFFF",
			Chrome:     "#AA0000",
			ChromeFg:   "#FFFF00",
			Accent:     "#5555FF",
			Highlight:  "#FFFF00",
			Warning:    "#FFFF00",
			Error:      "#FF4444",
			Border:     "#5555FF",
		}

	// ── ScotRail ────────────────────────────────────────────────────────────────
	// Dark blue chrome with Saltire yellow text; lighter Saltire blue content
	// with black text (the user asked for lighter blue content than before).
	// PS1: ScriptCs BrightYellow/Blue, mainWindow Black/BrightBlue.
	case "scotrail":
		return Theme{
			Name:       "scotrail",
			Background: "#5577CC",
			Foreground: "#000000",
			Chrome:     "#0033AA",
			ChromeFg:   "#FFFF55",
			Accent:     "#FFFF55",
			Highlight:  "#0033AA",
			Warning:    "#FFAA00",
			Error:      "#FF5555",
			Border:     "#FFFF55",
		}

	// ── Teletext ────────────────────────────────────────────────────────────────
	// BBC Ceefax / teletext: blue content, black chrome with yellow bar text.
	case "teletext":
		return Theme{
			Name:       "teletext",
			Background: "#0000AA",
			Foreground: "#FFFFFF",
			Chrome:     "#000000",
			ChromeFg:   "#FFFF00",
			Accent:     "#FFFF00",
			Highlight:  "#000000",
			Warning:    "#FFAA00",
			Error:      "#FF5555",
			Border:     "#FFFFFF",
		}

	// ── TWA (Trans World Airlines) ───────────────────────────────────────────────
	// Red throughout, dark grey selection, amber/gold accent. Not two.
	// PS1: ScriptCs White/Red, mainWindow White/Red, focus BrightYellow/DarkGray.
	case "twa":
		return Theme{
			Name:       "twa",
			Background: "#880000",
			Foreground: "#FFFFFF",
			Chrome:     "#CC0000",
			ChromeFg:   "#FFFFFF",
			Accent:     "#555555",
			Highlight:  "#FFFF44",
			Warning:    "#FFAA00",
			Error:      "#FF6666",
			Border:     "#FFAA00",
		}

	// ── VIA Rail Canada ─────────────────────────────────────────────────────────
	// Bright yellow throughout, VIA blue selection.
	// PS1: ScriptCs Black/BrightYellow, mainWindow Black/BrightYellow, focus White/Blue.
	case "viarail":
		return Theme{
			Name:       "viarail",
			Background: "#FFFF44",
			Foreground: "#000000",
			Chrome:     "#CCCC00",
			ChromeFg:   "#000000",
			Accent:     "#003399",
			Highlight:  "#FFFFFF",
			Warning:    "#FF8800",
			Error:      "#CC0000",
			Border:     "#003399",
		}

	// ── VIA Rail Soft ───────────────────────────────────────────────────────────
	// Blue throughout, yellow selection — the softer VIA livery.
	// PS1: ScriptCs White/Blue, mainWindow White/Blue, focus Blue/BrightYellow.
	case "viarail-soft":
		return Theme{
			Name:       "viarail-soft",
			Background: "#0033AA",
			Foreground: "#FFFFFF",
			Chrome:     "#001A6E",
			ChromeFg:   "#FFFFFF",
			Accent:     "#FFFF44",
			Highlight:  "#0033AA",
			Warning:    "#FF8800",
			Error:      "#FF5555",
			Border:     "#5577FF",
		}

	// ── Renaissance (default) ───────────────────────────────────────────────────
	case "renaissance":
		fallthrough

	default:
		return Theme{
			Name:       "renaissance",
			Background: "#1E1E1E",
			Foreground: "#D0D0D0",
			Chrome:     "#303030",
			ChromeFg:   "#D0D0D0",
			Accent:     "#5FD7FF",
			Highlight:  "#FFFFFF",
			Warning:    "#FFD75F",
			Error:      "#FF5F5F",
			Border:     "#5F87AF",
		}
	}
}

func SetTheme(name string) {
	CurrentTheme = GetTheme(name)
}

func HeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(CurrentTheme.Accent))
}

func MenuStyle(selected bool) lipgloss.Style {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color(CurrentTheme.Foreground)).
		Background(lipgloss.Color(CurrentTheme.Background)).
		Padding(0, 1)

	if selected {
		style = style.
			Bold(true).
			Foreground(lipgloss.Color(CurrentTheme.Highlight)).
			Background(lipgloss.Color(CurrentTheme.Accent))
	}

	return style
}

// ChromeStyle returns the style for menu bar and status bar elements.
func ChromeStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(CurrentTheme.ChromeFg)).
		Background(lipgloss.Color(CurrentTheme.Chrome))
}

func BorderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(CurrentTheme.Border)).
		Padding(1, 2)
}

func ThemeList() []string {
	return []string{
		"renaissance",
		"british",
		"class91",
		"dark",
		"db-1980s",
		"dsb",
		"gemstones",
		"intercity-swallow",
		"irn-bru",
		"light",
		"matrix",
		"network-southeast",
		"ns",
		"pan-am",
		"procomm",
		"scotrail",
		"teletext",
		"twa",
		"viarail",
		"viarail-soft",
	}
}

func ThemePreview() string {
	out := "Available Themes\n\n"

	for _, theme := range ThemeList() {
		t := GetTheme(theme)

		chromeChip := lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.ChromeFg)).
			Background(lipgloss.Color(t.Chrome)).
			Padding(0, 1).
			Render("▌ bar")

		contentChip := lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Foreground)).
			Background(lipgloss.Color(t.Background)).
			Padding(0, 1).
			Render(fmt.Sprintf(" %s ", t.Name))

		out += chromeChip + contentChip + "\n"
	}

	return out
}
