#!/usr/bin/env bash
set -euo pipefail
qa=$(cd -- "$(dirname -- "$0")" && pwd -P)
project=$(realpath "$qa/../..")
[[ $# -eq 1 || ( $# -eq 2 && $2 =~ ^--(operator-browser|catalog|pocsag|dstar-network|dstar-manager|ysf-manager|p25-manager|nxdn-manager|m17-manager|bm-manager|bm-static-simplex|bm-drop-dynamic(-simplex)?|bm-drop-qso(-(503|simplex))?|tgif-static|tgif-manager|tgif-dynamic|auto-ap)$ ) ]] || { echo "Usage: bash $qa/run.sh build/release-.../docroot [--operator-browser|--catalog|--pocsag|--dstar-network|--dstar-manager|--ysf-manager|--p25-manager|--nxdn-manager|--m17-manager|--bm-manager|--bm-static-simplex|--bm-drop-dynamic|--bm-drop-dynamic-simplex|--bm-drop-qso|--bm-drop-qso-503|--bm-drop-qso-simplex|--tgif-static|--tgif-manager|--tgif-dynamic|--auto-ap]" >&2; exit 2; }
bm_kind=static
if [[ ${2:-} == --bm-drop-* || ${2:-} == --bm-static-simplex ]]; then
  bm_kind=${2#--bm-}
  set -- "$1" --bm-manager
fi
remote_manager=
if [[ ${2:-} =~ ^--(ysf|p25|nxdn|m17)-manager$ ]]; then remote_manager=${BASH_REMATCH[1]}; fi
tgif_kind=static
if [[ ${2:-} == --tgif-dynamic ]]; then
  tgif_kind=dynamic
  set -- "$1" --tgif-manager
fi
root=$(realpath -e "$1")
[[ $root == "$project"/build/release-*/docroot && -f $root/operator/index.html ]] || {
  echo "Expected the local docroot printed by npm run package:stock" >&2; exit 2;
}
for source in api/last_heard.php mmdvmhost/functions.php mmdvmhost/tools.php; do
  cmp "$project/../Pi-Star_DV_Dash/$source" "$root/$source"
done
echo "Verified packaged Last Heard endpoint and MMDVM parser match upstream"
image=php:8.2-cli@sha256:080193293786a2d9442806eca60635516120a30309b694c4292a56a4f8e2d353
# Never inherit a remote context or issue build/deploy commands.
docker=(docker --host unix:///var/run/docker.sock)
network=none
rc=0
modes=(mmdvmhost dstarrepeater)
if [[ -n $remote_manager || ${2:-} =~ ^--(pocsag|dstar-network|dstar-manager|bm-manager|tgif-static|tgif-manager)$ ]]; then
  modes=(mmdvmhost)
  tmp=$(mktemp -d); trap 'rm -rf -- "$tmp"' EXIT
  if [[ $2 == --pocsag ]]; then
    sed '/^\[POCSAG\]$/{n;s/^Enable=0$/Enable=1\nFrequency=439987500/;}' "$qa/fixtures/mmdvmhost" >"$tmp/mmdvmhost"
    printf '\n[POCSAG Network]\nEnable=1\n' >>"$tmp/mmdvmhost"
  elif [[ $2 == --ysf-manager ]]; then
    sed '/^\[System Fusion\( Network\)\?\]$/{n;s/^Enable=0$/Enable=1/;}' "$qa/fixtures/mmdvmhost" >"$tmp/mmdvmhost"
    sed '/^\[Remote Commands\]$/,/^$/s/^Port=6073$/Port=16073/' "$project/qa/classic-write/configure/etc/ysfgateway" >"$tmp/ysfgateway"
    printf '54321;QA-YSF;Fixture reflector;127.0.0.1;42000;001\n' >"$tmp/YSFHosts.txt"
    chmod 644 "$tmp"/{ysfgateway,YSFHosts.txt}
  elif [[ $2 == --p25-manager ]]; then
    sed '/^\[P25\( Network\)\?\]$/{n;s/^Enable=0$/Enable=1/;}' "$qa/fixtures/mmdvmhost" >"$tmp/mmdvmhost"
    sed '/^\[Remote Commands\]$/,/^$/s/^Port=6074$/Port=16074/' "$project/qa/classic-write/configure/etc/p25gateway" >"$tmp/p25gateway"
    printf '31415 127.0.0.1 41000\n' >"$tmp/P25Hosts.txt"
    chmod 644 "$tmp"/{p25gateway,P25Hosts.txt}
  elif [[ $2 == --nxdn-manager ]]; then
    sed '/^\[NXDN\( Network\)\?\]$/{n;s/^Enable=0$/Enable=1/;}' "$qa/fixtures/mmdvmhost" >"$tmp/mmdvmhost"
    sed '/^\[Remote Commands\]$/,/^$/s/^Port=6075$/Port=16075/' "$project/qa/classic-write/configure/etc/nxdngateway" >"$tmp/nxdngateway"
    printf '23456 127.0.0.1 41400\n' >"$tmp/NXDNHosts.txt"
    chmod 644 "$tmp"/{nxdngateway,NXDNHosts.txt}
  elif [[ $2 == --m17-manager ]]; then
    sed '/^\[M17\( Network\)\?\]$/{n;s/^Enable=0$/Enable=1/;}' "$qa/fixtures/mmdvmhost" >"$tmp/mmdvmhost"
    sed '/^\[Remote Commands\]$/,/^$/s/^Port=6076$/Port=16076/' "$project/qa/classic-write/configure/etc/m17gateway" >"$tmp/m17gateway"
    printf 'M17-QAT 127.0.0.1 17000\n' >"$tmp/M17Hosts.txt"
    chmod 644 "$tmp"/{m17gateway,M17Hosts.txt}
  elif [[ $2 == --bm-manager ]]; then
    duplex=1
    if [[ $bm_kind == *-simplex ]]; then duplex=0; fi
    sed -e "s/^Duplex=0$/Duplex=$duplex/" -e '/^\[DMR Network\]$/{n;s/^Enable=0$/Enable=1\nSlot1='"$duplex"'\nSlot2=1/;}' "$qa/fixtures/mmdvmhost" >"$tmp/mmdvmhost"
    printf 'BM_QA 0000 fixture.invalid unused 62031\n' >"$tmp/DMR_Hosts.txt"
    printf '[key]\napikey="%s"\n' "$(openssl rand -hex 110)" >"$tmp/token"
    openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=api.brandmeister.network \
      -addext subjectAltName=DNS:api.brandmeister.network -keyout "$tmp/key" -out "$tmp/cert" 2>/dev/null
    chmod 644 "$tmp"/{key,cert,token,DMR_Hosts.txt}
  elif [[ $2 == --tgif-* ]]; then
    sed '/^\[DMR Network\]$/,/^\[/ { s/^Enable=0$/Enable=1/; s/^Address=fixture.invalid$/Address=tgif.network/; }' "$qa/fixtures/mmdvmhost" >"$tmp/mmdvmhost"
    printf '[key]\napikey=TGIFSTG1.%s.%s.%s\n' "$(openssl rand -hex 8)" "$(openssl rand -hex 8)" "$(openssl rand -hex 24)" >"$tmp/token"
    openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=api.tgif.network \
      -addext subjectAltName=DNS:api.tgif.network -keyout "$tmp/key" -out "$tmp/cert" 2>/dev/null
    # Bind mounts are read-only and the parent directory is private on the host.
    chmod 644 "$tmp"/{key,cert,token}
  else
    sed '/^\[D-Star\]$/{n;s/^Enable=0$/Enable=1\nModule=B/;}' "$qa/fixtures/mmdvmhost" >"$tmp/mmdvmhost"
    printf '\n[D-Star Network]\nEnable=1\n' >>"$tmp/mmdvmhost"
    sed -e 's/^ccsEnabled=0$/ccsEnabled=1/' -e 's/^dcsEnabled=0$/dcsEnabled=1/' "$qa/fixtures/ircddbgateway" >"$tmp/ircddbgateway"
    printf 'repeaterCall1=N0DEMO\nrepeaterBand1=B\n' >>"$tmp/ircddbgateway"
    if [[ $2 == --dstar-manager ]]; then
      modes=(mmdvmhost dstarrepeater)
      printf 'repeaterCall2=N0DEMO\nrepeaterBand2=C\natStartup2=0\n' >>"$tmp/ircddbgateway"
      printf 'DCS023 127.0.0.1\n' >"$tmp/DCS_Hosts.txt"
      chmod 644 "$tmp/DCS_Hosts.txt"
    fi
    chmod 644 "$tmp/ircddbgateway"
  fi
  chmod 644 "$tmp/mmdvmhost"
  if [[ $2 != *-manager ]]; then echo "${2#--} GET regression: $root; read-only release, --network none"; fi
elif [[ ${2:-} == --auto-ap ]]; then
  tmp=$(mktemp -d); trap 'rm -rf -- "$tmp"' EXIT
  mkdir -p "$tmp/net/wlan0"
  chmod 755 "$tmp/net" "$tmp/net/wlan0"
  echo "Auto AP GET regression: $root; synthetic /sys/class/net/wlan0, read-only release, --network none"
fi
if [[ -n $remote_manager || ${2:-} =~ ^--(operator-browser|catalog|dstar-manager|bm-manager|tgif-manager)$ ]]; then
  network=pstar-operator-$(openssl rand -hex 6)
  if [[ $2 != *-manager ]]; then tmp=$(mktemp -d); fi
  relay=
  cleanup_mode() {
    if [[ -n $relay ]]; then kill "$relay" 2>/dev/null || :; relay=; fi
    "${docker[@]}" rm -f "$network" >/dev/null 2>&1 || :
  }
  trap 'cleanup_mode; "${docker[@]}" network rm "$network" >/dev/null 2>&1 || :; rm -rf -- "$tmp"' EXIT
  "${docker[@]}" network create --internal "$network" >/dev/null
  echo "Operator browser test: $root; read-only release, isolated network, loopback relay"
fi
for mode in "${modes[@]}"; do
  bind=(--mount "type=bind,src=$root,dst=/var/www/dashboard,readonly"
        --mount "type=bind,src=$qa,dst=/qa,readonly")
  mount_fixture() { bind+=(--mount "type=bind,src=$qa/fixtures/$1,dst=$2,readonly"); }
  for conf in pistar-release dstarrepeater; do mount_fixture "$conf" "/etc/$conf"; done
  if [[ ${2:-} == --dstar-* ]]; then
    bind+=(--mount "type=bind,src=$tmp/ircddbgateway,dst=/etc/ircddbgateway,readonly")
    mount_fixture DStarLinks.txt /var/log/pi-star/Links.log
  else
    mount_fixture ircddbgateway /etc/ircddbgateway
    mount_fixture empty /var/log/pi-star/Links.log
  fi
  if [[ ${2:-} == --tgif-* ]]; then
    bind+=(--env "QA_TGIF_KIND=$tgif_kind" --add-host api.tgif.network:127.0.0.1 --sysctl net.ipv4.ip_unprivileged_port_start=0
           --mount "type=bind,src=$tmp/key,dst=/run/qa-tgif-key,readonly"
           --mount "type=bind,src=$tmp/cert,dst=/etc/ssl/certs/ca-certificates.crt,readonly"
           --mount "type=bind,src=$tmp/token,dst=/etc/tgifapi.key,readonly")
  fi
  if [[ -n $remote_manager || ${2:-} =~ ^--(pocsag|dstar-network|dstar-manager|bm-manager|tgif-static|tgif-manager)$ ]]; then
    bind+=(--mount "type=bind,src=$tmp/mmdvmhost,dst=/etc/mmdvmhost,readonly")
  else
    mount_fixture mmdvmhost /etc/mmdvmhost
  fi
  mount_fixture empty "/etc/dstar-radio.$mode"
  if [[ ${2:-} == --auto-ap ]]; then
    bind+=(--mount "type=bind,src=$tmp/net,dst=/sys/class/net,readonly")
  fi
  if [[ ${2:-} == --catalog ]]; then
    for conf in ysfgateway dapnetgateway dmrgateway p25gateway nxdngateway timeserver starnetserver; do
      bind+=(--mount "type=bind,src=$project/qa/classic-write/configure/etc/$conf,dst=/etc/$conf,readonly")
    done
    for conf in pistar-remote crontab; do
      printf '# QA %s\n' "$conf" >"$tmp/$conf"
      bind+=(--mount "type=bind,src=$tmp/$conf,dst=/etc/$conf,readonly")
    done
    printf 'country=GB\n' >"$tmp/wpa"
    printf '1 -100\n' >"$tmp/rssi"
    chmod 644 "$tmp"/{crontab,pistar-remote,wpa,rssi}
    bind+=(--env QA_CATALOG=1 --mount "type=bind,src=$tmp/wpa,dst=/etc/wpa_supplicant/wpa_supplicant.conf,readonly"
           --mount "type=bind,src=$tmp/rssi,dst=/usr/local/etc/RSSI.dat,readonly")
  elif [[ ${2:-} == --pocsag ]]; then
    bind+=(--mount "type=bind,src=$project/qa/classic-write/configure/etc/dapnetgateway,dst=/etc/dapnetgateway,readonly")
    for conf in ysfgateway dmrgateway; do mount_fixture empty "/etc/$conf"; done
  elif [[ -n $remote_manager ]]; then
    for conf in dapnetgateway dmrgateway; do mount_fixture empty "/etc/$conf"; done
    if [[ $remote_manager != ysf ]]; then mount_fixture empty /etc/ysfgateway; fi
    bind+=(--env QA_REMOTE_MANAGER=1
           --mount "type=bind,src=$tmp/${remote_manager}gateway,dst=/etc/${remote_manager}gateway,readonly"
           --mount "type=bind,src=$tmp/${remote_manager^^}Hosts.txt,dst=/usr/local/etc/${remote_manager^^}Hosts.txt,readonly"
           --mount "type=bind,src=$qa/remote-command.php,dst=/usr/local/bin/RemoteCommand,readonly")
  else
    for conf in ysfgateway dapnetgateway dmrgateway; do mount_fixture empty "/etc/$conf"; done
  fi
  if [[ ${2:-} == --bm-manager ]]; then
    bind+=(--env "QA_BM_KIND=$bm_kind" --add-host api.brandmeister.network:127.0.0.1 --sysctl net.ipv4.ip_unprivileged_port_start=0
           --mount "type=bind,src=$tmp/key,dst=/run/qa-bm-key,readonly"
           --mount "type=bind,src=$tmp/cert,dst=/etc/ssl/certs/ca-certificates.crt,readonly"
           --mount "type=bind,src=$tmp/token,dst=/etc/bmapi.key,readonly"
           --mount "type=bind,src=$tmp/DMR_Hosts.txt,dst=/usr/local/etc/DMR_Hosts.txt,readonly")
  else
    mount_fixture DMR_Hosts.txt /usr/local/etc/DMR_Hosts.txt
  fi
  if [[ ${2:-} == --dstar-manager ]]; then
    bind+=(--env QA_DSTAR_MANAGER=1 --mount "type=bind,src=$tmp/DCS_Hosts.txt,dst=/usr/local/etc/DCS_Hosts.txt,readonly")
  else
    mount_fixture empty /usr/local/etc/DCS_Hosts.txt
  fi
  for net in DPlus DExtra; do mount_fixture empty "/usr/local/etc/${net}_Hosts.txt"; done
  mount_fixture empty /usr/local/etc/APRSHosts.txt
  for conf in dmr2ysf ysf2dmr ysf2p25 dmr2nxdn; do mount_fixture disabled "/etc/$conf"; done
  if [[ $mode == dstarrepeater ]]; then
    mount_fixture Headers.txt /var/log/pi-star/Headers.log
  fi
  echo "=== $mode ==="
  run=(run --pull=never --rm --read-only --network "$network" \
    --user 65534:65534 --cap-drop ALL --security-opt no-new-privileges --hostname fixture \
     --memory 192m --cpus 1 --pids-limit 24 --tmpfs /tmp:rw,noexec,nosuid,nodev,size=12m \
     --tmpfs /var/log/pi-star:rw,noexec,nosuid,nodev,size=1m,uid=65534,mode=0700 \
    "${bind[@]}" --workdir /var/www/dashboard --entrypoint timeout)
  if [[ $network == none ]]; then
    timeout -k 3 60 "${docker[@]}" "${run[@]}" "$image" 40 sh /qa/inside.sh "$mode" "${2:-}"
    continue
  fi
  browser_mode=--operator-browser
  if [[ $2 == --tgif-manager || $2 == --bm-manager ]]; then browser_mode=$2; fi
  "${docker[@]}" "${run[@]}" --name "$network" -d "$image" 90 sh /qa/inside.sh "$mode" "$browser_mode" >/dev/null
  ip=$("${docker[@]}" inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$network")
  socat -d -d TCP4-LISTEN:0,bind=127.0.0.1,fork "TCP4:$ip:8080" 2>"$tmp/relay" &
  relay=$!
  for ((i=0;i<100;i++)); do
    port=$(sed -nE 's/.*listening on .*127\.0\.0\.1:([0-9]+).*/\1/p' "$tmp/relay" | head -1)
    [[ -z $port ]] || break
    sleep .02
  done
  [[ $port =~ ^[0-9]+$ ]]
  if [[ $2 == --bm-manager ]]; then
    python3 -B "$qa/bm_browser.py" "http://127.0.0.1:$port" "$network" "$bm_kind" || rc=1
    "${docker[@]}" exec "$network" sh -ec '
      cat /tmp/bm-audit
      grep -E "\[200\]: (GET|POST) /admin/index.php$" /tmp/server.log
      count=2
      case "$QA_BM_KIND" in drop-*) count=1;; esac
      test "$(grep -Fc "[200]: POST /admin/index.php" /tmp/server.log)" = "$count"
      ! grep -E "PHP (Fatal|Parse) error|PHP (Warning|Notice|Deprecated):" /tmp/server.log' || rc=1
    cleanup_mode
    continue
  fi
  if [[ -n $remote_manager ]]; then
    python3 -B "$qa/remote_manager_browser.py" "http://127.0.0.1:$port" "$network" "$remote_manager" || rc=1
    "${docker[@]}" exec "$network" sh -ec '
      cat /tmp/remote-command-audit
      grep -F "[200]: POST /admin/index.php" /tmp/server.log
      test "$(grep -Fc "[200]: POST /admin/index.php" /tmp/server.log)" = 2
      ! grep -E "PHP (Fatal|Parse) error|PHP (Warning|Notice|Deprecated):" /tmp/server.log' || rc=1
    if ((rc)); then "${docker[@]}" exec "$network" tail -35 /tmp/server.log; fi
    cleanup_mode
    continue
  fi
  if [[ $2 == --dstar-manager ]]; then
    python3 -B "$qa/dstar_browser.py" "http://127.0.0.1:$port" "$network" || rc=1
    "${docker[@]}" exec "$network" sh -ec '
      cat /tmp/remotecontrold-audit
      test "$(grep -Fc "[200]: POST /admin/index.php" /tmp/server.log)" = 2
      ! grep -E "PHP (Fatal|Parse) error|PHP (Warning|Notice|Deprecated):" /tmp/server.log' || rc=1
    cleanup_mode
    continue
  fi
  if [[ $2 == --tgif-manager ]]; then
    python3 -B "$qa/tgif_browser.py" "http://127.0.0.1:$port" "$network" "$tgif_kind" || rc=1
    "${docker[@]}" exec "$network" sh -ec '
      cat /tmp/tgif-audit
      grep -F "[200]: POST /admin/index.php" /tmp/server.log
      ! grep -E "PHP (Fatal|Parse) error|PHP (Warning|Notice|Deprecated):" /tmp/server.log' || rc=1
    cleanup_mode
    continue
  fi
  if [[ $2 == --catalog ]]; then
    python3 -B "$qa/catalog.py" "http://127.0.0.1:$port" "$mode" "$network" || rc=1
    cleanup_mode
    continue
  fi
  python3 -B "$qa/operator_browser.py" "http://127.0.0.1:$port" "$mode"
  "${docker[@]}" exec "$network" sh -ec '
    grep -F "[200]: GET /operator/" /tmp/server.log
    grep -F "[200]: GET /api/last_heard.php?num_transmissions=20" /tmp/server.log
    if grep -E "PHP (Fatal|Parse) error|PHP Warning:" /tmp/server.log; then exit 1; fi'
  echo "PASS $mode: PHP server log confirms requests and no warnings/fatal/parse errors"
  cleanup_mode
done
exit "$rc"
