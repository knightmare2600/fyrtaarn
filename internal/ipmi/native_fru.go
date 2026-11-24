package ipmi

import (
	"context"
	"fmt"
	"strings"

	"github.com/bougou/go-ipmi/pkg/client"
	"github.com/bougou/go-ipmi/pkg/types"
)

// Native FRU: go-ipmi walks the FRU devices as ipmitool's "fru" does (the
// management controller's own device 0, then each FRU device locator in
// the sensor repository) and decodes each area's fields; fyrtaarn lists
// them under ipmitool's field names (lib/ipmi_fru.c, fru_area_print_*),
// leaving out empty fields as parseFRU does. Board Mfg Date is printed as
// "2006-01-02 15:04:05 MST" in local time, as the native event log is,
// rather than ipmitool's locale-dependent text.

const fruEpoch = 820454400 // 1996-01-01 00:00:00 UTC (FRU spec, board area)

func fruString(tl types.TypeLength, raw []byte) string {
	return strings.TrimRight(types.FRUFieldString(tl, raw), "\x00 ")
}

// fruEntries lists one device's fields.
func fruEntries(f *types.FRU) []FRUEntry {
	name := strings.TrimRight(f.DeviceName, "\x00 ")
	if f.DeviceID == 0 && name == "Builtin FRU" {
		name = "Builtin FRU Device" // ipmitool's name for device 0
	}
	out := []FRUEntry{{Field: "FRU Device Description", Value: fmt.Sprintf("%s (ID %d)", name, f.DeviceID), IsHeader: true}}
	if !f.Present() {
		reason := "Device not present"
		if f.DeviceNotPresentReason != "" {
			reason += " (" + f.DeviceNotPresentReason + ")"
		}
		return append(out, FRUEntry{Field: "Status", Value: reason})
	}
	add := func(field, value string) {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, FRUEntry{Field: field, Value: value})
		}
	}
	if c := f.ChassisInfoArea; c != nil {
		add("Chassis Type", c.ChassisType.String())
		add("Chassis Part Number", fruString(c.PartNumberTypeLength, c.PartNumber))
		add("Chassis Serial", fruString(c.SerialNumberTypeLength, c.SerialNumber))
		for _, x := range c.Custom {
			add("Chassis Extra", fruString(x.TypeLength, x.Data))
		}
	}
	if b := f.BoardInfoArea; b != nil {
		// 0 minutes, the FRU epoch (1996-01-01 00:00 UTC), means unspecified.
		if !b.MfgDateTime.IsZero() && b.MfgDateTime.Unix() != fruEpoch {
			add("Board Mfg Date", b.MfgDateTime.Local().Format("2006-01-02 15:04:05 MST"))
		}
		add("Board Mfg", fruString(b.ManufacturerTypeLength, b.Manufacturer))
		add("Board Product", fruString(b.ProductNameTypeLength, b.ProductName))
		add("Board Serial", fruString(b.SerialNumberTypeLength, b.SerialNumber))
		add("Board Part Number", fruString(b.PartNumberTypeLength, b.PartNumber))
		for _, x := range b.Custom {
			add("Board Extra", fruString(x.TypeLength, x.Data))
		}
	}
	if p := f.ProductInfoArea; p != nil {
		add("Product Manufacturer", fruString(p.ManufacturerTypeLength, p.Manufacturer))
		add("Product Name", fruString(p.NameTypeLength, p.Name))
		add("Product Part Number", fruString(p.PartModelTypeLength, p.PartModel))
		add("Product Version", fruString(p.VersionTypeLength, p.Version))
		add("Product Serial", fruString(p.SerialNumberTypeLength, p.SerialNumber))
		add("Product Asset Tag", fruString(p.AssetTagTypeLength, p.AssetTag))
		for _, x := range p.Custom {
			add("Product Extra", fruString(x.TypeLength, x.Data))
		}
	}
	return out
}

func nativeFRU(host, user, pass string) ([]FRUEntry, error) {
	var out []FRUEntry
	err := withNative(host, user, pass, ipmiLongTimeout, func(ctx context.Context, c *client.Client) error {
		frus, err := c.GetFRUs(ctx)
		for _, f := range frus {
			out = append(out, fruEntries(f)...)
		}
		if len(frus) > 0 {
			return nil // what could be read; ipmitool also lists the rest
		}
		return err
	})
	return out, err
}
