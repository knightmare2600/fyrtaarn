package advisory

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// CISA Known Exploited Vulnerabilities catalog — public, no API key required.
const kevURL = "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"

type kevCatalog struct {
	Vulnerabilities []struct {
		CVEID string `json:"cveID"`
	} `json:"vulnerabilities"`
}

var (
	kevMu        sync.Mutex
	kevSet       map[string]struct{}
	kevFetchedAt time.Time
	kevTTL       = time.Hour
)

// kevContains returns true if the given CVE ID is in the CISA KEV catalog.
// The catalog is fetched once and cached for kevTTL. A fetch failure is
// silently ignored — KEV annotation is best-effort.
func kevContains(cveID string) bool {
	kevMu.Lock()
	defer kevMu.Unlock()

	if kevSet == nil || time.Since(kevFetchedAt) >= kevTTL {
		if s, err := fetchKEV(); err == nil {
			kevSet = s
			kevFetchedAt = time.Now()
		}
	}

	if kevSet == nil {
		return false
	}
	_, ok := kevSet[cveID]
	return ok
}

func fetchKEV() (map[string]struct{}, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(kevURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var catalog kevCatalog
	if err := json.NewDecoder(resp.Body).Decode(&catalog); err != nil {
		return nil, err
	}

	set := make(map[string]struct{}, len(catalog.Vulnerabilities))
	for _, v := range catalog.Vulnerabilities {
		set[v.CVEID] = struct{}{}
	}
	return set, nil
}
