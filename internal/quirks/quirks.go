// Package quirks records what is known to be wrong, or different, on
// particular BMCs and firmware, and what fyrtaarn does about each.
//
// A BMC is identified from what fyrtaarn already learns while connecting
// (Observe): IPMI "mc info" (manufacturer ID, product ID, firmware), the
// Redfish manager's firmware string, and the web UI recognised at scan
// time. Two kinds of entry:
//
//   - Declared quirks match on identity and are known before trying,
//     because trying does harm (a request that stalls or crashes a BMC's
//     web server).
//   - Learned quirks are found at run time by the code that hits them (a
//     refused cipher suite, TLS that needs the legacy client) and recorded
//     per host with Learn, so they show alongside the declared ones.
//
// Each entry says where it was seen. An entry matches the firmware
// actually seen unless it says it is widened, which is only done where
// guessing wrong in the other direction could harm a production BMC.
package quirks

import (
	"sort"
	"strings"
	"sync"
)

// ID names a quirk; code asks Has(host, ID) instead of knowing vendors.
type ID string

const (
	NoCipherSuiteList      ID = "no-cipher-suite-list"
	RefusesAutoCipherSuite ID = "refuses-auto-cipher-suite"
	RedfishLogEntriesStall ID = "redfish-log-entries-stall"
	JNLPNeedsEXTRNIP       ID = "jnlp-needs-extrnip"
	LMediaRegisterFails    ID = "lmedia-register-fails"
	LegacyTLSOnly          ID = "legacy-tls-only"
	NoConcurrentSessions   ID = "no-concurrent-sessions"
	IPMIOverLANWasOff      ID = "ipmi-over-lan-was-off"
	AccountsNoEnabledField ID = "accounts-no-enabled-field"
	RefusesEmptyUserName   ID = "refuses-empty-user-name"
	PayloadAccessNeedsLAN  ID = "payload-access-needs-lan-channel"
)

// IANA enterprise numbers as reported by "mc info" (Manufacturer ID).
const (
	MfrHP     = 11
	MfrDell   = 674
	MfrQuanta = 7244
	MfrHPE    = 47196
)

// Identity is what fyrtaarn knows about a BMC. Zero fields are unknown.
type Identity struct {
	ManufacturerID  int    // IPMI, e.g. 11 (HP)
	ProductID       int    // IPMI, e.g. 0x2000
	IPMIFirmware    string // IPMI "Firmware Revision", e.g. "2.54"
	RedfishFirmware string // Redfish manager FirmwareVersion, e.g. "iLO 4 v2.54"
	WebUI           string // recognised web UI, e.g. "AMI MegaRAC SP"
}

// merge fills id's unknown fields from o.
func (id Identity) merge(o Identity) Identity {
	if o.ManufacturerID != 0 {
		id.ManufacturerID = o.ManufacturerID
	}
	if o.ProductID != 0 {
		id.ProductID = o.ProductID
	}
	if o.IPMIFirmware != "" {
		id.IPMIFirmware = o.IPMIFirmware
	}
	if o.RedfishFirmware != "" {
		id.RedfishFirmware = o.RedfishFirmware
	}
	if o.WebUI != "" {
		id.WebUI = o.WebUI
	}
	return id
}

// Quirk is one known issue.
type Quirk struct {
	ID       ID
	BMC      string // human description of where it applies
	Effect   string // what goes wrong
	Handling string // what fyrtaarn does
	Source   string // where it was seen
	Learned  bool   // found at run time (Learn) rather than by identity
	match    func(Identity) bool
}

func isILO4(id Identity) bool {
	return strings.HasPrefix(id.RedfishFirmware, "iLO 4") ||
		(id.ManufacturerID == MfrHP && id.ProductID == 0x2000)
}

func isILO5(id Identity) bool {
	return strings.HasPrefix(id.RedfishFirmware, "iLO 5") ||
		(id.ManufacturerID == MfrHPE && id.ProductID == 0x2000 && strings.HasPrefix(id.IPMIFirmware, "1."))
}

func isQuantaAMI754(id Identity) bool {
	return (id.ManufacturerID == MfrQuanta && id.ProductID == 0x3252 && id.IPMIFirmware == "7.54") ||
		id.WebUI == "AMI MegaRAC SP"
}

