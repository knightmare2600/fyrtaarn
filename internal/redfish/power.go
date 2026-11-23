package redfish

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Power control over Redfish (ComputerSystem.Reset), for BMCs used without
// IPMI. The BMC lists the reset types it accepts in
// "ResetType@Redfish.AllowableValues" on the action itself (seen on an
// iDRAC9 4.20, an iLO 4 2.54 and an iLO 5 1.40; neither iLO offers
// GracefulShutdown, their soft shutdown being PushPowerButton).

// PowerInfo is the first system's power state and reset action.
type PowerInfo struct {
	State   string   // PowerState, e.g. "On"
	Allowed []string // reset types the BMC accepts; nil if it does not say
	target  string
}

// resetLabels names the reset types fyrtaarn offers, in display order. Nmi
// is left out on purpose: it makes the operating system crash-dump.
var resetLabels = []struct{ Type, Label string }{
	{"On", "Power On"},
	{"GracefulShutdown", "Graceful Shutdown"},
	{"PushPowerButton", "Press Power Button"},
	{"GracefulRestart", "Graceful Restart"},
	{"ForceRestart", "Force Restart"},
	{"PowerCycle", "Power Cycle"},
	{"ForceOff", "Force Off"},
}

// ResetChoice is a reset type with a human label.
type ResetChoice struct{ Type, Label string }

// Choices returns the reset types to offer, in a fixed order: those the BMC
// allows, or all of them when it does not say.
func (p PowerInfo) Choices() []ResetChoice {
	allowed := map[string]bool{}
	for _, a := range p.Allowed {
		allowed[a] = true
	}
	var out []ResetChoice
	for _, r := range resetLabels {
		if p.Allowed == nil || allowed[r.Type] {
			out = append(out, ResetChoice{r.Type, r.Label})
		}
	}
	return out
}

// GetPowerInfo reads the first system's power state and reset action.
func GetPowerInfo(host, user, pass string) (PowerInfo, error) {
	c := httpClient()
	base := "https://" + host
	col, err := fetchJSON(c, base+"/redfish/v1/Systems", user, pass)
	if err != nil {
		return PowerInfo{}, err
	}
	paths := collectionPaths(col)
	if len(paths) == 0 {
		return PowerInfo{}, errors.New("no Redfish systems")
	}
	sys, err := fetchJSON(c, base+paths[0], user, pass)
	if err != nil {
		return PowerInfo{}, err
	}
	info := PowerInfo{State: str(sys, "PowerState")}
	acts, _ := sys["Actions"].(map[string]any)
	reset, _ := acts["#ComputerSystem.Reset"].(map[string]any)
	info.target, _ = reset["target"].(string)
	if info.target == "" {
		return info, errors.New("this BMC's Redfish system has no ComputerSystem.Reset action")
	}
	info.target = base + info.target
	if vals, ok := reset["ResetType@Redfish.AllowableValues"].([]any); ok {
		info.Allowed = []string{}
		for _, v := range vals {
			if s, ok := v.(string); ok {
				info.Allowed = append(info.Allowed, s)
			}
		}
	}
	return info, nil
}

// ResetSystem asks the BMC to apply resetType to the first system.
func ResetSystem(host, user, pass, resetType string) error {
	info, err := GetPowerInfo(host, user, pass)
	if err != nil {
		return err
	}
	if info.Allowed != nil {
		ok := false
		for _, a := range info.Allowed {
			ok = ok || a == resetType
		}
		if !ok {
			return fmt.Errorf("the BMC does not offer %s", resetType)
		}
	}
	body, _ := json.Marshal(map[string]string{"ResetType": resetType})
	return doPost(httpClient(), info.target, user, pass, body)
}
