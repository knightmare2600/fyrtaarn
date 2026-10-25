package redfish

import "errors"

// supermicroProvider handles SuperMicro BMC virtual media.
//
// SuperMicro virtual media support varies widely by firmware generation:
//   - Older firmware: Java/HTML5 IKVM only; no programmatic API
//   - AMI MegaRAC-based firmware: Redfish VirtualMedia is available and
//     follows the standard DMTF schema (Actions["#VirtualMedia.InsertMedia"
//     / "EjectMedia"]) — the same shape genericProvider already handles for
//     iLO 4 and iDRAC, so supermicroProvider delegates to it rather than
//     duplicating the walk.
//
// The one thing genericProvider can't tell on its own is "no VirtualMedia
// resource at all" (IKVM-only firmware) vs. "found it, the call failed" —
// probeVirtualMedia distinguishes the two so IKVM-only hosts get a clear,
// specific error instead of a generic 404 buried in a Redfish-walk failure.
type supermicroProvider struct {
	host, user, pass string
}

// ErrIKVMOnly is returned when a Supermicro BMC exposes no Redfish
// VirtualMedia resource on any Manager — the firmware only supports the
// Java/HTML5 IKVM applet, which has no programmatic API and cannot be
// automated via this tool.
var ErrIKVMOnly = errors.New("this Supermicro BMC has no Redfish VirtualMedia support — firmware appears IKVM-only (Java/HTML5), which cannot be automated")

func (p *supermicroProvider) generic() *genericProvider {
	return &genericProvider{host: p.host, user: p.user, pass: p.pass}
}

func (p *supermicroProvider) Insert(isoURL string) error {
	if !p.probeVirtualMedia() {
		return ErrIKVMOnly
	}
	return p.generic().Insert(isoURL)
}

func (p *supermicroProvider) Eject() error {
	if !p.probeVirtualMedia() {
		return ErrIKVMOnly
	}
	return p.generic().Eject()
}

// probeVirtualMedia reports whether any Manager on this BMC advertises a
// VirtualMedia collection at all. It deliberately doesn't check for a
// specific action (InsertMedia/EjectMedia) or CD/DVD slot — that's
// genericProvider's job once we know it's worth trying.
func (p *supermicroProvider) probeVirtualMedia() bool {
	c := httpClient()
	base := "https://" + p.host

	mgrs, err := fetchJSON(c, base+"/redfish/v1/Managers/", p.user, p.pass)
	if err != nil {
		return false
	}

	members, _ := jsonArrayOf(mgrs, "Members")
	for _, m := range members {
		mgrPath, _ := m["@odata.id"].(string)
		if mgrPath == "" {
			continue
		}
		mgr, err := fetchJSON(c, base+mgrPath, p.user, p.pass)
		if err != nil {
			continue
		}
		if jsonOdataID(mgr, "VirtualMedia") != "" {
			return true
		}
	}

	return false
}
