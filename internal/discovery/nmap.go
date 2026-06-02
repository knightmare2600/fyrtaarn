package discovery

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"

	"github.com/knightmare2600/fyrtaarn/internal/util"
)

// StreamEvent represents a live update emitted while nmap is running.
// Type is one of:
//
//	"host"     — a host element closed in the XML (Data is the raw line)
//	"progress" — nmap --stats-every percentage (Data is e.g. "39.06")
//	"log"      — raw stderr line for status display
type StreamEvent struct {
	Type string
	Data string
}

// CIDRHostCount returns the total number of IP addresses in the given CIDR,
// including network and broadcast addresses (e.g. 256 for a /24).
// Returns 0 if the CIDR cannot be parsed.
func CIDRHostCount(cidr string) int {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return 0
	}
	ones, bits := network.Mask.Size()
	hostBits := bits - ones
	if hostBits >= 32 {
		return 0
	}
	return 1 << uint(hostBits)
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
// When events is non-nil it is closed after both I/O goroutines finish,
// allowing callers to range over it safely.
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

	var wg sync.WaitGroup
	wg.Add(2)

	// stdout carries the XML output. When -oX - is used nmap also writes its
	// --stats-every progress lines here, interleaved with the XML. Intercept
	// those lines before they reach xmlBuf so the XML parser stays clean.
	go func() {
		defer wg.Done()
		defer close(hostChan)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if events != nil {
				if pct, ok := parseProgress(line); ok {
					events <- StreamEvent{Type: "progress", Data: pct}
					continue // do not add stats lines to the XML buffer
				}
				if strings.Contains(line, "</host>") {
					events <- StreamEvent{Type: "host", Data: line}
				}
			}
			xmlBuf.WriteString(line + "\n")
		}
	}()

	// stderr carries nmap's interactive output in some configurations. Keep
	// the progress check here as a fallback for nmap builds that behave
	// differently, but don't forward bare log lines — they are noisy.
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			if events == nil {
				continue
			}
			line := scanner.Text()
			if pct, ok := parseProgress(line); ok {
				events <- StreamEvent{Type: "progress", Data: pct}
			}
		}
	}()

	err = cmd.Wait()

	// Wait for both goroutines to drain their pipes before closing events,
	// ensuring no sends race against the close.
	wg.Wait()
	if events != nil {
		close(events)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("nmap failed: %w", err)
	}

	results, err := ParseNmapXML(&xmlBuf)
	if err != nil {
		return nil, nil, err
	}

	return hostChan, results, nil
}

// parseProgress extracts the percentage string from a nmap --stats-every line.
// Returns ("39.06", true) for a line containing "About 39.06% done".
func parseProgress(line string) (string, bool) {
	i := strings.Index(line, "About ")
	if i < 0 {
		return "", false
	}
	rest := line[i+6:]
	j := strings.Index(rest, "% done")
	if j < 0 {
		return "", false
	}
	return strings.TrimSpace(rest[:j]), true
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
		"--stats-every", "2s",
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
