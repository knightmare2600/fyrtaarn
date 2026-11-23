package ipmi

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Live sensor readings for a chosen few sensors, without the cost of
// "sdr list" (21 s on the Quanta in Tested Hardware, nearly all of it
// walking the BMC's sensor repository). The repository is dumped to a file
// once ("sdr dump"); "-S <file>" then makes ipmitool load it instead of
// asking the BMC, and "sensor reading" fetches only the named sensors.
// Checked in ipmitool 1.8.18 source: ipmi_main.c loads -S with
// ipmi_sdr_list_cache_fromfile before running the command, which fills the
// list ipmi_sdr_find_sdr_byid searches (lib/ipmi_sdr.c).

// SDRCachePath is where host's sensor repository is cached.
func SDRCachePath(host string) (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	safe := regexp.MustCompile(`[^A-Za-z0-9.-]`).ReplaceAllString(host, "_")
	return filepath.Join(dir, "fyrtaarn", "sdr-"+safe+".cache"), nil
}

// DumpSDR writes host's sensor repository to path. ipmitool overwrites an
// existing regular file and refuses a symlink (ipmi_open_file in
// lib/helper.c).
func DumpSDR(host, user, pass, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	_, err := runIPMICommand(host, user, pass, ipmiLongTimeout, "sdr", "dump", path)
	return err
}

// SensorReading is one analog sensor's current value.
type SensorReading struct {
	Name  string
	Value float64
}

// ReadSensors reads the named analog sensors using the repository cached
// at cache. Sensors with no valid reading right now are left out (ipmitool
// skips them); discrete sensors are left out too, as "sensor reading" only
// reports analog values. When ipmitool reports a failure for some names,
// the readings it did print are returned along with the error.
func ReadSensors(host, user, pass, cache string, names []string) ([]SensorReading, error) {
	if len(names) == 0 {
		return nil, nil
	}
	args := append([]string{"-S", cache, "sensor", "reading"}, names...)
	out, err := runIPMIKeepOutput(host, user, pass, sensorReadTimeout, args...)
	return parseSensorReadings(out, names), err
}

// sensorReadTimeout: a refresh is one session plus one Get Sensor Reading
// per sensor; anything near this means the BMC is struggling.
const sensorReadTimeout = 30 * time.Second

// parseSensorReadings reads "name | value" lines (printf "%-16s | %s" in
// ipmi_sensor_get_reading). The printed name is exactly the requested one,
// padded, so it is matched back against names.
func parseSensorReadings(out string, names []string) []SensorReading {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var rs []SensorReading
	for _, line := range strings.Split(out, "\n") {
		i := strings.LastIndex(line, " | ")
		if i < 0 {
			continue
		}
		name := strings.TrimRight(line[:i], " ")
		if !want[name] {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(line[i+3:]), 64)
		if err != nil {
			continue
		}
		rs = append(rs, SensorReading{Name: name, Value: v})
	}
	return rs
}

// SensorUnit returns the unit part of an "sdr list" reading such as
// "41 degrees C" or "0.40 Volts", and whether the reading is analog (starts
// with a number). Discrete sensors show "0x01", "no reading" or "disabled".
func SensorUnit(sdrValue string) (unit string, analog bool) {
	f := strings.Fields(sdrValue)
	if len(f) == 0 || strings.HasPrefix(f[0], "0x") {
		return "", false
	}
	if _, err := strconv.ParseFloat(f[0], 64); err != nil {
		return "", false
	}
	return strings.Join(f[1:], " "), true
}

// ErrNoAnalogSensors is returned when none of the chosen sensors is analog.
var ErrNoAnalogSensors = errors.New("none of the chosen sensors has an analog reading")

// AnalogNames filters names down to sensors whose "sdr list" reading is
// analog, keeping the first of any duplicate names: ipmitool looks sensors
// up by name and always finds the first.
func AnalogNames(entries []SDREntry, names []string) ([]string, error) {
	value := map[string]string{}
	for _, e := range entries {
		if _, seen := value[e.Name]; !seen {
			value[e.Name] = e.Value
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			continue
		}
		seen[n] = true
		if _, ok := SensorUnit(value[n]); ok {
			out = append(out, n)
		}
	}
	if len(out) == 0 {
		return nil, ErrNoAnalogSensors
	}
	return out, nil
}

// String shows a reading compactly: integers without decimals.
func (r SensorReading) String() string {
	if r.Value == float64(int64(r.Value)) {
		return fmt.Sprintf("%d", int64(r.Value))
	}
	return strconv.FormatFloat(r.Value, 'f', -1, 64)
}
