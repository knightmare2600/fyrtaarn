#!/usr/bin/env bash
# fy-capture.sh — read-only hardware capture for fyrtaarn development.
#
# Usage: ./fy-capture.sh <name> <bmc-ip> <user>
#   e.g. ./fy-capture.sh idrac9 192.168.1.27 root
#
# Writes ~/fyrtaarn-fixtures/<name>/ containing:
#   ipmi/*.txt      exactly the ipmitool commands fyrtaarn runs
#   redfish/*.json  unauthenticated probes, then an authenticated walk of
#                   Systems, Managers (incl. VirtualMedia slots), Chassis
#                   (thermal/power/sensors) and SessionService
#   summary.txt     exit code / HTTP status and timing for every call
#
# Nothing here changes BMC state: ipmitool read commands and HTTP GETs only.
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

# --- IPMI: the commands fyrtaarn runs, in its order -------------------------
ipmi mc-info        mc info
ipmi lan-print      lan print
ipmi chassis-status chassis status
ipmi sdr-list       sdr list
ipmi sel-list       sel list
ipmi fru            fru
ipmi user-list      user list
ipmi sol-info       sol info

# --- Redfish: what the scan-time probe sees without credentials -------------
rf noauth /redfish/v1/ >/dev/null
rf noauth /redfish/v1/Systems/ >/dev/null

# --- Redfish: authenticated walk ------------------------------------------
rf auth /redfish/v1/SessionService/ >/dev/null

sys_col=$(rf auth /redfish/v1/Systems/)
for s in $(members "$sys_col" | head -2); do
  rf auth "$s" >/dev/null
done

mgr_col=$(rf auth /redfish/v1/Managers/)
for m in $(members "$mgr_col" | head -2); do
  mf=$(rf auth "$m")
  vm=$(link "$mf" VirtualMedia)
  if [ -n "$vm" ]; then
    vmf=$(rf auth "$vm")
    for slot in $(members "$vmf"); do
      rf auth "$slot" >/dev/null
    done
  fi
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

unset IPMITOOL_PASSWORD pw esc
log "== done: $out"
