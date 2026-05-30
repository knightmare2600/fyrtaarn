package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/knightmare2600/fyrtaarn/internal/config"
	"github.com/knightmare2600/fyrtaarn/internal/discovery"
	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
	"github.com/knightmare2600/fyrtaarn/internal/misc"
	"github.com/knightmare2600/fyrtaarn/internal/redfish"
	"github.com/knightmare2600/fyrtaarn/internal/session"
	"github.com/knightmare2600/fyrtaarn/internal/util"
)

type screen int

const (
	screenResults screen = iota
	screenMCInfo
	screenAbout
	screenSensor
	screenSEL
	screenFRU
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

type fruMsg struct {
	Entries []ipmi.FRUEntry
	Err     error
}

type powerMsg struct {
	Action string
	Err    error
}

type eggTickMsg struct{}

type solMsg struct {
	Err error
}

type vmMsg struct {
	Action string
	Err    error
}

/* ---------------- APP ---------------- */

type App struct {
	width    int
	height   int
	contentH int

	status   string
	scanning bool

	results      []discovery.HostResult
	selectedHost int

	currentScreen screen

	menuBar      MenuBar
	activeDialog *Dialog

	username      string
	password      string
	lastSubnet    string
	lastPorts     string
	scanPerformed bool

	mcInfo  *ipmi.MCInfo
	lanInfo *ipmi.LANInfo
	chassis *ipmi.ChassisStatus

	Version   string
	Commit    string
	BuildDate string

	treeExpanded bool

	spinner     spinner.Model
	ipmiLoading bool

	sensors    []ipmi.SDREntry
	selEntries []ipmi.SELEntry
	fru        []ipmi.FRUEntry
	sdrOffset  int
	selOffset  int
	fruOffset  int

	eggOffset    int
	sessionCache *session.Cache
}

/* ---------------- INIT ---------------- */

func (a *App) Init() tea.Cmd {
	return nil
}

func NewApp() *App {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot

	cfg := config.Load()
	if cfg.Theme != "" {
		SetTheme(cfg.Theme)
	}
	lastSubnet := cfg.LastSubnet
	if lastSubnet == "" {
		lastSubnet = "192.168.0.0/24"
	}

	return &App{
		status:        "Ready — press F9 for menu",
		currentScreen: screenResults,
		menuBar:       NewMenuBar(),
		spinner:       sp,
		sessionCache:  session.NewCache(),
		lastSubnet:    lastSubnet,
		lastPorts:     cfg.LastPorts,
	}
}

/* ---------------- COMMANDS ---------------- */

func runScanCmd(subnet string, profile discovery.ScanProfile, customPorts string) tea.Cmd {
	return func() tea.Msg {
		results, err := discovery.RunScan(subnet, profile, customPorts)
		if err != nil {
			return scanFinishedMsg{Err: err}
		}
		results = discovery.EnrichResults(results)
		return scanFinishedMsg{Results: results}
	}
}

func runVMAction(host, isoURL, user, pass, action string) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch action {
		case "mount":
			err = redfish.InsertMedia(host, isoURL, user, pass)
		case "eject":
			err = redfish.EjectMedia(host, user, pass)
		}
		return vmMsg{Action: action, Err: err}
	}
}

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

