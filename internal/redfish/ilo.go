package redfish

// iloProvider handles HP iLO virtual media.
//
// # iLO 4 (confirmed from live ProLiant ML310e Gen8 v2, FW 2.54)
//
// Redfish IS present but uses an older schema with HP-specific OEM extensions:
//
//   - Service root at /redfish/v1/ returns both "@odata.id" Members (standard)
//     and "links.Member[].href" (HP legacy) — collectionPaths() handles both.
//   - FirmwareVersion: "iLO 4 v2.54" (standard field populated correctly).
//   - Manager Status: only {"State": "Enabled"} — no Health key at root level.
//   - System fields present: Manufacturer, Model, SerialNumber, SKU, HostName,
//     BiosVersion, PowerState, ProcessorSummary, MemorySummary. String fields
//     may have trailing whitespace — str() trims these.
//   - MemorySummary.Status uses "HealthRollUp" (capital U) not "HealthRollup".
//   - VirtualMedia: two numeric slots under /redfish/v1/Managers/1/VirtualMedia/
//       Slot 1: MediaTypes = ["Floppy", "USBStick"]
//       Slot 2: MediaTypes = ["CD", "DVD"]  ← the ISO mount target
//   - VirtualMedia actions are NOT at the standard Actions map. They are under
//     Oem.Hp.Actions with HP-specific names:
//       #HpiLOVirtualMedia.InsertVirtualMedia → target URL
//       #HpiLOVirtualMedia.EjectVirtualMedia  → target URL
//     Insert body: {"Image": "http://...iso"} — Intent and Signature are
//     optional; Inserted/WriteProtected are not accepted by the HP OEM endpoint.
//   - Auth: HTTP Basic Auth works. Session-token auth not required on iLO 4.
//
// The generic provider (virtualmedia.go) handles all of the above correctly
// after the iLO 4 fixes in enumerate.go and virtualmedia.go. An explicit
// iloProvider backend is therefore not needed for iLO 4.
//
// # iLO 3
//
// No Redfish. Virtual media via RIBCL XML over HTTPS only.
// TODO: implement RIBCL path or surface a clean "not supported" error.
//
// # iLO 5 / iLO 6
//
// Standard Redfish VirtualMedia with the same numeric slot paths as iLO 4.
// Session-token auth (POST /redfish/v1/SessionService/Sessions/) is preferred
// over Basic Auth on iLO 5+; large ISOs may require chunked transfer on iLO 6.
// TODO: session-token auth path once tested against real iLO 5/6 hardware.
type iloProvider struct {
	host, user, pass string
	generation       int // 3, 4, 5, or 6; 0 = unknown
}

func (p *iloProvider) Insert(_ string) error { return ErrNotImplemented }
func (p *iloProvider) Eject() error          { return ErrNotImplemented }
