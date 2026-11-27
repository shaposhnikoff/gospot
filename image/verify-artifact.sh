#!/usr/bin/env bash
# verify-artifact.sh IMAGE RADIO_DEB DASHBOARD_DEB: check a built image
# offline. The root and boot partitions are attached read-only through a loop
# device (sudo) and inspected from the host; visudo, dpkg and ldd run inside
# the image (qemu-user via binfmt_misc). Every path and name comes from the
# settings file named in image/settings.path or from the two packages.
set -euo pipefail
[[ $# == 3 && -f $1 && -f $2 && -f $3 ]] || { echo "Usage: $0 IMAGE.img[.xz] RADIO_DEB DASHBOARD_DEB" >&2; exit 2; }
img=$(realpath -e "$1") radio_deb=$(realpath -e "$2") dashboard_deb=$(realpath -e "$3")
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
repo_conf=$(realpath -e "$here/$(cat "$here/settings.path")")
# shellcheck source=/dev/null
source "$repo_conf"
# The installed settings file and sudoers files of the dashboard package.
settings_file=$(dpkg-deb --ctrl-tarfile "$dashboard_deb" | tar -xO ./conffiles | grep -x "/.*/$(basename "$repo_conf")")
mapfile -t sudoers_files < <(dpkg-deb -c "$dashboard_deb" | awk '{ print $NF }' | sed -n 's|^\./etc/sudoers\.d/\([^/]\{1,\}\)$|/etc/sudoers.d/\1|p')
# Services started by timers the image enables are not radio units.
IFS=, read -ra enable_units <<<"$IMAGE_ENABLE_UNITS"
timer_services=$(printf '%s\n' "${enable_units[@]}" | sed -n 's/\.timer$/.service/p')

tmp=$(mktemp -d "${BUILD_PARENT:-$IMAGE_BUILD_PARENT}/$IMAGE_NAME-verify.XXXXXXXX")
mnt=$tmp/root loop=
cleanup() {
	set +e
	if mountpoint -q "$mnt/boot/firmware"; then sudo umount "$mnt/boot/firmware"; fi
	if mountpoint -q "$mnt"; then sudo umount "$mnt"; fi
	if [[ -n $loop ]]; then sudo losetup -d "$loop"; fi
	rm -rf -- "$tmp"
}
trap cleanup EXIT
case $img in
	*.xz) xz -dc -- "$img" >"$tmp/disk" ;;
	*) cp --sparse=always -- "$img" "$tmp/disk" ;;
esac
loop=$(sudo losetup --find --show --read-only --partscan "$tmp/disk")
for _ in 1 2 3 4 5 6 7 8 9 10; do [[ -b ${loop}p2 ]] && break; sleep 0.5; done
mkdir -p "$mnt"
sudo mount -o ro,noload "${loop}p2" "$mnt"
sudo mount -o ro "${loop}p1" "$mnt/boot/firmware"
in_image() { sudo chroot "$mnt" "$@"; }

failures=0 checks=0
check() { # check DESCRIPTION COMMAND...: PASS when the command succeeds
	local what=$1; shift
	checks=$((checks + 1))
	if "$@"; then echo "PASS $what"; else echo "FAIL $what"; failures=$((failures + 1)); fi
}
installed_version() { dpkg-query --admindir="$mnt/var/lib/dpkg" -W -f='${db:Status-Abbrev}${Version}' "$1" 2>/dev/null; }
package_is() { [[ $(installed_version "$1") == "ii $2" ]]; }
files_match() { (cd "$mnt" && sudo md5sum --quiet -c "var/lib/dpkg/info/$1.md5sums"); }
trusted_root_file() { [[ $(stat -c %u "$1") == 0 ]] && (( (0$(stat -c %a "$1") & 022) == 0 )); }
first_user() { # uid 1000 and a password hash (not locked)
	[[ $(awk -F: -v u="$1" '$1 == u { print $3 }' "$mnt/etc/passwd") == 1000 ]] &&
		sudo awk -F: -v u="$1" '$1 == u && $2 ~ /^[$]/ { ok = 1 } END { exit !ok }' "$mnt/etc/shadow"
}
no_user_rename() { ! systemctl --root="$mnt" is-enabled userconfig.service >/dev/null 2>&1 && [[ ! -e $mnt/etc/xdg/autostart/piwiz.desktop ]]; }
no_serial_console() { ! grep -Eq '(^| )console=(serial0|ttyAMA0|ttyS0)' "$mnt$BOOT_CMDLINE_TXT"; }
enabled() { [[ $(systemctl --root="$mnt" is-enabled "$1" 2>/dev/null) == enabled ]]; }
masked() { [[ $(systemctl --root="$mnt" is-enabled "$1" 2>/dev/null) == masked ]]; }
not_enabled() { # no unit file state that starts it at boot, and no wants link
	local state; state=$(systemctl --root="$mnt" is-enabled "$1" 2>/dev/null || true)
	[[ $state == disabled || $state == static ]] && [[ -z $(find "$mnt/etc/systemd/system" -name "$1" -type l 2>/dev/null) ]]
}
absent() { local p; for p; do [[ ! -e $mnt$p && ! -L $mnt$p ]] || { echo "  present: $p"; return 1; }; done; }
none() { [[ -z $1 ]] || { printf '  %s\n' $1; return 1; }; }

