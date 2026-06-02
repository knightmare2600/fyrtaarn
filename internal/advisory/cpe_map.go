package advisory

import "strings"

// cpeEntry is a single CPE vendor+product pair to query against NVD.
type cpeEntry struct {
	vendor  string
	product string
}

// lookupCPE returns the best single CPE entry for the given manufacturer and
// product strings as reported by ipmitool / Redfish. Returns nil if the
// manufacturer is not in the table; callers should skip the advisory lookup
// rather than falling back to a noisy keyword search.
func lookupCPE(manufacturer, productName string) *cpeEntry {
	mfr := strings.ToLower(strings.TrimSpace(manufacturer))
	prod := strings.ToLower(strings.TrimSpace(productName))

	switch {
	case strings.Contains(mfr, "hp") || strings.Contains(mfr, "hewlett") || strings.Contains(mfr, "hpe"):
		// Prefer generation-specific CPE where the product name gives it away.
		switch {
		case strings.Contains(prod, "ilo 6") || strings.Contains(prod, "ilo6"):
			return &cpeEntry{"hp", "integrated_lights-out_6_firmware"}
		case strings.Contains(prod, "ilo 5") || strings.Contains(prod, "ilo5"):
			return &cpeEntry{"hp", "integrated_lights-out_5_firmware"}
		case strings.Contains(prod, "ilo 4") || strings.Contains(prod, "ilo4"):
			return &cpeEntry{"hp", "integrated_lights-out_4_firmware"}
		case strings.Contains(prod, "ilo 3") || strings.Contains(prod, "ilo3"):
			return &cpeEntry{"hp", "integrated_lights-out_3_firmware"}
		default:
			return &cpeEntry{"hp", "integrated_lights-out_firmware"}
		}

	case strings.Contains(mfr, "dell"):
		// iDRAC generation from product name.
		switch {
		case strings.Contains(prod, "idrac9") || strings.Contains(prod, "idrac 9"):
			return &cpeEntry{"dell", "idrac9_firmware"}
		case strings.Contains(prod, "idrac8") || strings.Contains(prod, "idrac 8"):
			return &cpeEntry{"dell", "idrac8_firmware"}
		case strings.Contains(prod, "idrac7") || strings.Contains(prod, "idrac 7"):
			return &cpeEntry{"dell", "idrac7_firmware"}
		default:
			return &cpeEntry{"dell", "idrac_firmware"}
		}

	case strings.Contains(mfr, "supermicro") || strings.Contains(mfr, "super micro"):
		return &cpeEntry{"supermicro", "intelligent_platform_management_firmware"}

	case strings.Contains(mfr, "oracle") || strings.Contains(mfr, "sun microsystems"):
		return &cpeEntry{"oracle", "integrated_lights_out_manager"}

	case strings.Contains(mfr, "lenovo"):
		return &cpeEntry{"lenovo", "xclarity_controller_firmware"}

	case strings.Contains(mfr, "quanta"):
		return &cpeEntry{"quanta_computer", "bmc_firmware"}

	case strings.Contains(mfr, "ami") || strings.Contains(mfr, "american megatrends"):
		return &cpeEntry{"american_megatrends", "megarac_sp-x"}
	}

	return nil
}

// CPEString builds the full NVD formatted-string-binding CPE for a given
// vendor and product with a wildcard version component.
func cpeString(vendor, product string) string {
	return "cpe:2.3:o:" + vendor + ":" + product + ":*:*:*:*:*:*:*:*"
}
