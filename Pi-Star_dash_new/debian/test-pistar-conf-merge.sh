#!/bin/bash
# Test debian/pistar-conf-merge: only missing keys are appended, edits kept, idempotent.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
t=$(mktemp -d); trap 'rm -rf "${t:?}"' EXIT
printf '# local\nPANEL_PORT=9999\nCONF_MMDVMHOST=/etc/mmdvmhost\n' >"$t/conf"
printf '# default\nPANEL_PORT=8787\nCONF_MMDVMHOST=/etc/mmdvmhost\nWIFI_BACKEND=networkmanager\n# WIFI_INTERFACE=commented\nWIFI_INTERFACE=wlan0\n' >"$t/default"
out=$("$here/pistar-conf-merge" "$t/conf" "$t/default")
[[ $out == $'pistar-dashboard: added setting WIFI_BACKEND\npistar-dashboard: added setting WIFI_INTERFACE' ]]
grep -qx 'PANEL_PORT=9999' "$t/conf" && ! grep -q 'PANEL_PORT=8787' "$t/conf"
[[ $(grep -c '^WIFI_BACKEND=networkmanager$' "$t/conf") == 1 && $(grep -c '^WIFI_INTERFACE=wlan0$' "$t/conf") == 1 ]]
before=$(md5sum <"$t/conf"); [[ -z $("$here/pistar-conf-merge" "$t/conf" "$t/default") && $(md5sum <"$t/conf") == "$before" ]]
cp "$here/../pistar.conf" "$t/same"; [[ -z $("$here/pistar-conf-merge" "$t/same" "$here/../pistar.conf") ]]; cmp -s "$t/same" "$here/../pistar.conf"
echo "PASS pistar-conf-merge: missing WIFI_* keys appended once, local PANEL_PORT kept, unchanged conffile untouched, idempotent"
