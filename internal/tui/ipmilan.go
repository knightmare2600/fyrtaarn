package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/knightmare2600/fyrtaarn/internal/redfish"
)

// IPMI over LAN on/off through Redfish, for BMCs that have it switched off
// and so answer Redfish but not ipmitool (seen on an HPE iLO 5 v1.40).

// rmcpFailMarker is how ipmitool reports that no IPMI session could be set
// up. It covers a BMC with IPMI over LAN off (no answer at all) and some
// credential failures, so it is only a cue to check over Redfish.
const rmcpFailMarker = "Unable to establish IPMI v2 / RMCP+ session"

type ipmiLanMsg struct {
	Host        string
	State       redfish.IPMIOverLAN
	Err         error
	FromConnect bool  // a failed connect triggered the check
	ConnectErr  error // that connect's error, reported if the check is no help
}

type ipmiLanSetMsg struct {
	Host        string
	On          bool
	Err         error
	FromConnect bool
}

func queryIPMILan(host, user, pass string, fromConnect bool, connectErr error) tea.Cmd {
	return func() tea.Msg {
		st, err := redfish.GetIPMIOverLAN(host, user, pass)
		return ipmiLanMsg{Host: host, State: st, Err: err, FromConnect: fromConnect, ConnectErr: connectErr}
	}
}

func setIPMILan(host, user, pass string, on, fromConnect bool) tea.Cmd {
	return func() tea.Msg {
		return ipmiLanSetMsg{Host: host, On: on, Err: redfish.SetIPMIOverLAN(host, user, pass, on), FromConnect: fromConnect}
	}
}

// NewIPMILanDialog shows IPMI over LAN's state on host and offers to switch
// it. fromConnect words it as the explanation for a failed connect.
func NewIPMILanDialog(host string, st redfish.IPMIOverLAN, fromConnect bool) *Dialog {
	port := ""
	if st.Port != 0 {
		port = fmt.Sprintf(" (UDP %d)", st.Port)
	}
	var b strings.Builder
	if fromConnect {
		fmt.Fprintf(&b, "Could not connect: IPMI over LAN is switched off\non %s%s. Redfish works, so it can be\nswitched on from here.\n\n", host, port)
	} else {
		fmt.Fprintf(&b, "IPMI over LAN on %s is %s%s.\n\n", host, strings.ToUpper(onOffWord(st.Enabled)), port)
	}
	b.WriteString("Fyrtaarn's BMC Info, sensors, event log, users,\n" +
		"power and SOL all use IPMI over LAN.\n\n" +
		"Be aware: IPMI 2.0 lets anyone who can reach the\n" +
		"port request a password hash for a valid user name\n" +
		"and crack it offline (CVE-2013-4786). Keep BMCs on\n" +
		"a management network.")
	action, label := "ipmilan-on", "Enable"
	if st.Enabled {
		action, label = "ipmilan-off", "Disable"
	}
	buttons := []DialogButton{{Label: label, Action: action}}
	if fromConnect {
		// Leave IPMI off and manage the BMC over Redfish (redfishonly.go).
		buttons = append(buttons, DialogButton{Label: "Use Redfish only", Action: "redfish-view"})
	}
	buttons = append(buttons, DialogButton{Label: "Cancel", Action: "cancel"})
	return &Dialog{
		Title:   "IPMI over LAN",
		Body:    b.String(),
		buttons: buttons,
		focus:   len(buttons) - 1, // changing BMC security settings is never the default
	}
}

func onOffWord(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// ipmiLanCreds returns the credentials to use for host: the session cache
// first, then whatever the last login dialog was given.
func (a *App) ipmiLanCreds(host string) (user, pass string, ok bool) {
	if u, p, ok := a.sessionCache.Get(host); ok {
		return u, p, true
	}
	if a.username != "" && a.password != "" {
		return a.username, a.password, true
	}
	return "", "", false
}

// openIPMILan starts the IPMI over LAN check for the selected host (BMC >
// IPMI over LAN...).
func (a *App) openIPMILan() (tea.Model, tea.Cmd) {
	if len(a.results) == 0 || a.selectedHost < 0 {
		a.status = "Select a host first"
		return a, nil
	}
	host := a.results[a.selectedHost]
	if !host.HasRedfish {
		a.status = "IPMI over LAN can only be checked and switched over Redfish, which " + host.IP + " does not offer"
		return a, nil
	}
	user, pass, ok := a.ipmiLanCreds(host.IP)
	if !ok {
		a.status = "No credentials for " + host.IP + " yet: press Enter on it to log in first"
		return a, nil
	}
	a.username, a.password = user, pass
	a.ipmiLoading = true
	a.status = "Checking IPMI over LAN on " + host.IP + " via Redfish..."
	return a, tea.Batch(a.spinner.Tick, queryIPMILan(host.IP, user, pass, false, nil))
}

func (a *App) handleIPMILanMsg(msg ipmiLanMsg) (tea.Model, tea.Cmd) {
	a.ipmiLoading = false
	if msg.Err != nil {
		if msg.FromConnect {
			// The check could not help; report the connect failure itself.
			a.status = msg.ConnectErr.Error()
		} else {
			a.status = "IPMI over LAN check failed: " + msg.Err.Error()
		}
		return a, nil
	}
	if msg.FromConnect && msg.State.Enabled {
		a.status = msg.ConnectErr.Error() + " (IPMI over LAN is on: check the credentials and UDP 623)"
		return a, nil
	}
	a.ipmiLanHost, a.ipmiLanFromConnect = msg.Host, msg.FromConnect
	a.activeDialog = NewIPMILanDialog(msg.Host, msg.State, msg.FromConnect)
	return a, nil
}

func (a *App) handleIPMILanSetMsg(msg ipmiLanSetMsg) (tea.Model, tea.Cmd) {
	a.ipmiLoading = false
	if msg.Err != nil {
		a.status = "Could not switch IPMI over LAN " + onOffWord(msg.On) + " on " + msg.Host + ": " + msg.Err.Error()
		a.logf("IPMI over LAN %s on %s failed: %v", onOffWord(msg.On), msg.Host, msg.Err)
		return a, nil
	}
	a.logf("IPMI over LAN switched %s on %s via Redfish", onOffWord(msg.On), msg.Host)
	a.status = "IPMI over LAN is now " + onOffWord(msg.On) + " on " + msg.Host
	if msg.FromConnect && msg.On {
		a.ipmiLoading = true
		a.status += "; connecting..."
		return a, tea.Batch(a.spinner.Tick, runMCInfo(msg.Host, a.username, a.password))
	}
	return a, nil
}