func runGetFRU(host, user, pass string) tea.Cmd {
	return func() tea.Msg {
		entries, err := ipmi.GetFRU(host, user, pass)
		return fruMsg{Entries: entries, Err: err}
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

func eggTick() tea.Cmd {
	return tea.Tick(200*time.Millisecond, func(time.Time) tea.Msg {
		return eggTickMsg{}
	})
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

	case eggTickMsg:
		if misc.GlobalEgg.Active {
			a.eggOffset++
			return a, eggTick()
		}
		return a, nil

	case scanFinishedMsg:
		a.scanning = false
		a.scanPerformed = true
		if msg.Err != nil {
			a.status = msg.Err.Error()
			return a, nil
		}
		a.results = msg.Results
		a.selectedHost = 0
		a.treeExpanded = false
		a.currentScreen = screenResults
		a.status = fmt.Sprintf("Scan complete — %d hosts found", len(a.results))
		return a, nil

	case mcInfoMsg:
		a.ipmiLoading = false
		if msg.Err != nil {
			a.status = msg.Err.Error()
			// Clear bad credentials so the next Enter prompts again.
			if len(a.results) > 0 {
				a.sessionCache.Delete(a.results[a.selectedHost].IP)
			}
			a.currentScreen = screenResults
			return a, nil
		}
		// Cache successful credentials for this host.
		if len(a.results) > 0 {
			a.sessionCache.Set(a.results[a.selectedHost].IP, a.username, a.password)
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
		entries := msg.Entries
		for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
			entries[i], entries[j] = entries[j], entries[i]
		}
		a.selEntries = entries
		a.selOffset = 0
		a.currentScreen = screenSEL
		a.status = fmt.Sprintf("%d events", len(a.selEntries))
		return a, nil

	case fruMsg:
		a.ipmiLoading = false
		if msg.Err != nil {
			a.status = "FRU error: " + msg.Err.Error()
			return a, nil
		}
		a.fru = msg.Entries
		a.fruOffset = 0
		a.currentScreen = screenFRU
		a.status = fmt.Sprintf("%d FRU fields", len(a.fru))
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

	case solMsg:
		// SOL session ended — ipmitool has exited and the TUI is restored.
		if msg.Err != nil {
			a.status = "SOL ended: " + msg.Err.Error()
		} else {
			a.status = "SOL session ended"
		}
		a.currentScreen = screenMCInfo
		return a, nil

	case vmMsg:
		a.ipmiLoading = false
		if msg.Err != nil {
			a.status = "Virtual media error: " + msg.Err.Error()
		} else {
			if msg.Action == "eject" {
				a.status = "Virtual media ejected"
			} else {
				a.status = "ISO mounted via Redfish"
			}
		}
		a.currentScreen = screenMCInfo
		return a, nil

	case tea.KeyMsg:

		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}

		if a.scanning || a.ipmiLoading {
			return a, nil
		}

		r := []rune(msg.String())
		if len(r) > 0 && misc.CheckEggKey(r[0]) {
			if misc.GlobalEgg.Active {
				misc.ResetEgg()
				return a, nil
			}
			misc.TriggerEgg()
			a.eggOffset = 0
			return a, eggTick()
		}

		if msg.String() == "f2" {
			a.CycleTheme()
			return a, nil
		}

		// Menu bar takes priority: F9 or when already active.
		if msg.String() == "f9" || a.menuBar.active {
			action, consumed := a.menuBar.Update(msg.String())
			if action != "" {
				return a.handleMenuAction(action)
			}
			if consumed {
				return a, nil
			}
		}

		// Dialog routing.
		if a.activeDialog != nil {
			action, consumed, cmd := a.activeDialog.Update(msg)
			if action != "" {
				return a.handleDialogAction(action)
			}
			if consumed {
				return a, cmd
			}
		}

		// Screen-specific handlers.
		switch a.currentScreen {
		case screenResults:
			return a.updateResults(msg)
		case screenMCInfo:
			return a.updateMCInfo(msg)
		case screenSensor:
			return a.updateSensor(msg)
		case screenSEL:
			return a.updateSEL(msg)
		case screenFRU:
			return a.updateFRU(msg)
		case screenAbout:
			if msg.String() == "esc" || msg.String() == "q" {
				a.currentScreen = screenResults
			}
		}
	}

	return a, nil
}

/* ---------------- MENU ACTIONS ---------------- */

func (a *App) handleMenuAction(action string) (tea.Model, tea.Cmd) {
	switch {
	case action == "quit":
		return a, tea.Quit
	case action == "new-scan":
		a.activeDialog = NewScanDialog(a.lastSubnet, util.IsRoot(), a.lastPorts)
		return a, textinput.Blink
	case action == "about":
		a.currentScreen = screenAbout
	case strings.HasPrefix(action, "theme:"):
		name := strings.TrimPrefix(action, "theme:")
		SetTheme(name)
		a.status = "Theme: " + name
		_ = config.Save(config.Config{Theme: name, LastSubnet: a.lastSubnet})
	}
	return a, nil
}

/* ---------------- DIALOG ACTIONS ---------------- */

