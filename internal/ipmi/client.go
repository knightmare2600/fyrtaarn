package ipmi

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/creack/pty"
)

const (
	// Short timeout for single-round-trip commands (mc info, chassis, lan).
	ipmiShortTimeout = 15 * time.Second
	// Long timeout for bulk-read commands that iterate the full SDR/SEL/FRU
	// repository — large BMCs with 60+ sensors can take 60–90 s over LAN.
	ipmiLongTimeout = 90 * time.Second
)

// hostLocks serialises ipmitool invocations per BMC (host -> *sync.Mutex).
// BMCs have small RMCP+ session tables and some refuse overlapping session
// set-ups outright: an iDRAC9 (FW 4.20) given mc info, lan print and chassis
// status at once accepted one and answered the other two with "insufficient
// resources for session". One session at a time per host is the only
// ordering every BMC accepts.
var hostLocks sync.Map

// lockHost blocks until no other ipmitool command is running against host
// and returns the unlock function.
func lockHost(host string) func() {
	m, _ := hostLocks.LoadOrStore(host, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func runIPMICommand(
	host string,
	user string,
	pass string,
	timeout time.Duration,
	command ...string,
) (string, error) {

	defer wipeString(&pass)

	// Take the host lock before starting the timeout, so time spent queued
	// behind another command does not count against this one.
	defer lockHost(host)()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := ipmitoolCmd(ctx, host, user, pass, command...)

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

// ipmitoolCmd builds an ipmitool lanplus command for host. The session
// password goes in the child's environment (-E) rather than in argv: argv
// is world-readable (ps, /proc/<pid>/cmdline) for as long as the command
// runs, the environment only to the same user and root. ipmitool's -E reads
// IPMITOOL_PASSWORD before IPMI_PASSWORD (ipmi_main.c), so set the former
// and drop any inherited copy of either, which would otherwise win.
func ipmitoolCmd(ctx context.Context, host, user, pass string, command ...string) *exec.Cmd {
	args := append([]string{
		"-I", "lanplus",
		"-H", host,
		"-U", user,
		"-E",
	}, command...)
	cmd := exec.CommandContext(ctx, "ipmitool", args...)

	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "IPMITOOL_PASSWORD=") || strings.HasPrefix(kv, "IPMI_PASSWORD=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env, "IPMITOOL_PASSWORD="+pass)
	return cmd
}

// ptySupported reports whether creack/pty can start processes here; it has
// no Windows implementation.
var ptySupported = runtime.GOOS != "windows"

// runIPMIPrompted runs an ipmitool command that reads secrets interactively
// (getpass on /dev/tty), answering each prompt in turn, so the secrets never
// appear in argv. It runs in a pty because getpass needs a controlling
// terminal. Each answer is written only once its prompt has appeared:
// getpass flushes pending tty input (TCSAFLUSH) before printing the prompt,
// so an answer written early would be discarded. Checked in source: glibc
// misc/getpass.c, and Apple Libc gen/FreeBSD/readpassphrase.c (macOS
// getpass wraps readpassphrase) — both flush, then prompt.
func runIPMIPrompted(
	host, user, pass string,
	timeout time.Duration,
	answers []string,
	command ...string,
) (string, error) {
	if !ptySupported {
		// No pty here (creack/pty has no Windows/ConPTY support), so getpass
		// would have no terminal. Pass the answer as the final argument
		// instead — on this platform only, as every platform did before
		// prompted input existed. Checked before taking the host lock,
		// which runIPMICommand takes itself.
		return runIPMICommand(host, user, pass, timeout, append(command, answers[0])...)
	}

	defer wipeString(&pass)
	defer lockHost(host)()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := ipmitoolCmd(ctx, host, user, pass, command...)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return "", fmt.Errorf("ipmitool: %w", err)
	}

	// Reader: ends with EIO once ipmitool exits and the slave side closes.
	var (
		mu  sync.Mutex
		out bytes.Buffer
	)
	more := make(chan struct{}, 1)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				mu.Lock()
				out.Write(buf[:n])
				mu.Unlock()
				select {
				case more <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	output := func() string {
		mu.Lock()
		defer mu.Unlock()
		return out.String()
	}

	// ipmitool prompts "Password for user N: " for each answer. waitPrompts
	// reports whether n prompts have appeared; false if ipmitool exited or
	// the timeout passed first.
	waitPrompts := func(n int) bool {
		for strings.Count(output(), "assword") < n {
			select {
			case <-more:
			case <-readDone:
				return strings.Count(output(), "assword") >= n
			case <-ctx.Done():
				return false
			}
		}
		return true
	}
	for i, ans := range answers {
		if !waitPrompts(i + 1) {
			break
		}
		_, _ = io.WriteString(ptmx, ans+"\n")
	}

	waitErr := cmd.Wait()
	<-readDone
	_ = ptmx.Close()

	if waitErr != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("ipmitool timed out after %v — BMC may be slow or unreachable", timeout)
		}
		return "", fmt.Errorf("ipmitool: %s", lastLine(output()))
	}
	return output(), nil
}

// lastLine returns the last non-blank line of s.
func lastLine(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return "unknown error"
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

// SOLEscapeChar is the ipmitool SOL escape character fyrtaarn starts
// sessions with (-e). Ctrl+] cannot be produced by the TUI's key mapping, so
// a user typing ipmitool's default "~." at the start of a line no longer
// kills the session by accident, and the TUI can still send escape + '.'
// itself to ask ipmitool for a clean exit.
const SOLEscapeChar byte = 0x1d

// SOLDeactivate tells the BMC to tear down the active SOL payload (instance
// 1), whichever session holds it. Most BMCs allow only one SOL session at a
// time. ipmitool only deactivates the payload itself when it exits via the
// escape sequence or loses stdin; killed by a signal it leaves the payload
// marked active, and every following "sol activate" then fails with "SOL
// payload already active on another session".
func SOLDeactivate(host, user, pass string) error {
	_, err := runIPMICommand(host, user, pass, ipmiShortTimeout, "sol", "deactivate")
	return err
}

// SOLCmd returns an *exec.Cmd that activates a Serial-over-LAN session.
// The TUI runs it inside a pty (see solPane) and renders its output itself;
// the user disconnects with F10, which ends ipmitool via its escape sequence
// or SIGINT (see solPane.shutdown) so the BMC-side session is released.
// The password travels in the environment, not argv (see ipmitoolCmd).
func SOLCmd(host, user, pass string) *exec.Cmd {
	return ipmitoolCmd(context.Background(), host, user, pass,
		"-e", string([]byte{SOLEscapeChar}),
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
			status.PowerStateFound = true
			status.PowerOn = strings.HasPrefix(strings.ToLower(value), "on")
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

// parseSEL parses "ipmitool sel list" pipe-delimited output. Record shapes,
// from ipmitool 1.8.18 lib/ipmi_sel.c (ipmi_sel_print_std_entry):
//
//	standard:    ID | Date | Time | Sensor | Description | Direction
//	  threshold/ECC events add a field:  ... | Direction | Reading 47 > Threshold 45 degrees C
//	  pre-init timestamps:               ID |  Pre-Init  |0000000012| Sensor | ...
//	OEM, timestamped (0xC0–0xDF):        ID | Date | Time | OEM record c1 | Manufacturer | Data
//	OEM, no timestamp (0xE0–0xFF):       ID | OEM record e0 | Data
//	kernel panic (0xF0):                 ID | Linux kernel panic: ...
//
// Every shape keeps its ID; extra detail goes into Event so Direction stays
// exactly "Asserted"/"Deasserted" for colouring.
func parseSEL(data string) []SELEntry {
	var entries []SELEntry
	for _, line := range strings.Split(data, "\n") {
		raw := strings.Split(line, "|")
		if len(raw) < 2 {
			continue
		}
		parts := make([]string, len(raw))
		for i, p := range raw {
			parts[i] = strings.TrimSpace(p)
		}
		id := parts[0]
		if id == "" {
			continue
		}
		e := SELEntry{ID: id}
		switch {
		case len(parts) == 2: // kernel panic
			e.Event = parts[1]
		case strings.HasPrefix(parts[1], "OEM record"): // OEM, no timestamp
			e.Event = parts[1] + ": " + strings.Join(parts[2:], " ")
		case len(parts) >= 5 && strings.HasPrefix(parts[3], "OEM record"): // OEM, timestamped
			e.Timestamp = parts[1] + " " + parts[2]
			e.Event = parts[3] + ": " + strings.Join(parts[4:], " ")
		case len(parts) >= 5: // standard
			e.Timestamp = parts[1] + " " + parts[2]
			e.Event = parts[3] + ": " + parts[4]
			if len(parts) >= 6 {
				e.Direction = parts[5]
			}
			if len(parts) >= 7 {
				e.Event += " (" + strings.Join(parts[6:], " ") + ")"
			}
		default:
			continue
		}
		entries = append(entries, e)
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
