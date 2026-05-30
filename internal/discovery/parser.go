package discovery

import (
	"encoding/xml"
	"io"
	"strings"
)

type NmapRun struct {
	Hosts []NmapHost `xml:"host"`
}

type NmapHost struct {
	Addresses  []NmapAddress  `xml:"address"`
	Ports      NmapPorts      `xml:"ports"`
	HostScript NmapHostScript `xml:"hostscript"`
}

type NmapPorts struct {
	Ports []NmapPort `xml:"port"`
}

type NmapAddress struct {
	Addr     string `xml:"addr,attr"`
	AddrType string `xml:"addrtype,attr"`
	Vendor   string `xml:"vendor,attr"`
}

type NmapPort struct {
	Protocol string          `xml:"protocol,attr"`
	PortID   int             `xml:"portid,attr"`
	State    NmapPortState   `xml:"state"`
	Service  NmapPortService `xml:"service"`
}

type NmapPortState struct {
	State string `xml:"state,attr"`
}

type NmapPortService struct {
	Name string `xml:"name,attr"`
}

type NmapHostScript struct {
	Scripts []NmapScript `xml:"script"`
}

type NmapScript struct {
	ID     string `xml:"id,attr"`
	Output string `xml:"output,attr"`
}

func ParseNmapXML(r io.Reader) ([]HostResult, error) {

	var run NmapRun

	err := xml.NewDecoder(r).Decode(&run)
	if err != nil {
		return nil, err
	}

	results := []HostResult{}

	for _, host := range run.Hosts {

		result := HostResult{}

		for _, addr := range host.Addresses {

			switch addr.AddrType {

			case "ipv4":
				result.IP = addr.Addr

			case "mac":
				result.MAC = addr.Addr
				result.Vendor = addr.Vendor
			}
		}

		var ports []NmapPort
		if len(host.Ports.Ports) > 0 {
			ports = host.Ports.Ports
		}

		for _, port := range ports {

			p := PortInfo{
				Protocol: port.Protocol,
				PortID:   port.PortID,
				State:    port.State.State,
				Service:  port.Service.Name,
			}

			result.Ports = append(result.Ports, p)

			scoreHost(&result, p)
		}

		// Extract ipmi-version NSE script output (root-mode scans only).
		for _, script := range host.HostScript.Scripts {
			if script.ID == "ipmi-version" {
				result.IPMIScript = strings.TrimSpace(script.Output)
				// Confirmed IPMI response from the host — strong signal.
				result.Confidence += 200
				result.IsBMC = true
			}
		}

		results = append(results, result)
	}

	return results, nil
}

func scoreHost(host *HostResult, port PortInfo) {

	if port.Protocol == "udp" &&
		port.PortID == 623 &&
		port.Service == "asf-rmcp" {

		host.Confidence += 250
	}

	if port.Protocol == "tcp" &&
		port.PortID == 623 {

		host.Confidence += 100
	}

	if port.Protocol == "tcp" &&
		port.PortID == 443 &&
		port.State == "open" {

		host.Confidence += 10
	}

	if host.Vendor == "Quanta Computer" {
		host.Confidence += 100
	}

	if host.Confidence >= 200 {
		host.IsBMC = true
	}
}
