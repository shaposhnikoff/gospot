#!/bin/bash
# Test debian/gospot-migrate on fixture trees: a pistar-dashboard board becomes
# a gospot board (settings, credentials, state, Wi-Fi country, old units),
# nothing is lost or printed, and later runs change nothing.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
t=$(mktemp -d); trap 'rm -rf "${t:?}"' EXIT
me=$(id -un)
export GOSPOT_MIGRATE_NEW_DEFAULT=$here/../gospot.conf GOSPOT_MIGRATE_OLD_DEFAULT=$here/migrate/pistar-dashboard.conf
export GOSPOT_MIGRATE_ACCOUNTS="$me:$me" GOSPOT_MIGRATE_SYSTEMCTL=$t/systemctl
secret=S3CRET-$RANDOM-admin-password-value
printf '#!/bin/sh\necho "$*" >>%s/systemctl.log\n[ "$1 $5" = "show pistar-panel.service" ] && echo loaded\nexit 0\n' "$t" >"$t/systemctl"
chmod +x "$t/systemctl"
snapshot() { (cd "$1" && find . -printf '%p %m %s %T@\n' | LC_ALL=C sort; find . -type f -exec md5sum {} + | LC_ALL=C sort); }

# --- 1. upgraded board ------------------------------------------------------------
r=$t/board
mkdir -p "$r/etc/pistar-dashboard" "$r/etc/gospot" "$r/etc/pistar-wizard" "$r/etc/modprobe.d" \
  "$r/var/lib/pistar-panel-config/rollback" "$r/var/lib/pistar-dashboard-bm"
cp "$here/../gospot.conf" "$r/etc/gospot/gospot.conf"
sed -e 's|^TGIF_API_URL=.*|TGIF_API_URL=https://tgif.example.net/v1/|' -e 's|^PANEL_PORT=.*|PANEL_PORT=9999|' \
  "$here/migrate/pistar-dashboard.conf" >"$r/etc/pistar-dashboard/pistar.conf"
printf '\n# Added by a pistar-dashboard upgrade: new settings with their packaged defaults.\nOBSOLETE_KEY=1\n' >>"$r/etc/pistar-dashboard/pistar.conf"
printf '%s\n' "$secret" >"$r/etc/pistar-wizard/admin-password"
printf 'KEY-%s\n' "$secret" >"$r/etc/pistar-wizard/tls-key.pem"
printf 'CERT\n' >"$r/etc/pistar-wizard/tls-cert.pem"
chmod 600 "$r/etc/pistar-wizard/admin-password" "$r/etc/pistar-wizard/tls-key.pem"
chmod 644 "$r/etc/pistar-wizard/tls-cert.pem"
touch -d '2025-01-02 03:04:05' "$r/etc/pistar-wizard/"*
printf 'lock\n' >"$r/var/lib/pistar-panel-config/configure.lock"
printf 'snap\n' >"$r/var/lib/pistar-panel-config/rollback/mmdvmhost"
printf '{"tg":91}\n' >"$r/var/lib/pistar-dashboard-bm/profile.json"
chmod 700 "$r/var/lib/pistar-dashboard-bm"
printf 'options cfg80211 ieee80211_regdom=GB\n' >"$r/etc/modprobe.d/pistar-wifi-country.conf"
old_tree=$(cd "$r" && find etc/pistar-dashboard etc/pistar-wizard var/lib/pistar-panel-config var/lib/pistar-dashboard-bm -type f -exec md5sum {} + | sort)

out=$("$here/gospot-migrate" "$r")
printf '%s\n' "$out"
[[ $out != *"$secret"* ]]
conf=$r/etc/gospot/gospot.conf
# Old defaults become new defaults; a local value is kept; build-time and
# obsolete settings are not; the Linux account stays the board's.
grep -qx 'GOSPOT_LOG_DIR=/var/log/gospot' "$conf" && grep -qx 'WIZARD_CONFIG_DIR=/etc/gospot/wizard' "$conf"
grep -qx 'TGIF_API_URL=https://tgif.example.net/v1/' "$conf"
grep -qx 'PANEL_PORT=8787' "$conf" && ! grep -q OBSOLETE_KEY "$conf"
grep -qx 'GOSPOT_LINUX_USER=pi-star' "$conf"
diff <(grep -v -e '^TGIF_API_URL=' -e '^GOSPOT_LINUX_USER=' "$conf") \
     <(grep -v -e '^TGIF_API_URL=' -e '^GOSPOT_LINUX_USER=' "$here/../gospot.conf")
