package redfish

// xccProvider handles Lenovo XClarity Controller virtual media.
//
// XCC supports standard Redfish VirtualMedia on current firmware.
// The genericProvider Redfish walker likely works already, but explicit
// support allows handling of XCC session management and any firmware
// version quirks cleanly.
//
// TODO: implement xccProvider.Insert and xccProvider.Eject
// TODO: verify MediaTypes-based slot discovery works on XCC
// TODO: check whether XCC enforces session-token auth over Basic Auth
type xccProvider struct {
	host, user, pass string
}

func (p *xccProvider) Insert(_ string) error { return ErrNotImplemented }
func (p *xccProvider) Eject() error          { return ErrNotImplemented }
