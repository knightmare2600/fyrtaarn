package advisory

import (
	"strings"
)

// cpeEntry is a single CPE vendor+product pair to query against NVD.
type cpeEntry struct {
	vendor  string
	product string
}

// lookupCPEs returns all CPE entries to query for the given manufacturer and
// product strings as reported by ipmitool / Redfish. Multiple entries are
// returned for vendors whose NVD coverage is fragmented across product names.
// Returns nil if the manufacturer is not in the table.
func lookupCPEs(manufacturer, productName string) []cpeEntry {
	mfr := strings.ToLower(strings.TrimSpace(manufacturer))
	prod := strings.ToLower(strings.TrimSpace(productName))

	switch {
	case strings.Contains(mfr, "hp") || strings.Contains(mfr, "hewlett") || strings.Contains(mfr, "hpe"):
		switch {
		case strings.Contains(prod, "ilo 6") || strings.Contains(prod, "ilo6"):
			return []cpeEntry{{"hp", "integrated_lights-out_6_firmware"}}
		case strings.Contains(prod, "ilo 5") || strings.Contains(prod, "ilo5"):
			return []cpeEntry{{"hp", "integrated_lights-out_5_firmware"}}
		case strings.Contains(prod, "ilo 4") || strings.Contains(prod, "ilo4"):
			return []cpeEntry{{"hp", "integrated_lights-out_4_firmware"}}
		case strings.Contains(prod, "ilo 3") || strings.Contains(prod, "ilo3"):
			return []cpeEntry{{"hp", "integrated_lights-out_3_firmware"}}
		default:
			return []cpeEntry{{"hp", "integrated_lights-out_firmware"}}
		}

	case strings.Contains(mfr, "dell"):
		switch {
		case strings.Contains(prod, "idrac9") || strings.Contains(prod, "idrac 9"):
			return []cpeEntry{{"dell", "idrac9_firmware"}}
		case strings.Contains(prod, "idrac8") || strings.Contains(prod, "idrac 8"):
			return []cpeEntry{{"dell", "idrac8_firmware"}}
		case strings.Contains(prod, "idrac7") || strings.Contains(prod, "idrac 7"):
			return []cpeEntry{{"dell", "idrac7_firmware"}}
		default:
			return []cpeEntry{{"dell", "idrac_firmware"}}
		}

	case strings.Contains(mfr, "supermicro") || strings.Contains(mfr, "super micro"):
		return []cpeEntry{{"supermicro", "intelligent_platform_management_firmware"}}

	case strings.Contains(mfr, "oracle") || strings.Contains(mfr, "sun microsystems"):
		return []cpeEntry{{"oracle", "integrated_lights_out_manager"}}

	case strings.Contains(mfr, "lenovo"):
		return []cpeEntry{{"lenovo", "xclarity_controller_firmware"}}

	case strings.Contains(mfr, "cisco"):
		// Cisco Integrated Management Controller appears under two product slugs in NVD.
		return []cpeEntry{
			{"cisco", "integrated_management_controller_firmware"},
			{"cisco", "unified_computing_system_manager"},
		}

	case strings.Contains(mfr, "intel"):
		return []cpeEntry{{"intel", "baseboard_management_controller_firmware"}}

	case strings.Contains(mfr, "huawei"):
		return []cpeEntry{{"huawei", "ibmc_firmware"}}

	case strings.Contains(mfr, "fujitsu"):
		return []cpeEntry{{"fujitsu", "irmc_firmware"}}

	case strings.Contains(mfr, "quanta"):
		// Quanta BMCs are often AMI-based; query both slugs for better coverage.
		return []cpeEntry{
			{"quanta_computer", "bmc_firmware"},
			{"american_megatrends", "megarac_sp-x"},
		}

	case strings.Contains(mfr, "ami") || strings.Contains(mfr, "american megatrends"):
		// AMI MegaRAC appears under multiple product slugs in NVD.
		return []cpeEntry{
			{"american_megatrends", "megarac_sp-x"},
			{"american_megatrends", "megarac"},
		}
	}

	return nil
}

// lookupCPE is a convenience wrapper returning the primary (first) CPE entry.
// Used internally when only a single entry is needed for a version-specific query.
func lookupCPE(manufacturer, productName string) *cpeEntry {
	entries := lookupCPEs(manufacturer, productName)
	if len(entries) == 0 {
		return nil
	}
	return &entries[0]
}

// cpeString builds a NVD CPE 2.3 formatted-string-binding.
// Pass a specific version (e.g. "2.78") for a version-filtered query, or "*"
// for the product-family wildcard.
func cpeString(vendor, product, version string) string {
	if version == "" {
		version = "*"
	}
	return "cpe:2.3:o:" + vendor + ":" + product + ":" + version + ":*:*:*:*:*:*:*"
}

// normalizeVersion sanitises a firmware revision string into a CPE version
// component. Returns "" if the value is absent, zero, or non-numeric.
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "0.00" || v == "00.00" {
		return ""
	}
	// ipmitool sometimes appends a build date: "2.78 Apr 10 2023" — take the first token.
	if i := strings.IndexAny(v, " \t"); i >= 0 {
		v = v[:i]
	}
	// Accept only dotted-numeric strings so we never send garbage to NVD.
	for _, r := range v {
		if r != '.' && (r < '0' || r > '9') {
			return ""
		}
	}
	return v
}
