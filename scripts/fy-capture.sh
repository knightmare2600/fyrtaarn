#!/usr/bin/env bash
# fy-capture.sh — read-only hardware capture for fyrtaarn development.
#
# Usage: ./fy-capture.sh <name> <bmc-ip> <user>
#   e.g. ./fy-capture.sh idrac9 192.168.1.27 root
#
# Writes ~/fyrtaarn-fixtures/<name>/ containing:
#   ipmi/*.txt      exactly the ipmitool commands fyrtaarn runs
#   redfish/*.json  unauthenticated probes, then an authenticated walk of
#                   Systems and Managers (incl. log services with the first
#                   page of entries, NetworkProtocol, VirtualMedia slots),
#                   accounts, firmware inventory, Chassis
#                   (thermal/power/sensors) and SessionService
#   vmedia/*        on an iLO, read-mode RIBCL replies (firmware version,
#                   virtual media status); on any BMC, Dell's OS deployment
#                   service by GET, and GetAttachStatus where it answers
#   summary.txt     exit code / HTTP status and timing for every call
#
# Nothing here changes BMC state: ipmitool read commands, HTTP GETs, RIBCL in
# read mode, and Dell's GetAttachStatus query (a POST that only reports).
# The password is read without echo and never written to disk or argv —
# ipmitool gets it via -E, curl via a config file on a process-substitution fd.
# Commands run one at a time (iDRAC9 refuses overlapping sessions).

set -u