// Known lists every quirk; README.md's Known Issues table lists the same
// IDs (checked by a test).
var Known = []Quirk{
	{
		ID: NoCipherSuiteList, BMC: "HP iLO 4 (FW 2.54)",
		Effect:   "Does not answer Get Channel Cipher Suites; ipmitool 1.8.19+ waits about 10 s on every command before falling back to suite 3",
		Handling: "-C 3 for this host after the first such failure (or straight away once identified)",
		Source:   "ProLiant ML310e Gen8 v2 captures, 2026-11-23",
		match: func(id Identity) bool {
			return isILO4(id) && (id.IPMIFirmware == "2.54" || id.RedfishFirmware == "iLO 4 v2.54")
		},
	},
	{
		ID: RefusesAutoCipherSuite, BMC: "Quanta, AMI MegaRAC SP (FW 7.54)",
		Effect:   "Lists cipher suite 17, which ipmitool 1.8.19+ then picks, but refuses it (\"no matching cipher suite\"); -C 3 works",
		Handling: "Retry once with -C 3 and keep the host on it (or -C 3 straight away once identified)",
		Source:   "Quanta in Tested Hardware, 2026-11-23",
		match:    isQuantaAMI754,
	},
	{
		ID: RedfishLogEntriesStall, BMC: "HP iLO 4 (seen on FW 2.54; applied to all iLO 4 as a precaution)",
		Effect:   "Requesting the IML log entries over Redfish stalled its web server: no answer to Redfish or its web pages for at least 30 minutes, until an iLO reset (mc reset cold)",
		Handling: "Redfish event log entries are not requested; the IPMI event log is used instead",
		Source:   "ProLiant ML310e Gen8 v2, 2026-11-23 (production box): stalled about 16:00, still stalled about 16:30, reset by IPMI",
		match:    isILO4,
	},
	{
		ID: JNLPNeedsEXTRNIP, BMC: "Quanta, AMI MegaRAC SP (FW 7.54)",
		Effect:   "Requesting the JViewer launch file without EXTRNIP crashes its web server until a BMC reset",
		Handling: "EXTRNIP is always sent (amiweb), and the web UI proxy rewrites it to the BMC's address",
		Source:   "Quanta in Tested Hardware, 2026-11-23, reproduced with curl",
		match:    isQuantaAMI754,
	},
	{
		ID: LMediaRegisterFails, BMC: "Quanta, AMI MegaRAC SP (FW 7.54)",
		Effect:   "LMedia uploads are accepted but registering the image returns status -1, for any image",
		Handling: "Upload to BMC still offered, with a warning that it fails on this firmware",
		Source:   "Quanta in Tested Hardware, 2026-11-23, identical with the browser's requests replayed",
		match:    isQuantaAMI754,
	},
	{
		ID: RefusesEmptyUserName, BMC: "Quanta, AMI MegaRAC SP (FW 7.54)",
		Effect:   "Refuses Set User Name with an empty name (\"invalid data field\"), so a user's name cannot be cleared",
		Handling: "Delete User revokes the account first (disabled, NO ACCESS, link auth and IPMI messaging off) and says the name stays",
		Source:   "Quanta in Tested Hardware, 2026-11-23, test user in slot 10; ipmitool's \"user set name 10 \"\"\" is refused the same way",
		match:    isQuantaAMI754,
	},
	{
		ID: PayloadAccessNeedsLAN, BMC: "Quanta, AMI MegaRAC SP (FW 7.54)",
		Effect:   "Refuses Set User Payload Access on channel 14 (\"this channel\") although it reads payload access there; channel 1 works",
		Handling: "fyrtaarn's own IPMI asks the BMC for its LAN channel number and uses that; the ipmitool fallback still sends 14",
		Source:   "Quanta in Tested Hardware, 2026-11-23, test user in slot 10, with fyrtaarn and with ipmitool's \"sol payload enable 14 10\"",
		match:    isQuantaAMI754,
	},
	{
		ID: LegacyTLSOnly, BMC: "Hosts whose HTTPS only works with the legacy TLS client (seen: Quanta, TLS 1.0 RC4-MD5)",
		Effect:   "No TLS that Go or current browsers accept",
		Handling: "legacytls falls back to its own TLS 1.0/1.1 RC4 client; the web UI proxy shows the negotiated TLS",
		Source:   "Quanta in Tested Hardware, 2026-11-23",
		Learned:  true,
	},
	{
		ID: NoConcurrentSessions, BMC: "Dell iDRAC9 (FW 4.20)",
		Effect:   "Refuses overlapping IPMI session set-ups (\"insufficient resources for session\")",
		Handling: "One IPMI command at a time per BMC, for every vendor",
		Source:   "iDRAC9 in Tested Hardware, 2026-10",
		match:    func(id Identity) bool { return id.ManufacturerID == MfrDell && id.ProductID == 0x0100 },
	},
	{
		ID: IPMIOverLANWasOff, BMC: "HPE iLO 5 (FW 1.40)",
		Effect:   "IPMI over LAN was off on the one seen, so IPMI never answered while Redfish did",
		Handling: "A failed connect checks it over Redfish and offers to switch it on; BMC > IPMI over LAN...",
		Source:   "ProLiant ML350 Gen10 capture, 2026-11-23",
		match: func(id Identity) bool {
			return isILO5(id) && (id.IPMIFirmware == "1.40" || id.RedfishFirmware == "iLO 5 v1.40")
		},
	},
	{
		ID: AccountsNoEnabledField, BMC: "HPE iLO 5 (FW 1.40)",
		Effect:   "Redfish accounts carry no Enabled or Locked field",
		Handling: "Oem.Hpe.Privileges.LoginPriv is used for enabled",
		Source:   "ProLiant ML350 Gen10 capture, 2026-11-23",
		match: func(id Identity) bool {
			return isILO5(id) && (id.IPMIFirmware == "1.40" || id.RedfishFirmware == "iLO 5 v1.40")
		},
	},
}

