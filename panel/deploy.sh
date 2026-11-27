#!/usr/bin/env bash
set -euo pipefail

if (($#)); then
  echo "Usage: $0 (fixed target: pistar3)" >&2
  exit 2
fi
cd -- "$(dirname -- "$0")"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -o "$tmp/gospot-panel" .

cat >"$tmp/gospot-panel.service" <<'UNIT'
[Unit]
Description=GoSpot status panel
After=network.target

[Service]
ExecStart=/usr/local/bin/gospot-panel -listen :8787
User=gospot-panel
SupplementaryGroups=systemd-journal
NoNewPrivileges=yes
CapabilityBoundingSet=
PrivateDevices=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6 AF_NETLINK
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

tar -C "$tmp" -cf - gospot-panel gospot-panel.service |
  ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes pistar3 '
    set -eu
    test "$(hostname -s)" = pistar3 || { echo "Unexpected hostname" >&2; exit 1; }
    getent group systemd-journal >/dev/null
    sudo -n true
    if ! getent passwd gospot-panel >/dev/null; then
      sudo -n useradd --system --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin gospot-panel
    fi
    d=$(mktemp -d)
    trap "rm -rf -- \"$d\"" EXIT
    tar -xf - -C "$d"
    # Rename into place; a running executable cannot be overwritten in place.
    sudo -n install -m 0755 "$d/gospot-panel" /usr/local/bin/gospot-panel.new
    sudo -n mv -fT /usr/local/bin/gospot-panel.new /usr/local/bin/gospot-panel
    sudo -n install -m 0644 "$d/gospot-panel.service" /etc/systemd/system/gospot-panel.service
    sudo -n systemctl daemon-reload
    sudo -n systemctl enable gospot-panel.service
    sudo -n systemctl restart gospot-panel.service
    sudo -n systemctl is-active gospot-panel.service
  '
