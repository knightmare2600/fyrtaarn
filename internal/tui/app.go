package tui

import (
	"fmt"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/knightmare2600/fyrtaarn/internal/discovery"
	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
	"github.com/knightmare2600/fyrtaarn/internal/misc"
)

type screen int

const (
	screenMenu screen = iota
	screenResults
	screenLogin
	screenCIDR
	screenMCInfo
	screenAbout
	screenSensor
	screenSEL
	screenPower
)

/* ---------------- MESSAGES ---------------- */

type scanFinishedMsg struct {
	Results []discovery.HostResult
	Err     error
}

type mcInfoMsg struct {
	Info    *ipmi.MCInfo
	LAN     *ipmi.LANInfo
	Chassis *ipmi.ChassisStatus
	Err     error
}

type sdrMsg struct {
	Entries []ipmi.SDREntry
	Err     error
}

type selMsg struct {
	Entries []ipmi.SELEntry
	Err     error
}

type powerMsg struct {
	Action string
	Err    error
}

/* ---------------- APP ---------------- */

type App struct {
	width  int
	height int

	status   string
	scanning bool

	results      []discovery.HostResult
	selectedHost int

	currentScreen screen

	menuItems []string
	selected  int

	cidrInput textinput.Model

	usernameInput textinput.Model
	passwordInput textinput.Model

	username string
	password string

	loginFocus int // 0 = user, 1 = pass

	mcInfo  *ipmi.MCInfo
	lanInfo *ipmi.LANInfo
	chassis *ipmi.ChassisStatus

	Version   string
	Commit    string
	BuildDate string

	treeExpanded bool

	spinner     spinner.Model
	ipmiLoading bool

	sensors   []ipmi.SDREntry
	selEntries []ipmi.SELEntry
	sdrOffset  int
	selOffset  int

	powerAction string // pending power command: "on", "off", "reset", "soft"
}

/* ---------------- INIT ---------------- */

func (a *App) Init() tea.Cmd {
	return nil
}

func NewApp() *App {

	sp := spinner.New()
	sp.Spinner = spinner.MiniDot

	cidr := textinput.New()
	cidr.Placeholder = "192.168.139.0/24"
	cidr.SetValue("192.168.139.0/24")

	user := textinput.New()
	user.Placeholder = "Username"

	pass := textinput.New()
	pass.Placeholder = "Password"
	pass.EchoMode = textinput.EchoPassword
	pass.EchoCharacter = '•'

	return &App{
		status:        "Ready",
		currentScreen: screenMenu,
		menuItems:     []string{"Scan", "About", "Quit"},
		spinner:       sp,
		cidrInput:     cidr,
		usernameInput: user,
		passwordInput: pass,
	}
}

/* ---------------- COMMANDS ---------------- */

func runScanCmd(subnet string) tea.Cmd {
	return func() tea.Msg {
		results, err := discovery.RunScan(subnet)
		return scanFinishedMsg{Results: results, Err: err}
	}
}

// runMCInfo fetches mc info, LAN info, and chassis status concurrently.
func runMCInfo(host, user, pass string) tea.Cmd {
	return func() tea.Msg {
		var (
			info    *ipmi.MCInfo
			lan     *ipmi.LANInfo
			chassis *ipmi.ChassisStatus
			infoErr error
		)

		var wg sync.WaitGroup
		wg.Add(3)

		go func() {
			defer wg.Done()
			info, infoErr = ipmi.GetMCInfo(host, user, pass)
		}()
		go func() {
			defer wg.Done()
			lan, _ = ipmi.GetLANInfo(host, user, pass)
		}()
		go func() {
			defer wg.Done()
			chassis, _ = ipmi.GetChassisStatus(host, user, pass)
		}()

		wg.Wait()

		if infoErr != nil {
			return mcInfoMsg{Err: infoErr}
		}

		return mcInfoMsg{Info: info, LAN: lan, Chassis: chassis}
	}
}

