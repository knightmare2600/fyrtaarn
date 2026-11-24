package ipmi

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/bougou/go-ipmi/pkg/bmc"
	"github.com/bougou/go-ipmi/pkg/client"
	"github.com/bougou/go-ipmi/pkg/clock"
	"github.com/bougou/go-ipmi/pkg/hal/mock"
	"github.com/bougou/go-ipmi/pkg/server"
	"github.com/bougou/go-ipmi/pkg/transport/udp"
	"github.com/bougou/go-ipmi/pkg/types"
)

// startTestBMC runs go-ipmi's reference BMC on a loopback port offering
// suites, with user 2 ADMIN/ADMIN, and points the native backend at it.
func startTestBMC(t *testing.T, suites []types.CipherSuiteID) string {
	t.Helper()
	b := bmc.New(bmc.DeviceInfo{IPMIVersion: 0x20}, [16]byte{}, mock.New(), bmc.WithClock(clock.Real), bmc.WithCipherSuites(suites))
	u, err := b.Users.Add(2, "ADMIN")
	if err != nil {
		t.Fatal(err)
	}
	u.SetPassword([]byte("ADMIN"))
	u.Enabled = true
	u.ChannelAccess[1] = bmc.UserChannelAccess{MaxPrivilege: bmc.PrivilegeLevelAdministrator, Enabled: true}
	pc, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	srv := server.NewServer(b, udp.Wrap(pc))
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = srv.Serve(ctx) }()
	t.Cleanup(func() { cancel(); _ = srv.Close(); _ = pc.Close() })
	old := nativePort
	nativePort = pc.LocalAddr().(*net.UDPAddr).Port
	t.Cleanup(func() { nativePort = old })
	t.Setenv(backendEnv, "native")
	return "127.0.0.1"
}

// Suite 0's level from parameters 23 and 24, as the Quanta reports them:
// suites 0-3, 6-8, 11, 12, privileges "caaaXXaaaXXaaXX" by suite number.
func TestSuite0From(t *testing.T) {
	var ids [16]types.CipherSuiteID
	for i, id := range []types.CipherSuiteID{0, 1, 2, 3, 6, 7, 8, 11, 12} {
		ids[i] = id
	}
	var privs [16]types.PrivilegeLevel
	for i, c := range "caaaXXaaaXXaaXX" {
		privs[i] = map[rune]types.PrivilegeLevel{'c': 1, 'u': 2, 'o': 3, 'a': 4, 'X': 0x0f}[c]
	}
	s, err := suite0From(ids, 9, privs)
	if err != nil || !s.Enabled || s.Max != "CALLBACK" || s.Risky() {
		t.Fatalf("Quanta: %+v err=%v", s, err)
	}
	privs[0] = 4 // suite 0 up to ADMINISTRATOR: the vulnerable set-up
	if s, _ := suite0From(ids, 9, privs); !s.Risky() || s.Max != "ADMINISTRATOR" {
		t.Fatalf("admin: %+v", s)
	}
	// Suite 0 not listed.
	if s, err := suite0From([16]types.CipherSuiteID{3, 17}, 2, privs); err != nil || s.Enabled {
		t.Fatalf("absent: %+v err=%v", s, err)
	}
	// Suite 0 listed second: entry and suite-number readings disagree.
	if _, err := suite0From([16]types.CipherSuiteID{3, 0}, 2, [16]types.PrivilegeLevel{4, 1}); err != ErrSuite0Unknown {
		t.Fatalf("ambiguous: err=%v", err)
	}
}

// A BMC that does not answer parameters 23/24 (go-ipmi's reference BMC:
// "parameter not supported") gives an error, never a "not enabled" verdict.
func TestCipherSuite0Unanswered(t *testing.T) {
	host := startTestBMC(t, []types.CipherSuiteID{0, 3})
	if s, err := CipherSuite0(host, "ADMIN", "ADMIN"); err == nil {
		t.Fatalf("no error from a BMC without the parameters: %+v", s)
	}
}

// fyrtaarn builds go-ipmi from Robert's fork (go.mod replace), which can
// complete a cipher suite 0 session; upstream v0.9.2 cannot (bougou/go-ipmi
// PR #116). If this fails, the replace was dropped before upstream released
// the fix.
func TestCipherSuite0SessionWithFork(t *testing.T) {
	startTestBMC(t, []types.CipherSuiteID{0, 3})
	c, err := client.NewClient("127.0.0.1", nativePort, "ADMIN", "ADMIN")
	if err != nil {
		t.Fatal(err)
	}
	c.WithTimeout(2 * time.Second).WithRetry(0).WithCipherSuiteID(types.CipherSuiteID0)
	if err := c.Connect(context.Background()); err != nil {
		t.Fatalf("suite 0 session: %v (is go.mod still replacing go-ipmi with the fork?)", err)
	}
	_ = c.Close(context.Background())
}
