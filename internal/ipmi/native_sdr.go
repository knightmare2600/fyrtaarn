package ipmi

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"

	"github.com/bougou/go-ipmi/pkg/client"
	"github.com/bougou/go-ipmi/pkg/command/sensor"
	"github.com/bougou/go-ipmi/pkg/types"
)

// Native sensor list: a port of ipmitool 1.8.19's "sdr list" (lib/ipmi_sdr.c,
// ipmi_sdr_print_sdr with type 0xfe, ipmi_sdr_read_sensor_value and the
// normal-output branch of ipmi_sdr_print_sensor_fc), so that each line
// reads as ipmitool prints it and parseSDR's callers (BMC Info, the sensor
// dashboard's unit detection in SensorUnit, export) see the same text.
//
// go-ipmi reads the records and does the arithmetic (reading conversion,
// unit names, both of which match ipmitool's tables and formulas); the
// line-building rules are ipmitool's:
//   - only full and compact sensor records are listed;
//   - a valid analog reading is "<value> <unit>", with no decimals when the
//     value is whole, else two ("19 degrees C", "1.79 Volts");
//   - a valid discrete reading is "0x%02x" of the raw byte;
//   - no valid reading: "disabled" if scanning is off, else "no reading",
//     or "Not Readable" for a compact record either way;
//   - status for a threshold sensor (event/reading type 1) is the worst
//     threshold crossed (nr, cr, nc) or "ok", for others "ok"; "ns" with
//     no valid reading.
//
// A sensor owned by a controller other than the BMC is read through the
// BMC with Send Message, as ipmitool does (native_bridge.go).

// sdrReading is what Get Sensor Reading said, nil if it failed.
type sdrReading struct {
	raw              uint8
	unavailable      bool
	scanningDisabled bool
	unr, ucr, unc    bool
	lnr, lcr, lnc    bool
}

// sdrRecord is what one record contributes to its line, separate from the
// library's types so the line-building is testable without a BMC.
type sdrRecord struct {
	name      string
	full      bool
	threshold bool    // event/reading type 1
	analog    bool    // ipmitool's sdr_sensor_has_analog_reading
	value     float64 // converted reading, when analog
	unit      string
}

// sdrLine builds one "sdr list" line from a record and its reading.
func sdrLine(r sdrRecord, rd *sdrReading) SDREntry {
	e := SDREntry{Name: r.name}
	valid := rd != nil && !rd.unavailable && !rd.scanningDisabled
	switch {
	case valid && r.analog:
		prec := 2
		if r.value == float64(int64(r.value)) {
			prec = 0
		}
		e.Value = fmt.Sprintf("%.*f %s", prec, r.value, r.unit)
	case valid:
		e.Value = fmt.Sprintf("0x%02x", rd.raw)
	case rd != nil && rd.scanningDisabled && r.full:
		e.Value = "disabled"
	case r.full:
		e.Value = "no reading"
	default:
		e.Value = "Not Readable"
	}
	switch {
	case !valid:
		e.Status = "ns"
	case !r.threshold:
		e.Status = "ok"
	case rd.lnr || rd.unr:
		e.Status = "nr"
	case rd.lcr || rd.ucr:
		e.Status = "cr"
	case rd.lnc || rd.unc:
		e.Status = "nc"
	default:
		e.Status = "ok"
	}
	return e
}

// sdrCache keeps each host's sensor records (host -> []*types.SDR) for
// the dashboard's repeated reads, as ipmitool's -S cache file does.
var sdrCache sync.Map

// readSDRRecords reads the full and compact sensor records.
func readSDRRecords(ctx context.Context, c *client.Client) ([]*types.SDR, error) {
	return c.GetSDRsRaw(ctx, types.SDRRecordTypeFullSensor, types.SDRRecordTypeCompactSensor)
}

// readSensor reads one record's sensor, nil if it cannot be read. A
// sensor owned by another controller is read through the BMC
// (bridgedReading).
func readSensor(ctx context.Context, c *client.Client, owner types.GeneratorID, num uint8) *sdrReading {
	if owner.OwnerID() != uint8(types.BMC_SA) || owner.ChannelNumber() != 0 {
		return bridgedReading(ctx, c, owner, num)
	}
	res, err := c.GetSensorReading(ctx, num)
	if err != nil {
		return nil // ipmitool: any error or completion code leaves it invalid
	}
	return readingFrom(res)
}

func readingFrom(res *sensor.GetSensorReadingResponse) *sdrReading {
	return &sdrReading{
		raw: res.Reading, unavailable: res.ReadingUnavailable, scanningDisabled: res.SensorScanningDisabled,
		unr: res.Above_UNR, ucr: res.Above_UCR, unc: res.Above_UNC,
		lnr: res.Below_LNR, lcr: res.Below_LCR, lnc: res.Below_LNC,
	}
}

// recordOf describes an SDR for sdrLine; ok is false for other record
// types. hp is ipmitool's HP exception: HP puts analog readings in some
// non-threshold sensors, and ipmitool converts those for HP (IANA 11) only.
func recordOf(s *types.SDR, hp bool) (r sdrRecord, owner types.GeneratorID, num uint8, ok bool) {
	switch {
	case s.Full != nil:
		f := s.Full
		u := f.SensorUnit
		r = sdrRecord{
			name: strings.TrimRight(string(f.IDStringBytes), "\x00"), full: true,
			threshold: f.SensorEventReadingType == types.EventReadingTypeThreshold,
			unit:      u.String(),
		}
		r.analog = u.AnalogDataFormat != types.SensorAnalogUnitFormat_NotAnalog &&
			(r.threshold || (hp && (u.Percentage || u.ModifierRelation != 0 || u.BaseUnit != 0 || u.ModifierUnit != 0)))
		return r, f.GeneratorID, uint8(f.SensorNumber), true
	case s.Compact != nil:
		cp := s.Compact
		r = sdrRecord{
			name:      strings.TrimRight(string(cp.IDStringBytes), "\x00"),
			threshold: cp.SensorEventReadingType == types.EventReadingTypeThreshold,
		}
		return r, cp.GeneratorID, uint8(cp.SensorNumber), true
	}
	return r, 0, 0, false
}

