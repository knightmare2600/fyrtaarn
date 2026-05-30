package discovery

type PortInfo struct {
	Protocol string
	PortID   int

	State   string
	Service string
}

type HostResult struct {
	IP       string
	MAC      string
	Vendor   string
	Hostname string

	IsBMC      bool
	Confidence int

	Ports      []PortInfo
	IPMIScript string // output from nmap ipmi-version NSE script, if run

	HasRedfish          bool
	RedfishVersion      string
	RedfishManufacturer string
	RedfishModel        string
}
