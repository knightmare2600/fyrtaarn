package ipmi

import (
	"context"
	"fmt"
	"strings"

	"github.com/bougou/go-ipmi/pkg/client"
	"github.com/bougou/go-ipmi/pkg/types"
)

// Native IPMI, second group: LAN settings, the user list and SOL
// settings. Every channel number is 0x0E ("the channel this request
// arrives on", IPMI 2.0 table 6-1), as fyrtaarn already passes ipmitool
// ("channel getaccess 14", "sol info 14"), and as ipmitool's own "user
// list" defaults to; it avoids knowing each vendor's LAN channel (1 on
// most, 2 on iLOs).
const nativeThisChannel uint8 = 0x0E

// lanChannel finds the number of the LAN channel this session uses, for
// commands that do not take 0x0E: the Quanta in Tested Hardware answers Get
// LAN Configuration Parameters on 0x0E with "invalid data field" (0xCC)
// although it takes 0x0E for SOL and user commands. Get Channel Info on
// 0x0E names the actual channel; failing that, the first channel whose
// medium is LAN, as ipmitool's "lan print" picks (find_lan_channel,
// lib/ipmi_lanp.c, channels 1 to 11).
func lanChannel(ctx context.Context, c *client.Client) (uint8, error) {
	if r, err := c.GetChannelInfo(ctx, nativeThisChannel); err == nil && r.ChannelMedium == types.ChannelMediumLAN &&
		r.ActualChannelNumber != 0 && r.ActualChannelNumber != nativeThisChannel {
		return r.ActualChannelNumber, nil
	}
	for ch := uint8(1); ch <= 11; ch++ {
		if r, err := c.GetChannelInfo(ctx, ch); err == nil && r.ChannelMedium == types.ChannelMediumLAN {
			return ch, nil
		}
	}
	return 0, fmt.Errorf("no LAN channel found")
}

// privName names a privilege level as ipmitool prints it
// (ipmitool lib/ipmi_strings.c, ipmi_privlvl_vals).
func privName(l uint8) string {
	switch l {
	case 0x01:
		return "CALLBACK"
	case 0x02:
		return "USER"
	case 0x03:
		return "OPERATOR"
	case 0x04:
		return "ADMINISTRATOR"
	case 0x05:
		return "OEM"
	case 0x0F:
		return "NO ACCESS"
	}
	return fmt.Sprintf("Unknown (0x%02x)", l)
}

// solBitRate names an SOL bit rate code as ipmitool's "sol info" does, in
// kbps (IPMI 2.0 table 26-5, parameters 5 and 6).
func solBitRate(code uint8) string {
	switch code & 0x0F {
	case 0x00:
		return "IPMI-Over-Serial-Setting"
	case 0x06:
		return "9.6"
	case 0x07:
		return "19.2"
	case 0x08:
		return "38.4"
	case 0x09:
		return "57.6"
	case 0x0A:
		return "115.2"
	}
	return fmt.Sprintf("Unknown (0x%02x)", code)
}

func nativeLANInfo(host, user, pass string) (*LANInfo, error) {
	var info *LANInfo
	err := withNative(host, user, pass, ipmiShortTimeout, func(ctx context.Context, c *client.Client) error {
		ch, err := lanChannel(ctx, c)
		if err != nil {
			return err
		}
		ip := &types.LanConfigParam_IP{}
		mask := &types.LanConfigParam_SubnetMask{}
		mac := &types.LanConfigParam_MAC{}
		gw := &types.LanConfigParam_DefaultGatewayIP{}
		for _, p := range []types.LanConfigParameter{ip, mask, mac, gw} {
			if err := c.GetLanConfigParamFor(ctx, ch, p); err != nil {
				return err
			}
		}
		info = &LANInfo{
			IPAddress:  ip.IP.String(),
			SubnetMask: mask.SubnetMask.String(),
			MACAddress: mac.MAC.String(), // lower-case, colon-separated, as ipmitool
			Gateway:    gw.IP.String(),
		}
		return nil
	})
	return info, err
}

