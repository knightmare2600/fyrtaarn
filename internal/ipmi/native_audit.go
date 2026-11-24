package ipmi

import (
	"context"
	"errors"

	"github.com/bougou/go-ipmi/pkg/client"
	"github.com/bougou/go-ipmi/pkg/types"
)

// Cipher suite 0 (no authentication, integrity or confidentiality) is the
// weakness behind CVE-2013-4782: a suite 0 session is accepted whatever
// password is given, up to the privilege level the BMC allows for that
// suite. What matters is that level. The Quanta in Tested Hardware
// accepts suite 0 sessions but caps them at CALLBACK, below the USER
// level any real command needs ("Cipher Suite Priv Max: caaa..."), and
// ipmitool -C 0 at USER was refused there with "insufficient privilege".
//
// The level is read from the BMC's LAN configuration (parameters 23,
// cipher suite IDs, and 24, maximum privilege per suite) rather than tried
// with a session, which would also count against the BMC's session table
// and logs. (Upstream go-ipmi v0.9.2 could not complete a suite 0 session
// at all; fyrtaarn builds the fork that can, bougou/go-ipmi PR #116.)
//
// Parameter 24 is defined by entry position in parameter 23's list (IPMI
// 2.0 table 23-4), but the Quanta's answer only makes sense indexed by
// suite number (its listed suites 0-3, 6-8, 11, 12 are exactly the non-X
// positions). Both readings are taken; when they disagree for suite 0 the
// level is reported as unknown.

// ErrSuite0Unknown means the BMC's answer did not settle suite 0's level.
var ErrSuite0Unknown = errors.New("cipher suite 0 level not known")

// Suite0 is what the LAN configuration says about cipher suite 0.
type Suite0 struct {
	Enabled bool   // listed among the channel's cipher suites
	Max     string // its maximum privilege, e.g. "CALLBACK", if Enabled
}

// Risky reports whether suite 0 lets a session reach USER level or above.
func (s Suite0) Risky() bool {
	switch s.Max {
	case "USER", "OPERATOR", "ADMINISTRATOR", "OEM":
		return s.Enabled
	}
	return false
}

// suite0From reads suite 0 out of parameters 23 and 24; count is the
// number of valid entries in ids.
func suite0From(ids [16]types.CipherSuiteID, count int, privs [16]types.PrivilegeLevel) (Suite0, error) {
	pos := -1
	for i := 0; i < count && i < len(ids); i++ {
		if ids[i] == types.CipherSuiteID0 {
			pos = i
			break
		}
	}
	if pos < 0 {
		return Suite0{}, nil
	}
	byEntry, byID := privs[pos], privs[0]
	if byEntry != byID {
		return Suite0{Enabled: true}, ErrSuite0Unknown
	}
	return Suite0{Enabled: true, Max: privName(uint8(byEntry))}, nil
}

// CipherSuite0 reads host's cipher suite 0 setting on its LAN channel.
func CipherSuite0(host, user, pass string) (Suite0, error) {
	if !useNative(host) {
		return Suite0{}, ErrSuite0Unknown
	}
	var s Suite0
	err := withNative(host, user, pass, ipmiShortTimeout, func(ctx context.Context, c *client.Client) error {
		ch, err := lanChannel(ctx, c)
		if err != nil {
			return err
		}
		count := &types.LanConfigParam_CipherSuitesSupport{}
		ids := &types.LanConfigParam_CipherSuitesID{}
		privs := &types.LanConfigParam_CipherSuitesPrivLevel{}
		for _, p := range []types.LanConfigParameter{count, ids, privs} {
			if err := c.GetLanConfigParamFor(ctx, ch, p); err != nil {
				return err
			}
		}
		s, err = suite0From(ids.IDs, int(count.Count), privs.PrivLevels)
		return err
	})
	if errors.Is(err, errNativeOff) {
		return Suite0{}, ErrSuite0Unknown
	}
	return s, err
}
