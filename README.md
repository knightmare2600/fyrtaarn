
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
- Menu bar (F9) — File > **Connect to BMC** / **Recent Connections** / **Favourites** / New Scan / Export / **Start Log** / **Stop Log** / Theme submenu / Exit; Help > About
- Compact MC-style dropdown menus with submenu navigation
- MC-style modal windows — all dialogs and popups rendered as centered bordered windows with the title embedded in the top border line (`╭── Title ──╮`) and a drop shadow; consistent chrome across scan dialog, login, export, power control, user management, virtual media, about, and loading screens
- Menu bar keyboard accelerators — F9 activates the bar; bare letter keys navigate without Alt: **F**ile / **H**elp at top level; **C**onnect to BMC / **R**ecent Connections / Fa**v**ourites (**M**anage Favourites) / **N**ew Scan / **E**xport / **L**og start / **S**top log / **T**heme / e**X**it / **A**bout in dropdowns; accelerator character rendered underlined
- F2 and F9 pass through to the BMC when the SOL console is active rather than triggering TUI chrome
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

- **BMC info** — device ID, firmware revision, IPMI version, manufacturer, product name; server model from Redfish where available (fetched with your credentials after login, shown as a separate Model line — some BMCs report only a numeric product name, e.g. iDRAC9 `Unknown (0x100)`)
- **Network info** — IP address, MAC, subnet mask, gateway
- **Chassis status** — power state, power fault, power overload, drive fault, cooling fault
- **Sensor / SDR viewer** — scrollable live sensor readings; 90-second timeout for large repositories
- **Sensor dashboard** — on the Sensors screen, `[Space]` marks sensors to watch (★, saved per host in `config.json`) and `[D]` opens a live dashboard of them: current value, unit, session minimum and maximum, and a trend graph, refreshed every 5 s (`[+]`/`[-]` for 2 s to 1 min, `[R]` now). A full sensor list walks the BMC's whole sensor repository (27 s for 85 sensors on the Quanta under Tested Hardware); the dashboard caches that repository once per session (`ipmitool sdr dump`, 5.5 s there) and then reads only its own sensors through the cache (`-S`, 0.3 s for four). Analog sensors only, as ipmitool's `sensor reading` reports no discrete states. Sensors read in Redfish-only mode have their own dashboard, refreshed from the Chassis Thermal and Power resources, with a separate list of chosen sensors (Redfish names differ from IPMI ones)
- **System Event Log viewer** — scrollable, newest-first, colour-coded by severity:
  - Red — critical, IERR, fatal, failure
  - Orange — non-critical
  - Yellow — warning, degraded, correctable ECC
  - Green — deasserted (condition cleared), OK, working
  - Cyan — informational (default)
