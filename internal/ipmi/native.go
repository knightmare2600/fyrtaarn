package ipmi

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bougou/go-ipmi/pkg/client"
	"github.com/bougou/go-ipmi/pkg/command/chassis"
	"github.com/bougou/go-ipmi/pkg/oem/iana"
	"github.com/bougou/go-ipmi/pkg/types"
)

// Native IPMI: commands sent by fyrtaarn itself over IPMI 2.0 (RMCP+,
// UDP 623) through github.com/bougou/go-ipmi, instead of running ipmitool.
// Commands move across one group at a time; the rest still use ipmitool.
//
// ipmitool stays as the fallback. If a native session cannot be set up
// with a host, that host uses ipmitool for the rest of the run (a BMC the
// library cannot talk to is not retried on every call). A BMC that sets up
// a session but refuses a command is reported as refused, not retried with
// ipmitool: the refusal is the BMC's answer.
//
// FYRTAARN_IPMI selects the backend: "ipmitool" for ipmitool only (as
// before 0.9), "native" for native only with no fallback (for testing),
// anything else or unset for native with fallback.

const backendEnv = "FYRTAARN_IPMI"

// nativePort is the BMC's RMCP+ port; a variable so tests can point it at a
// test BMC on an unprivileged port.
var nativePort = 623

// errNativeOff is returned by the native calls when the backend is not to
// be used for this host; callers then run ipmitool.
var errNativeOff = errors.New("native IPMI not in use for this host")

// nativeFailed holds hosts whose native session set-up failed (host -> the
// error text), so they go straight to ipmitool.
var nativeFailed sync.Map

func backend() string { return strings.ToLower(strings.TrimSpace(os.Getenv(backendEnv))) }

// useNative reports whether host is to get a native attempt.
func useNative(host string) bool {
	switch backend() {
	case "ipmitool":
		return false
	case "native":
		return true
	}
	_, failed := nativeFailed.Load(host)
	return !failed
}

// fallbackAllowed reports whether a failed native set-up may fall back.
func fallbackAllowed() bool { return backend() != "native" }

// NativeFallback returns why host fell back to ipmitool, or "" if it has not.
func NativeFallback(host string) string {
	if v, ok := nativeFailed.Load(host); ok {
		return v.(string)
	}
	return ""
}

// withNative opens a native session with host, runs fn and closes it. It
// takes the same per-host lock as ipmitool (lockHost), since the BMC's
// session table does not care which client is asking. A set-up failure
// marks the host for ipmitool and returns errNativeOff when falling back is
// allowed, so the caller runs ipmitool instead.
func withNative(host, user, pass string, timeout time.Duration, fn func(ctx context.Context, c *client.Client) error) error {
	if !useNative(host) {
		return errNativeOff
	}
	defer lockHost(host)()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	c, err := client.NewClient(host, nativePort, user, pass)
	if err == nil {
		// Administrator, as ipmitool asks for by default; per-message
		// timeout and one retry, so a dead host fails inside the timeout.
		c.WithMaxPrivilegeLevel(types.PrivilegeLevelAdministrator).WithTimeout(4 * time.Second).WithRetry(1)
		if wantSuite3(host) {
			c.WithCipherSuiteID(types.CipherSuiteID3)
		}
		err = c.Connect(ctx)
	}
	if err != nil {
		if !fallbackAllowed() {
			return fmt.Errorf("native IPMI session: %w", err)
		}
		nativeFailed.Store(host, err.Error())
		return errNativeOff
	}
	defer func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer ccancel()
		_ = c.Close(cctx)
	}()
	return fn(ctx, c)
}

