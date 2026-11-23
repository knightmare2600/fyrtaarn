package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
	"github.com/knightmare2600/fyrtaarn/internal/redfish"
)

// BMC > Reset BMC (cold)...: restarts the management controller only,
// over IPMI ("mc reset cold") or, in Redfish-only mode, Redfish
// Manager.Reset. The dialog spells out what is and is not affected and
// needs the BMC's address typed before it does anything.

type bmcResetMsg struct {
	Host, Via string
	Err       error
}

func runBMCReset(host, user, pass string, redfishOnly bool) tea.Cmd {
	return func() tea.Msg {
		if redfishOnly {
			t, err := redfish.ResetManager(host, user, pass)
			via := "Redfish Manager.Reset"
			if t != "" {
				via += " (" + t + ")"
			}
			return bmcResetMsg{Host: host, Via: via, Err: err}
		}
		return bmcResetMsg{Host: host, Via: "IPMI mc reset cold", Err: ipmi.ColdResetBMC(host, user, pass)}
	}
}

// NewBMCResetDialog explains the reset and asks for the address. Focus
// starts in the address box; Cancel is the next stop, before Reset.
func NewBMCResetDialog(host, via string, dropping []string) *Dialog {
	var b strings.Builder
	fmt.Fprintf(&b, "Cold reset the BMC at %s (%s).\n\n", host, via)
	b.WriteString("This restarts the management controller ONLY:\n" +
		"  • the server it manages keeps running; its operating\n" +
		"    system, power state and workloads are not touched\n" +
		"  • BMC settings, users and logs are kept\n" +
		"  • the BMC does not answer IPMI, its web UI or Redfish\n" +
		"    for one to three minutes while it restarts\n" +
		"  • any open console, virtual media or web session to\n" +
		"    this BMC, from any client, is cut")
	if len(dropping) > 0 {
		b.WriteString("\n\nfyrtaarn closes its own first: " + strings.Join(dropping, ", ") + ".")
	}
	b.WriteString("\n\nIt is not a server reset and not a factory reset.\n" +
		"Type the BMC address to confirm.")
	in := textinput.New()
	in.Placeholder = "type the address shown above"
	in.Width = 30
	in.Focus()
	return &Dialog{
		Title:       "Reset BMC — " + host,
		Body:        b.String(),
		Warning:     "Restarts the BMC; the server is not affected",
		inputs:      []textinput.Model{in},
		inputLabels: []string{"BMC address"},
		buttons: []DialogButton{
			{Label: "Cancel", Action: "cancel"},
			{Label: "Reset BMC", Action: "bmc-reset-confirm"},
		},
		focus: 0,
	}
}

// openBMCReset shows the confirmation for the selected host.
func (a *App) openBMCReset() (tea.Model, tea.Cmd) {
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
	via := "over IPMI"
	if a.redfishOnly {
		via = "over Redfish"
	}
	var dropping []string
	if a.amiMediaHost == host && a.amiMediaStop != nil {
		dropping = append(dropping, "the attached virtual drive")
	}
	if _, ok := a.webProxies[host]; ok {
		dropping = append(dropping, "the web UI proxy")
	}
	a.bmcResetHost = host
	a.activeDialog = NewBMCResetDialog(host, via, dropping)
	return a, textinput.Blink
}

// confirmBMCReset runs only when the typed address matches exactly.
func (a *App) confirmBMCReset() (tea.Model, tea.Cmd) {
	typed := strings.TrimSpace(a.activeDialog.InputValue(0))
	a.activeDialog = nil
	host := a.bmcResetHost
	a.bmcResetHost = ""
	if host == "" || typed != host {
		a.status = "BMC not reset: the address typed did not match " + host
		return a, nil
	}
	if a.amiMediaHost == host {
		a.stopAMIMedia()
	}
	if p, ok := a.webProxies[host]; ok {
		_ = p.Close()
		delete(a.webProxies, host)
	}
	a.ipmiLoading = true
	a.status = "Resetting the BMC at " + host + "..."
	a.logf("BMC reset requested for %s", host)
	return a, tea.Batch(a.spinner.Tick, runBMCReset(host, a.username, a.password, a.redfishOnly))
}

func (a *App) handleBMCReset(msg bmcResetMsg) (tea.Model, tea.Cmd) {
	a.ipmiLoading = false
	if msg.Err != nil {
		// A BMC can restart before it answers, so a failure here does not
		// prove nothing happened.
		a.status = fmt.Sprintf("BMC reset of %s via %s reported: %v (it may still be restarting; check in a few minutes)", msg.Host, msg.Via, msg.Err)
	} else {
		a.status = fmt.Sprintf("BMC at %s is restarting (%s); it will not answer for one to three minutes", msg.Host, msg.Via)
	}
	a.logf("BMC reset of %s via %s: %v", msg.Host, msg.Via, msg.Err)
	return a, nil
}
