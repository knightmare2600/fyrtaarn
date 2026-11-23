package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
)

// BMC > SOL settings...: shows whether the BMC offers SOL and whether the
// logged-in user may use it, and switches either. The server's own
// firmware must still redirect its console to the BMC's serial port; that
// is per vendor and set in the server's setup, so fyrtaarn only says so.

type solCfgMsg struct {
	Host     string
	Settings ipmi.SOLSettings
	Err      error
	Note     string // result of a change, shown with the refreshed settings
}

func runSOLSettings(host, user, pass, note string) tea.Cmd {
	return func() tea.Msg {
		s, err := ipmi.GetSOLSettings(host, user, pass)
		return solCfgMsg{Host: host, Settings: s, Err: err, Note: note}
	}
}

// runSOLChange applies one change, then reads the settings back.
func runSOLChange(host, user, pass, action string, userID int) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch action {
		case "solcfg-enable":
			err = ipmi.SetSOLEnabled(host, user, pass, true)
		case "solcfg-disable":
			err = ipmi.SetSOLEnabled(host, user, pass, false)
		case "solcfg-allow-user":
			err = ipmi.SetSOLPayload(host, user, pass, userID, true)
		}
		note := "Change applied"
		if err != nil {
			note = "Change failed: " + err.Error()
		}
		s, gerr := ipmi.GetSOLSettings(host, user, pass)
		return solCfgMsg{Host: host, Settings: s, Err: gerr, Note: note}
	}
}

func onOffText(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// NewSOLSettingsDialog shows SOL's state and offers the changes that
// apply. Cancel is focused: these are BMC settings.
func NewSOLSettingsDialog(host, user string, s ipmi.SOLSettings, note string) *Dialog {
	var b strings.Builder
	fmt.Fprintf(&b, "SOL on %s: %s\n", host, strings.ToUpper(onOffText(s.Enabled)))
	if s.BitRate != "" {
		fmt.Fprintf(&b, "Bit rate: %s kbps (non-volatile %s)\n", s.BitRate, s.NVBitRate)
	}
	if s.Privilege != "" {
		fmt.Fprintf(&b, "Needs privilege: %s\n", s.Privilege)
	}
	switch s.UserPayload {
	case "":
		fmt.Fprintf(&b, "SOL access for %s: unknown\n", user)
	default:
		fmt.Fprintf(&b, "SOL access for %s (user %d): %s\n", user, s.UserID, s.UserPayload)
	}
	b.WriteString("\nThe server must also send its console to the BMC:\n" +
		"BIOS/UEFI console (serial) redirection to the BMC's\n" +
		"serial port, at the bit rate above. That is set in\n" +
		"the server's own setup and differs by vendor.")
	if note != "" {
		b.WriteString("\n\n" + note)
	}
	var buttons []DialogButton
	if s.Enabled {
		buttons = append(buttons, DialogButton{Label: "Disable SOL", Action: "solcfg-disable"})
	} else {
		buttons = append(buttons, DialogButton{Label: "Enable SOL", Action: "solcfg-enable"})
	}
	if s.UserPayload == "disabled" {
		buttons = append(buttons, DialogButton{Label: "Allow " + user, Action: "solcfg-allow-user"})
	}
	buttons = append(buttons, DialogButton{Label: "Cancel", Action: "cancel"})
	return &Dialog{Title: "SOL Settings", Body: b.String(), buttons: buttons, focus: len(buttons) - 1}
}

// openSOLSettings reads SOL settings for the selected host over IPMI.
func (a *App) openSOLSettings() (tea.Model, tea.Cmd) {
	if len(a.results) == 0 || a.selectedHost < 0 {
		a.status = "Select a host first"
		return a, nil
	}
	host := a.results[a.selectedHost].IP
	user, pass, ok := a.ipmiLanCreds(host)
	if !ok {
		a.status = "No credentials for " + host + " yet: press Enter on it to log in first"
		return a, nil
	}
	a.username, a.password = user, pass
	a.ipmiLoading = true
	a.status = "Reading SOL settings of " + host + "..."
	return a, tea.Batch(a.spinner.Tick, runSOLSettings(host, user, pass, ""))
}

func (a *App) handleSOLCfg(msg solCfgMsg) (tea.Model, tea.Cmd) {
	a.ipmiLoading = false
	if msg.Err != nil {
		a.status = "SOL settings: " + msg.Err.Error()
		if msg.Note != "" {
			a.status = msg.Note + "; reading back failed: " + msg.Err.Error()
		}
		return a, nil
	}
	a.solCfgHost, a.solCfgUserID = msg.Host, msg.Settings.UserID
	a.activeDialog = NewSOLSettingsDialog(msg.Host, a.username, msg.Settings, msg.Note)
	if msg.Note != "" {
		a.logf("SOL settings on %s: %s", msg.Host, msg.Note)
	}
	return a, nil
}

func (a *App) solCfgAction(action string) (tea.Model, tea.Cmd) {
	a.activeDialog = nil
	a.ipmiLoading = true
	a.status = "Changing SOL settings on " + a.solCfgHost + "..."
	return a, tea.Batch(a.spinner.Tick, runSOLChange(a.solCfgHost, a.username, a.password, action, a.solCfgUserID))
}
