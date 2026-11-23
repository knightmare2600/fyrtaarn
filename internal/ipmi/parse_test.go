package ipmi

import (
	"strings"
	"testing"
)

// Fixtures are either captured from real hardware or built from the exact
// printf formats in ipmitool 1.8.18 (file and line cited). Replace a built
// fixture with a real capture whenever one is available.

// Captured: Dell iDRAC9, FW 4.20, ipmitool 1.8.18, 2026-10-28.
const mcInfoIDRAC9 = `Device ID                 : 32
Device Revision           : 1
Firmware Revision         : 4.20
IPMI Version              : 2.0
Manufacturer ID           : 674
Manufacturer Name         : Dell Inc.
Product ID                : 256 (0x0100)
Product Name              : Unknown (0x100)
Device Available          : yes
Provides Device SDRs      : yes
Additional Device Support :
    Sensor Device
    SDR Repository Device
    SEL Device
    FRU Inventory Device
    IPMB Event Receiver
    Bridge
    Chassis Device
Aux Firmware Rev Info     :
    0x00
    0x22
    0x14
    0x14
`

func TestParseMCInfoIDRAC9(t *testing.T) {
	got := parseMCInfo(mcInfoIDRAC9)
	want := MCInfo{
		DeviceID:         "32",
		FirmwareRevision: "4.20",
		IPMIVersion:      "2.0",
		ManufacturerName: "Dell Inc.",
		ProductName:      "Unknown (0x100)",
		ManufacturerID:   674,
		ProductID:        256,
	}
	if *got != want {
		t.Errorf("parseMCInfo:\n got %+v\nwant %+v", *got, want)
	}
}

// First five lines captured from the same iDRAC9; the rest built from
// lib/ipmi_lanp.c:693-713 ("%-24s: ...", MAC via mac2str with ':' separators)
// and the parameter names in include/ipmitool/ipmi_lanp.h:131-142.
const lanPrint = `Set in Progress         : Set Complete
Auth Type Support       : MD5
Auth Type Enable        : Callback : MD5
                        : User     : MD5
                        : Operator : MD5
IP Address Source       : Static Address
IP Address              : 192.168.1.27
Subnet Mask             : 255.255.255.0
MAC Address             : 18:66:da:0a:1b:2c
Default Gateway IP      : 192.168.1.1
Default Gateway MAC     : 00:00:00:00:00:00
Backup Gateway IP       : 0.0.0.0
`

func TestParseLANInfo(t *testing.T) {
	got := parseLANInfo(lanPrint)
	want := LANInfo{
		IPAddress:  "192.168.1.27",
		MACAddress: "18:66:da:0a:1b:2c", // value itself contains colons
		SubnetMask: "255.255.255.0",
		Gateway:    "192.168.1.1", // not "Default Gateway MAC" or "Backup Gateway IP"
	}
	if *got != want {
		t.Errorf("parseLANInfo:\n got %+v\nwant %+v", *got, want)
	}
}

// Built from lib/ipmi_chassis.c:320-359.
func chassisStatus(power, overload, mainFault, drive, fan string) string {
	return "System Power         : " + power + "\n" +
		"Power Overload       : " + overload + "\n" +
		"Power Interlock      : inactive\n" +
		"Main Power Fault     : " + mainFault + "\n" +
		"Power Control Fault  : false\n" +
		"Power Restore Policy : always-off\n" +
		"Last Power Event     : \n" +
		"Chassis Intrusion    : inactive\n" +
		"Front-Panel Lockout  : inactive\n" +
		"Drive Fault          : " + drive + "\n" +
		"Cooling/Fan Fault    : " + fan + "\n"
}