echo "Image: $img"
echo "Image SHA-256: $(sha256sum <"$img" | cut -d' ' -f1)"

# Packages, at exactly the built versions, with unmodified files.
for deb in "$radio_deb" "$dashboard_deb"; do
	name=$(dpkg-deb -f "$deb" Package) version=$(dpkg-deb -f "$deb" Version)
	check "$name $version installed" package_is "$name" "$version"
	check "$name files match its md5sums" files_match "$name"
done

# Units: dashboard services, data timer and first boot enabled; radio units off.
for unit in "${enable_units[@]}" "$IMAGE_FIRSTBOOT_UNIT" ssh.service; do
	check "$unit enabled" enabled "$unit"
done
mapfile -t radio_units < <(dpkg-deb -c "$radio_deb" | awk '{ print $NF }' | grep -E "^\.$SYSTEMD_UNIT_DIR/[^/]+\.service$" | xargs -n1 basename | grep -vxF "${timer_services:-/}")
check "$(dpkg-deb -f "$radio_deb" Package) ships radio units (${#radio_units[@]})" test "${#radio_units[@]}" -ge 20
for unit in "${radio_units[@]}"; do check "radio unit $unit not enabled" not_enabled "$unit"; done
IFS=, read -ra mask_units <<<"$IMAGE_MASK_UNITS"
for unit in "${mask_units[@]}"; do check "$unit masked (modem UART)" masked "$unit"; done

# No PHP, nginx or PHP-FPM: packages, units, files, the classic docroot.
check "no PHP/web server packages" none "$(dpkg-query --admindir="$mnt/var/lib/dpkg" -W -f='${db:Status-Abbrev} ${Package}\n' | awk '$1 ~ /^.i/ { print $2 }' | grep -E '^(php|libapache2-mod-php|nginx|apache2|lighttpd)' || true)"
check "no PHP, nginx or php-fpm files" none "$(sudo find "$mnt" -xdev \( -name '*.php' -o -name 'php-fpm*' -o -name 'php*-fpm.service' -o -name nginx -o -name 'nginx.service' -o -path "$mnt/etc/php" \) -print 2>/dev/null | sed "s|^$mnt||")"
check "no classic dashboard docroot" absent /var/www/dashboard

# No board-specific secrets in the image.
check "no management credentials (created on first boot)" absent "$WIZARD_CONFIG_DIR/admin-password" "$WIZARD_CONFIG_DIR/tls-key.pem" "$WIZARD_CONFIG_DIR/tls-cert.pem"
check "first boot not yet run (no $IMAGE_FIRSTBOOT_DONE)" absent "$IMAGE_FIRSTBOOT_DONE"
check "no SSH host keys (generated on first boot)" none "$(cd "$mnt" && ls etc/ssh/ssh_host_* 2>/dev/null || true)"
check "no private keys under /etc, /home, /root, /var/lib" none "$(sudo grep -rlE -- '-----BEGIN ([A-Z]+ )?PRIVATE KEY-----' "$mnt/etc" "$mnt/home" "$mnt/root" "$mnt/var/lib" 2>/dev/null | sed "s|^$mnt||" || true)"
if [[ -n $IMAGE_BUILD_RASPBIAN_MIRROR ]]; then
	check "apt sources without the build mirror" none "$(grep -rlF "$IMAGE_BUILD_RASPBIAN_MIRROR" "$mnt/etc/apt" 2>/dev/null | sed "s|^$mnt||" || true)"
fi
check "machine-id uninitialised" grep -qx uninitialized "$mnt/etc/machine-id"

# Settings: the packaged conffile is the repository copy, and trusted.
check "$settings_file identical to the repository copy" cmp -s "$mnt$settings_file" "$repo_conf"
check "$settings_file root-owned and not group/world-writable" trusted_root_file "$mnt$settings_file"
check "sudoers parse (visudo -c in the image)" in_image /usr/sbin/visudo -cq
check "the dashboard ships sudoers files (${#sudoers_files[@]})" test "${#sudoers_files[@]}" -ge 1
for f in "${sudoers_files[@]}"; do check "$f parses" in_image /usr/sbin/visudo -cqf "$f"; done

