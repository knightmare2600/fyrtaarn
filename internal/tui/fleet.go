package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/knightmare2600/fyrtaarn/internal/fleet"
)

// Fleet > Audit all hosts...: a read-only audit of every BMC in the host
// list (internal/fleet), shown as a table with each BMC's worst finding.

type fleetProgressMsg struct {
	gen int
	r   fleet.Result
}

type fleetDoneMsg struct {
	gen int
	rs  []fleet.Result
}

// fleetState is the audit screen's state.
type fleetState struct {
	gen     int
	results []fleet.Result // in host-list order; filled as BMCs finish
	done    int
	running bool
	cancel  context.CancelFunc
	cursor  int
	offset  int
	events  chan tea.Msg // progress, then one fleetDoneMsg
}

// NewFleetAuditDialog asks for the login to use on BMCs without a saved one.
func NewFleetAuditDialog(n, saved int, defaultUser string) *Dialog {
	user := textinput.New()
	user.Placeholder = "admin"
	user.SetValue(defaultUser)
	user.Focus()
	pass := textinput.New()
	pass.EchoMode = textinput.EchoPassword
	body := fmt.Sprintf("Audit %s, read-only, %d at a time: identity, power,\n"+
		"administrators, the anonymous user, cipher suite 0 and\n"+
		"factory-default logins. Nothing on the BMCs is changed.\n\n"+
		"Saved logins (%d of %d) are used as they are; this login\n"+
		"is used for the rest. Leave it empty to audit only those.", fleet.Plural(n, "BMC"), fleet.Workers, saved, n)
	return &Dialog{
		Title:       "Fleet Audit",
		Body:        body,
		inputs:      []textinput.Model{user, pass},
		inputLabels: []string{"Username", "Password"},
		buttons: []DialogButton{
			{Label: "Start", Action: "fleet-start"},
			{Label: "Cancel", Action: "cancel"},
		},
	}
}

// fleetHosts lists the BMCs in the host list.
func (a *App) fleetHosts() []string {
	var hosts []string
	for _, r := range a.results {
		if r.IsBMC {
			hosts = append(hosts, r.IP)
		}
	}
	return hosts
}

func (a *App) openFleetAudit() (tea.Model, tea.Cmd) {
	hosts := a.fleetHosts()
	if len(hosts) == 0 {
		a.status = "No BMCs in the host list: scan or connect first"
		return a, nil
	}
	saved := 0
	for _, h := range hosts {
		if _, _, ok := a.sessionCache.Get(h); ok {
			saved++
		}
	}
	a.activeDialog = NewFleetAuditDialog(len(hosts), saved, a.username)
	return a, textinput.Blink
}

