#!/usr/bin/env bash
# Tests of the image first boot (files/firstboot) and the dashboard
# credentials helper it shares with the package postinst
# (Pi-Star_dash_new/debian/pistar-wizard-secrets). Runs unprivileged: each
# "board" is a directory with its own pistar.conf named by GOSPOT_SETTINGS.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
firstboot=$here/../files/firstboot
image=$(cd "$here/../../.." && pwd)
shipped=$(realpath -e "$image/$(cat "$image/settings.path")")
[[ $(id -u) -ne 0 ]] || { echo "run the tests unprivileged" >&2; exit 2; }
t=$(mktemp -d); trap 'rm -rf "${t:?}"' EXIT
pass=0
ok() { pass=$((pass + 1)); echo "ok $pass - $*"; }
fail() { echo "not ok - $*" >&2; exit 1; }
value() { sed -n "s/^$1=//p" "$shipped"; }
# The helper's source in the dashboard tree, named like its installed path.
secrets=$(dirname "$shipped")/debian/$(basename "$(value WIZARD_SECRETS_HELPER)")
[[ -x $secrets ]] || fail "no credentials helper $secrets"
default_password=$(value IMAGE_DEFAULT_PASSWORD)
[[ -n $default_password ]] || fail "pistar.conf has no IMAGE_DEFAULT_PASSWORD"

# board NAME [HELPER]: a root of its own; settings are the shipped pistar.conf
# with the board-specific paths moved below the root.
board() {
	local root=$t/$1 helper=${2:-$secrets}
	mkdir -p "$root/lib"
	ln -sf "$helper" "$root/lib/secrets-helper"
	grep -Ev '^(WIZARD_CONFIG_DIR|WIZARD_SECRETS_HELPER|IMAGE_FIRSTBOOT_DONE)=' "$shipped" >"$root/settings.conf"
	printf 'WIZARD_CONFIG_DIR=%s\nWIZARD_SECRETS_HELPER=%s\nIMAGE_FIRSTBOOT_DONE=%s\n' \
		"$root/etc/wizard" "$root/lib/secrets-helper" "$root/var/lib/firstboot/done" >>"$root/settings.conf"
	echo "$root"
}
run() { GOSPOT_SETTINGS=$1/settings.conf "${@:2}"; }
fingerprint() { openssl x509 -in "$1/etc/wizard/tls-cert.pem" -noout -fingerprint -sha256; }
keyhash() { sha256sum <"$1/etc/wizard/tls-key.pem"; }
mode() { stat -c %a "$1"; }

# 1. Credentials baked into the image are replaced on first boot.
a=$(board a)
mkdir -p "$a/etc/wizard"
printf 'built-in-image\n' >"$a/etc/wizard/admin-password"
(umask 077; openssl req -x509 -nodes -newkey rsa:2048 -days 1 -subj /CN=build \
	-keyout "$a/etc/wizard/tls-key.pem" -out "$a/etc/wizard/tls-cert.pem" 2>/dev/null)
built_key=$(keyhash "$a")
run "$a" "$firstboot" >"$t/out"
[[ $(keyhash "$a") != "$built_key" ]] || fail "image TLS key kept"
openssl x509 -in "$a/etc/wizard/tls-cert.pem" -noout -subject | grep -q "CN *= *$(hostname -s)" || fail "certificate subject"
openssl x509 -in "$a/etc/wizard/tls-cert.pem" -noout -ext subjectAltName | grep -q "DNS:$(hostname -s).local" || fail "certificate SAN"
openssl x509 -in "$a/etc/wizard/tls-cert.pem" -noout -checkend 86400 >/dev/null || fail "certificate expires"
cmp -s <(openssl x509 -in "$a/etc/wizard/tls-cert.pem" -noout -pubkey) \
	<(openssl pkey -in "$a/etc/wizard/tls-key.pem" -pubout) || fail "certificate does not match the key"
