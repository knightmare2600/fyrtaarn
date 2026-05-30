package ipmi

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	// Short timeout for single-round-trip commands (mc info, chassis, lan).
	ipmiShortTimeout = 15 * time.Second
	// Long timeout for bulk-read commands that iterate the full SDR/SEL/FRU
	// repository — large BMCs with 60+ sensors can take 60–90 s over LAN.
	ipmiLongTimeout = 90 * time.Second
)

func runIPMICommand(
	host string,
	user string,
	pass string,
	timeout time.Duration,
	command ...string,
) (string, error) {

	defer wipeString(&pass)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := []string{
		"-I", "lanplus",
		"-H", host,
		"-U", user,
		"-P", pass,
	}

	args = append(args, command...)

	cmd := exec.CommandContext(ctx, "ipmitool", args...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("ipmitool timed out after %v — BMC may be slow or unreachable", timeout)
		}
		return "", fmt.Errorf("ipmitool: %s", strings.TrimSpace(stderr.String()))
	}

	return stdout.String(), nil
}

func GetMCInfo(host, user, pass string) (*MCInfo, error) {
	output, err := runIPMICommand(host, user, pass, ipmiShortTimeout, "mc", "info")
	if err != nil {
		return nil, err
	}
	return parseMCInfo(output), nil
}

func GetLANInfo(host, user, pass string) (*LANInfo, error) {
	output, err := runIPMICommand(host, user, pass, ipmiShortTimeout, "lan", "print")
	if err != nil {
		return nil, err
	}
	return parseLANInfo(output), nil
}

func GetChassisStatus(host, user, pass string) (*ChassisStatus, error) {
	output, err := runIPMICommand(host, user, pass, ipmiShortTimeout, "chassis", "status")
	if err != nil {
		return nil, err
	}
	return parseChassisStatus(output), nil
}

func GetFRU(host, user, pass string) ([]FRUEntry, error) {
	output, err := runIPMICommand(host, user, pass, ipmiShortTimeout, "fru")
	if err != nil {
		return nil, err
	}
	return parseFRU(output), nil
}

func GetSDR(host, user, pass string) ([]SDREntry, error) {
	output, err := runIPMICommand(host, user, pass, ipmiLongTimeout, "sdr", "list")
	if err != nil {
		return nil, err
	}
	return parseSDR(output), nil
}

func GetSEL(host, user, pass string) ([]SELEntry, error) {
	output, err := runIPMICommand(host, user, pass, ipmiLongTimeout, "sel", "list")
	if err != nil {
		return nil, err
	}
	return parseSEL(output), nil
}

func PowerOn(host, user, pass string) error {
	_, err := runIPMICommand(host, user, pass, ipmiShortTimeout, "chassis", "power", "on")
	return err
}

func PowerOff(host, user, pass string) error {
	_, err := runIPMICommand(host, user, pass, ipmiShortTimeout, "chassis", "power", "off")
	return err
}

func PowerReset(host, user, pass string) error {
	_, err := runIPMICommand(host, user, pass, ipmiShortTimeout, "chassis", "power", "reset")
	return err
}

func PowerSoft(host, user, pass string) error {
	_, err := runIPMICommand(host, user, pass, ipmiShortTimeout, "chassis", "power", "soft")
	return err
}

// SOLCmd returns an *exec.Cmd that activates a Serial-over-LAN session.
// Run it via tea.ExecProcess so the TUI suspends and hands the terminal to
// ipmitool. The user disconnects with the ipmitool escape sequence (~.).
// Credentials appear in the process argument list — this is an ipmitool
// limitation shared by all other commands in this package.
func SOLCmd(host, user, pass string) *exec.Cmd {
	return exec.Command("ipmitool",
		"-I", "lanplus",
		"-H", host,
		"-U", user,
		"-P", pass,
		"sol", "activate",
	)
}

