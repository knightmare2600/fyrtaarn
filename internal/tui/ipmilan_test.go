package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/knightmare2600/fyrtaarn/internal/discovery"
	"github.com/knightmare2600/fyrtaarn/internal/redfish"
	"github.com/knightmare2600/fyrtaarn/internal/session"
)

func ipmiLanApp(redfishHost bool) *App {
	return &App{
		sessionCache: session.NewCache(),
		results:      []discovery.HostResult{{IP: "10.0.0.5", HasRedfish: redfishHost}},
		username:     "admin",
		password:     "pw",
	}
}

var rmcpErr = errors.New("ipmitool: Error: " + rmcpFailMarker)

func TestConnectFailureChecksIPMIOverLAN(t *testing.T) {
	a := ipmiLanApp(true)
	_, cmd := a.Update(mcInfoMsg{Err: rmcpErr})
	if cmd == nil || !a.ipmiLoading || !strings.Contains(a.status, "checking over Redfish") {
		t.Fatalf("no Redfish check started: status %q", a.status)
	}

	// IPMI over LAN off: dialog, Cancel focused, Enable offered.
	a.handleIPMILanMsg(ipmiLanMsg{Host: "10.0.0.5", State: redfish.IPMIOverLAN{Enabled: false, Port: 623}, FromConnect: true, ConnectErr: rmcpErr})
	d := a.activeDialog
	if d == nil || d.buttons[d.focus].Action != "cancel" || d.buttons[0].Action != "ipmilan-on" || !a.ipmiLanFromConnect {
		t.Fatalf("dialog %+v", d)
	}
	out := ansi.Strip(d.Render(80, 40))
	if !strings.Contains(out, "IPMI over LAN is switched off") || !strings.Contains(out, "CVE-2013-4786") {
		t.Fatalf("dialog text:\n%s", out)
	}

	// Enabling after a failed connect retries the connect.
	a.activeDialog = nil
	_, cmd = a.handleIPMILanSetMsg(ipmiLanSetMsg{Host: "10.0.0.5", On: true, FromConnect: true})
	if cmd == nil || !strings.Contains(a.status, "now on") || !strings.Contains(a.status, "connecting") {
		t.Fatalf("no reconnect: status %q", a.status)
	}
}

func TestConnectFailureIPMIOverLANAlreadyOn(t *testing.T) {
	a := ipmiLanApp(true)
	a.handleIPMILanMsg(ipmiLanMsg{Host: "10.0.0.5", State: redfish.IPMIOverLAN{Enabled: true}, FromConnect: true, ConnectErr: rmcpErr})
	if a.activeDialog != nil || !strings.Contains(a.status, rmcpFailMarker) || !strings.Contains(a.status, "check the credentials") {
		t.Fatalf("status %q dialog %v", a.status, a.activeDialog)
	}
	// Redfish check itself failed (e.g. wrong password): original error.
	a.handleIPMILanMsg(ipmiLanMsg{Host: "10.0.0.5", Err: errors.New("HTTP 401"), FromConnect: true, ConnectErr: rmcpErr})
	if a.status != rmcpErr.Error() {
		t.Fatalf("status %q", a.status)
	}
}

func TestConnectFailureWithoutRedfish(t *testing.T) {
	a := ipmiLanApp(false)
	_, cmd := a.Update(mcInfoMsg{Err: rmcpErr})
	if cmd != nil || a.ipmiLoading || a.status != rmcpErr.Error() {
		t.Fatalf("cmd %v status %q", cmd != nil, a.status)
	}
}

func TestIPMILanMenu(t *testing.T) {
	a := ipmiLanApp(true)
	_, cmd := a.handleMenuAction("ipmi-lan")
	if cmd == nil || !a.ipmiLoading {
		t.Fatalf("menu did not start a check: %q", a.status)
	}
	a = ipmiLanApp(false)
	if _, cmd := a.handleMenuAction("ipmi-lan"); cmd != nil || !strings.Contains(a.status, "Redfish") {
		t.Fatalf("non-Redfish host: %q", a.status)
	}
	// Menu path, IPMI on: dialog offers Disable.
	a = ipmiLanApp(true)
	a.handleIPMILanMsg(ipmiLanMsg{Host: "10.0.0.5", State: redfish.IPMIOverLAN{Enabled: true}})
	if a.activeDialog == nil || a.activeDialog.buttons[0].Action != "ipmilan-off" || a.ipmiLanFromConnect {
		t.Fatalf("dialog %+v", a.activeDialog)
	}
}
