package tui

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/knightmare2600/fyrtaarn/internal/advisory"
	"github.com/knightmare2600/fyrtaarn/internal/config"
	"github.com/knightmare2600/fyrtaarn/internal/discovery"
	"github.com/knightmare2600/fyrtaarn/internal/export"
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
	screenUsers
	screenFirmware
	screenRedfish
	screenSOL
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

type solChassisCheckMsg struct {
	host    string
	powerOn bool // true = chassis on, go straight to SOL; false = show power-on dialog
}

type scanProgressMsg struct {
	Text      string
	HostFound bool    // a </host> was seen in the XML stream
	Percent   float64 // parsed from --stats-every; 0 = not a progress update
}

type eggTickMsg struct{}

type vmMsg struct {
	Action string
	Err    error
}

type usersMsg struct {
	Entries []ipmi.UserEntry
	Err     error
}

type firmwareMsg struct {
	Result *ipmi.ComplianceResult
	Err    error
}

type redfishEnumMsg struct {
	Result *redfish.FullEnumeration
	Err    error
}

type userActionMsg struct {
	Action string
	Err    error
}

type advisoryMsg struct {
	Findings        []advisory.CVEFinding
	VersionSpecific bool // true when results are filtered to the exact firmware version
	Err             error
}

type exportMsg struct {
	Path   string
	Format string
	Count  int
	Err    error
}

/* ---------------- APP ---------------- */

type App struct {
	width    int
	height   int
	contentH int

	status   string
	scanning bool

	results        []discovery.HostResult
	selectedHost   int
	resultsOffset  int
	lastExportPath string

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

	loadProgress Progress
	hostsFound   int

	users         []ipmi.UserEntry
	usersOffset   int
	selectedUser  int
	pendingUserID int

	firmwareResult   *ipmi.ComplianceResult
	firmwareAdvisory []advisory.CVEFinding
	firmwareOffset   int
	advisoryLoading  bool
	nvdAPIKey        string

	redfishEnum   *redfish.FullEnumeration
	redfishOffset int

	solPane       *solPane
	solGeneration int  // incremented each session; guards stale solDoneMsg
	solAfterPower bool // when true, powerMsg "on" chains into SOL connect

	sessionLog *os.File // nil when logging is off

	eggOffset      int
	sessionCache   *session.Cache
	detailsCache   map[string]*ipmi.HostDetails
	scanProgressCh chan scanProgressMsg
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
		detailsCache:  make(map[string]*ipmi.HostDetails),
		lastSubnet:    lastSubnet,
		lastPorts:     cfg.LastPorts,
		nvdAPIKey:     cfg.NVDAPIKey,
	}
}

/* ---------------- COMMANDS ---------------- */

func runScanCmd(subnet string, profile discovery.ScanProfile, customPorts string, prog chan<- scanProgressMsg) tea.Cmd {
	return func() tea.Msg {
		events := make(chan discovery.StreamEvent, 32)
		go func() {
			for ev := range events {
				if prog == nil {
					continue
				}
				msg := scanProgressMsg{}
				switch ev.Type {
				case "host":
					msg.HostFound = true
				case "progress":
					pct, _ := strconv.ParseFloat(ev.Data, 64)
					msg.Percent = pct
				default:
					msg.Text = ev.Data
				}
				prog <- msg
			}
		}()
		_, results, err := discovery.RunScanStream(subnet, profile, customPorts, events)
		if err != nil {
			return scanFinishedMsg{Err: err}
		}
		results = discovery.EnrichResults(results)
		return scanFinishedMsg{Results: results}
	}
}

// listenScanProgress drains the progress channel and returns one msg per tick.
func listenScanProgress(ch <-chan scanProgressMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
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

func runGetUsers(host, user, pass string) tea.Cmd {
	return func() tea.Msg {
		entries, err := ipmi.GetUsers(host, user, pass)
		return usersMsg{Entries: entries, Err: err}
	}
}

func runUserEnable(host, adminUser, adminPass string, userID int) tea.Cmd {
	return func() tea.Msg {
		err := ipmi.EnableUser(host, adminUser, adminPass, userID)
		return userActionMsg{Action: "enable", Err: err}
	}
}

func runUserDisable(host, adminUser, adminPass string, userID int) tea.Cmd {
	return func() tea.Msg {
		err := ipmi.DisableUser(host, adminUser, adminPass, userID)
		return userActionMsg{Action: "disable", Err: err}
	}
}

func runUserSetPassword(host, adminUser, adminPass string, userID int, newPass string) tea.Cmd {
	return func() tea.Msg {
		err := ipmi.SetUserPassword(host, adminUser, adminPass, userID, newPass)
		return userActionMsg{Action: "set-password", Err: err}
	}
}

func runUserSetName(host, adminUser, adminPass string, userID int, name string) tea.Cmd {
	return func() tea.Msg {
		err := ipmi.SetUserName(host, adminUser, adminPass, userID, name)
		return userActionMsg{Action: "set-name", Err: err}
	}
}

func runUserSetPrivilege(host, adminUser, adminPass string, userID, level int) tea.Cmd {
	return func() tea.Msg {
		err := ipmi.SetUserPrivilege(host, adminUser, adminPass, userID, 1, level)
		return userActionMsg{Action: "set-privilege", Err: err}
	}
}

func runUserCreate(host, adminUser, adminPass string, userID int, name, password string, privilege int) tea.Cmd {
	return func() tea.Msg {
		err := ipmi.CreateUser(host, adminUser, adminPass, userID, name, password, privilege)
		return userActionMsg{Action: "create", Err: err}
	}
}

func runUserDelete(host, adminUser, adminPass string, userID int) tea.Cmd {
	return func() tea.Msg {
		err := ipmi.DeleteUser(host, adminUser, adminPass, userID)
		return userActionMsg{Action: "delete", Err: err}
	}
}

func runFirmwareCheck(host, user, pass string) tea.Cmd {
	return func() tea.Msg {
		result, err := ipmi.CheckFirmwareCompliance(host, user, pass)
		return firmwareMsg{Result: result, Err: err}
	}
}

func runAdvisoryCheck(manufacturer, productName, firmwareVersion, apiKey string) tea.Cmd {
	return func() tea.Msg {
		findings, versionSpecific, err := advisory.Check(manufacturer, productName, firmwareVersion, apiKey)
		return advisoryMsg{Findings: findings, VersionSpecific: versionSpecific, Err: err}
	}
}

func runExportCmd(path, format string, results []discovery.HostResult, details map[string]*ipmi.HostDetails) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch format {
		case "csv":
			err = export.WriteCSV(path, results, details)
		case "json":
			err = export.WriteJSON(path, results, details)
		}
		return exportMsg{Path: path, Format: format, Count: len(results), Err: err}
	}
}

