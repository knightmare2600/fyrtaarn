package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/knightmare2600/fyrtaarn/internal/webproxy"
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

	// Body scrolling, for a body taller than the screen: the first body
	// line shown, and how many body lines the last Render fitted (0 when
	// the whole body fitted).
	bodyOffset int
	bodyRows   int
}

// scrollBody moves the body window by delta lines; Render clamps it.
func (d *Dialog) scrollBody(delta int) bool {
	if d.bodyRows == 0 {
		return false
	}
	d.bodyOffset = max(d.bodyOffset+delta, 0)
	return true
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

// NewVirtualMediaDialog creates the virtual media mount dialog. Hosts with
// Redfish mount through it; hosts without get AMI MegaRAC SP network media
// redirection, plus an Upload to BMC (LMedia) button.
func NewVirtualMediaDialog(redfish bool) *Dialog {
	inp := textinput.New()
	inp.Placeholder = "http://fileserver/image.iso  or  ~/isos/image.iso"
	inp.Width = 50
	inp.Focus()

	body := "Mount or eject a virtual image via Redfish.\n" +
		".iso goes in the CD/DVD slot; .img, .ima and .vfd\n" +
		"(disk and floppy images) in the USB/floppy slot.\n" +
		"Enter an HTTP(S) URL, or a local file path — a local\n" +
		"path is served to the BMC automatically from this\n" +
		"machine for as long as the image stays mounted.\n" +
		"A URL is fetched by the BMC itself, so it must be\n" +
		"reachable from the BMC's network, not just from here.\n" +
		"Eject removes whatever is mounted."
	buttons := []DialogButton{
		{Label: "Mount", Action: "vm-mount"},
		{Label: "Browse...", Action: "vm-browse"},
		{Label: "Eject", Action: "vm-eject"},
		{Label: "Cancel", Action: "cancel"},
	}
	if !redfish {
		inp.Placeholder = "~/isos/image.iso"
		body = "This BMC has no Redfish. Mount attaches a local file\n" +
			"through AMI MegaRAC media redirection (what the Java\n" +
			"console does): .iso as a CD, .img/.ima/.vfd as a\n" +
			"floppy (up to 2.88 MB) or a USB disk. The image\n" +
			"is read from this machine on demand, any size, and\n" +
			"stays attached until Eject or fyrtaarn exits.\n\n" +
			"Upload to BMC stores a small image on the BMC itself\n" +
			"instead (LMedia, 16 MiB at most)."
		buttons = []DialogButton{
			{Label: "Mount", Action: "vm-mount"},
			{Label: "Browse...", Action: "vm-browse"},
			{Label: "Eject", Action: "vm-eject"},
			{Label: "Upload to BMC", Action: "vm-upload"},
			{Label: "Cancel", Action: "cancel"},
		}
	}
	return &Dialog{
		Title:       "Virtual Media",
		Body:        body,
		inputs:      []textinput.Model{inp},
		inputLabels: []string{"ISO URL / Local Path"},
		buttons:     buttons,
		focus:       0,
	}
}

// NewLMediaWarningDialog confirms an LMedia upload, which changes BMC state
// and has a hard size limit.
// knownToFail comes from internal/quirks (LMediaRegisterFails).
func NewLMediaWarningDialog(path string, knownToFail bool) *Dialog {
	warning := "Untested on this BMC"
	if knownToFail {
		warning = "Known to fail on this BMC's firmware (see [K] Known issues)"
	}
	return &Dialog{
		Title: "Upload to BMC",
		Body: "Store " + filepath.Base(path) + " on the BMC itself (LMedia)?\n\n" +
			"  • 16 MiB at most: floppy images and small ISOs only.\n" +
			"  • Switches the BMC's Local Media setting on if it is\n" +
			"    off, which closes any open virtual media session.\n" +
			"  • The BMC answers nothing else while it receives\n" +
			"    the file.\n" +
			"  • Replaces any image already in that slot.\n\n" +
			"Mount (network redirection) needs none of this and\n" +
			"has no size limit.",
		Warning: warning,
		buttons: []DialogButton{
			{Label: "Upload", Action: "vm-upload-confirm"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 1, // default to the harmless choice
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

// NewPowerDialog builds the [P] Power Control dialog. currentState is the
// live chassis state ("On", "Off", or "Unknown") — the button matching the
// current state is dimmed so the dialog never presents an already-current
// state as an available action.
func NewPowerDialog(host, currentState string) *Dialog {
	body := "Host: " + host
	if currentState != "" {
		body += "\nState: " + currentState
	}
	return &Dialog{
		Title: "Power Control",
		Body:  body,
		buttons: []DialogButton{
			{Label: "Power On", Action: "on", Disabled: currentState == "On"},
			{Label: "Power Off", Action: "off", Disabled: currentState == "Off"},
			{Label: "Soft Shutdown", Action: "soft", Disabled: currentState == "Off"},
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
		Title:       "Create User",
		Body:        "Choose privilege level via the buttons below:",
		inputs:      []textinput.Model{nameInp, pw1, pw2},
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

	// A body that does not fit scrolls with PgUp/PgDn, Home/End, and the
	// arrows when there is nothing else to move between.
	single := n == 1
	switch msg.String() {
	case "pgdown":
		d.scrollBody(max(d.bodyRows-1, 1))
		return "", true, nil
	case "pgup":
		d.scrollBody(-max(d.bodyRows-1, 1))
		return "", true, nil
	case "home":
		if d.focus >= len(d.inputs) && d.scrollBody(-d.bodyOffset) {
			return "", true, nil
		}
	case "end":
		if d.focus >= len(d.inputs) && d.scrollBody(1<<20) {
			return "", true, nil
		}
	case "down", "j":
		if single && d.scrollBody(1) {
			return "", true, nil
		}
	case "up", "k":
		if single && d.scrollBody(-1) {
			return "", true, nil
		}
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
		return "", true, d.UpdateInput(msg)
	}

	return "", true, nil
}

// UpdateInput passes msg to the focused input box, if any: key presses, and
// the messages an input's own commands produce later (Ctrl+V's clipboard
// text, cursor blinks). Inputs ignore messages that are not theirs.
func (d *Dialog) UpdateInput(msg tea.Msg) tea.Cmd {
	if d.focus >= len(d.inputs) {
		return nil
	}
	var cmd tea.Cmd
	d.inputs[d.focus], cmd = d.inputs[d.focus].Update(msg)
	return cmd
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
	titleStr := " " + title + " "
	titleW := lipgloss.Width(titleStr)

	// Widen the box if the title (plus a corner and at least one dash each
	// side) would not fit in its top border.
	st := BorderStyle()
	boxed := st.Render(body)
	if minW := titleW + 4; title != "" && lipgloss.Width(boxed) < minW {
		boxed = st.Width(minW - 2).Render(body) // Width excludes the border
	}

	lines := strings.Split(boxed, "\n")

	// Inject the title into the top border line (╭── Title ──╮).
	if len(lines) > 0 && title != "" {
		topW := lipgloss.Width(lines[0])
		dashTotal := topW - 2 - titleW // -2 for ╭ and ╮
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

	// Drop shadow: single column to the right and one row below, offset by
	// one. Every line is padded to the same width (boxW+1): lipgloss.Place
	// centres each line on its own width, so a shorter top line would be
	// pushed one column right of the box sides.
	shadowSt := lipgloss.NewStyle().Foreground(lipgloss.Color("236"))
	padSt := lipgloss.NewStyle().Background(lipgloss.Color(CurrentTheme.Background))
	boxW := 0
	if len(lines) > 0 {
		boxW = lipgloss.Width(lines[0])
		lines[0] += padSt.Render(" ")
	}
	for i := 1; i < len(lines); i++ {
		lines[i] += shadowSt.Render("▓")
	}
	lines = append(lines, padSt.Render(" ")+shadowSt.Render(strings.Repeat("▓", boxW)))

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

// NewSOLLaunchDialog is shown when the user presses O (SOL) after the live
// chassis check completes. When powerOn is false the dialog offers to power
// the chassis on before connecting; when powerOn is true it offers a direct
// SOL connection with a confirmation step so the user sees the actual state.
func NewSOLLaunchDialog(host string, powerOn bool) *Dialog {
	if powerOn {
		return &Dialog{
			Title: "Chassis is Powered On",
			Body: fmt.Sprintf(
				"The chassis on %s is powered on.\n\n"+
					"Open the SOL console now?", host),
			buttons: []DialogButton{
				{Label: "Open SOL", Action: "open-sol"},
				{Label: "Cancel", Action: "cancel"},
			},
			focus: 0,
		}
	}
	return &Dialog{
		Title: "Chassis is Powered Off",
		Body: fmt.Sprintf(
			"The chassis on %s is currently off.\n\n"+
				"Power it on and open the SOL console now?\n"+
				"The console connects immediately so you can\n"+
				"watch the full boot sequence from BIOS.", host),
		buttons: []DialogButton{
			{Label: "Power On + Open SOL", Action: "power-on-sol"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
}

// NewSOLUnsupportedDialog explains that the built-in SOL pane is not
// available on this platform and gives the equivalent ipmitool command to
// run in a terminal instead (password via -E, never on the command line).
func NewSOLUnsupportedDialog(host, user string) *Dialog {
	return &Dialog{
		Title: "SOL Console Not Available",
		Body: "The built-in SOL console needs a pseudo-terminal, which\n" +
			"this Windows build does not support yet (it needs ConPTY).\n\n" +
			"Run SOL in a separate terminal instead. Set the password\n" +
			"(Command Prompt or PowerShell form), then start SOL:\n\n" +
			"  set IPMITOOL_PASSWORD=<password>\n" +
			"  $env:IPMITOOL_PASSWORD='<password>'\n\n" +
			fmt.Sprintf("  ipmitool -I lanplus -H %s -U %s -E sol activate\n\n", host, user) +
			"Leave the console with ~. at the start of a line.",
		buttons: []DialogButton{
			{Label: "Close", Action: "cancel"},
		},
		focus: 0,
	}
}

// NewWebUIDialog shows where a BMC's web interface is being served locally.
func NewWebUIDialog(p *webproxy.Proxy) *Dialog {
	warning := ""
	if p.TLS.Legacy() {
		warning = "Old TLS to the BMC: not safe from an attacker on that network"
	}
	return &Dialog{
		Title: "Web UI",
		Body: fmt.Sprintf("The web interface of %s is being served at:\n\n"+
			"  %s\n\n"+
			"Open it in a browser on this machine. It runs until\n"+
			"stopped here or fyrtaarn exits.\n\n"+
			"Connection to the BMC:\n"+
			"  %s\n\n"+
			"Running fyrtaarn over SSH? Forward the port first:\n"+
			"  ssh -L %d:127.0.0.1:%d <this machine>",
			p.Host, p.URL, p.TLS, p.Port, p.Port),
		Warning: warning,
		buttons: []DialogButton{
			{Label: "Close", Action: "cancel"},
			{Label: "Stop Proxy", Action: "webui-stop"},
		},
		focus: 0,
	}
}

// NewSOLTakeoverDialog asks before ending another session's SOL console.
// The BMC cannot say whether that session is live or left over from a crash.
func NewSOLTakeoverDialog(host string) *Dialog {
	return &Dialog{
		Title: "SOL Console In Use",
		Body: fmt.Sprintf(
			"Another session already holds the SOL console on %s.\n\n"+
				"It may be a colleague's live console, or one left over\n"+
				"from a closed terminal or crashed client. Taking over\n"+
				"disconnects it.", host),
		buttons: []DialogButton{
			{Label: "Take Over", Action: "sol-takeover"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 1, // default to the harmless choice
	}
}

// NewConnectBMCDialog prompts for a BMC IP and credentials to connect directly,
// bypassing the discovery scan. defaultIP and defaultUsername pre-fill their
// fields when non-empty (used by Recent Connections / Favourites); focus
// starts on the password field when both are already known.
func NewConnectBMCDialog(defaultIP, defaultUsername string) *Dialog {
	ipInp := textinput.New()
	ipInp.Placeholder = "192.168.1.100"
	ipInp.Width = 25
	if defaultIP != "" {
		ipInp.SetValue(defaultIP)
	}

	userInp := textinput.New()
	userInp.Placeholder = "admin"
	userInp.Width = 20
	if defaultUsername != "" {
		userInp.SetValue(defaultUsername)
	}

	passInp := textinput.New()
	passInp.Placeholder = "password"
	passInp.EchoMode = textinput.EchoPassword
	passInp.EchoCharacter = '•'
	passInp.Width = 20

	focus := 0
	if defaultIP != "" && defaultUsername != "" {
		focus = 2
		passInp.Focus()
	} else {
		ipInp.Focus()
	}

	dlg := &Dialog{
		Title:       "Connect to BMC",
		Body:        "Connect directly to a known BMC — no scan required.",
		inputs:      []textinput.Model{ipInp, userInp, passInp},
		inputLabels: []string{"IP Address", "Username", "Password"},
		buttons: []DialogButton{
			{Label: "Connect", Action: "connect-bmc"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: focus,
	}
	return dlg
}

// NewLogDialog prompts for a file path to write the session log to.
func NewLogDialog() *Dialog {
	inp := textinput.New()
	inp.Placeholder = "~/fyrtaarn-session.log"
	inp.Width = 50
	inp.Focus()

	return &Dialog{
		Title:       "Start Session Log",
		Body:        "Log all activity to a plain-text file.\nSOL output is captured as rendered screen text.",
		inputs:      []textinput.Model{inp},
		inputLabels: []string{"Log file path"},
		buttons: []DialogButton{
			{Label: "Start Logging", Action: "log-start"},
			{Label: "Cancel", Action: "cancel"},
		},
		focus: 0,
	}
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
	// The body goes in last, once the rest is laid out, so that a body
	// taller than the screen can be cut to a window that fits.
	var rest strings.Builder

	if d.Warning != "" {
		rest.WriteString(lipgloss.NewStyle().
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
		rest.WriteString(ls.Render(label+":") + "\n")
		rest.WriteString(d.inputs[i].View() + "\n\n")
	}

	if len(d.buttons) > 0 {
		// Wrap onto further rows rather than run past the screen edge (a
		// BMC can offer six or more power actions). The modal's border,
		// padding and shadow take about 12 columns.
		limit := max(w-12, 30)
		row, rowW := "", 0
		for i, btn := range d.buttons {
			part := renderButton(btn.Label, d.focus == len(d.inputs)+i, btn.Disabled)
			pw := lipgloss.Width(part)
			if rowW > 0 && rowW+1+pw > limit {
				rest.WriteString(row + "\n")
				row, rowW = "", 0
			}
			if rowW > 0 {
				row += " "
				rowW++
			}
			row += part
			rowW += pw
		}
		rest.WriteString(row + "\n")
	}

	hints := "\n[Tab] Next  [Enter] Select  [Esc] Cancel"

	if d.Body != "" {
		lines := strings.Split(d.Body, "\n")
		// The modal's border, padding and shadow take 5 rows; the rest,
		// the hint line and the blank after the body take their own.
		avail := h - 5 - strings.Count(rest.String(), "\n") - strings.Count(hints, "\n") - 2
		d.bodyRows = 0
		if h > 0 && len(lines) > avail && avail >= 3 {
			rows := avail - 1 // one row for the position line
			d.bodyOffset = min(d.bodyOffset, len(lines)-rows)
			d.bodyRows = rows
			pos := fmt.Sprintf("── lines %d–%d of %d  [PgUp/PgDn] scroll ──", d.bodyOffset+1, d.bodyOffset+rows, len(lines))
			lines = append(lines[d.bodyOffset:d.bodyOffset+rows:d.bodyOffset+rows],
				lipgloss.NewStyle().Foreground(lipgloss.Color(CurrentTheme.Accent)).Render(pos))
			hints = "\n[PgUp/PgDn] Scroll  [Tab] Next  [Enter] Select  [Esc] Cancel"
		}
		for _, line := range lines {
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(rest.String())
	b.WriteString(hints)

	return renderModal(d.Title, b.String(), w, h)
}
