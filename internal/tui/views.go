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
	line := fmt.Sprintf("\n\n  %s  %s\n", a.spinner.View(), a.status)
	if a.loadProgress.Total > 0 {
		line += "\n  " + a.loadProgress.Render(a.width-4) + "\n"
	}
	line += "\n  [Ctrl+C] Quit"
	return line
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

/* ---------------- USERS ---------------- */

func (a *App) renderUsers() string {
	var b strings.Builder

	hostIP := ""
	if len(a.results) > 0 && a.selectedHost < len(a.results) {
		hostIP = a.results[a.selectedHost].IP
	}

	b.WriteString(HeaderStyle().Render(fmt.Sprintf("USER ACCOUNTS — %s", hostIP)) + "\n")
	b.WriteString(strings.Repeat("─", 50) + "\n\n")

	if len(a.users) == 0 {
		b.WriteString("  No users found.\n")
		return b.String()
	}

	visibleLines := a.contentH - 8
	if visibleLines < 1 {
		visibleLines = 5
	}

	end := a.usersOffset + visibleLines
	if end > len(a.users) {
		end = len(a.users)
	}

	hdr := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent))
	b.WriteString(fmt.Sprintf("  %s\n", hdr.Render(fmt.Sprintf("%-4s %-20s %-8s %s", "ID", "Name", "Enabled", "Privilege"))))
	b.WriteString(fmt.Sprintf("  %s\n", strings.Repeat("─", 60)))

	for i, u := range a.users[a.usersOffset:end] {
		absIdx := a.usersOffset + i
		selected := absIdx == a.selectedUser
		enabled := "Yes"
		if !u.Enabled {
			enabled = "No"
		}
		row := fmt.Sprintf("  %-4d %-20s %-8s %s", u.ID, u.Name, enabled, u.Privilege)
		if selected {
			b.WriteString(MenuStyle(true).Render(row) + "\n")
		} else {
			b.WriteString(row + "\n")
		}
	}

	return b.String()
}

/* ---------------- FIRMWARE COMPLIANCE ---------------- */

func (a *App) renderFirmware() string {
	hostIP := ""
	if len(a.results) > 0 && a.selectedHost < len(a.results) {
		hostIP = a.results[a.selectedHost].IP
	}

	hdr := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent))

	// Header lines — always shown, never scrolled.
	var header strings.Builder
	header.WriteString(HeaderStyle().Render(fmt.Sprintf("FIRMWARE COMPLIANCE — %s", hostIP)) + "\n")
	header.WriteString(strings.Repeat("─", 50) + "\n\n")

	if a.firmwareResult == nil {
		header.WriteString("  No compliance data.\n")
		return header.String()
	}

	r := a.firmwareResult
	header.WriteString(hdr.Render("  FIRMWARE") + "\n")
	header.WriteString(fmt.Sprintf("  %-24s %s\n", "Manufacturer:", r.Info.ManufacturerName))
	header.WriteString(fmt.Sprintf("  %-24s %s\n", "Product:", r.Info.ProductName))
	header.WriteString(fmt.Sprintf("  %-24s %s\n", "Firmware Revision:", r.Info.FirmwareRevision))
	header.WriteString(fmt.Sprintf("  %-24s %s\n", "IPMI Version:", r.Info.IPMIVersion))
	header.WriteString("\n")

	if r.Compliant {
		header.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#859900")).Bold(true).Render("✓ HEURISTICS PASS") + "\n")
	} else {
		header.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color("#DC322F")).Bold(true).Render("✗ HEURISTIC ISSUES") + "\n\n")
		header.WriteString(hdr.Render("  ISSUES") + "\n")
		for _, issue := range r.Issues {
			header.WriteString("  " + lipgloss.NewStyle().Foreground(lipgloss.Color(CurrentTheme.Warning)).Render("⚠ "+issue) + "\n")
		}
	}

	header.WriteString("\n")
	header.WriteString(hdr.Render("  ADVISORY FEED (NVD + CISA KEV)") + "\n")
	header.WriteString(fmt.Sprintf("  %s\n", strings.Repeat("─", 60)))

	if a.advisoryLoading {
		header.WriteString("  Fetching from NVD...\n")
		return header.String()
	}

	if len(a.firmwareAdvisory) == 0 {
		header.WriteString("  No CVEs found for this product family.\n")
		return header.String()
	}

	// Build scrollable CVE lines.
	var cveLines []string
	for _, f := range a.firmwareAdvisory {
		severityColor := advisorySeverityColor(f.Severity)
		badge := lipgloss.NewStyle().
			Foreground(lipgloss.Color(severityColor)).
			Bold(true).
			Render(fmt.Sprintf("[%s %.1f]", f.Severity, f.CVSS))

		kevBadge := ""
		if f.ActivelyExploited {
			kevBadge = " " + lipgloss.NewStyle().
				Foreground(lipgloss.Color("#DC322F")).
				Bold(true).
				Render("⚠ KEV")
		}

		cveLines = append(cveLines, fmt.Sprintf("  %-18s %s%s", f.ID, badge, kevBadge))
		if f.Description != "" {
			cveLines = append(cveLines, fmt.Sprintf("  %-18s %s", "",
				lipgloss.NewStyle().Foreground(lipgloss.Color(CurrentTheme.Foreground)).Render(f.Description)))
		}
		cveLines = append(cveLines, "")
	}

	visibleLines := a.contentH - strings.Count(header.String(), "\n") - 2
	if visibleLines < 1 {
		visibleLines = 5
	}

	start := a.firmwareOffset
	if start >= len(cveLines) {
		start = 0
	}
	end := start + visibleLines
	if end > len(cveLines) {
		end = len(cveLines)
	}

	var b strings.Builder
	b.WriteString(header.String())
	for _, line := range cveLines[start:end] {
		b.WriteString(line + "\n")
	}

	return b.String()
}

