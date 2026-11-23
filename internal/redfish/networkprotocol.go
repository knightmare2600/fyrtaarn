package redfish

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// IPMI over LAN on/off through the standard Redfish ManagerNetworkProtocol
// resource (Managers/<id>/NetworkProtocol, property IPMI.ProtocolEnabled).
// With it off, a BMC answers Redfish but never IPMI: every ipmitool call
// times out with "Unable to establish IPMI v2 / RMCP+ session" (seen on an
// HPE iLO 5 v1.40, ProLiant ML350 Gen10).

// ErrNoIPMIProtocol means the BMC's NetworkProtocol resource does not
// report IPMI at all, so it cannot be read or switched through Redfish.
var ErrNoIPMIProtocol = errors.New("this BMC's Redfish NetworkProtocol does not include IPMI")

// IPMIOverLAN is the IPMI entry of a manager's NetworkProtocol resource.
type IPMIOverLAN struct {
	Enabled bool
	Port    int    // 0 when not reported
	URL     string // the NetworkProtocol resource
	etag    string
}

// networkProtocolURL finds the first manager's NetworkProtocol resource.
func networkProtocolURL(c *http.Client, host, user, pass string) (string, error) {
	base := "https://" + host
	mgrs, err := fetchJSON(c, base+"/redfish/v1/Managers", user, pass)
	if err != nil {
		return "", err
	}
	members, _ := jsonArrayOf(mgrs, "Members")
	for _, m := range members {
		p, _ := m["@odata.id"].(string)
		if p == "" {
			continue
		}
		mgr, err := fetchJSON(c, base+p, user, pass)
		if err != nil {
			return "", err
		}
		if np, ok := mgr["NetworkProtocol"].(map[string]any); ok {
			if link, _ := np["@odata.id"].(string); link != "" {
				return base + link, nil
			}
		}
	}
	return "", errors.New("no Redfish manager links a NetworkProtocol resource")
}

func readIPMIOverLAN(c *http.Client, url, user, pass string) (IPMIOverLAN, error) {
	np, err := fetchJSON(c, url, user, pass)
	if err != nil {
		return IPMIOverLAN{}, err
	}
	ipmi, ok := np["IPMI"].(map[string]any)
	if !ok {
		return IPMIOverLAN{}, ErrNoIPMIProtocol
	}
	on, ok := ipmi["ProtocolEnabled"].(bool)
	if !ok {
		return IPMIOverLAN{}, ErrNoIPMIProtocol
	}
	st := IPMIOverLAN{Enabled: on, URL: url}
	if p, ok := ipmi["Port"].(float64); ok {
		st.Port = int(p)
	}
	st.etag, _ = np["@odata.etag"].(string)
	return st, nil
}

// GetIPMIOverLAN reports whether IPMI over LAN is enabled on host.
func GetIPMIOverLAN(host, user, pass string) (IPMIOverLAN, error) {
	c := httpClient()
	url, err := networkProtocolURL(c, host, user, pass)
	if err != nil {
		return IPMIOverLAN{}, err
	}
	return readIPMIOverLAN(c, url, user, pass)
}

// SetIPMIOverLAN switches IPMI over LAN on or off, then reads the setting
// back: a BMC that accepts the PATCH but keeps the old value (read-only
// property, or a change pending a BMC reset) is reported as an error rather
// than as success.
func SetIPMIOverLAN(host, user, pass string, on bool) error {
	c := httpClient()
	url, err := networkProtocolURL(c, host, user, pass)
	if err != nil {
		return err
	}
	cur, err := readIPMIOverLAN(c, url, user, pass)
	if err != nil {
		return err
	}
	if cur.Enabled == on {
		return nil
	}

	body, _ := json.Marshal(map[string]any{"IPMI": map[string]any{"ProtocolEnabled": on}})
	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OData-Version", "4.0")
	if cur.etag != "" {
		req.Header.Set("If-Match", cur.etag) // BMCs that enforce ETags need it
	}
	req.SetBasicAuth(user, pass)
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("the BMC refused the change: HTTP %d%s", resp.StatusCode, redfishMessage(msg))
	}

	after, err := readIPMIOverLAN(c, url, user, pass)
	if err != nil {
		return fmt.Errorf("change sent, but reading it back failed: %w", err)
	}
	if after.Enabled != on {
		hint := ""
		if strings.Contains(string(msg), "ResetRequired") {
			hint = " (the BMC says it needs a reset first)"
		}
		return fmt.Errorf("the BMC accepted the change but still reports IPMI over LAN %s%s",
			onOff(after.Enabled), hint)
	}
	return nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// redfishMessage pulls the first human-readable message out of a Redfish
// error body, if there is one.
func redfishMessage(body []byte) string {
	var e struct {
		Error struct {
			Message  string `json:"message"`
			Extended []struct {
				Message string `json:"Message"`
			} `json:"@Message.ExtendedInfo"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &e) != nil {
		return ""
	}
	for _, x := range e.Error.Extended {
		if x.Message != "" {
			return ": " + x.Message
		}
	}
	if e.Error.Message != "" {
		return ": " + e.Error.Message
	}
	return ""
}
