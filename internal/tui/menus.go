package tui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/lipgloss"
)

// SubItem is a single entry in a dropdown menu.
type SubItem struct {
	Label    string
	Accel    rune      // underlined accelerator letter; 0 = none
	Action   string    // empty when Sep is true or Children is non-empty
	Children []SubItem // non-empty makes this a submenu trigger
	Sep      bool      // render as a horizontal separator line
}

// TopMenu is one top-level menu title with its dropdown items.
type TopMenu struct {
	Label string
	Accel rune // underlined accelerator letter; 0 = none
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

	// Set by RenderDropdown; used by HandleMouse to hit-test the panels.
	dropWidth int // screen-cell width of the main dropdown panel
	subXOff   int // screen x where the submenu panel begins
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
				Accel: 'F',
				Items: []SubItem{
					{Label: "New Scan", Accel: 'N', Action: "new-scan"},
					{Label: "Export...", Accel: 'E', Action: "export"},
					{Sep: true},
					{Label: "Theme", Accel: 'T', Children: themeItems},
					{Sep: true},
					{Label: "Exit", Accel: 'X', Action: "quit"},
				},
			},
			{
				Label: "Help",
				Accel: 'H',
				Items: []SubItem{
					{Label: "About", Accel: 'A', Action: "about"},
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

// renderLabel renders label using s, with the accelerator character underlined.
// Falls back to s.Render(label) when accel is 0 or not present in label.
func renderLabel(label string, accel rune, s lipgloss.Style) string {
	if accel == 0 {
		return s.Render(label)
	}
	runes := []rune(label)
	target := unicode.ToLower(accel)
	for i, r := range runes {
		if unicode.ToLower(r) == target {
			// Strip padding for per-fragment renders — each s.Render() call would
			// otherwise add its own left/right padding, creating visible gaps.
			// Re-apply the original padding once around the combined result.
			bare := s.Copy().UnsetPadding()
			ul := bare.Copy().Underline(true)
			content := bare.Render(string(runes[:i])) +
				ul.Render(string(runes[i:i+1])) +
				bare.Render(string(runes[i+1:]))
			return lipgloss.NewStyle().
				Background(s.GetBackground()).
				Padding(s.GetPaddingTop(), s.GetPaddingRight(),
					s.GetPaddingBottom(), s.GetPaddingLeft()).
				Render(content)
		}
	}
	return s.Render(label)
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

	// Accelerator key: single character selects a menu title or item.
	if len(key) == 1 {
		r := unicode.ToLower([]rune(key)[0])
		if !m.open {
			// Top-level: jump to matching menu and open its dropdown.
			for i, menu := range m.menus {
				if menu.Accel != 0 && unicode.ToLower(menu.Accel) == r {
					m.focus = i
					m.open = true
					m.subOpen = false
					m.itemFocus = skipSep(m.currentItems(), -1, 1)
					return "", true
				}
			}
		} else if !m.subOpen {
			// Dropdown open: activate matching item.
			items := m.currentItems()
			for i, item := range items {
				if item.Sep || item.Accel == 0 {
					continue
				}
				if unicode.ToLower(item.Accel) == r {
					if len(item.Children) > 0 {
						m.itemFocus = i
						m.subOpen = true
						m.subFocus = 0
					} else {
						act := item.Action
						m.open = false
						m.active = false
						return act, true
					}
					return "", true
				}
			}
		} else {
			// Submenu open: activate matching child.
			sub := m.currentSubItems()
			for i, c := range sub {
				if c.Accel != 0 && unicode.ToLower(c.Accel) == r {
					act := c.Action
					m.open = false
					m.active = false
					m.subOpen = false
					m.subFocus = i
					return act, true
				}
			}
		}
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
			rendered = renderLabel(menu.Label, menu.Accel, activeStyle)
		} else {
			rendered = renderLabel(menu.Label, menu.Accel, normalStyle)
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
		mainLines = append(mainLines, renderLabel(lbl, item.Accel, MenuStyle(focused)))
	}

	mainBox := menuDropStyle().Render(strings.Join(mainLines, "\n"))

	// Record the main panel width before any submenu is joined (used by HandleMouse).
	if firstLine := strings.SplitN(mainBox, "\n", 2); len(firstLine) > 0 {
		m.dropWidth = lipgloss.Width(firstLine[0])
	}
	xOff := 0
	if m.focus < len(m.offsets) {
		xOff = m.offsets[m.focus]
	}
	m.subXOff = xOff + m.dropWidth

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
				subLines = append(subLines, renderLabel(lbl, c.Accel, MenuStyle(i == m.subFocus)))
			}

			subBox := menuDropStyle().Render(strings.Join(subLines, "\n"))
			mainBox = lipgloss.JoinHorizontal(lipgloss.Top, mainBox, subBox)
		}
	}

	// Shift the combined box to sit under the correct menu title.
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

// HandleMouse processes a left-click. Returns (action, consumed) like Update.
// y=0 is the menu bar row; y>=1 is the content area where dropdowns are overlaid.
func (m *MenuBar) HandleMouse(x, y int) (action string, consumed bool) {
	if y == 0 {
		// Hit-test each menu title in the bar.
		for i, menu := range m.menus {
			right := m.offsets[i] + len([]rune(menu.Label)) + 2 // +2 for Padding(0,1)
			if x >= m.offsets[i] && x < right {
				if m.active && m.focus == i && m.open {
					// Second click on the same open title closes it.
					m.active = false
					m.open = false
					m.subOpen = false
				} else {
					m.active = true
					m.focus = i
					m.open = true
					m.subOpen = false
					m.itemFocus = skipSep(m.currentItems(), -1, 1)
				}
				return "", true
			}
		}
		// Clicked on the blank part of the bar.
		if m.active {
			m.active = false
			m.open = false
			m.subOpen = false
			return "", true
		}
		return "", false
	}

	if !m.IsOpen() {
		return "", false
	}

	// Dropdown starts at screen y=1 (top border), items at y=2.
	itemLineY := y - 2
	dropX := 0
	if m.focus < len(m.offsets) {
		dropX = m.offsets[m.focus]
	}

	if m.subOpen && x >= m.subXOff {
		// Click inside the submenu panel.
		sub := m.currentSubItems()
		if itemLineY >= 0 && itemLineY < len(sub) {
			act := sub[itemLineY].Action
			m.open = false
			m.active = false
			m.subOpen = false
			return act, true
		}
		return "", true
	}

	if x >= dropX && x < dropX+m.dropWidth {
		// Click inside the main dropdown panel.
		items := m.currentItems()
		if itemLineY >= 0 && itemLineY < len(items) {
			item := items[itemLineY]
			if item.Sep {
				return "", true
			}
			m.itemFocus = itemLineY
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

	// Clicked outside all panels: dismiss.
	m.active = false
	m.open = false
	m.subOpen = false
	return "", true
}
