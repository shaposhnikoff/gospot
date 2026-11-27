#!/usr/bin/env bash
set -euo pipefail
umask 077
if (($#)); then
  echo "Usage: $0 (fixed target: pistar3)" >&2
  exit 2
fi
cd -- "$(dirname -- "$0")"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
install -m 0755 helper/pistar-config "$tmp/pistar-config"
go run . -h >"$tmp/flags" 2>&1
for flag in admin-listen tls-cert tls-key; do
  grep -q "^  -$flag " "$tmp/flags" || { echo "Missing flag: -$flag" >&2; exit 1; }
done
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -o "$tmp/pistar-wizard" .

cat >"$tmp/pistar-wizard.service" <<'UNIT'
[Unit]
Description=Pi-Star configuration wizard
After=network.target

[Service]
User=pistar-wizard
Group=pistar-wizard
SupplementaryGroups=systemd-journal
UMask=0077
LoadCredential=admin-password:/etc/pistar-wizard/admin-password
LoadCredential=tls-key:/etc/pistar-wizard/tls-key.pem
ExecStart=/usr/local/bin/pistar-wizard -admin-listen :8788 -tls-cert /etc/pistar-wizard/tls-cert.pem -tls-key %d/tls-key
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT
cat >"$tmp/sudoers" <<'RULE'
pistar-wizard ALL=(root) NOPASSWD: /usr/local/libexec/pistar-config read, /usr/local/libexec/pistar-config apply, /usr/local/libexec/pistar-config rollback
pistar-wizard ALL=(root) NOPASSWD: /usr/local/libexec/pistar-config services-read, /usr/local/libexec/pistar-config services-apply
RULE

cat >"$tmp/install.sh" <<'INSTALL'
#!/usr/bin/env bash
set -euo pipefail
umask 077
test "$(hostname -s)" = pistar3
ip -4 -o addr show | grep -E ' inet 192\.168\.210\.194/' >/dev/null
visudo -cf sudoers
version=$(systemctl --version | awk 'NR==1 {print $2}')
(( version >= 247 )) || { echo "LoadCredential requires systemd >= 247" >&2; exit 1; }
# Validate before changing the node, including on first install.
mkdir check
sed "s|ExecStart=/usr/local/bin/|ExecStart=$PWD/|" pistar-wizard.service >check/pistar-wizard.service
systemd-analyze verify "$PWD/check/pistar-wizard.service"
if ! getent passwd pistar-wizard >/dev/null; then
  useradd --system --user-group --no-create-home --home-dir /nonexistent --shell /usr/sbin/nologin pistar-wizard
fi
test "$(getent passwd pistar-wizard | cut -d: -f7)" = /usr/sbin/nologin
test "$(id -Gn pistar-wizard)" = pistar-wizard
test "$(id -u pistar-wizard)" != 0
cfg=/etc/pistar-wizard
install -d -m 0755 -o root -g root "$cfg"
if [[ ! -d /usr/local/libexec ]]; then install -d -m 0755 /usr/local/libexec; fi
# Generate on this node only. Keep credentials and trust stable on upgrades.
if [[ ! -e $cfg/admin-password ]]; then
  openssl rand -hex 32 >admin-password
  install -m 0600 admin-password "$cfg/admin-password"
fi
test -s "$cfg/admin-password"
if [[ ! -e $cfg/tls-key.pem && ! -e $cfg/tls-cert.pem ]]; then
  openssl req -x509 -newkey rsa:3072 -nodes -sha256 -days 825 \
    -subj /CN=pistar3 -addext 'subjectAltName=DNS:pistar3,IP:192.168.210.194' \
    -keyout tls-key.pem -out tls-cert.pem
  install -m 0600 tls-key.pem "$cfg/tls-key.pem"
  install -m 0644 tls-cert.pem "$cfg/tls-cert.pem"
fi
openssl x509 -in "$cfg/tls-cert.pem" -noout -checkend 0
openssl x509 -in "$cfg/tls-cert.pem" -pubkey -noout >cert.pub
openssl pkey -in "$cfg/tls-key.pem" -pubout >key.pub
cmp cert.pub key.pub
chown root:root "$cfg"/{admin-password,tls-key.pem,tls-cert.pem}
chmod 0600 "$cfg"/{admin-password,tls-key.pem}
chmod 0644 "$cfg/tls-cert.pem"

replace() {
  local stage
  stage=$(mktemp "$(dirname "$3")/.wizard.XXXXXXXX")
  install -o root -g root -m "$1" "$2" "$stage"
  mv -fT "$stage" "$3"
}
replace 0755 pistar-config /usr/local/libexec/pistar-config
replace 0755 pistar-wizard /usr/local/bin/pistar-wizard
replace 0440 sudoers /etc/sudoers.d/pistar-wizard
replace 0644 pistar-wizard.service /etc/systemd/system/pistar-wizard.service
systemctl daemon-reload
systemctl enable pistar-wizard.service
systemctl restart pistar-wizard.service
systemctl is-active pistar-wizard.service
INSTALL

tar -C "$tmp" -cf - pistar-wizard pistar-config pistar-wizard.service sudoers install.sh |
  ssh -T -o BatchMode=yes -o StrictHostKeyChecking=yes pistar3 "sudo -n sh -ec '
    test \"\$(hostname -s)\" = pistar3
    umask 077
    d=\$(mktemp -d)
    trap \"rm -rf -- \\\"\$d\\\"\" EXIT
    cd \"\$d\"
    tar -xf -
    bash install.sh
  '"
