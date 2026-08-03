package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/knightmare2600/fyrtaarn/internal/config"
)

const maxFavourites = 15

// FavouritesDialog is the small pop-up modal opened from File > Favourites >
// Manage Favourites.... It lists existing favourites and supports adding,
// editing, and removing entries in place, capped at maxFavourites. Selecting
// a favourite directly from the File menu (rather than through this dialog)
// connects to it — this dialog is management-only.
type FavouritesDialog struct {
	items  []config.Favourite
	cursor int

	editing   bool // true while the add/edit sub-form is shown
	editIdx   int  // index being edited; -1 while adding a new entry
	ipInput   textinput.Model
	userInput textinput.Model
	formFocus int // 0=ip, 1=username, 2=save, 3=cancel

	notice string // transient inline warning, e.g. "Favourites full (15 max)"
}

func NewFavouritesDialog(items []config.Favourite) *FavouritesDialog {
	cp := make([]config.Favourite, len(items))
	copy(cp, items)
	return &FavouritesDialog{items: cp, editIdx: -1}
}

func (f *FavouritesDialog) startForm(idx int) {
	f.editing = true
	f.editIdx = idx
	f.notice = ""

	ip := textinput.New()
	ip.Placeholder = "192.168.1.100"
	ip.Width = 25

	user := textinput.New()
	user.Placeholder = "admin"
	user.Width = 20

	if idx >= 0 && idx < len(f.items) {
		ip.SetValue(f.items[idx].IP)
		user.SetValue(f.items[idx].Username)
	}
	ip.Focus()

	f.ipInput = ip
	f.userInput = user
	f.formFocus = 0
}

// HintText returns the status-bar hint line for the dialog's current mode.
func (f *FavouritesDialog) HintText() string {
	if f.editing {
		return "[Tab] Next  [Enter] Select  [Esc] Cancel"
	}
	return "[↑↓] Select  [Enter] Edit  [A] Add  [D] Delete  [Esc] Close"
}

// Update processes a key event. changed reports whether f.items was mutated
// (the caller should persist it and refresh the menu); closed reports
// whether the dialog should be dismissed entirely.
func (f *FavouritesDialog) Update(msg tea.KeyMsg) (changed, closed bool, cmd tea.Cmd) {
	if f.editing {
		return f.updateForm(msg)
	}
	return f.updateList(msg)
}

func (f *FavouritesDialog) updateList(msg tea.KeyMsg) (changed, closed bool, cmd tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		return false, true, nil

	case "up", "k":
		if len(f.items) > 0 {
			f.cursor = (f.cursor - 1 + len(f.items)) % len(f.items)
		}

	case "down", "j":
		if len(f.items) > 0 {
			f.cursor = (f.cursor + 1) % len(f.items)
		}

	case "a", "n":
		if len(f.items) >= maxFavourites {
			f.notice = fmt.Sprintf("Favourites full (%d max)", maxFavourites)
			return false, false, nil
		}
		f.startForm(-1)
		return false, false, textinput.Blink

	case "enter":
		if len(f.items) == 0 {
			return false, false, nil
		}
		f.startForm(f.cursor)
		return false, false, textinput.Blink

	case "d", "delete", "backspace":
		if len(f.items) == 0 {
			return false, false, nil
		}
		f.items = append(f.items[:f.cursor], f.items[f.cursor+1:]...)
		if f.cursor >= len(f.items) && f.cursor > 0 {
			f.cursor--
		}
		f.notice = ""
		return true, false, nil
	}
	return false, false, nil
}

func (f *FavouritesDialog) updateForm(msg tea.KeyMsg) (changed, closed bool, cmd tea.Cmd) {
	switch msg.String() {
	case "esc":
		f.editing = false
		return false, false, nil

	case "tab", "down":
		f.formFocus = (f.formFocus + 1) % 4
		return false, false, f.syncFormFocus()

	case "shift+tab", "up":
		f.formFocus = (f.formFocus - 1 + 4) % 4
		return false, false, f.syncFormFocus()

	case "enter":
		switch f.formFocus {
		case 2: // Save
			return f.commitForm()
		case 3: // Cancel
			f.editing = false
			return false, false, nil
		default:
			f.formFocus = (f.formFocus + 1) % 4
			return false, false, f.syncFormFocus()
		}
	}

	var c tea.Cmd
	switch f.formFocus {
	case 0:
		f.ipInput, c = f.ipInput.Update(msg)
	case 1:
		f.userInput, c = f.userInput.Update(msg)
	}
	return false, false, c
}

func (f *FavouritesDialog) syncFormFocus() tea.Cmd {
	if f.formFocus == 0 {
		f.ipInput.Focus()
		f.userInput.Blur()
		return textinput.Blink
	}
	f.ipInput.Blur()
	if f.formFocus == 1 {
		f.userInput.Focus()
		return textinput.Blink
	}
	f.userInput.Blur()
	return nil
}

func (f *FavouritesDialog) commitForm() (changed, closed bool, cmd tea.Cmd) {
	ip := strings.TrimSpace(f.ipInput.Value())
	user := strings.TrimSpace(f.userInput.Value())
	if ip == "" {
		f.notice = "IP address required"
		return false, false, nil
	}
	entry := config.Favourite{IP: ip, Username: user}
	if f.editIdx >= 0 && f.editIdx < len(f.items) {
		f.items[f.editIdx] = entry
	} else {
		f.items = append(f.items, entry)
	}
	f.editing = false
	f.notice = ""
	return true, false, nil
}

func (f *FavouritesDialog) Render(w, h int) string {
	var b strings.Builder

	if f.editing {
		title := "Edit Favourite"
		if f.editIdx < 0 {
			title = "Add Favourite"
		}

		fieldLabel := func(label string, focused bool) string {
			s := lipgloss.NewStyle().Foreground(lipgloss.Color(CurrentTheme.Foreground))
			if focused {
				s = s.Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent))
			}
			return s.Render(label + ":")
		}

		b.WriteString(fieldLabel("IP Address", f.formFocus == 0) + "\n")
		b.WriteString(f.ipInput.View() + "\n\n")
		b.WriteString(fieldLabel("Username", f.formFocus == 1) + "\n")
		b.WriteString(f.userInput.View() + "\n\n")

		if f.notice != "" {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#CB4B16")).Bold(true).
				Render("⚠ "+f.notice) + "\n\n")
		}

		b.WriteString(renderButton("Save", f.formFocus == 2, false) + " " +
			renderButton("Cancel", f.formFocus == 3, false))

		return renderModal(title, b.String(), w, h)
	}

	if len(f.items) == 0 {
		b.WriteString("No favourites yet — press [A] to add one.\n\n")
	} else {
		for i, fav := range f.items {
			line := fmt.Sprintf("%2d. %s", i+1, fav.IP)
			if fav.Username != "" {
				line += " (" + fav.Username + ")"
			}
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(CurrentTheme.Foreground))
			if i == f.cursor {
				style = lipgloss.NewStyle().
					Background(lipgloss.Color(CurrentTheme.Accent)).
					Foreground(lipgloss.Color(CurrentTheme.Highlight)).
					Bold(true)
			}
			b.WriteString(style.Render(line) + "\n")
		}
		b.WriteString("\n")
	}

	if f.notice != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#CB4B16")).Bold(true).
			Render("⚠ "+f.notice) + "\n\n")
	}

	b.WriteString(fmt.Sprintf("%d/%d favourites", len(f.items), maxFavourites))

	return renderModal("Favourites", b.String(), w, h)
}