// mcInfoFromDeviceID builds MCInfo as parseMCInfo does from ipmitool's
// "mc info" output, so the rest of fyrtaarn (vendor names, compliance
// lookups keyed on ProductName) sees the same values:
//   - firmware "major.minor" with the minor in two digits ("4.20", "2.54");
//   - manufacturer named from the IANA registry, which is also where
//     ipmitool's names come from ("Hewlett-Packard" for 11, "Hewlett
//     Packard Enterprise" for 47196, "Dell Inc.", "Quanta Computer Inc.");
//   - product "Unknown (0x…)", hex without padding, as ipmitool prints for
//     every BMC fyrtaarn has met (its product table knows none of them).
func mcInfoFromDeviceID(d deviceID) *MCInfo {
	name, ok := iana.Lookup(d.ManufacturerID)
	if !ok {
		name = "Unknown"
	}
	return &MCInfo{
		DeviceID:         fmt.Sprintf("%d", d.DeviceID),
		FirmwareRevision: fmt.Sprintf("%d.%02d", d.MajorFirmware, d.MinorFirmware),
		IPMIVersion:      fmt.Sprintf("%d.%d", d.MajorIPMI, d.MinorIPMI),
		ManufacturerName: name,
		ProductName:      fmt.Sprintf("Unknown (0x%x)", d.ProductID),
		ManufacturerID:   int(d.ManufacturerID),
		ProductID:        int(d.ProductID),
	}
}

// deviceID is the part of Get Device ID that MCInfo uses, separate from the
// library's type so the conversion is testable without a BMC.
type deviceID struct {
	DeviceID                     uint8
	MajorFirmware, MinorFirmware uint8 // minor already decoded from BCD
	MajorIPMI, MinorIPMI         uint8
	ManufacturerID               uint32
	ProductID                    uint16
}

func nativeMCInfo(host, user, pass string) (*MCInfo, error) {
	var info *MCInfo
	err := withNative(host, user, pass, ipmiShortTimeout, func(ctx context.Context, c *client.Client) error {
		r, err := c.GetDeviceID(ctx)
		if err != nil {
			return err
		}
		info = mcInfoFromDeviceID(deviceID{
			DeviceID: r.DeviceID, MajorFirmware: r.MajorFirmwareRevision, MinorFirmware: r.MinorFirmwareRevision,
			MajorIPMI: r.MajorIPMIVersion, MinorIPMI: r.MinorIPMIVersion,
			ManufacturerID: r.ManufacturerID, ProductID: r.ProductID,
		})
		return nil
	})
	return info, err
}

func nativeChassisStatus(host, user, pass string) (*ChassisStatus, error) {
	var st *ChassisStatus
	err := withNative(host, user, pass, ipmiShortTimeout, func(ctx context.Context, c *client.Client) error {
		r, err := c.GetChassisStatus(ctx)
		if err != nil {
			return err
		}
		st = &ChassisStatus{
			PowerOn:         r.PowerIsOn,
			PowerStateFound: true,
			PowerOverload:   r.PowerOverload,
			PowerFault:      r.PowerFault,
			DriveFault:      r.DriveFault,
			CoolingFault:    r.CollingFanFault, // sic, the library's spelling
		}
		return nil
	})
	return st, err
}

func nativeChassisControl(host, user, pass string, ctl chassis.ChassisControl) error {
	return withNative(host, user, pass, ipmiShortTimeout, func(ctx context.Context, c *client.Client) error {
		_, err := c.ChassisControl(ctx, ctl)
		return err
	})
}

func nativeColdReset(host, user, pass string) error {
	return withNative(host, user, pass, ipmiShortTimeout, func(ctx context.Context, c *client.Client) error {
		return c.ColdReset(ctx)
	})
}

// BackendFor names the IPMI backend host is on, for BMC Info.
func BackendFor(host string) string {
	switch {
	case backend() == "ipmitool":
		return "ipmitool (" + backendEnv + "=ipmitool)"
	case backend() == "native":
		return "native, go-ipmi (" + backendEnv + "=native, no fallback)"
	case NativeFallback(host) != "":
		return "ipmitool (native session failed: " + NativeFallback(host) + ")"
	}
	return "native, go-ipmi (ipmitool as fallback)"
}

// UsesNative reports whether host's next command goes to fyrtaarn's own
// IPMI first (it may still fall back if the session cannot be set up).
func UsesNative(host string) bool { return useNative(host) }
