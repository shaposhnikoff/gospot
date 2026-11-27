#!/usr/bin/env bash
# Loopback-only real Svelte -> Go -> sudo -> shipped helpers -> test INIs.
set -euo pipefail
umask 077
cd "$(dirname "$0")/../.."
here=$PWD
fixture=$here/qa/real-write
template=$here/../image/stage-mmdvm/00-install/files
export DOCKER_HOST=unix:///var/run/docker.sock
unset DOCKER_CONTEXT
dist=
scenario=()
preview=false
wifi=false
if [[ ${1:-} == --preview ]]; then preview=true; shift; fi
if [[ ${1:-} == --services ]]; then scenario+=(--services); shift; fi
if [[ ${1:-} == --expert ]]; then scenario+=(--expert); shift; fi
if [[ ${1:-} == --backup ]]; then scenario+=(--backup); shift; fi
if [[ ${1:-} == --configure ]]; then scenario+=(--configure); shift; fi
if [[ ${1:-} == --css ]]; then scenario+=(--css); shift; fi
if [[ ${1:-} == --factory ]]; then scenario+=(--factory); shift; fi
if [[ ${1:-} == --wifi ]]; then scenario+=(--wifi); wifi=true; shift; fi
if [[ ${1:-} == --calibration ]]; then scenario+=(--calibration); shift; fi
if [[ ${1:-} == --jitter ]]; then scenario+=(--jitter); shift; fi
# --dstar: fixture logs/configs and a fake remotecontrold are installed by the scenario itself.
if [[ ${1:-} == --dstar ]]; then scenario+=(--dstar); shift; fi
tgif=false
if [[ ${1:-} == --tgif ]]; then scenario+=(--tgif); tgif=true; shift; fi
hoseline=false
if [[ ${1:-} == --hoseline ]]; then scenario+=(--hoseline); hoseline=true; shift; fi
if (($#)); then
  [[ $# == 2 && $1 == --dist ]] || { echo "Usage: $0 [--dist build/release-.../ui]" >&2; exit 2; }
  dist=$(realpath -e "$2")
  [[ $dist == "$here"/build/release-*/ui && -f $dist/index.html && -d $dist/assets ]] || exit 2
fi
base=/tmp/opencode
[[ -d $base ]] || base=/tmp
work=$(mktemp -d "$base/write.XXXXXXXX")
name=pstar-write-qa-$(openssl rand -hex 5)
bridge=
relay=
gateway=
cleanup() {
  [[ -z $gateway ]] || kill "$gateway" 2>/dev/null || :
  [[ -z $relay ]] || kill "$relay" 2>/dev/null || :
  [[ -z $bridge ]] || kill "$bridge" 2>/dev/null || :
  docker rm -f "$name" >/dev/null 2>&1 || :
  docker network rm "$name" >/dev/null 2>&1 || :
  for role in mmdvmhost dmrgateway; do
    systemctl --user stop "$name-$role.service" >/dev/null 2>&1 || :
    systemctl --user reset-failed "$name-$role.service" >/dev/null 2>&1 || :
  done
  rm -rf -- "$work"
}
trap cleanup EXIT
systemctl --user show -p Version
docker build -q -t pistar-write-qa-runtime:bookworm - < "$fixture/Dockerfile"
if [[ -z $dist ]]; then
  dist=$work/dist
  npm run build -- --outDir "$dist"
fi
CGO_ENABLED=0 go -C ../panel build -trimpath -o "$work/server" .
mkdir "$work/bridge"
for role in dmrgateway mmdvmhost; do
  systemd-run --user --quiet --unit="$name-$role" --property=Type=oneshot --property=RemainAfterExit=yes --property=StartLimitIntervalSec=0 \
    /bin/sh -c 'if test -f "$1"; then exit 42; fi' sh "$work/fault-$role"
done
python3 "$fixture/bridge.py" "$work/bridge" "$name" >"$work/service-results" 2>&1 &
bridge=$!
sed -e 's/^Callsign=NOCALL$/Callsign=DEMO1/' -e 's/^Id=1$/Id=10001/' "$template/MMDVM-Host.ini" >"$work/MMDVM-Host.ini"
sed -e 's/^Address=master.invalid$/Address=192.0.2.11/' -e 's/^Id=1$/Id=10001/' \
    -e 's/^Password=$/Password=test-only/' "$template/DMRGateway.ini" >"$work/DMRGateway.ini"
openssl rand -hex 20 >"$work/admin-password"
openssl req -x509 -nodes -newkey rsa:2048 -days 1 -keyout "$work/key.pem" -out "$work/cert.pem" \
  -subj /CN=127.0.0.1 -addext 'subjectAltName=IP:127.0.0.1' 2>/dev/null
sed 's|@DASHBOARD_LIB_DIR@|/usr/local/libexec|g' debian/sudoers >"$work/sudoers"
# The fixture keeps its own layout; only its copy of pistar.conf differs.
# --wifi: the Wi-Fi page edits a wpa_supplicant.conf fixture (no NetworkManager here).
stand_conf=()
if $wifi; then stand_conf=(-e 's|^WIFI_BACKEND=.*|WIFI_BACKEND=wpa_supplicant|'); fi
sed "${stand_conf[@]}" -e 's|^CONF_MMDVMHOST=.*|CONF_MMDVMHOST=/etc/MMDVM-Host.ini|' -e 's|^CONF_DMRGATEWAY=.*|CONF_DMRGATEWAY=/etc/DMRGateway.ini|' \
    -e 's|^DASHBOARD_LIB_DIR=.*|DASHBOARD_LIB_DIR=/usr/local/libexec|' -e 's|^DASHBOARD_UI_DIR=.*|DASHBOARD_UI_DIR=/usr/local/share/pistar-native-ui|' \
    pistar.conf >"$work/pistar.conf"
if $tgif; then
  # The container has no internet: only the stand's pistar.conf points TGIF at
  # fake_tgif.py inside it (HTTPS with the stand certificate, legacy over HTTP).
  # DMRGateway network 1 becomes the TGIF master with a 9-digit ID.
  sed -i -e 's|^TGIF_API_URL=.*|TGIF_API_URL=https://127.0.0.1:8601/v1/|' \
         -e 's|^TGIF_SESSION_URL=.*|TGIF_SESSION_URL=http://127.0.0.1:8602/api/sessions/update/|' "$work/pistar.conf"
  sed -i -e 's/^Address=192\.0\.2\.11$/Address=tgif.network/' -e 's/^Id=10001$/Id=234123401/' "$work/DMRGateway.ini"
  grep -qx 'Address=tgif.network' "$work/DMRGateway.ini" && grep -qx 'Id=234123401' "$work/DMRGateway.ini"
fi
if $hoseline; then
  # The container has no internet: only the stand's pistar.conf points the
  # Hoseline page and spotter at fake_hoseline.py inside it (HTTPS/WSS with the
  # stand certificate).
  sed -i -e 's|^HOSELINE_PAGE_URL=.*|HOSELINE_PAGE_URL=https://127.0.0.1:8603/|' \
         -e 's|^HOSELINE_SPOTTER_URL=.*|HOSELINE_SPOTTER_URL=wss://127.0.0.1:8603/spotter/|' "$work/pistar.conf"
  grep -qx 'HOSELINE_SPOTTER_URL=wss://127.0.0.1:8603/spotter/' "$work/pistar.conf"
fi
test -s "$work/sudoers"
docker network create --internal "$name" >/dev/null
docker run -d --name "$name" --network "$name" \
  --mount type=bind,source="$work/bridge",target=/qa-bridge,readonly \
  --pids-limit=120 pistar-write-qa-runtime:bookworm sleep infinity >/dev/null
docker exec "$name" mkdir -p /usr/local/libexec /usr/local/share/pistar-native-ui /run/credentials /run/pistar-qa /etc/pistar-dashboard
docker cp "$work/pistar.conf" "$name:/etc/pistar-dashboard/pistar.conf"
docker cp "$dist/." "$name:/usr/local/share/pistar-native-ui/"
(cd "$dist" && sha256sum index.html assets/*) >"$work/ui.sha256"
echo "Admin UI source: $dist; verifying container files against source hashes"
docker exec -i -w /usr/local/share/pistar-native-ui "$name" sha256sum -c - <"$work/ui.sha256"
for f in MMDVM-Host.ini DMRGateway.ini; do docker cp "$work/$f" "$name:/etc/$f"; done
# Pi-Star default configurations for Factory Reset, where pistar-radio installs them.
radio_share=$(sed -n 's/^RADIO_SHARE_DIR=//p' pistar.conf)
bins=$(sed -n 's/^PISTAR_BINARIES_BIN_DIR=//p' pistar.conf)
docker exec "$name" mkdir -p "$radio_share"
docker cp "$bins/config_clean.zip" "$name:$radio_share/config_clean.zip"
for h in pistar-config pistar-configure pistar-calibrate pistar-jitter; do
  docker cp "../panel/helper/$h" "$name:/usr/local/libexec/$h"
  test "$(sha256sum "../panel/helper/$h"|cut -d' ' -f1)" = "$(docker exec "$name" sha256sum "/usr/local/libexec/$h"|cut -d' ' -f1)"
done
docker cp "$work/server" "$name:/usr/local/bin/pistar-wizard"
docker cp "$fixture/systemctl-client" "$name:/usr/bin/systemctl"
docker cp "$work/sudoers" "$name:/etc/sudoers.d/pistar-wizard"
for f in admin-password key.pem cert.pem; do docker cp "$work/$f" "$name:/run/credentials/$f"; done
docker exec "$name" sh -eu -c '
  chmod 755 /usr/local/bin/pistar-wizard /usr/bin/systemctl /usr/local/libexec/pistar-*
  chmod -R a+rX /usr/local/share/pistar-native-ui
  chown mmdvm:mmdvm /etc/DMRGateway.ini /etc/MMDVM-Host.ini
  chmod 600 /etc/DMRGateway.ini; chmod 640 /etc/MMDVM-Host.ini
  chown -R pistar-wizard:pistar-wizard /run/credentials; chmod -R go-rwx /run/credentials
  chown root:root /etc/sudoers.d/pistar-wizard /usr/local/libexec/pistar-* /usr/bin/systemctl /etc/pistar-dashboard/pistar.conf
  chmod 644 /etc/pistar-dashboard/pistar.conf
  chmod 0440 /etc/sudoers.d/pistar-wizard; visudo -c
  touch /run/pistar-qa/events
'
wizard_env=()
if $tgif; then
  docker cp "$fixture/fake_tgif.py" "$name:/usr/local/libexec/fake_tgif.py"
  docker exec "$name" sh -eu -c '
    printf "[Pi-Star]\nVersion=4.3.9\n" >/etc/pistar-release
    # Pi-Star keeps the radio configs world-readable (classic restore writes 0644);
    # the admin page reads them to find the TGIF master and ID.
    chmod 644 /etc/DMRGateway.ini /etc/MMDVM-Host.ini
  '
  docker exec -d "$name" python3 -I /usr/local/libexec/fake_tgif.py --settings /etc/pistar-dashboard/pistar.conf \
    --cert /run/credentials/cert.pem --key /run/credentials/key.pem --log /run/pistar-qa/tgif-requests --delay 1.5
  # Trust the stand certificate for the fake HTTPS API only in this stand.
  wizard_env=(-e SSL_CERT_FILE=/run/credentials/cert.pem)
fi
if $hoseline; then
  docker cp "$fixture/fake_hoseline.py" "$name:/usr/local/libexec/fake_hoseline.py"
  docker exec -d "$name" python3 -I /usr/local/libexec/fake_hoseline.py --settings /etc/pistar-dashboard/pistar.conf \
    --cert /run/credentials/cert.pem --key /run/credentials/key.pem --log /run/pistar-qa/hoseline-events
  # Trust the stand certificate for the fake page and spotter only in this stand.
  wizard_env=(-e SSL_CERT_FILE=/run/credentials/cert.pem)
fi
# The status panel feeds /manage/api/dashboard (theme, versions); it listens on PANEL_PORT from pistar.conf.
docker exec -d --user pistar-wizard "$name" sh -c 'exec /usr/local/bin/pistar-wizard 2>>/tmp/pistar-panel.log'
docker exec -d --user pistar-wizard -e CREDENTIALS_DIRECTORY=/run/credentials ${wizard_env[@]+"${wizard_env[@]}"} "$name" \
  sh -c 'exec /usr/local/bin/pistar-wizard -admin-listen :8443 -tls-cert /run/credentials/cert.pem -tls-key /run/credentials/key.pem 2>>/tmp/pistar-wizard.log'
ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$name")
[[ $ip =~ ^([0-9]+\.){3}[0-9]+$ ]]
# An internal-only Docker network deliberately has no published ports. Relay
# raw TLS bytes on loopback; TLS/auth still terminate at the real Go server.
socat -d -d TCP4-LISTEN:0,bind=127.0.0.1,fork "TCP4:$ip:8443" 2>"$work/relay.log" &
relay=$!
for ((i=0;i<100;i++)); do
  port=$(sed -nE 's/.*listening on .*127\.0\.0\.1:([0-9]+).*/\1/p' "$work/relay.log" | head -1)
  [[ -z $port ]] || break
  sleep .02
done
[[ $port =~ ^[0-9]+$ ]] || { cat "$work/relay.log"; exit 1; }
origin=https://127.0.0.1:$port
curl --noproxy '*' --retry 5 --retry-connrefused --retry-delay 1 --max-time 8 -sS --cacert "$work/cert.pem" \
  -o /dev/null -w 'HTTPS /manage/ without credentials: %{http_code}\n' "$origin/manage/"
if $preview; then
  go build -o "$work/preview-gateway" "$fixture/preview.go"
  "$work/preview-gateway" -listen "127.0.0.1:${PREVIEW_PORT:-8790}" -upstream "$origin" -assets "$dist" -credentials "$work" &
  gateway=$!
  echo "Preview: http://127.0.0.1:${PREVIEW_PORT:-8790}/#console"
  echo "Isolated fixture: $name; state: $work; no hardware access"
  wait "$gateway"
  exit $?
fi
python3 qa/real_write_browser.py "${scenario[@]}" --origin "$origin" --work "$work" --key "$work/key.pem" --container "$name" || {
  cat "$work/service-results"; echo "wizard log:"; docker exec "$name" tail -n 20 /tmp/pistar-wizard.log; exit 1;
}
cat "$work/service-results"
for role in dmrgateway mmdvmhost; do
  systemctl --user is-active "$name-$role.service"
done
