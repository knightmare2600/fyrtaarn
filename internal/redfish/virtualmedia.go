package redfish

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// VirtualMediaProvider is the interface each vendor backend must satisfy.
// Vendor-specific files (ilo.go, drac.go, supermicro.go, …) implement this;
// genericProvider (this file) is the Redfish-standard fallback.
type VirtualMediaProvider interface {
	// Insert mounts an ISO image. isoURL must be reachable by the BMC itself
	// (HTTP/HTTPS) — the BMC fetches the image, not the client. A local
	// file path is not accepted here: the TUI layer resolves one to a URL
	// first via ServeLocalISO (see localmedia.go).
	Insert(isoURL string) error
	// Eject unmounts the currently loaded virtual media.
	Eject() error
}

// ErrNotImplemented is returned by vendor stubs pending a full backend.
var ErrNotImplemented = errors.New("virtual media: vendor backend not yet implemented")

// NewProvider returns the backend for a BMC with Redfish, by its Redfish
// manufacturer:
//
//   - Lenovo: xccProvider (EXT slots by PATCH; xcc.go);
//   - Dell: idracProvider (nfs:// and cifs:// images through Dell's OEM
//     network ISO service for iDRAC 7/8, http(s) through the generic walker
//     for iDRAC9; drac.go);
//   - Cisco: cimcProvider (IMC XML API; cimc.go);
//   - Supermicro: supermicroProvider, which probes for AMI MegaRAC's
//     standard VirtualMedia before using the generic walker;
//   - anyone else, HP iLO 4 and 5 included: the generic Redfish walker.
//
// Oracle ILOM (lom.go) is not built: its virtual media is driven over SSH.
func NewProvider(host, user, pass, manufacturer, _ string) VirtualMediaProvider {
	m := strings.ToLower(manufacturer)
	switch {
	case strings.Contains(m, "supermicro"):
		return &supermicroProvider{host: host, user: user, pass: pass}
	case strings.Contains(m, "lenovo"):
		return &xccProvider{host: host, user: user, pass: pass}
	case strings.Contains(m, "dell"):
		return &idracProvider{host: host, user: user, pass: pass}
	case strings.Contains(m, "cisco"):
		return &cimcProvider{host: host, user: user, pass: pass}
	}
	return &genericProvider{host: host, user: user, pass: pass}
}

// NonRedfishBackend is the backend for a BMC without Redfish, chosen by its
// IPMI manufacturer ID: HP (11) is an iLO 3 (every later iLO has Redfish),
// Cisco (9) a CIMC. Anyone else gets nil, and fyrtaarn's AMI media
// redirection is used.
func NonRedfishBackend(host, user, pass string, ipmiManufacturer int) VirtualMediaProvider {
	switch ipmiManufacturer {
	case 11:
		return &ilo3Provider{host: host, user: user, pass: pass}
	case 9:
		return &cimcProvider{host: host, user: user, pass: pass}
	}
	return nil
}

func httpClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		// Old BMCs may only speak TLS 1.0/1.1 or RC4; see legacytls.
		Transport: newTransport(),
	}
}

// InsertMedia mounts an ISO image, dispatching to the vendor-specific
// backend NewProvider selects for manufacturer (empty string falls through
// to the generic Redfish walker).
func InsertMedia(host, isoURL, user, pass, manufacturer string) error {
	return NewProvider(host, user, pass, manufacturer, "").Insert(isoURL)
}

// EjectMedia unmounts the current virtual media. See InsertMedia.
func EjectMedia(host, user, pass, manufacturer string) error {
	return NewProvider(host, user, pass, manufacturer, "").Eject()
}

// genericProvider is the Redfish-standard fallback used until a vendor-specific
// backend is written. It walks Managers → VirtualMedia and posts to the first
// CD/DVD slot that advertises the requested action.
//
// Slot discovery accepts a match on either "cd"/"dvd" in the @odata.id path
// (descriptive-path vendors like iDRAC) or the MediaTypes array (numeric-path
// vendors like HP iLO, where path sniffing alone would miss the slot).
type genericProvider struct {
	host, user, pass string
}

func (p *genericProvider) Insert(isoURL string) error {
	c := httpClient()
	base := "https://" + p.host

	actionURL, err := findVMActionKind(c, base, p.user, p.pass, "InsertMedia", KindForImage(isoURL))
	if err != nil {
		return fmt.Errorf("could not locate Redfish VirtualMedia InsertMedia action: %w", err)
	}

	// "Image" is the only universally required field — iLO 4 HP OEM actions
	// do not accept the standard Inserted/WriteProtected parameters.
	body, _ := json.Marshal(map[string]any{"Image": isoURL})

	return doPost(c, actionURL, p.user, p.pass, body)
}

