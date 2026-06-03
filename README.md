
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
- Lighthouse glyph ⛯ marks confirmed BMC hosts; `[RF]` tag (dim) marks Redfish-confirmed hosts — informational only, not a gate
- Tree-view results with Tab-expandable host detail (hostname, vendor, confidence, IPMI version, hardware, session status)
- Scrollable results tree with cursor tracking and `... N more hosts` footer on large subnets

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
- **SOL console** — Serial over LAN runs inside a TUI pane (`[O]` from BMC Info); ipmitool executes in a pty so the menu bar and status bar stay visible; ANSI stripped, 1000-line scrollback buffer, `[F10]` disconnect, `[PgUp/PgDn]` scroll; Ctrl+C forwarded to the BMC rather than quitting the app
- **Virtual media** — Redfish ISO mount and eject via `InsertMedia`/`EjectMedia` actions; walks `Managers → VirtualMedia` to find the CD/DVD slot (`[V]` from BMC Info; requires Redfish-capable host)

### Architecture

- Provider abstraction interface (groundwork only; vendor implementations pending)
- Parallel IPMI queries — mc info, lan print, chassis status fetched concurrently
- Per-command timeouts — 15 s for single-round-trip commands; 90 s for bulk SDR/SEL reads
- ipmitool credential string zeroed from memory after each command
- Structured file logger (`internal/logging`) — level-filtered, RFC3339 timestamps, defaults to discard (opt-in)
- Scan progress bar — status bar shows `[████░░░] 89/256 · 3 found` during nmap scans; CIDR host count is the denominator; nmap `--stats-every 2s` drives the bar; `</host>` closures in the XML stream increment the found counter

### User Management

- **User list screen** — scrollable table of ID, name, enabled state, privilege level; `[U]` from BMC Info; cursor navigation with `[↑↓/jk]`; `[Enter]` opens per-user action dialog; `[N]` creates a new user
- **User management dialogs** — enable/disable, set password (with confirmation), set username, set privilege level (User/Operator/Administrator/OEM); user list refreshes automatically after each action
- **Create user** — `[N]` from user list; picks the first free slot automatically; sets username, password (with confirmation), and privilege level in one dialog
- **Delete user** — Delete button in the per-user action dialog; two-step confirmation; disables the account and clears the slot name (IPMI has no native delete command)
- Backend (`internal/ipmi/users.go`): `GetUsers`, `CreateUser`, `DeleteUser`, `EnableUser`, `DisableUser`, `SetUserPassword`, `SetUserName`, `SetUserPrivilege`; passwords wiped from memory post-call

### Firmware Compliance

- **Compliance screen** — `[C]` from BMC Info; two-pass check:
  1. Heuristic pass — IPMI 1.5 (CVE-2013-4782 class), empty firmware revision, factory `0.00` default
  2. Advisory feed pass — queries NIST NVD (CPE-matched) and CISA KEV (active-exploitation flag); runs concurrently after heuristics while the screen is already visible
- **Version-aware CPE matching** — when a firmware revision is available, a version-specific NVD query is attempted first (returns only CVEs affecting that exact version); falls back to a product-family wildcard query if the version string is absent or unrecognised by NVD; status bar shows `[version-specific]` or `[family-wide]` to indicate which mode was used
- Vendor CPE coverage: HP iLO (gen 3–6), Dell iDRAC (7/8/9), Supermicro IPMI, Oracle ILOM, Lenovo XCC, Cisco CIMC, Intel BMC, Huawei iBMC, Fujitsu iRMC, Quanta BMC, AMI MegaRAC (multi-slug vendors queried across all NVD entries and deduplicated)
- CVEs shown with CVSS score, severity badge, and `⚠ KEV` marker if actively exploited in the wild
- Up to 15 CVEs shown, sorted by CVSS descending with actively-exploited entries first
- Scrollable (`[↑↓/jk]`) when CVE list overflows the screen
- NVD API key optional (add `"nvd_api_key"` to `~/.config/fyrtaarn/config.json`); without a key the rate limit is 5 req/30 s which is fine for single-host spot checks

### Redfish Full Enumeration

- **Redfish screen** — scrollable Systems + Managers view; `[R]` from BMC Info (attempts on any host; surfaces a clean error if Redfish is not available)
- Authenticated walk (`internal/redfish/enumerate.go`): Systems (manufacturer, model, serial, SKU, hostname, BIOS version, power state, CPU count, memory GiB) and Managers (firmware version, health, UUID)

---

## Planned Features

### Authentication

- LDAP
- Active Directory
- RADIUS

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

## Advisory Feed Setup

The firmware compliance screen (`[C]`) queries two public sources:

