#!/bin/bash
# Test radio/seed-configs: missing files are created from the zip, existing ones untouched.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
t=$(mktemp -d); trap 'rm -rf "${t:?}"' EXIT
cd "$t"
printf '[General]\nCallsign=M1ABC\n' >ysfgateway
printf 'gatewayCallsign=M1ABC\n' >ircddbgateway
printf '[General]\nCallsign=DEFAULT\n' >mmdvmhost
zip -q defaults.zip ysfgateway ircddbgateway mmdvmhost
mkdir etc
printf '[General]\nCallsign=UT3UDX\n' >etc/mmdvmhost
chmod 600 etc/mmdvmhost
printf 'ysfgateway %s/etc/ysfgateway\nircddbgateway %s/etc/ircddbgateway\nmmdvmhost %s/etc/mmdvmhost\nmissing %s/etc/missing\n' "$t" "$t" "$t" "$t" >seed
"$here/seed-configs" seed defaults.zip >out 2>err || true
cmp -s ysfgateway etc/ysfgateway
cmp -s ircddbgateway etc/ircddbgateway
[[ $(cat etc/mmdvmhost) == $'[General]\nCallsign=UT3UDX' ]]
[[ $(stat -c %a etc/mmdvmhost) == 600 && $(stat -c %a etc/ysfgateway) == 644 ]]
[[ ! -e etc/missing ]] && ! ls etc/.pistar-seed.* >/dev/null 2>&1
before=$(stat -c %Y etc/ysfgateway); "$here/seed-configs" seed defaults.zip >/dev/null 2>&1 || true
[[ $(stat -c %Y etc/ysfgateway) == "$before" ]]
grep -q "created $t/etc/ysfgateway" out
echo "PASS seed-configs: missing ysfgateway/ircddbgateway created 0644 from the zip, existing mmdvmhost (0600) untouched, unknown member skipped, idempotent"
