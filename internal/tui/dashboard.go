package tui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/knightmare2600/fyrtaarn/internal/config"
	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
	"github.com/knightmare2600/fyrtaarn/internal/redfish"
)

// Sensor dashboard: live readings for sensors chosen on the Sensors screen
// (Space marks, D opens), over IPMI or, for sensors read in Redfish-only
// mode, over Redfish. A full "sdr list" walks the BMC's whole sensor
// repository (27 s on the Quanta in Tested Hardware); the dashboard dumps
// that repository once per session and then reads only its sensors (0.3 s
// for four on the same BMC), so it can refresh every few seconds.

// dashIntervals are the refresh periods +/- step through.
var dashIntervals = []time.Duration{2 * time.Second, 5 * time.Second, 10 * time.Second, 30 * time.Second, time.Minute}

const (
	dashDefaultInterval = 1 // index into dashIntervals: 5 s
	dashHistory         = 30
)

type dashSeries struct {
	unit     string
	values   []float64 // oldest first, at most dashHistory
	min, max float64
	seen     bool
}

type dashboard struct {
	host     string
	redfish  bool     // readings from Redfish Chassis resources, not ipmitool
	names    []string // analog sensors, in display order
	series   map[string]*dashSeries
	cache    string
	gen      int // bumped to orphan pending ticks and reads
	interval int // index into dashIntervals
	updated  time.Time
	err      string
	missing  []string // chosen sensors with no reading in the last refresh
}

type dashDumpMsg struct {
	gen int
	err error
}

type dashReadMsg struct {
	gen      int
	readings []ipmi.SensorReading
	err      error
	at       time.Time
}

type dashTickMsg struct{ gen int }

func dumpSDRCmd(gen int, host, user, pass, cache string) tea.Cmd {
	return func() tea.Msg { return dashDumpMsg{gen: gen, err: ipmi.DumpSDR(host, user, pass, cache)} }
}

func readSensorsCmd(gen int, host, user, pass, cache string, names []string) tea.Cmd {
	return func() tea.Msg {
		rs, err := ipmi.ReadSensors(host, user, pass, cache, names)
		return dashReadMsg{gen: gen, readings: rs, err: err, at: time.Now()}
	}
}

// readRedfishSensorsCmd reads the chosen sensors over Redfish: every
// Chassis's Thermal and Power resources (a handful of GETs), filtered to
// names, numbers taken from the readings' "<number> <unit>" form.
func readRedfishSensorsCmd(gen int, host, user, pass string, names []string) tea.Cmd {
	return func() tea.Msg {
		all, err := redfish.GetChassisSensors(host, user, pass)
		want := map[string]bool{}
		for _, n := range names {
			want[n] = true
		}
		var rs []ipmi.SensorReading
		seen := map[string]bool{}
		for _, s := range all {
			if !want[s.Name] || seen[s.Name] {
				continue
			}
			f := strings.Fields(s.Value)
			if len(f) == 0 {
				continue
			}
			if v, perr := strconv.ParseFloat(f[0], 64); perr == nil {
				seen[s.Name] = true
				rs = append(rs, ipmi.SensorReading{Name: s.Name, Value: v})
			}
		}
		return dashReadMsg{gen: gen, readings: rs, err: err, at: time.Now()}
	}
}

// readCmd starts one refresh of d from its source.
func (a *App) readCmd(d *dashboard) tea.Cmd {
	if d.redfish {
		return readRedfishSensorsCmd(d.gen, d.host, a.username, a.password, d.names)
	}
	return readSensorsCmd(d.gen, d.host, a.username, a.password, d.cache, d.names)
}

func dashTick(gen int, d time.Duration) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return dashTickMsg{gen: gen} })
}

// watchKey is where host's chosen sensors are kept: Redfish sensor names
// differ from IPMI ones (e.g. "CPU1 Temp" against "Temp"), so each source
// has its own list.
func (a *App) watchKey(host string) string {
	if a.sensorsFromRedfish {
		return "redfish:" + host
	}
	return host
}

