package ipmi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeIPMITool installs a stand-in "ipmitool" first on PATH. It records its
// argv and the password it received via -E into dir, and for
// "user set password <id>" prompts twice with Python's getpass, which, like
// glibc's, flushes pending tty input (TCSAFLUSH) before each prompt — so an
// answer written before its prompt would be lost and the test would fail.
func fakeIPMITool(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat("/usr/bin/env"); err != nil {
		t.Skip("needs /usr/bin/env")
	}
	dir := t.TempDir()
	script := `#!/usr/bin/env python3
import getpass, os, sys
d = os.environ["FAKE_DIR"]
open(os.path.join(d, "argv"), "w").write("\0".join(sys.argv))
open(os.path.join(d, "envpass"), "w").write(os.environ.get("IPMITOOL_PASSWORD", "<unset>"))
if os.environ.get("FAKE_NO_CIPHER_LIST") and "-C" not in sys.argv:
    # iLO 4 does not answer Get Channel Cipher Suites; ipmitool 1.8.19+
    # prints this, then falls back to suite 3 (lanplus.c).
    print("Unable to Get Channel Cipher Suites", file=sys.stderr)
if os.environ.get("FAKE_NO_SUITE") and "-C" not in sys.argv:
    # The Quanta (AMI FW 7.54) refuses ipmitool 1.8.19's own suite choice.
    with open(os.path.join(d, "refused"), "a") as f:
        f.write("x")
    print("Error in open session response message : no matching cipher suite", file=sys.stderr)
    sys.exit(1)
if "reading" in sys.argv:
    # ipmitool "sensor reading": printf "%-16s | %s" per sensor; an unknown
    # name is reported on stderr and makes the exit status 1.
    rc = 0
    for n in sys.argv[sys.argv.index("reading") + 1:]:
        if n == "Missing":
            print('Sensor "%s" not found!' % n, file=sys.stderr)
            rc = 1
        else:
            print("%-16s | %s" % (n, {"Inlet Temp": "26.000", "Fan1 RPM": "4800"}.get(n, "1.5")))
    sys.exit(rc)
if os.environ.get("FAKE_FAIL"):
    print("Error: Unable to establish IPMI v2 / RMCP+ session", file=sys.stderr)
    sys.exit(1)
if sys.argv[-4:-1] == ["user", "set", "password"]:
    uid = sys.argv[-1]
    a = getpass.getpass("Password for user %s: " % uid)
    b = getpass.getpass("Password for user %s: " % uid)
    if a != b:
        print("Passwords do not match.")
        sys.exit(1)
    open(os.path.join(d, "newpass"), "w").write(a)
    print("Set User Password command successful (user %s)" % uid)
else:
    print("ok")
`
	if err := os.WriteFile(filepath.Join(dir, "ipmitool"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_DIR", dir)
	return dir
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

const trickyPass = `K^<9L"t D)#Z'-6u*m?`

func TestSessionPasswordNotInArgv(t *testing.T) {
	dir := fakeIPMITool(t)
	// Inherited values must not override ours: -E prefers IPMITOOL_PASSWORD.
	t.Setenv("IPMITOOL_PASSWORD", "inherited-wrong")
	t.Setenv("IPMI_PASSWORD", "inherited-wrong-too")

	if _, err := runIPMICommand("10.0.0.1", "root", trickyPass, ipmiShortTimeout, "mc", "info"); err != nil {
		t.Fatal(err)
	}
	argv := readFile(t, filepath.Join(dir, "argv"))
	if strings.Contains(argv, trickyPass) || strings.Contains(argv, "-P") {
		t.Errorf("password or -P in argv: %q", argv)
	}
	if !strings.Contains(argv, "\x00-E\x00") {
		t.Errorf("-E missing from argv: %q", argv)
	}
	if got := readFile(t, filepath.Join(dir, "envpass")); got != trickyPass {
		t.Errorf("IPMITOOL_PASSWORD = %q, want %q", got, trickyPass)
	}
}

func TestSOLCmdPasswordNotInArgv(t *testing.T) {
	cmd := SOLCmd("10.0.0.1", "root", trickyPass)
	if strings.Contains(strings.Join(cmd.Args, " "), trickyPass) {
		t.Errorf("password in SOL argv: %q", cmd.Args)
	}
	found := false
	for _, kv := range cmd.Env {
		if kv == "IPMITOOL_PASSWORD="+trickyPass {
			found = true
		}
	}
	if !found {
		t.Error("SOL command lacks IPMITOOL_PASSWORD in its environment")
	}
}

func TestSetUserPasswordViaPrompts(t *testing.T) {
	dir := fakeIPMITool(t)
	newPass := `n3w#pa ss"'x`
	if err := SetUserPassword("10.0.0.1", "root", trickyPass, 3, newPass); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "newpass")); got != newPass {
		t.Errorf("BMC received new password %q, want %q", got, newPass)
	}
	argv := readFile(t, filepath.Join(dir, "argv"))
	if strings.Contains(argv, newPass) || strings.Contains(argv, trickyPass) {
		t.Errorf("a password reached argv: %q", argv)
	}
}

func TestPromptedFailureReported(t *testing.T) {
	fakeIPMITool(t)
	t.Setenv("FAKE_FAIL", "1")
	err := SetUserPassword("10.0.0.1", "root", "x", 3, "y")
	if err == nil || !strings.Contains(err.Error(), "RMCP+ session") {
		t.Errorf("err = %v, want the ipmitool error line", err)
	}
}

// TestPromptedFallbackWithoutPty covers the Windows path: no pty, so the
// answer goes as the final argument. Also guards against deadlocking on the
// per-host lock, which the fallback must not hold while calling
// runIPMICommand.
func TestPromptedFallbackWithoutPty(t *testing.T) {
	dir := fakeIPMITool(t)
	old := ptySupported
	ptySupported = false
	t.Cleanup(func() { ptySupported = old })

	done := make(chan error, 1)
	go func() { done <- SetUserPassword("10.0.0.1", "root", "admin-pw", 3, "fallback-pw") }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("fallback deadlocked")
	}
	argv := readFile(t, filepath.Join(dir, "argv"))
	if !strings.HasSuffix(argv, "\x00password\x003\x00fallback-pw") {
		t.Errorf("fallback argv = %q", argv)
	}
	if strings.Contains(argv, "admin-pw") {
		t.Errorf("session password in argv: %q", argv)
	}
}

