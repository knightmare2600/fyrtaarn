package advisory

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

const nvdBaseURL = "https://services.nvd.nist.gov/rest/json/cves/2.0"

type nvdClient struct {
	apiKey string
	http   *http.Client
}

func newNVDClient(apiKey string) *nvdClient {
	return &nvdClient{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 30 * time.Second},
	}
}

// nvdResponse is the outer envelope of a NVD CVE API 2.0 response.
type nvdResponse struct {
	TotalResults    int `json:"totalResults"`
	Vulnerabilities []struct {
		CVE nvdCVE `json:"cve"`
	} `json:"vulnerabilities"`
}

type nvdCVE struct {
	ID          string `json:"id"`
	Published   string `json:"published"`
	Descriptions []struct {
		Lang  string `json:"lang"`
		Value string `json:"value"`
	} `json:"descriptions"`
	Metrics struct {
		V31 []nvdMetric31 `json:"cvssMetricV31"`
		V30 []nvdMetric31 `json:"cvssMetricV30"`
		V2  []nvdMetricV2 `json:"cvssMetricV2"`
	} `json:"metrics"`
}

type nvdMetric31 struct {
	CVSSData struct {
		BaseScore    float64 `json:"baseScore"`
		BaseSeverity string  `json:"baseSeverity"`
	} `json:"cvssData"`
}

type nvdMetricV2 struct {
	CVSSData struct {
		BaseScore float64 `json:"baseScore"`
	} `json:"cvssData"`
	BaseSeverity string `json:"baseSeverity"`
}

// queryCPE fetches CVEs from NVD for a given CPE formatted string binding.
// Returns at most resultsPerPage entries (caller caps further).
func (c *nvdClient) queryCPE(cpeName string) ([]CVEFinding, error) {
	params := url.Values{}
	params.Set("cpeName", cpeName)
	params.Set("resultsPerPage", "20")

	req, err := http.NewRequest(http.MethodGet, nvdBaseURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "fyrtaarn/0.0.7 (BMC management tool)")
	if c.apiKey != "" {
		req.Header.Set("apiKey", c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("NVD request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("NVD API key rejected (HTTP 403) — check nvd_api_key in config")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("NVD returned HTTP %d", resp.StatusCode)
	}

	var result nvdResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("NVD response decode: %w", err)
	}

	findings := make([]CVEFinding, 0, len(result.Vulnerabilities))
	for _, v := range result.Vulnerabilities {
		f := CVEFinding{
			ID:        v.CVE.ID,
			Published: v.CVE.Published,
		}

		for _, d := range v.CVE.Descriptions {
			if d.Lang == "en" {
				f.Description = truncate(d.Value, 120)
				break
			}
		}

		// Prefer CVSS v3.1 > v3.0 > v2.
		switch {
		case len(v.CVE.Metrics.V31) > 0:
			f.CVSS = v.CVE.Metrics.V31[0].CVSSData.BaseScore
			f.Severity = v.CVE.Metrics.V31[0].CVSSData.BaseSeverity
		case len(v.CVE.Metrics.V30) > 0:
			f.CVSS = v.CVE.Metrics.V30[0].CVSSData.BaseScore
			f.Severity = v.CVE.Metrics.V30[0].CVSSData.BaseSeverity
		case len(v.CVE.Metrics.V2) > 0:
			f.CVSS = v.CVE.Metrics.V2[0].CVSSData.BaseScore
			f.Severity = v.CVE.Metrics.V2[0].BaseSeverity
		}

		if f.Severity == "" {
			f.Severity = "UNKNOWN"
		}

		findings = append(findings, f)
	}

	return findings, nil
}

func truncate(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes-1]) + "…"
}