[[ $(stat -c %a "$conf") == 644 ]]
grep -qx 'gospot-migrate: kept local TGIF_API_URL' <<<"$out"
grep -qx 'gospot-migrate: kept local GOSPOT_LINUX_USER' <<<"$out"
grep -qx 'gospot-migrate: ignored local PANEL_PORT (build-time setting, packaged value used)' <<<"$out"
grep -qx 'gospot-migrate: dropped OBSOLETE_KEY (no longer used)' <<<"$out"
# The merged file is complete: the conffile merge has nothing to add.
[[ -z $("$here/gospot-conf-merge" "$conf" "$here/../gospot.conf") ]]
# Credentials: same bytes, modes and times; the old copies are untouched.
w=$r/etc/gospot/wizard
for f in admin-password tls-key.pem tls-cert.pem; do
  cmp -s "$r/etc/pistar-wizard/$f" "$w/$f"
  [[ $(stat -c '%a %U %Y' "$r/etc/pistar-wizard/$f") == $(stat -c '%a %U %Y' "$w/$f") ]]
done
[[ $(stat -c %a "$w/admin-password") == 600 && $(stat -c %a "$w/tls-key.pem") == 600 && $(stat -c %a "$w/tls-cert.pem") == 644 ]]
# State directories, with their modes.
cmp -s "$r/var/lib/gospot/rollback/mmdvmhost" "$r/var/lib/pistar-panel-config/rollback/mmdvmhost"
cmp -s "$r/var/lib/gospot/configure.lock" "$r/var/lib/pistar-panel-config/configure.lock"
cmp -s "$r/var/lib/gospot-bm/profile.json" "$r/var/lib/pistar-dashboard-bm/profile.json"
[[ $(stat -c %a "$r/var/lib/gospot-bm") == 700 ]]
[[ $old_tree == $(cd "$r" && find etc/pistar-dashboard etc/pistar-wizard var/lib/pistar-panel-config var/lib/pistar-dashboard-bm -type f -exec md5sum {} + | sort) ]]
# Wi-Fi country file renamed, content kept.
[[ ! -e $r/etc/modprobe.d/pistar-wifi-country.conf && $(cat "$r/etc/modprobe.d/gospot-wifi-country.conf") == 'options cfg80211 ieee80211_regdom=GB' ]]
# Only the loaded old unit is stopped and disabled.
grep -qx 'disable --now pistar-panel.service' "$t/systemctl.log"
[[ $(grep -c '^disable' "$t/systemctl.log") == 1 ]]
[[ -e $r/var/lib/gospot/.migrated-from-pistar-dashboard ]]

# --- 2. idempotent; later edits of the new settings survive ---------------------
sed -i 's|^TGIF_API_URL=.*|TGIF_API_URL=https://edited.example.net/|' "$conf"
before=$(snapshot "$r"); : >"$t/systemctl.log"
[[ -z $("$here/gospot-migrate" "$r") ]]
[[ $(snapshot "$r") == "$before" && ! -s $t/systemctl.log ]]

