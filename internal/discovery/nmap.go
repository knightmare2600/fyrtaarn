package discovery

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"

	"github.com/knightmare2600/fyrtaarn/internal/util"
)

// StreamEvent represents live scan updates (future UI hook).
type StreamEvent struct {
	Type string // "host", "progress", "log"
	Data string
}

// ScanProfile selects the nmap timing and port set.
type ScanProfile string

const (
	ProfileQuick    ScanProfile = "quick"    // -T5, IPMI port only
	ProfileStandard ScanProfile = "standard" // -T4, IPMI + web ports
	ProfileDeep     ScanProfile = "deep"     // -T3, extended port set
	ProfileCustom   ScanProfile = "custom"   // -T4, user-supplied port list
)

// RunScan is a convenience wrapper that blocks until the scan completes.
// customPorts is only used when profile == ProfileCustom; pass "" otherwise.
func RunScan(subnet string, profile ScanProfile, customPorts string) ([]HostResult, error) {
	_, results, err := RunScanStream(subnet, profile, customPorts, nil)
	return results, err
}

// RunScanStream runs nmap and optionally streams events to the caller.
func RunScanStream(
	subnet string,
	profile ScanProfile,
	customPorts string,
	events chan<- StreamEvent,
) (chan HostResult, []HostResult, error) {

	args := buildArgs(subnet, profile, customPorts)

	cmd := exec.Command("nmap", args...)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	var xmlBuf bytes.Buffer

	hostChan := make(chan HostResult, 100)

	go func() {
		defer close(hostChan)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			xmlBuf.WriteString(line + "\n")
			if events != nil && containsHostUp(line) {
				events <- StreamEvent{Type: "log", Data: "nmap: " + line}
			}
		}
	}()

	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if events != nil {
				events <- StreamEvent{Type: "log", Data: scanner.Text()}
			}
		}
	}()

	err = cmd.Wait()
	if err != nil {
		return nil, nil, fmt.Errorf("nmap failed: %w", err)
	}

	results, err := ParseNmapXML(&xmlBuf)
	if err != nil {
		return nil, nil, err
	}

	return hostChan, results, nil
}

func buildArgs(subnet string, profile ScanProfile, customPorts string) []string {
	timing := "-T4"
	ports := "623,443,80"

	switch profile {
	case ProfileQuick:
		timing = "-T5"
		ports = "623"
	case ProfileDeep:
		timing = "-T3"
		ports = "623,443,80,22,8080,8443,5900"
	case ProfileCustom:
		if customPorts != "" {
			ports = customPorts
		}
	}

	args := []string{
		timing,
		"-n",
		"-Pn",
		"--open",
		"-p", ports,
		"-oX", "-",
	}

	if util.IsRoot() {
		// ipmi-version NSE script fingerprints IPMI over UDP 623 — only
		// useful when we can actually scan UDP (i.e. running as root).
		return append(
			[]string{"-sS", "-sU", "--script", "ipmi-version"},
			append(args, subnet)...,
		)
	}

	return append([]string{"-sT"}, append(args, subnet)...)
}

func containsHostUp(line string) bool {
	return len(line) > 0 && (line[0] == '<' || line[0] == '#')
}
