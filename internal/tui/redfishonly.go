package tui

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
	"github.com/knightmare2600/fyrtaarn/internal/redfish"
)

// Redfish-only mode: manage a BMC whose IPMI over LAN is off, or that has
// none, entirely over Redfish. BMC > Redfish view... (or "Use Redfish only"
// in the IPMI over LAN dialog) opens the Redfish screen, where [S] reads
// sensors from the Chassis Thermal/Power resources, [P] offers the reset
// types the BMC allows, and [V]/[W] work as elsewhere.

type rfSensorsMsg struct {
	Sensors []redfish.ChassisSensor
	Err     error
}

type rfPowerInfoMsg struct {
	Host string
	Info redfish.PowerInfo
	Err  error
}

type rfLogMsg struct {
	Entries []redfish.LogEntry
	Err     error
}

type rfUsersMsg struct {
	Accounts []redfish.Account
	Err      error
}

type rfFirmwareMsg struct {
	Items []redfish.Firmware
	Err   error
}

type rfResetMsg struct {
	Host, Type string
	Err        error
}

func runRedfishSensors(host, user, pass string) tea.Cmd {
	return func() tea.Msg {
		s, err := redfish.GetChassisSensors(host, user, pass)
		return rfSensorsMsg{Sensors: s, Err: err}
	}
}

func runRedfishPowerInfo(host, user, pass string) tea.Cmd {
	return func() tea.Msg {
		info, err := redfish.GetPowerInfo(host, user, pass)
		return rfPowerInfoMsg{Host: host, Info: info, Err: err}
	}
}

func runRedfishReset(host, user, pass, resetType string) tea.Cmd {
	return func() tea.Msg {
		return rfResetMsg{Host: host, Type: resetType, Err: redfish.ResetSystem(host, user, pass, resetType)}
	}
}

// NewRedfishPowerDialog offers the reset types the BMC allows. Cancel is
// focused: power changes are never the default.
func NewRedfishPowerDialog(host string, info redfish.PowerInfo) *Dialog {
	body := "Host: " + host + " (via Redfish)"
	if info.State != "" {
		body += "\nState: " + info.State
	}
	var buttons []DialogButton
	for _, c := range info.Choices() {
		buttons = append(buttons, DialogButton{Label: c.Label, Action: "rfpower:" + c.Type})
	}
	buttons = append(buttons, DialogButton{Label: "Cancel", Action: "cancel"})
	return &Dialog{Title: "Power Control", Body: body, buttons: buttons, focus: len(buttons) - 1}
}

// openRedfishView starts Redfish-only mode for the selected host.
func (a *App) openRedfishView() (tea.Model, tea.Cmd) {
	if len(a.results) == 0 || a.selectedHost < 0 {
		a.status = "Select a host first"
		return a, nil
	}
	host := a.results[a.selectedHost]
	if !host.HasRedfish {
		a.status = host.IP + " does not offer Redfish"
		return a, nil
	}
	user, pass, ok := a.ipmiLanCreds(host.IP)
	if !ok {
		a.status = "No credentials for " + host.IP + " yet: press Enter on it to log in first"
		return a, nil
	}
	a.username, a.password = user, pass
	a.redfishOnly = true
	a.ipmiLoading = true
	a.status = "Reading " + host.IP + " over Redfish..."
	return a, tea.Batch(a.spinner.Tick, runRedfishEnum(host.IP, user, pass))
}

// redfishScreenKey handles the Redfish screen's action keys; handled is
// false for keys it does not use.
func (a *App) redfishScreenKey(k string) (tea.Model, tea.Cmd, bool) {
	if len(a.results) == 0 || a.selectedHost < 0 {
		return a, nil, false
	}
	host := a.results[a.selectedHost].IP
	switch k {
	case "s":
		a.ipmiLoading = true
		a.status = "Reading sensors from " + host + " over Redfish..."
		return a, tea.Batch(a.spinner.Tick, runRedfishSensors(host, a.username, a.password)), true
	case "p":
		a.ipmiLoading = true
		a.status = "Reading power state of " + host + " over Redfish..."
		return a, tea.Batch(a.spinner.Tick, runRedfishPowerInfo(host, a.username, a.password)), true
	case "l", "u", "f":
		what := map[string]string{"l": "event log", "u": "user accounts", "f": "firmware inventory"}[k]
		a.ipmiLoading = true
		a.status = "Reading " + what + " of " + host + " over Redfish..."
		user, pass := a.username, a.password
		var cmd tea.Cmd
		switch k {
		case "l":
			cmd = func() tea.Msg { e, err := redfish.GetEventLogs(host, user, pass); return rfLogMsg{e, err} }
		case "u":
			cmd = func() tea.Msg { u, err := redfish.GetAccounts(host, user, pass); return rfUsersMsg{u, err} }
		case "f":
			cmd = func() tea.Msg { f, err := redfish.GetFirmware(host, user, pass); return rfFirmwareMsg{f, err} }
		}
		return a, tea.Batch(a.spinner.Tick, cmd), true
	case "v":
		a.activeDialog = NewVirtualMediaDialog(true)
		return a, nil, true
	case "w":
		m, c := a.openWebUI(host)
		return m, c, true
	case "k":
		a.activeDialog = NewKnownIssuesDialog(host)
		return a, nil, true
	}
	return a, nil, false
}