func (a *App) handleDialogAction(action string) (tea.Model, tea.Cmd) {
	switch action {

	case "cancel":
		a.activeDialog = nil
		return a, nil

	case "scan-quick", "scan-standard", "scan-deep", "scan-custom":
		dlg := a.activeDialog
		a.activeDialog = nil
		subnet := dlg.InputValue(0)
		if subnet == "" {
			subnet = a.lastSubnet
		}
		customPorts := dlg.InputValue(1)
		profile := discovery.ScanProfile(strings.TrimPrefix(action, "scan-"))
		a.lastSubnet = subnet
		if action == "scan-custom" && customPorts != "" {
			a.lastPorts = customPorts
		}
		_ = config.Save(config.Config{
			Theme:      CurrentTheme.Name,
			LastSubnet: subnet,
			LastPorts:  a.lastPorts,
		})
		a.scanning = true
		a.status = fmt.Sprintf("Scanning %s (%s profile)", subnet, profile)
		return a, tea.Batch(a.spinner.Tick, runScanCmd(subnet, profile, customPorts))

	case "connect":
		dlg := a.activeDialog
		a.activeDialog = nil
		a.username = dlg.InputValue(0)
		a.password = dlg.InputValue(1)
		if len(a.results) == 0 {
			a.status = "No host selected"
			return a, nil
		}
		host := a.results[a.selectedHost]
		a.status = "Enumerating " + host.IP
		a.ipmiLoading = true
		return a, tea.Batch(a.spinner.Tick, runMCInfo(host.IP, a.username, a.password))

	case "on", "off", "soft", "reset":
		a.activeDialog = nil
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = fmt.Sprintf("Sending power %s to %s", action, host)
		return a, tea.Batch(a.spinner.Tick, runPowerAction(host, a.username, a.password, action))

	case "vm-mount":
		dlg := a.activeDialog
		a.activeDialog = nil
		isoURL := dlg.InputValue(0)
		if isoURL == "" {
			a.status = "ISO URL required"
			return a, nil
		}
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost]
		if !host.HasRedfish {
			a.status = "Virtual media requires Redfish — host does not advertise it"
			return a, nil
		}
		a.ipmiLoading = true
		a.status = fmt.Sprintf("Mounting %s on %s", isoURL, host.IP)
		return a, tea.Batch(a.spinner.Tick, runVMAction(host.IP, isoURL, a.username, a.password, "mount"))

	case "vm-eject":
		a.activeDialog = nil
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost]
		if !host.HasRedfish {
			a.status = "Virtual media requires Redfish — host does not advertise it"
			return a, nil
		}
		a.ipmiLoading = true
		a.status = "Ejecting virtual media from " + host.IP
		return a, tea.Batch(a.spinner.Tick, runVMAction(host.IP, "", a.username, a.password, "eject"))
	}

	return a, nil
}

/* ---------------- RESULTS ---------------- */

func (a *App) updateResults(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {

	case "q":
		return a, tea.Quit

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
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost]
		// Use cached credentials if we have a previous successful session.
		if user, pass, ok := a.sessionCache.Get(host.IP); ok {
			a.username = user
			a.password = pass
			a.status = "Reconnecting to " + host.IP
			a.ipmiLoading = true
			return a, tea.Batch(a.spinner.Tick, runMCInfo(host.IP, user, pass))
		}
		a.activeDialog = NewLoginDialog(a.username)
		return a, textinput.Blink
	}

	return a, nil
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

	case "f":
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = "Loading FRU data from " + host
		return a, tea.Batch(a.spinner.Tick, runGetFRU(host, a.username, a.password))

	case "p":
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		currentState := "Unknown"
		if a.chassis != nil {
			if a.chassis.PowerOn {
				currentState = "On"
			} else {
				currentState = "Off"
			}
		}
		a.activeDialog = NewPowerDialog(host, currentState)
		return a, nil

	case "o":
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		cmd := ipmi.SOLCmd(host, a.username, a.password)
		return a, tea.ExecProcess(cmd, func(err error) tea.Msg {
			return solMsg{Err: err}
		})

	case "v":
		a.activeDialog = NewVirtualMediaDialog()
		return a, textinput.Blink
	}

	return a, nil
}

/* ---------------- SENSORS ---------------- */