// advisorySeverityColor returns a Solarized-palette hex colour for a CVSS severity label.
func advisorySeverityColor(severity string) string {
	switch severity {
	case "CRITICAL":
		return "#DC322F"
	case "HIGH":
		return "#CB4B16"
	case "MEDIUM":
		return "#B58900"
	case "LOW":
		return "#859900"
	default:
		return "#2AA198"
	}
}

/* ---------------- REDFISH FULL ENUMERATION ---------------- */

func (a *App) renderRedfishEnum() string {
	var b strings.Builder

	hostIP := ""
	if len(a.results) > 0 && a.selectedHost < len(a.results) {
		hostIP = a.results[a.selectedHost].IP
	}

	b.WriteString(HeaderStyle().Render(fmt.Sprintf("REDFISH ENUMERATION — %s", hostIP)) + "\n")
	b.WriteString(strings.Repeat("─", 50) + "\n\n")

	if a.redfishEnum == nil {
		b.WriteString("  No Redfish data.\n")
		return b.String()
	}

	// Build full content lines then apply scroll offset.
	var lines []string
	hdr := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(CurrentTheme.Accent))

	for i, sys := range a.redfishEnum.Systems {
		lines = append(lines, hdr.Render(fmt.Sprintf("  SYSTEM %d", i+1)))
		lines = append(lines, renderRedfishField("Manufacturer", sys.Manufacturer))
		lines = append(lines, renderRedfishField("Model", sys.Model))
		lines = append(lines, renderRedfishField("Serial", sys.SerialNumber))
		lines = append(lines, renderRedfishField("SKU", sys.SKU))
		lines = append(lines, renderRedfishField("Hostname", sys.HostName))
		lines = append(lines, renderRedfishField("BIOS Version", sys.BIOSVersion))
		lines = append(lines, renderRedfishField("Power State", sys.PowerState))
		if sys.ProcessorCount > 0 {
			lines = append(lines, renderRedfishField("CPUs", fmt.Sprintf("%d", sys.ProcessorCount)))
		}
		if sys.MemoryGiB > 0 {
			lines = append(lines, renderRedfishField("Memory", fmt.Sprintf("%.0f GiB", sys.MemoryGiB)))
		}
		lines = append(lines, "")
	}

	for i, mgr := range a.redfishEnum.Managers {
		lines = append(lines, hdr.Render(fmt.Sprintf("  MANAGER %d  —  %s", i+1, mgr.Name)))
		lines = append(lines, renderRedfishField("Firmware", mgr.FirmwareVersion))
		lines = append(lines, renderRedfishField("Health", mgr.Status))
		lines = append(lines, renderRedfishField("UUID", mgr.UUID))
		lines = append(lines, "")
	}

	visibleLines := a.contentH - 6
	if visibleLines < 1 {
		visibleLines = 5
	}

	start := a.redfishOffset
	if start >= len(lines) {
		start = 0
	}
	end := start + visibleLines
	if end > len(lines) {
		end = len(lines)
	}

	for _, l := range lines[start:end] {
		b.WriteString(l + "\n")
	}

	return b.String()
}

func renderRedfishField(label, value string) string {
	if value == "" {
		return ""
	}
	return fmt.Sprintf("  %-24s %s", label+":", value)
}

/* ---------------- SOL CONSOLE ---------------- */

func (a *App) renderSOL() string {
	var b strings.Builder

	hostIP := ""
	if len(a.results) > 0 && a.selectedHost < len(a.results) {
		hostIP = a.results[a.selectedHost].IP
	}

	b.WriteString(HeaderStyle().Render(fmt.Sprintf("SOL CONSOLE — %s", hostIP)) + "\n")
	b.WriteString(strings.Repeat("─", 50) + "\n")

	if a.solPane == nil {
		b.WriteString("\n  Connecting...\n")
		return b.String()
	}

	pane := a.solPane

	// Snapshot lines + partial current line for rendering.
	displayLines := make([]string, len(pane.lines))
	copy(displayLines, pane.lines)
	if pane.partial != "" {
		displayLines = append(displayLines, pane.partial+"▌") // blinking cursor hint
	}

	visibleLines := a.contentH - 4 // header + divider + 2 padding rows
	if visibleLines < 1 {
		visibleLines = 5
	}

	total := len(displayLines)
	bottom := total - pane.scrollUp
	if bottom > total {
		bottom = total
	}
	if bottom < 0 {
		bottom = 0
	}
	top := bottom - visibleLines
	if top < 0 {
		top = 0
	}

	maxWidth := a.width - 2
	if maxWidth < 1 {
		maxWidth = 80
	}

	for _, line := range displayLines[top:bottom] {
		runes := []rune(line)
		if len(runes) > maxWidth {
			line = string(runes[:maxWidth])
		}
		b.WriteString(line + "\n")
	}

	// Scroll indicator when not pinned to bottom.
	if pane.scrollUp > 0 {
		indicator := lipgloss.NewStyle().
			Foreground(lipgloss.Color(CurrentTheme.Warning)).
			Render(fmt.Sprintf("  ── scrolled %d lines up ──", pane.scrollUp))
		b.WriteString(indicator + "\n")
	}

	return b.String()
}
