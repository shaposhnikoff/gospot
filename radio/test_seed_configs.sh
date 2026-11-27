set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
t=$(mktemp -d); trap 'rm -rf "${t:?}"' EXIT
cd "$t"
"$here/gospot-rebrand" resolve "$here/gospot-rename.table" "$here/../Pi-Star_dash_new/gospot.conf" >table
log_dir=$(sed -n 's/^GOSPOT_LOG_DIR=//p' "$here/../Pi-Star_dash_new/gospot.conf")
printf '[General]\nCallsign=M1ABC\n[Info]\nDescription=M1ABC_Pi-Star\nURL=http://www.mw0mwz.co.uk/pi-star/\n[Log]\nFilePath=/var/log/pi-star\nFileRoot=YSFGateway\n' >ysfgateway
printf 'gatewayCallsign=M1ABC\n' >ircddbgateway
printf '[General]\nCallsign=DEFAULT\n' >mmdvmhost
printf '# Pi-Star Remote config file\n[enable]\nenabled=false\n' >pistar-remote
printf '[General]\nPath=/opt/pistar-unknown\n' >unknown
zip -q defaults.zip ysfgateway ircddbgateway mmdvmhost pistar-remote unknown
mkdir etc
printf '[General]\nCallsign=UT3UDX\n' >etc/mmdvmhost
chmod 600 etc/mmdvmhost
member=$("$here/gospot-rebrand" name table pistar-remote)
[[ $member == gospot-remote ]]
printf 'ysfgateway %s/etc/ysfgateway\nircddbgateway %s/etc/ircddbgateway\nmmdvmhost %s/etc/mmdvmhost\npistar-remote %s/etc/%s\nmissing %s/etc/missing\n' \
  "$t" "$t" "$t" "$t" "$member" "$t" >seed
printf 'unknown %s/etc/unknown\n' "$t" >>seed
"$here/seed-configs" seed defaults.zip table >out 2>err && false
grep -q 'unknown:2: not in gospot-rename.table' err
[[ ! -e etc/unknown ]]
[[ $(cat etc/ysfgateway) == $'[General]\nCallsign=M1ABC\n[Info]\nDescription=M1ABC_GoSpot\nURL=http://www.mw0mwz.co.uk/pi-star/\n[Log]\nFilePath='"$log_dir"$'\nFileRoot=YSFGateway' ]]
cmp -s ircddbgateway etc/ircddbgateway
[[ $(cat etc/gospot-remote) == $'# GoSpot Remote config file\n[enable]\nenabled=false' ]]
[[ $(cat etc/mmdvmhost) == $'[General]\nCallsign=UT3UDX' ]]
[[ $(stat -c %a etc/mmdvmhost) == 600 && $(stat -c %a etc/ysfgateway) == 644 && $(stat -c %a etc/gospot-remote) == 644 ]]
[[ ! -e etc/missing ]] && ! ls etc/.gospot-seed.* >/dev/null 2>&1
before=$(stat -c %Y etc/ysfgateway); sed -i '/^unknown /d' seed; "$here/seed-configs" seed defaults.zip table >out2 2>/dev/null
[[ $(stat -c %Y etc/ysfgateway) == "$before" && ! -s out2 ]]
grep -q "created $t/etc/ysfgateway" out
echo "PASS seed-configs: missing ysfgateway/ircddbgateway/gospot-remote created 0644 from the zip and rewritten (FilePath -> $log_dir, Description, comments; URL kept), unknown pistar path refused, existing mmdvmhost (0600) untouched, unknown member skipped, idempotent"
