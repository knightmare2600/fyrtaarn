package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (a *App) renderMCInfo() string {
	var b strings.Builder

	hostIP := ""
	if len(a.results) > 0 && a.selectedHost < len(a.results) {
		hostIP = a.results[a.selectedHost].IP
	}

	b.WriteString(HeaderStyle().Render(fmt.Sprintf("BMC INFORMATION — %s", hostIP)) + "\n")
	b.WriteString(strings.Repeat("─", 50) + "\n\n")

	if a.mcInfo != nil {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent)).Render("  CONTROLLER") + "\n")
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Device ID:", a.mcInfo.DeviceID))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Firmware Revision:", a.mcInfo.FirmwareRevision))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "IPMI Version:", a.mcInfo.IPMIVersion))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Manufacturer:", a.mcInfo.ManufacturerName))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Product:", a.mcInfo.ProductName))
		b.WriteString("\n")
	}

	if a.lanInfo != nil {
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent)).Render("  NETWORK") + "\n")
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "IP Address:", a.lanInfo.IPAddress))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "MAC Address:", a.lanInfo.MACAddress))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Subnet Mask:", a.lanInfo.SubnetMask))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Gateway:", a.lanInfo.Gateway))
		b.WriteString("\n")
	}

	if a.chassis != nil {
		powerStr := "Off"
		if a.chassis.PowerOn {
			powerStr = "On"
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent)).Render("  CHASSIS") + "\n")
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Power:", powerStr))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Power Fault:", boolYesNo(a.chassis.PowerFault)))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Power Overload:", boolYesNo(a.chassis.PowerOverload)))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Drive Fault:", boolYesNo(a.chassis.DriveFault)))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Cooling Fault:", boolYesNo(a.chassis.CoolingFault)))
		b.WriteString("\n")
	}

	return b.String()
}

func (a *App) renderSensors() string {
	var b strings.Builder

	hostIP := ""
	if len(a.results) > 0 && a.selectedHost < len(a.results) {
		hostIP = a.results[a.selectedHost].IP
	}

	b.WriteString(HeaderStyle().Render(fmt.Sprintf("SENSORS / SDR — %s", hostIP)) + "\n")
	b.WriteString(strings.Repeat("─", 50) + "\n\n")

	if len(a.sensors) == 0 {
		b.WriteString("  No sensor data available.\n")
		return b.String()
	}

	visibleLines := a.contentH - 6
	if visibleLines < 1 {
		visibleLines = 5
	}

	end := a.sdrOffset + visibleLines
	if end > len(a.sensors) {
		end = len(a.sensors)
	}

	hdr := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent))
	b.WriteString(fmt.Sprintf("  %s\n", hdr.Render(fmt.Sprintf("%-25s %-22s %s", "Sensor", "Reading", "Status"))))
	b.WriteString(fmt.Sprintf("  %s\n", strings.Repeat("─", 60)))

	for _, e := range a.sensors[a.sdrOffset:end] {
		b.WriteString(fmt.Sprintf("  %-25s %-22s %s\n", e.Name, e.Value, e.Status))
	}

	return b.String()
}

