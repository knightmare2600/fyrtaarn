// Package export writes scan results to disk in CSV or JSON format.
package export

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/knightmare2600/fyrtaarn/internal/discovery"
)

// WriteCSV writes results to a CSV file at path.
func WriteCSV(path string, results []discovery.HostResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	fmt.Fprintf(f, "# fyrtaarn export — %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintln(f, "ip,mac,vendor,hostname,is_bmc,confidence,has_redfish,redfish_version,redfish_manufacturer,redfish_model,open_ports")

	for _, h := range results {
		fmt.Fprintf(f, "%s,%s,%s,%s,%v,%d,%v,%s,%s,%s,%s\n",
			q(h.IP), q(h.MAC), q(h.Vendor), q(h.Hostname),
			h.IsBMC, h.Confidence,
			h.HasRedfish, q(h.RedfishVersion), q(h.RedfishManufacturer), q(h.RedfishModel),
			q(portList(h.Ports)),
		)
	}
	return nil
}

// exportRecord is the JSON-serialisable form of a HostResult.
type exportRecord struct {
	IP                  string `json:"ip"`
	MAC                 string `json:"mac,omitempty"`
	Vendor              string `json:"vendor,omitempty"`
	Hostname            string `json:"hostname,omitempty"`
	IsBMC               bool   `json:"is_bmc"`
	Confidence          int    `json:"confidence"`
	HasRedfish          bool   `json:"has_redfish"`
	RedfishVersion      string `json:"redfish_version,omitempty"`
	RedfishManufacturer string `json:"redfish_manufacturer,omitempty"`
	RedfishModel        string `json:"redfish_model,omitempty"`
	OpenPorts           string `json:"open_ports,omitempty"`
}

// WriteJSON writes results to an indented JSON file at path.
func WriteJSON(path string, results []discovery.HostResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	records := make([]exportRecord, len(results))
	for i, h := range results {
		records[i] = exportRecord{
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
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(records)
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