var (
	mu         sync.Mutex
	identities = map[string]Identity{}
	learned    = map[string]map[ID]bool{}
)

// Observe records what is known about host's identity, adding to earlier
// observations.
func Observe(host string, id Identity) {
	mu.Lock()
	defer mu.Unlock()
	identities[host] = identities[host].merge(id)
}

// IdentityOf returns what is known about host.
func IdentityOf(host string) Identity {
	mu.Lock()
	defer mu.Unlock()
	return identities[host]
}

// Learn records a quirk found at run time on host.
func Learn(host string, q ID) {
	mu.Lock()
	defer mu.Unlock()
	if learned[host] == nil {
		learned[host] = map[ID]bool{}
	}
	learned[host][q] = true
}

// Lookup returns the entry for q.
func Lookup(q ID) (Quirk, bool) {
	for _, k := range Known {
		if k.ID == q {
			return k, true
		}
	}
	return Quirk{}, false
}

// For returns the quirks that apply to host: declared ones matching its
// identity, then learned ones, without duplicates, in Known's order.
func For(host string) []Quirk {
	mu.Lock()
	id := identities[host]
	ls := learned[host]
	mu.Unlock()
	var out []Quirk
	for _, k := range Known {
		if (k.match != nil && k.match(id)) || ls[k.ID] {
			out = append(out, k)
		}
	}
	return out
}

// Has reports whether quirk q applies to host.
func Has(host string, q ID) bool {
	for _, k := range For(host) {
		if k.ID == q {
			return true
		}
	}
	return false
}

// IDs lists every known quirk ID, sorted.
func IDs() []string {
	var out []string
	for _, k := range Known {
		out = append(out, string(k.ID))
	}
	sort.Strings(out)
	return out
}

// Reset forgets every host (tests).
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	identities = map[string]Identity{}
	learned = map[string]map[ID]bool{}
}