ok "image TLS key and certificate replaced by a new pair for this host"
[[ $(cat "$a/etc/wizard/admin-password") == "$default_password" ]] || fail "admin password not reset"
ok "admin password reset to the IMAGE_DEFAULT_PASSWORD of pistar.conf"
[[ $(mode "$a/etc/wizard/admin-password") == 600 && $(mode "$a/etc/wizard/tls-key.pem") == 600 &&
	$(mode "$a/etc/wizard/tls-cert.pem") == 644 && $(mode "$a/etc/wizard") == 755 ]] || fail "modes"
ok "password and key 0600, certificate 0644, directory 0755"
[[ -f $a/var/lib/firstboot/done ]] || fail "marker missing"
! ls "$a/etc/wizard"/.admin-password.* >/dev/null 2>&1 || fail "temporary file left"
grep -q "firstboot: created this board" "$t/out" || fail "no log line"
! grep -q "$default_password" "$t/out" || fail "password in the log"
ok "marker written, no temporary file, log line without the password"

# 2. Later boots change nothing, even after the operator set a password.
printf 'operator-choice\n' >"$a/etc/wizard/admin-password"
key=$(keyhash "$a"); fp=$(fingerprint "$a")
run "$a" "$firstboot" >/dev/null
[[ $(keyhash "$a") == "$key" && $(fingerprint "$a") == "$fp" &&
	$(cat "$a/etc/wizard/admin-password") == operator-choice ]] || fail "second boot changed credentials"
ok "second boot keeps key, certificate and the operator's password"

# 3. Two boards flashed from one image get different keys.
b=$(board b)
run "$b" "$firstboot" >/dev/null
[[ $(keyhash "$b") != "$(keyhash "$a")" && $(fingerprint "$b") != "$(fingerprint "$a")" ]] || fail "boards share a key"
ok "two boards from the same image have different TLS keys and certificates"

# 4. A failed first boot writes no marker and is retried on the next boot.
c=$(board c "$t/failing-helper")
printf '#!/bin/sh\ncat >/dev/null\nexit 1\n' >"$t/failing-helper"; chmod 755 "$t/failing-helper"
if run "$c" "$firstboot" >/dev/null 2>&1; then fail "failure not reported"; fi
[[ ! -e $c/var/lib/firstboot/done ]] || fail "marker after failure"
ln -sf "$secrets" "$c/lib/secrets-helper"
run "$c" "$firstboot" >/dev/null
[[ -f $c/var/lib/firstboot/done && -s $c/etc/wizard/tls-key.pem ]] || fail "retry"
ok "failed first boot leaves no marker; the next boot completes"

# 5. The package postinst path: random password, existing files kept.
d=$(board d)
run "$d" "$secrets"
pw=$(cat "$d/etc/wizard/admin-password")
[[ $pw =~ ^[0-9a-f]{64}$ ]] || fail "postinst password format"
key=$(keyhash "$d")
run "$d" "$secrets"
[[ $(cat "$d/etc/wizard/admin-password") == "$pw" && $(keyhash "$d") == "$key" ]] || fail "postinst rerun changed files"
ok "postinst path: 64 random hex digits, rerun keeps password and key"

# 6. The helper refuses an empty password and leaves nothing behind.
e=$(board e)
if printf '\n' | run "$e" "$secrets" --password-stdin 2>/dev/null; then fail "empty password accepted"; fi
[[ ! -e $e/etc/wizard/admin-password ]] && ! ls "$e/etc/wizard"/.admin-password.* >/dev/null 2>&1 || fail "empty password left files"
if run "$e" "$secrets" --bogus 2>/dev/null; then fail "unknown option accepted"; fi
ok "empty password and unknown options refused without files"

# 7. A certificate without a key (or the reverse) is an error, not overwritten.
f=$(board f)
mkdir -p "$f/etc/wizard"; printf 'x\n' >"$f/etc/wizard/tls-cert.pem"
if run "$f" "$secrets" 2>/dev/null; then fail "half key pair accepted"; fi
[[ $(cat "$f/etc/wizard/tls-cert.pem") == x ]] || fail "half key pair overwritten"
ok "a half TLS key pair is reported, not replaced"

echo "PASS firstboot: $pass checks"