func runGetSDR(host, user, pass string) tea.Cmd {
	return func() tea.Msg {
		entries, err := ipmi.GetSDR(host, user, pass)
		return sdrMsg{Entries: entries, Err: err}
	}
}

func runGetSEL(host, user, pass string) tea.Cmd {
	return func() tea.Msg {
		entries, err := ipmi.GetSEL(host, user, pass)
		return selMsg{Entries: entries, Err: err}
	}
}

func runPowerAction(host, user, pass, action string) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch action {
		case "on":
			err = ipmi.PowerOn(host, user, pass)
		case "off":
			err = ipmi.PowerOff(host, user, pass)
		case "reset":
			err = ipmi.PowerReset(host, user, pass)
		case "soft":
			err = ipmi.PowerSoft(host, user, pass)
		}
		return powerMsg{Action: action, Err: err}
	}
}

/* ---------------- UPDATE ---------------- */

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		return a, nil

	case spinner.TickMsg:
		if a.scanning || a.ipmiLoading {
			var cmd tea.Cmd
			a.spinner, cmd = a.spinner.Update(msg)
			return a, cmd
		}
		return a, nil

	case scanFinishedMsg:
		a.scanning = false
		if msg.Err != nil {
			a.status = msg.Err.Error()
			return a, nil
		}
		a.results = msg.Results
		a.currentScreen = screenResults
		a.status = fmt.Sprintf("Scan complete — %d hosts found", len(a.results))
		return a, nil

	case mcInfoMsg:
		a.ipmiLoading = false
		if msg.Err != nil {
			a.status = msg.Err.Error()
			a.currentScreen = screenResults
			return a, nil
		}
		a.mcInfo = msg.Info
		a.lanInfo = msg.LAN
		a.chassis = msg.Chassis
		a.currentScreen = screenMCInfo
		a.status = "BMC enumerated"
		return a, nil

	case sdrMsg:
		a.ipmiLoading = false
		if msg.Err != nil {
			a.status = "SDR error: " + msg.Err.Error()
			return a, nil
		}
		a.sensors = msg.Entries
		a.sdrOffset = 0
		a.currentScreen = screenSensor
		a.status = fmt.Sprintf("%d sensors", len(a.sensors))
		return a, nil

	case selMsg:
		a.ipmiLoading = false
		if msg.Err != nil {
			a.status = "SEL error: " + msg.Err.Error()
			return a, nil
		}
		// Reverse so newest events appear first.
		entries := msg.Entries
		for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
			entries[i], entries[j] = entries[j], entries[i]
		}
		a.selEntries = entries
		a.selOffset = 0
		a.currentScreen = screenSEL
		a.status = fmt.Sprintf("%d events", len(a.selEntries))
		return a, nil

	case powerMsg:
		a.ipmiLoading = false
		if msg.Err != nil {
			a.status = "Power error: " + msg.Err.Error()
		} else {
			a.status = fmt.Sprintf("Power %s: command sent", msg.Action)
		}
		a.currentScreen = screenMCInfo
		return a, nil

	case tea.KeyMsg:

		// Always allow quit.
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}

		// Block all other input during async operations.
		if a.scanning || a.ipmiLoading {
			return a, nil
		}

		// Easter egg.
		r := []rune(msg.String())
		if len(r) > 0 && misc.CheckEggKey(r[0]) {
			misc.TriggerEgg()
			a.status = "Egg activated"
		}

		switch msg.String() {
		case "f2":
			a.CycleTheme()
		}

		switch a.currentScreen {
		case screenMenu:
			return a.updateMenu(msg)
		case screenCIDR:
			return a.updateCIDR(msg)
		case screenResults:
			return a.updateResults(msg)
		case screenLogin:
			return a.updateLogin(msg)
		case screenMCInfo:
			return a.updateMCInfo(msg)
		case screenSensor:
			return a.updateSensor(msg)
		case screenSEL:
			return a.updateSEL(msg)
		case screenPower:
			return a.updatePower(msg)
		case screenAbout:
			if msg.String() == "esc" || msg.String() == "q" {
				a.currentScreen = screenMenu
			}
		}
	}

	return a, nil
}

