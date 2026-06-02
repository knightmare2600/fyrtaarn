package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// SubItem is a single entry in a dropdown menu.
type SubItem struct {
	Label    string
	Action   string    // empty when Sep is true or Children is non-empty
	Children []SubItem // non-empty makes this a submenu trigger
	Sep      bool      // render as a horizontal separator line
}

// TopMenu is one top-level menu title with its dropdown items.
type TopMenu struct {
	Label string
	Items []SubItem
}

// MenuBar is the horizontal menu bar at the top of the screen.
type MenuBar struct {
	menus     []TopMenu
	active    bool // keyboard focus on the menu bar
	focus     int  // which top-level menu is highlighted
	open      bool // main dropdown is visible
	itemFocus int  // focused item index in the main dropdown
	subOpen   bool // a submenu panel is visible
	subFocus  int  // focused item index in the submenu

	// Computed during RenderBar, used by RenderDropdown for x-positioning.
	offsets []int
}

// NewMenuBar builds the menu bar. Theme submenu is constructed dynamically.
func NewMenuBar() MenuBar {
	themeItems := make([]SubItem, 0, len(ThemeList()))
	for _, t := range ThemeList() {
		themeItems = append(themeItems, SubItem{Label: t, Action: "theme:" + t})
	}

	return MenuBar{
		offsets: []int{},
		menus: []TopMenu{
			{
				Label: "File",
				Items: []SubItem{
					{Label: "New Scan", Action: "new-scan"},
					{Label: "Export...", Action: "export"},
					{Sep: true},
					{Label: "Theme", Children: themeItems},
					{Sep: true},
					{Label: "Exit", Action: "quit"},
				},
			},
			{
				Label: "Help",
				Items: []SubItem{
					{Label: "About", Action: "about"},
				},
			},
		},
	}
}

// IsOpen reports whether a dropdown is currently visible.
func (m *MenuBar) IsOpen() bool {
	return m.active && m.open
}

// skipSep advances focus by dir (+1 or -1), skipping any Sep items.
func skipSep(items []SubItem, focus, dir int) int {
	n := len(items)
	if n == 0 {
		return 0
	}
	next := (focus + dir + n) % n
	for i := 0; i < n; i++ {
		if !items[next].Sep {
			return next
		}
		next = (next + dir + n) % n
	}
	return focus
}

func (m *MenuBar) currentItems() []SubItem {
	if m.focus >= len(m.menus) {
		return nil
	}
	return m.menus[m.focus].Items
}

func (m *MenuBar) currentSubItems() []SubItem {
	items := m.currentItems()
	if m.itemFocus >= len(items) {
		return nil
	}
	return items[m.itemFocus].Children
}

// Update processes a key event. Returns (action, consumed).
func (m *MenuBar) Update(key string) (action string, consumed bool) {
	if key == "f9" {
		if m.active {
			m.active = false
			m.open = false
			m.subOpen = false
		} else {
			m.active = true
			m.focus = 0
			m.open = false
		}
		return "", true
	}

	if !m.active {
		return "", false
	}

	switch key {

	case "esc":
		if m.subOpen {
			m.subOpen = false
		} else if m.open {
			m.open = false
		} else {
			m.active = false
		}
		return "", true

	case "left":
		if m.subOpen {
			m.subOpen = false
			return "", true
		}
		m.focus = (m.focus - 1 + len(m.menus)) % len(m.menus)
		if m.open {
			m.itemFocus = skipSep(m.currentItems(), len(m.currentItems())-1, 1)
		}
		return "", true

	case "right":
		if m.subOpen {
			// Already in submenu — nothing deeper.
			return "", true
		}
		if m.open {
			items := m.currentItems()
			if m.itemFocus < len(items) && len(items[m.itemFocus].Children) > 0 {
				m.subOpen = true
				m.subFocus = 0
				return "", true
			}
			// No submenu on this item — move to next top-level menu.
			m.focus = (m.focus + 1) % len(m.menus)
			m.itemFocus = skipSep(m.currentItems(), len(m.currentItems())-1, 1)
			return "", true
		}
		m.focus = (m.focus + 1) % len(m.menus)
		return "", true

	case "down":
		if !m.open {
			m.open = true
			m.subOpen = false
			m.itemFocus = skipSep(m.currentItems(), len(m.currentItems())-1, 1)
			return "", true
		}
		if m.subOpen {
			sub := m.currentSubItems()
			if len(sub) > 0 {
				m.subFocus = (m.subFocus + 1) % len(sub)
			}
			return "", true
		}
		items := m.currentItems()
		m.itemFocus = skipSep(items, m.itemFocus, 1)
		return "", true

	case "up":
		if !m.open {
			return "", true
		}
		if m.subOpen {
			sub := m.currentSubItems()
			if len(sub) > 0 {
				m.subFocus = (m.subFocus - 1 + len(sub)) % len(sub)
			}
			return "", true
		}
		items := m.currentItems()
		m.itemFocus = skipSep(items, m.itemFocus, -1)
		return "", true

	case "enter":
		if !m.open {
			m.open = true
			m.subOpen = false
			m.itemFocus = skipSep(m.currentItems(), len(m.currentItems())-1, 1)
			return "", true
		}
		if m.subOpen {
			sub := m.currentSubItems()
			if m.subFocus < len(sub) {
				act := sub[m.subFocus].Action
				m.open = false
				m.active = false
				m.subOpen = false
				return act, true
			}
			return "", true
		}
		items := m.currentItems()
		if m.itemFocus < len(items) {
			item := items[m.itemFocus]
			if item.Sep {
				return "", true
			}
			if len(item.Children) > 0 {
				m.subOpen = true
				m.subFocus = 0
				return "", true
			}
			act := item.Action
			m.open = false
			m.active = false
			return act, true
		}
		return "", true
	}

	// Any other key while active: consume without action.
	return "", true
}