// watched returns the dashboard sensors chosen for host (current source).
func (a *App) watched(host string) []string { return a.dashSensors[a.watchKey(host)] }

func (a *App) isWatched(host, name string) bool {
	for _, n := range a.watched(host) {
		if n == name {
			return true
		}
	}
	return false
}

// toggleWatch adds or removes name from host's dashboard and saves it.
func (a *App) toggleWatch(host, name string) {
	if a.dashSensors == nil {
		a.dashSensors = map[string][]string{}
	}
	host = a.watchKey(host)
	cur := a.dashSensors[host]
	out := cur[:0:0]
	removed := false
	for _, n := range cur {
		if n == name {
			removed = true
			continue
		}
		out = append(out, n)
	}
	if !removed {
		out = append(out, name)
	}
	if len(out) == 0 {
		delete(a.dashSensors, host)
	} else {
		a.dashSensors[host] = out
	}
	saved := map[string][]string{}
	for h, ns := range a.dashSensors {
		saved[h] = append([]string(nil), ns...)
	}
	_ = config.Update(func(c *config.Config) { c.DashboardSensors = saved })
}

// openDashboard starts the dashboard for the current host's chosen sensors.
func (a *App) openDashboard() (tea.Model, tea.Cmd) {
	if len(a.results) == 0 || a.selectedHost < 0 {
		return a, nil
	}
	host := a.results[a.selectedHost].IP
	names, err := ipmi.AnalogNames(a.sensors, a.watched(host))
	if err != nil {
		if len(a.watched(host)) == 0 {
			a.status = "No sensors chosen: press Space on the sensors to watch, then D"
		} else {
			a.status = "None of the chosen sensors has a numeric reading; the dashboard shows analog sensors only"
		}
		return a, nil
	}
	cache, err := ipmi.SDRCachePath(host)
	if err != nil {
		a.status = "Dashboard: " + err.Error()
		return a, nil
	}
	d := &dashboard{host: host, redfish: a.sensorsFromRedfish, names: names, series: map[string]*dashSeries{}, cache: cache, interval: dashDefaultInterval}
	if a.dash != nil && a.dash.host == host && a.dash.redfish == d.redfish {
		d.interval = a.dash.interval
		d.gen = a.dash.gen
	}
	units := map[string]string{}
	for _, e := range a.sensors {
		if _, ok := units[e.Name]; !ok {
			units[e.Name], _ = ipmi.SensorUnit(e.Value)
		}
	}
	for _, n := range names {
		d.series[n] = &dashSeries{unit: units[n]}
	}
	d.gen++
	a.dash = d
	a.currentScreen = screenDashboard

	if d.redfish || a.sdrCached[host] {
		return a, a.readCmd(d)
	}
	a.ipmiLoading = true
	a.status = "Caching the sensor repository of " + host + " (once per session)..."
	return a, tea.Batch(a.spinner.Tick, dumpSDRCmd(d.gen, host, a.username, a.password, cache))
}

func (a *App) handleDashDump(msg dashDumpMsg) (tea.Model, tea.Cmd) {
	d := a.dash
	if d == nil || msg.gen != d.gen {
		return a, nil
	}
	a.ipmiLoading = false
	if msg.err != nil {
		a.status = "Could not cache the sensor repository: " + msg.err.Error()
		a.currentScreen = screenSensor
		return a, nil
	}
	if a.sdrCached == nil {
		a.sdrCached = map[string]bool{}
	}
	a.sdrCached[d.host] = true
	a.status = "Sensor repository cached"
	return a, a.readCmd(d)
}

