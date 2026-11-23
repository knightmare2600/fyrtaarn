package quirks

import (
	"os"
	"strings"
	"testing"
)

func ids(qs []Quirk) string {
	var s []string
	for _, q := range qs {
		s = append(s, string(q.ID))
	}
	return strings.Join(s, ",")
}

// The identities below are those reported by the BMCs in Tested Hardware
// (mc info and Redfish manager firmware, 2026-11-23).
func TestKnownBMCs(t *testing.T) {
	Reset()
	Observe("ilo4", Identity{ManufacturerID: MfrHP, ProductID: 0x2000, IPMIFirmware: "2.54"})
	Observe("ilo5", Identity{RedfishFirmware: "iLO 5 v1.40"})
	Observe("quanta", Identity{ManufacturerID: MfrQuanta, ProductID: 0x3252, IPMIFirmware: "7.54"})
	Observe("idrac", Identity{ManufacturerID: MfrDell, ProductID: 0x0100, IPMIFirmware: "4.20"})
	Observe("other", Identity{ManufacturerID: 10876, ProductID: 1, IPMIFirmware: "1.0"})

	cases := map[string]string{
		"ilo4":   "no-cipher-suite-list,redfish-log-entries-stall",
		"ilo5":   "ipmi-over-lan-was-off,accounts-no-enabled-field",
		"quanta": "refuses-auto-cipher-suite,jnlp-needs-extrnip,lmedia-register-fails",
		"idrac":  "no-concurrent-sessions",
		"other":  "",
	}
	for host, want := range cases {
		if got := ids(For(host)); got != want {
			t.Errorf("%s: got %q want %q", host, got, want)
		}
	}
}

// An iLO 4 on other firmware still gets the precautionary log quirk, but
// not the cipher one, which was only seen on 2.54.
func TestILO4OtherFirmware(t *testing.T) {
	Reset()
	Observe("h", Identity{RedfishFirmware: "iLO 4 v2.80"})
	if !Has("h", RedfishLogEntriesStall) || Has("h", NoCipherSuiteList) {
		t.Fatalf("got %s", ids(For("h")))
	}
}

func TestObserveMergesAndLearn(t *testing.T) {
	Reset()
	Observe("q", Identity{WebUI: "AMI MegaRAC SP"}) // from a scan, before IPMI
	if !Has("q", JNLPNeedsEXTRNIP) {
		t.Fatal("web UI alone should identify the Quanta firmware")
	}
	Observe("q", Identity{IPMIFirmware: "7.54"})
	if id := IdentityOf("q"); id.WebUI == "" || id.IPMIFirmware != "7.54" {
		t.Fatalf("merge lost a field: %+v", id)
	}
	Learn("q", LegacyTLSOnly)
	if !Has("q", LegacyTLSOnly) || Has("nobody", LegacyTLSOnly) {
		t.Fatal("learned quirk not per host")
	}
}

func TestEntriesComplete(t *testing.T) {
	seen := map[ID]bool{}
	for _, q := range Known {
		if q.BMC == "" || q.Effect == "" || q.Handling == "" || q.Source == "" {
			t.Errorf("%s: missing text", q.ID)
		}
		if (q.match == nil) != q.Learned {
			t.Errorf("%s: a quirk is either matched by identity or learned", q.ID)
		}
		if seen[q.ID] {
			t.Errorf("%s: duplicate", q.ID)
		}
		seen[q.ID] = true
	}
}

// TestReadmeListsEveryQuirk keeps README.md's Known Issues table in step
// with the code.
func TestReadmeListsEveryQuirk(t *testing.T) {
	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range IDs() {
		if !strings.Contains(string(b), "`"+id+"`") {
			t.Errorf("README.md Known Issues does not list `%s`", id)
		}
	}
}

func TestFingerprints(t *testing.T) {
	Reset()
	for etag, want := range map[string]string{
		`"00ce4159"`:             "HP iLO 4, firmware 2.54",
		`W/"00d2585c"`:           "HPE iLO 5, firmware 1.40",
		`"99a-5a50953d044c0"`:    "Dell iDRAC9, firmware 4.20.20.20", // /restgui/start.html, what a scan reads
		`"10be-5a50953d044c0"`:   "Dell iDRAC9, firmware 4.20.20.20", // favicon.ico
		`"0-5a50953d044c0"`:      "Dell iDRAC9, firmware 4.20.20.20", // an empty file
		`W/"1e0b-5a50953d044c0"`: "Dell iDRAC9, firmware 4.20.20.20",
	} {
		f, ok := FromETag(etag)
		if !ok || f.BMC != want {
			t.Errorf("%s: got %v %q", etag, ok, f.BMC)
		}
	}
	for _, e := range []string{`"deadbeef"`, `"10be-5a50953d044c1"`, `"5a50953d044c0"`} {
		if _, ok := FromETag(e); ok {
			t.Errorf("%s matched", e)
		}
	}
	if _, ok := FromETag(""); ok {
		t.Error("empty ETag matched")
	}
	// AMI GoAhead: the gzip header of a pre-compressed file.
	const quanta = "Quanta S2RQ/S2RS, AMI MegaRAC SP, firmware 7.54"
	tree := "/AMI_AST2300_2011Q3_tmp/branches/s2rq_s2rs/S2Rx_workspace/Build/output/ImageTree/usr/local/www/"
	if f, ok := FromGzip(1378190263, tree+"index.html"); !ok || f.BMC != quanta || f.Identity.ProductID != 0x3252 {
		t.Errorf("Quanta gzip: %v %q", ok, f.BMC)
	}
	for _, c := range []struct {
		mtime int64
		name  string
	}{{1378190264, tree + "index.html"}, {1378190263, "/other/tree/index.html"}, {0, tree + "index.html"}, {1378190263, ""}} {
		if _, ok := FromGzip(c.mtime, c.name); ok {
			t.Errorf("gzip %d %q matched", c.mtime, c.name)
		}
	}
	// A fingerprint alone, before any login, brings the known issues.
	f, _ := FromETag(`"00ce4159"`)
	Observe("h", f.Identity)
	if !Has("h", NoCipherSuiteList) || !Has("h", RedfishLogEntriesStall) {
		t.Fatalf("iLO 4 fingerprint: %s", ids(For("h")))
	}
}

func TestReadmeListsEveryFingerprint(t *testing.T) {
	b, err := os.ReadFile("../../README.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range Fingerprints {
		key := f.ETag
		if key == "" {
			key = f.Build
		}
		if key == "" {
			key = f.GzipRoot
		}
		if !strings.Contains(string(b), "`"+key+"`") {
			t.Errorf("README.md does not list fingerprint `%s`", key)
		}
	}
}
