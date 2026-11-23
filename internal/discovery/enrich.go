package discovery

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/knightmare2600/fyrtaarn/internal/legacytls"

	"github.com/knightmare2600/fyrtaarn/internal/quirks"
)

// EnrichResults runs reverse DNS and Redfish probing concurrently after nmap
// returns, so the scan itself stays fast. Two sequential phases are used to
// avoid data races on the same HostResult struct from multiple goroutines.
func EnrichResults(results []HostResult) []HostResult {
	var wg sync.WaitGroup

	// Phase 1: reverse DNS — populates Hostname.
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			names, err := net.DefaultResolver.LookupAddr(ctx, results[i].IP)
			if err == nil && len(names) > 0 {
				results[i].Hostname = strings.TrimSuffix(names[0], ".")
			}
		}(i)
	}
	wg.Wait()

	// Phase 2: Redfish probe — only for confirmed BMCs or hosts with 443 open.
	// Populates HasRedfish, RedfishVersion, RedfishManufacturer, RedfishModel.
	for i := range results {
		if !results[i].IsBMC && !hasOpenPort(results[i], 443) {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// The front page: one GET of "/", for the web UI's name (by
			// title) and the firmware build (by ETag).
			ui, etag := probeFrontPage(results[i].IP)
			if fp, ok := quirks.FromETag(etag); ok {
				results[i].Fingerprint = fp.BMC
				quirks.Observe(results[i].IP, fp.Identity)
			}
			rf, ok := probeRedfish(results[i].IP)
			if !ok {
				// No Redfish: the recognised web UI still gives older
				// BMCs a name.
				if ui != "" {
					results[i].WebUI = ui
					quirks.Observe(results[i].IP, quirks.Identity{WebUI: ui})
					if results[i].Vendor == "" {
						results[i].Vendor = ui
					}
				}
				return
			}
			results[i].HasRedfish = true
			results[i].RedfishVersion = rf.version
			results[i].RedfishManufacturer = rf.manufacturer
			results[i].RedfishModel = rf.model
		}(i)
	}
	wg.Wait()

	return results
}

func hasOpenPort(h HostResult, portID int) bool {
	for _, p := range h.Ports {
		if p.PortID == portID && p.State == "open" {
			return true
		}
	}
	return false
}

type redfishResult struct {
	version      string
	manufacturer string
	model        string
}

func redfishClient() *http.Client {
	// BMC devices universally use self-signed certificates.
	return &http.Client{
		Timeout: 4 * time.Second,
		// Old BMCs may only speak TLS 1.0/1.1 or RC4; see legacytls.
		Transport: legacytls.Transport(),
	}
}

func probeRedfish(ip string) (redfishResult, bool) {
	client := redfishClient()

	resp, err := client.Get("https://" + ip + "/redfish/v1/")
	if err != nil {
		return redfishResult{}, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return redfishResult{}, false
	}

	var root map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return redfishResult{}, false
	}

	// Must have either RedfishVersion or @odata.type to be considered Redfish.
	ver, _ := root["RedfishVersion"].(string)
	if ver == "" {
		if _, has := root["@odata.type"]; !has {
			return redfishResult{}, false
		}
		ver = "unknown"
	}

	rf := redfishResult{version: ver}
	rf.manufacturer, rf.model = fetchRedfishSystem(client, ip)
	return rf, true
}

// fetchRedfishSystem attempts to read manufacturer/model from the Redfish
// Systems collection without authentication. Many BMCs allow this unauthenticated.
func fetchRedfishSystem(client *http.Client, ip string) (manufacturer, model string) {
	resp, err := client.Get("https://" + ip + "/redfish/v1/Systems/")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	var col struct {
		Members []struct {
			OdataID string `json:"@odata.id"`
		} `json:"Members"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&col); err != nil || len(col.Members) == 0 {
		return
	}

	path := col.Members[0].OdataID
	if !strings.HasPrefix(path, "/") {
		return
	}

	resp2, err := client.Get("https://" + ip + path)
	if err != nil {
		return
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		return
	}

	var sys struct {
		Manufacturer string `json:"Manufacturer"`
		Model        string `json:"Model"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&sys); err != nil {
		return
	}

	return strings.TrimSpace(sys.Manufacturer), strings.TrimSpace(sys.Model)
}

// webUITitles maps the <title> of a BMC's web front page to a name.
// "Megarac SP" is the title of AMI MegaRAC SP firmware on the Quanta in
// Tested Hardware (FW 7.54, 2008-09 GoAhead-Webs UI, no Redfish).
var webUITitles = map[string]string{
	"megarac sp": "AMI MegaRAC SP",
}

var titleRE = regexp.MustCompile(`(?is)<title>\s*(.*?)\s*</title>`)

// probeWebUI names the web UI from the front page's title; "" if
// unrecognised or unreachable.
func probeWebUI(ip string) string {
	ui, _ := probeFrontPage(ip)
	return ui
}

// probeFrontPage fetches the BMC's HTTPS front page once (old TLS included,
// through the same client as the Redfish probe), following redirects (the
// Quanta answers / with a 302 to /index.html, iDRAC9 into /restgui/), and
// returns the web UI named from its title and the final page's ETag.
func probeFrontPage(ip string) (ui, etag string) {
	resp, err := redfishClient().Get("https://" + ip + "/")
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	etag = resp.Header.Get("ETag")
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.Header.Get("Content-Encoding") == "gzip" {
		if zr, err := gzip.NewReader(bytes.NewReader(body)); err == nil {
			body, _ = io.ReadAll(io.LimitReader(zr, 64<<10))
		}
	}
	m := titleRE.FindSubmatch(body)
	if m == nil {
		return "", etag
	}
	return webUITitles[strings.ToLower(string(m[1]))], etag
}