// Fingerprint ties a web front page's ETag to a firmware build. On the
// iLOs below every static file carries the same ETag, so the front page's
// alone identifies the build, before any login: known issues then apply
// from the first request (for an iLO 4 2.54, -C 3 straight away).
// GoAhead-based AMI firmware (the Quanta) sends no ETag at all.
//
// Apache (iDRAC9) sends a different ETag per file, "<size>-<mtime>" in hex,
// but every file of a firmware build shares the mtime part, the build time
// in microseconds: such entries match on Build, the part after the dash.
//
// GoAhead-based AMI firmware sends no ETag but serves its web files
// pre-compressed, and each file's gzip header carries the build time and
// the file's path in the build tree (RFC 1952 MTIME and FNAME). Every file
// of a build shares both; such entries match on GzipTime and GzipRoot.
type Fingerprint struct {
	ETag     string // whole ETag, quotes included; or empty when Build is set
	Build    string // Apache ETag's part after "-", shared by every file
	GzipTime int64  // gzip MTIME (Unix seconds) shared by every file
	GzipRoot string // start of the gzip FNAME: the build tree
	BMC      string
	Identity Identity
	Source   string
}

// Fingerprints lists the known ETags; README.md's Known Issues lists them.
var Fingerprints = []Fingerprint{
	{
		ETag: `"00ce4159"`, BMC: "HP iLO 4, firmware 2.54",
		Identity: Identity{ManufacturerID: MfrHP, ProductID: 0x2000, IPMIFirmware: "2.54", RedfishFirmware: "iLO 4 v2.54"},
		Source:   "ProLiant ML310e Gen8 v2, 2026-11-23: same ETag on /, /favicon.ico and /css/eov_lteIE9.css",
	},
	{
		GzipTime: 1378190263, GzipRoot: "/AMI_AST2300_2011Q3_tmp/branches/s2rq_s2rs/",
		BMC:      "Quanta S2RQ/S2RS, AMI MegaRAC SP, firmware 7.54",
		Identity: Identity{ManufacturerID: MfrQuanta, ProductID: 0x3252, IPMIFirmware: "7.54", WebUI: "AMI MegaRAC SP"},
		Source:   "Quanta in Tested Hardware, 2026-11-23: gzip header on /index.html and five other files, built 2013-09-03 06:37:43 UTC in /AMI_AST2300_2011Q3_tmp/branches/s2rq_s2rs/S2Rx_workspace/",
	},
	{
		Build: "5a50953d044c0", BMC: "Dell iDRAC9, firmware 4.20.20.20",
		Identity: Identity{ManufacturerID: MfrDell, ProductID: 0x0100, IPMIFirmware: "4.20", RedfishFirmware: "4.20.20.20"},
		Source:   "PowerEdge T140, 2026-11-23: Apache ETags \"<size>-5a50953d044c0\" on every file, \"99a-5a50953d044c0\" on /restgui/start.html that / redirects to; 5a50953d044c0 is 2020-05-07 06:52:43 UTC in microseconds, the files' Last-Modified",
	},
	{
		ETag: `"00d2585c"`, BMC: "HPE iLO 5, firmware 1.40",
		Identity: Identity{ManufacturerID: MfrHPE, ProductID: 0x2000, IPMIFirmware: "1.40", RedfishFirmware: "iLO 5 v1.40"},
		Source:   "ProLiant ML350 Gen10, 2026-11-23: same ETag on /, /favicon.ico and /images/app-icon.svg",
	},
}

// normaliseETag drops a weak-validator prefix; quotes are kept, as sent.
func normaliseETag(e string) string {
	return strings.TrimPrefix(strings.TrimSpace(e), "W/")
}

// FromETag returns the fingerprint for a front page's ETag.
func FromETag(etag string) (Fingerprint, bool) {
	e := normaliseETag(etag)
	if e == "" {
		return Fingerprint{}, false
	}
	for _, f := range Fingerprints {
		if f.ETag != "" && f.ETag == e {
			return f, true
		}
		if f.Build != "" && strings.HasSuffix(e, "-"+f.Build+`"`) {
			return f, true
		}
	}
	return Fingerprint{}, false
}

// FromGzip returns the fingerprint for a pre-compressed file's gzip header:
// its MTIME (Unix seconds) and FNAME.
func FromGzip(mtime int64, name string) (Fingerprint, bool) {
	if mtime == 0 || name == "" {
		return Fingerprint{}, false
	}
	for _, f := range Fingerprints {
		if f.GzipRoot != "" && f.GzipTime == mtime && strings.HasPrefix(name, f.GzipRoot) {
			return f, true
		}
	}
	return Fingerprint{}, false
}