// TestCipherSuite3Fallback checks a host that cannot list its cipher suites
// (iLO 4) gets -C 3 from the second call on, and other hosts never do.
func TestCipherSuite3Fallback(t *testing.T) {
	dir := fakeIPMITool(t)
	hasC3 := func() bool { return strings.Contains(readFile(t, filepath.Join(dir, "argv")), "\x00-C\x003\x00") }

	if _, err := runIPMICommand("10.9.9.1", "u", "p", 5*time.Second, "mc", "info"); err != nil {
		t.Fatal(err)
	}
	if hasC3() {
		t.Fatal("-C 3 passed before any cipher-list failure was seen")
	}

	t.Setenv("FAKE_NO_CIPHER_LIST", "1")
	const ilo = "10.9.9.2"
	t.Cleanup(func() { suite3Hosts.Delete(ilo) })
	if _, err := runIPMICommand(ilo, "u", "p", 5*time.Second, "mc", "info"); err != nil {
		t.Fatal(err)
	}
	if hasC3() {
		t.Fatal("first call already had -C 3")
	}
	if _, err := runIPMICommand(ilo, "u", "p", 5*time.Second, "mc", "info"); err != nil {
		t.Fatal(err)
	}
	if !hasC3() {
		t.Fatalf("second call to %s lacks -C 3: %q", ilo, readFile(t, filepath.Join(dir, "argv")))
	}
	if got := readFile(t, filepath.Join(dir, "argv")); !strings.HasSuffix(got, "\x00-E\x00-C\x003\x00mc\x00info") {
		t.Fatalf("-C 3 not before the command: %q", got)
	}

	if _, err := runIPMICommand("10.9.9.1", "u", "p", 5*time.Second, "mc", "info"); err != nil {
		t.Fatal(err)
	}
	if hasC3() {
		t.Fatal("unaffected host got -C 3")
	}
}

// TestNoMatchingSuiteRetry checks a BMC that refuses ipmitool's own suite
// choice is retried once with -C 3, and kept on it afterwards.
func TestNoMatchingSuiteRetry(t *testing.T) {
	dir := fakeIPMITool(t)
	t.Setenv("FAKE_NO_SUITE", "1")
	const host = "10.9.9.4"
	t.Cleanup(func() { suite3Hosts.Delete(host) })

	out, err := runIPMICommand(host, "u", "p", 5*time.Second, "mc", "info")
	if err != nil || strings.TrimSpace(out) != "ok" {
		t.Fatalf("retry did not succeed: %q %v", out, err)
	}
	if got := readFile(t, filepath.Join(dir, "refused")); got != "x" {
		t.Fatalf("refusals %q, want exactly one", got)
	}
	if _, err := runIPMICommand(host, "u", "p", 5*time.Second, "mc", "info"); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(dir, "refused")); got != "x" {
		t.Fatalf("second call was refused again: %q", got)
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "argv")), "\x00-C\x003\x00") {
		t.Fatal("second call lacks -C 3")
	}
}

// TestSOLSetCommands checks the exact ipmitool commands for switching SOL
// and a user's SOL payload access (syntax from ipmi_sol.c, channel 14).
func TestSOLSetCommands(t *testing.T) {
	dir := fakeIPMITool(t)
	argv := func() string { return readFile(t, filepath.Join(dir, "argv")) }
	if err := SetSOLEnabled("10.9.9.5", "u", "p", true); err != nil || !strings.HasSuffix(argv(), "\x00sol\x00set\x00enabled\x00true\x0014") {
		t.Fatalf("enable: %v %q", err, argv())
	}
	if err := SetSOLEnabled("10.9.9.5", "u", "p", false); err != nil || !strings.HasSuffix(argv(), "\x00enabled\x00false\x0014") {
		t.Fatalf("disable: %v %q", err, argv())
	}
	if err := SetSOLPayload("10.9.9.5", "u", "p", 3, true); err != nil || !strings.HasSuffix(argv(), "\x00sol\x00payload\x00enable\x0014\x003") {
		t.Fatalf("payload: %v %q", err, argv())
	}
	if err := SetSOLPayload("10.9.9.5", "u", "p", 0, true); err == nil {
		t.Fatal("user ID 0 accepted")
	}
}
