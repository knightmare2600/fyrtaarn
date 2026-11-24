package tui

import (
	"os"
	"testing"
)

// TestMain keeps the tests on ipmitool: they replace it with a fake script
// on PATH and use made-up addresses, which the native IPMI backend would
// otherwise try to reach over UDP first. Native IPMI has its own tests.
func TestMain(m *testing.M) {
	os.Setenv("FYRTAARN_IPMI", "ipmitool")
	os.Exit(m.Run())
}
