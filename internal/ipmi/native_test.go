package ipmi

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/bougou/go-ipmi/pkg/types"
)

// The native MCInfo matches what parseMCInfo gives from ipmitool's mc info
// on the same BMCs (captures from the iDRAC9, ML310e iLO 4, ML350 iLO 5 and
// Quanta in Tested Hardware).
func TestMCInfoFromDeviceIDMatchesIpmitool(t *testing.T) {
	for _, c := range []struct {
		d        deviceID
		ipmitool string
	}{
		{deviceID{32, 4, 20, 2, 0, 674, 0x0100}, "Device ID                 : 32\nFirmware Revision         : 4.20\nIPMI Version              : 2.0\nManufacturer ID           : 674\nManufacturer Name         : Dell Inc.\nProduct ID                : 256 (0x0100)\nProduct Name              : Unknown (0x100)\n"},
		{deviceID{19, 2, 54, 2, 0, 11, 0x2000}, "Device ID                 : 19\nFirmware Revision         : 2.54\nIPMI Version              : 2.0\nManufacturer ID           : 11\nManufacturer Name         : Hewlett-Packard\nProduct ID                : 8192 (0x2000)\nProduct Name              : Unknown (0x2000)\n"},
		{deviceID{19, 1, 40, 2, 0, 47196, 0x2000}, "Device ID                 : 19\nFirmware Revision         : 1.40\nIPMI Version              : 2.0\nManufacturer ID           : 47196\nManufacturer Name         : Hewlett Packard Enterprise\nProduct ID                : 8192 (0x2000)\nProduct Name              : Unknown (0x2000)\n"},
		{deviceID{32, 7, 54, 2, 0, 7244, 0x3252}, "Device ID                 : 32\nFirmware Revision         : 7.54\nIPMI Version              : 2.0\nManufacturer ID           : 7244\nManufacturer Name         : Quanta Computer Inc.\nProduct ID                : 12882 (0x3252)\nProduct Name              : Unknown (0x3252)\n"},
	} {
		got, want := *mcInfoFromDeviceID(c.d), *parseMCInfo(c.ipmitool)
		if got != want {
			t.Errorf("native %+v\nipmitool %+v", got, want)
		}
	}
	// A one-digit minor firmware number keeps two digits, as ipmitool's.
	if fw := mcInfoFromDeviceID(deviceID{MajorFirmware: 3, MinorFirmware: 5}).FirmwareRevision; fw != "3.05" {
		t.Errorf("firmware %q", fw)
	}
}

func TestBackendSelection(t *testing.T) {
	const h = "10.0.0.99"
	t.Cleanup(func() { nativeFailed.Delete(h) })
	t.Setenv(backendEnv, "")
	if !useNative(h) {
		t.Fatal("default: native not tried")
	}
	nativeFailed.Store(h, "set-up failed")
	if useNative(h) || NativeFallback(h) == "" {
		t.Fatal("a failed host still tried natively")
	}
	t.Setenv(backendEnv, "native")
	if !useNative(h) || fallbackAllowed() {
		t.Fatal("native-only mode")
	}
	t.Setenv(backendEnv, "IPMItool")
	if useNative("10.0.0.98") {
		t.Fatal("ipmitool-only mode tried native")
	}
	if err := withNative("10.0.0.98", "u", "p", ipmiShortTimeout, nil); !errors.Is(err, errNativeOff) {
		t.Fatalf("ipmitool-only: %v", err)
	}
}

