package discovery

import (
  "bufio"
  "bytes"
  "fmt"
  "os/exec"

  "github.com/knightmare2600/fyrtaarn/internal/util"
)

// StreamEvent represents live scan updates (future UI hook)
type StreamEvent struct {
  Type string // "host", "progress", "log"
  Data string
}

// RunScan is now a SAFE wrapper around streaming scan
func RunScan(subnet string) ([]HostResult, error) {
  _, results, err := RunScanStream(subnet, nil)
  return results, err
}

// RunScanStream runs nmap and optionally streams events
func RunScanStream(
  subnet string,
  events chan<- StreamEvent,
) (chan HostResult, []HostResult, error) {

  args := buildArgs(subnet)

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

  // buffer full XML for final parsing (unchanged behaviour)
  var xmlBuf bytes.Buffer

  hostChan := make(chan HostResult, 100)

  go func() {
    defer close(hostChan)

    scanner := bufio.NewScanner(stdout)

    for scanner.Scan() {
      line := scanner.Text()

      xmlBuf.WriteString(line + "\n")

      // lightweight heuristic events (safe, non-breaking)
      if events != nil {
        if containsHostUp(line) {
          events <- StreamEvent{
            Type: "log",
            Data: "nmap output: " + line,
          }
        }
      }
    }
  }()

  // consume stderr (optional debug channel)
  go func() {
    scanner := bufio.NewScanner(stderr)
    for scanner.Scan() {
      if events != nil {
        events <- StreamEvent{
          Type: "log",
          Data: scanner.Text(),
        }
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

// buildArgs centralises privilege logic
func buildArgs(subnet string) []string {

    args := []string{
        "-T4",
        "-n",
        "-Pn",
        "--open",
        "-p", "623,443,80",
        "-oX", "-",
    }

    if util.IsRoot() {

        return append(
            []string{
                "-sS",
                "-sU",
            },
            append(args, subnet)...,
        )
    }

    return append(
        []string{
            "-sT",
        },
        append(args, subnet)...,
    )
}

// tiny heuristic helper (safe parsing, no assumptions)
func containsHostUp(line string) bool {
  return len(line) > 0 && (line[0] == '<' || line[0] == '#')
}