func (a *App) handleRFSensors(msg rfSensorsMsg) (tea.Model, tea.Cmd) {
	a.ipmiLoading = false
	if msg.Err != nil {
		a.status = "Redfish sensors: " + msg.Err.Error()
		return a, nil
	}
	a.sensors = a.sensors[:0]
	for _, s := range msg.Sensors {
		a.sensors = append(a.sensors, ipmi.SDREntry{Name: s.Name, Value: s.Value, Status: s.Status})
	}
	a.sensorsFromRedfish = true
	a.sdrOffset, a.sensorCursor = 0, 0
	a.currentScreen = screenSensor
	a.status = "Sensors read over Redfish"
	return a, nil
}

func (a *App) handleRFPowerInfo(msg rfPowerInfoMsg) (tea.Model, tea.Cmd) {
	a.ipmiLoading = false
	if msg.Err != nil {
		a.status = "Redfish power: " + msg.Err.Error()
		return a, nil
	}
	a.activeDialog = NewRedfishPowerDialog(msg.Host, msg.Info)
	return a, nil
}

func (a *App) handleRFReset(msg rfResetMsg) (tea.Model, tea.Cmd) {
	a.ipmiLoading = false
	if msg.Err != nil {
		a.status = "Redfish " + msg.Type + " on " + msg.Host + " failed: " + msg.Err.Error()
	} else {
		a.status = "Redfish " + msg.Type + " sent to " + msg.Host
	}
	a.logf("Redfish reset %s on %s: %v", msg.Type, msg.Host, msg.Err)
	return a, nil
}

// rfPowerAction runs a "rfpower:<ResetType>" dialog action.
func (a *App) rfPowerAction(action string) (tea.Model, tea.Cmd) {
	a.activeDialog = nil
	t := strings.TrimPrefix(action, "rfpower:")
	host := a.results[a.selectedHost].IP
	a.ipmiLoading = true
	a.status = "Sending " + t + " to " + host + " over Redfish..."
	return a, tea.Batch(a.spinner.Tick, runRedfishReset(host, a.username, a.password, t))
}

// handleRFLog shows Redfish log entries on the event log screen, newest
// first, each tagged with its log service.
func (a *App) handleRFLog(msg rfLogMsg) (tea.Model, tea.Cmd) {
	a.ipmiLoading = false
	if msg.Err != nil {
		a.status = "Redfish event log: " + msg.Err.Error()
		return a, nil
	}
	a.selEntries = a.selEntries[:0]
	for _, e := range msg.Entries {
		a.selEntries = append(a.selEntries, ipmi.SELEntry{
			ID: e.ID, Timestamp: e.Created, Event: "[" + e.Log + "] " + e.Message, Direction: e.Severity,
		})
	}
	a.selOffset, a.backToRedfish = 0, true
	a.currentScreen = screenSEL
	a.status = fmt.Sprintf("%d events over Redfish (first page of each log)", len(a.selEntries))
	return a, nil
}

// handleRFUsers shows Redfish accounts on the Users screen, read-only: its
// actions are IPMI commands.
func (a *App) handleRFUsers(msg rfUsersMsg) (tea.Model, tea.Cmd) {
	a.ipmiLoading = false
	if msg.Err != nil {
		a.status = "Redfish accounts: " + msg.Err.Error()
		return a, nil
	}
	a.users = a.users[:0]
	for _, acc := range msg.Accounts {
		id, _ := strconv.Atoi(acc.ID)
		priv := acc.Role
		if acc.Locked {
			priv += " (locked)"
		}
		a.users = append(a.users, ipmi.UserEntry{ID: id, Name: acc.UserName, Enabled: acc.Enabled, Privilege: priv})
	}
	a.usersOffset, a.selectedUser = 0, 0
	a.usersFromRedfish, a.backToRedfish = true, true
	a.currentScreen = screenUsers
	a.status = fmt.Sprintf("%d accounts over Redfish (read-only here)", len(a.users))
	return a, nil
}

// handleRFFirmware shows the firmware inventory on the FRU screen.
func (a *App) handleRFFirmware(msg rfFirmwareMsg) (tea.Model, tea.Cmd) {
	a.ipmiLoading = false
	if msg.Err != nil {
		a.status = "Redfish firmware: " + msg.Err.Error()
		return a, nil
	}
	a.fru = []ipmi.FRUEntry{{Field: "Firmware inventory", Value: "over Redfish", IsHeader: true}}
	for _, f := range msg.Items {
		name := f.Name
		if strings.HasPrefix(f.ID, "Previous") {
			name += " (previous)"
		}
		a.fru = append(a.fru, ipmi.FRUEntry{Field: name, Value: f.Version})
	}
	a.fruOffset, a.backToRedfish = 0, true
	a.currentScreen = screenFRU
	a.status = fmt.Sprintf("%d firmware items over Redfish", len(msg.Items))
	return a, nil
}
