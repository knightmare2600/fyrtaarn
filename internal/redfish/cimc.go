package redfish

// cimcProvider handles Cisco Integrated Management Controller virtual media.
//
// Cisco CIMC exposes virtual media via its own proprietary XML API (XMLAPI 2.0)
// rather than standard Redfish. There is no Redfish VirtualMedia surface on
// current CIMC versions.
//
// TODO: implement cimcProvider.Insert and cimcProvider.Eject
// TODO: CIMC XML API 2.0 — lsbootVMedia / set virtual media mapping
// TODO: surface clear unsupported error until implemented
type cimcProvider struct {
	host, user, pass string
}

func (p *cimcProvider) Insert(_ string) error { return ErrNotImplemented }
func (p *cimcProvider) Eject() error          { return ErrNotImplemented }