// The native user entries match parseUserList plus "channel getaccess" on
// the same BMC states (the Quanta's user list, and iLO 5's "(Empty User)").
func TestUserEntryFromMatchesIpmitool(t *testing.T) {
	list := "ID  Name\t     Callin  Link Auth\tIPMI Msg   Channel Priv Limit\n" +
		"1                    false   false      true       ADMINISTRATOR\n" +
		"2   admin            false   false      true       ADMINISTRATOR\n" +
		"4                    true    false      false      NO ACCESS\n" +
		"5   (Empty User)     true    false      false      NO ACCESS\n"
	want := parseUserList(list)
	applyEnableStatus(want, map[int]bool{1: false, 2: true})
	got := []UserEntry{
		userEntryFrom(userAccess{ID: 1, CallbackOnly: true, IPMIMsg: true, Priv: 4, EnableStatus: 2}),
		userEntryFrom(userAccess{ID: 2, Name: "admin", CallbackOnly: true, IPMIMsg: true, Priv: 4, EnableStatus: 1}),
		userEntryFrom(userAccess{ID: 4, Priv: 0x0F}),
		userEntryFrom(userAccess{ID: 5, Name: "(Empty User)", Priv: 0x0F}),
	}
	if len(got) != len(want) {
		t.Fatalf("ipmitool parse gave %d users", len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("user %d\n native   %+v\n ipmitool %+v", got[i].ID, got[i], want[i])
		}
	}
}

func TestSOLNamesMatchIpmitool(t *testing.T) {
	for code, want := range map[uint8]string{0x0A: "115.2", 0x09: "57.6", 0x06: "9.6", 0x00: "IPMI-Over-Serial-Setting"} {
		if got := solBitRate(code); got != want {
			t.Errorf("bit rate %#x: %q", code, got)
		}
	}
	if privName(4) != "ADMINISTRATOR" || privName(0x0F) != "NO ACCESS" || privName(0) != "Unknown (0x00)" {
		t.Error("privilege names")
	}
}

// Event text from ipmitool's tables, as "sel list" printed it for the
// Quanta's events (sensor type, event/reading type, event data 1 and 2).
func TestEventDescriptionMatchesIpmitool(t *testing.T) {
	for _, c := range []struct {
		sensorType, eventType, d1, d2 uint8
		want                          string
	}{
		{0x09, 0x6f, 0x00, 0x00, "Power off/down"},
		{0x09, 0x6f, 0x04, 0x00, "AC lost"},
		{0x10, 0x6f, 0x02, 0x00, "Log area reset/cleared"},
		{0x10, 0x6f, 0x04, 0x00, "Log full"},
		{0x0d, 0x6f, 0x00, 0x00, "Drive Present"},
		{0x04, 0x01, 0x00, 0x00, "Lower Non-critical going low"}, // trailing space trimmed
		{0x04, 0x01, 0x02, 0x00, "Lower Critical going low"},
		{0x0f, 0x6f, 0xc0, 0x0a, "No video device selected"},               // event data 2 picks the row
		{0x0f, 0x6f, 0xc0, 0x00, "Unspecified"},                            // likewise
		{0x0f, 0x6f, 0xc0, 0x09, "Unrecoverable video controller failure"}, // likewise
		{0x08, 0x6f, 0x03, 0x00, "Power Supply AC lost"},
		{0x0f, 0x6f, 0x00, 0x0a, "Unknown Error"}, // data 2 not flagged as given: the "any" row
		{0x08, 0x6f, 0x02, 0x00, "Predictive failure"},
		{0x09, 0x71, 0x00, 0x00, ""}, // OEM event type: no description
	} {
		if got := eventDescription(c.eventType, c.sensorType, c.d1, c.d2); got != c.want {
			t.Errorf("sensor %#x event %#x data %#x/%#x: %q, want %q", c.sensorType, c.eventType, c.d1, c.d2, got, c.want)
		}
	}
	if sensorTypeName(0x04) != "Fan" || sensorTypeName(0x0f) != "System Firmwares" || sensorTypeName(0xc1) != "Unknown" {
		t.Error("sensor type names")
	}
	if len(genericEventTypes) != 57 || len(sensorSpecificEventTypes) != 335 || len(genericSensorTypes) != 0x2c+1 {
		t.Errorf("tables: %d %d %d (from %s)", len(genericEventTypes), len(sensorSpecificEventTypes), len(genericSensorTypes), ipmitoolTablesTag)
	}
}

