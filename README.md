
# Fyrtaarn

Nordic Out-of-Band Management for IPMI, BMC, iLO, DRAC, and friends.

---

## Overview

`fyrtaarn` is a cross-platform Golang TUI application for discovering, inspecting, and managing out-of-band management controllers such as:

- SuperMicro IPMI
- Quanta BMC
- Dell DRAC
- HP iLO
- Oracle/Sun ILOM
- generic IPMI 1.5 / 2.0 devices

The project aims to provide a clean, fast, keyboard-friendly alternative to:

- ancient Java KVM launchers
- vendor-specific Windows-only tooling
- inconsistent web interfaces
- fragile browser compatibility requirements

The long-term goal is to create a modern terminal-first operational toolkit for infrastructure engineers, homelab users, incident responders, and security researchers.

---

## Why "Fyrtaarn"?

"Fyrtaarn" is Danish for:

> lighthouse / beacon / watchtower

The name reflects the project's purpose: visibility, remote control, and infrastructure oversight — without tying the project to a single vendor.

---

## Current Features

### Interface

- Bubble Tea TUI with alt-screen mode
- Multi-theme engine with 12 themes (F2 to cycle; Theme submenu for direct selection)
- Keyboard navigation and mouse support (cell-motion)
- Menu bar (F9) — File > New Scan / Theme submenu / Exit; Help > About
- Compact MC-style dropdown menus with submenu navigation
- Async spinner during all network operations
- Status bar shows current theme name
- Config file (`~/.config/fyrtaarn/config.json`) persists theme and last-used subnet

### Discovery

- Configurable CIDR subnet entry via scan dialog
- **Scan profiles** — Quick (`-T5`, port 623 only), Standard (`-T4`, ports 623/443/80), Deep (`-T3`, extended port set)
- Asynchronous nmap execution — TCP connect scan unprivileged; SYN + UDP scan as root
- `ipmi-version` NSE script fingerprinting when running as root (bundled with nmap, no extra install)
- Nmap XML output parsing with heuristic BMC confidence scoring
- Reverse DNS resolution after scan (async, 2-second per-host timeout)
- Redfish probe after scan — unauthenticated `GET /redfish/v1/` to detect Redfish hosts
- Manufacturer and model fetched unauthenticated via `/redfish/v1/Systems/` where permitted
- Lighthouse glyph ⛯ marks confirmed BMC hosts; `[RF]` tag marks Redfish-capable hosts
- Tree-view results with Tab-expandable host detail (hostname, vendor, confidence, IPMI version, hardware, session status)

### Authentication

- Credential dialog with masked password entry
- In-memory session cache: re-entering a host skips the login dialog
- Credentials XOR-obfuscated with a one-time session key — not held as plaintext
- Failed authentication clears the cache entry; login dialog reappears automatically

### BMC Management

- **BMC info** — device ID, firmware revision, IPMI version, manufacturer, product name
- **Network info** — IP address, MAC, subnet mask, gateway
- **Chassis status** — power state, power fault, power overload, drive fault, cooling fault
- **Sensor / SDR viewer** — scrollable live sensor readings; 90-second timeout for large repositories
- **System Event Log viewer** — scrollable, newest-first, colour-coded by severity:
  - Red — critical, IERR, fatal, failure
  - Orange — non-critical
  - Yellow — warning, degraded, correctable ECC
  - Green — deasserted (condition cleared), OK, working
  - Cyan — informational (default)
- **FRU / hardware inventory** — scrollable `ipmitool fru` output with device section headers
- **Power control** — on, off, soft shutdown, reset; dialog shows current chassis power state

### Architecture

- Provider abstraction interface (groundwork only; vendor implementations pending)
- Parallel IPMI queries — mc info, lan print, chassis status fetched concurrently
- Per-command timeouts — 15 s for single-round-trip commands; 90 s for bulk SDR/SEL reads
- ipmitool credential string zeroed from memory after each command

---

## Planned Features

### Discovery

- Free-form configurable port list (currently profile presets only)
- Redfish full enumeration beyond detection and basic hardware info

### Authentication

- LDAP
- Active Directory
- RADIUS

### Management

- SOL (Serial over LAN) console access
- User management (`ipmitool user`)
- Firmware compliance checks

### Virtual Media

- ISO mounting
- Floppy image support
- Remote media redirection
- Redfish virtual media support

### Providers

Full vendor-specific implementations for:
- SuperMicro
- Quanta
- Dell DRAC
- HP iLO
- Oracle ILOM
- Lenovo XCC

---

## Requirements

Both `nmap` and `ipmitool` must be on your `PATH` at runtime.

> **Note:** `nmap` is required for discovery. `ipmitool` is only required for BMC
> management commands (sensors, SEL, FRU, power). Redfish probing uses Go's built-in
> HTTP client and needs neither tool.

### Linux

```bash
# Debian / Ubuntu / Raspberry Pi OS
sudo apt install -y nmap ipmitool

# Fedora / RHEL / Rocky
sudo dnf install -y nmap ipmitool

# Arch
sudo pacman -S nmap ipmitool
```

Root or `CAP_NET_RAW` is required for SYN scan (`-sS`) and UDP scan (`-sU`).
Without root, the scanner automatically falls back to TCP connect scan (`-sT`),
which loses UDP 623 visibility but still works for TCP-based IPMI and Redfish hosts.

The `ipmi-version` NSE script is **bundled with nmap** — no separate install needed.

### macOS

```bash
brew install nmap ipmitool
```

`ipmitool` on macOS supports IPMI over LAN only (`-I lanplus`) — there is no
`/dev/ipmi0` device on macOS. This is exactly how fyrtaarn uses it.