func checkChassisForSOL(host, user, pass string) tea.Cmd {
	return func() tea.Msg {
		status, err := ipmi.GetChassisStatus(host, user, pass)
		if err != nil || status == nil || !status.PowerStateFound {
			// Can't determine state — assume on so the dialog shows "Powered On".
			return solChassisCheckMsg{host: host, powerOn: true}
		}
		return solChassisCheckMsg{host: host, powerOn: status.PowerOn}
	}
}

func runRedfishEnum(host, user, pass string) tea.Cmd {
	return func() tea.Msg {
		result, err := redfish.EnumerateFull(host, user, pass)
		return redfishEnumMsg{Result: result, Err: err}
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
		if a.solPane != nil {
			// SOL is always capped at 80 cols — BIOS/GRUB format for that width
			// and IPMI SOL has no mechanism to tell the remote about wider terminals.
			a.solPane.resize(min(msg.Width-2, solDefaultCols), msg.Height-6)
		}
		return a, nil

	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			action, _ := a.menuBar.HandleMouse(msg.X, msg.Y)
			if action != "" {
				return a.handleMenuAction(action)
			}
		}
		return a, nil

	case spinner.TickMsg:
		if a.scanning || a.ipmiLoading || a.advisoryLoading {
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

	case scanProgressMsg:
		switch {
		case msg.HostFound:
			a.hostsFound++
		case msg.Percent > 0 && a.loadProgress.Total > 0:
			a.loadProgress.Current = int(msg.Percent / 100 * float64(a.loadProgress.Total))
		case msg.Text != "":
			a.status = msg.Text
		}
		return a, listenScanProgress(a.scanProgressCh)

	case scanFinishedMsg:
		a.scanning = false
		a.scanPerformed = true
		a.scanProgressCh = nil
		if msg.Err != nil {
			a.status = msg.Err.Error()
			return a, nil
		}
		a.results = msg.Results
		a.selectedHost = -1
		a.resultsOffset = 0
		a.treeExpanded = false
		a.currentScreen = screenResults
		a.status = fmt.Sprintf("Scan complete — %d hosts found", len(a.results))
		a.logf("Scan of %s complete — %d host(s) found", a.lastSubnet, len(a.results))
		return a, nil

	case mcInfoMsg:
		a.ipmiLoading = false
		if msg.Err != nil {
			a.status = msg.Err.Error()
			// Clear bad credentials so the next Enter prompts again.
			if len(a.results) > 0 && a.selectedHost >= 0 {
				a.sessionCache.Delete(a.results[a.selectedHost].IP)
			}
			a.currentScreen = screenResults
			return a, nil
		}
		// Cache successful credentials and BMC details for this host.
		if len(a.results) > 0 && a.selectedHost >= 0 {
			hostIP := a.results[a.selectedHost].IP
			a.sessionCache.Set(hostIP, a.username, a.password)
			a.detailsCache[hostIP] = &ipmi.HostDetails{MCInfo: msg.Info, LAN: msg.LAN, Chassis: msg.Chassis}
		}
		a.mcInfo = msg.Info
		a.lanInfo = msg.LAN
		a.chassis = msg.Chassis
		a.currentScreen = screenMCInfo
		a.status = "BMC enumerated"
		if len(a.results) > 0 && a.selectedHost >= 0 {
			a.logf("Connected to %s — %s %s (FW %s)",
				a.results[a.selectedHost].IP,
				msg.Info.ManufacturerName, msg.Info.ProductName,
				msg.Info.FirmwareRevision)
		}
		return a, nil

	case sdrMsg:
		a.ipmiLoading = false
		a.loadProgress = Progress{}
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
		a.loadProgress = Progress{}
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
		a.loadProgress = Progress{}
		if msg.Err != nil {
			a.status = "FRU error: " + msg.Err.Error()
			return a, nil
		}
		a.fru = msg.Entries
		a.fruOffset = 0
		a.currentScreen = screenFRU
		a.status = fmt.Sprintf("%d FRU fields", len(a.fru))
		return a, nil

	case usersMsg:
		a.ipmiLoading = false
		a.loadProgress = Progress{}
		if msg.Err != nil {
			a.status = "User list error: " + msg.Err.Error()
			return a, nil
		}
		a.users = msg.Entries
		a.usersOffset = 0
		a.selectedUser = 0
		a.currentScreen = screenUsers
		a.status = fmt.Sprintf("%d users", len(a.users))

	case userActionMsg:
		a.ipmiLoading = false
		if msg.Err != nil {
			a.status = fmt.Sprintf("User %s error: %s", msg.Action, msg.Err.Error())
			a.currentScreen = screenUsers
			return a, nil
		}
		a.status = fmt.Sprintf("User %s: done", msg.Action)
		// Re-fetch the user list so the display reflects the change.
		if len(a.results) > 0 {
			host := a.results[a.selectedHost].IP
			a.ipmiLoading = true
			return a, tea.Batch(a.spinner.Tick, runGetUsers(host, a.username, a.password))
		}
		a.currentScreen = screenUsers
		return a, nil

	case firmwareMsg:
		a.ipmiLoading = false
		a.loadProgress = Progress{}
		if msg.Err != nil {
			a.status = "Firmware check error: " + msg.Err.Error()
			return a, nil
		}
		a.firmwareResult = msg.Result
		a.firmwareAdvisory = nil
		a.firmwareOffset = 0
		a.currentScreen = screenFirmware
		if msg.Result.Compliant {
			a.status = "Firmware: compliant — checking advisory feed..."
		} else {
			a.status = fmt.Sprintf("Firmware: %d heuristic issue(s) — checking advisory feed...", len(msg.Result.Issues))
		}
		a.advisoryLoading = true
		return a, tea.Batch(a.spinner.Tick, runAdvisoryCheck(
			msg.Result.Info.ManufacturerName,
			msg.Result.Info.ProductName,
			msg.Result.Info.FirmwareRevision,
			a.nvdAPIKey,
		))

	case advisoryMsg:
		a.advisoryLoading = false
		if msg.Err != nil {
			// Non-fatal: surface as a note in the status bar.
			a.status = "Advisory: " + msg.Err.Error()
			return a, nil
		}
		a.firmwareAdvisory = msg.Findings
		heurIssues := 0
		if a.firmwareResult != nil {
			heurIssues = len(a.firmwareResult.Issues)
		}
		kev := 0
		for _, f := range msg.Findings {
			if f.ActivelyExploited {
				kev++
			}
		}
		scope := "family-wide"
		if msg.VersionSpecific {
			scope = "version-specific"
		}
		if len(msg.Findings) == 0 && heurIssues == 0 {
			a.status = fmt.Sprintf("Firmware: compliant — no CVEs found (%s)", scope)
		} else if kev > 0 {
			a.status = fmt.Sprintf("Firmware: %d CVE(s) (%d actively exploited), %d heuristic issue(s) [%s]",
				len(msg.Findings), kev, heurIssues, scope)
		} else {
			a.status = fmt.Sprintf("Firmware: %d CVE(s), %d heuristic issue(s) [%s]",
				len(msg.Findings), heurIssues, scope)
		}
		return a, nil

	case redfishEnumMsg:
		a.ipmiLoading = false
		a.loadProgress = Progress{}
		if msg.Err != nil {
			a.status = "Redfish error: " + msg.Err.Error()
			return a, nil
		}
		a.redfishEnum = msg.Result
		a.redfishOffset = 0
		a.currentScreen = screenRedfish
		a.status = fmt.Sprintf("Redfish: %d system(s), %d manager(s)",
			len(msg.Result.Systems), len(msg.Result.Managers))
		return a, nil

	case solChassisCheckMsg:
		a.ipmiLoading = false
		// Always show the dialog so the user sees the actual reported power state.
		// This guards against parseChassisStatus returning a zero-value struct
		// (PowerOn=false) when the BMC uses an unexpected key format.
		a.activeDialog = NewSOLLaunchDialog(msg.host, msg.powerOn)
		return a, nil

	case powerMsg:
		a.ipmiLoading = false
		if msg.Err != nil {
			a.solAfterPower = false
			a.status = "Power error: " + msg.Err.Error()
			a.currentScreen = screenMCInfo
			return a, nil
		}
		// If the user chose "Power On + Open SOL", chain straight into SOL so
		// they catch the full boot sequence from BIOS onwards.
		if msg.Action == "on" && a.solAfterPower {
			a.solAfterPower = false
			host := a.results[a.selectedHost].IP
			a.ipmiLoading = true
			a.loadProgress = Progress{}
			a.status = "Power on sent — opening SOL console on " + host + "..."
			cols := solDefaultCols
			rows := max(a.contentH-4, 24)
			return a, tea.Batch(a.spinner.Tick, startSOLPane(host, a.username, a.password, cols, rows))
		}
		a.status = fmt.Sprintf("Power %s: command sent", msg.Action)
		a.currentScreen = screenMCInfo
		return a, nil

	case solPaneReadyMsg:
		a.ipmiLoading = false
		a.solPane = msg.pane
		a.solGeneration++ // new session; stale solDoneMsg from previous session is ignored
		a.currentScreen = screenSOL
		a.status = "SOL active — [F10] disconnect  [PgUp/PgDn] scroll"
		hostIP := ""
		if len(a.results) > 0 && a.selectedHost >= 0 {
			hostIP = a.results[a.selectedHost].IP
		}
		a.logf("SOL session started — %s", hostIP)
		return a, listenSOL(msg.pane.ptmx, a.solGeneration)

	case solReadMsg:
		if msg.Err != nil {
			a.ipmiLoading = false
			a.status = "SOL error: " + msg.Err.Error()
			a.currentScreen = screenMCInfo
			return a, nil
		}
		if a.solPane == nil {
			return a, nil // pane was closed — discard stale read
		}
		if len(msg.Data) > 0 {
			prevHistory := len(a.solPane.history)
			a.solPane.ingest(msg.Data)
			// Write any newly-scrolled-off lines to the session log.
			if a.sessionLog != nil {
				for i := prevHistory; i < len(a.solPane.history); i++ {
					fmt.Fprintln(a.sessionLog, a.solPane.history[i])
				}
			}
		}
		return a, listenSOL(a.solPane.ptmx, a.solGeneration)

	case solDoneMsg:
		if msg.gen != a.solGeneration {
			return a, nil // stale — from a previous session whose pty was closed
		}
		if a.solPane != nil {
			a.flushSOLLog()
			a.solPane.close()
			a.solPane = nil
		}
		a.currentScreen = screenMCInfo
		a.status = "SOL session ended"
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

	case exportMsg:
		if msg.Err != nil {
			a.status = "Export failed: " + msg.Err.Error()
		} else {
			a.status = fmt.Sprintf("Exported %d hosts (%s) → %s", msg.Count, strings.ToUpper(msg.Format), msg.Path)
		}
		return a, nil

	case tea.KeyMsg:

		if msg.String() == "ctrl+c" && a.currentScreen != screenSOL {
			return a, a.quit()
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

		// F2 and F9 are intercepted for the TUI chrome — but not in SOL mode
		// where they must be forwarded to the remote console.
		if msg.String() == "f2" && a.currentScreen != screenSOL {
			a.CycleTheme()
			return a, nil
		}

		// Menu bar takes priority: F9 or when already active.
		// Skip in SOL mode so F9 reaches the BMC as \x1b[20~.
		if a.currentScreen != screenSOL && (msg.String() == "f9" || a.menuBar.active) {
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
		case screenUsers:
			return a.updateUsers(msg)
		case screenFirmware:
			return a.updateFirmware(msg)
		case screenRedfish:
			return a.updateRedfishEnum(msg)
		case screenSOL:
			return a.updateSOL(msg)
		case screenAbout:
			// About is now an activeDialog — this case is unreachable but kept
			// as a safety fallback.
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
		return a, a.quit()
	case action == "connect-bmc-dialog":
		a.activeDialog = NewConnectBMCDialog(a.username)
		return a, textinput.Blink
	case action == "log-start-dialog":
		a.activeDialog = NewLogDialog()
		return a, textinput.Blink
	case action == "log-stop":
		if a.sessionLog != nil {
			a.logf("Session log stopped.")
			_ = a.sessionLog.Close()
			a.sessionLog = nil
		}
		a.status = "Session logging stopped"
	case action == "new-scan":
		a.activeDialog = NewScanDialog(a.lastSubnet, util.IsRoot(), a.lastPorts)
		return a, textinput.Blink
	case action == "export":
		if len(a.results) == 0 {
			a.status = "Nothing to export — run a scan first"
			return a, nil
		}
		a.activeDialog = NewExportDialog(a.lastExportPath)
		return a, textinput.Blink
	case action == "about":
		a.activeDialog = NewAboutDialog(a.Version, a.Commit, a.BuildDate)
	case strings.HasPrefix(action, "theme:"):
		name := strings.TrimPrefix(action, "theme:")
		SetTheme(name)
		a.status = "Theme: " + name
		_ = config.Save(config.Config{Theme: name, LastSubnet: a.lastSubnet, NVDAPIKey: a.nvdAPIKey})
	}
	return a, nil
}

/* ---------------- DIALOG ACTIONS ---------------- */

func (a *App) handleDialogAction(action string) (tea.Model, tea.Cmd) {
	switch action {

	case "cancel":
		a.activeDialog = nil
		return a, nil

	case "export-csv", "export-json":
		dlg := a.activeDialog
		a.activeDialog = nil
		path := strings.TrimSpace(dlg.InputValue(0))
		if path == "" {
			a.status = "Export path cannot be empty"
			return a, nil
		}
		// Expand leading ~
		if strings.HasPrefix(path, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				path = home + path[1:]
			}
		}
		format := strings.TrimPrefix(action, "export-")
		a.lastExportPath = path
		a.status = fmt.Sprintf("Exporting %d hosts to %s...", len(a.results), path)
		return a, runExportCmd(path, format, a.results, a.detailsCache)

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
			NVDAPIKey:  a.nvdAPIKey,
		})
		a.scanning = true
		a.hostsFound = 0
		a.loadProgress = Progress{Total: discovery.CIDRHostCount(subnet)}
		a.scanProgressCh = make(chan scanProgressMsg, 32)
		a.status = fmt.Sprintf("Scanning %s (%s profile)", subnet, profile)
		return a, tea.Batch(
			a.spinner.Tick,
			runScanCmd(subnet, profile, customPorts, a.scanProgressCh),
			listenScanProgress(a.scanProgressCh),
		)

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

	case "open-sol":
		a.activeDialog = nil
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.loadProgress = Progress{}
		a.status = "Starting SOL session with " + host
		cols := solDefaultCols
		rows := max(a.contentH-4, 24)
		return a, tea.Batch(a.spinner.Tick, startSOLPane(host, a.username, a.password, cols, rows))

	case "power-on-sol":
		a.activeDialog = nil
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.solAfterPower = true
		a.status = "Powering on " + host + "..."
		a.logf("Power on + SOL — %s", host)
		return a, tea.Batch(a.spinner.Tick, runPowerAction(host, a.username, a.password, "on"))

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

	case "user-enable":
		a.activeDialog = nil
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = fmt.Sprintf("Enabling user %d on %s", a.pendingUserID, host)
		return a, tea.Batch(a.spinner.Tick, runUserEnable(host, a.username, a.password, a.pendingUserID))

	case "user-disable":
		a.activeDialog = nil
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = fmt.Sprintf("Disabling user %d on %s", a.pendingUserID, host)
		return a, tea.Batch(a.spinner.Tick, runUserDisable(host, a.username, a.password, a.pendingUserID))

	case "user-setpwd":
		// Open the password sub-dialog; pendingUserID is already set.
		a.activeDialog = NewSetPasswordDialog()
		return a, textinput.Blink

	case "user-setname":
		// Find the current name for pre-fill.
		currentName := ""
		for _, u := range a.users {
			if u.ID == a.pendingUserID {
				currentName = u.Name
				break
			}
		}
		a.activeDialog = NewSetNameDialog(currentName)
		return a, textinput.Blink

	case "user-setpriv":
		a.activeDialog = NewSetPrivilegeDialog()
		return a, nil

	case "user-setpwd-confirm":
		dlg := a.activeDialog
		a.activeDialog = nil
		pw1 := dlg.InputValue(0)
		pw2 := dlg.InputValue(1)
		if pw1 == "" {
			a.status = "Password cannot be empty"
			a.currentScreen = screenUsers
			return a, nil
		}
		if pw1 != pw2 {
			a.status = "Passwords do not match"
			a.currentScreen = screenUsers
			return a, nil
		}
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = fmt.Sprintf("Setting password for user %d on %s", a.pendingUserID, host)
		return a, tea.Batch(a.spinner.Tick, runUserSetPassword(host, a.username, a.password, a.pendingUserID, pw1))

	case "user-setname-confirm":
		dlg := a.activeDialog
		a.activeDialog = nil
		name := dlg.InputValue(0)
		if name == "" {
			a.status = "Name cannot be empty"
			a.currentScreen = screenUsers
			return a, nil
		}
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = fmt.Sprintf("Setting name for user %d on %s", a.pendingUserID, host)
		return a, tea.Batch(a.spinner.Tick, runUserSetName(host, a.username, a.password, a.pendingUserID, name))

	case "user-priv-2", "user-priv-3", "user-priv-4", "user-priv-5":
		a.activeDialog = nil
		if len(a.results) == 0 {
			return a, nil
		}
		level, _ := strconv.Atoi(strings.TrimPrefix(action, "user-priv-"))
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = fmt.Sprintf("Setting privilege level %d for user %d on %s", level, a.pendingUserID, host)
		return a, tea.Batch(a.spinner.Tick, runUserSetPrivilege(host, a.username, a.password, a.pendingUserID, level))

	case "user-delete":
		// Open confirmation dialog; pendingUserID already set.
		name := ""
		for _, u := range a.users {
			if u.ID == a.pendingUserID {
				name = u.Name
				break
			}
		}
		a.activeDialog = NewDeleteUserDialog(a.pendingUserID, name)
		return a, nil

	case "user-delete-confirm":
		a.activeDialog = nil
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = fmt.Sprintf("Deleting user %d on %s", a.pendingUserID, host)
		return a, tea.Batch(a.spinner.Tick, runUserDelete(host, a.username, a.password, a.pendingUserID))

	case "user-create-2", "user-create-3", "user-create-4":
		dlg := a.activeDialog
		a.activeDialog = nil
		name := dlg.InputValue(0)
		pw1 := dlg.InputValue(1)
		pw2 := dlg.InputValue(2)
		if name == "" {
			a.status = "Username cannot be empty"
			a.currentScreen = screenUsers
			return a, nil
		}
		if pw1 == "" {
			a.status = "Password cannot be empty"
			a.currentScreen = screenUsers
			return a, nil
		}
		if pw1 != pw2 {
			a.status = "Passwords do not match"
			a.currentScreen = screenUsers
			return a, nil
		}
		slotID := 0
		for _, u := range a.users {
			if !u.Enabled && u.Name == "" {
				slotID = u.ID
				break
			}
		}
		if slotID == 0 {
			a.status = "No free user slots available on this BMC"
			a.currentScreen = screenUsers
			return a, nil
		}
		if len(a.results) == 0 {
			return a, nil
		}
		level, _ := strconv.Atoi(strings.TrimPrefix(action, "user-create-"))
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = fmt.Sprintf("Creating user %q in slot %d on %s", name, slotID, host)
		return a, tea.Batch(a.spinner.Tick, runUserCreate(host, a.username, a.password, slotID, name, pw1, level))

	case "connect-bmc":
		dlg := a.activeDialog
		a.activeDialog = nil
		ip := strings.TrimSpace(dlg.InputValue(0))
		user := strings.TrimSpace(dlg.InputValue(1))
		pass := dlg.InputValue(2)
		if ip == "" {
			a.status = "IP address required"
			return a, nil
		}
		// Add the host to results if not already present, then connect directly.
		targetIdx := -1
		for i, r := range a.results {
			if r.IP == ip {
				targetIdx = i
				break
			}
		}
		if targetIdx < 0 {
			a.results = append(a.results, discovery.HostResult{
				IP:         ip,
				IsBMC:      true,
				Confidence: 100,
				Vendor:     "Direct",
			})
			targetIdx = len(a.results) - 1
		}
		a.selectedHost = targetIdx
		a.username = user
		a.password = pass
		a.ipmiLoading = true
		a.status = "Connecting to " + ip
		a.logf("Direct connect to %s as %s", ip, user)
		return a, tea.Batch(a.spinner.Tick, runMCInfo(ip, user, pass))

	case "log-start":
		dlg := a.activeDialog
		a.activeDialog = nil
		path := strings.TrimSpace(dlg.InputValue(0))
		if path == "" {
			a.status = "Log path cannot be empty"
			return a, nil
		}
		if strings.HasPrefix(path, "~/") {
			if home, err := os.UserHomeDir(); err == nil {
				path = home + path[1:]
			}
		}
		if a.sessionLog != nil {
			_ = a.sessionLog.Close()
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			a.status = "Log error: " + err.Error()
			return a, nil
		}
		a.sessionLog = f
		fmt.Fprintf(a.sessionLog, "Fyrtaarn Session Log\nStarted: %s\n%s\n\n",
			time.Now().Format(time.RFC3339), strings.Repeat("=", 60))
		a.status = "Session logging → " + path
		a.logf("Session log started.")
		return a, nil
	}

	return a, nil
}

