#!/bin/bash -e
# Install the two local packages (radio and dashboard) with apt inside the
# target, so their dependencies come from the Raspberry Pi OS archive.
# image/build.sh puts exactly these two .deb files into files/ and the
# installed path of the settings file into ../image.env.
: "${ROOTFS_DIR:?pi-gen root filesystem required}"
# shellcheck source=/dev/null
. ../image.env
shopt -s nullglob
debs=(files/*.deb)
[ ${#debs[@]} -eq 2 ] || { echo "expected two local packages in files/" >&2; exit 1; }
stage=/var/tmp/local-debs
install -d -m 0755 "${ROOTFS_DIR}${stage}"
list=()
for deb in "${debs[@]}"; do
	install -m 0644 "$deb" "${ROOTFS_DIR}${stage}/"
	list+=("${stage}/$(basename "$deb")")
done

on_chroot <<EOF
set -e
apt-get update
DEBIAN_FRONTEND=noninteractive apt-get -o APT::Acquire::Retries=3 install -y --no-install-recommends ${list[*]}
apt-get clean
EOF
rm -rf "${ROOTFS_DIR}${stage}"

# Every other path and unit name comes from the installed settings file,
# which is the repository copy (image/settings.path).
cmp -s ../settings.conf "${ROOTFS_DIR}${SETTINGS_FILE}" || { echo "installed ${SETTINGS_FILE} differs from the repository copy" >&2; exit 1; }
# shellcheck source=/dev/null
. "${ROOTFS_DIR}${SETTINGS_FILE}"

# The package scripts skip systemctl without a running systemd: enable the
# dashboard services and the host-file timer here. The radio units stay
# disabled; the operator enables the modes in use.
on_chroot <<EOF
set -e
systemctl enable ${IMAGE_ENABLE_UNITS//,/ }
EOF

# The dashboard postinst generated management credentials for the build
# container. Each board creates its own on first boot (01-firstboot).
for name in admin-password tls-key.pem tls-cert.pem; do
	rm -f "${ROOTFS_DIR}${WIZARD_CONFIG_DIR}/${name}"
done
