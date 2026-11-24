package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/knightmare2600/fyrtaarn/internal/discovery"
	"github.com/knightmare2600/fyrtaarn/internal/quirks"
	"github.com/knightmare2600/fyrtaarn/internal/session"
)

// A BMC without Redfish gets the iLO 3 or CIMC backend by its IPMI
// manufacturer, and AMI media redirection otherwise.
func TestVMBackendChoice(t *testing.T) {
	quirks.Reset()
	t.Cleanup(quirks.Reset)
	a := &App{sessionCache: session.NewCache(), width: 80}
	quirks.Observe("10.0.0.3", quirks.Identity{ManufacturerID: 11})
	quirks.Observe("10.0.0.9", quirks.Identity{ManufacturerID: 9})
	quirks.Observe("10.0.0.4", quirks.Identity{ManufacturerID: quirks.MfrQuanta})
	for ip, want := range map[string]string{"10.0.0.3": "iLO 3 (RIBCL)", "10.0.0.9": "Cisco IMC (XML API)", "10.0.0.4": "AMI MegaRAC"} {
		d := ansi.Strip(a.vmDialogFor(discovery.HostResult{IP: ip}).Render(80, 40))
		if !strings.Contains(d, want) {
			t.Errorf("%s: dialog does not name %q:\n%s", ip, want, d)
		}
	}
	// With Redfish, the Redfish dialog whatever the IPMI manufacturer.
	if d := ansi.Strip(a.vmDialogFor(discovery.HostResult{IP: "10.0.0.3", HasRedfish: true}).Render(80, 40)); !strings.Contains(d, "via Redfish") {
		t.Errorf("Redfish host:\n%s", d)
	}
	for in, want := range map[string]bool{"http://x/y.iso": true, "NFS://srv/e/y.iso": true, "smb://s/sh/y.iso": true, "~/isos/y.iso": false, "/tmp/y.iso": false} {
		if isImageURL(in) != want {
			t.Errorf("isImageURL(%q) != %v", in, want)
		}
	}
}
