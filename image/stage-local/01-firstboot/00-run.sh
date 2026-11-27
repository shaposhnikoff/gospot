#!/bin/bash -e
# Install the first-boot service: it runs once per board before the HTTPS
# management service and creates that board's credentials (files/firstboot).
: "${ROOTFS_DIR:?pi-gen root filesystem required}"
# shellcheck source=/dev/null
. ../image.env
# shellcheck source=/dev/null
. "${ROOTFS_DIR}${SETTINGS_FILE}"
units=${ROOTFS_DIR}/etc/systemd/system
install -d -m 0755 "$(dirname "${ROOTFS_DIR}${IMAGE_FIRSTBOOT_SCRIPT}")"
sed "s|@SETTINGS_FILE@|${SETTINGS_FILE}|" files/firstboot >"${ROOTFS_DIR}${IMAGE_FIRSTBOOT_SCRIPT}"
chmod 0755 "${ROOTFS_DIR}${IMAGE_FIRSTBOOT_SCRIPT}"
if grep -q @SETTINGS_FILE@ "${ROOTFS_DIR}${IMAGE_FIRSTBOOT_SCRIPT}"; then echo "unresolved settings path" >&2; exit 1; fi
dropin=${units}/${UNIT_WIZARD}.d/10-${IMAGE_FIRSTBOOT_UNIT%.service}.conf
cat >"${units}/${IMAGE_FIRSTBOOT_UNIT}" <<EOF
[Unit]
Description=First boot: this board's management TLS key and admin password
After=network-online.target
Wants=network-online.target
Before=${UNIT_WIZARD}
ConditionPathExists=!${IMAGE_FIRSTBOOT_DONE}

[Service]
Type=oneshot
ExecStart=${IMAGE_FIRSTBOOT_SCRIPT}
RemainAfterExit=yes
TimeoutStartSec=5min

[Install]
WantedBy=multi-user.target
EOF
chmod 0644 "${units}/${IMAGE_FIRSTBOOT_UNIT}"
# The management service loads the credentials the first boot creates.
install -d -m 0755 "${units}/${UNIT_WIZARD}.d"
cat >"$dropin" <<EOF
[Unit]
Requires=${IMAGE_FIRSTBOOT_UNIT}
After=${IMAGE_FIRSTBOOT_UNIT}
EOF
chmod 0644 "$dropin"
on_chroot <<EOF
set -e
systemctl enable ${IMAGE_FIRSTBOOT_UNIT}
EOF
for path in "${IMAGE_FIRSTBOOT_DONE}" "${WIZARD_CONFIG_DIR}/admin-password" "${WIZARD_CONFIG_DIR}/tls-key.pem" "${WIZARD_CONFIG_DIR}/tls-cert.pem"; do
	if [ -e "${ROOTFS_DIR}${path}" ] || [ -L "${ROOTFS_DIR}${path}" ]; then
		echo "board-specific file in the image: ${path}" >&2
		exit 1
	fi
done