/* ---------------- HELPERS ---------------- */

// quit closes the session log (if open) and returns tea.Quit.
func (a *App) quit() tea.Cmd {
	if a.sessionLog != nil {
		a.logf("Fyrtaarn exited.")
		_ = a.sessionLog.Close()
		a.sessionLog = nil
	}
	return tea.Quit
}

// logf writes a timestamped entry to the session log if logging is active.
func (a *App) logf(format string, args ...any) {
	if a.sessionLog == nil {
		return
	}
	fmt.Fprintf(a.sessionLog, "[%s] %s\n", time.Now().Format("15:04:05"), fmt.Sprintf(format, args...))
}

// flushSOLLog writes the current SOL screen content to the log and marks the
// end of the SOL session. Called before closing a pane (F10 or natural end).
func (a *App) flushSOLLog() {
	if a.sessionLog == nil || a.solPane == nil {
		return
	}
	for _, line := range a.solPane.screenLines() {
		fmt.Fprintln(a.sessionLog, line)
	}
	hostIP := ""
	if len(a.results) > 0 && a.selectedHost >= 0 {
		hostIP = a.results[a.selectedHost].IP
	}
	a.logf("SOL session ended — %s", hostIP)
}

/* ---------------- RESULTS ---------------- */

func (a *App) updateResults(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visibleLines := a.contentH - 4
	if visibleLines < 1 {
		visibleLines = 5
	}

	switch msg.String() {

	case "q":
		return a, a.quit()

	case "up", "k":
		if a.selectedHost > 0 {
			a.selectedHost--
			if a.selectedHost < a.resultsOffset {
				a.resultsOffset = a.selectedHost
			}
		} else if a.selectedHost == 0 {
			a.selectedHost = -1
		}

	case "down", "j":
		if a.selectedHost < 0 {
			if len(a.results) > 0 {
				a.selectedHost = 0
			}
		} else if a.selectedHost < len(a.results)-1 {
			a.selectedHost++
			if a.selectedHost >= a.resultsOffset+visibleLines {
				a.resultsOffset = a.selectedHost - visibleLines + 1
			}
		}

	case "tab":
		a.treeExpanded = !a.treeExpanded

	case "enter":
		if len(a.results) == 0 || a.selectedHost < 0 {
			return a, nil
		}
		host := a.results[a.selectedHost]
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
		// Do a live chassis check so we never show the power-on dialog for a
		// chassis that is already running (cached data can be stale).
		a.ipmiLoading = true
		a.loadProgress = Progress{}
		a.status = "Checking chassis power state..."
		return a, tea.Batch(a.spinner.Tick, checkChassisForSOL(host, a.username, a.password))

	case "v":
		a.activeDialog = NewVirtualMediaDialog()
		return a, textinput.Blink

	case "u":
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = "Loading users from " + host
		return a, tea.Batch(a.spinner.Tick, runGetUsers(host, a.username, a.password))

	case "c":
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost].IP
		a.ipmiLoading = true
		a.status = "Checking firmware compliance on " + host
		return a, tea.Batch(a.spinner.Tick, runFirmwareCheck(host, a.username, a.password))

	case "r":
		if len(a.results) == 0 {
			return a, nil
		}
		host := a.results[a.selectedHost]
		a.ipmiLoading = true
		a.status = "Enumerating Redfish on " + host.IP
		return a, tea.Batch(a.spinner.Tick, runRedfishEnum(host.IP, a.username, a.password))
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

/* ---------------- USERS ---------------- */

func (a *App) updateUsers(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visibleLines := a.contentH - 8
	if visibleLines < 1 {
		visibleLines = 5
	}

	switch msg.String() {
	case "esc", "q":
		a.currentScreen = screenMCInfo

	case "up", "k":
		if a.selectedUser > 0 {
			a.selectedUser--
			if a.selectedUser < a.usersOffset {
				a.usersOffset = a.selectedUser
			}
		}

	case "down", "j":
		if a.selectedUser < len(a.users)-1 {
			a.selectedUser++
			if a.selectedUser >= a.usersOffset+visibleLines {
				a.usersOffset = a.selectedUser - visibleLines + 1
			}
		}

	case "enter":
		if len(a.users) == 0 {
			return a, nil
		}
		u := a.users[a.selectedUser]
		a.pendingUserID = u.ID
		a.activeDialog = NewUserActionDialog(u.ID, u.Name, u.Enabled)
		return a, nil

	case "n":
		a.activeDialog = NewCreateUserDialog()
		return a, textinput.Blink
	}

	return a, nil
}

/* ---------------- FIRMWARE ---------------- */

func (a *App) updateFirmware(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Each CVE entry takes ~3 lines (ID+badge, description, blank).
	maxOffset := len(a.firmwareAdvisory)*3 + 10
	if maxOffset < 0 {
		maxOffset = 0
	}

	switch msg.String() {
	case "esc", "q":
		a.currentScreen = screenMCInfo
	case "up", "k":
		if a.firmwareOffset > 0 {
			a.firmwareOffset--
		}
	case "down", "j":
		if a.firmwareOffset < maxOffset {
			a.firmwareOffset++
		}
	}
	return a, nil
}

/* ---------------- REDFISH ENUM ---------------- */

func (a *App) updateRedfishEnum(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	visibleLines := a.contentH - 6
	if visibleLines < 1 {
		visibleLines = 5
	}

	totalLines := 0
	if a.redfishEnum != nil {
		// 12 lines per system (added Health, processor model, memory health);
		// 5 lines per manager.
		totalLines = len(a.redfishEnum.Systems)*12 + len(a.redfishEnum.Managers)*5
	}

	maxOffset := totalLines - visibleLines
	if maxOffset < 0 {
		maxOffset = 0
	}

	switch msg.String() {
	case "esc", "q":
		a.currentScreen = screenMCInfo
	case "up", "k":
		if a.redfishOffset > 0 {
			a.redfishOffset--
		}
	case "down", "j":
		if a.redfishOffset < maxOffset {
			a.redfishOffset++
		}
	}

	return a, nil
}

/* ---------------- SOL ---------------- */

func (a *App) updateSOL(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if a.solPane == nil {
		return a, nil
	}

	half := (a.contentH - 4) / 2
	if half < 1 {
		half = 1
	}

	switch msg.String() {
	case "f10":
		a.flushSOLLog()
		a.solPane.close()
		a.solPane = nil
		a.currentScreen = screenMCInfo
		a.status = "SOL session ended"
		return a, nil

	case "pgup", "shift+up":
		a.solPane.scrollUp += half
		return a, nil

	case "pgdown", "shift+down":
		a.solPane.scrollUp -= half
		if a.solPane.scrollUp < 0 {
			a.solPane.scrollUp = 0
		}
		return a, nil
	}

	// Backspace: send ESC[D (cursor-left) + \x04 (Ctrl+D, delete-at-cursor).
	// GRUB's serial driver maps ESC[D → GRUB_TERM_KEY_LEFT and \x04 →
	// GRUB_TERM_CTRL|'d' (forward-delete), giving reliable backward-delete
	// without relying on \x08 (ignored by some GRUB builds) or \x7f (echoed
	// as '.' by GRUB builds that treat it as a printable char).
	// editBuf tracks what has been typed so we only send when there is
	// actually a char to the left of the cursor.
	switch msg.String() {
	case "backspace", "ctrl+backspace", "ctrl+h":
		if len(a.solPane.editBuf) > 0 {
			a.solPane.editBuf = a.solPane.editBuf[:len(a.solPane.editBuf)-1]
			a.solPane.write([]byte{'\x1b', '[', 'D', '\x04'})
			a.solPane.scrollUp = 0
		}
		return a, nil
	}

	// Forward everything else to the pty.
	if b := keyToBytes(msg); len(b) > 0 {
		// Track printable chars and line-kill sequences in editBuf.
		if len(b) == 1 {
			switch {
			case b[0] >= 0x20 && b[0] < 0x7f:
				a.solPane.editBuf = append(a.solPane.editBuf, rune(b[0]))
			case b[0] == '\r', b[0] == '\x03', b[0] == '\x15':
				a.solPane.editBuf = a.solPane.editBuf[:0]
			}
		}
		a.solPane.write(b)
		// New input: snap back to bottom so the user sees the response.
		a.solPane.scrollUp = 0
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
			case screenUsers:
				content = a.renderUsers()
			case screenFirmware:
				content = a.renderFirmware()
			case screenRedfish:
				content = a.renderRedfishEnum()
			case screenSOL:
				content = a.renderSOL()
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
		if a.loadProgress.Total > 0 {
			// Reserve space: right side + padding + "N found" suffix.
			rw := lipgloss.Width(right) + 2
			suffix := fmt.Sprintf("  %d found", a.hostsFound)
			barWidth := a.width - rw - lipgloss.Width(suffix) - 18
			if barWidth < 4 {
				barWidth = 4
			}
			if barWidth > 20 {
				barWidth = 20
			}
			left = a.spinner.View() + " " + a.loadProgress.RenderCompact(barWidth) + suffix
		} else {
			left = a.spinner.View() + " Scanning..."
		}
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

	visibleLines := a.contentH - 4
	if visibleLines < 1 {
		visibleLines = 5
	}
	end := a.resultsOffset + visibleLines
	if end > len(a.results) {
		end = len(a.results)
	}

	if a.selectedHost < 0 {
		b.WriteString(MenuStyle(true).Render("  │") + "\n")
	} else {
		b.WriteString("  │\n")
	}

	rfStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(CurrentTheme.Accent)).
		Faint(true)

	for i, h := range a.results[a.resultsOffset:end] {
		absIdx := a.resultsOffset + i
		isLast := absIdx == len(a.results)-1
		selected := absIdx == a.selectedHost

		connector := "├─"
		if isLast {
			connector = "└─"
		}

		glyph := "   "
		if h.IsBMC {
			glyph = "⛯  "
		}

		// [RF] is purely informational — dim so it doesn't dominate the line.
		rfTag := ""
		if h.HasRedfish {
			rfTag = " " + rfStyle.Render("[RF]")
		}

		line := fmt.Sprintf("  %s %s%s", connector, glyph, h.IP) + rfTag

		if selected {
			b.WriteString(MenuStyle(true).Render(fmt.Sprintf("  %s %s%s", connector, glyph, h.IP)) + rfTag + "\n")
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

	if len(a.results) > end {
		b.WriteString(fmt.Sprintf("  └─ ... %d more hosts\n", len(a.results)-end))
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

	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(CurrentTheme.Accent)).Bold(true)

	b.WriteString(accent.Render("  Dedications") + "\n\n")

	b.WriteString(accent.Render("  Dan Kaminsky") + "\n")
	b.WriteString("  For pioneering work on BMC/IPMI security, remote management\n")
	b.WriteString("  attack surfaces, and infrastructure visibility. His research\n")
	b.WriteString("  exposed how dangerous and under-examined out-of-band systems\n")
	b.WriteString("  could be. A small tribute remains in the code.\n\n")

	b.WriteString(accent.Render("  IppSec") + "\n")
	b.WriteString("  For educational walkthroughs and practical demonstrations.\n")
	b.WriteString("  Trying your hardest to say Rødgrød med fløde... if you know,\n")
	b.WriteString("  you know. ;)\n\n")

	b.WriteString(accent.Render("  0xDF") + "\n")
	b.WriteString("  For detailed technical writeups on post-exploitation, beyond-\n")
	b.WriteString("  root methodologies, and operational enumeration. Jeg var at gå\n")
	b.WriteString("  agurk med \"should\" på \"Release Comittee\" (:\n")

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
		total := len(a.results)
		visLines := a.contentH - 4
		if visLines < 1 {
			visLines = 5
		}
		pos := "│"
		if a.selectedHost >= 0 {
			pos = strconv.Itoa(a.selectedHost + 1)
		}
		hint := fmt.Sprintf("[%s/%d]  [↑↓/jk] Navigate  [Tab] Expand  [Enter] Connect  [F9] Menu  [Q] Quit", pos, total)
		if total > visLines {
			end := a.resultsOffset + visLines
			if end > total {
				end = total
			}
			hint += fmt.Sprintf("  (showing %d–%d)", a.resultsOffset+1, end)
		}
		return hint

	case screenMCInfo:
		return "[S] Sensors  [L] Log  [F] FRU  [U] Users  [C] Compliance  [R] Redfish  [P] Power  [O] SOL  [V] VM  [ESC] Back"

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

	case screenUsers:
		total := len(a.users)
		if total == 0 {
			return "[ESC] Back"
		}
		return fmt.Sprintf("[%d/%d]  [↑/k] Up  [↓/j] Down  [Enter] Manage  [N] New  [ESC] Back",
			a.selectedUser+1, total)

	case screenFirmware:
		if a.advisoryLoading {
			return a.spinner.View() + " Fetching advisory feed (NVD + CISA KEV)...  [ESC] Back"
		}
		if len(a.firmwareAdvisory) > 0 {
			return fmt.Sprintf("%d CVE(s) shown  [↑/k] Up  [↓/j] Down  [ESC] Back", len(a.firmwareAdvisory))
		}
		return "[ESC] Back"

	case screenSOL:
		if a.solPane != nil && a.solPane.scrollUp > 0 {
			return "[F10] Disconnect  [PgUp] Scroll up  [PgDn] Scroll down  (any key snaps to bottom)"
		}
		return "[F10] Disconnect  [PgUp] Scroll up  — all other keys forwarded to BMC"

	case screenRedfish:
		return "[↑/k] Up  [↓/j] Down  [ESC] Back"

	}

	return a.status
}
