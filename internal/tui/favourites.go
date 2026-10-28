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
//
// Passwords are session-only: never part of config.Favourite, never written
// to disk. A password entered here is handed back to the App as a favCred
// (see TakeCreds), which puts it in the in-memory session cache — the same
// cache connectToHost already checks, so the favourite then connects
// without prompting for the rest of the run.
type FavouritesDialog struct {
	items  []config.Favourite
	cursor int

	editing   bool // true while the add/edit sub-form is shown
	editIdx   int  // index being edited; -1 while adding a new entry
	nameInput textinput.Model
	ipInput   textinput.Model
	userInput textinput.Model
	passInput textinput.Model
	formFocus int // see favFocus* constants

	// cachedPass looks up the session-cached password for an IP, so the edit
	// form can pre-fill it. May be nil.
	cachedPass func(ip string) (string, bool)
	hadPass    bool      // the form's password box was pre-filled from the cache
	creds      []favCred // pending session-cache updates, drained by TakeCreds

	notice string // transient inline warning, e.g. "Favourites full (15 max)"
}

// favCred is a session-cache update produced by saving the add/edit form.
// An empty pass means a pre-filled password box was cleared: drop the
// cached entry. A box left empty that was never pre-filled queues nothing,
// so adding a favourite cannot wipe a session the user is logged in with.
type favCred struct {
	ip, user, pass string
}

// Add/edit form focus order.
const (
	favFocusName = iota
	favFocusIP
	favFocusUser
	favFocusPass
	favFocusSave
	favFocusCancel
	favFocusCount
)

// favouriteLabel is how a favourite is shown in both the File > Favourites
// menu and the management dialog: "Name — IP (user)", omitting the parts
// that are empty.
func favouriteLabel(fav config.Favourite) string {
	label := fav.IP
	if fav.Name != "" {
		label = fav.Name + " — " + fav.IP
	}
	if fav.Username != "" {
		label += " (" + fav.Username + ")"
	}
	return label
}

func NewFavouritesDialog(items []config.Favourite, cachedPass func(ip string) (string, bool)) *FavouritesDialog {
	cp := make([]config.Favourite, len(items))
	copy(cp, items)
	return &FavouritesDialog{items: cp, editIdx: -1, cachedPass: cachedPass}
}

// TakeCreds returns and clears the session-cache updates queued by saving
// the add/edit form.
func (f *FavouritesDialog) TakeCreds() []favCred {
	c := f.creds
	f.creds = nil
	return c
}

func (f *FavouritesDialog) startForm(idx int) {
	f.editing = true
	f.editIdx = idx
	f.notice = ""

	name := textinput.New()
	name.Placeholder = "web-01 iLO"
	name.Width = 25
	name.CharLimit = 32

	ip := textinput.New()
	ip.Placeholder = "192.168.1.100"
	ip.Width = 25

	user := textinput.New()
	user.Placeholder = "admin"
	user.Width = 25

	pass := textinput.New()
	pass.Placeholder = "optional, this session only"
	pass.Width = 25
	pass.EchoMode = textinput.EchoPassword
	pass.EchoCharacter = '•'

	f.hadPass = false
	if idx >= 0 && idx < len(f.items) {
		name.SetValue(f.items[idx].Name)
		ip.SetValue(f.items[idx].IP)
		user.SetValue(f.items[idx].Username)
		if f.cachedPass != nil {
			if pw, ok := f.cachedPass(f.items[idx].IP); ok {
				pass.SetValue(pw)
				f.hadPass = true
			}
		}
	}
	name.Focus()

	f.nameInput = name
	f.ipInput = ip
	f.userInput = user
	f.passInput = pass
	f.formFocus = favFocusName
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
		f.formFocus = (f.formFocus + 1) % favFocusCount
		return false, false, f.syncFormFocus()

	case "shift+tab", "up":
		f.formFocus = (f.formFocus - 1 + favFocusCount) % favFocusCount
		return false, false, f.syncFormFocus()

	case "enter":
		switch f.formFocus {
		case favFocusSave:
			return f.commitForm()
		case favFocusCancel:
			f.editing = false
			return false, false, nil
		default:
			f.formFocus = (f.formFocus + 1) % favFocusCount
			return false, false, f.syncFormFocus()
		}
	}

	var c tea.Cmd
	switch f.formFocus {
	case favFocusName:
		f.nameInput, c = f.nameInput.Update(msg)
	case favFocusIP:
		f.ipInput, c = f.ipInput.Update(msg)
	case favFocusUser:
		f.userInput, c = f.userInput.Update(msg)
	case favFocusPass:
		f.passInput, c = f.passInput.Update(msg)
	}
	return false, false, c
}

func (f *FavouritesDialog) syncFormFocus() tea.Cmd {
	f.nameInput.Blur()
	f.ipInput.Blur()
	f.userInput.Blur()
	f.passInput.Blur()
	switch f.formFocus {
	case favFocusName:
		f.nameInput.Focus()
	case favFocusIP:
		f.ipInput.Focus()
	case favFocusUser:
		f.userInput.Focus()
	case favFocusPass:
		f.passInput.Focus()
	default:
		return nil
	}
	return textinput.Blink
}

func (f *FavouritesDialog) commitForm() (changed, closed bool, cmd tea.Cmd) {
	name := strings.TrimSpace(f.nameInput.Value())
	ip := strings.TrimSpace(f.ipInput.Value())
	user := strings.TrimSpace(f.userInput.Value())
	if ip == "" {
		f.notice = "IP address required"
		return false, false, nil
	}
	entry := config.Favourite{Name: name, IP: ip, Username: user}
	if pw := f.passInput.Value(); pw != "" || f.hadPass {
		f.creds = append(f.creds, favCred{ip: ip, user: user, pass: pw})
	}
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

		// Label and input share a row so all four fields fit on a 24-row
		// terminal; the modal grows wider instead of taller.
		field := func(label string, focus int, inp textinput.Model) string {
			s := lipgloss.NewStyle().Foreground(lipgloss.Color(CurrentTheme.Foreground))
			if f.formFocus == focus {
				s = s.Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent))
			}
			return s.Render(fmt.Sprintf("%-12s", label+":")) + inp.View() + "\n"
		}

		b.WriteString(field("Name", favFocusName, f.nameInput))
		b.WriteString(field("IP Address", favFocusIP, f.ipInput))
		b.WriteString(field("Username", favFocusUser, f.userInput))
		b.WriteString(field("Password", favFocusPass, f.passInput))
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).
			Render("Password is kept for this session only, never saved.") + "\n\n")

		if f.notice != "" {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#CB4B16")).Bold(true).
				Render("⚠ "+f.notice) + "\n\n")
		}

		b.WriteString(renderButton("Save", f.formFocus == favFocusSave, false) + " " +
			renderButton("Cancel", f.formFocus == favFocusCancel, false))

		return renderModal(title, b.String(), w, h)
	}

	if len(f.items) == 0 {
		b.WriteString("No favourites yet — press [A] to add one.\n\n")
	} else {
		for i, fav := range f.items {
			line := fmt.Sprintf("%2d. %s", i+1, favouriteLabel(fav))
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