- **FRU / hardware inventory** — scrollable `ipmitool fru` output with device section headers
- **Power control** — on, off, soft shutdown, reset; dialog shows current chassis power state
- **SOL console** — Serial over LAN runs inside a TUI pane (`[O]` from BMC Info); ipmitool executes in a pty so the menu bar and status bar stay visible; full VT100/VT220 screen emulator (cols×rows character grid) — cursor positioning, clear-screen, scroll regions, insert/delete lines, erase-in-line/display, and all standard CSI sequences are honoured so GRUB menus, AMI/Award BIOS splashes, and interactive programs render correctly; bare `\r` (firmware line endings) and `\r\n` both handled; backspace is implemented as `ESC[D` + `Ctrl+D` (cursor-left then forward-delete) so it works correctly in GRUB's readline regardless of which erase character the BMC serial port uses; 1000-line scrollback history; `[F10]` disconnects cleanly — ipmitool is told to exit and the BMC's SOL slot is released before a new session can start, so reconnecting works straight away; ipmitool runs with Ctrl+] as its escape character so `~.` typed at the start of a line reaches the console instead of killing the session; if ipmitool exits on its own its last message is shown in the status bar; if another session already holds the BMC's SOL console, a dialog asks before taking it over (default Cancel) — the BMC cannot tell a colleague's live console from one left behind by a crashed client; menu-bar mouse clicks are ignored while SOL is open, as F9 is; on Windows the pane runs ipmitool in a pseudo-console (ConPTY, Windows 10 1809 / Server 2019 or later), and on older Windows `[O]` shows the equivalent ipmitool command to run in a terminal instead; `[PgUp/PgDn]` scroll; all keys including F1–F12 forwarded to the BMC; Ctrl+C forwarded rather than quitting the app; pressing `[O]` performs a live chassis power check — if the chassis is off a dialog offers to power it on and open SOL immediately so the full boot sequence is captured from BIOS POST; if the chassis is already on SOL opens directly without a dialog
- **Virtual media** — Redfish image mount and eject via `InsertMedia`/`EjectMedia` actions; walks `Managers → VirtualMedia` to find the slot for the image (`.iso` → CD/DVD; `.img`, `.ima`, `.vfd` → USB/floppy), and Eject removes whatever is mounted (`[V]` from BMC Info; requires Redfish-capable host); accepts an HTTP(S) URL or a local file path — a local ISO is served to the BMC from an ad-hoc HTTP server on this machine (random URL path, local address chosen by route to the BMC) for as long as it stays mounted, stopped on eject, on mounting another image, or on exit; Supermicro hosts are probed for Redfish VirtualMedia first and IKVM-only firmware gets a clear unsupported error
- **Virtual media without Redfish (AMI MegaRAC SP)** — on a BMC that has no Redfish, `[V]` Mount attaches a local file through AMI's IUSB media redirection — `.iso` as the server's virtual CD-ROM, `.img`/`.ima`/`.vfd` as a floppy (up to 2.88 MB) or a USB disk, served read-only with 512-byte blocks and write protection reported to the host —, the protocol the Java JViewer console uses (`internal/iusb`); no Java needed. Fyrtaarn logs in to the BMC's web interface with the same credentials (`internal/amiweb`), takes the media token from the JViewer launch file, connects to the CD port (5120) and answers the server's SCSI reads from the file on demand, so there is no size limit; the image stays attached until Eject or exit. Upload to BMC (LMedia) stores an image of up to 16 MiB on the BMC itself instead, behind a warning dialog: it switches the BMC's Local Media setting on and blocks the BMC's other requests while uploading. The 16 MiB limit is not reported by any BMC call; it is hard-coded in the BMC's own upload page
- **Web UI proxy** — `[W]` from the results list or BMC Info serves the selected BMC's HTTPS web interface on `http://127.0.0.1:<port>/`, so any modern browser can use it, however old the BMC's TLS. No old OpenSSL, VM or container is needed: fyrtaarn makes the BMC connection itself (see Legacy TLS below). The dialog shows the local URL, the TLS version and cipher negotiated with the BMC (with a warning when it is obsolete), and the `ssh -L` line to use when fyrtaarn runs on another machine. Absolute redirects and links back to the BMC are rewritten to the local address; cookies lose `Secure`/`Domain` so the browser keeps the login session over local HTTP. Listens on localhost only; one proxy per BMC, running until Stop Proxy or exit. Java KVM consoles connect to their own ports directly and are not proxied
- **Legacy TLS** — every HTTPS connection fyrtaarn makes to a BMC (Redfish, scan enrichment, web UI proxy) accepts TLS 1.0 and 1.1 and the old CBC, 3DES and RC4-SHA suites through Go's TLS library, and falls back to a built-in TLS 1.0/1.1 RC4-MD5/RC4-SHA client for BMCs Go cannot reach at all, such as 2009-era AMI MegaRAC SP firmware whose only usable suite is RC4-MD5. Modern BMCs still negotiate TLS 1.3/1.2 as before. Certificates are not verified, as before (BMCs use self-signed certificates)
- **IPMI over LAN on/off** — when a connect fails with no IPMI session and the BMC has Redfish, fyrtaarn checks the BMC's Redfish `NetworkProtocol` resource; if IPMI over LAN is switched off it says so and offers to switch it on (default Cancel), then retries the connect. BMC > IPMI over LAN... shows the selected host's current state and switches it either way. The change is a Redfish `PATCH` of `IPMI.ProtocolEnabled`, read back afterwards: a BMC that accepts it but keeps the old value is reported as a failure. The dialog notes IPMI 2.0's password-hash exposure (CVE-2013-4786)
- **Reset BMC (cold)** — BMC > Reset BMC (cold)... restarts the management controller only, over IPMI (`mc reset cold`) or, in Redfish-only mode, the standard Redfish `Manager.Reset` (never vendor actions such as HPE's ResetToFactoryDefaults beside it). The dialog states that the server keeps running, settings are kept, the BMC is unreachable for one to three minutes and open sessions to it are cut, and the BMC's address must be typed to confirm; Enter in the address box moves to Cancel, not Reset. fyrtaarn closes its own virtual drive and web UI proxy to that BMC first. Recovers a BMC whose web server has stalled, as the iLO 4 under Known Issues did
- **SOL settings** — BMC > SOL settings... shows whether the BMC offers SOL, its bit rate and required privilege, and whether the logged-in user may use SOL, and offers to switch SOL on or off and to allow the user (Cancel focused). Uses channel 14 ("this channel"), so no per-vendor LAN channel number. The server must still redirect its console to the BMC's serial port in its own BIOS/UEFI setup, which differs by vendor; the dialog says so rather than guessing. Reading checked on the Quanta; the changes are untested on hardware
- **Redfish-only mode** — for a BMC with IPMI over LAN off, or none: BMC > Redfish view... (or Use Redfish only in the IPMI over LAN dialog, so IPMI can stay off) shows the system and manager details over Redfish, and from there `[S]` reads sensors from the Chassis Thermal and Power resources (temperatures, fans, voltages, power draw; absent parts skipped), `[P]` offers power actions from the BMC's own list of allowed reset types (never NMI; Cancel focused), `[L]` the event log (every Systems and Managers log service, first page each, newest first), `[U]` user accounts (shown read-only: their IDs are not IPMI user IDs), `[F]` the firmware inventory, `[V]` virtual media and `[W]` the web UI. Checked against captured Redfish responses of an iDRAC9 4.20, an iLO 4 2.54 (whose fans use `FanName`/`CurrentReading`/`Units` instead of the standard names) and an iLO 5 1.40; the power actions are untested on hardware
- **File browser for images** — Browse... in the Virtual Media dialog opens a full-screen browser to pick a local image instead of typing its path. It shows folders and images (`.iso`, `.img`, `.ima`, `.vfd`; `[A]` for all files, `[.]` for hidden ones), starts from the folder of a path already typed or else the home folder, and fills the chosen path into the dialog with Mount focused. Keyboard: arrows or `j`/`k`, PgUp/PgDn, Home/End, Enter to open or choose, Backspace or Left for the parent, `~` for home, Esc to go back. Mouse: click selects, clicking the selected entry opens or chooses it (terminals report no double-click), the wheel scrolls. Paths use the platform's own separators on Linux, macOS and Windows; on Windows, going up from a drive's root lists the drives. Folder links are followed; unreadable folders show their error without leaving the current one
- **Direct connect** — File > Connect to BMC opens a dialog (IP, username, password) and connects without running a scan; host is added to the results tree so all subsequent operations work normally, and gets the same reverse-DNS and Redfish enrichment a scan gives it
- **Recent Connections** — File > Recent Connections lists the last 5 hosts connected to, newest first, one entry per IP; selecting one reconnects straight away if this session already holds its credentials, otherwise opens Connect to BMC pre-filled with IP and username. Saved to `config.json` (IP and username only)
- **Favourites** — up to 15 pinned hosts under File > Favourites, each with an optional name (shown as `Name — IP (user)`); File > Favourites > Manage Favourites... adds (`[A]`), edits (`[Enter]`) and deletes (`[D]`) them. The form's password box is session-only: it goes into the in-memory credential cache so the favourite connects without prompting until you quit, and is never written to disk. Saved to `config.json` (name, IP and username only)
- **Session log** — File > Start Log… prompts for a file path and begins logging all activity; SOL output is written as rendered plain text (lines are flushed to the log as they scroll off the top of the VT100 screen buffer, final screen state captured on disconnect); scan results, BMC connections, and SOL start/end events are logged with timestamps; File > Stop Log closes the file

### Architecture

- Per-vendor virtual media via `redfish.VirtualMediaProvider`, selected by manufacturer in `redfish.NewProvider`; everything else is vendor-neutral through ipmitool
- One IPMI session per BMC at a time — mc info, lan print and chassis status are fetched sequentially, and every ipmitool call takes a per-host lock; some BMCs (iDRAC9) refuse overlapping session set-ups with "insufficient resources for session"
- Per-command timeouts — 15 s for single-round-trip commands; 90 s for bulk SDR/SEL reads
- Passwords never on a command line — ipmitool gets the session password via its environment (`-E` with `IPMITOOL_PASSWORD`), so it does not show up in `ps`; new BMC user passwords are typed into ipmitool's own prompts through a pseudo-terminal — a pty on Unix, ConPTY on Windows 10 1809 / Server 2019 and later (only older Windows still passes the new password as an argument)
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

- Per-vendor backends — current implementation uses a generic Redfish walker
  that handles iDRAC 9, HP iLO 4 (via its `Oem.Hp.Actions`), iLO 5 (standard
  actions) and Supermicro MegaRAC; checked on 2026-11-23 against captured
  Redfish responses of an iDRAC9 4.20, an iLO 4 2.54 and an iLO 5 1.40, where
  it picks the CD slot's Insert/Eject actions on each; XCC is untested; vendors
  without Redfish VirtualMedia need their own backend, and each waits for a
  BMC to test it on:
  - `ilo.go` — iLO 3 via RIBCL (no Redfish); iLO 5/6 session-token auth
    (iLO 4 needs no backend — the generic walker handles it)
  - `drac.go` — iDRAC 7/8 via WSMAN; iDRAC 9 Redfish + Dell OEM RemoteImage extension
  - `supermicro.go` — AMI MegaRAC Redfish where available; detect IKVM-only firmware
    (implemented in 0.6.3 — delegates to the generic walker after a VirtualMedia probe)
  - `lom.go` — Oracle ILOM 3.x (proprietary REST) and 4.x (Redfish where supported)
  - `xcc.go` — Lenovo XCC Redfish with session management
  - `cimc.go` — Cisco CIMC XML API 2.0 (no Redfish VirtualMedia surface)
- Generic Redfish fallback: replace path-sniffing slot discovery with `MediaTypes`
  array inspection so numeric and non-descriptive slot paths are found correctly
  (done — the generic walker accepts a slot by path or by `MediaTypes`)
- Floppy / USB image support (done: Redfish USB/floppy slot, and AMI MegaRAC SP floppy and hard-disk redirection)
- Remote media redirection
- File browser in the Virtual Media dialog (done: see BMC Management)
- AMI MegaRAC SP (pre-Redfish): CD-ROM, floppy and hard-disk redirection are done (see BMC
  Management), including secure mode (`-vmsecure 1`: TLS around the same stream, as JViewer's
  `PacketMaster.connectVmediaSSl` does); secure mode tested on the Quanta
- Surface BMC-reachability requirement to user (ISO URL must be accessible by the
  BMC, not the client) (done — the mount dialog says so; local files are
  served to the BMC automatically since 0.6.3)

### Providers

What each vendor gets today, and how far it has been checked. "Capture" means checked against a
read-only `scripts/fy-capture.sh` capture of that BMC, "hardware" means used on the BMC itself
(see Tested Hardware).

| Vendor / BMC | IPMI (info, sensors, SOL, power, users) | Virtual media | Other |
|---|---|---|---|
| Dell iDRAC9 | Hardware (FW 4.20) | Generic Redfish walker, CD and USB slots: capture | Redfish-only mode sensors: capture |
| Dell iDRAC 7/8 | Untested | Not built (needs WS-Man); no hardware to test on | |
| HPE iLO 4 | Hardware (FW 2.54); `-C 3` after its cipher-list failure | Redfish `Oem.Hp` actions: hardware for CD (earlier), capture for USB | |
| HPE iLO 5 | Connect after switching IPMI over LAN on: hardware (FW 1.40) | Standard Redfish actions: capture | IPMI over LAN switch: hardware |
| HPE iLO 3 / 6 | Untested | iLO 3 needs RIBCL, not built; iLO 6 untested | |
| Quanta, AMI MegaRAC SP (no Redfish) | Hardware (FW 7.54); `-C 3` after its suite refusal | AMI IUSB redirection, CD / floppy / USB disk, plain or TLS (secure mode): sessions accepted on hardware, no sector reads yet (server off); LMedia fails on this firmware | Web UI proxy over RC4-MD5: hardware |
| Supermicro (own firmware) | Untested | Redfish VirtualMedia probe, then the generic walker; untested | |
| Oracle ILOM, Lenovo XCC, Cisco CIMC | Untested | Generic Redfish walker may work; no dedicated backend; untested | |

The files `internal/redfish/{ilo,drac,lom,xcc,cimc}.go` are placeholders for vendor backends that
need hardware to build and test against.

---

## Tested Hardware

Confirmed on real hardware — connect (BMC Info), SOL console, and SOL close/re-open with `[F10]` then `[O]`:

| Vendor     | Controller | Firmware      | Release | Date       |
|------------|------------|---------------|---------|------------|
| Dell       | iDRAC9 (PowerEdge T140) | 4.20.20.20 | 0.6.5 | 2026-10-28 |
| HP         | iLO 4 (ProLiant ML310e Gen8 v2) | 2.54 | 0.6.5 | 2026-10-28 |
| Quanta     | AMI MegaRAC SP (IPMI manufacturer ID 7244), sold as a Supermicro BMC | 7.54 | 0.6.5 | 2026-10-28 |

Earlier, separately: Redfish enumeration and virtual media on HP iLO 4 (ProLiant ML310e Gen8 v2, FW 2.54) — see `internal/redfish/ilo.go`.

The Quanta's web interface speaks TLS 1.0 at most, with NULL-MD5, RC4-MD5 and two 40-bit export suites only, and it has no Redfish. Checked from the development machine on 2026-11-23 with the code released as 0.8.0 (web UI proxy from 0.7.0):

- Web UI proxy: login and authenticated pages work (built-in RC4-MD5 client).
- Virtual media: Mount attaches an ISO through AMI media redirection (session accepted on port 5120, held, and released by Eject); a floppy image on port 5122 and a disk image on 5123 likewise, and a CD over TLS on 5124 with media encryption switched on (then switched back off). The server was powered off, so no sector reads have been observed yet.
- LMedia (Upload to BMC) fails, and the BMC's own Java console launch file can crash its web server: see [Known Issues](#known-issues).

HPE iLO 5 (ProLiant ML350 Gen10, FW 1.40), release 0.7.0, 2026-11-23: IPMI over LAN was switched off, so Redfish answered and IPMI never did. BMC > IPMI over LAN... switched it on over Redfish; the iLO's own web UI then showed it on, where it had been off, and fyrtaarn connected over IPMI. SOL not yet tried on this BMC.

Not yet tested on hardware: Supermicro's own BMC firmware, Oracle/Sun ILOM, Lenovo XCC, Cisco CIMC, iLO 3 and 6. Reports welcome.

To report a BMC, run `scripts/fy-capture.sh` against it and attach the output:

```bash
scripts/fy-capture.sh <name> <bmc-ip> <user>
tar czf fyrtaarn-fixtures.tgz -C ~ fyrtaarn-fixtures
```

It is read-only — ipmitool read commands and Redfish GETs, nothing that changes BMC state — and runs one command at a time. It captures the IPMI output fyrtaarn parses, plus a Redfish walk of Systems, Managers (including VirtualMedia slots), Chassis sensors and SessionService, with timings, into `~/fyrtaarn-fixtures/<name>/`. The password is prompted for without echo and never written to disk or passed on a command line. The output does contain serial numbers, MAC and IP addresses, UUIDs and BMC user names: review or redact it before sharing publicly. Needs `ipmitool`, `curl` and `python3`.

---

## Known Issues

BMCs are embedded systems with long-lived firmware, and each has its own bugs. This section lists
the ones fyrtaarn has run into on real hardware, what they look like from the outside, and what
fyrtaarn does about each. Everything here was seen on a specific BMC and firmware version, and
says so; where fyrtaarn applies a fix more widely than that, the entry says why.

In fyrtaarn itself, BMC Info and the Redfish view show a **Known issues** line for any BMC with an
entry below, and `[K]` lists them. The list lives in one place in the code, `internal/quirks`,
and the table at the end of this section is generated from it.

### How fyrtaarn recognises a BMC

Fyrtaarn identifies a BMC from what it already learns while connecting, so no extra requests are
made just to check for known issues:

- IPMI `mc info`: the manufacturer ID, product ID and firmware revision (for example, HP iLO 4 is
  manufacturer 11, product 0x2000; HPE iLO 5 is manufacturer 47196, product 0x2000).
- The Redfish manager's firmware string, such as `iLO 4 v2.54`.
- The web interface recognised during a scan, such as `AMI MegaRAC SP`.
- The firmware build recognised from the web front page's ETag during a scan, before any login:
  see [Firmware fingerprints](#firmware-fingerprints).

Some issues can only be discovered by running into them, such as a BMC that refuses ipmitool's
choice of cipher suite. Fyrtaarn detects those as they happen, works around them, and records them
for that BMC for the rest of the session; `[K]` marks them "seen this session".

### Firmware fingerprints

A web server's `ETag` header identifies a version of a file, and many BMCs derive it from the
firmware image rather than from each file. On both HP iLOs seen so far, every static file of the
web interface carries one and the same ETag, and it differs between firmware builds. So the ETag
of the front page alone identifies the firmware, without logging in.

Fyrtaarn reads the front page's ETag during a scan (the same single request that also names the
web interface) and, if it is a known one, treats the BMC as that firmware from then on: its known
issues apply before the first login, so an iLO 4 2.54 gets `-C 3` on its very first IPMI command
instead of after a ten-second delay. The scan's host details show the result as "firmware: ...
(web fingerprint)".

Dell's iDRAC9 works differently: its web server is Apache, which gives every file its own ETag
in the form `"<size>-<modified>"`, both in hexadecimal. The size part differs per file, but the
modified part is the same on every file of a firmware build, because they all carry the time the
image was built. Fyrtaarn matches on that part. On the iDRAC9 below it is `5a50953d044c0`, which
is 7 May 2020 06:52:43 UTC in microseconds, the same as every file's `Last-Modified` header; the
size parts were checked against the files' actual sizes.

An ETag only identifies a build that has been seen before, and different products could in
principle share one, so each entry below records where it was taken.

AMI MegaRAC SP firmware on the GoAhead web server, as on the Quanta, sends no ETag at all, and its
clock was never set, so its dates mean nothing either. It does serve every web file
pre-compressed, and a gzip file's header (RFC 1952) records when it was compressed and the name of
the file it came from. On the Quanta every file says it was compressed at 3 September 2013
06:37:43 UTC from a path in AMI's build tree,
`/AMI_AST2300_2011Q3_tmp/branches/s2rq_s2rs/S2Rx_workspace/Build/output/ImageTree/usr/local/www/index.html`
for the front page: the chip (AST2300), AMI's 2011 Q3 code base and Quanta's board branch
(S2RQ/S2RS). Fyrtaarn reads that header from the front page a scan already fetches, so it costs no
extra request, and matches on the time together with the start of the path.

| ETag | Firmware | Seen |
|---|---|---|
| `"00ce4159"` | HP iLO 4, firmware 2.54 | ProLiant ML310e Gen8 v2, 23 November 2026: the same ETag on `/`, `/favicon.ico` and `/css/eov_lteIE9.css` |
| `5a50953d044c0` (after the dash) | Dell iDRAC9, firmware 4.20.20.20 | PowerEdge T140, 23 November 2026: on every file, e.g. `"99a-5a50953d044c0"` on the 2458-byte `/restgui/start.html` that `/` redirects to, which is what a scan reads |
| gzip header: 2013-09-03 06:37:43 UTC, `/AMI_AST2300_2011Q3_tmp/branches/s2rq_s2rs/` | Quanta S2RQ/S2RS, AMI MegaRAC SP, firmware 7.54 | Quanta in Tested Hardware, 23 November 2026: the same time and build tree on `/index.html` and five other files |
| `"00d2585c"` | HPE iLO 5, firmware 1.40 | ProLiant ML350 Gen10, 23 November 2026: the same ETag on `/`, `/favicon.ico` and `/images/app-icon.svg` |

To add a BMC: fetch its front page and look at the `ETag` response header, for example with
`curl -skI https://<bmc-address>/` (or `curl -sk -D - -o /dev/null` for BMCs that refuse HEAD, as
the iLO 5 does), together with its firmware version from BMC Info.

### HP iLO 4 (ProLiant Gen8; seen on firmware 2.54)

**Redfish event log stalls the web server** (`redfish-log-entries-stall`)

- *What you see:* the iLO's web interface stops loading and Redfish stops answering. IPMI, and the
  server itself, carry on as normal.
- *Cause:* requesting the Integrated Management Log (IML) entries over Redfish. The first such
  request was never answered, and the iLO's web server then answered nothing at all, web pages
  included, for at least 30 minutes. It did not recover on its own.
- *What fyrtaarn does:* never requests Redfish log entries from an iLO 4, on any firmware. The
  event log is read over IPMI instead (`[L]` on BMC Info), which is unaffected. This is applied to
  all iLO 4 firmware, not only 2.54, because the cost of being wrong is a stalled iLO on a
  production server.
- *What you can do:* if an iLO 4's web interface has stalled, a cold reset of the iLO brings it
  back without touching the server: see [Recovering a stalled BMC](#recovering-a-stalled-bmc).
- *Evidence:* ProLiant ML310e Gen8 v2, iLO 4 2.54, on 23 November 2026, a production server.
  Stalled at about 16:00 during a capture, still stalled at about 16:30, recovered by `mc reset cold`.

**Every IPMI command takes ten seconds** (`no-cipher-suite-list`)

- *What you see:* each IPMI action against the iLO 4 takes about ten seconds, where other BMCs
  answer in under one.
- *Cause:* ipmitool 1.8.19 and later first asks the BMC which cipher suites it supports. The iLO 4
  never answers that question, and ipmitool waits about ten seconds before falling back to suite 3,
  on every command. Older ipmitool versions do not ask, so they are not affected.
- *What fyrtaarn does:* after the first such delay, passes `-C 3` to ipmitool for that BMC, which is
  the suite ipmitool would have ended up on anyway. Once the BMC is recognised as an iLO 4 2.54,
  `-C 3` is used from the first command.
- *What you can do:* nothing. If you run ipmitool by hand against an iLO 4, add `-C 3`.
- *Evidence:* captures of the ML310e above, 23 November 2026: every command took 10.2–10.8 seconds
  without `-C 3`.

### HPE iLO 5 (ProLiant Gen10; seen on firmware 1.40)

**IPMI over LAN was switched off** (`ipmi-over-lan-was-off`)

- *What you see:* connecting fails with "Unable to establish IPMI v2 / RMCP+ session" after about
  twenty seconds, although the iLO's web interface and Redfish work.
- *Cause:* the iLO's "IPMI/DCMI over LAN" setting was off, so the iLO ignores IPMI entirely. Whether
  this is HPE's factory default is not established.
- *What fyrtaarn does:* when a connect fails this way and the BMC has Redfish, fyrtaarn asks it over
  Redfish whether IPMI over LAN is off, and if so offers to switch it on, with a note about IPMI's
  password-hash weakness (CVE-2013-4786). BMC > IPMI over LAN... checks or switches it at any time.
  If you would rather leave IPMI off, BMC > Redfish view... manages the BMC over Redfish alone.
- *Evidence:* ProLiant ML350 Gen10, iLO 5 1.40, 23 November 2026; switched on through fyrtaarn, and
  the change confirmed in the iLO's own web interface.

**Redfish accounts have no "enabled" field** (`accounts-no-enabled-field`)

- *What you see:* nothing now. Before 0.8.1, every account in the Redfish view showed as disabled.
- *Cause:* iLO 5 leaves out the standard `Enabled` and `Locked` fields of a Redfish account. Whether
  an account may log in is in HPE's own `Oem.Hpe.Privileges.LoginPriv` instead.
- *What fyrtaarn does:* uses `LoginPriv` when `Enabled` is missing.
- *Evidence:* capture of the ML350 Gen10 above, 23 November 2026.

### Quanta server with AMI MegaRAC SP (no Redfish; seen on firmware 7.54)

This BMC is sold as a Supermicro BMC; IPMI reports manufacturer 7244 (Quanta), product 0x3252.
Its web interface dates from 2009 and its firmware build from 2013.

**Asking for the Java console the wrong way crashes the web server** (`jnlp-needs-extrnip`)

- *What you see:* the web interface goes dead and stays dead; IPMI keeps working.
- *Cause:* the Java console's launch file, `/Java/jviewer.jnlp`, is generated on request. The BMC's
  own web page asks for it with an `EXTRNIP` parameter giving the BMC's address. Asked for without
  it, the firmware builds the file around a missing address, writes `(null)` in its place, and its
  web server crashes partway through the reply. It stays down until the BMC is reset.
- *What fyrtaarn does:* always includes `EXTRNIP` when it asks for the file, which it does to start
  virtual media. The web UI proxy rewrites `EXTRNIP` to the BMC's real address, so the Java console
  works through the proxy as well.
- *What you can do:* if the web interface has died, see [Recovering a stalled BMC](#recovering-a-stalled-bmc).
- *Evidence:* the Quanta under Tested Hardware, 23 November 2026, reproduced with plain curl from
  Ubuntu 14.04, so it is the firmware, not fyrtaarn.

**Refuses ipmitool's own cipher-suite choice** (`refuses-auto-cipher-suite`)

- *What you see:* with ipmitool 1.8.19 or later, every IPMI command fails with "no matching cipher
  suite"; with ipmitool 1.8.18 everything works.
- *Cause:* ipmitool 1.8.19 asks the BMC for its supported suites and picks the strongest it knows,
  suite 17 (HMAC-SHA256 with AES). This BMC advertises suite 17 in its own list, then refuses it
  when a session is opened with it. Suite 3 works.
- *What fyrtaarn does:* on that error, retries once with `-C 3` and keeps the BMC on it. Once the
  BMC is recognised, `-C 3` is used from the first command.
- *What you can do:* nothing. By hand, add `-C 3`.
- *Evidence:* the Quanta, 23 November 2026, with ipmitool 1.8.19.

**Uploading an image to the BMC does not work** (`lmedia-register-fails`)

- *What you see:* Upload to BMC (LMedia) reports that the BMC would not register the image.
- *Cause:* the upload itself is accepted, but the BMC answers status -1 when asked to register the
  uploaded file. This happened with a formatted floppy image and a valid ISO alike, and identically
  when the browser's own requests were replayed with curl, so it is the firmware.
- *What fyrtaarn does:* still offers Upload to BMC, with a warning that it fails on this firmware.
- *What you can do:* use Mount instead, which presents the image from your machine over the
  network, has no size limit and works on this BMC.
- *Evidence:* the Quanta, 23 November 2026.

**Only obsolete TLS** (`legacy-tls-only`)

- *What you see:* nothing in fyrtaarn. Current browsers, curl and OpenSSL 1.1 or later cannot open
  the BMC's web interface at all.
- *Cause:* the web server offers TLS 1.0 at most, and only the RC4-MD5, NULL-MD5 and two 40-bit
  export cipher suites, none of which modern software will use.
- *What fyrtaarn does:* its own small TLS client speaks RC4-MD5, so Redfish probes, AMI virtual
  media set-up and the web UI proxy all work. The proxy serves the web interface to your browser on
  your own machine, and its dialog shows the TLS in use with a warning that it is not secure. This
  issue is detected when it happens, on any BMC, rather than by model.
- *Evidence:* the Quanta, 23 November 2026; confirmed with Ubuntu 14.04's OpenSSL 1.0.1f.

### Dell iDRAC9 (PowerEdge 14th generation; seen on firmware 4.20)

**Refuses overlapping IPMI sessions** (`no-concurrent-sessions`)

- *What you see:* nothing now. Before 0.6.5, connecting sometimes failed, leaving the host listed as
  "vendor: Direct".
- *Cause:* the iDRAC refuses a second IPMI session while one is being set up ("insufficient
  resources for session"), and fyrtaarn used to fetch BMC Info with three ipmitool commands at
  once; when the first was refused, the connect failed.
- *What fyrtaarn does:* runs one IPMI command at a time per BMC, for every vendor.
- *Evidence:* the iDRAC9 under Tested Hardware, October 2026.

### Recovering a stalled BMC

A BMC whose web interface or Redfish has stopped answering, while IPMI still works, can usually be
brought back with a **cold reset of the BMC**. This restarts the management controller only: the
server keeps running, its operating system and power state are untouched, and the BMC's settings,
users and logs are kept. The BMC is unreachable for one to three minutes while it restarts, and
any open console, virtual media or web session to it is cut.

In fyrtaarn: select the host, then **BMC > Reset BMC (cold)...**. The dialog explains the above and
asks you to type the BMC's address before it does anything. By hand:

```bash
ipmitool -I lanplus -H <bmc-address> -U <user> -E -C 3 mc reset cold
```

(`-E` reads the password from `IPMITOOL_PASSWORD`, or prompts for it.) This is what recovered both
the iLO 4 and the Quanta above.

### At a glance

| Quirk | BMC | What goes wrong | What fyrtaarn does | Seen |
|---|---|---|---|---|
| `no-cipher-suite-list` | HP iLO 4 (FW 2.54) | Does not answer Get Channel Cipher Suites; ipmitool 1.8.19+ waits about 10 s on every command before falling back to suite 3 | -C 3 for this host after the first such failure (or straight away once identified) | ProLiant ML310e Gen8 v2 captures, 2026-11-23 |
| `refuses-auto-cipher-suite` | Quanta, AMI MegaRAC SP (FW 7.54) | Lists cipher suite 17, which ipmitool 1.8.19+ then picks, but refuses it ("no matching cipher suite"); -C 3 works | Retry once with -C 3 and keep the host on it (or -C 3 straight away once identified) | Quanta in Tested Hardware, 2026-11-23 |
| `redfish-log-entries-stall` | HP iLO 4 (seen on FW 2.54; applied to all iLO 4 as a precaution) | Requesting the IML log entries over Redfish stalled its web server: no answer to Redfish or its web pages for at least 30 minutes, until an iLO reset (mc reset cold) | Redfish event log entries are not requested; the IPMI event log is used instead | ProLiant ML310e Gen8 v2, 2026-11-23 (production box): stalled about 16:00, still stalled about 16:30, reset by IPMI |
| `jnlp-needs-extrnip` | Quanta, AMI MegaRAC SP (FW 7.54) | Requesting the JViewer launch file without EXTRNIP crashes its web server until a BMC reset | EXTRNIP is always sent (amiweb), and the web UI proxy rewrites it to the BMC's address | Quanta in Tested Hardware, 2026-11-23, reproduced with curl |
| `lmedia-register-fails` | Quanta, AMI MegaRAC SP (FW 7.54) | LMedia uploads are accepted but registering the image returns status -1, for any image | Upload to BMC still offered, with a warning that it fails on this firmware | Quanta in Tested Hardware, 2026-11-23, identical with the browser's requests replayed |
| `legacy-tls-only` | Hosts whose HTTPS only works with the legacy TLS client (seen: Quanta, TLS 1.0 RC4-MD5) (detected at run time) | No TLS that Go or current browsers accept | legacytls falls back to its own TLS 1.0/1.1 RC4 client; the web UI proxy shows the negotiated TLS | Quanta in Tested Hardware, 2026-11-23 |
| `no-concurrent-sessions` | Dell iDRAC9 (FW 4.20) | Refuses overlapping IPMI session set-ups ("insufficient resources for session") | One IPMI command at a time per BMC, for every vendor | iDRAC9 in Tested Hardware, 2026-10 |
| `ipmi-over-lan-was-off` | HPE iLO 5 (FW 1.40) | IPMI over LAN was off on the one seen, so IPMI never answered while Redfish did | A failed connect checks it over Redfish and offers to switch it on; BMC > IPMI over LAN... | ProLiant ML350 Gen10 capture, 2026-11-23 |
| `accounts-no-enabled-field` | HPE iLO 5 (FW 1.40) | Redfish accounts carry no Enabled or Locked field | Oem.Hpe.Privileges.LoginPriv is used for enabled | ProLiant ML350 Gen10 capture, 2026-11-23 |

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
# Native build (current platform)
make build
# Output: dist/fyrtaarn

# All cross-compilation targets in one shot
make cross
```

### Cross-compilation targets

| Target | Binary | Notes |
|--------|--------|-------|
| Linux x86-64 | `fyrtaarn-linux-amd64` | |
| Linux ARM64 | `fyrtaarn-linux-arm64` | Raspberry Pi 4+, Ampere, AWS Graviton |
| Linux ARMv7 | `fyrtaarn-linux-armv7` | Orange Pi, Raspberry Pi 2/3, most 32-bit ARM SBCs |
| Linux PowerPC64 BE | `fyrtaarn-linux-ppc64` | G3/G4/G5 era PowerPC hardware |
| Linux mipsel (32-bit LE) | `fyrtaarn-linux-mipsel` | softfloat; embedded routers, no FPU required |
| Linux mips64el (64-bit LE) | `fyrtaarn-linux-mips64el` | softfloat |
| macOS x86-64 | `fyrtaarn-darwin-amd64` | Intel Mac |
| macOS ARM64 | `fyrtaarn-darwin-arm64` | Apple Silicon (M1 / M2 / M3 / M4) |
| Windows x64 | `fyrtaarn-windows-amd64.exe` | |
| Windows ARM64 | `fyrtaarn-windows-arm64.exe` | Surface Pro X, Qualcomm Snapdragon laptops |

All targets set `CGO_ENABLED=0` for fully static binaries with no system library dependencies. Version, commit, and build date are injected via ldflags from the current git state.

Pre-built binaries for all targets are attached to each [GitHub Release](../../releases).

### Releasing

Two paths to cut a release:

**Button click (recommended)** — go to Actions → **Publish Release** → Run workflow; enter the version (e.g. `v0.6.1`) and optionally tick pre-release. GitHub builds all targets, creates the tag, generates `SHA256SUMS.txt`, and publishes the release automatically.

**Command line** — push a version tag and `release.yml` fires:

```bash
git tag v0.6.1
git push origin v0.6.1
```

Both paths produce identical output: 10 binaries + `SHA256SUMS.txt` attached to the release, release notes auto-generated from commits since the previous tag.

---

## Run

```bash
make run

# Or with elevated privileges for full scan capability
sudo ./dist/fyrtaarn
```

---

## Key Bindings

| Key                    | Context           | Action                                        |
|------------------------|-------------------|-----------------------------------------------|
| F9                     | Global (non-SOL)  | Open / close menu bar                         |
| F2                     | Global (non-SOL)  | Cycle to next theme                           |
| Ctrl+C                 | Global (non-SOL)  | Quit                                          |
| ↑ / k                  | Results, lists    | Move selection up                             |
| ↓ / j                  | Results, lists    | Move selection down                           |
| Tab                    | Results           | Expand / collapse host detail                 |
| Enter                  | Results           | Connect (or reconnect)                        |
| W                      | Results, BMC Info | Web UI via local proxy                        |
| S                      | BMC Info          | Load sensor list (SDR)                        |
| Space                  | Sensors           | Watch / unwatch sensor on the dashboard       |
| D                      | Sensors           | Open the sensor dashboard                     |
| + / - / R              | Dashboard         | Refresh slower / faster / now                 |
| L                      | BMC Info          | Load event log (SEL)                          |
| F                      | BMC Info          | Load FRU / hardware inventory                 |
| U                      | BMC Info          | Load user account list                        |
| C                      | BMC Info          | Run firmware compliance check                 |
| R                      | BMC Info          | Redfish full enumeration                      |
| P                      | BMC Info          | Power control dialog                          |
| O                      | BMC Info          | Live chassis check; opens SOL or prompts to power on if off |
| F10                    | SOL Console       | Disconnect and return                         |
| PgUp                   | SOL Console       | Scroll up through history                     |
| PgDn                   | SOL Console       | Scroll down / snap to live bottom             |
| All other keys / F1–F12| SOL Console       | Forwarded to BMC (incl. F2, F9)               |
| V                      | BMC Info          | Virtual media dialog (Redfish)                |
| Browse... (in V)       | Virtual media     | Pick a local image in a file browser          |
| Enter                  | User list         | Open action dialog for user                   |
| N                      | User list         | Create new user                               |
| F9 > Connect to BMC    | Global            | Connect directly without scanning             |
| F9 > BMC > IPMI over LAN | Results         | Check or switch IPMI over LAN (Redfish)       |
| F9 > BMC > Redfish view | Results          | Manage the host over Redfish only             |
| F9 > BMC > SOL settings | Results          | Check or switch SOL and your SOL access       |
| F9 > BMC > Reset BMC   | Results           | Cold reset the BMC only (typed confirmation)  |
| S / L / U / F          | Redfish view      | Sensors / event log / users / firmware        |
| P / V / W              | Redfish view      | Power / virtual media / web UI                |
| F9 > Export            | Results           | Export inventory to CSV / JSON                |
| F9 > Start Log         | Global            | Begin session log to file                     |
| F9 > Stop Log          | Global            | End session log                               |
| Esc / Q                | Most screens      | Back / quit                                   |

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
- sensor dashboards
- cluster / fleet operations

---

## Changelog

### 0.8.2

- **AMI firmware fingerprint** — a scan now also recognises the Quanta's AMI MegaRAC SP 7.54 before login, from the build time and build-tree path in the gzip header of its pre-compressed front page (its GoAhead server sends no ETag). Checked by a live scan of the Quanta
- **Long dialogs scroll** — a dialog whose text is taller than the terminal, such as Known Issues on a small screen, shows a window of it with its position (`lines 1–12 of 30`); PgUp/PgDn scroll it, as do Home/End and the arrows when the dialog has only a Close button. Checked in a 24-row terminal on the Quanta's Known Issues
- **File browser for images** — Browse... in the Virtual Media dialog picks a local image with keyboard or mouse, on Linux, macOS and Windows (drive list on Windows), and fills its path in. Checked in a real terminal against the Quanta: browsed, chosen with two mouse clicks, mounted and ejected

### 0.8.1

- **Firmware fingerprints** — a scan reads each BMC's front-page ETag and recognises known firmware builds before any login, so their known issues apply from the first request (an iLO 4 2.54 gets `-C 3` on its first IPMI command). Known so far: iLO 4 2.54 and iLO 5 1.40, each serving one ETag on every static file, and iDRAC9 4.20.20.20, matched on the build-time part of Apache's per-file ETags. Listed under Known Issues, kept in step with the code by a test
- **README: Known Issues** — a new section, by BMC, describing each known issue in full: what you see, the cause as far as it is known, what fyrtaarn does, what you can do, and the evidence, plus how to recover a stalled BMC with a cold reset. Writing it corrected one earlier claim: the Quanta lists cipher suite 17 among its suites and then refuses it, rather than leaving it out of its list
- **Reset BMC (cold)** — new BMC menu item to restart a BMC's management controller only, over IPMI or Redfish `Manager.Reset`, behind a dialog that spells out what is and is not affected and needs the BMC's address typed to confirm
- **Known issues registry** — BMC-specific problems now live in one place, `internal/quirks`, instead of in the code that hits them: a BMC's identity (IPMI manufacturer and product ID and firmware, Redfish manager firmware, web UI seen at scan time) is recorded as fyrtaarn learns it, and code asks the registry (e.g. "skip Redfish log entries on this host?"). Problems found at run time (a refused cipher suite, TLS that needs the legacy client) are recorded per host too. BMC Info and the Redfish view show "Known issues: N" and `[K]` lists them with what fyrtaarn does and where each was seen; the README's Known Issues table is kept in step by a test. Checked live on the Quanta: four issues, one of them learned during the session
- **iLO 4: no Redfish event log** — the first request for an iLO 4 2.54's IML entries (ProLiant ML310e Gen8 v2, a capture run on 2026-11-23) went unanswered, and so did every Redfish request after it for minutes: its web server stalled. The Redfish view's `[L]` and `scripts/fy-capture.sh` no longer request log entries from an iLO 4 (recognised by manager firmware "iLO 4 ..."); its IPMI event log, `[L]` on BMC Info, is unaffected. Other BMCs are unchanged: an iLO 5 1.40 served 400 entries in about 3 seconds
- **Users: names with spaces** — the user list was split on whitespace, so a name with a space shifted every later column; on an iLO 5 (FW 1.40), whose unused slots are named "(Empty User)", eleven users showed privilege "true NO ACCESS". Names are now read as everything between the ID and the three true/false columns
- **Redfish accounts on iLO 5** — iLO 5 sends no `Enabled` field, so every account showed as disabled; HPE's `Oem.Hpe.Privileges.LoginPriv` is used where `Enabled` is absent, and a named account with neither counts as enabled
- Both found in a capture of an iLO 5 1.40 (ProLiant ML350 Gen10) with IPMI over LAN switched on, which also confirmed on real data: event log (400 entries from the IML and IEL, newest first), firmware inventory (19 items), Chassis sensors (29), power actions, SEL (256), FRU and SOL info

### 0.8.0

- **Redfish session login** — when a BMC refuses HTTP basic auth (401), fyrtaarn opens a Redfish session (POST to the Sessions collection found from the service root: `Links.Sessions`, or iLO 4's `links.Sessions.href`), uses its `X-Auth-Token`, logs in again if it expires, and logs all sessions out on exit. A session is tied to host, user and password, so a wrong password never rides on an earlier session. BMCs that accept basic auth behave as before. Tested against a fake BMC with basic auth off; untested on hardware
- **SOL settings** — BMC > SOL settings... checks and switches SOL on the BMC (`ipmitool sol set enabled`) and the logged-in user's SOL access (`sol payload`), both on channel 14; BIOS console redirection stays the server's own setting and is explained, not attempted
- **Redfish view: event log, users, firmware** — `[L]`, `[U]` and `[F]` on the Redfish view read the log services (first page of each), AccountService accounts and UpdateService firmware inventory, shown on the event log, Users and FRU screens; Redfish accounts are always read-only there, as their IDs are not IPMI user IDs. Built to the DMTF schemas and tested against a fake BMC; `scripts/fy-capture.sh` now collects these resources (read-only) so real BMCs can be checked
- **Scans name BMCs without Redfish** — when the Redfish probe fails, fyrtaarn fetches the HTTPS front page (old TLS included) and recognises the web UI by its title; the Quanta is now listed as "AMI MegaRAC SP" (vendor and a new "web UI" detail line), for scans and direct connects alike. Checked live on the Quanta: 0.4 s including the failed Redfish probe
- **Sensor dashboard over Redfish** — in Redfish-only mode, `[Space]` and `[D]` on the Sensors screen now open a dashboard refreshed from the Chassis Thermal and Power resources; the chosen sensors are saved separately from the IPMI ones
- **Windows: new BMC user passwords off the command line** — `user set password` on Windows now answers ipmitool's prompts through ConPTY, as Unix does through a pty, instead of passing the password as an argument; only Windows older than 10 1809 keeps the argument. The SOL console and the prompts now share one pseudo-terminal package (`internal/ptyproc`). Untested on Windows
- **AMI media redirection in secure mode** — when the BMC's launch file says `-vmsecure 1`, the media connection is made in TLS (any certificate accepted, as JViewer does) with the same protocol inside, through `legacytls` for these BMCs' old TLS. Tested on the Quanta on 2026-11-23: with media encryption switched on, its launch file moves the drives to ports 5124/5126/5127 and fyrtaarn's CD session over TLS was accepted and ejected cleanly; the setting was then switched back off
- **AMI MegaRAC SP: floppy and USB disk images** — `.img`, `.ima` and `.vfd` images attach as a floppy (up to 2.88 MB, port 5122) or a USB disk (port 5123), read-only. The disk command set, error encoding and the hard-disk "drive type" exchange come from disassembling the BMC's own JViewer native library (`libjavaharddiskwrapper.so`, not stripped); sessions on both ports were accepted by the Quanta
- **Status bar: long messages** — a status wider than the terminal wrapped the bar onto further lines and only its last words showed; it is now cut to one line with an ellipsis, keeping its start
- **Virtual media: USB and floppy images on Redfish BMCs** — `.img`, `.ima` and `.vfd` images now mount in the USB/floppy slot (iDRAC `RemovableDisk`, iLO slot 1) and `.iso` in the CD/DVD slot; Eject ejects whatever is mounted in any slot instead of only the CD. Slot choice checked against captured responses of an iDRAC9 4.20, an iLO 4 2.54 and an iLO 5 1.40; mounting a disk image is untested on hardware
- **Redfish-only mode** — BMC > Redfish view..., or Use Redfish only when a connect finds IPMI over LAN off: system details, sensors (Chassis Thermal/Power), power actions limited to the BMC's allowed reset types, virtual media and web UI, all without IPMI (`internal/redfish/chassis.go`, `power.go`)
- **Dialogs: long button rows wrap** — rows of buttons wider than the screen now wrap onto further rows instead of running off the edge
- **Users: real enabled state** — `ipmitool user list` has no enabled column, so a named user was always shown as enabled; on the Quanta that showed a disabled account as enabled. The Users screen now takes each user's Enable Status from `ipmitool channel getaccess 14` (channel 14 is "this channel" in the IPMI spec, so no per-vendor LAN channel number); BMCs that do not answer it keep the old name-based guess
- **FRU: empty devices explained** — devices ipmitool reports as "Device not present (...)" now show that line under their header instead of an empty section (four such devices on the ML310e's iLO 4)
- **Sensor dashboard** — `[Space]` on the Sensors screen picks sensors, `[D]` shows them live with min, max and a trend graph, refreshed every 2 s to 1 min; the sensor repository is cached once per session so each refresh reads only the chosen sensors (0.3 s against 27 s for a full list on the Quanta)
- **Quanta / old AMI BMCs with ipmitool 1.8.19+** — ipmitool 1.8.19 picks a cipher suite from the BMC's list when no `-C` is given, and the Quanta (FW 7.54) refuses that pick with "no matching cipher suite" although `-C 3` works, so fyrtaarn could not connect to it at all. On that refusal fyrtaarn now retries once with `-C 3` and keeps the host on it
- **SOL console on Windows** — the built-in SOL pane now runs on Windows 10 1809 / Server 2019 and later, with ipmitool in a pseudo-console (ConPTY, via `charmbracelet/x/conpty`). Closing it works as on Unix: ipmitool's escape sequence first, then Ctrl+C (Windows' counterpart of SIGINT, which ipmitool answers with Close Session), then TerminateProcess, followed by `sol deactivate`. Older Windows keeps the dialog showing the ipmitool command to run instead. Needs an `ipmitool.exe` on PATH

### 0.7.1

- **Copy and paste in input boxes and SOL** — fyrtaarn captured the mouse everywhere for menu-bar clicks, so the terminal never got the mouse: no drag-to-select copying, and PuTTY's right-click paste arrived as a click. Mouse capture is now off while a dialog (Connect, passwords, Favourites, Virtual Media, ...) or the SOL console is open, and back on for the menu bar elsewhere. Pasting into input boxes, including passwords, works with every terminal paste (Ctrl+Shift+V, Cmd+V, right-click). With the mouse captured, Shift+drag (PuTTY, Windows Terminal) or Option+drag (iTerm2) still selects. Ctrl+V inside fyrtaarn's input boxes never pasted either: it reads the clipboard in a separate step whose result was dropped before reaching the box; it now arrives. Ctrl+V reads the clipboard of the machine fyrtaarn runs on, so over SSH use the terminal's own paste instead
- **SOL console: `+` and pasted text** — `+` could not be typed (key names containing `+` were taken for combinations like ctrl+x), pasted text arrived wrapped in `[...]`, Insert typed the word "insert", and Alt+key was dropped. Typed and pasted characters are now sent as themselves, pasted line endings as CR, Alt+key as ESC+key

### 0.7.0

- **IPMI over LAN on/off via Redfish** — a failed connect to a Redfish BMC checks whether IPMI over LAN is switched off and offers to switch it on, then reconnects; new BMC menu, IPMI over LAN..., to check or switch it on the selected host (`internal/redfish/networkprotocol.go`). Prompted by an HPE iLO 5 v1.40 (ProLiant ML350 Gen10) that answered Redfish but not IPMI
- **HP iLO 4: no more 10-second wait per IPMI command** — iLO 4 does not answer Get Channel Cipher Suites, which ipmitool 1.8.19+ asks on every connection when no `-C` is given, waiting about 10 s before falling back to cipher suite 3 (`lanplus.c`). After the first such failure fyrtaarn passes `-C 3` to that host, the suite ipmitool would have used anyway. Seen on an ML310e Gen8 v2, iLO 4 FW 2.54: every command took 10.2–10.8 s. Other BMCs keep ipmitool's own choice
- **Virtual media on AMI MegaRAC SP without Redfish** — `[V]` Mount attaches a local ISO as the server's virtual CD through AMI's IUSB media redirection (`internal/iusb`, `internal/amiweb`), with no Java and no size limit; Upload to BMC (LMedia, 16 MiB) behind a warning dialog. Protocol from the BMC's own JViewer.jar, cross-checked against [james-crowley/ansible-collection-asmb8-ikvm](https://github.com/james-crowley/ansible-collection-asmb8-ikvm)'s protocol notes
- **Status messages visible again** — on BMC Info and other screens with a key-hint line, the status bar always showed the hints, so the results and errors of mount, eject, power and similar actions were never displayed. A new status message now replaces the hints for 10 seconds
- **Web UI proxy: AMI Java console** — the `EXTRNIP` parameter of AMI's JViewer launch file is rewritten to the BMC's address, so the console and its media connect to the BMC rather than to 127.0.0.1
- **Web UI proxy** — `[W]` serves a BMC's web interface on a localhost port for any browser, including BMCs whose TLS no current browser accepts (`internal/webproxy`)
- **Legacy TLS for every BMC connection** — Redfish, scan enrichment and the web UI proxy accept TLS 1.0/1.1 and the CBC, 3DES and RC4-SHA suites, and fall back to a built-in RC4-MD5/RC4-SHA client (`internal/legacytls`); key derivation is tested against GnuTLS vectors and the client against Go's own TLS server at 1.0 and 1.1
- **Tested Hardware** — the box listed as "Supermicro BMC" reports itself as a Quanta with AMI MegaRAC SP firmware; corrected
- **Hardware capture script** — `scripts/fy-capture.sh` collects, read-only, everything needed to support or debug a BMC: the IPMI output fyrtaarn parses and a Redfish walk (Systems, Managers and VirtualMedia slots, Chassis sensors, SessionService), with per-call status and timing. The password goes to ipmitool via `-E` and to curl via a config file on a pipe, never argv or disk; commands run sequentially so BMCs that refuse overlapping sessions (iDRAC9) are fine. Documented under Tested Hardware

### 0.6.6

- **Passwords off the command line** — every ipmitool call, SOL included, passed the session password as `-P <password>`, readable by any local user via `ps` or `/proc/<pid>/cmdline` while it ran. It now goes in the child's environment with `-E`. ipmitool's `-E` reads `IPMITOOL_PASSWORD` before `IPMI_PASSWORD`, so fyrtaarn sets the former and strips any inherited copy of either, which would otherwise override it. `user set password` also took the *new* password as an argument; it now runs in a pty and answers ipmitool's two `getpass` prompts, each written only after its prompt appears (glibc's `getpass` flushes pending input before prompting). ipmitool's `exec` file mode was rejected for this: its parser truncates at `#` and rewrites quoted spaces, which would corrupt real passwords. Windows has no pty support, so there the new password is still passed as an argument
- **SOL takeover asks first** — fyrtaarn used to send `sol deactivate` before every activation, which silently disconnected anyone else on that BMC's SOL console. It now activates normally; if the BMC says the console is in use, a dialog offers to take it over (default Cancel), since the BMC cannot tell a colleague's live console from one left by a crashed client
- **Menu clicks during SOL** — mouse clicks on the menu bar were handled on every screen, even though the keyboard blocks the menu in SOL and while loading. Clicking e.g. Connect to BMC or a favourite during SOL switched screens with the SOL pane still live, and a later `[O]` replaced it without tearing it down, stranding ipmitool on the BMC's SOL slot. Clicks are now ignored on the SOL screen and while loading or scanning, matching the keyboard
- **Server model** — after login, the server model is read from Redfish with your credentials and shown as a separate Model line on BMC Info (iDRAC9 reports its IPMI product name only as `Unknown (0x100)`, and refuses the unauthenticated scan-time probe). The IPMI product name is left unchanged, since the compliance CVE lookup keys on it
- **Windows: SOL explained up front** — the built-in SOL pane needs a pty, which `creack/pty` does not provide on Windows. `[O]` now says so straight away and shows the equivalent `ipmitool … -E sol activate` command, instead of running a chassis check and power prompt that end in an error
- **Virtual media dialog** — states that an ISO URL is fetched by the BMC and must be reachable from the BMC's network
- **README** — documented Recent Connections and Favourites; Planned Features no longer lists MediaTypes slot discovery or the iLO numeric-path gap as outstanding
- **SEL viewer: missing and mangled records** — the SEL parser expected exactly the six-field standard line. Per ipmitool's `ipmi_sel.c`, OEM records without a timestamp (3 fields) and kernel-panic records (2 fields) were dropped from the viewer entirely; threshold and memory-ECC events carry a 7th field (`Reading 47 > Threshold 45 degrees C`) that was glued onto Direction, so a cleared threshold alarm never matched `Deasserted` and was not coloured green; timestamped OEM records had their hex data shown as the Direction. Every record shape is now parsed, extra detail goes into the event text, and Direction is only ever `Asserted`/`Deasserted`
- **Parser tests** — `parseMCInfo`, `parseLANInfo`, `parseChassisStatus`, `parseSDR`, `parseSEL`, `parseFRU` and `parseUserList` now have tests. The `mc info` fixture is a real iDRAC9 capture; the others are built from the exact printf formats in the ipmitool 1.8.18 source (file and line cited in each fixture) until real captures replace them
- **Removed `internal/providers`** — an empty `Provider interface{}` plus three files containing only a package line, imported by nothing since the initial commit. The working per-vendor seam is `redfish.VirtualMediaProvider`; a wider vendor layer can be designed once real vendor differences call for one
- **CI: tag-push releases fire again** — `release.yml` only triggered on `v*` tags, and tags have had no `v` prefix since 0.6.3, so pushing a release tag did nothing. It now triggers on `[0-9]+.[0-9]+.[0-9]+` (GitHub's documented semantic-version filter form) and names the release `Fyrtaarn <tag>`, as Publish Release does. Publish Release's own tag push cannot double-trigger it: pushes made with `GITHUB_TOKEN` start no workflow runs
- **CI: action updates** — setup-go 7.0.0 → 7.1.0 and action-gh-release 3.0.3 → 3.0.4 (Dependabot PR #2, folded in here because the shared build workflow moved where those pins live)
- **Windows SOL dialog** — shows the PowerShell form (`$env:IPMITOOL_PASSWORD='…'`) as well as Command Prompt's `set`
- **CI: one build definition** — the target matrix and build steps now live only in `build-binaries.yml`, a reusable workflow called by Build Fyrtaarn, Publish Release and the tag-push Release workflow. The copies had already drifted: `release.yml` was missing linux-armv7, darwin-amd64 and darwin-arm64. Release uploads now take whatever was built (`dist/fyrtaarn-*`) instead of a second hand-kept list. Also: the release version reaches shell steps via an environment variable rather than inline `${{ }}` (no script injection from the input), Build Fyrtaarn now fetches tags so `git describe` gives a version rather than a bare hash, and Publish Release's version prompt now says no `v` prefix, matching the tag convention

### 0.6.5

- **iDRAC9 connect failure** — connecting fetched `mc info`, `lan print` and `chassis status` in parallel, opening three RMCP+ sessions at once. An iDRAC9 (FW 4.20) accepts one and refuses the others with `insufficient resources for session`; whenever `mc info` was refused the connect failed and the host was left on the results tree as `vendor: Direct`. The three are now fetched sequentially (`mc info` first, so a bad password still fails fast), and `runIPMICommand` takes a per-host lock so no code path can open overlapping sessions to one BMC. The command timeout starts once the lock is held
- **Direct connect: vendor and Redfish** — hosts added via File > Connect to BMC, Recent Connections or Favourites were given a placeholder vendor of `Direct` and never probed, so the tree showed `vendor: Direct` and `HasRedfish` stayed false, which also blocked Redfish-gated actions such as `[V]` virtual media on directly connected hosts. A newly added direct host now gets the same enrichment a scan runs (reverse DNS and the Redfish probe), and the placeholder is replaced by the Redfish manufacturer or, failing that, the `mc info` manufacturer name. The two duplicated add-host code paths are now one helper
- **Favourites: password box** — the add/edit form has a masked Password field. It is session-only: it goes into the in-memory (XOR-obfuscated) session cache, never into `config.json`, so the favourite connects without prompting for the rest of the run. Editing pre-fills it from the cache; clearing it drops the cached entry; leaving it empty when adding a favourite leaves any live session untouched
- **Favourites: compact form** — labels and inputs share a row, so the four-field form fits an 80×24 terminal (12 rows instead of 17)

### 0.6.4

- **Favourites: name field** — favourites now have an optional display name (up to 32 characters), entered first in the add/edit form and shown as `Name — IP (user)` in both File > Favourites and the Manage Favourites dialog. Saved as `"name"` in `config.json`; existing entries without one load unchanged and display as before
- **Modal border alignment** — the top border of modal windows (Connect to BMC, Open SOL, and every other dialog) was drawn one column to the right of the side borders. `lipgloss.Place` centres each line on its own width, and the top border line (no shadow cell) was one column narrower than the body lines and two narrower than the bottom shadow row, so it got different padding. Every line is now padded to the same width. Also fixed: a title wider than the dialog body produced a top border wider than the box; the box is now widened to fit the title

### 0.6.3

- **Virtual media: local ISO** — the `[V]` dialog accepts a local file path as well as a URL; Redfish `InsertMedia` only ever fetches from a URL, so a local image is served from an HTTP server on this machine at a random path, bound to whichever local address routes to the BMC, and kept running for as long as the media stays mounted (some BMCs stream the image rather than copying it once); stopped on eject, on mounting another image, or on app exit
- **Virtual media: Supermicro backend** — `supermicroProvider` probes each Manager for a `VirtualMedia` collection and delegates to the generic Redfish walker when present; IKVM-only firmware now gets a specific "no Redfish VirtualMedia support" error instead of a raw 404
- **Virtual media: vendor dispatch** — `InsertMedia`/`EjectMedia` now pass the host's Redfish manufacturer through to `NewProvider`, so vendor backends are actually selected
- **Status bar** — `[V] VM` hint spelled out as `[V] Virtual Media`; easter-egg ticker width measured with `lipgloss.Width` rather than rune count
- **SOL reconnect after F10** — root cause found: F10 closed the pty while `listenSOL` had a `Read` in flight on it. `creack/pty` leaves the master fd in blocking mode (`pty.Open` calls `Fd()`), and Go's `os.File.Close` on a blocking fd defers the real `close(2)` until that `Read` returns. On an idle console it never did, so ipmitool was never hung up, kept running, and kept the BMC's single SOL slot alive with its 500 ms keepalives — every later `sol activate` was refused. F10 now ends ipmitool first and only then closes the pty: escape + `.` when at the start of a line (ipmitool deactivates the payload and closes its session itself), otherwise SIGINT (ipmitool 1.8.18's handler sends Close Session), escalating to SIGTERM and SIGKILL; an explicit `sol deactivate` follows whenever a signal was needed. The pty is drained during teardown so a busy console cannot block ipmitool's exit
- **SOL teardown/reconnect race** — the previous fire-and-forget `sol deactivate` after F10 could land after a quick reconnect's `sol activate` and kill the new session (payload instance is always 1). Teardown now completes before a new session can start (spinner shows "Closing SOL session...")
- **SOL stale reads** — pty reads now carry the session generation; a read still in flight from a closed session is discarded instead of being fed into the new pane and starting a second reader on it
- **SOL process reaping** — ipmitool is now `Wait()`ed on; previously every SOL session left a zombie process
- **SOL failure reason shown** — when ipmitool exits on its own with an error, its last output line is shown in the status bar and session log instead of a bare "SOL session ended"
- **SOL escape character** — ipmitool is started with `-e` set to Ctrl+], which the TUI never forwards, so typing `~.` at the start of a line no longer drops the session by accident
- **SOL teardown on exit** — quitting the app with a SOL session open now ends ipmitool cleanly rather than leaving it to a SIGHUP
- **Tests** — `internal/tui/sol_test.go` covers pty teardown with a blocked reader, the clean escape exit, no escape mid-line, signal escalation, and stale-read rejection

### 0.6.2

- **SOL backspace** — neither `\x08` (silently discarded by some GRUB builds) nor `\x7f` (treated as a printable dot character and stuffed into the command buffer) worked as a reliable erase character over IPMI SOL. Backspace is now implemented as the two-stroke sequence `ESC[D` (cursor-left, mapped by GRUB's serial driver to `GRUB_TERM_KEY_LEFT`) followed by `\x04` (Ctrl+D, `GRUB_TERM_CTRL|'d'` = delete-char-at-cursor). The net effect is backward-delete that works correctly in GRUB's readline without depending on any erase-character setting. An `editBuf` shadow tracks typed characters so the sequence is only sent when there is actually a character to the left of the cursor, preventing accidental Ctrl+D EOF on an empty line. Works correctly in bash readline over SOL as well.
- **SOL chassis power check** — pressing `[O]` (SOL) now performs a live `ipmitool chassis status` query rather than relying on the power state cached when MCInfo was loaded. Stale cache data (chassis was off at load time but is now running) no longer causes the power-on dialog to fire incorrectly. A spinner is shown during the brief IPMI round-trip. If the live check fails the old behaviour is preserved: SOL opens directly without a dialog.

### 0.6.1

- **SOL SS3 sequence handling** — `ESC O` (SS3) was unhandled in the VT100 parser; the following letter fell through to `processNormal` and was rendered as a literal character, causing artefacts such as `A2` and `B2` on AMI BIOS POST screens. Added `stateSSThree` — `ESC O A/B/C/D` now execute single-step cursor movement and `ESC O P–S` (F1–F4 in application mode) are silently consumed
- **SOL destructive backspace** — incoming `\x08` (BS) was non-destructive (cursor-left only); bare `\x08` from BIOS text fields left the deleted character visible on screen. Both `\x08` and `\x7f` in the incoming data stream now erase the cell to the left as well as moving the cursor, matching xterm behaviour
- **SOL 80-column pane cap** — the VT100 pane was sized to the full window width; the AMI BIOS formats its copyright and version strings for an 80-column terminal and relies on the terminal to word-wrap them. On wider windows the text never wrapped and appeared on a single line. The pane is now capped at 80 columns regardless of window width — IPMI SOL has no mechanism to advertise terminal width to the remote
- **CI: linux/armv7 target added** — `fyrtaarn-linux-armv7` (`GOARCH=arm GOARM=7`) added to both the build and publish workflows; covers Orange Pi, Raspberry Pi 2/3, and most 32-bit ARM SBCs
- **CI: macOS targets added** — `fyrtaarn-darwin-amd64` and `fyrtaarn-darwin-arm64` added; cross-compiled from the Ubuntu runner with `CGO_ENABLED=0`
- **CI: manual-trigger only** — build and lint workflows no longer fire on every push; both require an explicit "Run workflow" click in the Actions UI
- **Code: gofmt** — applied `gofmt -w` across all 13 files that had formatting inconsistencies; fixes the lint workflow `gofmt -l` check

### 0.6.0

- **VT100 screen emulator** — SOL console replaced its ANSI-stripping line buffer with a full VT100/VT220 screen grid; cursor positioning (`\x1b[r;cH`), clear-screen (`\x1b[2J`), scroll regions (`\x1b[r`), insert/delete lines (`CSI L/M`), erase-in-line/display (`CSI K/J`), reverse index (`ESC M`), save/restore cursor, and all standard CSI sequences are now processed correctly; GRUB menus, AMI/Award BIOS splashes, and interactive serial programs render with proper layout instead of garbled text
- **SOL CR/LF handling** — bare `\r` (used by Award BIOS and some firmware as a line ending without `\n`) is now converted to `\r\n` via a `pendingCR` flag; `\r\n` pairs and bare `\n` are unaffected; `\r` followed by an ESC sequence only resets the column (no implicit line-feed) so BIOS cursor-position sequences don't trigger spurious scrolls
- **Backspace fix** — SOL backspace now sends `\x08` (BS / ctrl+H) instead of `\x7f`; GRUB's readline editor and serial firmware universally expect `\x08` — `\x7f` was being rendered as a literal dot character
- **SOL window sizing** — pty is now told the correct visible dimensions (width−2 × height−6) rather than the full content height; GRUB and other applications that query terminal size now receive accurate row/column counts and draw their output correctly
- **SOL reconnect race fix** — `solDoneMsg` carries a session generation counter; stale done-messages from a closed pty no longer kill a newly reconnected session; stale scan progress bar no longer appears in the SOL loading modal on reconnect
- **SOL F2 / F9 pass-through** — function keys F2 (theme cycle) and F9 (menu bar) are now forwarded to the BMC when the SOL console is active rather than being intercepted by the TUI; all F1–F12 keys reach the remote console
- **Power on + SOL** — pressing `[O]` when the chassis is powered off shows a dialog offering to power the chassis on and open SOL immediately; the console connects right after the power command is issued so the full boot sequence is visible from BIOS POST
- **Direct connect** — File > Connect to BMC opens a three-field dialog (IP address, username, password) and connects without requiring a discovery scan; the host is added to the results tree so sensors, SEL, FRU, users, power control, and all other operations work normally from that point
- **Session log** — File > Start Log… prompts for a file path and begins logging; SOL output is written as rendered plain text in chronological order (lines are appended as they scroll off the VT100 grid, final screen state flushed on disconnect); scan results, BMC connections, SOL start/end, and direct-connect events are logged with timestamps; File > Stop Log closes the file; log is flushed cleanly on F10, session end, and application exit
- **Empty SOL pane hint** — when SOL is connected but the remote system is idle (already booted, getty waiting for input), a message prompts the user to press Enter to wake the remote terminal

### 0.5.3

- **Cross-platform release builds** — GitHub Actions release workflow builds and publishes pre-compiled binaries for all supported targets on every version tag: Linux amd64, arm64, ppc64 (G3/G4/G5 big-endian), mipsel/mips64el (softfloat); Windows amd64 and arm64; SHA256 checksum file included in every release
- **Makefile cross-compilation** — `make cross` builds all 8 targets locally; individual `make linux-amd64`, `make linux-sparc64`, etc. rules for targeted builds; version/commit/builddate injected via ldflags from git state
- **MC-style modal windows** — title embedded in top border line (`╭── Title ──╮`), drop shadow on right and bottom edges; consistent across all dialogs and loading screens; `renderModal()` centralises the chrome
- **About as modal** — Help > About is now a proper centered modal overlay; `screenAbout` full-screen path retired
- **Menu bar keyboard accelerators** — F9 activates bar; bare letter keys navigate: **F**ile / **H**elp at top level; **N**ew Scan / **E**xport / **T**heme / e**X**it / **A**bout in dropdowns; accelerator character rendered underlined
- **Virtual media provider interface** — `VirtualMediaProvider` interface with per-vendor stub files (`ilo.go`, `drac.go`, `supermicro.go`, `lom.go`, `xcc.go`, `cimc.go`); each documents the vendor's protocol landscape and version quirks as TODOs; `NewProvider()` factory dispatches when backends are implemented
- **Version-aware CPE matching** — firmware compliance tries an exact-version NVD query first; status bar shows `[version-specific]` or `[family-wide]`
- **Enriched export** — CSV/JSON exports include full BMC detail columns for connected hosts (`firmware_revision`, `ipmi_version`, `bmc_mac`, `bmc_ip`, `bmc_gateway`)
- **Broader vendor CPE coverage** — Cisco CIMC, Intel BMC, Huawei iBMC, Fujitsu iRMC added; AMI/Quanta query multiple NVD slugs and deduplicate
- **IPMI username bug fix** — `parseUserList` correctly handles ipmitool rows where the Name column is absent
- **Tree root `│` selectable** — cursor can sit on the root glyph before any host; `selectedHost = -1`
- **Scan progress bar fix** — nmap `<taskprogress percent="..."/>` XML elements parsed correctly; bar advances in real time

### 0.1.2

- **MC-style modal windows** — all dialogs and popups now render as proper centered modal windows: title embedded in the top border line (`╭── Title ──╮`), single-char drop shadow on right and bottom edges; consistent chrome across scan dialog, login, export, power control, user management, virtual media, and loading screens; `renderModal()` helper centralises the chrome so adding future dialogs is one call
- **About as modal** — Help > About now opens as an `activeDialog` overlay rather than replacing the content area; Esc or Enter on Close dismisses it; `screenAbout` full-screen path retired
- **Loading screen as modal** — scan and IPMI loading spinner/progress bar now appear in a centered `Scanning` / `Loading` modal rather than raw top-aligned text
- **Menu bar keyboard accelerators** — F9 activates the bar; bare letter keys navigate without requiring Alt (which collides with Esc in terminals): **F**ile / **H**elp at top level; **N**ew Scan / **E**xport / **T**heme / e**X**it / **A**bout in dropdowns; accelerator character rendered underlined in place

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
