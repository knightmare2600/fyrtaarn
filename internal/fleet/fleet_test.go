package fleet

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"os"
	"strings"

	"sync/atomic"
	"testing"
	"time"

	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
)

func TestDefaultLogin(t *testing.T) {
	if DefaultLogin("root", "calvin") != "Dell iDRAC" || DefaultLogin("admin", "admin") != "AMI MegaRAC" {
		t.Error("known defaults not recognised")
	}
	if DefaultLogin("root", "Calvin") != "" || DefaultLogin("admin", "s3cret") != "" {
		t.Error("non-default recognised")
	}
}

func TestUserFindings(t *testing.T) {
	// The Quanta: user 1 disabled; admin and thesneak are administrators,
	// thesneak disabled.
	quanta := []ipmi.UserEntry{
		{ID: 1, Privilege: "ADMINISTRATOR"},
		{ID: 2, Name: "admin", Enabled: true, Privilege: "ADMINISTRATOR"},
		{ID: 3, Name: "thesneak", Privilege: "ADMINISTRATOR"},
		{ID: 4, Privilege: "NO ACCESS"},
	}
	if f := userFindings(quanta); len(f) != 0 {
		t.Errorf("Quanta: %v", f)
	}
	if n, admins := userCounts(quanta); n != 1 || len(admins) != 1 || admins[0] != "admin" {
		t.Errorf("counts: %d %v", n, admins)
	}
	quanta[0].Enabled = true
	if f := userFindings(quanta); len(f) != 1 || f[0].Severity != Critical {
		t.Errorf("anonymous enabled: %v", f)
	}
	quanta[0].Privilege = "NO ACCESS"
	if f := userFindings(quanta); len(f) != 0 {
		t.Errorf("anonymous with no access: %v", f)
	}
}

func TestSuite0Finding(t *testing.T) {
	if _, ok := suite0Finding(ipmi.Suite0{}, nil); ok {
		t.Error("suite 0 off gave a finding")
	}
	if f, _ := suite0Finding(ipmi.Suite0{Enabled: true, Max: "CALLBACK"}, nil); f.Severity != Note {
		t.Errorf("callback: %v", f)
	}
	if f, _ := suite0Finding(ipmi.Suite0{Enabled: true, Max: "ADMINISTRATOR"}, nil); f.Severity != Critical {
		t.Errorf("admin: %v", f)
	}
	if f, _ := suite0Finding(ipmi.Suite0{}, ipmi.ErrSuite0Unknown); f.Severity != Note || suite0Text(ipmi.Suite0{}, ipmi.ErrSuite0Unknown) != "unknown" {
		t.Errorf("unknown: %v", f)
	}
}

// Run keeps the targets' order, never exceeds Workers at once, and does
// not start new BMCs once cancelled.
func TestRun(t *testing.T) {
	var running, peak atomic.Int32
	audit := func(tg Target) Result {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return Result{IP: tg.IP}
	}
	var targets []Target
	for _, ip := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"} {
		targets = append(targets, Target{IP: ip})
	}
	calls := 0
	rs := run(context.Background(), targets, func(Result) { calls++ }, audit)
	for i, r := range rs {
		if r.IP != targets[i].IP {
			t.Fatalf("order: %d is %s", i, r.IP)
		}
	}
	if calls != len(targets) || peak.Load() > Workers {
		t.Fatalf("calls %d, peak %d", calls, peak.Load())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rs = run(ctx, targets, nil, audit)
	for _, r := range rs {
		if r.Err == "" {
			t.Fatalf("%s audited after cancel", r.IP)
		}
	}
}

func TestSummary(t *testing.T) {
	rs := []Result{
		{IP: "a", Findings: []Finding{{Critical, "x"}}},
		{IP: "b", Findings: []Finding{{Warning, "y"}, {Note, "z"}}},
		{IP: "c"},
		{IP: "d", Err: "timeout"},
	}
	if got := Summary(rs); got != "3 BMCs audited, 1 with critical findings, 1 with warnings, 1 not reached" {
		t.Errorf("%q", got)
	}

}

func TestExport(t *testing.T) {
	rs := []Result{
		{IP: "10.0.0.1", Manufacturer: "Quanta Computer Inc.", Firmware: "7.54", Admins: []string{"admin"},
			Suite0: "CALLBACK", Findings: []Finding{{Critical, `default login, "quoted", with, commas`}}},
		{IP: "10.0.0.2", Err: "timeout"},
	}
	dir := t.TempDir()
	csvPath, jsonPath := dir+"/a.csv", dir+"/a.json"
	if err := WriteCSV(csvPath, rs); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSON(jsonPath, rs); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{csvPath, jsonPath} {
		if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
			t.Errorf("%s mode %v", p, st.Mode().Perm())
		}
	}
	b, _ := os.ReadFile(csvPath)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	r := csv.NewReader(strings.NewReader(strings.Join(lines[1:], "\n")))
	rows, err := r.ReadAll()
	if err != nil || len(rows) != 3 || rows[1][10] != "critical" || rows[2][10] != "not reached" ||
		rows[1][11] != `[critical] default login, "quoted", with, commas` {
		t.Fatalf("csv rows %q err %v", rows, err)
	}
	var j struct {
		Results []struct {
			IP       string                      `json:"ip"`
			Worst    string                      `json:"worst"`
			Findings []struct{ Severity string } `json:"findings"`
		} `json:"results"`
	}
	b, _ = os.ReadFile(jsonPath)
	if err := json.Unmarshal(b, &j); err != nil || len(j.Results) != 2 || j.Results[0].Findings[0].Severity != "critical" {
		t.Fatalf("json %s err %v", b, err)
	}
}
