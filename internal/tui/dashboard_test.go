package tui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/knightmare2600/fyrtaarn/internal/config"
	"github.com/knightmare2600/fyrtaarn/internal/discovery"
	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
	"github.com/knightmare2600/fyrtaarn/internal/session"
)

func dashApp(t *testing.T) *App {
	t.Helper()
	// toggleWatch saves to config.json: keep it out of the real one.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	return &App{
		sessionCache:  session.NewCache(),
		results:       []discovery.HostResult{{IP: "10.0.0.7"}},
		currentScreen: screenSensor,
		contentH:      30,
		sensors: []ipmi.SDREntry{
			{Name: "Inlet Temp", Value: "26 degrees C", Status: "ok"},
			{Name: "PS Redundancy", Value: "0x01", Status: "ok"},
			{Name: "Fan1", Value: "4800 RPM", Status: "ok"},
		},
	}
}

func key(s string) tea.KeyMsg {
	if s == " " {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestDashboardFlow(t *testing.T) {
	a := dashApp(t)

	// Nothing chosen yet.
	a.updateSensor(key("d"))
	if a.currentScreen != screenSensor || !strings.Contains(a.status, "No sensors chosen") {
		t.Fatalf("status %q", a.status)
	}

	// Watch Inlet Temp, PS Redundancy (discrete) and Fan1; saved to config.
	a.updateSensor(key(" "))
	a.updateSensor(tea.KeyMsg{Type: tea.KeyDown})
	a.updateSensor(key(" "))
	a.updateSensor(tea.KeyMsg{Type: tea.KeyDown})
	a.updateSensor(key(" "))
	if got := strings.Join(a.watched("10.0.0.7"), ","); got != "Inlet Temp,PS Redundancy,Fan1" {
		t.Fatalf("watched %q", got)
	}
	if got := strings.Join(config.Load().DashboardSensors["10.0.0.7"], ","); got != "Inlet Temp,PS Redundancy,Fan1" {
		t.Fatalf("saved %q", got)
	}
	if !strings.Contains(a.renderSensors(), "★ Inlet Temp") {
		t.Fatal("watched marker not rendered")
	}

	// D: first time this session, the repository is cached first.
	_, cmd := a.updateSensor(key("d"))
	if cmd == nil || a.currentScreen != screenDashboard || !a.ipmiLoading {
		t.Fatal("dashboard did not start with a repository dump")
	}
	if got := strings.Join(a.dash.names, ","); got != "Inlet Temp,Fan1" {
		t.Fatalf("dashboard sensors %q (discrete ones are left out)", got)
	}
	gen := a.dash.gen

	_, cmd = a.handleDashDump(dashDumpMsg{gen: gen})
	if cmd == nil || a.ipmiLoading || !a.sdrCached["10.0.0.7"] {
		t.Fatal("dump result did not start a read")
	}

	// Two readings: values, min/max, and a tick scheduled each time.
	for _, v := range []float64{26, 29} {
		_, cmd = a.handleDashRead(dashReadMsg{gen: gen, readings: []ipmi.SensorReading{{Name: "Inlet Temp", Value: v}}, at: time.Now()})
		if cmd == nil {
			t.Fatal("no next tick scheduled")
		}
	}
	s := a.dash.series["Inlet Temp"]
	if s.min != 26 || s.max != 29 || len(s.values) != 2 || s.unit != "degrees C" {
		t.Fatalf("series %+v", s)
	}
	out := a.renderDashboard()
	if !strings.Contains(out, "Inlet Temp") || !strings.Contains(out, "No reading this time: Fan1") {
		t.Fatalf("render:\n%s", out)
	}

	// A stale message from an earlier generation changes nothing.
	if _, cmd := a.handleDashRead(dashReadMsg{gen: gen - 1, readings: []ipmi.SensorReading{{Name: "Inlet Temp", Value: 99}}}); cmd != nil || s.max != 29 {
		t.Fatal("stale read was used")
	}

	// Interval steps, then Esc orphans the pending tick.
	a.updateDashboard(key("+"))
	if dashIntervals[a.dash.interval] != 10*time.Second {
		t.Fatalf("interval %v", dashIntervals[a.dash.interval])
	}
	a.updateDashboard(tea.KeyMsg{Type: tea.KeyEsc})
	if a.currentScreen != screenSensor {
		t.Fatal("Esc did not return to sensors")
	}
	if _, cmd := a.handleDashTick(dashTickMsg{gen: gen}); cmd != nil {
		t.Fatal("tick after Esc still read sensors")
	}

	// Reopening in the same session skips the dump.
	_, cmd = a.updateSensor(key("d"))
	if cmd == nil || a.ipmiLoading {
		t.Fatal("second open should read straight away")
	}
	if dashIntervals[a.dash.interval] != 10*time.Second {
		t.Fatal("interval not kept between openings")
	}
	if p, _ := ipmi.SDRCachePath("10.0.0.7"); !strings.HasPrefix(p, os.Getenv("XDG_CACHE_HOME")) || filepath.Base(p) != "sdr-10.0.0.7.cache" {
		t.Fatalf("cache path %s", p)
	}
}

func TestSparkline(t *testing.T) {
	if got := sparkline([]float64{1, 2, 3, 4, 5, 6, 7, 8}, 30); got != "▁▂▃▄▅▆▇█" {
		t.Fatalf("ramp %q", got)
	}
	if got := sparkline([]float64{5, 5, 5}, 30); got != "▅▅▅" {
		t.Fatalf("flat %q", got)
	}
	if got := sparkline([]float64{1, 2, 3, 4}, 2); got != "▁█" {
		t.Fatalf("width %q", got)
	}
}

// TestRedfishDashboardRead checks a Redfish refresh picks the chosen
// sensors out of the Chassis Thermal resource and keeps their numbers.
func TestRedfishDashboardRead(t *testing.T) {
	docs := map[string]string{
		"/redfish/v1/Chassis":           `{"Members":[{"@odata.id":"/redfish/v1/Chassis/1"}]}`,
		"/redfish/v1/Chassis/1":         `{"Thermal":{"@odata.id":"/redfish/v1/Chassis/1/Thermal"}}`,
		"/redfish/v1/Chassis/1/Thermal": `{"Temperatures":[{"Name":"CPU1 Temp","ReadingCelsius":42,"Status":{"Health":"OK"}},{"Name":"Inlet","ReadingCelsius":21}],"Fans":[{"Name":"Fan1","Reading":1920,"ReadingUnits":"RPM"}]}`,
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, ok := docs[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(d))
	}))
	defer srv.Close()
	msg := readRedfishSensorsCmd(3, strings.TrimPrefix(srv.URL, "https://"), "u", "p", []string{"CPU1 Temp", "Fan1"})().(dashReadMsg)
	if msg.err != nil || msg.gen != 3 || len(msg.readings) != 2 || msg.readings[0].Value != 42 || msg.readings[1].Value != 1920 {
		t.Fatalf("got %+v", msg)
	}
}
