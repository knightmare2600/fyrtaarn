package ipmi

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/bougou/go-ipmi/pkg/client"
	"github.com/bougou/go-ipmi/pkg/types"
)

// SOL configuration: whether the BMC offers SOL at all ("sol info", SOL
// parameter "Enabled") and whether the logged-in user may use the SOL
// payload ("sol payload status"). Both are needed for a console; the
// server's BIOS must also redirect its console to the BMC's serial port,
// which is set in the server's firmware, not here.
//
// Channel 14 means "the channel this request arrived on" in the IPMI spec,
// so no per-vendor LAN channel number is needed; the Quanta in Tested
// Hardware answers both commands on it. Syntax from ipmitool 1.8.18
// lib/ipmi_sol.c: "set <parameter> <value> [channel]" and
// "payload <enable|disable|status> [channel] [userid]".

const thisChannel = "14"

// SOLSettings is what fyrtaarn shows about SOL.
type SOLSettings struct {
	Enabled         bool
	Privilege       string // minimum privilege for SOL, e.g. "ADMINISTRATOR"
	BitRate         string // volatile bit rate, kbps, e.g. "115.2"
	NVBitRate       string // non-volatile bit rate, kbps
	ForceEncryption bool
	UserID          int    // the logged-in user's ID; 0 if not found
	UserPayload     string // "enabled", "disabled" or "" if unknown
}

// GetSOLSettings reads SOL info and the logged-in user's payload access.
func GetSOLSettings(host, user, pass string) (SOLSettings, error) {
	if s, err := nativeSOLSettings(host, user, pass); err != errNativeOff {
		return s, err
	}
	out, err := runIPMICommand(host, user, pass, ipmiShortTimeout, "sol", "info", thisChannel)
	if err != nil {
		return SOLSettings{}, err
	}
	s := parseSOLInfo(out)
	if users, err := GetUsers(host, user, pass); err == nil {
		for _, u := range users {
			if u.Name == user {
				s.UserID = u.ID
				break
			}
		}
	}
	if s.UserID != 0 {
		st, err := runIPMICommand(host, user, pass, ipmiShortTimeout,
			"sol", "payload", "status", thisChannel, strconv.Itoa(s.UserID))
		if err == nil {
			s.UserPayload = parsePayloadStatus(st)
		}
	}
	return s, nil
}

func parseSOLInfo(out string) SOLSettings {
	var s SOLSettings
	for _, line := range strings.Split(out, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "Enabled":
			s.Enabled = v == "true"
		case "Privilege Level":
			s.Privilege = v
		case "Volatile Bit Rate (kbps)":
			s.BitRate = v
		case "Non-Volatile Bit Rate (kbps)":
			s.NVBitRate = v
		case "Force Encryption":
			s.ForceEncryption = v == "true"
		}
	}
	return s
}

// parsePayloadStatus reads "User 2 on channel 14 is enabled".
func parsePayloadStatus(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasSuffix(line, " is enabled"):
			return "enabled"
		case strings.HasSuffix(line, " is disabled"):
			return "disabled"
		}
	}
	return ""
}

// SetSOLEnabled switches SOL on or off for the BMC.
func SetSOLEnabled(host, user, pass string, on bool) error {
	// Natively on the BMC's LAN channel number rather than 14 ("this
	// channel"): the Quanta refuses 14 for other SOL and payload changes
	// (quirks.PayloadAccessNeedsLAN), and the real number works everywhere.
	if err := nativeUserOp(host, user, pass, func(ctx context.Context, c *client.Client) error {
		ch, err := lanChannel(ctx, c)
		if err != nil {
			return err
		}
		return c.SetSOLConfigParamFor(ctx, ch, &types.SOLConfigParam_SOLEnable{EnableSOLPayload: on})
	}); err != errNativeOff {
		return err
	}
	_, err := runIPMICommand(host, user, pass, ipmiShortTimeout,
		"sol", "set", "enabled", strconv.FormatBool(on), thisChannel)
	return err
}

// SetSOLPayload allows or denies userID the SOL payload.
func SetSOLPayload(host, user, pass string, userID int, on bool) error {
	if userID <= 0 {
		return fmt.Errorf("no user ID for %s", user)
	}
	if err := nativeUserOp(host, user, pass, func(ctx context.Context, c *client.Client) error {
		return setSOLPayload(ctx, c, userID, on)
	}); err != errNativeOff {
		return err
	}
	verb := "disable"
	if on {
		verb = "enable"
	}
	_, err := runIPMICommand(host, user, pass, ipmiShortTimeout,
		"sol", "payload", verb, thisChannel, strconv.Itoa(userID))
	return err
}