func (p *genericProvider) Eject() error {
	c := httpClient()
	base := "https://" + p.host

	// Eject whatever is inserted, in any slot. BMCs that do not report
	// Inserted get the CD slot ejected, as before.
	slots, err := vmSlots(c, base, p.user, p.pass)
	if err != nil {
		return fmt.Errorf("could not locate Redfish VirtualMedia slots: %w", err)
	}
	ejected, reported := 0, false
	for _, sl := range slots {
		in, has := sl.res["Inserted"].(bool)
		reported = reported || has
		if !sl.ok || !in {
			continue
		}
		if t := slotAction(sl.res, "EjectMedia"); t != "" {
			if err := doPost(c, base+t, p.user, p.pass, []byte("{}")); err != nil {
				return fmt.Errorf("ejecting the %s slot: %w", sl.kind, err)
			}
			ejected++
		}
	}
	if ejected > 0 || reported {
		return nil // nothing inserted is not an error
	}

	actionURL, err := findVMAction(c, base, p.user, p.pass, "EjectMedia")
	if err != nil {
		return fmt.Errorf("could not locate Redfish VirtualMedia EjectMedia action: %w", err)
	}
	return doPost(c, actionURL, p.user, p.pass, []byte("{}"))
}

// findVMAction discovers the URL for actionName (InsertMedia or EjectMedia)
// by walking Managers → VirtualMedia collection and returning the first
// CD/DVD member that advertises the requested action.
// MediaKind is the kind of virtual-media slot an image goes into.
type MediaKind int

const (
	MediaCD        MediaKind = iota // CD/DVD slot: ISO images
	MediaRemovable                  // USB stick / floppy slot: raw disk images
)

func (k MediaKind) String() string {
	if k == MediaRemovable {
		return "USB/floppy"
	}
	return "CD/DVD"
}

// KindForImage picks the slot for an image by its file extension: raw disk
// and floppy images (.img, .ima, .vfd) go to the USB/floppy slot, anything
// else to the CD/DVD slot.
func KindForImage(name string) MediaKind {
	n := strings.ToLower(name)
	if i := strings.IndexAny(n, "?#"); i >= 0 {
		n = n[:i] // URL query or fragment
	}
	for _, ext := range []string{".img", ".ima", ".vfd"} {
		if strings.HasSuffix(n, ext) {
			return MediaRemovable
		}
	}
	return MediaCD
}

// vmSlot is one VirtualMedia resource and the kind of media it takes.
type vmSlot struct {
	path string
	res  map[string]any
	kind MediaKind
	ok   bool // kind recognised; unrecognised slots are never used
}

// vmSlots walks Managers -> VirtualMedia and classifies each slot. A slot is
// a CD slot by its path (descriptive vendors like iDRAC) or its MediaTypes
// (numeric-path vendors like HP iLO, where path sniffing would miss it);
// likewise a USB/floppy slot (iDRAC "RemovableDisk" with MediaTypes
// USBStick; iLO slot 1 with Floppy and USBStick).
func vmSlots(c *http.Client, base, user, pass string) ([]vmSlot, error) {
	mgrs, err := fetchJSON(c, base+"/redfish/v1/Managers/", user, pass)
	if err != nil {
		return nil, err
	}
	members, _ := jsonArrayOf(mgrs, "Members")
	if len(members) == 0 {
		return nil, fmt.Errorf("no Managers found")
	}
	var slots []vmSlot
	for _, m := range members {
		mgrPath, _ := m["@odata.id"].(string)
		if mgrPath == "" {
			continue
		}
		mgr, err := fetchJSON(c, base+mgrPath, user, pass)
		if err != nil {
			continue
		}
		vmLink := jsonOdataID(mgr, "VirtualMedia")
		if vmLink == "" {
			continue
		}
		vmCol, err := fetchJSON(c, base+vmLink, user, pass)
		if err != nil {
			continue
		}
		vmMembers, _ := jsonArrayOf(vmCol, "Members")
		for _, vm := range vmMembers {
			vmPath, _ := vm["@odata.id"].(string)
			if vmPath == "" {
				continue
			}
			vmRes, err := fetchJSON(c, base+vmPath, user, pass)
			if err != nil {
				continue
			}
			slot := vmSlot{path: vmPath, res: vmRes}
			lower := strings.ToLower(vmPath)
			switch {
			case strings.Contains(lower, "cd") || strings.Contains(lower, "dvd") ||
				vmHasMediaType(vmRes, "CD") || vmHasMediaType(vmRes, "DVD"):
				slot.kind, slot.ok = MediaCD, true
			case strings.Contains(lower, "removable") || strings.Contains(lower, "floppy") || strings.Contains(lower, "usb") ||
				vmHasMediaType(vmRes, "USBStick") || vmHasMediaType(vmRes, "Floppy"):
				slot.kind, slot.ok = MediaRemovable, true
			}
			slots = append(slots, slot)
		}
	}
	return slots, nil
}