/* ---------------- MENU ---------------- */

func (a *App) updateMenu(msg tea.KeyMsg) (tea.Model, tea.Cmd) {

	switch msg.String() {

	case "q", "ctrl+c":
		return a, tea.Quit

	case "up", "k":
		if a.selected > 0 {
			a.selected--
		}

	case "down", "j":
		if a.selected < len(a.menuItems)-1 {
			a.selected++
		}

	case "enter":
		switch a.menuItems[a.selected] {
		case "Scan":
			a.currentScreen = screenCIDR
			a.cidrInput.Focus()
		case "About":
			a.currentScreen = screenAbout
		case "Quit":
			return a, tea.Quit
		}
	}

	return a, nil
}

/* ---------------- CIDR ---------------- */

func (a *App) updateCIDR(msg tea.KeyMsg) (tea.Model, tea.Cmd) {

	switch msg.String() {

	case "esc":
		a.currentScreen = screenMenu
		return a, nil

	case "enter":
		subnet := a.cidrInput.Value()
		a.scanning = true
		a.status = "Scanning " + subnet
		return a, tea.Batch(a.spinner.Tick, runScanCmd(subnet))
	}

	var cmd tea.Cmd
	a.cidrInput, cmd = a.cidrInput.Update(msg)
	return a, cmd
}

/* ---------------- RESULTS ---------------- */

func (a *App) updateResults(msg tea.KeyMsg) (tea.Model, tea.Cmd) {

	switch msg.String() {

	case "esc", "q":
		a.currentScreen = screenMenu

	case "up", "k":
		if a.selectedHost > 0 {
			a.selectedHost--
		}

	case "down", "j":
		if a.selectedHost < len(a.results)-1 {
			a.selectedHost++
		}

	case "tab":
		a.treeExpanded = !a.treeExpanded

	case "enter":
		a.currentScreen = screenLogin
		a.loginFocus = 0
		a.usernameInput.Focus()
		a.passwordInput.Blur()
		return a, textinput.Blink
	}

	return a, nil
}

/* ---------------- LOGIN ---------------- */

func (a *App) updateLogin(msg tea.KeyMsg) (tea.Model, tea.Cmd) {

	switch msg.String() {

	case "esc":
		a.currentScreen = screenResults
		a.usernameInput.Blur()
		a.passwordInput.Blur()
		return a, nil

	case "tab":
		a.loginFocus = (a.loginFocus + 1) % 2
		return a, nil

	case "enter":
		a.username = a.usernameInput.Value()
		a.password = a.passwordInput.Value()

		if len(a.results) == 0 {
			a.status = "No host selected"
			return a, nil
		}

		host := a.results[a.selectedHost]
		a.status = "Enumerating " + host.IP
		a.ipmiLoading = true
		return a, tea.Batch(a.spinner.Tick, runMCInfo(host.IP, a.username, a.password))
	}

	var cmd tea.Cmd

	if a.loginFocus == 0 {
		a.usernameInput.Focus()
		a.passwordInput.Blur()
		a.usernameInput, cmd = a.usernameInput.Update(msg)
	} else {
		a.passwordInput.Focus()
		a.usernameInput.Blur()
		a.passwordInput, cmd = a.passwordInput.Update(msg)
	}

	return a, cmd
}

/* ---------------- MC INFO ---------------- */

func (a *App) updateMCInfo(msg tea.KeyMsg) (tea.Model, tea.Cmd) {

	switch msg.String() {

	case "esc", "q":
		a.currentScreen = screenResults
		return a, nil

	case "s":
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = "Loading sensors from " + host
		return a, tea.Batch(a.spinner.Tick, runGetSDR(host, a.username, a.password))

	case "l":
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = "Loading event log from " + host
		return a, tea.Batch(a.spinner.Tick, runGetSEL(host, a.username, a.password))

	case "p":
		a.powerAction = ""
		a.currentScreen = screenPower
		return a, nil
	}

	return a, nil
}

