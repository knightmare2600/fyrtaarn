package redfish

// lomProvider handles Oracle/Sun ILOM (Integrated Lights Out Manager) virtual media.
//
// Oracle ILOM virtual media is exposed via its own CLI/web interface and,
// on newer systems, partially via Redfish. The API surface changed significantly
// between ILOM 3.x (Sun-era) and ILOM 4.x (Oracle-era).
//
// TODO: implement lomProvider.Insert and lomProvider.Eject
// TODO: detect ILOM version from firmware string
// TODO: ILOM 3.x — proprietary REST/CLI path
// TODO: ILOM 4.x — check extent of Redfish VirtualMedia support
type lomProvider struct {
	host, user, pass string
}

func (p *lomProvider) Insert(_ string) error { return ErrNotImplemented }
func (p *lomProvider) Eject() error          { return ErrNotImplemented }
