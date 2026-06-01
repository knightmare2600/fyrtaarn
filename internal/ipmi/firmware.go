package ipmi

import "strings"

// FirmwareInfo holds the firmware revision extracted from mc info, plus the
// raw IPMI version so callers can apply compliance rules.
type FirmwareInfo struct {
	FirmwareRevision string
	IPMIVersion      string
	ManufacturerName string
	ProductName      string
}

// ComplianceResult is returned by CheckFirmwareCompliance.
type ComplianceResult struct {
	Compliant bool
	Issues    []string
	Info      FirmwareInfo
}

// GetFirmwareInfo fetches only the mc info fields needed for compliance checks.
func GetFirmwareInfo(host, user, pass string) (*FirmwareInfo, error) {
	mc, err := GetMCInfo(host, user, pass)
	if err != nil {
		return nil, err
	}
	return &FirmwareInfo{
		FirmwareRevision: mc.FirmwareRevision,
		IPMIVersion:      mc.IPMIVersion,
		ManufacturerName: mc.ManufacturerName,
		ProductName:      mc.ProductName,
	}, nil
}

// CheckFirmwareCompliance runs heuristic compliance checks against the BMC
// firmware. It does not connect to a vendor advisory database; it flags
// known-bad patterns that are worth human review:
//
//   - IPMI 1.5 — lacks mandatory authentication improvements from IPMI 2.0
//   - Empty firmware revision — BMC did not report one (common on unconfigured/bricked BMCs)
//   - Firmware revision "0.00" — placeholder, likely factory default not updated
func CheckFirmwareCompliance(host, user, pass string) (*ComplianceResult, error) {
	info, err := GetFirmwareInfo(host, user, pass)
	if err != nil {
		return nil, err
	}

	result := &ComplianceResult{Info: *info, Compliant: true}

	if info.IPMIVersion == "1.5" || strings.HasPrefix(info.IPMIVersion, "1.") {
		result.Issues = append(result.Issues,
			"IPMI 1.5 detected — lacks IPMI 2.0 authentication hardening (CVE-2013-4782 class)")
		result.Compliant = false
	}

	if info.FirmwareRevision == "" {
		result.Issues = append(result.Issues,
			"Firmware revision not reported — BMC may be unconfigured or in recovery mode")
		result.Compliant = false
	} else if info.FirmwareRevision == "0.00" || info.FirmwareRevision == "00.00" {
		result.Issues = append(result.Issues,
			"Firmware revision is 0.00 — likely factory default, update required")
		result.Compliant = false
	}

	return result, nil
}
