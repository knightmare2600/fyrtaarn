package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/knightmare2600/fyrtaarn/internal/discovery"
	"github.com/knightmare2600/fyrtaarn/internal/quirks"
	"github.com/knightmare2600/fyrtaarn/internal/redfish"
)

// Virtual media backends for BMCs without Redfish. A BMC with Redfish goes
// through redfish.InsertMedia, which picks its vendor backend by Redfish
// manufacturer. One without Redfish is an iLO 3 or a CIMC when its IPMI
// manufacturer ID says so (recorded at login, internal/quirks), and
// otherwise gets AMI media redirection, as before.

// nonRedfishVM returns the vendor backend for a BMC without Redfish, with a
// name for the dialog, or nil for AMI media redirection.
func (a *App) nonRedfishVM(h discovery.HostResult) (redfish.VirtualMediaProvider, string) {
	if h.HasRedfish {
		return nil, ""
	}
	mfr := quirks.IdentityOf(h.IP).ManufacturerID
	p := redfish.NonRedfishBackend(h.IP, a.username, a.password, mfr)
	switch mfr {
	case 11:
		return p, "iLO 3 (RIBCL)"
	case 9:
		return p, "Cisco IMC (XML API)"
	}
	return nil, ""
}

// vmDialogFor builds the Virtual Media dialog for host.
func (a *App) vmDialogFor(h discovery.HostResult) *Dialog {
	if _, name := a.nonRedfishVM(h); name != "" {
		return NewVendorVirtualMediaDialog(name)
	}
	return NewVirtualMediaDialog(h.HasRedfish)
}

// NewVendorVirtualMediaDialog is the dialog for an iLO 3 or a CIMC: the
// BMC fetches the image by URL, as over Redfish, and a local file is served
// to it from here over HTTP.
func NewVendorVirtualMediaDialog(name string) *Dialog {
	d := NewVirtualMediaDialog(true)
	d.Body = "This BMC has no Redfish; fyrtaarn uses its own\n" +
		"interface: " + name + ".\n" +
		"Enter an HTTP(S) URL the BMC can reach, or a local\n" +
		"file path, served to the BMC from this machine for\n" +
		"as long as the image stays mounted.\n" +
		"Eject removes whatever is mounted.\n\n" +
		"Not yet checked on this kind of BMC: built from the\n" +
		"vendor's documentation."
	return d
}

// isImageURL reports whether input is a URL for the BMC to fetch rather
// than a local path: http(s), or an NFS/CIFS share.
func isImageURL(input string) bool {
	l := strings.ToLower(input)
	for _, s := range []string{"http://", "https://", "nfs://", "cifs://", "smb://"} {
		if strings.HasPrefix(l, s) {
			return true
		}
	}
	return false
}

func runProviderAction(p redfish.VirtualMediaProvider, isoURL, action string) tea.Cmd {
	return func() tea.Msg {
		var err error
		if action == "mount" {
			err = p.Insert(isoURL)
		} else {
			err = p.Eject()
		}
		return vmMsg{Action: action, Err: err}
	}
}
