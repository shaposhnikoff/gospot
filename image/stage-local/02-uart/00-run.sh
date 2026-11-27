#!/bin/bash -e
# GPIO modems (MMDVM HATs) use the Raspberry Pi UART: enable it and keep the
# kernel and getty off it, as on a classic Pi-Star image.
: "${ROOTFS_DIR:?pi-gen root filesystem required}"
# shellcheck source=/dev/null
. ../image.env
# shellcheck source=/dev/null
. "${ROOTFS_DIR}${SETTINGS_FILE}"
config=${ROOTFS_DIR}${BOOT_CONFIG_TXT}
cmdline=${ROOTFS_DIR}${BOOT_CMDLINE_TXT}
test -s "$config"
test -s "$cmdline"
if ! grep -qxF '# GPIO modem UART' "$config"; then
	printf '\n[all]\n# GPIO modem UART\nenable_uart=1\n' >>"$config"
fi
sed -Ei 's/(^|[[:space:]])console=(serial0|ttyAMA0|ttyS0)(,[^[:space:]]*)?([[:space:]]|$)/\1/g; s/[[:space:]]+/ /g; s/^ //; s/ $//' "$cmdline"
if grep -Eq '(^|[[:space:]])console=(serial0|ttyAMA0|ttyS0)' "$cmdline"; then
	echo "serial console still in ${BOOT_CMDLINE_TXT}" >&2
	exit 1
fi
on_chroot <<EOF
set -e
systemctl mask ${IMAGE_MASK_UNITS//,/ }
EOF
