// Package fleet runs read-only checks across many BMCs at once.
//
// The first is the audit: for each BMC, who it is, its power state, which
// IPMI backend reached it, and the classic IPMI security findings, all
// read without changing anything:
//   - cipher suite 0 enabled at USER level or above (CVE-2013-4782: such a
//     session is accepted whatever password is given), read from the LAN
//     configuration (ipmi.CipherSuite0);
//   - the anonymous user (user 1, no name) enabled with some privilege;
//   - the audit's own login used a vendor's factory default password;
//   - IPMI 1.5 only, or a firmware revision of 0.00 (ipmi.ComplianceFromMC);
//   - known issues for the BMC (internal/quirks).
//
// Each BMC gets one IPMI session per question, one BMC at a time per host
// (the per-host lock in internal/ipmi), at most Workers BMCs at once.
package fleet

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
	"github.com/knightmare2600/fyrtaarn/internal/quirks"
)

// Target is one BMC to audit and the login to use.
type Target struct {
	IP, User, Pass string
}

// Severity orders findings: Critical before Warning before Note.
type Severity int

const (
	Note Severity = iota
	Warning
	Critical
)

func (s Severity) String() string {
	return [...]string{"note", "warning", "critical"}[s]
}

// Finding is one thing the audit found.
type Finding struct {
	Severity Severity
	Text     string
}

// Result is one BMC's audit.
type Result struct {
	IP           string
	Manufacturer string
	Product      string
	Firmware     string
	IPMIVersion  string
	Power        string // "on", "off" or "" if unknown
	Via          string // IPMI backend (ipmi.BackendFor)
	EnabledUsers int
	Admins       []string // enabled users with ADMINISTRATOR
	Suite0       string   // "off", its privilege cap (e.g. "CALLBACK"), or "unknown"
	KnownIssues  []string // quirk IDs
	Findings     []Finding
	Err          string // why the BMC could not be audited at all
	Took         time.Duration
}

// Worst is the most severe finding's severity, or -1 with none.
func (r Result) Worst() Severity {
	w := Severity(-1)
	for _, f := range r.Findings {
		if f.Severity > w {
			w = f.Severity
		}
	}
	return w
}

// factoryDefaults are vendors' documented factory default logins. Sources:
// Rapid7, "A Penetration Tester's Guide to IPMI and BMCs" (2013), for
// Dell, Supermicro, IBM/Lenovo and Oracle; Dell's knowledge base 000133536,
// which notes that current iDRACs ship with a unique random password
// instead; the Quanta in Tested Hardware for AMI MegaRAC SP. Cisco CIMC is
// left out: no default could be confirmed.
var factoryDefaults = []struct{ User, Pass, Vendor string }{
	{"root", "calvin", "Dell iDRAC"},
	{"ADMIN", "ADMIN", "Supermicro"},
	{"USERID", "PASSW0RD", "IBM IMM / Lenovo"},
	{"root", "changeme", "Oracle ILOM"},
	{"admin", "admin", "AMI MegaRAC"},
}

// DefaultLogin names the vendor whose factory default user/pass is, or "".
func DefaultLogin(user, pass string) string {
	for _, d := range factoryDefaults {
		if d.User == user && d.Pass == pass {
			return d.Vendor
		}
	}
	return ""
}

// AuditHost audits one BMC.
func AuditHost(t Target) (r Result) {
	start := time.Now()
	r.IP = t.IP
	// r is the named result, so this lands in what the caller receives.
	defer func() { r.Took = time.Since(start) }()

	mc, err := ipmi.GetMCInfo(t.IP, t.User, t.Pass)
	if err != nil {
		r.Err = err.Error()
		r.Via = ipmi.BackendFor(t.IP)
		return r
	}
	r.Manufacturer, r.Product = mc.ManufacturerName, mc.ProductName
	r.Firmware, r.IPMIVersion = mc.FirmwareRevision, mc.IPMIVersion
	r.Via = ipmi.BackendFor(t.IP)
	quirks.Observe(t.IP, mc.Identity())

	if st, err := ipmi.GetChassisStatus(t.IP, t.User, t.Pass); err == nil && st.PowerStateFound {
		r.Power = "off"
		if st.PowerOn {
			r.Power = "on"
		}
	}

	r.Findings = append(r.Findings, credentialFindings(t)...)
	for _, issue := range ipmi.ComplianceFromMC(mc).Issues {
		r.Findings = append(r.Findings, Finding{Warning, issue})
	}

	if users, err := ipmi.GetUsers(t.IP, t.User, t.Pass); err == nil {
		r.EnabledUsers, r.Admins = userCounts(users)
		r.Findings = append(r.Findings, userFindings(users)...)
	} else {
		r.Findings = append(r.Findings, Finding{Note, "user list not read: " + err.Error()})
	}

	s0, err := ipmi.CipherSuite0(t.IP, t.User, t.Pass)
	r.Suite0 = suite0Text(s0, err)
	if f, ok := suite0Finding(s0, err); ok {
		r.Findings = append(r.Findings, f)
	}

	for _, q := range quirks.For(t.IP) {
		r.KnownIssues = append(r.KnownIssues, string(q.ID))
	}
	sortFindings(r.Findings)
	return r
}