if [ $# -ne 3 ]; then
  echo "usage: $0 <name> <bmc-ip> <user>" >&2
  exit 2
fi
name=$1
ip=$2
user=$3

for tool in ipmitool curl python3; do
  command -v "$tool" >/dev/null || { echo "missing: $tool" >&2; exit 1; }
done

# Fail fast on a wrong or empty address (e.g. an unset shell variable):
# otherwise every call below waits out its own 20-30 s timeout.
if ! timeout 5 bash -c "exec 3<>/dev/tcp/$ip/443" 2>/dev/null; then
  echo "cannot reach '$ip' on port 443 (HTTPS) within 5 s; check the address" >&2
  exit 1
fi

out=~/fyrtaarn-fixtures/$name
mkdir -p "$out/ipmi" "$out/redfish"
: > "$out/summary.txt"

read -rsp "Password for $user@$ip: " pw
echo
if [ -z "$pw" ]; then
  echo "no password given; nothing captured" >&2
  exit 1
fi
export IPMITOOL_PASSWORD=$pw

# curl config needs \ and " escaped inside a quoted value.
esc=${pw//\\/\\\\}
esc=${esc//\"/\\\"}

now_ms() { echo $(( $(date +%s%N) / 1000000 )); }

log() { printf '%s\n' "$*" | tee -a "$out/summary.txt"; }

ipmi() {
  local f=$1
  shift
  local t0
  t0=$(now_ms)
  ipmitool -I lanplus -H "$ip" -U "$user" -E "$@" > "$out/ipmi/$f.txt" 2>&1
  local rc=$?
  log "$(printf 'ipmi    %-26s exit=%-3d %6d ms' "$f" "$rc" $(( $(now_ms) - t0 )))"
}

# rf <auth|noauth> <path> : GET https://<ip><path>, saved under a name
# derived from the path. Prints the saved file's path.
rf() {
  local mode=$1 path=$2
  local f
  f=$out/redfish/$(printf '%s' "$path" | sed 's#^/##; s#/$##; s#[/.]#_#g')$([ "$mode" = noauth ] && echo .noauth).json
  local t0 code rc
  t0=$(now_ms)
  if [ "$mode" = auth ]; then
    code=$(curl -sk --max-time 30 -o "$f" -w '%{http_code}' \
      -K <(printf 'user = "%s:%s"\n' "$user" "$esc") "https://$ip$path")
  else
    code=$(curl -sk --max-time 30 -o "$f" -w '%{http_code}' "https://$ip$path")
  fi
  rc=$?
  # http=000 alone does not say why; curl's exit code does
  # (7 refused, 28 timeout, 35 TLS handshake failed, 60 certificate).
  log "$(printf 'redfish %-6s %-60s http=%s curl=%-3d %6d ms' "$mode" "$path" "$code" "$rc" $(( $(now_ms) - t0 )))" >&2
  echo "$f"
}

# members <file> : @odata.id of each collection member (standard Members, or
# the iLO 4 legacy links.Member[].href).
members() {
  python3 - "$1" <<'PY' 2>/dev/null
import json, sys
try:
  d = json.load(open(sys.argv[1]))
except Exception:
  sys.exit(0)
m = d.get("Members") or d.get("links", {}).get("Member") or []
for x in m:
  p = x.get("@odata.id") or x.get("href") or ""
  if p:
    print(p)
PY
}

# link <file> <key> : @odata.id (or href) of a linked resource, e.g. VirtualMedia.
link() {
  python3 - "$1" "$2" <<'PY' 2>/dev/null
import json, sys
try:
  d = json.load(open(sys.argv[1]))
except Exception:
  sys.exit(0)
v = d.get(sys.argv[2]) or d.get("links", {}).get(sys.argv[2]) or {}
if isinstance(v, dict):
  print(v.get("@odata.id") or v.get("href") or "")
PY
}

log "== $name ($ip) $(date -u +%Y-%m-%dT%H:%M:%SZ)  $(ipmitool -V 2>&1)"

# protocols <file> : one line listing each protocol in a Redfish
# ManagerNetworkProtocol resource as name=on/off:port, e.g. "IPMI=off:623".
# Shows at a glance whether IPMI over LAN is switched off on the BMC.
protocols() {
  python3 - "$1" <<'PY' 2>/dev/null
import json, sys
try:
  d = json.load(open(sys.argv[1]))
except Exception:
  sys.exit(0)
out = []
for k, v in sorted(d.items()):
  if isinstance(v, dict) and "ProtocolEnabled" in v:
    state = {True: "on", False: "off"}.get(v["ProtocolEnabled"], "?")
    port = v.get("Port")
    out.append("%s=%s%s" % (k, state, ":%s" % port if port is not None else ""))
print(" ".join(out))
PY
}

# --- IPMI: the commands fyrtaarn runs, in its order -------------------------
ipmi mc-info        mc info
ipmi lan-print      lan print
ipmi chassis-status chassis status
ipmi sdr-list       sdr list
# The raw sensor repository, which records each sensor's owner (the BMC or
# another controller): fyrtaarn reads other owners' sensors through the BMC,
# and no capture so far shows whether any BMC here has such sensors.
ipmi sdr-dump       sdr dump "$out/ipmi/sdr.bin"
ipmi sel-list       sel list
ipmi fru            fru
ipmi user-list      user list
ipmi sol-info       sol info

# --- Redfish: what the scan-time probe sees without credentials -------------
rf noauth /redfish/v1/ >/dev/null
rf noauth /redfish/v1/Systems/ >/dev/null

# --- Redfish: authenticated walk ------------------------------------------
rf auth /redfish/v1/SessionService/ >/dev/null

# field <file> <key> : a top-level string field of a JSON document.
field() {
  python3 - "$1" "$2" <<'PY' 2>/dev/null
import json, sys
try:
  v = json.load(open(sys.argv[1])).get(sys.argv[2])
except Exception:
  sys.exit(0)
if isinstance(v, str):
  print(v)
PY
}

# iLO 4 is not asked for log entries: the first request for its IML entries
# stalled an iLO 4 2.54's web server (no answer to anything for minutes, on
# a production box). Its IPMI event log (sel-list) is captured above.
ilo4=0
ilo=0
for m in $(members "$(rf auth /redfish/v1/Managers/)" | head -2); do
  case $(field "$(rf auth "$m")" FirmwareVersion) in
    "iLO 4"*) ilo4=1; ilo=1 ;;
    "iLO"*) ilo=1 ;;
  esac
done

# logs <file> : the log services linked from a System or Manager, each with
# the first page of its entries (no paging: iDRAC's Lifecycle log alone runs
# to thousands of entries).
logs() {
  local ls svc
  ls=$(link "$1" LogServices)
  [ -n "$ls" ] || return 0
  if [ "$ilo4" = 1 ]; then
    log "logs    skipped on iLO 4 (requesting entries stalled its web server)"
    return 0
  fi
  for svc in $(members "$(rf auth "$ls")"); do
    local e
    e=$(link "$(rf auth "$svc")" Entries)
    if [ -n "$e" ]; then
      rf auth "$e" >/dev/null
    fi
  done
}

sys_col=$(rf auth /redfish/v1/Systems/)
for s in $(members "$sys_col" | head -2); do
  logs "$(rf auth "$s")"
done

mgr_col=$(rf auth /redfish/v1/Managers/)
for m in $(members "$mgr_col" | head -2); do
  mf=$(rf auth "$m")
  logs "$mf"
  np=$(link "$mf" NetworkProtocol)
  if [ -n "$np" ]; then
    npf=$(rf auth "$np")
    log "protocols $m: $(protocols "$npf")"
  fi
  vm=$(link "$mf" VirtualMedia)
  if [ -n "$vm" ]; then
    vmf=$(rf auth "$vm")
    for slot in $(members "$vmf"); do
      rf auth "$slot" >/dev/null
    done
  fi
done

# Accounts (user names, roles, enabled/locked; no passwords) and firmware
# inventory, followed from the service root since paths differ (iLO ends
# them in "/"; the iLO 4 has no UpdateService).
root=$(rf auth /redfish/v1/)
for svc_key in AccountService:Accounts UpdateService:FirmwareInventory; do
  sl=$(link "$root" "${svc_key%%:*}")
  [ -n "$sl" ] || continue
  cl=$(link "$(rf auth "$sl")" "${svc_key##*:}")
  [ -n "$cl" ] || continue
  for item in $(members "$(rf auth "$cl")"); do
    rf auth "$item" >/dev/null
  done
done

ch_col=$(rf auth /redfish/v1/Chassis/)
for c in $(members "$ch_col" | head -2); do
  cf=$(rf auth "$c")
  for key in Thermal Power ThermalSubsystem PowerSubsystem Sensors EnvironmentMetrics; do
    l=$(link "$cf" "$key")
    if [ -n "$l" ]; then
      rf auth "$l" >/dev/null
    fi
  done
done

# --- Virtual media protocols of other backends -----------------------------
# fyrtaarn's iLO 3 backend speaks RIBCL to /ribcl, which iLO 4 and 5 still
# answer; its iDRAC 7/8 backend uses Dell's OEM OS deployment service, which
# iDRAC9 also has. Read-only here: RIBCL in MODE="read" (GET_FW_VERSION,
# GET_VM_STATUS), Dell's two documented URI prefixes by GET, and on a Dell
# that answers, GetAttachStatus (a POST, documented by Dell as a status
# query; it attaches nothing). Raw replies are kept with their headers.
mkdir -p "$out/vmedia"

# ribcl <name> <RIB_INFO MODE> <command> : POST a RIBCL document. The
# password is inside the XML, so the body goes to curl on a process-
# substitution fd, never to disk or argv. XML specials in the password are
# not escaped, as python-hpilo does not escape them.
ribcl() {
  local f=$out/vmedia/ribcl-$1.txt t0 code rc
  t0=$(now_ms)
  code=$(curl -sk --max-time 30 -D "$out/vmedia/ribcl-$1.headers" -o "$f" -w '%{http_code}' \
    -H 'Content-Type: text/xml' \
    --data-binary @<(printf '<?xml version="1.0"?>\r\n<RIBCL VERSION="2.0"><LOGIN USER_LOGIN="%s" PASSWORD="%s"><RIB_INFO MODE="%s">%s</RIB_INFO></LOGIN></RIBCL>\r\n' "$user" "$pw" "$2" "$3") \
    "https://$ip/ribcl")
  rc=$?
  log "$(printf 'ribcl   %-26s http=%s curl=%-3d %6d ms' "$1" "$code" "$rc" $(( $(now_ms) - t0 )))"
}

if [ "$ilo" = 1 ]; then
  ribcl fw-version read '<GET_FW_VERSION/>'
  ribcl vm-status-cdrom read '<GET_VM_STATUS DEVICE="CDROM"/>'
  ribcl vm-status-floppy read '<GET_VM_STATUS DEVICE="FLOPPY"/>'
fi

for base in /redfish/v1/Dell/Systems/System.Embedded.1/DellOSDeploymentService \
            /redfish/v1/Systems/System.Embedded.1/Oem/Dell/DellOSDeploymentService; do
  f=$(rf auth "$base")
  if [ -s "$f" ] && grep -q '"@odata.id"\|"Actions"' "$f"; then
    t0=$(now_ms)
    g=$out/vmedia/dell-getattachstatus$(printf '%s' "$base" | tr '/' '_').json
    code=$(curl -sk --max-time 30 -D "${g%.json}.headers" -o "$g" -w '%{http_code}' \
      -K <(printf 'user = "%s:%s"\n' "$user" "$esc") \
      -H 'Content-Type: application/json' --data-binary '{}' \
      "https://$ip$base/Actions/DellOSDeploymentService.GetAttachStatus")
    rc=$?
    log "$(printf 'dell    GetAttachStatus %-44s http=%s curl=%-3d %6d ms' "$base" "$code" "$rc" $(( $(now_ms) - t0 )))"
  fi
done

unset IPMITOOL_PASSWORD pw esc
log "== done: $out"
