package redfish

// supermicroProvider handles SuperMicro BMC virtual media.
//
// SuperMicro virtual media support varies widely by firmware generation:
//   - Older firmware: Java/HTML5 IKVM only; no programmatic API
//   - AMI MegaRAC-based firmware: Redfish VirtualMedia may be available
//     but path structure and auth behaviour differ from the generic walker
//
// TODO: implement supermicroProvider.Insert and supermicroProvider.Eject
// TODO: probe for AMI MegaRAC Redfish support before attempting
// TODO: surface a clear unsupported error for IKVM-only devices
type supermicroProvider struct {
	host, user, pass string
}

func (p *supermicroProvider) Insert(_ string) error { return ErrNotImplemented }
func (p *supermicroProvider) Eject() error          { return ErrNotImplemented }
