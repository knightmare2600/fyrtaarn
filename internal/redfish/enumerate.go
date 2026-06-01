package redfish

import (
	"fmt"
	"strings"
)

// SystemInfo holds the full hardware picture retrieved from /redfish/v1/Systems/<id>.
type SystemInfo struct {
	Manufacturer    string
	Model           string
	SerialNumber    string
	SKU             string
	HostName        string
	BIOSVersion     string
	PowerState      string
	ProcessorCount  int
	MemoryGiB       float64
	MemorySummary   string
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
	members, _ := jsonArray(sysCols, "Members")
	for _, m := range members {
		path, _ := m["@odata.id"].(string)
		if path == "" {
			continue
		}
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
	mgrMembers, _ := jsonArray(mgrCols, "Members")
	for _, m := range mgrMembers {
		path, _ := m["@odata.id"].(string)
		if path == "" {
			continue
		}
		mgr, err := fetchJSON(c, base+path, user, pass)
		if err != nil {
			continue
		}
		out.Managers = append(out.Managers, parseManager(mgr))
	}

	return out, nil
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

	// ProcessorSummary.Count
	if ps, ok := obj["ProcessorSummary"].(map[string]any); ok {
		if c, ok := ps["Count"].(float64); ok {
			s.ProcessorCount = int(c)
		}
	}

	// MemorySummary.TotalSystemMemoryGiB
	if ms, ok := obj["MemorySummary"].(map[string]any); ok {
		if g, ok := ms["TotalSystemMemoryGiB"].(float64); ok {
			s.MemoryGiB = g
		}
		s.MemorySummary = str(ms, "Status")
	}

	return s
}

func parseManager(obj map[string]any) ManagerInfo {
	m := ManagerInfo{
		Name:            str(obj, "Name"),
		FirmwareVersion: str(obj, "FirmwareVersion"),
		UUID:            str(obj, "UUID"),
	}

	// Status.Health
	if st, ok := obj["Status"].(map[string]any); ok {
		m.Status = str(st, "Health")
	}

	return m
}

func str(obj map[string]any, key string) string {
	v, _ := obj[key].(string)
	return strings.TrimSpace(v)
}
