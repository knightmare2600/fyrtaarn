package tui

import (
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

func (d *Dialog) Render(w, h int) string {
	var b strings.Builder

	b.WriteString(HeaderStyle().Render(d.Title) + "\n\n")

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

	boxed := BorderStyle().Render(b.String())

	if w < 1 {
		w = 80
	}
	if h < 1 {
		h = 20
	}

	return lipgloss.Place(
		w, h,
		lipgloss.Center, lipgloss.Center,
		boxed,
		lipgloss.WithWhitespaceBackground(lipgloss.Color(CurrentTheme.Background)),
	)
}
