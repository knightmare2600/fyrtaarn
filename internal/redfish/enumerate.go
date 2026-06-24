package redfish

import (
	"fmt"
	"strings"
)

// SystemInfo holds the full hardware picture retrieved from /redfish/v1/Systems/<id>.
type SystemInfo struct {
	Manufacturer   string
	Model          string
	SerialNumber   string
	SKU            string
	HostName       string
	BIOSVersion    string
	PowerState     string
	Health         string // Status.Health
	ProcessorCount int
	ProcessorModel string // ProcessorSummary.Model
	MemoryGiB      float64
	MemoryHealth   string // MemorySummary.Status.HealthRollup
}

// ManagerInfo holds BMC/manager details from /redfish/v1/Managers/<id>.
type ManagerInfo struct {
	Name            string
	FirmwareVersion string
	Status          string
	UUID            string
}

// FullEnumeration is the complete picture returned by EnumerateFull.
type FullEnumeration struct {
	Systems  []SystemInfo
	Managers []ManagerInfo
}

// EnumerateFull performs an authenticated walk of the Redfish service root,
// populating Systems and Managers collections.
func EnumerateFull(host, user, pass string) (*FullEnumeration, error) {
	c := httpClient()
	base := "https://" + host

	out := &FullEnumeration{}

	// --- Systems ---
	sysCols, err := fetchJSON(c, base+"/redfish/v1/Systems/", user, pass)
	if err != nil {
		return nil, fmt.Errorf("cannot fetch Systems collection: %w", err)
	}
	for _, path := range collectionPaths(sysCols) {
		sys, err := fetchJSON(c, base+path, user, pass)
		if err != nil {
			continue
		}
		out.Systems = append(out.Systems, parseSystem(sys))
	}

	// --- Managers ---
	mgrCols, err := fetchJSON(c, base+"/redfish/v1/Managers/", user, pass)
	if err != nil {
		// Non-fatal — not all BMCs expose Managers.
		return out, nil
	}
	for _, path := range collectionPaths(mgrCols) {
		mgr, err := fetchJSON(c, base+path, user, pass)
		if err != nil {
			continue
		}
		out.Managers = append(out.Managers, parseManager(mgr))
	}

	return out, nil
}

// collectionPaths returns member paths from a Redfish collection response.
// Standard Redfish uses Members[*]["@odata.id"]; HP iLO 4 also provides
// links.Member[*]["href"] — we check both and deduplicate.
func collectionPaths(col map[string]any) []string {
	seen := map[string]bool{}
	var paths []string

	add := func(p string) {
		if p != "" && !seen[p] {
			seen[p] = true
			paths = append(paths, p)
		}
	}

	// Standard: Members[*]["@odata.id"]
	members, _ := jsonArrayOf(col, "Members")
	for _, m := range members {
		add(str(m, "@odata.id"))
	}

	// iLO 4 fallback: links.Member[*]["href"]
	if links, ok := col["links"].(map[string]any); ok {
		linked, _ := jsonArrayOf(links, "Member")
		for _, m := range linked {
			add(str(m, "href"))
		}
	}

	return paths
}

func parseSystem(obj map[string]any) SystemInfo {
	s := SystemInfo{
		Manufacturer: str(obj, "Manufacturer"),
		Model:        str(obj, "Model"),
		SerialNumber: str(obj, "SerialNumber"),
		SKU:          str(obj, "SKU"),
		HostName:     str(obj, "HostName"),
		BIOSVersion:  str(obj, "BiosVersion"),
		PowerState:   str(obj, "PowerState"),
	}

	// Status.Health
	if st, ok := obj["Status"].(map[string]any); ok {
		s.Health = str(st, "Health")
	}

	// ProcessorSummary.Count + Model
	if ps, ok := obj["ProcessorSummary"].(map[string]any); ok {
		if c, ok := ps["Count"].(float64); ok {
			s.ProcessorCount = int(c)
		}
		s.ProcessorModel = str(ps, "Model")
	}

	// MemorySummary.TotalSystemMemoryGiB + Status.HealthRollup
	if ms, ok := obj["MemorySummary"].(map[string]any); ok {
		if g, ok := ms["TotalSystemMemoryGiB"].(float64); ok {
			s.MemoryGiB = g
		}
		// Status is a nested object — must drill in.
		// Standard Redfish uses "HealthRollup"; iLO 4 spells it "HealthRollUp".
		if st, ok := ms["Status"].(map[string]any); ok {
			s.MemoryHealth = str(st, "HealthRollup")
			if s.MemoryHealth == "" {
				s.MemoryHealth = str(st, "HealthRollUp")
			}
			if s.MemoryHealth == "" {
				s.MemoryHealth = str(st, "Health")
			}
		}
	}

	return s
}

func parseManager(obj map[string]any) ManagerInfo {
	m := ManagerInfo{
		Name:            str(obj, "Name"),
		FirmwareVersion: str(obj, "FirmwareVersion"),
		UUID:            str(obj, "UUID"),
	}

	// Status: try Health first (standard Redfish), fall back to State.
	// iLO returns {"State": "Enabled"} with no Health key at root level.
	if st, ok := obj["Status"].(map[string]any); ok {
		m.Status = str(st, "Health")
		if m.Status == "" {
			m.Status = str(st, "State")
		}
	}

	// If Name is the generic placeholder, extract a better name from
	// FirmwareVersion. e.g. "iLO 5 v1.40" → "iLO 5".
	if m.Name == "Manager" || m.Name == "BMC" || m.Name == "" {
		if idx := strings.Index(m.FirmwareVersion, " v"); idx > 0 {
			m.Name = m.FirmwareVersion[:idx]
		}
	}

	return m
}

func str(obj map[string]any, key string) string {
	v, _ := obj[key].(string)
	return strings.TrimSpace(v)
}