# First boot wiring.
unit=$mnt/etc/systemd/system/$IMAGE_FIRSTBOOT_UNIT
dropin=$mnt/etc/systemd/system/$UNIT_WIZARD.d/10-${IMAGE_FIRSTBOOT_UNIT%.service}.conf
check "first boot unit runs $IMAGE_FIRSTBOOT_SCRIPT once, before $UNIT_WIZARD" \
	grep -qxF "ExecStart=$IMAGE_FIRSTBOOT_SCRIPT" "$unit"
check "first boot condition on its marker" grep -qxF "ConditionPathExists=!$IMAGE_FIRSTBOOT_DONE" "$unit"
check "$UNIT_WIZARD requires and follows the first boot" grep -qxF "Requires=$IMAGE_FIRSTBOOT_UNIT" "$dropin"
check "first boot script installed and executable" test -x "$mnt$IMAGE_FIRSTBOOT_SCRIPT"
check "first boot script reads $settings_file" grep -qxF "conf=$settings_file" "$mnt$IMAGE_FIRSTBOOT_SCRIPT"
check "credentials helper $WIZARD_SECRETS_HELPER of the dashboard package present" test -x "$mnt$WIZARD_SECRETS_HELPER"

# Image identity: host name, first user, SSH, no user rename.
check "hostname $IMAGE_HOSTNAME" grep -qx "$IMAGE_HOSTNAME" "$mnt$HOSTNAME_FILE"
check "first user $GOSPOT_LINUX_USER (uid 1000) with a password" first_user "$GOSPOT_LINUX_USER"
check "first boot user rename disabled" no_user_rename
check "SSH host keys regenerated on first boot (regenerate_ssh_host_keys or cloud-init)" bash -c \
	'systemctl --root="$1" is-enabled regenerate_ssh_host_keys.service >/dev/null 2>&1 || systemctl --root="$1" is-enabled cloud-init-main.service >/dev/null 2>&1 || systemctl --root="$1" is-enabled cloud-init.service >/dev/null 2>&1' _ "$mnt"

# System layout (Pi-Star compatible).
for key in GOSPOT_BIN_DIR GOSPOT_SBIN_DIR GOSPOT_HOSTS_DIR GOSPOT_LOG_DIR DSTAR_IRCDDB_DATA_DIR DSTAR_REPEATER_DATA_DIR \
	RADIO_SHARE_DIR DASHBOARD_LIB_DIR DASHBOARD_UI_DIR WIZARD_CONFIG_DIR; do
	check "$key ${!key} is a directory" test -d "$mnt${!key}"
done
check "$GOSPOT_LOG_DIR mode 775" bash -c '[[ $(stat -c %a "$1") == *775 ]]' _ "$mnt$GOSPOT_LOG_DIR"
check "$GOSPOT_RELEASE_FILE present" test -s "$mnt$GOSPOT_RELEASE_FILE"
mapfile -t seeded < <(awk '{ print $2 }' "$mnt$RADIO_SHARE_DIR/config-seed")
missing=
for target in "${seeded[@]}"; do [[ -s $mnt$target ]] || missing+=" $target"; done
check "radio configurations seeded from config_clean.zip (${#seeded[@]})" none "$missing"
mapfile -t radio_bins < <(dpkg-deb -c "$radio_deb" | awk '$1 ~ /^-/ { print $NF }' | sed -n "s|^\\.\\($GOSPOT_BIN_DIR/[^/]*\\)$|\\1|p")
notexec=
for bin in "${radio_bins[@]}"; do [[ -x $mnt$bin ]] || notexec+=" $bin"; done
check "the radio package's programs in $GOSPOT_BIN_DIR are executable (${#radio_bins[@]})" none "$notexec"
unresolved=$(for f in "$mnt$GOSPOT_BIN_DIR"/*; do
	[[ -f $f && $(head -c4 "$f" | od -An -tx1 | tr -d ' ') == 7f454c46 ]] || continue
	in_image /usr/bin/ldd "${f#"$mnt"}" 2>&1 | grep -q 'not found' && echo "${f#"$mnt"}"
done; true)
check "all ELF programs in $GOSPOT_BIN_DIR resolve their libraries" none "$unresolved"

# Modem UART.
check "enable_uart=1 in $BOOT_CONFIG_TXT" grep -qx enable_uart=1 "$mnt$BOOT_CONFIG_TXT"
check "no serial console in $BOOT_CMDLINE_TXT" no_serial_console

echo "$((checks - failures))/$checks checks passed"
(( failures == 0 ))