func (a *App) renderSEL() string {
	var b strings.Builder

	hostIP := ""
	if len(a.results) > 0 && a.selectedHost < len(a.results) {
		hostIP = a.results[a.selectedHost].IP
	}

	b.WriteString(HeaderStyle().Render(fmt.Sprintf("SYSTEM EVENT LOG — %s", hostIP)) + "\n")
	b.WriteString(strings.Repeat("─", 50) + "\n\n")

	if len(a.selEntries) == 0 {
		b.WriteString("  No events logged.\n")
		return b.String()
	}

	visibleLines := a.contentH - 6
	if visibleLines < 1 {
		visibleLines = 5
	}

	total := len(a.selEntries)
	end := a.selOffset + visibleLines
	if end > total {
		end = total
	}

	maxEventWidth := a.width - 30
	if maxEventWidth < 30 {
		maxEventWidth = 30
	}

	hdr := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent))
	b.WriteString(fmt.Sprintf("  %s\n", hdr.Render(fmt.Sprintf("%-6s %-20s %s", "ID", "Timestamp", "Event"))))
	b.WriteString(fmt.Sprintf("  %s\n", strings.Repeat("─", 70)))

	for _, e := range a.selEntries[a.selOffset:end] {
		event := e.Event
		if len(event) > maxEventWidth {
			event = event[:maxEventWidth]
		}
		dir := ""
		if e.Direction != "" {
			dir = " [" + e.Direction + "]"
		}
		color := selEventColor(e.Direction, e.Event)
		eventStr := lipgloss.NewStyle().Foreground(lipgloss.Color(color)).Render(event + dir)
		b.WriteString(fmt.Sprintf("  %-6s %-20s %s\n", e.ID, e.Timestamp, eventStr))
	}

	return b.String()
}

func (a *App) renderFRU() string {
	var b strings.Builder

	hostIP := ""
	if len(a.results) > 0 && a.selectedHost < len(a.results) {
		hostIP = a.results[a.selectedHost].IP
	}

	b.WriteString(HeaderStyle().Render(fmt.Sprintf("FRU / HARDWARE INVENTORY — %s", hostIP)) + "\n")
	b.WriteString(strings.Repeat("─", 50) + "\n\n")

	if len(a.fru) == 0 {
		b.WriteString("  No FRU data available.\n")
		return b.String()
	}

	visibleLines := a.contentH - 6
	if visibleLines < 1 {
		visibleLines = 5
	}

	total := len(a.fru)
	end := a.fruOffset + visibleLines
	if end > total {
		end = total
	}

	hdr := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent))

	for _, e := range a.fru[a.fruOffset:end] {
		if e.IsHeader {
			b.WriteString("\n  " + hdr.Render("── "+e.Field+" ──") + "\n")
			if e.Value != "" {
				b.WriteString(fmt.Sprintf("  %s\n", e.Value))
			}
		} else {
			b.WriteString(fmt.Sprintf("  %-30s %s\n", e.Field+":", e.Value))
		}
	}

	return b.String()
}

func (a *App) renderLoading() string {
	return fmt.Sprintf("\n\n  %s  %s\n\n  [Ctrl+C] Quit", a.spinner.View(), a.status)
}

func boolYesNo(v bool) string {
	if v {
		return "Yes"
	}
	return "No"
}

// selEventColor returns a Solarized-palette hex color based on SEL event
// severity. Ordering of checks matters: "non-recoverable" must be tested
// before "non-critical", and "non-critical" before bare "critical".
func selEventColor(direction, event string) string {
	dir := strings.ToLower(strings.TrimSpace(direction))
	ev := strings.ToLower(event)

	// Deasserted = condition has cleared → green.
	if dir == "deasserted" {
		return "#859900" // Solarized green
	}

	if strings.Contains(ev, "non-recoverable") {
		return "#DC322F" // Solarized red — worst case
	}
	if strings.Contains(ev, "non-critical") {
		return "#CB4B16" // Solarized orange
	}
	if strings.Contains(ev, "critical") ||
		strings.Contains(ev, "ierr") ||
		strings.Contains(ev, "failure") ||
		strings.Contains(ev, "fatal") {
		return "#DC322F" // Solarized red
	}
	if strings.Contains(ev, "warning") ||
		strings.Contains(ev, "degraded") ||
		strings.Contains(ev, "correctable") {
		return "#B58900" // Solarized yellow
	}
	if strings.Contains(ev, "ok") ||
		strings.Contains(ev, "working") ||
		strings.Contains(ev, "presence") ||
		strings.Contains(ev, "s0/g0") ||
		strings.Contains(ev, "power on") {
		return "#859900" // Solarized green
	}

	return "#2AA198" // Solarized cyan — informational default
}