| Source | Key required | Rate limit |
|---|---|---|
| NIST NVD | No (optional) | 5 req / 30 s without key · 50 req / 30 s with key |
| CISA KEV | No | No limit — single bulk download, cached 1 h |

### NIST NVD API Key

A key is not required. For single-host spot checks the unauthenticated limit is
fine. If you are running compliance checks against many hosts in quick succession,
get a free key.

**How to register (takes about 2 minutes):**

1. Go to <https://nvd.nist.gov/developers/request-an-api-key>
2. Enter your email address and submit the form
3. NIST emails you a UUID-format key immediately — no payment, no account, no approval process
4. The key looks like: `xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx`

**Add the key to `~/.config/fyrtaarn/config.json`:**

```json
{
  "theme": "dracula",
  "last_subnet": "192.168.0.0/24",
  "nvd_api_key": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
}
```

The file is created automatically on first run. You can add `nvd_api_key` by hand
with any text editor — fyrtaarn preserves it across theme and scan config saves.

### CISA Known Exploited Vulnerabilities

No setup required. fyrtaarn fetches the catalog from the public CISA endpoint
on demand and caches it in-process for one hour. CVEs present in the catalog are
flagged `⚠ KEV` in red on the compliance screen — these are vulnerabilities with
confirmed active exploitation in the wild and should be treated as urgent regardless
of their CVSS score.

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
| U           | BMC Info       | Load user account list         |
| C           | BMC Info       | Run firmware compliance check  |
| R           | BMC Info       | Redfish full enumeration       |
| P           | BMC Info       | Power control dialog           |
| O           | BMC Info       | Open SOL console (in-pane)     |
| F10         | SOL Console    | Disconnect and return          |
| PgUp        | SOL Console    | Scroll up through buffer       |
| PgDn        | SOL Console    | Scroll down / snap to bottom   |
| V           | BMC Info       | Virtual media dialog (Redfish) |
| Enter       | User list      | Open action dialog for user    |
| N           | User list      | Create new user                |
| F9 > Export | Results        | Export inventory to CSV / JSON |
| Esc / Q     | Most screens   | Back / quit                    |

---

## Development Status

Active development — core discovery, authentication, and management workflows are functional.

Currently focused on:

- provider-specific vendor implementations
- expanded Redfish coverage
- fleet / cluster operations

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

### Longer Term

- native IPMI support in Go
- SSH tunnelling support
- VPN-aware workflows
- inventory export
- sensor dashboards
- cluster / fleet operations

---

## Changelog

### 0.1.1

- **Version-aware CPE matching** — firmware compliance check (`[C]`) now tries a version-specific NVD CPE query first when a firmware revision is known; only falls back to a product-family wildcard if the version yields no results or is absent; status bar appends `[version-specific]` or `[family-wide]` so the scope is always visible
- **Enriched export** — CSV and JSON exports now include BMC detail columns (`firmware_revision`, `ipmi_version`, `bmc_manufacturer`, `bmc_product`, `bmc_mac`, `bmc_ip`, `bmc_gateway`) for any host that was connected to during the session; JSON gets a nested `bmc_details` object; hosts never connected to export blank/omitted fields as before
- **Broader vendor CPE coverage** — advisory feed now covers Cisco CIMC (queried under two NVD slugs), Intel BMC, Huawei iBMC, and Fujitsu iRMC; AMI MegaRAC and Quanta BMC now query multiple NVD product slugs and deduplicate by CVE ID to avoid duplicates
- **IPMI username bug fix** — `parseUserList` now correctly handles ipmitool output rows where the Name column is absent (empty slot); previously the Callin boolean was being parsed as the username, causing new users to be created with `true` as their name
- **Tree root `│` selectable** — the discovery tree root glyph is now a proper selection target; cursor can sit on `│` before moving down to the first host; `selectedHost = -1` represents this state
- **Scan progress bar fix** — nmap `--stats-every 2s` emits `<taskprogress percent="..."/>` XML elements in the stdout stream (not plain text); added `parseXMLProgress` to extract these correctly; progress bar now advances in real time rather than jumping 0 → 100

### 0.1.0

- **Inventory export** — `File > Export...` writes all discovered hosts to disk; choose CSV (one row per host with header comment) or JSON (array of objects); path supports `~/` expansion; export is non-blocking and result appears in the status bar
- **Scan results pagination** — discovery tree now scrolls on large subnets; cursor tracks correctly through `[↑↓/jk]`; status bar shows `[N/total]` and `(showing X–Y)` when the list overflows; `... N more hosts` footer visible at the bottom of a truncated list
- **`[RF]` tag demoted to indicator** — tag is now rendered dim/faint in the accent colour rather than plain text, signalling "confirmed at scan time" rather than a gate; `[R]` Redfish enumeration works on any host regardless
- `internal/export` package: `WriteCSV`, `WriteJSON`