// Sensor lines by ipmitool's rules, on the shapes the Quanta's list has.
func TestSDRLineMatchesIpmitool(t *testing.T) {
	ok := &sdrReading{}
	for _, c := range []struct {
		r    sdrRecord
		rd   *sdrReading
		want SDREntry
	}{
		{sdrRecord{name: "Ambient_Temp", full: true, threshold: true, analog: true, value: 19, unit: "degrees C"}, ok, SDREntry{"Ambient_Temp", "19 degrees C", "ok"}},
		{sdrRecord{name: "P1V8_AUX", full: true, threshold: true, analog: true, value: 1.785, unit: "Volts"}, ok, SDREntry{"P1V8_AUX", "1.78 Volts", "ok"}},
		{sdrRecord{name: "P3V3", full: true, threshold: true, analog: true, unit: "Volts"}, nil, SDREntry{"P3V3", "no reading", "ns"}},
		{sdrRecord{name: "PS1_Status"}, nil, SDREntry{"PS1_Status", "Not Readable", "ns"}},
		{sdrRecord{name: "Fan1", full: true, threshold: true, analog: true, unit: "RPM"}, &sdrReading{scanningDisabled: true}, SDREntry{"Fan1", "disabled", "ns"}},
		{sdrRecord{name: "Intrusion"}, &sdrReading{raw: 1}, SDREntry{"Intrusion", "0x01", "ok"}},
		{sdrRecord{name: "CPU", full: true, threshold: true, analog: true, value: 95, unit: "degrees C"}, &sdrReading{ucr: true, unc: true}, SDREntry{"CPU", "95 degrees C", "cr"}},
		{sdrRecord{name: "Fan2", full: true, threshold: true, analog: true, value: 300, unit: "RPM"}, &sdrReading{lnr: true, lcr: true}, SDREntry{"Fan2", "300 RPM", "nr"}},
	} {
		if got := sdrLine(c.r, c.rd); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.r.name, got, c.want)
		}
	}
}

// FRU fields under ipmitool's names, empty ones left out, as parseFRU
// gives them (the Quanta's FP FRU, plus an unspecified date).
func TestFRUEntries(t *testing.T) {
	ascii := func(s string) (types.TypeLength, []byte) { return types.TypeLength(0xc0 | len(s)), []byte(s) }
	ptl, pn := ascii("34S99CB0000")
	stl, sn := ascii("")
	f := &types.FRU{DeviceID: 1, DeviceName: "FP FRU",
		ChassisInfoArea: &types.FRUChassisInfoArea{ChassisType: 0x17, SerialNumberTypeLength: stl, SerialNumber: sn},
		BoardInfoArea:   &types.FRUBoardInfoArea{MfgDateTime: time.Unix(fruEpoch, 0), PartNumberTypeLength: ptl, PartNumber: pn},
	}
	got := fruEntries(f)
	want := []FRUEntry{
		{Field: "FRU Device Description", Value: "FP FRU (ID 1)", IsHeader: true},
		{Field: "Chassis Type", Value: "Rack Mount Chassis"},
		{Field: "Board Part Number", Value: "34S99CB0000"},
	}
	if len(got) != len(want) {
		t.Fatalf("%+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: %+v, want %+v", i, got[i], want[i])
		}
	}
	if e := fruEntries(&types.FRU{DeviceName: "Builtin FRU"}); e[0].Value != "Builtin FRU Device (ID 0)" {
		t.Errorf("device 0: %q", e[0].Value)
	}
}

// ipmitool's length rules (lib/ipmi_user.c): names up to 16 bytes;
// passwords up to 20, stored as 20 bytes when over 16.
func TestUserLengthRules(t *testing.T) {
	if checkUserName("0123456789abcdef") != nil || checkUserName("0123456789abcdefg") == nil {
		t.Error("name length")
	}
	for pw, want20 := range map[string]bool{"": false, "0123456789abcdef": false, "0123456789abcdefg": true, "0123456789abcdefghij": true} {
		if s20, err := passwordStored20(pw); err != nil || s20 != want20 {
			t.Errorf("%d-byte password: stored20=%v err=%v", len(pw), s20, err)
		}
	}
	if _, err := passwordStored20("0123456789abcdefghijk"); err == nil {
		t.Error("21-byte password accepted")
	}
}

