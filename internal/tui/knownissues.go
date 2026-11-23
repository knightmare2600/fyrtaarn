package tui

import (
	"fmt"
	"strings"

	"github.com/knightmare2600/fyrtaarn/internal/ipmi"
	"github.com/knightmare2600/fyrtaarn/internal/quirks"
	"github.com/knightmare2600/fyrtaarn/internal/redfish"
)

// Known issues (internal/quirks): fyrtaarn records each BMC's identity as
// it learns it, and BMC Info / the Redfish view list the issues that apply.

func observeMCInfo(host string, info *ipmi.MCInfo) {
	if info == nil {
		return
	}
	quirks.Observe(host, quirks.Identity{
		ManufacturerID: info.ManufacturerID,
		ProductID:      info.ProductID,
		IPMIFirmware:   info.FirmwareRevision,
	})
}

func observeRedfish(host string, e *redfish.FullEnumeration) {
	if e == nil {
		return
	}
	for _, m := range e.Managers {
		if m.FirmwareVersion != "" {
			quirks.Observe(host, quirks.Identity{RedfishFirmware: m.FirmwareVersion})
		}
	}
}

// knownIssuesLine is the BMC Info / Redfish view line, or "" if none.
func knownIssuesLine(host string) string {
	n := len(quirks.For(host))
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("  %-24s %d  [K] details\n", "Known issues:", n)
}

// NewKnownIssuesDialog lists what applies to host and what fyrtaarn does.
func NewKnownIssuesDialog(host string) *Dialog {
	qs := quirks.For(host)
	var b strings.Builder
	if len(qs) == 0 {
		b.WriteString("No known issues for " + host + ".")
	}
	for i, q := range qs {
		if i > 0 {
			b.WriteString("\n\n")
		}
		kind := ""
		if q.Learned {
			kind = " (seen this session)"
		}
		fmt.Fprintf(&b, "%d. %s\n", i+1, wrap(q.BMC+kind, 60, "   "))
		fmt.Fprintf(&b, "   Problem: %s\n", wrap(q.Effect, 54, "            "))
		fmt.Fprintf(&b, "   Handled: %s\n", wrap(q.Handling, 54, "            "))
		fmt.Fprintf(&b, "   Seen:    %s", wrap(q.Source, 54, "            "))
	}
	return &Dialog{
		Title:   "Known Issues — " + host,
		Body:    b.String(),
		buttons: []DialogButton{{Label: "Close", Action: "cancel"}},
	}
}

// wrap breaks s into lines of at most width runes, indenting the rest.
func wrap(s string, width int, indent string) string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(s) {
		if line != "" && len([]rune(line))+1+len([]rune(w)) > width {
			lines = append(lines, line)
			line = w
			continue
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	lines = append(lines, line)
	return strings.Join(lines, "\n"+indent)
}
