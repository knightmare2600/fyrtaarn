package redfish

// idracProvider handles Dell iDRAC virtual media.
//
// iDRAC generations differ:
//   - iDRAC 7/8: limited Redfish; virtual media via WSMAN or proprietary API
//   - iDRAC 9:   full Redfish VirtualMedia; slot path usually contains "/CD/"
//                so genericProvider accidentally works, but explicit support
//                is cleaner and handles OEM extensions (e.g. RemoteImage)
//
// TODO: implement idracProvider.Insert and idracProvider.Eject
// TODO: detect generation from product string (e.g. "iDRAC9", "iDRAC 9")
// TODO: iDRAC 7/8 — WSMAN path or surface a clear unsupported error
// TODO: iDRAC 9   — Redfish path; consider Dell OEM RemoteImage extension
//                   for large or NFS-hosted images
type idracProvider struct {
	host, user, pass string
	generation       int // 7, 8, or 9; 0 = unknown
}

func (p *idracProvider) Insert(_ string) error { return ErrNotImplemented }
func (p *idracProvider) Eject() error          { return ErrNotImplemented }