func parseMCInfo(data string) *MCInfo {

	info := &MCInfo{}

	for _, line := range strings.Split(data, "\n") {

		parts := strings.SplitN(line, ":", 2)

		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "Device ID":
			info.DeviceID = value
		case "Firmware Revision":
			info.FirmwareRevision = value
		case "IPMI Version":
			info.IPMIVersion = value
		case "Manufacturer Name":
			info.ManufacturerName = value
		case "Product Name":
			info.ProductName = value
		}
	}

	return info
}

func parseLANInfo(data string) *LANInfo {

	info := &LANInfo{}

	for _, line := range strings.Split(data, "\n") {

		parts := strings.SplitN(line, ":", 2)

		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "IP Address":
			info.IPAddress = value
		case "MAC Address":
			info.MACAddress = value
		case "Subnet Mask":
			info.SubnetMask = value
		case "Default Gateway IP":
			info.Gateway = value
		}
	}

	return info
}

func parseChassisStatus(data string) *ChassisStatus {

	status := &ChassisStatus{}

	for _, line := range strings.Split(data, "\n") {

		parts := strings.SplitN(line, ":", 2)

		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		switch key {
		case "System Power":
			status.PowerOn = strings.ToLower(value) == "on"
		case "Power Overload":
			status.PowerOverload = strings.ToLower(value) == "true"
		case "Main Power Fault":
			status.PowerFault = strings.ToLower(value) == "true"
		case "Drive Fault":
			status.DriveFault = strings.ToLower(value) == "true"
		case "Cooling/Fan Fault":
			status.CoolingFault = strings.ToLower(value) == "true"
		}
	}

	return status
}

// parseFRU parses "ipmitool fru" colon-separated output.
// Lines that begin without leading whitespace and contain "FRU Device" are
// treated as section headers.
func parseFRU(data string) []FRUEntry {
	var entries []FRUEntry

	for _, line := range strings.Split(data, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		field := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if field == "" {
			continue
		}
		isHeader := !strings.HasPrefix(line, " ") && strings.Contains(field, "FRU Device")
		if value == "" && !isHeader {
			continue
		}
		entries = append(entries, FRUEntry{
			Field:    field,
			Value:    value,
			IsHeader: isHeader,
		})
	}

	return entries
}

// parseSDR parses "ipmitool sdr list" pipe-delimited output:
// Name | Value | Status
func parseSDR(data string) []SDREntry {

	var entries []SDREntry

	for _, line := range strings.Split(data, "\n") {

		parts := strings.SplitN(line, "|", 3)

		if len(parts) != 3 {
			continue
		}

		name := strings.TrimSpace(parts[0])

		if name == "" {
			continue
		}

		entries = append(entries, SDREntry{
			Name:   name,
			Value:  strings.TrimSpace(parts[1]),
			Status: strings.TrimSpace(parts[2]),
		})
	}

	return entries
}

// parseSEL parses "ipmitool sel list" pipe-delimited output:
// ID | Date | Time | Sensor | Event | Direction
func parseSEL(data string) []SELEntry {

	var entries []SELEntry

	for _, line := range strings.Split(data, "\n") {

		parts := strings.SplitN(line, "|", 6)

		if len(parts) < 5 {
			continue
		}

		id := strings.TrimSpace(parts[0])

		if id == "" {
			continue
		}

		direction := ""
		if len(parts) == 6 {
			direction = strings.TrimSpace(parts[5])
		}

		entries = append(entries, SELEntry{
			ID:        id,
			Timestamp: strings.TrimSpace(parts[1]) + " " + strings.TrimSpace(parts[2]),
			Event:     strings.TrimSpace(parts[3]) + ": " + strings.TrimSpace(parts[4]),
			Direction: direction,
		})
	}

	return entries
}

func wipeString(s *string) {

	if s == nil {
		return
	}

	runes := []rune(*s)

	for i := range runes {
		runes[i] = 0
	}

	*s = string(runes)
}