### 0.0.9

- **User create** — `[N]` from the user list screen opens a create dialog; first free slot (disabled + blank name) is selected automatically; username, password (with confirmation), and privilege level (User/Operator/Administrator) set in one step; user list refreshes on success
- **User delete** — Delete button added to the per-user action dialog; two-step confirmation with amber warning; disables the account and blanks the slot name (IPMI has no native delete command; this is the standard approach)
- **Redfish on any host** — `[R]` from BMC Info now attempts Redfish enumeration regardless of whether the host was flagged `[RF]` at scan time; surfaces a clean error message if Redfish is unavailable rather than refusing silently
- Backend additions: `ipmi.CreateUser`, `ipmi.DeleteUser`

### 0.0.8

- **Firmware advisory feed** — compliance screen (`[C]`) now runs a second async pass after heuristics: queries NIST NVD via CPE 2.3 formatted-string-binding (wildcard version, product-family scoped) and cross-references CISA Known Exploited Vulnerabilities catalog; NVD API key optional (free registration); CISA KEV cached in-process for 1 hour
- CPE mapping table covers HP iLO (gen 3–6), Dell iDRAC (7/8/9), Supermicro IPMI, Oracle ILOM, Lenovo XCC, Quanta BMC, AMI MegaRAC
- CVEs shown with CVSS v3.1/v3.0/v2 score, severity badge (CRITICAL/HIGH/MEDIUM/LOW colour-coded), and `⚠ KEV` marker for actively-exploited entries; sorted CVSS descending with KEV entries first; capped at 15
- Compliance screen is now scrollable (`[↑↓/jk]`) once advisory results populate
- `nvd_api_key` field added to `~/.config/fyrtaarn/config.json`; preserved across theme and scan config saves
- `internal/advisory` package: `advisory.go`, `cpe_map.go`, `nvd.go`, `kev.go`

### 0.0.7

- **User write-action dialogs** — `[Enter]` on the user list opens a per-user action picker; available actions: Enable/Disable (toggled by current state), Set Password (with confirmation field), Set Username, Set Privilege (User/Operator/Administrator/OEM); user list refreshes automatically after each successful action
- **User list selection** — cursor-based row navigation (`[↑↓/jk]`) with selection highlighting; scroll follows the cursor; status bar shows `[N/total]` position and `[Enter] Manage` hint
- User write-action dialogs are multi-step: the action picker opens a sub-dialog for password/name/privilege, keeping `pendingUserID` in scope throughout; password mismatch and empty-name validation surfaced as status bar messages

### 0.0.6

- **In-pane SOL console** — `[O]` from BMC Info now opens Serial over LAN inside the TUI rather than handing the terminal to ipmitool; the menu bar and status bar remain visible throughout. ipmitool runs inside a pty (`github.com/creack/pty`); ANSI/VT100 escape codes are stripped and output captured into a 1000-line scrollback buffer. All keys forwarded to the BMC; `[F10]` disconnects; `[PgUp/PgDn]` scrolls the buffer; any forwarded keypress snaps back to the bottom. Ctrl+C is forwarded to the SOL session rather than quitting the app. Requires `go mod tidy` on first build to pull in the pty dependency.
- **Scan progress bar** — status bar now shows a live `[████░░░] 89/256 · 3 found` progress bar during nmap scans; denominator is the CIDR host count (`CIDRHostCount`); numerator advances from nmap `--stats-every 2s` percentage updates parsed from stderr; discovered host count increments on each `</host>` closure in the XML stream
- **Progress bar wired** — `Progress` type now rendered in the loading screen; resets cleanly when SDR/SEL/FRU loads complete; bar appears when total count is available
- **Structured logging** — `internal/logging` is now a real level-filtered logger (DEBUG/INFO/WARN/ERROR) writing RFC3339 timestamped lines to a file; defaults to discard so existing behaviour is unchanged
- **User management** (`internal/ipmi/users.go` + TUI screen) — `[U]` from BMC Info; scrollable list of ID, name, enabled state, privilege level; backend supports enable/disable, set password, set name, set privilege; write-action dialogs planned
- **Firmware compliance** (`internal/ipmi/firmware.go` + TUI screen) — `[C]` from BMC Info; colour-coded pass/fail with per-issue amber warnings; flags IPMI 1.5, empty revision, and `0.00` factory default
- **Redfish full enumeration** (`internal/redfish/enumerate.go` + TUI screen) — `[R]` from BMC Info (Redfish hosts only); authenticated walk of Systems (manufacturer, model, serial, SKU, hostname, BIOS version, power state, CPU count, memory GiB) and Managers (firmware version, health, UUID); scrollable

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