func (a *App) handleDashRead(msg dashReadMsg) (tea.Model, tea.Cmd) {
	d := a.dash
	if d == nil || msg.gen != d.gen || a.currentScreen != screenDashboard {
		return a, nil
	}
	got := map[string]bool{}
	for _, r := range msg.readings {
		s := d.series[r.Name]
		if s == nil {
			continue
		}
		got[r.Name] = true
		s.values = append(s.values, r.Value)
		if len(s.values) > dashHistory {
			s.values = s.values[len(s.values)-dashHistory:]
		}
		if !s.seen || r.Value < s.min {
			s.min = r.Value
		}
		if !s.seen || r.Value > s.max {
			s.max = r.Value
		}
		s.seen = true
	}
	d.missing = d.missing[:0]
	for _, n := range d.names {
		if !got[n] {
			d.missing = append(d.missing, n)
		}
	}
	d.updated = msg.at
	d.err = ""
	if msg.err != nil {
		d.err = msg.err.Error()
	}
	return a, dashTick(d.gen, dashIntervals[d.interval])
}

func (a *App) handleDashTick(msg dashTickMsg) (tea.Model, tea.Cmd) {
	d := a.dash
	if d == nil || msg.gen != d.gen || a.currentScreen != screenDashboard {
		return a, nil
	}
	return a, a.readCmd(d)
}

func (a *App) updateDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := a.dash
	switch msg.String() {
	case "esc", "q":
		d.gen++ // orphan the pending tick
		a.currentScreen = screenSensor
	case "+", "=":
		if d.interval < len(dashIntervals)-1 {
			d.interval++
		}
	case "-", "_":
		if d.interval > 0 {
			d.interval--
		}
	case "r":
		d.gen++ // replace the pending tick with an immediate read
		return a, a.readCmd(d)
	}
	return a, nil
}

// sparkline draws values scaled between their own min and max.
func sparkline(values []float64, width int) string {
	const bars = "▁▂▃▄▅▆▇█"
	levels := []rune(bars)
	if len(values) > width {
		values = values[len(values)-width:]
	}
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, v := range values {
		lo, hi = math.Min(lo, v), math.Max(hi, v)
	}
	var b strings.Builder
	for _, v := range values {
		i := len(levels) / 2
		if hi > lo {
			i = int((v - lo) / (hi - lo) * float64(len(levels)-1))
		}
		b.WriteRune(levels[i])
	}
	return b.String()
}

func fmtReading(v float64) string {
	return ipmi.SensorReading{Value: v}.String()
}

func (a *App) renderDashboard() string {
	d := a.dash
	var b strings.Builder
	if d == nil {
		return ""
	}
	when := "waiting for first reading"
	if !d.updated.IsZero() {
		when = "updated " + d.updated.Format("15:04:05")
	}
	src := "IPMI"
	if d.redfish {
		src = "Redfish"
	}
	b.WriteString(HeaderStyle().Render(fmt.Sprintf("SENSOR DASHBOARD — %s (%s)", d.host, src)) + "\n")
	b.WriteString(fmt.Sprintf("every %v · %s\n\n", dashIntervals[d.interval], when))

	hdr := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent))
	b.WriteString("  " + hdr.Render(fmt.Sprintf("%-24s %10s %-10s %10s %10s  %s", "Sensor", "Now", "Unit", "Min", "Max", "Trend")) + "\n")
	b.WriteString("  " + strings.Repeat("─", 90) + "\n")
	for _, n := range d.names {
		s := d.series[n]
		now, lo, hi := "—", "—", "—"
		if s.seen {
			now, lo, hi = fmtReading(s.values[len(s.values)-1]), fmtReading(s.min), fmtReading(s.max)
		}
		b.WriteString(fmt.Sprintf("  %-24s %10s %-10s %10s %10s  %s\n", n, now, s.unit, lo, hi, sparkline(s.values, dashHistory)))
	}
	if len(d.missing) > 0 && !d.updated.IsZero() {
		b.WriteString("\n  No reading this time: " + strings.Join(d.missing, ", ") + "\n")
	}
	if d.err != "" {
		b.WriteString("\n  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#CB4B16")).Render(d.err) + "\n")
	}
	return b.String()
}
