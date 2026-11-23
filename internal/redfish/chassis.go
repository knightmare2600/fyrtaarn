package redfish

import (
	"fmt"
	"strconv"
	"strings"
)

// Sensor readings over Redfish, for BMCs used without IPMI: each Chassis's
// Thermal (temperatures, fans) and Power (voltages, consumption, supplies)
// resources. Field names vary: iLO 4 (FW 2.54) names fans with FanName,
// CurrentReading and Units where the schema has Name, Reading and
// ReadingUnits; iDRAC9 sends both name forms; iLO 5 lists every possible
// temperature sensor, absent ones included (seen in captures of an iDRAC9
// 4.20, an iLO 4 2.54 and an iLO 5 1.40).

// ChassisSensor is one reading, shaped like an "sdr list" line so the
// Sensors screen can show either.
type ChassisSensor struct {
	Name   string
	Value  string // number and unit, e.g. "42 degrees C"
	Status string // "ok", "warning", "critical" or the BMC's own word
}

// GetChassisSensors reads Thermal and Power of every Chassis.
func GetChassisSensors(host, user, pass string) ([]ChassisSensor, error) {
	c := httpClient()
	base := "https://" + host
	col, err := fetchJSON(c, base+"/redfish/v1/Chassis", user, pass)
	if err != nil {
		return nil, err
	}
	var out []ChassisSensor
	for _, p := range collectionPaths(col) {
		ch, err := fetchJSON(c, base+p, user, pass)
		if err != nil {
			continue
		}
		if l := link(ch, "Thermal"); l != "" {
			if th, err := fetchJSON(c, base+l, user, pass); err == nil {
				out = append(out, thermalSensors(th)...)
			}
		}
		if l := link(ch, "Power"); l != "" {
			if pw, err := fetchJSON(c, base+l, user, pass); err == nil {
				out = append(out, powerSensors(pw)...)
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no Thermal or Power readings in this BMC's Redfish Chassis resources")
	}
	return out, nil
}

func link(obj map[string]any, key string) string {
	if m, ok := obj[key].(map[string]any); ok {
		s, _ := m["@odata.id"].(string)
		return s
	}
	return ""
}

func items(obj map[string]any, key string) []map[string]any {
	l, _ := jsonArrayOf(obj, key)
	return l
}

// num returns the first of keys holding a number.
func num(obj map[string]any, keys ...string) (float64, bool) {
	for _, k := range keys {
		if v, ok := obj[k].(float64); ok {
			return v, true
		}
	}
	return 0, false
}

func firstStr(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := str(obj, k); s != "" {
			return s
		}
	}
	return ""
}

// health maps Status to an "sdr list"-style word; absent is reported so the
// caller can skip the item.
func health(obj map[string]any) (word string, absent bool) {
	st, _ := obj["Status"].(map[string]any)
	if state, _ := st["State"].(string); state == "Absent" {
		return "", true
	}
	switch h, _ := st["Health"].(string); h {
	case "OK":
		return "ok", false
	case "Warning":
		return "warning", false
	case "Critical":
		return "critical", false
	case "":
		return "", false
	default:
		return strings.ToLower(h), false
	}
}

func fmtNum(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func thermalSensors(th map[string]any) []ChassisSensor {
	var out []ChassisSensor
	for _, t := range items(th, "Temperatures") {
		v, ok := num(t, "ReadingCelsius", "CurrentReading")
		h, absent := health(t)
		if absent || !ok {
			continue
		}
		out = append(out, ChassisSensor{Name: firstStr(t, "Name"), Value: fmtNum(v) + " degrees C", Status: h})
	}
	for _, f := range items(th, "Fans") {
		v, ok := num(f, "Reading", "CurrentReading")
		h, absent := health(f)
		if absent || !ok {
			continue
		}
		unit := firstStr(f, "ReadingUnits", "Units")
		if unit == "Percent" {
			unit = "percent"
		}
		out = append(out, ChassisSensor{Name: firstStr(f, "Name", "FanName"), Value: strings.TrimSpace(fmtNum(v) + " " + unit), Status: h})
	}
	return out
}

func powerSensors(pw map[string]any) []ChassisSensor {
	var out []ChassisSensor
	for _, v := range items(pw, "Voltages") {
		r, ok := num(v, "ReadingVolts")
		h, absent := health(v)
		if absent || !ok {
			continue
		}
		out = append(out, ChassisSensor{Name: firstStr(v, "Name"), Value: fmtNum(r) + " Volts", Status: h})
	}
	for i, pc := range items(pw, "PowerControl") {
		w, ok := num(pc, "PowerConsumedWatts")
		if !ok {
			continue
		}
		h, _ := health(pc)
		name := firstStr(pc, "Name")
		if name == "" {
			name = fmt.Sprintf("Power Consumption %d", i+1)
		}
		out = append(out, ChassisSensor{Name: name, Value: fmtNum(w) + " Watts", Status: h})
	}
	for i, ps := range items(pw, "PowerSupplies") {
		h, absent := health(ps)
		if absent {
			continue
		}
		name := firstStr(ps, "Name")
		if name == "" {
			name = fmt.Sprintf("Power Supply %d", i+1)
		}
		if w, ok := num(ps, "LastPowerOutputWatts", "PowerOutputWatts"); ok {
			out = append(out, ChassisSensor{Name: name + " Output", Value: fmtNum(w) + " Watts", Status: h})
		}
	}
	return out
}
