// Package export writes scan results to disk in CSV or JSON format.
package export

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/knightmare2600/fyrtaarn/internal/discovery"
	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
)

// WriteCSV writes results to a CSV file at path.
// details is a map of IP → HostDetails for any hosts that were connected to
// during the session; those rows get additional BMC columns populated.
func WriteCSV(path string, results []discovery.HostResult, details map[string]*ipmi.HostDetails) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "# fyrtaarn export — %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintln(f, "ip,mac,vendor,hostname,is_bmc,confidence,has_redfish,redfish_version,redfish_manufacturer,redfish_model,open_ports,firmware_revision,ipmi_version,bmc_manufacturer,bmc_product,bmc_mac,bmc_ip,bmc_gateway")

	for _, h := range results {
		d := details[h.IP]
		fmt.Fprintf(f, "%s,%s,%s,%s,%v,%d,%v,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			q(h.IP), q(h.MAC), q(h.Vendor), q(h.Hostname),
			h.IsBMC, h.Confidence,
			h.HasRedfish, q(h.RedfishVersion), q(h.RedfishManufacturer), q(h.RedfishModel),
			q(portList(h.Ports)),
			q(mcField(d, func(m *ipmi.MCInfo) string { return m.FirmwareRevision })),
			q(mcField(d, func(m *ipmi.MCInfo) string { return m.IPMIVersion })),
			q(mcField(d, func(m *ipmi.MCInfo) string { return m.ManufacturerName })),
			q(mcField(d, func(m *ipmi.MCInfo) string { return m.ProductName })),
			q(lanField(d, func(l *ipmi.LANInfo) string { return l.MACAddress })),
			q(lanField(d, func(l *ipmi.LANInfo) string { return l.IPAddress })),
			q(lanField(d, func(l *ipmi.LANInfo) string { return l.Gateway })),
		)
	}
	return nil
}

// bmcDetails is the JSON-serialisable form of per-host BMC info.
type bmcDetails struct {
	FirmwareRevision string `json:"firmware_revision,omitempty"`
	IPMIVersion      string `json:"ipmi_version,omitempty"`
	Manufacturer     string `json:"manufacturer,omitempty"`
	Product          string `json:"product,omitempty"`
	BMCMAC           string `json:"bmc_mac,omitempty"`
	BMCIP            string `json:"bmc_ip,omitempty"`
	BMCGateway       string `json:"bmc_gateway,omitempty"`
}

// exportRecord is the JSON-serialisable form of a HostResult.
type exportRecord struct {
	IP                  string      `json:"ip"`
	MAC                 string      `json:"mac,omitempty"`
	Vendor              string      `json:"vendor,omitempty"`
	Hostname            string      `json:"hostname,omitempty"`
	IsBMC               bool        `json:"is_bmc"`
	Confidence          int         `json:"confidence"`
	HasRedfish          bool        `json:"has_redfish"`
	RedfishVersion      string      `json:"redfish_version,omitempty"`
	RedfishManufacturer string      `json:"redfish_manufacturer,omitempty"`
	RedfishModel        string      `json:"redfish_model,omitempty"`
	OpenPorts           string      `json:"open_ports,omitempty"`
	BMCDetails          *bmcDetails `json:"bmc_details,omitempty"`
}

// WriteJSON writes results to an indented JSON file at path.
// details is a map of IP → HostDetails for any hosts connected to during the session.
func WriteJSON(path string, results []discovery.HostResult, details map[string]*ipmi.HostDetails) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	records := make([]exportRecord, len(results))
	for i, h := range results {
		rec := exportRecord{
			IP:                  h.IP,
			MAC:                 h.MAC,
			Vendor:              h.Vendor,
			Hostname:            h.Hostname,
			IsBMC:               h.IsBMC,
			Confidence:          h.Confidence,
			HasRedfish:          h.HasRedfish,
			RedfishVersion:      h.RedfishVersion,
			RedfishManufacturer: h.RedfishManufacturer,
			RedfishModel:        h.RedfishModel,
			OpenPorts:           portList(h.Ports),
		}
		if d := details[h.IP]; d != nil {
			bd := &bmcDetails{}
			if d.MCInfo != nil {
				bd.FirmwareRevision = d.MCInfo.FirmwareRevision
				bd.IPMIVersion = d.MCInfo.IPMIVersion
				bd.Manufacturer = d.MCInfo.ManufacturerName
				bd.Product = d.MCInfo.ProductName
			}
			if d.LAN != nil {
				bd.BMCMAC = d.LAN.MACAddress
				bd.BMCIP = d.LAN.IPAddress
				bd.BMCGateway = d.LAN.Gateway
			}
			// Only attach if at least one field is populated.
			if *bd != (bmcDetails{}) {
				rec.BMCDetails = bd
			}
		}
		records[i] = rec
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(records)
}

// mcField extracts a string from MCInfo when the HostDetails entry exists.
func mcField(d *ipmi.HostDetails, fn func(*ipmi.MCInfo) string) string {
	if d == nil || d.MCInfo == nil {
		return ""
	}
	return fn(d.MCInfo)
}

// lanField extracts a string from LANInfo when the HostDetails entry exists.
func lanField(d *ipmi.HostDetails, fn func(*ipmi.LANInfo) string) string {
	if d == nil || d.LAN == nil {
		return ""
	}
	return fn(d.LAN)
}

// q CSV-quotes a field only when necessary.
func q(s string) string {
	if strings.ContainsAny(s, ",\"\n\r") {
		return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
	}
	return s
}

func portList(ports []discovery.PortInfo) string {
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d/%s", p.PortID, p.Protocol))
	}
	return strings.Join(parts, ";")
}
