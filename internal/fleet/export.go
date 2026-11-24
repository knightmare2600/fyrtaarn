package fleet

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Audit reports name the BMCs with weak spots, so they are written for the
// owner only (0600). They never contain passwords.
const reportMode = 0o600

func create(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, reportMode)
}

func findingsText(fs []Finding) string {
	var parts []string
	for _, f := range fs {
		parts = append(parts, "["+f.Severity.String()+"] "+f.Text)
	}
	return strings.Join(parts, "; ")
}

func worstText(r Result) string {
	switch {
	case r.Err != "":
		return "not reached"
	case r.Worst() < 0:
		return "none"
	}
	return r.Worst().String()
}

// WriteCSV writes one row per BMC, after a "#" line naming the run, as the
// inventory export does.
func WriteCSV(path string, rs []Result) error {
	f, err := create(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(f, "# fyrtaarn fleet audit — %s\n", time.Now().Format(time.RFC3339))
	w := csv.NewWriter(f)
	_ = w.Write([]string{"ip", "manufacturer", "product", "firmware", "ipmi_version", "power", "ipmi_via",
		"enabled_users", "admins", "cipher_suite_0", "worst", "findings", "known_issues", "error", "seconds"})
	for _, r := range rs {
		_ = w.Write([]string{r.IP, r.Manufacturer, r.Product, r.Firmware, r.IPMIVersion, r.Power, r.Via,
			fmt.Sprint(r.EnabledUsers), strings.Join(r.Admins, ";"), r.Suite0, worstText(r),
			findingsText(r.Findings), strings.Join(r.KnownIssues, ";"), r.Err, fmt.Sprintf("%.1f", r.Took.Seconds())})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

type jsonFinding struct {
	Severity string `json:"severity"`
	Text     string `json:"text"`
}

type jsonResult struct {
	IP           string        `json:"ip"`
	Manufacturer string        `json:"manufacturer,omitempty"`
	Product      string        `json:"product,omitempty"`
	Firmware     string        `json:"firmware,omitempty"`
	IPMIVersion  string        `json:"ipmi_version,omitempty"`
	Power        string        `json:"power,omitempty"`
	Via          string        `json:"ipmi_via,omitempty"`
	EnabledUsers int           `json:"enabled_users"`
	Admins       []string      `json:"admins"`
	Suite0       string        `json:"cipher_suite_0,omitempty"`
	Worst        string        `json:"worst"`
	Findings     []jsonFinding `json:"findings"`
	KnownIssues  []string      `json:"known_issues"`
	Error        string        `json:"error,omitempty"`
	Seconds      float64       `json:"seconds"`
}

// WriteJSON writes {"generated": ..., "results": [...]}.
func WriteJSON(path string, rs []Result) error {
	out := struct {
		Generated string       `json:"generated"`
		Results   []jsonResult `json:"results"`
	}{Generated: time.Now().Format(time.RFC3339)}
	for _, r := range rs {
		j := jsonResult{IP: r.IP, Manufacturer: r.Manufacturer, Product: r.Product, Firmware: r.Firmware,
			IPMIVersion: r.IPMIVersion, Power: r.Power, Via: r.Via, EnabledUsers: r.EnabledUsers,
			Admins: append([]string{}, r.Admins...), Suite0: r.Suite0, Worst: worstText(r),
			Findings: []jsonFinding{}, KnownIssues: append([]string{}, r.KnownIssues...), Error: r.Err,
			Seconds: r.Took.Seconds()}
		for _, f := range r.Findings {
			j.Findings = append(j.Findings, jsonFinding{f.Severity.String(), f.Text})
		}
		out.Results = append(out.Results, j)
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	f, err := create(path)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