// slotAction returns the target of actionName ("InsertMedia"/"EjectMedia")
// on a slot: the standard action, or HP iLO's OEM one (Oem.Hp/Oem.Hpe
// .Actions, named "InsertVirtualMedia"/"EjectVirtualMedia" on iLO 4).
func slotAction(res map[string]any, actionName string) string {
	if target := vmActionTarget(res["Actions"], actionName); target != "" {
		return target
	}
	oemName := strings.ReplaceAll(actionName, "Media", "VirtualMedia")
	if oem, ok := res["Oem"].(map[string]any); ok {
		for _, ns := range []string{"Hp", "Hpe"} {
			if vendor, ok := oem[ns].(map[string]any); ok {
				if t := vmActionTarget(vendor["Actions"], actionName); t != "" {
					return t
				}
				if t := vmActionTarget(vendor["Actions"], oemName); t != "" {
					return t
				}
			}
		}
	}
	return ""
}

// findVMAction returns the URL of actionName on the first CD/DVD slot.
func findVMAction(c *http.Client, base, user, pass, actionName string) (string, error) {
	return findVMActionKind(c, base, user, pass, actionName, MediaCD)
}

// findVMActionKind returns the URL of actionName on the first slot of kind.
func findVMActionKind(c *http.Client, base, user, pass, actionName string, kind MediaKind) (string, error) {
	slots, err := vmSlots(c, base, user, pass)
	if err != nil {
		return "", err
	}
	for _, sl := range slots {
		if sl.ok && sl.kind == kind {
			if t := slotAction(sl.res, actionName); t != "" {
				return base + t, nil
			}
		}
	}
	return "", fmt.Errorf("no %s VirtualMedia slot with a %s action on any Manager", kind, actionName)
}

// vmActionTarget scans an Actions map (or Oem.Vendor.Actions map) and returns
// the target URL of the first action whose key contains name (substring match).
func vmActionTarget(raw any, name string) string {
	m, _ := raw.(map[string]any)
	for k, v := range m {
		if strings.Contains(k, name) {
			if av, ok := v.(map[string]any); ok {
				if target, _ := av["target"].(string); target != "" {
					return target
				}
			}
		}
	}
	return ""
}

func doPost(c *http.Client, url, user, pass string, body []byte) error {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OData-Version", "4.0")
	if user != "" {
		req.SetBasicAuth(user, pass)
	}

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("Redfish returned HTTP %d", resp.StatusCode)
	}

	return nil
}

func fetchJSON(c *http.Client, url, user, pass string) (map[string]any, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	// OData-Version and Accept are required by strict Redfish implementations
	// (iLO 5/6 in particular) and harmless on permissive ones.
	req.Header.Set("OData-Version", "4.0")
	req.Header.Set("Accept", "application/json")
	if user != "" {
		req.SetBasicAuth(user, pass)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	return out, nil
}

// vmHasMediaType reports whether a VirtualMedia resource's MediaTypes array
// contains the given type (case-insensitive). Used to identify CD/DVD slots
// on vendors like HP iLO that use numeric paths rather than descriptive ones.
func vmHasMediaType(obj map[string]any, mediaType string) bool {
	raw, _ := obj["MediaTypes"].([]any)
	for _, v := range raw {
		if s, ok := v.(string); ok && strings.EqualFold(s, mediaType) {
			return true
		}
	}
	return false
}

func jsonArrayOf(obj map[string]any, key string) ([]map[string]any, bool) {
	raw, ok := obj[key].([]any)
	if !ok {
		return nil, false
	}
	var out []map[string]any
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, true
}

func jsonOdataID(obj map[string]any, key string) string {
	nested, _ := obj[key].(map[string]any)
	if nested == nil {
		return ""
	}
	id, _ := nested["@odata.id"].(string)
	return id
}