// RenderBar renders the single-line menu bar and updates m.offsets.
func (m *MenuBar) RenderBar(width int) string {
	normalStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(CurrentTheme.Chrome)).
		Foreground(lipgloss.Color(CurrentTheme.ChromeFg)).
		Padding(0, 1)

	activeStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(CurrentTheme.Accent)).
		Foreground(lipgloss.Color(CurrentTheme.Highlight)).
		Bold(true).
		Padding(0, 1)

	m.offsets = make([]int, len(m.menus))
	var parts []string
	currentX := 0

	for i, menu := range m.menus {
		m.offsets[i] = currentX
		isHighlighted := m.active && i == m.focus
		var rendered string
		if isHighlighted {
			rendered = activeStyle.Render(menu.Label)
		} else {
			rendered = normalStyle.Render(menu.Label)
		}
		parts = append(parts, rendered)
		currentX += len([]rune(menu.Label)) + 2 // +2 for Padding(0,1)
	}

	bar := strings.Join(parts, "")
	barWidth := lipgloss.Width(bar)
	if barWidth < width {
		fill := normalStyle.Width(width - barWidth).Render("")
		bar += fill
	}

	return bar
}

// menuDropStyle is the compact MC-style border used for dropdowns.
func menuDropStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(CurrentTheme.Border))
}

// RenderDropdown renders the open dropdown and any open submenu.
// Returns an empty string when no dropdown is open.
func (m *MenuBar) RenderDropdown() string {
	if !m.IsOpen() || m.focus >= len(m.menus) {
		return ""
	}

	menu := m.menus[m.focus]
	if len(menu.Items) == 0 {
		return ""
	}

	// Compute the widest label (excluding separators).
	maxLen := 0
	hasSubmenus := false
	for _, item := range menu.Items {
		if item.Sep {
			continue
		}
		if l := len([]rune(item.Label)); l > maxLen {
			maxLen = l
		}
		if len(item.Children) > 0 {
			hasSubmenus = true
		}
	}

	// Build main dropdown lines.
	var mainLines []string
	for i, item := range menu.Items {
		if item.Sep {
			sepWidth := maxLen
			if hasSubmenus {
				sepWidth += 2 // align with " ►" suffix
			}
			mainLines = append(mainLines, MenuStyle(false).Render(strings.Repeat("─", sepWidth)))
			continue
		}

		focused := !m.subOpen && i == m.itemFocus

		var lbl string
		if len(item.Children) > 0 {
			lbl = fmt.Sprintf("%-*s ►", maxLen, item.Label)
		} else {
			if hasSubmenus {
				lbl = fmt.Sprintf("%-*s  ", maxLen, item.Label)
			} else {
				lbl = fmt.Sprintf("%-*s", maxLen, item.Label)
			}
		}
		mainLines = append(mainLines, MenuStyle(focused).Render(lbl))
	}

	mainBox := menuDropStyle().Render(strings.Join(mainLines, "\n"))

	// Render submenu panel to the right when open.
	if m.subOpen {
		sub := m.currentSubItems()
		if len(sub) > 0 {
			subMaxLen := 0
			for _, c := range sub {
				if l := len([]rune(c.Label)); l > subMaxLen {
					subMaxLen = l
				}
			}

			var subLines []string
			for i, c := range sub {
				check := "  "
				if strings.HasPrefix(c.Action, "theme:") &&
					strings.TrimPrefix(c.Action, "theme:") == CurrentTheme.Name {
					check = "✓ "
				}
				lbl := fmt.Sprintf("%s%-*s", check, subMaxLen, c.Label)
				subLines = append(subLines, MenuStyle(i == m.subFocus).Render(lbl))
			}

			subBox := menuDropStyle().Render(strings.Join(subLines, "\n"))
			mainBox = lipgloss.JoinHorizontal(lipgloss.Top, mainBox, subBox)
		}
	}

	// Shift the combined box to sit under the correct menu title.
	xOff := 0
	if m.focus < len(m.offsets) {
		xOff = m.offsets[m.focus]
	}
	if xOff > 0 {
		bgPad := lipgloss.NewStyle().
			Background(lipgloss.Color(CurrentTheme.Background)).
			Render(strings.Repeat(" ", xOff))
		ddLines := strings.Split(mainBox, "\n")
		for i, l := range ddLines {
			ddLines[i] = bgPad + l
		}
		mainBox = strings.Join(ddLines, "\n")
	}

	return mainBox
}