// userAccess is the part of Get User Access and Get User Name that
// UserEntry uses, separate from the library's types so the conversion is
// testable without a BMC.
type userAccess struct {
	ID           int
	Name         string
	CallbackOnly bool
	LinkAuth     bool
	IPMIMsg      bool
	Priv         uint8
	EnableStatus uint8 // 1 enabled, 2 disabled, 0 unspecified
}

// userEntryFrom builds a UserEntry as parseUserList plus "channel
// getaccess" do from ipmitool's output: enabled guessed from the name, then
// corrected by the BMC's own enable status when it reports one.
func userEntryFrom(u userAccess) UserEntry {
	e := UserEntry{
		ID:            u.ID,
		Name:          u.Name,
		Enabled:       u.Name != "" && u.Name != "(Empty User)",
		CallinEnabled: !u.CallbackOnly, // ipmitool's "Callin" column
		LinkEnabled:   u.LinkAuth,
		IPMIEnabled:   u.IPMIMsg,
		Privilege:     privName(u.Priv),
	}
	switch u.EnableStatus {
	case 1:
		e.Enabled = true
	case 2:
		e.Enabled = false
	}
	return e
}

// listUsers reads every user slot on an open session.
func listUsers(ctx context.Context, c *client.Client) ([]UserEntry, error) {
	var users []UserEntry
	for id := 1; id <= 63; id++ {
		acc, err := c.GetUserAccess(ctx, nativeThisChannel, uint8(id))
		if err != nil {
			return nil, fmt.Errorf("user %d access: %w", id, err)
		}
		name := ""
		if r, err := c.GetUsername(ctx, uint8(id)); err == nil {
			name = strings.TrimRight(r.Username, "\x00 ")
		} else if _, isBMCAnswer := types.IsResponseError(err); !isBMCAnswer {
			return nil, fmt.Errorf("user %d name: %w", id, err)
		}
		users = append(users, userEntryFrom(userAccess{
			ID: id, Name: name, CallbackOnly: acc.CallbackOnly, LinkAuth: acc.LinkAuthEnabled,
			IPMIMsg: acc.IPMIMessagingEnabled, Priv: uint8(acc.MaxPrivLevel), EnableStatus: acc.EnableStatus,
		}))
		if id >= int(acc.MaxUsersIDCount) {
			break
		}
	}
	return users, nil
}

func nativeUsers(host, user, pass string) ([]UserEntry, error) {
	var users []UserEntry
	err := withNative(host, user, pass, ipmiLongTimeout, func(ctx context.Context, c *client.Client) error {
		var err error
		users, err = listUsers(ctx, c)
		return err
	})
	return users, err
}

func nativeSOLSettings(host, user, pass string) (SOLSettings, error) {
	var s SOLSettings
	err := withNative(host, user, pass, ipmiLongTimeout, func(ctx context.Context, c *client.Client) error {
		en := &types.SOLConfigParam_SOLEnable{}
		auth := &types.SOLConfigParam_SOLAuthentication{}
		vbr := &types.SOLConfigParam_VolatileBitRate{}
		nvbr := &types.SOLConfigParam_NonVolatileBitRate{}
		for _, p := range []types.SOLConfigParameter{en, auth, vbr, nvbr} {
			if err := c.GetSOLConfigParamFor(ctx, nativeThisChannel, p); err != nil {
				return err
			}
		}
		s = SOLSettings{
			Enabled:         en.EnableSOLPayload,
			Privilege:       privName(auth.PrivilegeLevel),
			BitRate:         solBitRate(vbr.BitRate),
			NVBitRate:       solBitRate(nvbr.BitRate),
			ForceEncryption: auth.ForceEncryption,
		}
		// The logged-in user's ID and SOL payload access; unknown if the
		// BMC will not say, as with ipmitool.
		users, err := listUsers(ctx, c)
		if err != nil {
			return nil
		}
		for _, u := range users {
			if u.Name == user {
				s.UserID = u.ID
				break
			}
		}
		if s.UserID != 0 {
			if pa, err := c.GetUserPayloadAccess(ctx, nativeThisChannel, uint8(s.UserID)); err == nil {
				s.UserPayload = "disabled"
				if pa.PayloadTypeSOL {
					s.UserPayload = "enabled"
				}
			}
		}
		return nil
	})
	return s, err
}