func (a *App) updateSensor(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visibleLines := a.contentH - 6
	if visibleLines < 1 {
		visibleLines = 5
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
	visibleLines := a.contentH - 6
	if visibleLines < 1 {
		visibleLines = 5
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

/* ---------------- FRU ---------------- */

func (a *App) updateFRU(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visibleLines := a.contentH - 6
	if visibleLines < 1 {
		visibleLines = 5
	}

	maxOffset := len(a.fru) - visibleLines
	if maxOffset < 0 {
		maxOffset = 0
	}

	switch msg.String() {
	case "esc", "q":
		a.currentScreen = screenMCInfo
	case "up", "k":
		if a.fruOffset > 0 {
			a.fruOffset--
		}
	case "down", "j":
		if a.fruOffset < maxOffset {
			a.fruOffset++
		}
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
	_ = config.Save(config.Config{Theme: next, LastSubnet: a.lastSubnet})
}

/* ---------------- VIEW ---------------- */

func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "Initialising..."
	}

	topBar := a.menuBar.RenderBar(a.width)

	// Content area fills the full space between the menu bar and the status bar.
	// The dropdown is overlaid on the first N lines of the content rather than
	// inserted as extra lines — this prevents it from "eating" screen real estate.
	a.contentH = a.height - 2
	if a.contentH < 1 {
		a.contentH = 1
	}

	lineBg := lipgloss.NewStyle().
		Background(lipgloss.Color(CurrentTheme.Background)).
		Width(a.width)

	var content string
	if a.activeDialog != nil {
		// lipgloss.Place fills the full viewport with Background already.
		content = a.activeDialog.Render(a.width, a.contentH)
	} else {
		if a.scanning || a.ipmiLoading {
			content = a.renderLoading()
		} else {
			switch a.currentScreen {
			case screenResults:
				content = a.renderResults()
			case screenMCInfo:
				content = a.renderMCInfo()
			case screenSensor:
				content = a.renderSensors()
			case screenSEL:
				content = a.renderSEL()
			case screenFRU:
				content = a.renderFRU()
			case screenAbout:
				content = a.aboutView()
			default:
				content = a.status
			}
		}

		// Pad to contentH lines, then fill each line to the full terminal width
		// with the theme background so bright themes don't bleed as black.
		nLines := strings.Count(content, "\n") + 1
		if nLines < a.contentH {
			content += strings.Repeat("\n", a.contentH-nLines)
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			lines[i] = lineBg.Render(line)
		}

		// Overlay the open dropdown on the top N lines of content. The dropdown
		// already carries its own left-offset padding from RenderDropdown, so we
		// just stamp each dropdown line over the corresponding content line.
		if a.menuBar.IsOpen() {
			if dd := a.menuBar.RenderDropdown(); dd != "" {
				for i, ddLine := range strings.Split(dd, "\n") {
					if i < len(lines) {
						lines[i] = lineBg.Render(ddLine)
					}
				}
			}
		}

		content = strings.Join(lines, "\n")
	}

	right := fmt.Sprintf("[F9] Menu  Theme: %s", CurrentTheme.Name)
	var left string
	switch {
	case a.scanning:
		left = a.spinner.View() + " Scanning..."
	case a.ipmiLoading:
		left = a.spinner.View() + " Querying BMC..."
	case misc.GlobalEgg.Active:
		rw := len([]rune(right)) + 2
		maxScroll := a.width - rw
		if maxScroll < 10 {
			maxScroll = 10
		}
		left = a.eggScrollText(maxScroll)
	default:
		left = a.screenStatusHint()
	}
	bar := StatusBar(a.width, left, right)

	var out strings.Builder
	out.WriteString(topBar + "\n")
	out.WriteString(content + "\n")
	out.WriteString(bar)

	return out.String()
}

/* ---------------- RENDERING HELPERS ---------------- */

func (a *App) renderResults() string {
	var b strings.Builder

	b.WriteString(HeaderStyle().Render("DISCOVERY TREE") + "\n\n")

	if len(a.results) == 0 {
		b.WriteString("  No hosts discovered.\n\n")
		b.WriteString("  Use [F9] > File > New Scan to discover BMC hosts.")
		return b.String()
	}

	b.WriteString("  │\n")

	for i, h := range a.results {
		isLast := i == len(a.results)-1
		selected := i == a.selectedHost

		connector := "├─"
		if isLast {
			connector = "└─"
		}

		// Lighthouse glyph marks confirmed BMC hosts.
		glyph := "   "
		if h.IsBMC {
			glyph = "⛯  "
		}

		// Redfish tag appended inline so it's visible without expanding.
		rfTag := ""
		if h.HasRedfish {
			rfTag = " [RF]"
		}

		line := fmt.Sprintf("  %s %s%s%s", connector, glyph, h.IP, rfTag)

		if selected {
			b.WriteString(MenuStyle(true).Render(line) + "\n")
		} else {
			b.WriteString(line + "\n")
		}

		if selected && a.treeExpanded {
			b.WriteString(fmt.Sprintf("     ├─ vendor:     %s\n", h.Vendor))
			b.WriteString(fmt.Sprintf("     ├─ hostname:   %s\n", h.Hostname))
			b.WriteString(fmt.Sprintf("     ├─ confidence: %d\n", h.Confidence))
			if h.IPMIScript != "" {
				first := strings.SplitN(strings.TrimSpace(h.IPMIScript), "\n", 2)[0]
				b.WriteString(fmt.Sprintf("     ├─ ipmi:       %s\n", strings.TrimSpace(first)))
			}
			if h.HasRedfish {
				b.WriteString(fmt.Sprintf("     ├─ redfish:    v%s\n", h.RedfishVersion))
				if h.RedfishManufacturer != "" || h.RedfishModel != "" {
					hw := strings.TrimSpace(h.RedfishManufacturer + " " + h.RedfishModel)
					b.WriteString(fmt.Sprintf("     ├─ hardware:   %s\n", hw))
				}
			}
			_, _, cached := a.sessionCache.Get(h.IP)
			b.WriteString(fmt.Sprintf("     └─ session:    %s\n", boolCached(cached)))
		}
	}

	return b.String()
}

func (a *App) aboutView() string {
	var b strings.Builder
	b.WriteString(HeaderStyle().Render("FYRTAARN") + "\n")
	b.WriteString("Nordic Out-of-Band Management Toolkit\n\n")
	b.WriteString(fmt.Sprintf("  Version:     %s\n", a.Version))
	b.WriteString(fmt.Sprintf("  Commit:      %s\n", a.Commit))
	b.WriteString(fmt.Sprintf("  Build Date:  %s\n\n", a.BuildDate))
	b.WriteString("  Dedicated to the work of Dan Kaminsky\n")
	b.WriteString("\n  [ESC] Back")
	return b.String()
}

// eggScrollText returns a right-to-left scrolling window of the egg message
// for display in the status bar.
func (a *App) eggScrollText(maxWidth int) string {
	raw := strings.ReplaceAll(misc.GlobalEgg.Message, "\n", "  ")
	raw += "        "

	runes := []rune(raw)
	n := len(runes)
	if n == 0 || maxWidth <= 0 {
		return ""
	}

	offset := a.eggOffset % n

	var sb strings.Builder
	for i := 0; i < maxWidth; i++ {
		sb.WriteRune(runes[(offset+i)%n])
	}
	return sb.String()
}

func boolCached(v bool) string {
	if v {
		return "cached"
	}
	return "not cached"
}

// screenStatusHint returns a context-sensitive hint line for the status bar.
// Scrollable screens include the current pagination range so the user always
// knows where they are without a separate footer line in the content area.
func (a *App) screenStatusHint() string {
	if a.activeDialog != nil {
		return "[Tab] Next  [Enter] Select  [Esc] Cancel"
	}

	visibleLines := a.contentH - 6
	if visibleLines < 1 {
		visibleLines = 5
	}

	switch a.currentScreen {
	case screenResults:
		if !a.scanPerformed {
			return "No scan performed — [F9] Menu > File > New Scan"
		}
		if len(a.results) == 0 {
			return "No hosts found — try a wider subnet or deeper scan profile"
		}
		return "[↑↓/jk] Navigate  [Tab] Expand  [Enter] Connect  [F9] Menu  [Q] Quit"

	case screenMCInfo:
		return "[S] Sensors  [L] Event Log  [F] FRU  [P] Power  [O] SOL  [V] VM  [ESC] Back"

	case screenSensor:
		total := len(a.sensors)
		if total == 0 {
			return "[ESC] Back"
		}
		end := a.sdrOffset + visibleLines
		if end > total {
			end = total
		}
		return fmt.Sprintf("Showing %d–%d of %d  [↑/k] Up  [↓/j] Down  [ESC] Back",
			a.sdrOffset+1, end, total)

	case screenSEL:
		total := len(a.selEntries)
		if total == 0 {
			return "[ESC] Back"
		}
		end := a.selOffset + visibleLines
		if end > total {
			end = total
		}
		return fmt.Sprintf("Showing %d–%d of %d (newest first)  [↑/k] Up  [↓/j] Down  [ESC] Back",
			a.selOffset+1, end, total)

	case screenFRU:
		total := len(a.fru)
		if total == 0 {
			return "[ESC] Back"
		}
		end := a.fruOffset + visibleLines
		if end > total {
			end = total
		}
		return fmt.Sprintf("Showing %d–%d of %d  [↑/k] Up  [↓/j] Down  [ESC] Back",
			a.fruOffset+1, end, total)

	case screenAbout:
		return "[ESC] Back"
	}

	return a.status
}
