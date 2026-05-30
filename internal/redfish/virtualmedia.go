package redfish

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

func httpClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
}

// InsertMedia mounts an ISO image via the Redfish VirtualMedia InsertMedia
// action. It walks the Managers collection to discover the correct path rather
// than hard-coding vendor-specific URLs.
func InsertMedia(host, isoURL, user, pass string) error {
	c := httpClient()
	base := "https://" + host

	actionURL, err := findVMAction(c, base, user, pass, "InsertMedia")
	if err != nil {
		return fmt.Errorf("could not locate Redfish VirtualMedia InsertMedia action: %w", err)
	}

	body, _ := json.Marshal(map[string]any{
		"Image":          isoURL,
		"Inserted":       true,
		"WriteProtected": true,
	})

	return doPost(c, actionURL, user, pass, body)
}

// EjectMedia unmounts the current virtual media via the Redfish EjectMedia action.
func EjectMedia(host, user, pass string) error {
	c := httpClient()
	base := "https://" + host

	actionURL, err := findVMAction(c, base, user, pass, "EjectMedia")
	if err != nil {
		return fmt.Errorf("could not locate Redfish VirtualMedia EjectMedia action: %w", err)
	}

	return doPost(c, actionURL, user, pass, []byte("{}"))
}

// findVMAction discovers the URL for actionName (InsertMedia or EjectMedia)
// by walking Managers → VirtualMedia collection and returning the first
// CD/DVD member that advertises the requested action.
func findVMAction(c *http.Client, base, user, pass, actionName string) (string, error) {
	// Fetch manager list.
	mgrs, err := fetchJSON(c, base+"/redfish/v1/Managers/", user, pass)
	if err != nil {
		return "", err
	}

	members, _ := jsonArray(mgrs, "Members")
	if len(members) == 0 {
		return "", fmt.Errorf("no Managers found")
	}

	// Walk each manager looking for a VirtualMedia collection.
	for _, m := range members {
		mgrPath, _ := m["@odata.id"].(string)
		if mgrPath == "" {
			continue
		}

		mgr, err := fetchJSON(c, base+mgrPath, user, pass)
		if err != nil {
			continue
		}

		vmLink := jsonOdataID(mgr, "VirtualMedia")
		if vmLink == "" {
			continue
		}

		vmCol, err := fetchJSON(c, base+vmLink, user, pass)
		if err != nil {
			continue
		}

		vmMembers, _ := jsonArray(vmCol, "Members")
		for _, vm := range vmMembers {
			vmPath, _ := vm["@odata.id"].(string)
			if vmPath == "" {
				continue
			}

			// Only consider CD/DVD entries (skip floppy, USB, etc.).
			lower := strings.ToLower(vmPath)
			if !strings.Contains(lower, "cd") && !strings.Contains(lower, "dvd") {
				continue
			}

			vmRes, err := fetchJSON(c, base+vmPath, user, pass)
			if err != nil {
				continue
			}

			// Look for the action URL in Actions.
			actions, _ := vmRes["Actions"].(map[string]any)
			for k, v := range actions {
				if strings.Contains(k, actionName) {
					if av, ok := v.(map[string]any); ok {
						if target, ok := av["target"].(string); ok && target != "" {
							return base + target, nil
						}
					}
				}
			}
		}
	}

	return "", fmt.Errorf("VirtualMedia %s action not found on any Manager", actionName)
}

func doPost(c *http.Client, url, user, pass string, body []byte) error {
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if user != "" {
		req.SetBasicAuth(user, pass)
	}

	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("Redfish returned HTTP %d", resp.StatusCode)
	}

	return nil
}

func fetchJSON(c *http.Client, url, user, pass string) (map[string]any, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if user != "" {
		req.SetBasicAuth(user, pass)
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	return out, nil
}

func jsonArray(obj map[string]any, key string) ([]map[string]any, bool) {
	raw, ok := obj[key].([]any)
	if !ok {
		return nil, false
	}
	var out []map[string]any
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out, true
}

func jsonOdataID(obj map[string]any, key string) string {
	nested, _ := obj[key].(map[string]any)
	if nested == nil {
		return ""
	}
	id, _ := nested["@odata.id"].(string)
	return id
}