/* ---------------- SENSORS ---------------- */

func (a *App) updateSensor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {

	visibleLines := a.height - 8
	if visibleLines < 1 {
		visibleLines = 10
	}

	maxOffset := len(a.sensors) - visibleLines
	if maxOffset < 0 {
		maxOffset = 0
	}

	switch msg.String() {
	case "esc", "q":
		a.currentScreen = screenMCInfo
	case "up", "k":
		if a.sdrOffset > 0 {
			a.sdrOffset--
		}
	case "down", "j":
		if a.sdrOffset < maxOffset {
			a.sdrOffset++
		}
	}

	return a, nil
}

/* ---------------- SEL ---------------- */

func (a *App) updateSEL(msg tea.KeyMsg) (tea.Model, tea.Cmd) {

	visibleLines := a.height - 8
	if visibleLines < 1 {
		visibleLines = 10
	}

	maxOffset := len(a.selEntries) - visibleLines
	if maxOffset < 0 {
		maxOffset = 0
	}

	switch msg.String() {
	case "esc", "q":
		a.currentScreen = screenMCInfo
	case "up", "k":
		if a.selOffset > 0 {
			a.selOffset--
		}
	case "down", "j":
		if a.selOffset < maxOffset {
			a.selOffset++
		}
	}

	return a, nil
}

/* ---------------- POWER CONTROL ---------------- */