// The SOL output never blocks its writer, and Read drains before EOF.
func TestSOLOutput(t *testing.T) {
	o := newSOLOutput()
	for i := 0; i < 1000; i++ {
		if _, err := o.Write([]byte("0123456789")); err != nil {
			t.Fatal(err)
		}
	}
	_ = o.Close()
	n, buf := 0, make([]byte, 4096)
	for {
		k, err := o.Read(buf)
		n += k
		if err != nil {
			break
		}
	}
	if n != 10000 {
		t.Fatalf("read %d bytes", n)
	}
	if _, err := o.Write([]byte("x")); err == nil {
		t.Fatal("write after close accepted")
	}
}

// Keystrokes pass through unchanged, "~X" included (go-ipmi's SOLActivate
// would end the session on it); the pane's escape + '.' at the start of a
// line ends the session instead of being sent.
func TestNativeSOLInput(t *testing.T) {
	s := &NativeSOL{done: make(chan struct{}), lineSt: true, cancel: func() {}}
	s.inR, s.inW = io.Pipe()
	got := make(chan string, 1)
	go func() { b, _ := io.ReadAll(s.inR); got <- string(b) }()
	for _, k := range []string{"~X", "\r", "ls", string([]byte{SOLEscapeChar, '.'}), "\r"} {
		_, _ = s.Write([]byte(k))
	}
	_, _ = s.Write([]byte{SOLEscapeChar, '.'}) // at line start: ends input
	close(s.done)
	if g := <-got; g != "~X\rls"+string([]byte{SOLEscapeChar, '.'})+"\r" {
		t.Fatalf("sent %q", g)
	}
	if _, err := s.Write([]byte("x")); err == nil {
		t.Fatal("input accepted after the session was ended")
	}
}

// IPMB framing for a bridged Get Sensor Reading, worked by hand from IPMI
// 2.0 figure 13-4: owner 0x2c (an ME), LUN 0, sensor 0x10, sequence 1.
func TestBridgedReadingFraming(t *testing.T) {
	got := bridgedReadingRequest(0x2c, 0, 0x10, 1)
	// 0x2c+0x10 = 0x3c -> checksum 0xc4; 0x81+0x04+0x2d+0x10 = 0xc2 -> 0x3e
	want := []byte{0x2c, 0x10, 0xc4, 0x81, 0x04, 0x2d, 0x10, 0x3e}
	if string(got) != string(want) {
		t.Fatalf("request % x, want % x", got, want)
	}
	// Reply: to 0x81, netFn SE response (0x05<<2 = 0x14), from 0x2c, seq 1,
	// cmd 0x2d, ccode 0, reading 0x30, status 0xc0, threshold 0x00.
	// 0x81+0x14 = 0x95 -> 0x6b; 0x2c+0x04+0x2d+0x00+0x30+0xc0+0x00 = 0x14d -> 0xb3
	reply := []byte{0x81, 0x14, 0x6b, 0x2c, 0x04, 0x2d, 0x00, 0x30, 0xc0, 0x00, 0xb3}
	data, err := parseBridgedReply(reply)
	if err != nil || string(data) != string([]byte{0x30, 0xc0, 0x00}) {
		t.Fatalf("data % x err %v", data, err)
	}
	bad := append([]byte{}, reply...)
	bad[7]++ // reading changed: second checksum no longer matches
	if _, err := parseBridgedReply(bad); err == nil {
		t.Error("corrupt reply accepted")
	}
	cc := []byte{0x81, 0x14, 0x6b, 0x2c, 0x04, 0x2d, 0xcb, 0x00}
	cc[7] = ipmbChecksum(cc[3:7])
	if _, err := parseBridgedReply(cc); err == nil {
		t.Error("completion code 0xcb accepted")
	}
	if _, err := parseBridgedReply([]byte{0x00}); !errors.Is(err, errNoEmbeddedReply) {
		t.Errorf("short reply: %v", err)
	}
}
