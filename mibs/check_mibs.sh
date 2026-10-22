#!/usr/bin/env bash
# Validates the MIBs with net-snmp:
#   1. the D-Link MIBs load without diagnostics (standard dependencies from deps/);
#   2. every private OID the exporter reads (taken from the recorded walks) resolves
#      to a symbolic name.
set -euo pipefail

cd "$(dirname "$0")"
STD=${SNMP_STD_MIBS:-/usr/share/snmp/mibs}
MIBDIRS="$STD:$PWD/deps:$PWD"
MODULES=AGENT-GENERAL-MIB:EQUIPMENT-MIB:DLINK-DDM-RECONSTRUCTED-MIB
DIAG='At line [0-9]|^Cannot|^Did not find|^Unlinked'
fail=0

n=$(snmptranslate -M "$MIBDIRS" -m "$MODULES" -Tp 2>&1 | grep -cE "$DIAG" || true)
echo "load: $n diagnostics"
[ "$n" -eq 0 ] || { snmptranslate -M "$MIBDIRS" -m "$MODULES" -Tp 2>&1 | grep -E "$DIAG"; fail=1; }

# Private branches read by internal/collector (keep in sync with the OID constants there).
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
LC_ALL=C grep -ahoE '^\.1\.3\.6\.1\.4\.1\.171\.12\.[0-9.]+' ../testdata/*.walk |
  grep -E '171\.12\.(1\.1\.(6|9|10|12|13)\.|11\.1\.(6|7|8)\.1\.|11\.1\.9\.4\.1\.(2|9|10|11|12|17)\.|72\.(2\.1\.1\.1|3\.1\.1\.1)\.)' |
  xargs -n 200 snmptranslate -M "$MIBDIRS" -m "$MODULES" -OS 2>&1 | grep . > "$tmp"
bad=$(grep -cE 'enterprises\.171|^\.?1\.3\.6|Unknown|Cannot|Entry\.[0-9]' "$tmp" || true)
echo "resolve: $(wc -l < "$tmp" | tr -d ' ') OIDs, $bad unresolved"
[ "$bad" -eq 0 ] || { grep -E 'enterprises\.171|^\.?1\.3\.6|Unknown|Cannot|Entry\.[0-9]' "$tmp" | head; fail=1; }
exit $fail