func (a *App) updatePower(msg tea.KeyMsg) (tea.Model, tea.Cmd) {

	switch msg.String() {

	case "esc":
		if a.powerAction != "" {
			a.powerAction = ""
			return a, nil
		}
		a.currentScreen = screenMCInfo
		return a, nil
	}

	if a.powerAction == "" {
		// Selecting an action.
		switch msg.String() {
		case "o", "O":
			a.powerAction = "on"
		case "f", "F":
			a.powerAction = "off"
		case "s", "S":
			a.powerAction = "soft"
		case "r", "R":
			a.powerAction = "reset"
		}
		return a, nil
	}

	// Confirming an action.
	switch msg.String() {
	case "y", "Y":
		if len(a.results) == 0 {
			a.powerAction = ""
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		action := a.powerAction
		a.powerAction = ""
		a.ipmiLoading = true
		a.status = fmt.Sprintf("Sending power %s to %s", action, host)
		return a, tea.Batch(a.spinner.Tick, runPowerAction(host, a.username, a.password, action))
	case "n", "N":
		a.powerAction = ""
	}

	return a, nil
}

/* ---------------- THEME ---------------- */

func (a *App) CycleTheme() {
	themes := ThemeList()
	if len(themes) == 0 {
		return
	}

	currentIdx := 0
	for i, t := range themes {
		if t == CurrentTheme.Name {
			currentIdx = i
			break
		}
	}

	next := themes[(currentIdx+1)%len(themes)]
	SetTheme(next)
	a.status = fmt.Sprintf("Theme: %s", next)
}

/* ---------------- VIEW ---------------- */

func (a *App) View() string {
	var content string

	if a.ipmiLoading {
		content = a.renderLoading()
	} else {
		switch a.currentScreen {
		case screenMenu:
			content = a.renderMenu()
		case screenCIDR:
			content = a.renderCIDRScreen()
		case screenResults:
			content = a.renderResults()
		case screenLogin:
			content = renderLoginModal(a)
		case screenMCInfo:
			content = a.renderMCInfo()
		case screenSensor:
			content = a.renderSensors()
		case screenSEL:
			content = a.renderSEL()
		case screenPower:
			content = a.renderPower()
		case screenAbout:
			content = a.aboutView()
		default:
			content = a.status
		}
	}

	return a.applyLayout(content)
}

/* ---------------- RENDERING HELPERS ---------------- */

func (a *App) renderMenu() string {
	title := HeaderStyle().Render("FYRTAARN")

	var itemLines []string
	for i, item := range a.menuItems {
		itemLines = append(itemLines, MenuStyle(i == a.selected).Render("  "+item+"  "))
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		title,
		"BMC/IPMI Management Toolkit",
		"",
		lipgloss.JoinVertical(lipgloss.Left, itemLines...),
		"",
		"[↑↓] Navigate  [Enter] Select  [Q] Quit  [F2] Theme",
	)

	return BorderStyle().Render(content)
}

func (a *App) renderCIDRScreen() string {
	var b strings.Builder
	b.WriteString(HeaderStyle().Render("SUBNET SCAN") + "\n\n")
	b.WriteString("Enter CIDR subnet to scan:\n\n")
	b.WriteString(a.cidrInput.View())
	b.WriteString("\n\n[Enter] Start Scan  [Esc] Cancel")
	return b.String()
}

func (a *App) renderResults() string {
	var b strings.Builder

	b.WriteString(HeaderStyle().Render("DISCOVERY TREE") + "\n\n")

	if len(a.results) == 0 {
		b.WriteString("No hosts discovered.\n")
		return b.String()
	}

	for i, h := range a.results {

		isLast := i == len(a.results)-1
		selected := i == a.selectedHost

		connector := "├─"
		if isLast {
			connector = "└─"
		}

		line := fmt.Sprintf("  %s %s (score=%d)", connector, h.IP, h.Confidence)

		if selected {
			b.WriteString(MenuStyle(true).Render(line) + "\n")
		} else {
			b.WriteString(line + "\n")
		}

		if selected && a.treeExpanded {
			b.WriteString(fmt.Sprintf("     ├─ vendor: %s\n", h.Vendor))
			b.WriteString(fmt.Sprintf("     ├─ hostname: %s\n", h.Hostname))
			b.WriteString(fmt.Sprintf("     └─ bmc candidate: %t\n", h.IsBMC))
		}
	}

	b.WriteString("\n[TAB] Expand/Collapse  [ENTER] Login  [ESC] Back")
	return b.String()
}

func renderLoginModal(a *App) string {
	var b strings.Builder

	b.WriteString("BMC LOGIN\n\n")

	if a.loginFocus == 0 {
		b.WriteString("▶ ")
	} else {
		b.WriteString("  ")
	}
	b.WriteString("Username:\n")
	b.WriteString(a.usernameInput.View())
	b.WriteString("\n\n")

	if a.loginFocus == 1 {
		b.WriteString("▶ ")
	} else {
		b.WriteString("  ")
	}
	b.WriteString("Password:\n")
	b.WriteString(a.passwordInput.View())

	b.WriteString("\n\n[Tab] Switch  [Enter] Login  [Esc] Cancel")

	return modal(80, 20, b.String())
}

func (a *App) aboutView() string {
	var b strings.Builder
	b.WriteString("FYRTAARN\n")
	b.WriteString("Nordic Out-of-Band Management Toolkit\n\n")
	b.WriteString(fmt.Sprintf("  Version:     %s\n", a.Version))
	b.WriteString(fmt.Sprintf("  Commit:      %s\n", a.Commit))
	b.WriteString(fmt.Sprintf("  Build Date:  %s\n\n", a.BuildDate))
	b.WriteString("  Dedicated to the work of Dan Kaminsky\n\n")
	b.WriteString("  [ESC] Back")
	return b.String()
}

// applyLayout pads content to a consistent terminal height and appends the
// status bar, preventing the flickering caused by variable-height frames.
func (a *App) applyLayout(content string) string {
	left := a.status
	if a.scanning {
		left = a.spinner.View() + " Scanning..."
	} else if a.ipmiLoading {
		left = a.spinner.View() + " Querying BMC..."
	}

	if a.width == 0 {
		return content
	}

	right := fmt.Sprintf("[F2] %s", CurrentTheme.Name)
	bar := StatusBar(a.width, left, right)

	// Pad to fill the terminal so the frame height never changes.
	lines := strings.Count(content, "\n")
	targetLines := a.height - 1
	if targetLines > lines {
		content += strings.Repeat("\n", targetLines-lines)
	}

	return content + bar
}
