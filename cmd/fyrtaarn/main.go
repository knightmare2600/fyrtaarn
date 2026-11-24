package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
	"github.com/knightmare2600/fyrtaarn/internal/ptyproc"
	"github.com/knightmare2600/fyrtaarn/internal/tui"
	"github.com/knightmare2600/fyrtaarn/internal/util"
)

var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

const usage = `Fyrtaarn: a terminal console for BMCs (IPMI and Redfish).

Usage: fyrtaarn [options]

Options:
  -h, --help      show this help and exit
  -v, --version   show the version, build and IPMI library, and exit
  -c, --check     check what fyrtaarn needs here (nmap, ipmitool, root,
                  pseudo-terminals) and exit; non-zero if nmap is missing
  -n, --native    IPMI through fyrtaarn's own client only, never ipmitool
  -i, --ipmitool  IPMI through ipmitool only

Without -n or -i, fyrtaarn uses its own IPMI client and falls back to
ipmitool for a BMC it cannot set up a session with. The environment
variable FYRTAARN_IPMI=native|ipmitool does the same; a flag overrides it.
`

// options is what the command line asked for.
type options struct {
	help, version, check, native, ipmitool bool
}

// parseArgs reads args (without the program name). Each option has a
// short and a long form; Go's flag package takes either with one or two
// dashes.
func parseArgs(args []string, stderr io.Writer) (options, error) {
	var o options
	fs := flag.NewFlagSet("fyrtaarn", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors are reported once, below
	for _, f := range []struct {
		p           *bool
		short, long string
	}{
		{&o.help, "h", "help"}, {&o.version, "v", "version"}, {&o.check, "c", "check"},
		{&o.native, "n", "native"}, {&o.ipmitool, "i", "ipmitool"},
	} {
		fs.BoolVar(f.p, f.short, false, "")
		fs.BoolVar(f.p, f.long, false, "")
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			o.help = true
			return o, nil
		}
		return o, err
	}
	if fs.NArg() > 0 {
		return o, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if o.native && o.ipmitool {
		return o, fmt.Errorf("-n and -i cannot be used together")
	}
	return o, nil
}

// ipmiLibrary names the go-ipmi this binary was built with, and the
// replacement if go.mod replaces it (fyrtaarn builds a fork from 0.9.1).
func ipmiLibrary() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, d := range bi.Deps {
		if d.Path != "github.com/bougou/go-ipmi" {
			continue
		}
		if d.Replace != nil {
			return fmt.Sprintf("%s %s (replacing %s %s)", d.Replace.Path, d.Replace.Version, d.Path, d.Version)
		}
		return d.Path + " " + d.Version
	}
	return "unknown"
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "Fyrtaarn %s (%s) built %s\n", Version, Commit, BuildDate)
	fmt.Fprintf(w, "%s %s/%s; IPMI library %s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH, ipmiLibrary())
}

// lookPath and toolVersion are variables so tests can stand in for them.
var lookPath = exec.LookPath

var toolVersion = func(path string, args ...string) string {
	out, _ := exec.Command(path, args...).CombinedOutput()
	for _, l := range strings.Split(string(out), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return "version not reported"
}

// check reports what fyrtaarn needs here and returns the exit status:
// 1 if nmap, without which nothing can be scanned, is missing; else 0.
func check(w io.Writer) int {
	status := 0
	line := func(mark, what, detail string) { fmt.Fprintf(w, "  [%s] %-12s %s\n", mark, what, detail) }
	fmt.Fprintf(w, "Fyrtaarn %s: checking %s/%s\n", Version, runtime.GOOS, runtime.GOARCH)

	if p, err := lookPath("nmap"); err == nil {
		line("ok", "nmap", p+": "+toolVersion(p, "--version"))
	} else {
		line("!!", "nmap", "not found in PATH: required for discovery scans")
		status = 1
	}
	if p, err := lookPath("ipmitool"); err == nil {
		line("ok", "ipmitool", p+": "+toolVersion(p, "-V"))
	} else {
		line("--", "ipmitool", "not found in PATH: optional, fyrtaarn's own IPMI is used (no fallback)")
	}
	switch {
	case runtime.GOOS == "windows":
		line("--", "privileges", "not checked on Windows: scans use TCP connect only, as when not root")
	// What changes is in discovery.buildArgs: -sS -sU --script ipmi-version
	// as root, -sT (TCP connect) otherwise.
	case util.IsRoot():
		line("ok", "privileges", "root: scans use SYN and UDP 623 with nmap's ipmi-version probe")
	default:
		line("--", "privileges", "not root: scans use TCP connect only, without UDP 623 and its ipmi-version probe")
	}
	if ptyproc.Available() {
		line("ok", "pty", "pseudo-terminals available (only the ipmitool SOL fallback needs them)")
	} else {
		line("--", "pty", "no ConPTY: the ipmitool SOL fallback cannot run; fyrtaarn's own SOL still works")
	}
	switch b := strings.ToLower(strings.TrimSpace(os.Getenv(ipmi.BackendEnv))); b {
	case "native", "ipmitool":
		line("ok", "IPMI", ipmi.BackendEnv+"="+b)
	case "":
		line("ok", "IPMI", "fyrtaarn's own, ipmitool as fallback")
	default:
		line("--", "IPMI", fmt.Sprintf("%s=%q is not native or ipmitool: the default is used", ipmi.BackendEnv, b))
	}
	fmt.Fprintf(w, "  IPMI library: %s\n", ipmiLibrary())
	return status
}

// run handles the command line; start runs the TUI. It returns the exit
// status, or -1 to start the TUI.
func run(args []string, stdout, stderr io.Writer) int {
	o, err := parseArgs(args, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "fyrtaarn: %v\n\n%s", err, usage)
		return 2
	}
	switch {
	case o.help:
		fmt.Fprint(stdout, usage)
		return 0
	case o.version:
		printVersion(stdout)
		return 0
	case o.check:
		return check(stdout)
	}
	if o.native {
		os.Setenv(ipmi.BackendEnv, "native")
	}
	if o.ipmitool {
		os.Setenv(ipmi.BackendEnv, "ipmitool")
	}
	return -1
}

func main() {
	if code := run(os.Args[1:], os.Stdout, os.Stderr); code >= 0 {
		os.Exit(code)
	}

	// Startup banner, before the alternate screen (useful for debugging).
	fmt.Printf("Fyrtaarn %s (%s) built %s\n", Version, Commit, BuildDate)

	app := tui.NewApp()
	app.Version = Version
	app.Commit = Commit
	app.BuildDate = BuildDate

	p := tea.NewProgram(
		app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