# --- 3. existing gospot files are never overwritten --------------------------------
r2=$t/partial
mkdir -p "$r2/etc/pistar-wizard" "$r2/etc/gospot/wizard" "$r2/etc/modprobe.d"
cp "$here/../gospot.conf" "$r2/etc/gospot/gospot.conf"
printf 'old\n' >"$r2/etc/pistar-wizard/admin-password"
printf 'oldkey\n' >"$r2/etc/pistar-wizard/tls-key.pem"; printf 'oldcert\n' >"$r2/etc/pistar-wizard/tls-cert.pem"
printf 'new\n' >"$r2/etc/gospot/wizard/admin-password"; printf 'newcert\n' >"$r2/etc/gospot/wizard/tls-cert.pem"
printf 'a\n' >"$r2/etc/modprobe.d/pistar-wifi-country.conf"; printf 'b\n' >"$r2/etc/modprobe.d/gospot-wifi-country.conf"
out=$("$here/gospot-migrate" "$r2")
[[ $(cat "$r2/etc/gospot/wizard/admin-password") == new && ! -e $r2/etc/gospot/wizard/tls-key.pem ]]
[[ $(cat "$r2/etc/gospot/wizard/tls-cert.pem") == newcert ]]
grep -q 'kept existing TLS key pair' <<<"$out" && grep -q 'kept existing /etc/gospot/wizard/admin-password' <<<"$out"
[[ $(cat "$r2/etc/modprobe.d/pistar-wifi-country.conf") == a && $(cat "$r2/etc/modprobe.d/gospot-wifi-country.conf") == b ]]
cmp -s "$r2/etc/gospot/gospot.conf" "$here/../gospot.conf"

# --- 4. fresh install: nothing to migrate ----------------------------------------------
r3=$t/fresh
mkdir -p "$r3/etc/gospot"; cp "$here/../gospot.conf" "$r3/etc/gospot/gospot.conf"
: >"$t/systemctl.log"
[[ -z $(GOSPOT_MIGRATE_SYSTEMCTL=true "$here/gospot-migrate" "$r3") ]]
cmp -s "$r3/etc/gospot/gospot.conf" "$here/../gospot.conf"
[[ $(cd "$r3" && find . -type f | sort | tr '\n' ' ') == './etc/gospot/gospot.conf ./var/lib/gospot/.migrated-from-pistar-dashboard ' ]]
# --- 5. files of the old service accounts move to the new ones (fakeroot) -----------
if command -v fakeroot >/dev/null; then
  r4=$t/owners
  mkdir -p "$r4/etc/gospot" "$r4/etc/pistar-wizard" "$r4/var/lib/pistar-dashboard-bm"
  cp "$here/../gospot.conf" "$r4/etc/gospot/gospot.conf"
  owners=$(GOSPOT_MIGRATE_ACCOUNTS=daemon:bin GOSPOT_MIGRATE_SYSTEMCTL=true fakeroot sh -c '
    r=$1; shift
    printf x >"$r/etc/pistar-wizard/admin-password"; printf k >"$r/etc/bmapi.key"; printf k >"$r/etc/tgifapi.key"
    printf c >"$r/var/lib/pistar-dashboard-bm/cache.json"
    chown root:root "$r/etc/pistar-wizard/admin-password"; chown daemon:daemon "$r/etc/bmapi.key" "$r/var/lib/pistar-dashboard-bm" "$r/var/lib/pistar-dashboard-bm/cache.json"
    chown root:root "$r/etc/tgifapi.key"
    "$@" "$r" >/dev/null
    stat -c "%n %U:%G" "$r/etc/gospot/wizard/admin-password" "$r/etc/bmapi.key" "$r/etc/tgifapi.key" "$r/var/lib/gospot-bm" "$r/var/lib/gospot-bm/cache.json" "$r/var/lib/pistar-dashboard-bm/cache.json"
  ' sh "$r4" "$here/gospot-migrate")
  [[ $owners == "$r4/etc/gospot/wizard/admin-password root:root
$r4/etc/bmapi.key bin:bin
$r4/etc/tgifapi.key root:root
$r4/var/lib/gospot-bm bin:bin
$r4/var/lib/gospot-bm/cache.json bin:bin
$r4/var/lib/pistar-dashboard-bm/cache.json daemon:daemon" ]] || { printf '%s\n' "$owners"; exit 1; }
else
  echo "SKIP owner mapping: fakeroot is not installed"
fi
echo "PASS gospot-migrate: settings merged (old defaults -> new, local value kept, build-time/obsolete dropped), credentials/state copied with modes and times, Wi-Fi country renamed, old-account files re-owned, old unit disabled, no secret printed, idempotent, never overwrites, fresh install untouched"
