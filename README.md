
# Fyrtaarn

Nordic Out-of-Band Management for IPMI, BMC, iLO, DRAC, and friends.

---

## Overview

`fyrtaarn` is a cross-platform Golang TUI application for discovering, inspecting, and eventually managing out-of-band management controllers such as:

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

The name reflects the project's purpose:

- visibility
- remote control
- infrastructure oversight
- out-of-band access

It also intentionally alludes to:
- iLO
- LOM
- remote management systems generally

without tying the project to a single vendor.

---

## Current Features

- Bubble Tea TUI interface
- Solarized Dark theme
- keyboard navigation
- mouse support
- asynchronous network scanning
- Nmap XML parsing
- heuristic BMC detection
- IPMI/RMCP fingerprinting
- provider abstraction groundwork

---

## Planned Features

### Discovery

- configurable subnets
- configurable ports
- hostnames
- reverse DNS
- scan profiles
- Redfish detection

### Authentication

- local authentication
- LDAP
- Active Directory
- RADIUS

### Management

- power control
- sensor viewing
- SOL console access
- event log viewing
- user management
- firmware inventory

### Virtual Media

- ISO mounting
- floppy image support
- remote media redirection
- Redfish virtual media support

### Providers

- SuperMicro
- Quanta
- Dell DRAC
- HP iLO
- Oracle ILOM
- Lenovo XCC

---

## Requirements

### Runtime

- `nmap`
- `ipmitool`

### Development

Ubuntu/Debian:

```bash
sudo apt install golang-go nmap ipmitool make
````

---

## Build

```bash
make build
```

Binary output:

```text
dist/fyrtaarn
```

---

## Run

```bash
make run
```

---

## Development Status

Early Proof-of-Concept.

The project currently focuses on:

* architecture
* discovery
* provider abstraction
* TUI workflows

before implementing advanced BMC functionality.

---

## Project Goals

* cross-platform
* minimal dependencies
* terminal-first UX
* keyboard-first operation
* vendor abstraction
* operational simplicity
* infrastructure-friendly workflows
* sane theming
* no browser dependency
* no Java dependency

---

## Design Philosophy

`fyrtaarn` intentionally shells out to mature tooling initially:

* `nmap`
* `ipmitool`

rather than prematurely reimplementing decades of vendor quirks in pure Go.

The project prioritises:

* operational reliability
* maintainability
* portability
* incremental architecture

over unnecessary reinvention.

Native protocol implementations may come later.

---

## Inspirations & Acknowledgements

This project exists partly because of the work of many researchers, operators, and educators.

Special acknowledgement and thanks to:

### Dan Kaminsky

For pioneering work on:

* BMC/IPMI security research
* remote management attack surfaces
* infrastructure visibility

His work helped expose how dangerous and under-examined out-of-band systems could be.

A small tribute remains in the code comments.

### IppSec

For educational walkthroughs and practical demonstrations including:

* Keeper
* Shibboleth
* Active Directory abuse
* infrastructure operations
* Trying your hardest to say Rødgrød med fløde... If you know, you know... ;)

### 0xDF

For detailed technical writeups and excellent explanations of:

* post-exploitation workflows
* "beyond root" methodologies
* operational enumeration
* infrastructure internals
* Jeg var at gå agurk med "should" på "Release Comittee" (:

---

## TODO

### Immediate

* selectable scan results
* configurable subnet entry
* proper table rendering
* credential dialog
* IPMI login workflow
* async spinner/progress display

### Near Term

* provider detection improvements
* Redfish support
* SOL console support
* configuration file
* theme switching
* virtual media groundwork

### Longer Term

* native IPMI support in Go
* SSH tunnelling support
* VPN-aware workflows
* inventory export
* firmware compliance checks
* sensor dashboards
* cluster/fleet operations

---

## Changelog

### 0.0.1

Initial Proof-of-Concept release.

Implemented:

* project structure
* Bubble Tea TUI
* Solarized theme
* keyboard navigation
* popup dialogs
* async Nmap execution
* XML parsing
* heuristic BMC detection
* GitHub Actions build workflow

---

## License

GPL-3.0-or-later

```
