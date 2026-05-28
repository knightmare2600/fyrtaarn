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

	Ports []PortInfo
}