// startFleetAudit builds the targets: a host's saved login, else the one
// typed; hosts with neither are left out and said so.
func (a *App) startFleetAudit() (tea.Model, tea.Cmd) {
	user, pass := a.activeDialog.InputValue(0), a.activeDialog.InputValue(1)
	a.activeDialog = nil
	var targets []fleet.Target
	skipped := 0
	for _, h := range a.fleetHosts() {
		if u, p, ok := a.sessionCache.Get(h); ok {
			targets = append(targets, fleet.Target{IP: h, User: u, Pass: p})
		} else if user != "" && pass != "" {
			targets = append(targets, fleet.Target{IP: h, User: user, Pass: pass})
		} else {
			skipped++
		}
	}
	if len(targets) == 0 {
		a.status = "Fleet audit: no BMC has a login to use"
		return a, nil
	}
	if a.fleet != nil && a.fleet.cancel != nil {
		a.fleet.cancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	gen := 1
	if a.fleet != nil {
		gen = a.fleet.gen + 1
	}
	a.fleet = &fleetState{gen: gen, running: true, cancel: cancel, results: make([]fleet.Result, len(targets))}
	for i, t := range targets {
		a.fleet.results[i] = fleet.Result{IP: t.IP}
	}
	a.fleetReturn = a.currentScreen
	a.currentScreen = screenFleet
	a.status = fmt.Sprintf("Fleet audit: 0 of %d", len(targets))
	if skipped > 0 {
		a.status += fmt.Sprintf(" (%d left out: no login)", skipped)
	}
	a.logf("fleet audit of %d BMCs started (%d without a login left out)", len(targets), skipped)
	a.fleet.events = startFleetAudit(ctx, gen, targets)
	return a, waitFleet(a.fleet.events)
}

// startFleetAudit runs the audit in the background; each finished BMC,
// then the whole result, arrives on the returned channel, which the UI
// reads one message at a time (waitFleet), as the SOL pane reads its
// output.
func startFleetAudit(ctx context.Context, gen int, targets []fleet.Target) chan tea.Msg {
	ch := make(chan tea.Msg, len(targets)+1)
	go func() {
		rs := fleet.Run(ctx, targets, func(r fleet.Result) { ch <- fleetProgressMsg{gen: gen, r: r} })
		ch <- fleetDoneMsg{gen: gen, rs: rs}
	}()
	return ch
}

func waitFleet(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (a *App) handleFleetProgress(msg fleetProgressMsg) tea.Cmd {
	if a.fleet == nil || msg.gen != a.fleet.gen {
		return nil
	}
	for i := range a.fleet.results {
		if a.fleet.results[i].IP == msg.r.IP {
			a.fleet.results[i] = msg.r
			break
		}
	}
	a.fleet.done++
	a.status = fmt.Sprintf("Fleet audit: %d of %d", a.fleet.done, len(a.fleet.results))
	return waitFleet(a.fleet.events)
}

func (a *App) handleFleetDone(msg fleetDoneMsg) {
	if a.fleet == nil || msg.gen != a.fleet.gen {
		return
	}
	a.fleet.results, a.fleet.running, a.fleet.done = msg.rs, false, len(msg.rs)
	a.status = "Fleet audit: " + fleet.Summary(msg.rs)
	a.logf("fleet audit: %s", fleet.Summary(msg.rs))
}

func fleetVisibleRows(h int) int { return max(h-5, 1) }

func (a *App) updateFleet(k string) (tea.Model, tea.Cmd) {
	f := a.fleet
	rows := fleetVisibleRows(a.contentH)
	move := func(d int) {
		if len(f.results) == 0 {
			return
		}
		f.cursor = min(max(f.cursor+d, 0), len(f.results)-1)
		if f.cursor < f.offset {
			f.offset = f.cursor
		}
		if f.cursor >= f.offset+rows {
			f.offset = f.cursor - rows + 1
		}
	}
	switch k {
	case "esc":
		if f.running && f.cancel != nil {
			f.cancel() // BMCs already started finish; no new ones start
			a.status = "Fleet audit: stopping after the BMCs already started"
			return a, nil
		}
		a.currentScreen = a.fleetReturn
		return a, nil
	case "up", "k":
		move(-1)
	case "down", "j":
		move(1)
	case "pgup":
		move(-rows)
	case "pgdown":
		move(rows)
	case "home":
		move(-len(f.results))
	case "end":
		move(len(f.results))
	case "enter":
		if len(f.results) > 0 {
			a.activeDialog = NewFleetDetailDialog(f.results[f.cursor])
		}
	case "e":
		if f.running {
			a.status = "Fleet audit still running; export when it has finished"
			return a, nil
		}
		a.activeDialog = NewFleetExportDialog(a.lastFleetExportPath)
		return a, textinput.Blink
	}
	return a, nil
}

// NewFleetDetailDialog lists one BMC's findings.
func NewFleetDetailDialog(r fleet.Result) *Dialog {
	var b strings.Builder
	if r.Err != "" {
		b.WriteString("Not audited: " + wrap(r.Err, 56, "") + "\n")
	} else {
		fmt.Fprintf(&b, "%s %s, firmware %s, IPMI %s\n", r.Manufacturer, r.Product, r.Firmware, r.IPMIVersion)
		fmt.Fprintf(&b, "Power %s; IPMI via %s\n", orDash(r.Power), wrap(r.Via, 50, "  "))
		fmt.Fprintf(&b, "Enabled users: %d; administrators: %s\n", r.EnabledUsers, orDash(strings.Join(r.Admins, ", ")))
		fmt.Fprintf(&b, "Cipher suite 0: %s\n", r.Suite0)
		if len(r.KnownIssues) > 0 {
			fmt.Fprintf(&b, "Known issues: %s\n", wrap(strings.Join(r.KnownIssues, ", "), 50, "  "))
		}
		b.WriteString("\n")
		if len(r.Findings) == 0 {
			b.WriteString("No findings.")
		}
		for i, f := range r.Findings {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "%-8s %s", strings.ToUpper(f.Severity.String()), wrap(f.Text, 52, "         "))
		}
	}
	return &Dialog{
		Title:   "Fleet Audit — " + r.IP,
		Body:    b.String(),
		buttons: []DialogButton{{Label: "Close", Action: "cancel"}},
	}
}

// NewFleetExportDialog asks where to write the audit.
func NewFleetExportDialog(defaultPath string) *Dialog {
	inp := textinput.New()
	inp.Placeholder = "~/fyrtaarn-fleet-audit.csv"
	inp.Width = 50
	inp.SetValue(defaultPath)
	inp.Focus()
	return &Dialog{
		Title:       "Export Fleet Audit",
		Body:        "Write the audit to a file, readable by you only.\nIt lists findings per BMC; it contains no passwords.",
		inputs:      []textinput.Model{inp},
		inputLabels: []string{"Output path"},
		buttons: []DialogButton{
			{Label: "CSV", Action: "fleet-export-csv"},
			{Label: "JSON", Action: "fleet-export-json"},
			{Label: "Cancel", Action: "cancel"},
		},
	}
}

func (a *App) exportFleet(asJSON bool) (tea.Model, tea.Cmd) {
	path := strings.TrimSpace(expandHome(a.activeDialog.InputValue(0)))
	a.activeDialog = nil
	if path == "" || a.fleet == nil {
		a.status = "Fleet export: no path given"
		return a, nil
	}
	var err error
	if asJSON {
		err = fleet.WriteJSON(path, a.fleet.results)
	} else {
		err = fleet.WriteCSV(path, a.fleet.results)
	}
	if err != nil {
		a.status = "Fleet export failed: " + err.Error()
		return a, nil
	}
	a.lastFleetExportPath = path
	a.status = "Fleet audit written to " + path
	return a, nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// renderFleet draws the audit table.
func (a *App) renderFleet() string {
	f := a.fleet
	var b strings.Builder
	b.WriteString(HeaderStyle().Render("FLEET AUDIT") + "  ")
	if f.running {
		fmt.Fprintf(&b, "%d of %d done", f.done, len(f.results))
	} else {
		b.WriteString(fleet.Summary(f.results))
	}
	b.WriteString("\n")
	hdr := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent))
	b.WriteString("  " + hdr.Render(fmt.Sprintf("%-15s %-16s %-8s %-5s %-9s %s", "BMC", "Manufacturer", "Firmware", "Power", "Suite 0", "Findings")) + "\n")
	b.WriteString("  " + strings.Repeat("─", max(min(a.width-4, 100), 20)) + "\n")
	sel := lipgloss.NewStyle().Reverse(true)
	colour := map[fleet.Severity]string{fleet.Critical: "#DC322F", fleet.Warning: "#CB4B16", fleet.Note: "#839496"}
	rows := fleetVisibleRows(a.contentH)
	for i := f.offset; i < len(f.results) && i < f.offset+rows; i++ {
		r := f.results[i]
		findings := "…"
		switch {
		case r.Err != "":
			findings = "not reached"
		case r.Manufacturer == "" && f.running:
		default:
			findings = findingCounts(r.Findings)
		}
		line := fmt.Sprintf("%-15s %-16s %-8s %-5s %-9s %s", r.IP, truncRunes(r.Manufacturer, 16), truncRunes(r.Firmware, 8),
			orDash(r.Power), truncRunes(orDash(r.Suite0), 9), findings)
		line = truncRunes(line, max(a.width-4, 40))
		switch {
		case i == f.cursor:
			line = sel.Render(line)
		case r.Err == "" && r.Worst() >= 0:
			line = lipgloss.NewStyle().Foreground(lipgloss.Color(colour[r.Worst()])).Render(line)
		}
		b.WriteString("  " + line + "\n")
	}
	return b.String()
}

func truncRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:max(n-1, 0)]) + "…"
	}
	return s
}

// findingCounts is "1 critical, 1 note", most severe first, or "none".
func findingCounts(fs []fleet.Finding) string {
	n := map[fleet.Severity]int{}
	for _, f := range fs {
		n[f.Severity]++
	}
	var parts []string
	for _, sev := range []fleet.Severity{fleet.Critical, fleet.Warning, fleet.Note} {
		if n[sev] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n[sev], sev))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}
