package ipmi

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestSensorUnit(t *testing.T) {
	cases := []struct {
		in     string
		unit   string
		analog bool
	}{
		{"41 degrees C", "degrees C", true},
		{"0.40 Volts", "Volts", true},
		{"4800 RPM", "RPM", true},
		{"30 percent", "percent", true},
		{"0x01", "", false},
		{"no reading", "", false},
		{"disabled", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		u, a := SensorUnit(c.in)
		if u != c.unit || a != c.analog {
			t.Errorf("%q: got %q,%v", c.in, u, a)
		}
	}
}

func TestAnalogNames(t *testing.T) {
	entries := []SDREntry{
		{Name: "Temp", Value: "41 degrees C"},
		{Name: "Temp", Value: "0x01"}, // a later duplicate: ipmitool finds the first
		{Name: "PS Redundancy", Value: "0x00"},
		{Name: "Fan1 RPM", Value: "4800 RPM"},
	}
	got, err := AnalogNames(entries, []string{"Temp", "PS Redundancy", "Fan1 RPM", "Temp"})
	if err != nil || strings.Join(got, ",") != "Temp,Fan1 RPM" {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := AnalogNames(entries, []string{"PS Redundancy"}); !errors.Is(err, ErrNoAnalogSensors) {
		t.Fatalf("err %v", err)
	}
}

func TestParseSensorReadings(t *testing.T) {
	out := "Inlet Temp       | 26.000\nA Very Long Sensor Name | 3.300\nOther            | 1\nnoise line\n"
	rs := parseSensorReadings(out, []string{"Inlet Temp", "A Very Long Sensor Name"})
	if len(rs) != 2 || rs[0].Value != 26 || rs[1].Name != "A Very Long Sensor Name" || rs[1].Value != 3.3 {
		t.Fatalf("got %+v", rs)
	}
	if rs[0].String() != "26" || rs[1].String() != "3.3" {
		t.Fatalf("String: %s %s", rs[0], rs[1])
	}
}

func TestReadSensors(t *testing.T) {
	dir := fakeIPMITool(t)
	cache := filepath.Join(t.TempDir(), "sdr.cache")

	rs, err := ReadSensors("10.9.9.3", "u", "p", cache, []string{"Inlet Temp", "Fan1 RPM"})
	if err != nil || len(rs) != 2 || rs[0].Value != 26 || rs[1].Value != 4800 {
		t.Fatalf("got %+v %v", rs, err)
	}
	argv := readFile(t, filepath.Join(dir, "argv"))
	if !strings.Contains(argv, "\x00-S\x00"+cache+"\x00sensor\x00reading\x00Inlet Temp\x00Fan1 RPM") {
		t.Fatalf("argv %q", argv)
	}

	// One unknown name: the others' readings still come back, with the error.
	rs, err = ReadSensors("10.9.9.3", "u", "p", cache, []string{"Inlet Temp", "Missing"})
	if err == nil || !strings.Contains(err.Error(), "not found") || len(rs) != 1 || rs[0].Name != "Inlet Temp" {
		t.Fatalf("partial: %+v %v", rs, err)
	}
}
