package redfish

// iloProvider handles HP iLO virtual media.
//
// iLO versions differ significantly:
//   - iLO 3/4: RIBCL XML over HTTPS; no standard Redfish VirtualMedia
//   - iLO 5:   Redfish VirtualMedia present; slot paths are numeric
//              (e.g. /redfish/v1/Managers/1/VirtualMedia/2/) — path sniffing
//              won't find them; must filter by MediaTypes array
//   - iLO 6:   Redfish VirtualMedia with session-token auth preferred over
//              Basic Auth; large ISOs may require chunked transfer
//
// TODO: implement iloProvider.Insert and iloProvider.Eject
// TODO: detect generation from product string (e.g. "iLO 5", "iLO 6")
// TODO: iLO 3/4 — RIBCL XML path (or surface a clear unsupported error)
// TODO: iLO 5/6 — Redfish path using MediaTypes-based slot discovery
// TODO: iLO 5/6 — prefer session token auth (POST /redfish/v1/SessionService/Sessions)
type iloProvider struct {
	host, user, pass string
	generation       int // 3, 4, 5, or 6; 0 = unknown
}

func (p *iloProvider) Insert(_ string) error { return ErrNotImplemented }
func (p *iloProvider) Eject() error          { return ErrNotImplemented }
