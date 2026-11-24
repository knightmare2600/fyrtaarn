package ipmi

import (
	"context"
	"errors"
	"fmt"

	"github.com/bougou/go-ipmi/pkg/client"
	"github.com/bougou/go-ipmi/pkg/command/app"
	"github.com/bougou/go-ipmi/pkg/types"

	"github.com/knightmare2600/fyrtaarn/internal/quirks"
)

// Native user changes, with ipmitool 1.8.19's rules (lib/ipmi_user.c):
//   - a name is at most 16 bytes ("Username is too long (> 16 bytes)");
//   - a password is at most 20 bytes; over 16 it is stored as a 20-byte
//     (IPMI 2.0) password, else as a 16-byte one, as ipmitool picks when
//     not told. go-ipmi pads or silently truncates to the size given, so
//     the lengths are checked here first;
//   - enable and disable are Set User Password operations;
//   - "channel setaccess ... link=on ipmi=on callin=on privilege=N" is Set
//     User Access with those bits, which is what SetUserPrivilege sends.
// The password travels only inside the IPMI session, never in argv or a
// pty, unlike the ipmitool path's prompted entry.

const (
	maxUserName = 16
	maxPassword = 20
	ipmi15Pass  = 16
)

func checkUserName(name string) error {
	if len(name) > maxUserName {
		return fmt.Errorf("user name is too long (%d bytes, at most %d)", len(name), maxUserName)
	}
	return nil
}

// passwordStored20 checks the length and says whether to store 20 bytes.
func passwordStored20(pass string) (bool, error) {
	if len(pass) > maxPassword {
		return false, fmt.Errorf("password is too long (%d bytes, at most %d)", len(pass), maxPassword)
	}
	return len(pass) > ipmi15Pass, nil
}

func nativeUserOp(host, user, pass string, fn func(ctx context.Context, c *client.Client) error) error {
	return withNative(host, user, pass, ipmiShortTimeout, fn)
}

func setUserPassword(ctx context.Context, c *client.Client, id int, newPass string) error {
	s20, err := passwordStored20(newPass)
	if err != nil {
		return err
	}
	_, err = c.SetUserPassword(ctx, uint8(id), newPass, s20)
	return err
}

func setUserName(ctx context.Context, c *client.Client, id int, name string) error {
	if err := checkUserName(name); err != nil {
		return err
	}
	_, err := c.SetUsername(ctx, uint8(id), name)
	return err
}

// setUserAccess is "channel setaccess <ch> <id> link=on ipmi=on callin=on
// privilege=<level>". Channel 0 means the LAN channel of this session.
func setUserAccess(ctx context.Context, c *client.Client, id, channel, level int) error {
	ch := uint8(channel)
	if channel == 0 {
		var err error
		if ch, err = lanChannel(ctx, c); err != nil {
			return err
		}
	}
	_, err := c.SetUserAccess(ctx, &app.SetUserAccessRequest{
		EnableChanging: true, RestrictedToCallback: false, EnableLinkAuth: true, EnableIPMIMessaging: true,
		ChannelNumber: ch, UserID: uint8(id), MaxPrivLevel: uint8(level),
	})
	return err
}

// setSOLPayload grants or removes the SOL payload. go-ipmi's own
// SetUserPayloadAccess sends an empty request whatever it is given
// (v0.9.2, pkg/client/api_user.go), so the request is built here.
func setSOLPayload(ctx context.Context, c *client.Client, id int, on bool) error {
	op := app.SetUserPayloadAccessOperationDisable
	if on {
		op = app.SetUserPayloadAccessOperationEnable
	}
	// The actual LAN channel, not 0x0E: the Quanta refuses 0x0E here
	// (quirks.PayloadAccessNeedsLAN), and the real number works everywhere.
	ch, err := lanChannel(ctx, c)
	if err != nil {
		return err
	}
	req := &app.SetUserPayloadAccessRequest{ChannelNumber: ch, UserID: uint8(id), Operation: op, PayloadTypeSOL: true}
	return c.Exchange(ctx, req, &app.SetUserPayloadAccessResponse{})
}

// ErrNameKept is returned by DeleteUser when the account was revoked but
// the BMC would not clear its name (quirks.RefusesEmptyUserName).
var ErrNameKept = errors.New("account revoked (disabled, no access), but this BMC refuses an empty user name, so the name stays")

// deleteUser frees a slot as far as the BMC allows. IPMI has no delete:
// the account is disabled and its access revoked first, so it grants
// nothing even if the name cannot be cleared; then the name is cleared.
func deleteUser(ctx context.Context, c *client.Client, host string, id int) error {
	if err := c.DisableUser(ctx, uint8(id)); err != nil {
		return fmt.Errorf("disable: %w", err)
	}
	ch, err := lanChannel(ctx, c)
	if err != nil {
		return err
	}
	if _, err := c.SetUserAccess(ctx, &app.SetUserAccessRequest{
		EnableChanging: true, ChannelNumber: ch, UserID: uint8(id), MaxPrivLevel: 0x0f, // NO ACCESS; link auth, IPMI messaging off
	}); err != nil {
		return fmt.Errorf("revoke access: %w", err)
	}
	if _, err := c.SetUsername(ctx, uint8(id), ""); err != nil {
		if r, ok := types.IsResponseError(err); ok && r.CompletionCode() == types.CodeRequestDataFieldInvalid {
			quirks.Learn(host, quirks.RefusesEmptyUserName)
			return ErrNameKept
		}
		return fmt.Errorf("clear name: %w", err)
	}
	return nil
}
