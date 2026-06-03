// Package advisory queries the NIST NVD and CISA KEV catalog for known
// vulnerabilities affecting BMC firmware. When a firmware version is supplied,
// a version-specific CPE is tried first against the primary vendor entry; if
// NVD returns no results the query falls back to querying all CPE entries for
// the vendor with a wildcard version, deduplicating across entries, so nothing
// is silently missed. Results are sorted CVSS descending; actively-exploited
// entries (present in the CISA KEV catalog) bubble to the top.
package advisory

import (
	"fmt"
	"sort"
)

// CVEFinding is a single vulnerability entry returned by Check.
type CVEFinding struct {
	ID                string
	Description       string
	CVSS              float64 // 0–10; 0 means not scored
	Severity          string  // CRITICAL / HIGH / MEDIUM / LOW / NONE / UNKNOWN
	ActivelyExploited bool    // true if present in the CISA KEV catalog
	Published         string  // RFC3339 date string from NVD
}

// Check queries NVD (and CISA KEV for active-exploitation tagging) for known
// vulnerabilities affecting the described BMC.
//
// firmwareVersion is the revision string from ipmitool mc info (e.g. "2.78").
// When non-empty a version-specific CPE is tried first using the primary CPE
// entry; if that returns no results all CPE entries for the vendor are queried
// with a wildcard version and deduplicated.
//
// Returns (findings, versionSpecific, error). versionSpecific is true when
// the results were narrowed to the exact firmware version.
//
// apiKey is optional; omit for unauthenticated access (5 req/30 s rate limit).
func Check(manufacturer, productName, firmwareVersion, apiKey string) ([]CVEFinding, bool, error) {
	entries := lookupCPEs(manufacturer, productName)
	if len(entries) == 0 {
		return nil, false, fmt.Errorf("no CPE mapping for %q — advisory lookup skipped", manufacturer)
	}

	client := newNVDClient(apiKey)

	// Attempt a version-specific query against the primary entry first.
	if ver := normalizeVersion(firmwareVersion); ver != "" {
		cpe := cpeString(entries[0].vendor, entries[0].product, ver)
		raw, err := client.queryCPE(cpe)
		if err != nil {
			return nil, false, fmt.Errorf("NVD query failed: %w", err)
		}
		if len(raw) > 0 {
			return postProcess(raw), true, nil
		}
		// Zero results — version string may not match NVD's format; fall through.
	}

	// Wildcard pass: query all CPE entries for the vendor and deduplicate.
	seen := make(map[string]bool)
	var merged []CVEFinding
	for _, entry := range entries {
		cpe := cpeString(entry.vendor, entry.product, "*")
		raw, err := client.queryCPE(cpe)
		if err != nil {
			// Best-effort for secondary entries; propagate errors from the primary.
			if entry == entries[0] {
				return nil, false, fmt.Errorf("NVD query failed: %w", err)
			}
			continue
		}
		for _, f := range raw {
			if !seen[f.ID] {
				seen[f.ID] = true
				merged = append(merged, f)
			}
		}
	}

	return postProcess(merged), false, nil
}

// postProcess annotates findings with KEV data, sorts, and caps the slice.
func postProcess(raw []CVEFinding) []CVEFinding {
	for i := range raw {
		raw[i].ActivelyExploited = kevContains(raw[i].ID)
	}
	sort.Slice(raw, func(i, j int) bool {
		if raw[i].ActivelyExploited != raw[j].ActivelyExploited {
			return raw[i].ActivelyExploited
		}
		return raw[i].CVSS > raw[j].CVSS
	})
	if len(raw) > 15 {
		raw = raw[:15]
	}
	return raw
}
