package ipmi

type MCInfo struct {
	DeviceID         string
	FirmwareRevision string
	IPMIVersion      string
	ManufacturerName string
	ProductName      string
}

type LANInfo struct {
	IPAddress  string
	MACAddress string
	SubnetMask string
	Gateway    string
}

type ChassisStatus struct {
	PowerOn         bool
	PowerStateFound bool // true only when the "System Power" key was present in output
	PowerOverload   bool
	PowerFault      bool
	DriveFault      bool
	CoolingFault    bool
}

type HostDetails struct {
	MCInfo  *MCInfo
	LAN     *LANInfo
	Chassis *ChassisStatus
}

type SDREntry struct {
	Name   string
	Value  string
	Status string
}

type SELEntry struct {
	ID        string
	Timestamp string
	Event     string
	Direction string
}

type FRUEntry struct {
	Field    string
	Value    string
	IsHeader bool // true for "FRU Device Description" section lines
}