SYN/UDP scans require root (`sudo`) on macOS as on Linux.

### Windows

```
choco install nmap
```

`ipmitool` is **not available as a Chocolatey package**. The project does not have
an official maintained Windows binary. Options:

- Use **WSL2** (recommended) — install nmap and ipmitool inside the WSL2 environment
  and run fyrtaarn there.
- Build ipmitool from source under **Cygwin** (`choco install cygwin`).

---

## Build

```bash
# Native build
make build
# Output: dist/fyrtaarn

# Cross-compile for ARM (example)
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -o bin/fyrtaarn-armv7 ./cmd/fyrtaarn
```

---

## Run

```bash
make run

# Or with elevated privileges for full scan capability
sudo ./dist/fyrtaarn
```

---

## Key Bindings

| Key         | Context        | Action                         |
|-------------|----------------|--------------------------------|
| F9          | Global         | Open / close menu bar          |
| F2          | Global         | Cycle to next theme            |
| Ctrl+C      | Global         | Quit                           |
| ↑ / k       | Results, lists | Move selection up              |
| ↓ / j       | Results, lists | Move selection down            |
| Tab         | Results        | Expand / collapse host detail  |
| Enter       | Results        | Connect (or reconnect)         |
| S           | BMC Info       | Load sensor list (SDR)         |
| L           | BMC Info       | Load event log (SEL)           |
| F           | BMC Info       | Load FRU / hardware inventory  |
| P           | BMC Info       | Power control dialog           |
| Esc / Q     | Most screens   | Back / quit                    |

---

## Development Status

Active development — core discovery, authentication, and management workflows are functional.

Currently focused on:

- provider-specific vendor implementations
- Redfish full enumeration
- SOL console access

---

## Project Goals

- cross-platform
- minimal dependencies
- terminal-first UX
- keyboard-first operation
- vendor abstraction
- operational simplicity
- infrastructure-friendly workflows
- sane theming
- no browser dependency
- no Java dependency

---

## Design Philosophy

`fyrtaarn` intentionally shells out to mature tooling:

- `nmap` for discovery
- `ipmitool` for BMC management

rather than prematurely reimplementing decades of vendor quirks in pure Go.
Native protocol implementations may follow once the operational workflows are solid.

---

## Inspirations & Acknowledgements

### Dan Kaminsky

For pioneering work on:

- BMC/IPMI security research
- remote management attack surfaces
- infrastructure visibility

His work helped expose how dangerous and under-examined out-of-band systems could be.
A small tribute remains in the code.

### IppSec

For educational walkthroughs and practical demonstrations including:

- Keeper
- Shibboleth
- Active Directory abuse
- infrastructure operations
- Trying your hardest to say Rødgrød med fløde... If you know, you know... ;)

### 0xDF

For detailed technical writeups and excellent explanations of:

- post-exploitation workflows
- "beyond root" methodologies
- operational enumeration
- infrastructure internals
- Jeg var at gå agurk med "should" på "Release Comittee" (:

---

## TODO

### Near Term

- free-form configurable port list for scanning
- SOL console support
- virtual media groundwork
- nmap privilege detection — grey out scan options requiring root when running unprivileged

### Longer Term

- native IPMI support in Go
- SSH tunnelling support
- VPN-aware workflows
- inventory export
- firmware compliance checks
- sensor dashboards
- cluster / fleet operations

---

## Changelog

### 0.0.4

- SEL event log colour coding (red/orange/yellow/green/cyan by severity)
- SDR / SEL commands timeout raised to 90 s (from 15 s) to handle large repositories
- Redfish manufacturer and model fetched unauthenticated post-scan; shown in expanded tree
- Status bar now shows `Theme: <name>` instead of bare theme name
- Power control dialog button spacing tightened
- Easter egg scroller: plain text only, rate slowed to 38400-baud feel (200 ms/char)

### 0.0.3

- nmap `ipmi-version` NSE script integration for root-mode scans
- In-memory session credential cache (XOR-obfuscated, clears on auth failure)
- Reconnect to previously authenticated hosts without re-entering credentials
- Compact MC-style dropdown menus (normal border, no excess padding)
- File menu consolidation: New Scan, Theme submenu, Exit
- Submenu navigation (right-arrow opens, left-arrow returns, Esc closes)
- Lighthouse glyph ⛯ next to confirmed BMC hosts in the discovery tree
- Easter egg scrolling marquee in the status bar
- Reverse DNS resolution after scan (async, 2-second per-host timeout)
- Redfish probe after scan — detection, version, basic hardware info
- `[RF]` tag on Redfish hosts; version and hardware shown on Tab-expand
- Scan profiles: Quick / Standard / Deep
- FRU / hardware inventory screen (`[F]` from BMC Info)
- Config file persists theme and last-used subnet

### 0.0.2

- Per-session credential login dialog (masked password)
- IPMI login workflow and BMC enumeration
- BMC info, network info, chassis status views
- Sensor / SDR viewer (scrollable)
- System Event Log viewer (scrollable, newest-first)
- Power control dialog (on / off / soft / reset)
- Parallel IPMI queries with timeout
- Multi-theme engine: 12 themes
- F2 theme cycling and Theme menu
- Configurable subnet via scan dialog
- Async spinner for all network operations
- Selectable discovery results with tree expand

### 0.0.1

Initial proof-of-concept.

- Project structure
- Bubble Tea TUI
- Keyboard navigation and popup dialogs
- Async nmap execution and XML parsing
- Heuristic BMC detection
- GitHub Actions build workflow

---

## License

GPL-3.0-or-later