// sdrEntries reads every listed sensor's current reading and builds the
// lines. A non-linear full sensor needs its reading factors fetched for
// the raw value read, as ipmitool does; if that fails, the reading is
// treated as invalid, again as ipmitool does.
func sdrEntries(ctx context.Context, c *client.Client, sdrs []*types.SDR, hp bool, only map[string]bool) []SDREntry {
	var out []SDREntry
	for _, s := range sdrs {
		r, owner, num, ok := recordOf(s, hp)
		if !ok || (only != nil && !only[r.name]) {
			continue
		}
		rd := readSensor(ctx, c, owner, num)
		if rd != nil && r.analog && !rd.unavailable && !rd.scanningDisabled {
			f := s.Full
			if f.LinearizationFunc.IsNonLinear() {
				fr, err := c.GetSensorReadingFactors(ctx, num, rd.raw)
				if err != nil {
					rd.unavailable = true
				} else {
					f.ReadingFactors = fr.ReadingFactors
				}
			}
			r.value = f.ConvertReading(rd.raw)
		}
		out = append(out, sdrLine(r, rd))
	}
	return out
}

// isHP reports whether the BMC is HP's (IANA 11), for ipmitool's analog
// exception; HPE's iLO 5 (47196) is not covered by it, as in ipmitool.
func isHP(ctx context.Context, c *client.Client) bool {
	d, err := c.GetDeviceID(ctx)
	return err == nil && d.ManufacturerID == 11
}

func nativeSDR(host, user, pass string) ([]SDREntry, error) {
	var out []SDREntry
	err := withNative(host, user, pass, ipmiLongTimeout, func(ctx context.Context, c *client.Client) error {
		sdrs, err := readSDRRecords(ctx, c)
		if err != nil {
			return err
		}
		sdrCache.Store(host, sdrs)
		out = sdrEntries(ctx, c, sdrs, isHP(ctx, c), nil)
		return nil
	})
	return out, err
}

// nativeDumpSDR is DumpSDR's native form: the records are kept in memory
// (sdrCache) instead of a file, for nativeReadSensors.
func nativeDumpSDR(host, user, pass string) error {
	return withNative(host, user, pass, ipmiLongTimeout, func(ctx context.Context, c *client.Client) error {
		sdrs, err := readSDRRecords(ctx, c)
		if err == nil {
			sdrCache.Store(host, sdrs)
		}
		return err
	})
}

// nativeReadSensors is ReadSensors' native form, with ipmitool's "sensor
// reading" rules (ipmi_sensor_get_reading, lib/ipmi_sensor.c): each name
// finds its first record; a name with no record is an error ("Sensor
// \"x\" not found!"), reported after the readings that worked, as
// ipmitool does; a reading that is not valid or not analog is left out; the
// value is the converted reading, which ipmitool prints with up to three
// decimals.
func nativeReadSensors(host, user, pass string, names []string) ([]SensorReading, error) {
	var out []SensorReading
	var missing []string
	err := withNative(host, user, pass, sensorReadTimeout, func(ctx context.Context, c *client.Client) error {
		v, ok := sdrCache.Load(host)
		if !ok {
			sdrs, err := readSDRRecords(ctx, c)
			if err != nil {
				return err
			}
			sdrCache.Store(host, sdrs)
			v = sdrs
		}
		first := map[string]*types.SDR{}
		for _, s := range v.([]*types.SDR) {
			if r, _, _, ok := recordOf(s, false); ok {
				if _, seen := first[r.name]; !seen {
					first[r.name] = s
				}
			}
		}
		hp := isHP(ctx, c)
		for _, n := range names {
			s, ok := first[n]
			if !ok {
				missing = append(missing, n)
				continue
			}
			if val, ok := analogReading(ctx, c, s, hp); ok {
				out = append(out, SensorReading{Name: n, Value: val})
			}
		}
		return nil
	})
	if err == nil && len(missing) > 0 {
		err = fmt.Errorf("sensor %q not found", strings.Join(missing, `", "`))
	}
	return out, err
}

// analogReading reads one sensor's converted value, rounded to three
// decimals as ipmitool prints it; ok is false for no valid analog reading.
func analogReading(ctx context.Context, c *client.Client, s *types.SDR, hp bool) (float64, bool) {
	r, owner, num, ok := recordOf(s, hp)
	if !ok || !r.analog {
		return 0, false
	}
	rd := readSensor(ctx, c, owner, num)
	if rd == nil || rd.unavailable || rd.scanningDisabled {
		return 0, false
	}
	f := s.Full
	if f.LinearizationFunc.IsNonLinear() {
		fr, err := c.GetSensorReadingFactors(ctx, num, rd.raw)
		if err != nil {
			return 0, false
		}
		f.ReadingFactors = fr.ReadingFactors
	}
	return math.Round(f.ConvertReading(rd.raw)*1000) / 1000, true
}
