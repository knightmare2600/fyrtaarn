
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
- Multi-theme engine with 20 themes (F2 to cycle; Theme submenu for direct selection)
- Keyboard navigation and mouse support (cell-motion)
- Menu bar (F9) — File > New Scan / Theme submenu / Exit; Help > About
- Compact MC-style dropdown menus with submenu navigation
- Async spinner during all network operations
- Status bar shows context-sensitive keyboard hints per screen with live pagination for scrollable views; shows "No scan performed" before the first scan runs
- Chrome/content colour split: menu bar and status bar use their own foreground/background pair (`Chrome`/`ChromeFg`) independent of the content area, allowing light-chrome themes (Pan Am, Network SouthEast) to render correctly
- Config file (`~/.config/fyrtaarn/config.json`) persists theme, last-used subnet, and last-used custom port list

### Discovery

- Configurable CIDR subnet entry via scan dialog
- **Scan profiles** — Quick (`-T5`, port 623 only), Standard (`-T4`, ports 623/443/80), Deep (`-T3`, extended port set), Custom (free-form port list)
- Asynchronous nmap execution — TCP connect scan unprivileged; SYN + UDP scan as root
- nmap privilege detection — scan dialog shows an amber `⚠` warning when running without root; SYN/UDP-dependent profiles degrade gracefully to TCP connect
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
- **SOL console** — Serial over LAN via `ipmitool sol activate`; TUI suspends and hands the full terminal to the SOL session; resuming returns to the BMC info screen (`[O]` from BMC Info)
- **Virtual media** — Redfish ISO mount and eject via `InsertMedia`/`EjectMedia` actions; walks `Managers → VirtualMedia` to find the CD/DVD slot (`[V]` from BMC Info; requires Redfish-capable host)

### Architecture

- Provider abstraction interface (groundwork only; vendor implementations pending)
- Parallel IPMI queries — mc info, lan print, chassis status fetched concurrently
- Per-command timeouts — 15 s for single-round-trip commands; 90 s for bulk SDR/SEL reads
- ipmitool credential string zeroed from memory after each command
- Structured file logger (`internal/logging`) — level-filtered, RFC3339 timestamps, defaults to discard (opt-in)
- Live scan streaming — nmap log events surfaced to the status bar while a scan is running; progress bar shown in the loading screen when count data is available

### User Management (backend)

Functions available in `internal/ipmi/users.go` — TUI screen pending:

- `GetUsers` — `ipmitool user list` with parsed privilege levels
- `EnableUser` / `DisableUser`
- `SetUserPassword` — password wiped from memory after the call
- `SetUserName`
- `SetUserPrivilege` — maps to `ipmitool channel setaccess`

### Firmware Compliance (backend)

Heuristic checks in `internal/ipmi/firmware.go` — TUI screen pending:

- IPMI 1.5 detection (CVE-2013-4782 class — flags for human review)
- Empty firmware revision (unconfigured or bricked BMC)
- Firmware revision `0.00` / `00.00` (factory default, update required)

### Redfish Full Enumeration (backend)

Authenticated walk in `internal/redfish/enumerate.go` — TUI screen pending:

- Systems collection: manufacturer, model, serial, SKU, hostname, BIOS version, power state, CPU count, memory GiB
- Managers collection: name, firmware version, health status, UUID

---

## Planned Features

### Authentication

- LDAP
- Active Directory
- RADIUS

### Management

- User management TUI screen (backend complete — `internal/ipmi/users.go`)
- Firmware compliance TUI screen (backend complete — `internal/ipmi/firmware.go`)

### Discovery / Redfish

- Redfish full enumeration TUI screen (backend complete — `internal/redfish/enumerate.go`)

### Virtual Media

- Floppy image support
- Remote media redirection

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
| O           | BMC Info       | Open SOL console               |
| V           | BMC Info       | Virtual media dialog (Redfish) |
| Esc / Q     | Most screens   | Back / quit                    |

---

## Development Status

Active development — core discovery, authentication, and management workflows are functional.

Currently focused on:

- provider-specific vendor implementations
- Redfish full enumeration
- user management

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

- User management TUI screen (`internal/ipmi/users.go` is complete; needs screen + keybinding)
- Firmware compliance TUI screen (`internal/ipmi/firmware.go` is complete; needs screen + keybinding)
- Redfish full enumeration TUI screen (`internal/redfish/enumerate.go` is complete; needs screen + keybinding)

### Longer Term

- native IPMI support in Go
- SSH tunnelling support
- VPN-aware workflows
- inventory export
- sensor dashboards
- cluster / fleet operations

---

## Changelog

### 0.0.6

- **Live scan streaming** — nmap log events now surfaced to the status bar while a scan is running, replacing the static "Scanning…" message; uses `RunScanStream` internally via a buffered progress channel
- **Progress bar wired** — `Progress` type now rendered in the loading screen; resets cleanly when SDR/SEL/FRU loads complete; bar appears when total count is available
- **Structured logging** — `internal/logging` is now a real level-filtered logger (DEBUG/INFO/WARN/ERROR) writing RFC3339 timestamped lines to a file; defaults to discard so existing behaviour is unchanged
- **User management backend** (`internal/ipmi/users.go`) — `GetUsers`, `EnableUser`, `DisableUser`, `SetUserPassword`, `SetUserName`, `SetUserPrivilege`; password wiped from memory post-call; TUI screen pending
- **Firmware compliance backend** (`internal/ipmi/firmware.go`) — `GetFirmwareInfo`, `CheckFirmwareCompliance`; flags IPMI 1.5, empty revision, and `0.00` factory default; TUI screen pending
- **Redfish full enumeration backend** (`internal/redfish/enumerate.go`) — authenticated walk of Systems (manufacturer, model, serial, SKU, hostname, BIOS version, power state, CPU count, memory GiB) and Managers (firmware version, health, UUID); TUI screen pending

### 0.0.5

- Theme engine reworked: `Chrome`/`ChromeFg` colour pair for menu bar and status bar, independent of content area colours — light-chrome themes (Pan Am, Network SouthEast) now render correctly
- 20 themes total — added class91, dark, db-1980s, gemstones, light, pan-am, twa, viarail-soft; corrected British (red chrome, Prussian blue content) and ScotRail (lighter Saltire blue content, navy chrome, yellow chrome text)
- Dropdown menus rendered as an overlay on content instead of inserted lines — no longer "gobbles" lines from the content area when open
- Status bar shows context-sensitive keyboard hints per screen with live pagination for scrollable views
- Status bar shows "No scan performed" before the first scan; "No hosts found" after a scan returns zero results
- nmap privilege detection — scan dialog shows amber `⚠` warning when running without root
- Custom scan profile — free-form port list input in scan dialog; last-used ports persisted to config
- SOL console (`[O]` from BMC Info) — hands full terminal to `ipmitool sol activate` via `tea.ExecProcess`; returns to TUI on exit
- Virtual media groundwork (`[V]` from BMC Info) — Redfish ISO mount and eject via `InsertMedia`/`EjectMedia` actions on Redfish-capable hosts
- Easter egg: `£` accepted as second trigger key alongside `ø`; second press of either key dismisses the scroller and restores the status bar hint

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
