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
	// WebUI names the BMC's web interface when it was recognised from its
	// front page (see probeWebUI), e.g. "AMI MegaRAC SP"; used for BMCs
	// without Redfish.
	WebUI string
	// Fingerprint names the firmware build recognised from the web front
	// page's ETag (internal/quirks Fingerprints), e.g. "HP iLO 4, firmware
	// 2.54"; known before any login.
	Fingerprint string
}