func credentialFindings(t Target) []Finding {
	if v := DefaultLogin(t.User, t.Pass); v != "" {
		return []Finding{{Critical, fmt.Sprintf("logs in with the %s factory default password (user %q)", v, t.User)}}
	}
	return nil
}

func userCounts(users []ipmi.UserEntry) (enabled int, admins []string) {
	for _, u := range users {
		if !u.Enabled {
			continue
		}
		enabled++
		if u.Privilege == "ADMINISTRATOR" {
			name := u.Name
			if name == "" {
				name = fmt.Sprintf("(user %d, no name)", u.ID)
			}
			admins = append(admins, name)
		}
	}
	return enabled, admins
}

// userFindings flags the anonymous user: user 1, which IPMI reserves for a
// login with no name, enabled with a privilege that lets it do anything.
func userFindings(users []ipmi.UserEntry) []Finding {
	for _, u := range users {
		if u.ID == 1 && u.Name == "" && u.Enabled && u.Privilege != "NO ACCESS" && u.Privilege != "CALLBACK" {
			return []Finding{{Critical, "anonymous user (user 1, no name) is enabled at " + u.Privilege}}
		}
	}
	return nil
}

func suite0Text(s ipmi.Suite0, err error) string {
	switch {
	case err != nil:
		return "unknown"
	case !s.Enabled:
		return "off"
	}
	return s.Max
}

func suite0Finding(s ipmi.Suite0, err error) (Finding, bool) {
	switch {
	case err != nil:
		return Finding{Note, "cipher suite 0 not checked: " + errText(err)}, true
	case s.Risky():
		return Finding{Critical, "cipher suite 0 (no authentication) is enabled up to " + s.Max + ": any password is accepted at that level (CVE-2013-4782)"}, true
	case s.Enabled:
		return Finding{Note, "cipher suite 0 is enabled but capped at " + s.Max + ", below USER"}, true
	}
	return Finding{}, false
}

func errText(err error) string {
	if errors.Is(err, ipmi.ErrSuite0Unknown) {
		return "the BMC's answer did not settle it, or the host is on ipmitool"
	}
	return err.Error()
}

func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Severity > fs[j].Severity })
}

// Workers is how many BMCs are audited at once.
const Workers = 4

// Run audits every target, at most Workers at once, calling done (if not
// nil) as each finishes. Results are in the targets' order. A cancelled
// ctx stops starting new BMCs; those not started are returned with Err.
func Run(ctx context.Context, targets []Target, done func(Result)) []Result {
	return run(ctx, targets, done, AuditHost)
}

func run(ctx context.Context, targets []Target, done func(Result), audit func(Target) Result) []Result {
	out := make([]Result, len(targets))
	sem := make(chan struct{}, Workers)
	var wg sync.WaitGroup
	var mu sync.Mutex
	for i, t := range targets {
		// Checked first: with a free slot and a cancelled ctx both ready,
		// select would pick either at random.
		if ctx.Err() != nil {
			out[i] = Result{IP: t.IP, Err: "not audited: cancelled"}
			continue
		}
		select {
		case <-ctx.Done():
			out[i] = Result{IP: t.IP, Err: "not audited: cancelled"}
			continue
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(i int, t Target) {
			defer wg.Done()
			defer func() { <-sem }()
			r := audit(t)
			out[i] = r
			if done != nil {
				mu.Lock()
				done(r)
				mu.Unlock()
			}
		}(i, t)
	}
	wg.Wait()
	return out
}

// Plural is "1 BMC", "2 BMCs".
func Plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// Summary is one line for a status bar.
func Summary(rs []Result) string {
	var crit, warn, failed int
	for _, r := range rs {
		switch {
		case r.Err != "":
			failed++
		case r.Worst() == Critical:
			crit++
		case r.Worst() == Warning:
			warn++
		}
	}
	parts := []string{Plural(len(rs)-failed, "BMC") + " audited"}
	if crit > 0 {
		parts = append(parts, fmt.Sprintf("%d with critical findings", crit))
	}
	if warn > 0 {
		parts = append(parts, fmt.Sprintf("%d with warnings", warn))
	}
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d not reached", failed))
	}
	return strings.Join(parts, ", ")
}
