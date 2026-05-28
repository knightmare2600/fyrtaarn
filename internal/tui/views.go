package tui

import (
	"fmt"
	"strings"
)

func (a *App) renderMCInfo() string {
	var b strings.Builder

	hostIP := ""
	if len(a.results) > 0 && a.selectedHost < len(a.results) {
		hostIP = a.results[a.selectedHost].IP
	}

	b.WriteString(fmt.Sprintf("BMC INFORMATION — %s\n", hostIP))
	b.WriteString(strings.Repeat("═", 50) + "\n\n")

	if a.mcInfo != nil {
		b.WriteString("  CONTROLLER\n")
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Device ID:", a.mcInfo.DeviceID))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Firmware Revision:", a.mcInfo.FirmwareRevision))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "IPMI Version:", a.mcInfo.IPMIVersion))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Manufacturer:", a.mcInfo.ManufacturerName))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Product:", a.mcInfo.ProductName))
		b.WriteString("\n")
	}

	if a.lanInfo != nil {
		b.WriteString("  NETWORK\n")
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
		b.WriteString("  CHASSIS\n")
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Power:", powerStr))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Power Fault:", boolYesNo(a.chassis.PowerFault)))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Power Overload:", boolYesNo(a.chassis.PowerOverload)))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Drive Fault:", boolYesNo(a.chassis.DriveFault)))
		b.WriteString(fmt.Sprintf("  %-24s %s\n", "Cooling Fault:", boolYesNo(a.chassis.CoolingFault)))
		b.WriteString("\n")
	}

	b.WriteString("  [S] Sensors  [L] Event Log  [P] Power Control  [ESC] Back")
	return b.String()
}

func (a *App) renderSensors() string {
	var b strings.Builder

	hostIP := ""
	if len(a.results) > 0 && a.selectedHost < len(a.results) {
		hostIP = a.results[a.selectedHost].IP
	}

	b.WriteString(fmt.Sprintf("SENSORS / SDR — %s\n", hostIP))
	b.WriteString(strings.Repeat("═", 50) + "\n\n")

	if len(a.sensors) == 0 {
		b.WriteString("  No sensor data available.\n")
		b.WriteString("\n  [ESC] Back")
		return b.String()
	}

	visibleLines := a.height - 8
	if visibleLines < 1 {
		visibleLines = 10
	}

	end := a.sdrOffset + visibleLines
	if end > len(a.sensors) {
		end = len(a.sensors)
	}

	b.WriteString(fmt.Sprintf("  %-25s %-22s %s\n", "Sensor", "Reading", "Status"))
	b.WriteString(fmt.Sprintf("  %s\n", strings.Repeat("─", 60)))

	for _, e := range a.sensors[a.sdrOffset:end] {
		b.WriteString(fmt.Sprintf("  %-25s %-22s %s\n", e.Name, e.Value, e.Status))
	}

	b.WriteString(fmt.Sprintf("\n  Showing %d–%d of %d  [↑/k] Up  [↓/j] Down  [ESC] Back",
		a.sdrOffset+1, end, len(a.sensors)))

	return b.String()
}

func (a *App) renderSEL() string {
	var b strings.Builder

	hostIP := ""
	if len(a.results) > 0 && a.selectedHost < len(a.results) {
		hostIP = a.results[a.selectedHost].IP
	}

	b.WriteString(fmt.Sprintf("SYSTEM EVENT LOG — %s\n", hostIP))
	b.WriteString(strings.Repeat("═", 50) + "\n\n")

	if len(a.selEntries) == 0 {
		b.WriteString("  No events logged.\n")
		b.WriteString("\n  [ESC] Back")
		return b.String()
	}

	visibleLines := a.height - 8
	if visibleLines < 1 {
		visibleLines = 10
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

	b.WriteString(fmt.Sprintf("  %-6s %-20s %s\n", "ID", "Timestamp", "Event"))
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
		b.WriteString(fmt.Sprintf("  %-6s %-20s %s%s\n", e.ID, e.Timestamp, event, dir))
	}

	b.WriteString(fmt.Sprintf("\n  Showing %d–%d of %d (newest first)  [↑/k] Up  [↓/j] Down  [ESC] Back",
		a.selOffset+1, end, total))

	return b.String()
}

func (a *App) renderPower() string {
	var b strings.Builder

	hostIP := ""
	if len(a.results) > 0 && a.selectedHost < len(a.results) {
		hostIP = a.results[a.selectedHost].IP
	}

	b.WriteString(fmt.Sprintf("POWER CONTROL — %s\n", hostIP))
	b.WriteString(strings.Repeat("═", 50) + "\n\n")

	if a.chassis != nil {
		powerStr := "Off"
		if a.chassis.PowerOn {
			powerStr = "On"
		}
		b.WriteString(fmt.Sprintf("  Current state: %s\n\n", powerStr))
	}

	if a.powerAction == "" {
		b.WriteString("  [O]  Power On\n")
		b.WriteString("  [F]  Power Off (forced)\n")
		b.WriteString("  [S]  Soft Shutdown (graceful ACPI)\n")
		b.WriteString("  [R]  Reset\n\n")
		b.WriteString("  [ESC] Back to BMC Info")
	} else {
		labels := map[string]string{
			"on":    "Power On",
			"off":   "Power Off (forced)",
			"soft":  "Soft Shutdown",
			"reset": "Reset",
		}
		b.WriteString(fmt.Sprintf("  Confirm: %s %s?\n\n", labels[a.powerAction], hostIP))
		b.WriteString("  [Y]  Confirm\n")
		b.WriteString("  [N]  Cancel\n\n")
		b.WriteString("  [ESC] Cancel")
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
