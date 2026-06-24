package redfish

import (
	"bytes"
	"crypto/tls"
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
	// (HTTP/HTTPS) — the BMC fetches the image, not the client.
	Insert(isoURL string) error
	// Eject unmounts the currently loaded virtual media.
	Eject() error
}

// ErrNotImplemented is returned by vendor stubs pending a full backend.
var ErrNotImplemented = errors.New("virtual media: vendor backend not yet implemented")

// NewProvider returns the vendor-specific VirtualMediaProvider for the given
// manufacturer and product strings (as returned by Redfish or ipmitool mc info).
// Falls back to the generic Redfish walker when no specific vendor match exists.
//
// iLO 4 (HP/HPE) is handled correctly by the generic provider: it walks
// Managers → VirtualMedia, identifies the CD/DVD slot via MediaTypes, and
// finds the HP OEM action URLs under Oem.Hp.Actions. No separate iloProvider
// backend is needed for iLO 4.
//
// TODO: dispatch to vendor backends for cases the generic provider cannot handle:
//
//	"hp" / "hpe"  (iLO 3)  → ilo.go  (RIBCL XML path, no Redfish)
//	"hp" / "hpe"  (iLO 5+) → ilo.go  (session-token auth, chunked transfer on iLO 6)
//	"dell"                  → drac.go (iDRAC 7/8 WSMAN; iDRAC 9 Redfish + Dell OEM)
//	"supermicro"            → supermicro.go
//	"oracle" / "sun"        → lom.go
//	"lenovo"                → xcc.go
//	"cisco"                 → cimc.go
func NewProvider(host, user, pass, manufacturer, _ string) VirtualMediaProvider {
	return &genericProvider{host: host, user: user, pass: pass}
}

func httpClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
}

// InsertMedia mounts an ISO image. It delegates to NewProvider, which
// currently falls through to the generic Redfish walker for all vendors.
// Once vendor backends are implemented, callers should pass manufacturer and
// product so the correct backend is selected.
func InsertMedia(host, isoURL, user, pass string) error {
	return NewProvider(host, user, pass, "", "").Insert(isoURL)
}

// EjectMedia unmounts the current virtual media. See InsertMedia.
func EjectMedia(host, user, pass string) error {
	return NewProvider(host, user, pass, "", "").Eject()
}

// genericProvider is the Redfish-standard fallback used until a vendor-specific
// backend is written. It walks Managers → VirtualMedia and posts to the first
// CD/DVD slot that advertises the requested action.
//
// Known limitation: slot discovery filters by "cd"/"dvd" in the @odata.id path,
// which misses HP iLO (numeric paths) and any vendor that uses non-descriptive
// paths. Fix: check the MediaTypes array in each VirtualMedia resource instead.
// TODO: replace path sniffing with MediaTypes-based slot selection.
type genericProvider struct {
	host, user, pass string
}

func (p *genericProvider) Insert(isoURL string) error {
	c := httpClient()
	base := "https://" + p.host

	actionURL, err := findVMAction(c, base, p.user, p.pass, "InsertMedia")
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

	actionURL, err := findVMAction(c, base, p.user, p.pass, "EjectMedia")
	if err != nil {
		return fmt.Errorf("could not locate Redfish VirtualMedia EjectMedia action: %w", err)
	}

	return doPost(c, actionURL, p.user, p.pass, []byte("{}"))
}

// findVMAction discovers the URL for actionName (InsertMedia or EjectMedia)
// by walking Managers → VirtualMedia collection and returning the first
// CD/DVD member that advertises the requested action.
func findVMAction(c *http.Client, base, user, pass, actionName string) (string, error) {
	// Fetch manager list.
	mgrs, err := fetchJSON(c, base+"/redfish/v1/Managers/", user, pass)
	if err != nil {
		return "", err
	}

	members, _ := jsonArrayOf(mgrs, "Members")
	if len(members) == 0 {
		return "", fmt.Errorf("no Managers found")
	}

	// Walk each manager looking for a VirtualMedia collection.
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

			// Accept CD/DVD slots identified by either the path (descriptive
			// vendors like iDRAC) or the MediaTypes array (numeric-path vendors
			// like HP iLO where path sniffing would miss the slot entirely).
			lower := strings.ToLower(vmPath)
			pathMatch := strings.Contains(lower, "cd") || strings.Contains(lower, "dvd")
			typeMatch := vmHasMediaType(vmRes, "CD") || vmHasMediaType(vmRes, "DVD")
			if !pathMatch && !typeMatch {
				continue
			}

			// 1. Standard Redfish: Actions["#VirtualMedia.InsertMedia"].
			if target := vmActionTarget(vmRes["Actions"], actionName); target != "" {
				return base + target, nil
			}

			// 2. HP iLO 4+ OEM: Oem.Hp.Actions / Oem.Hpe.Actions.
			// iLO uses "InsertVirtualMedia"/"EjectVirtualMedia" rather than
			// the standard "InsertMedia"/"EjectMedia".
			oemName := strings.ReplaceAll(actionName, "Media", "VirtualMedia")
			if oem, ok := vmRes["Oem"].(map[string]any); ok {
				for _, ns := range []string{"Hp", "Hpe"} {
					if vendor, ok := oem[ns].(map[string]any); ok {
						if t := vmActionTarget(vendor["Actions"], actionName); t != "" {
							return base + t, nil
						}
						if t := vmActionTarget(vendor["Actions"], oemName); t != "" {
							return base + t, nil
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("VirtualMedia %s action not found on any Manager", actionName)
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
