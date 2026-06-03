package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type DialogButton struct {
	Label    string
	Action   string
	Disabled bool // rendered dim, still selectable (scan degrades gracefully)
}

type Dialog struct {
	Title       string
	Body        string
	Warning     string // optional amber warning line rendered below body
	inputs      []textinput.Model
	inputLabels []string
	buttons     []DialogButton
	focus       int
}

func (d *Dialog) totalFocusable() int {
	return len(d.inputs) + len(d.buttons)
}

func (d *Dialog) InputValue(i int) string {
	if i < 0 || i >= len(d.inputs) {
		return ""
	}
	return d.inputs[i].Value()
}

// NewScanDialog creates the scan configuration dialog.
// isRoot controls whether a privilege warning is shown.
// lastPorts pre-fills the custom ports field.
func NewScanDialog(cidr string, isRoot bool, lastPorts string) *Dialog {
	subnetInp := textinput.New()
	subnetInp.Placeholder = "192.168.0.0/24"
	subnetInp.SetValue(cidr)
	subnetInp.Focus()

	portsInp := textinput.New()
	portsInp.Placeholder = "623,443,80,22"
	if lastPorts != "" {
		portsInp.SetValue(lastPorts)
	}

	warning := ""
	if !isRoot {
		warning = "No root: SYN/UDP scan and NSE fingerprinting unavailable — TCP only"
	}

	return &Dialog{
		Title:       "New Scan",
		Body:        "Quick: port 623 only (-T5)\nStandard: 623, 443, 80 (-T4)\nDeep: extended ports (-T3)\nCustom: use the port list below",
		Warning:     warning,
		inputs:      []textinput.Model{subnetInp, portsInp},
		inputLabels: []string{"Subnet", "Custom Ports"},
		buttons: []DialogButton{
			{Label: "Quick", Action: "scan-quick"},
			{Label: "Standard", Action: "scan-standard"},
			{Label: "Deep", Action: "scan-deep"},
			{Label: "Custom", Action: "scan-custom"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
}

// NewVirtualMediaDialog creates the virtual media mount dialog.
func NewVirtualMediaDialog() *Dialog {
	inp := textinput.New()
	inp.Placeholder = "http://fileserver/image.iso"
	inp.Width = 50
	inp.Focus()

	return &Dialog{
		Title:       "Virtual Media",
		Body:        "Mount or eject a virtual ISO image via Redfish.\nHost must support Redfish VirtualMedia.",
		inputs:      []textinput.Model{inp},
		inputLabels: []string{"ISO URL"},
		buttons: []DialogButton{
			{Label: "Mount", Action: "vm-mount"},
			{Label: "Eject", Action: "vm-eject"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
}

// NewExportDialog prompts for an output path and format (CSV or JSON).
func NewExportDialog(defaultPath string) *Dialog {
	inp := textinput.New()
	inp.Placeholder = "~/fyrtaarn-export.csv"
	inp.Width = 50
	if defaultPath != "" {
		inp.SetValue(defaultPath)
	}
	inp.Focus()

	return &Dialog{
		Title:       "Export Inventory",
		Body:        "Write discovered hosts to a file.\nCSV includes one row per host; JSON is an array of objects.",
		inputs:      []textinput.Model{inp},
		inputLabels: []string{"Output path"},
		buttons: []DialogButton{
			{Label: "CSV", Action: "export-csv"},
			{Label: "JSON", Action: "export-json"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
}

func NewLoginDialog(defaultUsername string) *Dialog {
	user := textinput.New()
	user.Placeholder = "admin"
	if defaultUsername != "" {
		user.SetValue(defaultUsername)
	}
	user.Focus()

	pass := textinput.New()
	pass.Placeholder = "Password"
	pass.EchoMode = textinput.EchoPassword
	pass.EchoCharacter = '•'

	return &Dialog{
		Title:       "BMC Login",
		Body:        "Enter credentials for the selected host:",
		inputs:      []textinput.Model{user, pass},
		inputLabels: []string{"Username", "Password"},
		buttons: []DialogButton{
			{Label: "Login", Action: "connect"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
}

func NewPowerDialog(host, currentState string) *Dialog {
	body := "Host: " + host
	if currentState != "" {
		body += "\nState: " + currentState
	}
	return &Dialog{
		Title: "Power Control",
		Body:  body,
		buttons: []DialogButton{
			{Label: "Power On", Action: "on"},
			{Label: "Power Off", Action: "off"},
			{Label: "Soft Shutdown", Action: "soft"},
			{Label: "Reset", Action: "reset"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
}

// NewUserActionDialog opens the per-user action picker.
// If enabled is true the Disable button is shown; otherwise Enable is shown.
func NewUserActionDialog(userID int, name string, enabled bool) *Dialog {
	toggleLabel := "Enable"
	toggleAction := "user-enable"
	if enabled {
		toggleLabel = "Disable"
		toggleAction = "user-disable"
	}
	return &Dialog{
		Title: fmt.Sprintf("User %d — %s", userID, name),
		Body:  fmt.Sprintf("Manage user account (ID %d):", userID),
		buttons: []DialogButton{
			{Label: toggleLabel, Action: toggleAction},
			{Label: "Set Password", Action: "user-setpwd"},
			{Label: "Set Name", Action: "user-setname"},
			{Label: "Set Privilege", Action: "user-setpriv"},
			{Label: "Delete", Action: "user-delete"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
}

// NewCreateUserDialog prompts for the new account details.
// Privilege is chosen via button action ("user-create-2" through "user-create-4").
func NewCreateUserDialog() *Dialog {
	nameInp := textinput.New()
	nameInp.Placeholder = "username"
	nameInp.Width = 20
	nameInp.Focus()

	pw1 := textinput.New()
	pw1.Placeholder = "Password"
	pw1.EchoMode = textinput.EchoPassword
	pw1.EchoCharacter = '•'
	pw1.Width = 30

	pw2 := textinput.New()
	pw2.Placeholder = "Confirm password"
	pw2.EchoMode = textinput.EchoPassword
	pw2.EchoCharacter = '•'
	pw2.Width = 30

	return &Dialog{
		Title: "Create User",
		Body:  "Choose privilege level via the buttons below:",
		inputs: []textinput.Model{nameInp, pw1, pw2},
		inputLabels: []string{"Username", "Password", "Confirm"},
		buttons: []DialogButton{
			{Label: "User (2)", Action: "user-create-2"},
			{Label: "Operator (3)", Action: "user-create-3"},
			{Label: "Admin (4)", Action: "user-create-4"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
}

// NewDeleteUserDialog asks for explicit confirmation before wiping a slot.
func NewDeleteUserDialog(userID int, name string) *Dialog {
	return &Dialog{
		Title:   fmt.Sprintf("Delete User %d — %s", userID, name),
		Body:    fmt.Sprintf("This will disable user %d (%s) and clear the account name,\nfreeing the slot. The action cannot be undone.", userID, name),
		Warning: "IPMI has no true delete — the slot is disabled and blanked.",
		buttons: []DialogButton{
			{Label: "Confirm Delete", Action: "user-delete-confirm"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
}

// NewSetPasswordDialog prompts for a new password (with confirmation).
func NewSetPasswordDialog() *Dialog {
	pw1 := textinput.New()
	pw1.Placeholder = "New password"
	pw1.EchoMode = textinput.EchoPassword
	pw1.EchoCharacter = '•'
	pw1.Width = 30
	pw1.Focus()

	pw2 := textinput.New()
	pw2.Placeholder = "Confirm password"
	pw2.EchoMode = textinput.EchoPassword
	pw2.EchoCharacter = '•'
	pw2.Width = 30

	return &Dialog{
		Title:       "Set Password",
		inputs:      []textinput.Model{pw1, pw2},
		inputLabels: []string{"New Password", "Confirm"},
		buttons: []DialogButton{
			{Label: "Set Password", Action: "user-setpwd-confirm"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
}

// NewSetNameDialog prompts for a new username, pre-filled with the current one.
func NewSetNameDialog(currentName string) *Dialog {
	inp := textinput.New()
	inp.Placeholder = "username"
	inp.SetValue(currentName)
	inp.Width = 20
	inp.Focus()

	return &Dialog{
		Title:       "Set Username",
		inputs:      []textinput.Model{inp},
		inputLabels: []string{"Name"},
		buttons: []DialogButton{
			{Label: "Set Name", Action: "user-setname-confirm"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
}

// NewSetPrivilegeDialog lets the operator pick a privilege level.
// IPMI levels: 2=User, 3=Operator, 4=Administrator, 5=OEM.
func NewSetPrivilegeDialog() *Dialog {
	return &Dialog{
		Title: "Set Privilege Level",
		Body:  "Select the new IPMI privilege level:",
		buttons: []DialogButton{
			{Label: "User (2)", Action: "user-priv-2"},
			{Label: "Operator (3)", Action: "user-priv-3"},
			{Label: "Administrator (4)", Action: "user-priv-4"},
			{Label: "OEM (5)", Action: "user-priv-5"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
}

func (d *Dialog) Update(msg tea.KeyMsg) (action string, consumed bool, cmd tea.Cmd) {
	n := d.totalFocusable()
	if n == 0 {
		return "", false, nil
	}

	switch msg.String() {
	case "esc":
		return "cancel", true, nil

	case "tab", "down":
		d.focus = (d.focus + 1) % n
		cmd = d.syncFocus()
		return "", true, cmd

	case "shift+tab", "up":
		d.focus = (d.focus - 1 + n) % n
		cmd = d.syncFocus()
		return "", true, cmd

	case "enter":
		if d.focus >= len(d.inputs) {
			btnIdx := d.focus - len(d.inputs)
			if btnIdx < len(d.buttons) {
				return d.buttons[btnIdx].Action, true, nil
			}
		}
		d.focus = (d.focus + 1) % n
		cmd = d.syncFocus()
		return "", true, cmd
	}

	if d.focus < len(d.inputs) {
		d.inputs[d.focus], cmd = d.inputs[d.focus].Update(msg)
		return "", true, cmd
	}

	return "", true, nil
}

func (d *Dialog) syncFocus() tea.Cmd {
	for i := range d.inputs {
		if i == d.focus {
			d.inputs[i].Focus()
		} else {
			d.inputs[i].Blur()
		}
	}
	if d.focus < len(d.inputs) {
		return textinput.Blink
	}
	return nil
}

// renderModal wraps body in a bordered modal window with title embedded in the
// top border line, a drop shadow on the right and bottom, and centers the
// result on the screen. All dialogs and the about box route through this so
// the modal chrome is consistent across the application.
func renderModal(title, body string, screenW, screenH int) string {
	boxed := BorderStyle().Render(body)

	lines := strings.Split(boxed, "\n")

	// Inject the title into the top border line (╭── Title ──╮).
	if len(lines) > 0 && title != "" {
		topW := lipgloss.Width(lines[0])
		titleStr := " " + title + " "
		titleW := lipgloss.Width(titleStr)
		dashTotal := topW - 2 - titleW // -2 for ╭ and ╮
		if dashTotal < 2 {
			dashTotal = 2
		}
		left := dashTotal / 2
		right := dashTotal - left

		borderSt := lipgloss.NewStyle().
			Foreground(lipgloss.Color(CurrentTheme.Border))
		titleSt := lipgloss.NewStyle().
			Foreground(lipgloss.Color(CurrentTheme.Accent)).Bold(true)

		lines[0] = borderSt.Render("╭"+strings.Repeat("─", left)) +
			titleSt.Render(titleStr) +
			borderSt.Render(strings.Repeat("─", right)+"╮")
	}

	// Drop shadow: single column to the right and one row below.
	shadowSt := lipgloss.NewStyle().Foreground(lipgloss.Color("236"))
	boxW := 0
	if len(lines) > 0 {
		boxW = lipgloss.Width(lines[0])
	}
	for i := 1; i < len(lines); i++ {
		lines[i] += shadowSt.Render("▓")
	}
	lines = append(lines, "  "+shadowSt.Render(strings.Repeat("▓", boxW)))

	modal := strings.Join(lines, "\n")

	if screenW < 1 {
		screenW = 80
	}
	if screenH < 1 {
		screenH = 24
	}

	return lipgloss.Place(
		screenW, screenH,
		lipgloss.Center, lipgloss.Center,
		modal,
		lipgloss.WithWhitespaceBackground(lipgloss.Color(CurrentTheme.Background)),
	)
}

func renderButton(label string, focused, disabled bool) string {
	if disabled {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Render("[ " + label + " ]")
	}
	if focused {
		return lipgloss.NewStyle().
			Background(lipgloss.Color(CurrentTheme.Accent)).
			Foreground(lipgloss.Color(CurrentTheme.Highlight)).
			Bold(true).
			Padding(0, 1).
			Render("[ " + label + " ]")
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(CurrentTheme.Foreground)).
		Render("[ " + label + " ]")
}

// NewAboutDialog returns a modal dialog showing version info and dedications.
func NewAboutDialog(version, commit, buildDate string) *Dialog {
	accent := lipgloss.NewStyle().
		Foreground(lipgloss.Color(CurrentTheme.Accent)).Bold(true)

	var b strings.Builder
	b.WriteString("Nordic Out-of-Band Management Toolkit\n\n")
	b.WriteString(fmt.Sprintf("  Version:     %s\n", version))
	b.WriteString(fmt.Sprintf("  Commit:      %s\n", commit))
	b.WriteString(fmt.Sprintf("  Build Date:  %s\n\n", buildDate))

	b.WriteString(accent.Render("  Dedications") + "\n\n")

	b.WriteString(accent.Render("  Dan Kaminsky") + "\n")
	b.WriteString("  For pioneering work on BMC/IPMI security, remote management\n")
	b.WriteString("  attack surfaces, and infrastructure visibility. His research\n")
	b.WriteString("  exposed how dangerous and under-examined out-of-band systems\n")
	b.WriteString("  could be. A small tribute remains in the code.\n\n")

	b.WriteString(accent.Render("  IppSec") + "\n")
	b.WriteString("  For educational walkthroughs and practical demonstrations.\n")
	b.WriteString("  Trying your hardest to say Rødgrød med fløde... if you\n")
	b.WriteString("  know, you know. ;)\n\n")

	b.WriteString(accent.Render("  0xDF") + "\n")
	b.WriteString("  For detailed technical writeups on post-exploitation,\n")
	b.WriteString("  beyond-root methodologies, and operational enumeration.\n")
	b.WriteString("  Jeg var at gå agurk med \"should\" på \"Release Comittee\" (:\n")

	return &Dialog{
		Title: "About Fyrtaarn",
		Body:  b.String(),
		buttons: []DialogButton{
			{Label: "Close", Action: "cancel"},
		},
		focus: 0, // no inputs — focus lands directly on the Close button
	}
}

func (d *Dialog) Render(w, h int) string {
	var b strings.Builder

	// Title is rendered in the border line by renderModal — not in the body.

	if d.Body != "" {
		for _, line := range strings.Split(d.Body, "\n") {
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}

	if d.Warning != "" {
		b.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CB4B16")).
			Bold(true).
			Render("⚠ "+d.Warning) + "\n\n")
	}

	for i := range d.inputs {
		label := ""
		if i < len(d.inputLabels) {
			label = d.inputLabels[i]
		}
		focused := d.focus == i
		ls := lipgloss.NewStyle().Foreground(lipgloss.Color(CurrentTheme.Foreground))
		if focused {
			ls = ls.Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent))
		}
		b.WriteString(ls.Render(label+":") + "\n")
		b.WriteString(d.inputs[i].View() + "\n\n")
	}

	if len(d.buttons) > 0 {
		var parts []string
		for i, btn := range d.buttons {
			parts = append(parts, renderButton(btn.Label, d.focus == len(d.inputs)+i, btn.Disabled))
		}
		b.WriteString(strings.Join(parts, " "))
		b.WriteString("\n")
	}

	b.WriteString("\n[Tab] Next  [Enter] Select  [Esc] Cancel")

	return renderModal(d.Title, b.String(), w, h)
}
