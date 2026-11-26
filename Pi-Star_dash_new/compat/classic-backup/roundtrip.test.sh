#!/usr/bin/env bash
# Requires a local PHP CLI Docker image with ext-zip, fileinfo, curl and zip.
# bash roundtrip.test.sh [IMAGE]
set -euo pipefail
if [[ ${1:-} != --inside ]]; then
  here=$(cd "$(dirname "$0")" && pwd)
  stock=$(realpath "$here/../../../Pi-Star_DV_Dash")
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  mkdir "$tmp/admin"
  cp "$stock/admin/config_backup.php" "$tmp/admin/config_backup.php"
  sed -i 's@</head>@<link rel="stylesheet" href="/css/pistar-modern.css" /></head>@' "$tmp/admin/config_backup.php"
  chmod a+r "$tmp/admin/config_backup.php"
  node --input-type=module -e 'import {pathToFileURL} from "node:url";
    (await import(pathToFileURL(process.argv[1]))).patchBackup(process.argv[2])' "$here/patch.mjs" "$tmp"
  docker run --rm --network none --cap-drop ALL --security-opt no-new-privileges --user www-data \
    -v "$stock/config:/stock/config:ro" -v "$stock/lang:/stock/lang:ro" \
    -v "$tmp/admin/config_backup.php:/page.php:ro" -v "$here/roundtrip.test.sh:/test.sh:ro" \
    "${1:-pstar-backup-test-php:local}" bash /test.sh --inside
  exit
fi

# Map device paths to disposable files. Every command runs unprivileged.
cd /tmp
mkdir -p www/admin dev/etc bin
cp -r /stock/config www/
ln -s /stock/lang www/lang
ln -s ../config www/admin/config
sed -E -e 's@(/usr/local)?/etc/@/tmp/dev\1/etc/@g' \
    -e 's@/var/www/dashboard/config/@/tmp/www/config/@g' /page.php >www/admin/config_backup.php
sed -nE "s/^ +'([^']+)' +=> array\\('([^']+)', +([0-9]+),.*/\\1 \\2 \\3/p" www/admin/config_backup.php >map
while read -r name path mode; do
  [[ $name != dstar-radio.dstarrepeater ]] || continue
  mkdir -p "$(dirname "$path")"
  [[ -f $path ]] || echo "fixture=$name" >"$path"
done <map
echo 'static ip_address=192.0.2.1/24' >dev/etc/dhcpcd.conf
echo 'remotePassword=archive-test' >dev/etc/ircddbgateway
echo '[Pi-Star]' >dev/etc/pistar-release
echo 'Version=4.3.0' >>dev/etc/pistar-release
touch dev/etc/crontab
echo sentinel >dev/etc/dstar-radio.qa-keep
echo 'password=independent' >remote
touch -t 200001010000 remote
cat >bin/sudo <<'SH'
#!/bin/bash
set -eu
[[ $1 != -n ]] || shift
case $1 in
  install)
    dst=$9
    [[ $dst != '/root/.Remote Control' ]] || dst=/tmp/remote
    [[ ! -f /tmp/fail-install || $dst != /tmp/dev/etc/dstar-radio.mmdvmhost ]] || exit 1
    exec /usr/bin/install -m "$3" "$8" "$dst" ;;
  cat) [[ $2 == '/root/.Remote Control' ]] && exec /bin/cat /tmp/remote ;;
  systemctl|mount|timedatectl|sync) exit 0 ;;
  zip) [[ ! -f /tmp/fail-zip ]] || exit 1; exec /usr/bin/zip "${@:2}" ;;
  mkdir|cp|rm) exec "/bin/$1" "${@:2}" ;;
esac
exit 1
SH
chmod +x bin/sudo
php -l www/admin/config_backup.php
PATH="/tmp/bin:$PATH" php -d output_buffering=4096 -d display_errors=1 -S 127.0.0.1:8091 -t www >server.log 2>&1 &
pid=$!
trap 'kill "$pid" 2>/dev/null || true' EXIT
url=http://127.0.0.1:8091/admin/config_backup.php
curl -fs --retry 4 --retry-connrefused -c cookie "$url" >form
field=$(grep -oE 'name="[^"]+" value="[0-9a-f]{64}"' form | sed -E 's/name="([^"]+)" value="([^"]+)"/\1=\2/')
test -n "$field"
download() { curl -sS -b cookie -D headers -d "$field" -d action=download "$url" -o downloaded.zip; }
restore() { curl -fsS -b cookie -F "$field" -F action=restore -F fileToUpload=@downloaded.zip "$url" >result; }
download
grep -iq '^Content-Type: application/zip' headers
grep -iq '^Content-Disposition: attachment;' headers
php -r '
  $z = new ZipArchive;
  if (substr(file_get_contents("downloaded.zip"),0,4) !== "PK\3\4" || $z->open("downloaded.zip") !== true) exit(1);
  $count = 0;
  foreach (file("map") as $entry) {
      [$name,$path] = explode(" ", $entry);
      if (!file_exists($path)) continue;
      if ($z->getFromName($name) !== file_get_contents($path)) exit(2);
      $count++;
  }
  if ($count !== 27 || $z->numFiles !== $count) exit(3);'
rm dev/etc/dstar-radio.mmdvmhost
touch dev/etc/dstar-radio.dstarrepeater
echo 'mutated=1' >dev/etc/mmdvmhost
restore
grep -q 'Installed 27 file(s)' result
grep -q 'Configuration Restore Complete' result
php -r '
  $z = new ZipArchive;
  if ($z->open("downloaded.zip") !== true) exit(1);
  foreach (file("map") as $entry) {
      [$name,$path,$mode] = explode(" ", trim($entry));
      if ($z->locateName($name) === false) continue;
      if (file_get_contents($path) !== $z->getFromName($name) || (fileperms($path) & 0777) !== octdec($mode)) exit(2);
  }'
grep -qx sentinel dev/etc/dstar-radio.qa-keep
test -f dev/etc/dstar-radio.mmdvmhost
test ! -e dev/etc/dstar-radio.dstarrepeater
grep -qx 'password=independent' remote
test "$(stat -c %Y remote)" = 946684800
echo 'otherField=changed' >>dev/etc/ircddbgateway
restore
grep -q 'Configuration Restore Complete' result
test "$(stat -c %Y remote)" = 946684800
echo 'remotePassword=other' >dev/etc/ircddbgateway
restore
grep -q 'Configuration Restore Complete' result
grep -qx 'password=archive-test' remote
touch fail-install dev/etc/dstar-radio.dstarrepeater
restore
grep -q 'install failed for dstar-radio.mmdvmhost' result
! grep -q 'Configuration Restore Complete' result || exit 1
test -f dev/etc/dstar-radio.dstarrepeater
touch fail-zip
download
grep -q ' 500 ' headers
grep -q 'Cannot create backup ZIP' downloaded.zip
echo 'PASS: full ZIP round trip, marker and password effects, ZIP and install failures'