func TestParseChassisStatus(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want ChassisStatus
	}{
		{"on, healthy", chassisStatus("on", "false", "false", "false", "false"),
			ChassisStatus{PowerStateFound: true, PowerOn: true}},
		{"off, all faults", chassisStatus("off", "true", "true", "true", "true"),
			ChassisStatus{PowerStateFound: true, PowerOverload: true, PowerFault: true, DriveFault: true, CoolingFault: true}},
		{"no power line", "Drive Fault          : false\n", ChassisStatus{}},
	}
	for _, c := range cases {
		if got := parseChassisStatus(c.in); *got != c.want {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, *got, c.want)
		}
	}
}

// Built from lib/ipmi_sdr.c:1619-1649: "%-16s | " then the value padded to
// 17 columns (sval[16]), " | ", status; plus the event-only (2113) and
// MC-locator (2155) shapes.
const sdrList = `Inlet Temp       | 24 degrees C      | ok
Fan1 RPM         | 7200 RPM          | ok
PS1 Status       | 0x01              | ok
Pwr Consumption  | no reading        | ns
Intrusion        | Not Readable      | ns
PS Redundancy    | Event-Only        | ns
BMC              | Dell MC @ 20h      | ok
`

func TestParseSDR(t *testing.T) {
	got := parseSDR(sdrList)
	want := []SDREntry{
		{"Inlet Temp", "24 degrees C", "ok"},
		{"Fan1 RPM", "7200 RPM", "ok"},
		{"PS1 Status", "0x01", "ok"},
		{"Pwr Consumption", "no reading", "ns"},
		{"Intrusion", "Not Readable", "ns"},
		{"PS Redundancy", "Event-Only", "ns"},
		{"BMC", "Dell MC @ 20h", "ok"},
	}
	if len(got) != len(want) {
		t.Fatalf("parseSDR: %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

// Built from lib/ipmi_sel.c:1802-2010 (ipmi_sel_print_std_entry): one line
// per record shape. Dates "%m/%d/%Y", times "%H:%M:%S".
const selList = `   1 | 10/28/2026 | 13:41:56 | Power Supply PS1 Status | Presence detected | Asserted
   2 | 10/28/2026 | 13:42:10 | Temperature Inlet Temp | Upper Critical going high | Asserted | Reading 47 > Threshold 45 degrees C
   3 | 10/28/2026 | 13:50:02 | Temperature Inlet Temp | Upper Critical going high | Deasserted | Reading 40 < Threshold 45 degrees C
   4 |  Pre-Init  |0000000012| System Event #0x83 | OEM System boot event | Asserted
   5 | 10/28/2026 | 14:00:00 | OEM record c1 | 0002a2 | 0102030405060708
   6 | OEM record e0 | 0102030405060708090a0b0c0d
   7 | Linux kernel panic: Fatal excep
`

func TestParseSEL(t *testing.T) {
	got := parseSEL(selList)
	want := []SELEntry{
		{ID: "1", Timestamp: "10/28/2026 13:41:56", Event: "Power Supply PS1 Status: Presence detected", Direction: "Asserted"},
		{ID: "2", Timestamp: "10/28/2026 13:42:10", Event: "Temperature Inlet Temp: Upper Critical going high (Reading 47 > Threshold 45 degrees C)", Direction: "Asserted"},
		{ID: "3", Timestamp: "10/28/2026 13:50:02", Event: "Temperature Inlet Temp: Upper Critical going high (Reading 40 < Threshold 45 degrees C)", Direction: "Deasserted"},
		{ID: "4", Timestamp: "Pre-Init 0000000012", Event: "System Event #0x83: OEM System boot event", Direction: "Asserted"},
		{ID: "5", Timestamp: "10/28/2026 14:00:00", Event: "OEM record c1: 0002a2 0102030405060708"},
		{ID: "6", Event: "OEM record e0: 0102030405060708090a0b0c0d"},
		{ID: "7", Event: "Linux kernel panic: Fatal excep"},
	}
	if len(got) != len(want) {
		t.Fatalf("parseSEL: %d entries, want %d:\n%+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
}

// Built from lib/ipmi_fru.c:953-1169 and 3038/3094.
const fruOut = `FRU Device Description : Builtin FRU Device (ID 0)
 Board Mfg Date        : Mon Jan  1 00:00:00 1996
 Board Mfg             : DELL
 Product Name          : PowerEdge R640
 Chassis Serial        : ABC1234
 Board Extra           :

FRU Device Description : PS1 (ID 1)
 Board Mfg             : DELL
`

func TestParseFRU(t *testing.T) {
	got := parseFRU(fruOut)
	want := []FRUEntry{
		{Field: "FRU Device Description", Value: "Builtin FRU Device (ID 0)", IsHeader: true},
		{Field: "Board Mfg Date", Value: "Mon Jan  1 00:00:00 1996"}, // value has colons
		{Field: "Board Mfg", Value: "DELL"},
		{Field: "Product Name", Value: "PowerEdge R640"},
		{Field: "Chassis Serial", Value: "ABC1234"},
		{Field: "FRU Device Description", Value: "PS1 (ID 1)", IsHeader: true},
		{Field: "Board Mfg", Value: "DELL"},
	}
	if len(got) != len(want) {
		t.Fatalf("parseFRU: %d entries, want %d:\n%+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

// Built from lib/ipmi_user.c:239-250: header, then
// "%-4d%-17s%-8s%-11s%-11s%-s" with "true " / "false" fields.
const userList = "ID  Name\t     Callin  Link Auth\tIPMI Msg   Channel Priv Limit\n" +
	"1                    true    false      false      NO ACCESS\n" +
	"2   root             false   true       true       ADMINISTRATOR\n" +
	"3   operator         true    true       true       OPERATOR\n"

func TestParseUserList(t *testing.T) {
	got := parseUserList(userList)
	want := []UserEntry{
		{ID: 1, CallinEnabled: true, Privilege: "NO ACCESS"}, // empty slot: no name column
		{ID: 2, Name: "root", Enabled: true, LinkEnabled: true, IPMIEnabled: true, Privilege: "ADMINISTRATOR"},
		{ID: 3, Name: "operator", Enabled: true, CallinEnabled: true, LinkEnabled: true, IPMIEnabled: true, Privilege: "OPERATOR"},
	}
	if len(got) != len(want) {
		t.Fatalf("parseUserList: %d entries, want %d:\n%+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d:\n got %+v\nwant %+v", i, got[i], want[i])
		}
	}
	if strings.Contains(userList, "(Empty User)") {
		t.Fatal("fixture drifted from ipmitool's format")
	}
}

// channel getaccess output, built from ipmi_channel.c's printf formats
// (ipmitool 1.8.18, lines 718-738); user 3 has a name but is disabled, the
// case the name-based guess got wrong on the Quanta in Tested Hardware.
const getAccessOutput = `Maximum User IDs     : 10
Enabled User IDs     : 1

User ID              : 1
User Name            : 
Fixed Name           : Yes
Access Available     : call-in / callback
Link Authentication  : disabled
IPMI Messaging       : disabled
Privilege Level      : NO ACCESS
Enable Status        : disabled

User ID              : 2
User Name            : admin
Fixed Name           : No
Access Available     : call-in / callback
Link Authentication  : enabled
IPMI Messaging       : enabled
Privilege Level      : ADMINISTRATOR
Enable Status        : enabled

User ID              : 3
User Name            : olduser
Fixed Name           : No
Access Available     : call-in / callback
Link Authentication  : disabled
IPMI Messaging       : disabled
Privilege Level      : USER
Enable Status        : disabled

User ID              : 4
User Name            : ghost
Fixed Name           : No
Access Available     : call-in / callback
Link Authentication  : disabled
IPMI Messaging       : disabled
Privilege Level      : USER
Enable Status        : unknown
`

func TestEnableStatus(t *testing.T) {
	users := []UserEntry{{ID: 2, Name: "admin", Enabled: true}, {ID: 3, Name: "olduser", Enabled: true}, {ID: 4, Name: "ghost", Enabled: true}}
	applyEnableStatus(users, parseEnableStatus(getAccessOutput))
	if !users[0].Enabled || users[1].Enabled || !users[2].Enabled {
		t.Fatalf("got %+v (want admin on, olduser off, ghost unchanged)", users)
	}
}

// TestFRUDeviceNotPresent checks an empty FRU device keeps its explanation
// (ipmitool ipmi_fru.c prints " Device not present (%s)" under the header).
func TestFRUDeviceNotPresent(t *testing.T) {
	out := "FRU Device Description : Builtin FRU Device (ID 0)\n Product Name          : Test\n\n" +
		"FRU Device Description : CPU 2 (ID 2)\n Device not present (Parameter out of range)\n"
	es := parseFRU(out)
	if len(es) != 4 || !es[2].IsHeader || es[3].Field != "Status" || es[3].Value != "Device not present (Parameter out of range)" {
		t.Fatalf("got %+v", es)
	}
}

// sol info output as ipmitool 1.8.18 (ipmi_sol.c) prints it, modelled on
// the iDRAC9 in Tested Hardware including its "not supported" notice line;
// the non-volatile rate is changed from the iDRAC's 115.2 so the two rates
// can be told apart.
const solInfoOutput = `Info: SOL parameter 'Payload Channel (7)' not supported - defaulting to 0x0e
Set in progress                 : set-complete
Enabled                         : true
Force Encryption                : true
Force Authentication            : false
Privilege Level                 : ADMINISTRATOR
Character Accumulate Level (ms) : 50
Character Send Threshold        : 255
Retry Count                     : 7
Retry Interval (ms)             : 480
Volatile Bit Rate (kbps)        : 115.2
Non-Volatile Bit Rate (kbps)    : 57.6
Payload Channel                 : 14 (0x0e)
Payload Port                    : 623
`

func TestParseSOLInfo(t *testing.T) {
	s := parseSOLInfo(solInfoOutput)
	if !s.Enabled || !s.ForceEncryption || s.Privilege != "ADMINISTRATOR" || s.BitRate != "115.2" || s.NVBitRate != "57.6" {
		t.Fatalf("got %+v", s)
	}
	if parseSOLInfo("Enabled : false\n").Enabled {
		t.Fatal("disabled read as enabled")
	}
	for in, want := range map[string]string{
		"User 2 on channel 14 is enabled": "enabled",
		"User 3 on channel 1 is disabled": "disabled",
		"Error: something":                "",
	} {
		if got := parsePayloadStatus(in); got != want {
			t.Errorf("%q: got %q", in, got)
		}
	}
}

// TestUserListNamesWithSpaces checks names with spaces, as iLO 5 (FW 1.40)
// prints for unused slots, do not shift the columns after them.
func TestUserListNamesWithSpaces(t *testing.T) {
	out := "ID  Name\t     Callin  Link Auth\tIPMI Msg   Channel Priv Limit\n" +
		"1                    true    false      false      NO ACCESS\n" +
		"2   Administrator    true    false      true       ADMINISTRATOR\n" +
		"3   (Empty User)     true    false      true       NO ACCESS\n" +
		"4   ops team         true    true       true       OPERATOR\n"
	us := parseUserList(out)
	if len(us) != 4 {
		t.Fatalf("got %d users: %+v", len(us), us)
	}
	want := []struct {
		name, priv string
		enabled    bool
	}{{"", "NO ACCESS", false}, {"Administrator", "ADMINISTRATOR", true}, {"(Empty User)", "NO ACCESS", false}, {"ops team", "OPERATOR", true}}
	for i, w := range want {
		if us[i].Name != w.name || us[i].Privilege != w.priv || us[i].Enabled != w.enabled {
			t.Errorf("user %d: got %+v want %+v", i+1, us[i], w)
		}
	}
	if !us[1].IPMIEnabled || us[0].IPMIEnabled || !us[3].LinkEnabled {
		t.Errorf("flags: %+v", us)
	}
}
