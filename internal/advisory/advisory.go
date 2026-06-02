// Package advisory queries the NIST NVD and CISA KEV catalog for known
// vulnerabilities affecting BMC firmware. NVD lookups use the CPE 2.3
// formatted-string-binding with a wildcard version component, so all CVEs for
// the product family are returned regardless of specific firmware revision.
// Results are sorted CVSS descending; actively-exploited entries (present in
// the CISA KEV catalog) bubble to the top within each severity tier.
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
// vulnerabilities affecting the BMC described by manufacturer and productName.
// apiKey is optional; omit for unauthenticated access (5 req/30 s rate limit).
// Returns up to 15 findings. Returns a descriptive error if the manufacturer
// has no CPE mapping — callers should surface this as a non-fatal notice.
func Check(manufacturer, productName, apiKey string) ([]CVEFinding, error) {
	entry := lookupCPE(manufacturer, productName)
	if entry == nil {
		return nil, fmt.Errorf("no CPE mapping for %q — advisory lookup skipped", manufacturer)
	}

	client := newNVDClient(apiKey)
	cpe := cpeString(entry.vendor, entry.product)

	raw, err := client.queryCPE(cpe)
	if err != nil {
		return nil, fmt.Errorf("NVD query failed: %w", err)
	}

	if len(raw) == 0 {
		return nil, nil
	}

	// Annotate with CISA KEV data (best-effort — failures are silently ignored
	// inside kevContains).
	for i := range raw {
		raw[i].ActivelyExploited = kevContains(raw[i].ID)
	}

	// Sort: actively-exploited first, then by CVSS descending.
	sort.Slice(raw, func(i, j int) bool {
		if raw[i].ActivelyExploited != raw[j].ActivelyExploited {
			return raw[i].ActivelyExploited
		}
		return raw[i].CVSS > raw[j].CVSS
	})

	// Cap to 15 to keep the compliance screen readable.
	if len(raw) > 15 {
		raw = raw[:15]
	}

	return raw, nil
}
