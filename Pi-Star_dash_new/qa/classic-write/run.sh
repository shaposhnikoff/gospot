#!/usr/bin/env bash
# Pass a built stock docroot. No host configs, system dbus, WLAN or devices.
set -euo pipefail
cd "$(dirname "$0")/../.."
export DOCKER_HOST=unix:///var/run/docker.sock
unset DOCKER_CONTEXT
q="$PWD/qa/classic-write"
base="$PWD/qa/classic-runtime/fixtures"
root=$(realpath -e "${1:?expected local build/release-*/docroot}")
[[ $root == "$PWD"/build/release-*/docroot && -f $root/admin/configure.php ]] || exit 2
name=pstar-classic-write-$(openssl rand -hex 5)
only=${2:-both}; variant=default
menu=
bookworm=
if [[ $only == menu-bookworm-dmr ]]; then
  bookworm=1; menu=bookworm; only=configure; variant=bookworm
  export QA_MENU_WIDTH=1366
fi
if [[ $only =~ ^menu-(configuration|expert)-(desktop|mobile)$ ]]; then
  menu=${BASH_REMATCH[1]}
  export QA_MENU_WIDTH=1366
  if [[ ${BASH_REMATCH[2]} == mobile ]]; then QA_MENU_WIDTH=390; fi
  if [[ $menu == expert ]]; then only=expert-p25; else only=configure-location; fi
fi
quick_service=
case "$only" in
  expert-nxdn|expert-ysf|expert-p25|expert-dapnet) quick_service="${only#expert-}gateway" ;;
  expert-mmdvmhost|expert-dstarrepeater|expert-ircddbgateway|expert-timeserver|expert-starnetserver) quick_service=${only#expert-} ;;
esac
if [[ -n $quick_service ]]; then variant=${only#expert-}; only=expert; fi
if [[ $only =~ ^full-(dmrgateway|wifi|pistar-remote|rssidat|cron|bmapikey|dapnetapi|tgifapikey|apikeys(-bm|-dapnet)?)$ ]]; then variant=$only; only=expert; fi
if [[ $only == wifi-delete ]]; then only=wifi; variant=delete; fi
if [[ $only == wifi-psk ]]; then only=wifi; variant=psk; fi
if [[ $only == wifi-stale ]]; then only=wifi; variant=stale; fi
if [[ $only =~ ^configure-auto-ap(-dstar)?(-install-failure)?$ ]]; then variant=${only#configure-}; only=configure; fi
if [[ $only == configure-color ]]; then only=configure; variant=color; fi
if [[ $only == configure-color-zero ]]; then only=configure; variant=zero; fi
if [[ $only == configure-location ]]; then only=configure; variant=location; fi
if [[ $only == configure-timezone ]]; then only=configure; variant=timezone; fi
if [[ $only == configure-language ]]; then only=configure; variant=language; fi
if [[ $only == configure-modem ]]; then only=configure; variant=modem; fi
if [[ $only == configure-controller ]]; then only=configure; variant=controller; fi
if [[ $only == configure-controller-reverse ]]; then only=configure; variant=controller-reverse; fi
if [[ $only == configure-controller-first ]]; then only=configure; variant=controller-first; fi
if [[ $only == configure-controller-first-default ]]; then only=configure; variant=controller-first-default; fi
[[ $only == both || $only == wifi || $only == expert || $only == backup || $only == configure || $only == power || $only == update || $only == upgrade ]] || exit 2
d=$(mktemp -d); relay=
trap '[[ -z $relay ]] || kill "$relay" 2>/dev/null || :; docker rm -f "$name" >/dev/null 2>&1 || :; docker network rm "$name" >/dev/null 2>&1 || :; rm -rf -- "$d"' EXIT
runtime=pstar-classic-write-runtime:8.2
if [[ -n $bookworm ]]; then
  runtime=pstar-classic-ini:bookworm
  docker build -q -t "$runtime" - < "$q/bookworm/Dockerfile"
else
  docker build -q -t "$runtime" - < "$q/Dockerfile"
fi
if [[ $only == backup ]]; then
  runtime=pstar-classic-backup-runtime:8.2
  docker build -q -t "$runtime" - < "$q/Dockerfile.backup"
fi
docker network create --internal "$name" >/dev/null
limit=48
[[ $only != configure ]] || limit=192 # stock Apply starts/stops many units concurrently
mount_root=$root
if [[ $menu == expert ]]; then
  # Quick Edit locks __FILE__. Independent stations must not share the same
  # inode through a host bind mount when desktop/mobile scenarios overlap.
  cp -a "$root" "$d/docroot"
  mount_root=$d/docroot
fi
docker run --detach --name "$name" --network "$name" --memory 256m --pids-limit "$limit" \
  --mount "type=bind,src=$mount_root,dst=/var/www/dashboard,readonly" \
  --mount "type=bind,src=$q,dst=/qa-write,readonly" \
  --mount "type=bind,src=$(realpath ../image/stage-mmdvm/00-install/files/DMRGateway.ini),dst=/work/DMRGateway.ini,readonly" \
  --mount "type=bind,src=$base,dst=/qa-base,readonly" \
  --workdir /var/www/dashboard "$runtime" sleep infinity >/dev/null
umask 077; openssl rand -hex 16 >"$d/auth"; docker cp "$d/auth" "$name:/run/qa-basic"
docker exec -e ONLY="$only" -e VARIANT="$variant" -e QUICK_SERVICE="$quick_service" "$name" bash -euc '
  install -d /work; cd /work
  chown root:www-data /run/qa-basic; chmod 640 /run/qa-basic
  cp /qa-base/{pistar-release,ircddbgateway,mmdvmhost} /etc/
  touch /work/effects
  install -m755 /qa-write/priv /usr/local/sbin/qa-priv
  printf "%s\n" "#!/bin/sh" "exec /usr/bin/sudo -n /usr/local/sbin/qa-priv \"\$@\"" >/usr/local/bin/sudo
  chmod 755 /usr/local/bin/sudo
  echo "www-data ALL=(root) NOPASSWD: NOLOG_INPUT: NOLOG_OUTPUT: /usr/local/sbin/qa-priv" >/etc/sudoers.d/php-fixture
  chmod 440 /etc/sudoers.d/php-fixture; /usr/sbin/visudo -cq
  if [[ $ONLY == power || $ONLY == update || $ONLY == upgrade ]]; then
    # These three scenarios get a logging-only sudo, with no privileged forwarding.
    rm /etc/sudoers.d/php-fixture
    if [[ $ONLY == power ]]; then
      install -m755 /qa-write/power-stub /usr/local/bin/sudo
    else
      install -m755 /qa-write/update-stub /usr/local/bin/sudo
      install -d /var/log/pi-star
    fi
    chown www-data:www-data /work/effects
  fi
  for cmd in ifconfig iwconfig iw; do
    printf "%s\n" "#!/bin/sh" "exit 0" >"/usr/local/bin/$cmd"; chmod 755 "/usr/local/bin/$cmd"
  done
  printf "%s\n" "#!/bin/sh" "echo wlan0 HWaddr 02:00:00:00:00:01" >/usr/local/bin/ifconfig
  install -d /etc/wpa_supplicant
  touch /etc/dmrgateway /etc/wpa_supplicant/wpa_supplicant.conf
  if [[ $ONLY == both || $ONLY == wifi ]]; then bash /qa-write/wifi-fixture.sh "$VARIANT"; fi
  if [[ $VARIANT == full-dmrgateway ]]; then touch /work/priv-commands; fi
  if [[ $VARIANT == full-wifi ]]; then
    bash /qa-write/wifi-fixture.sh psk
    touch /work/priv-commands
  elif [[ $VARIANT == full-pistar-remote ]]; then
    install -m644 /qa-write/pistar-remote.fixture /etc/pistar-remote
    touch /work/priv-commands
  elif [[ $VARIANT == full-rssidat ]]; then
    install -D -m644 /qa-write/RSSI.dat.fixture /usr/local/etc/RSSI.dat
    touch /work/priv-commands
  elif [[ $VARIANT == full-cron ]]; then
    for bin in cron crond anacron; do
      if command -v "$bin" >/dev/null; then exit 1; fi
    done
    for task in /proc/[0-9]*/comm; do
      case "$(cat "$task" 2>/dev/null)" in cron|crond|anacron) exit 1 ;; esac
    done
    echo "PASS disposable container: no cron, crond or anacron binary or running scheduler"
    install -m644 /qa-write/crontab.fixture /etc/crontab
    sed "s/^15 4 /25 4 /" /qa-write/crontab.fixture >/work/cron-approved
    touch /work/priv-commands
  elif [[ $VARIANT == full-bmapikey ]]; then
    test ! -e /etc/bmapi.key
    touch /work/qa-bmapikey /work/priv-commands
  elif [[ $VARIANT == full-dapnetapi ]]; then
    test ! -e /etc/dapnetapi.key && test ! -e /etc/dapnetapi
    touch /work/qa-dapnetapi /work/priv-commands
  elif [[ $VARIANT == full-tgifapikey ]]; then
    test ! -e /etc/tgifapi.key
    grep -Fx "Version=4.3.9" /etc/pistar-release
    touch /work/qa-tgifapikey /work/priv-commands
  elif [[ $VARIANT == full-apikeys* ]]; then
    grep -Fx "Version=4.3.9" /etc/pistar-release
    for file in bmapi dapnetapi tgifapi; do
      install -m600 -o www-data -g www-data /dev/null "/etc/$file.key"
    done
    touch /work/qa-bmapikey /work/qa-dapnetapi /work/qa-tgifapikey /work/priv-commands
  elif [[ -n $QUICK_SERVICE ]]; then
    install -m644 "/qa-write/configure/etc/$QUICK_SERVICE" "/etc/$QUICK_SERVICE"
    if [[ $VARIANT == p25 ]]; then
      sed -i "/^HostsFile2=/i ; Site note: secondary host list contains local reflectors." /etc/p25gateway
    elif [[ $VARIANT == dapnet ]]; then
      sed -i "/^RptPort=/i ; Site note: local paging port." /etc/dapnetgateway
    elif [[ $VARIANT == dstarrepeater ]]; then
      sed -i "/^gateway=/i ; Site note: keep the module suffix." /etc/dstarrepeater
    elif [[ $VARIANT == timeserver ]]; then
      sed -i "/^sendA=/i ; Modules used for site time announcements." /etc/timeserver
    elif [[ $VARIANT == starnetserver ]]; then
      sed -i "/^ircddbUsername=/i ; Local StarNet login." /etc/starnetserver
    fi
  elif [[ $ONLY == both || $ONLY == expert ]]; then bash /qa-write/expert-fixture.sh; fi
  install -d -o www-data -g www-data -m700 /tmp/qa-sessions
  install -o www-data -g www-data -m600 /dev/null /tmp/qa-server.log
'
inside=(docker exec "$name")
ini_args=()
prepend=/qa-write/gate.php
if [[ $only == backup || $only == configure ]]; then
  docker exec "$name" bash /qa-write/backup-root.sh "$only"
  php=(docker exec -d "$name" chroot --userspec=www-data:www-data /stand)
  inside+=(chroot /stand)
  if [[ -n $bookworm ]]; then
    snapshot=${QA_BOOKWORM_SNAPSHOT:-"$q/bookworm/pistar3-sanitized.json"}
    for file in bridge.py dispatch session.php; do docker cp "$q/bookworm/$file" "$name:/stand/work/$file"; done
    docker cp "$snapshot" "$name:/stand/work/snapshot.json"
    docker cp ../panel/helper/pistar-config "$name:/stand/work/pistar-config"
    docker cp "$q/priv" "$name:/stand/work/expert-priv"
    for unit in mmdvmhost dmrgateway; do
      docker cp "../image/stage-mmdvm/00-install/files/$unit.service" "$name:/stand/etc/systemd/system/$unit.service"
    done
    "${inside[@]}" bash -euc '
      . /etc/os-release; test "$VERSION_CODENAME" = bookworm
      install -d -m700 /var/lib/pistar-panel-config
      install -Dm755 /work/pistar-config /usr/local/libexec/pistar-config
      mv /usr/local/sbin/qa-priv /work/configure-priv
      install -m755 /work/dispatch /usr/local/sbin/qa-priv
      chmod 755 /work/expert-priv
      cp /etc/mmdvmhost /work/host-form-template
      python3 -c "import json,pathlib; s=json.loads(pathlib.Path(\"/work/snapshot.json\").read_bytes()); [pathlib.Path(\"/etc\",p).write_bytes(s[p].encode()) for p in (\"MMDVM-Host.ini\",\"DMRGateway.ini\")]"
      chown mmdvm:mmdvm /etc/{MMDVM-Host,DMRGateway}.ini
      chmod 644 /etc/{MMDVM-Host,DMRGateway}.ini
      printf "%s\n" "#!/bin/sh" "echo native-helper-systemctl \"\$@\" >>/work/effects" >/usr/bin/systemctl
      chmod 755 /usr/bin/systemctl
      grep "^ExecStart=" /etc/systemd/system/{mmdvmhost,dmrgateway}.service
      stat -c "%n %U:%G %a" /etc/{MMDVM-Host,DMRGateway}.ini
      echo "Bookworm PHP: $(php -v | head -1); native helper SHA256: $(sha256sum /usr/local/libexec/pistar-config)"
    '
    prepend=/work/session.php
    ini_args=(-d extension=dom -d extension=ctype -d extension=posix)
  fi
  if [[ $variant == auto-ap* ]]; then
    "${inside[@]}" sh -ec '
      test -d /sys/class/net/wlan0
      install -d /etc/hostapd
      printf "%s\n" "# isolated Auto AP" interface=wlan0 ssid=QA-AutoAP wpa=1 wpa_passphrase=BeforeFixtureOnly channel=6 >/etc/hostapd/hostapd.conf
      chmod 600 /etc/hostapd/hostapd.conf
      case "$1" in *-install-failure) touch /work/fail-hostapd-install ;; esac
      case "$1" in auto-ap-dstar*)
        mv /etc/dstar-radio.mmdvmhost /etc/dstar-radio.dstarrepeater
        for net in DCS DPlus DExtra; do echo "# fixture" >"/usr/local/etc/${net}_Hosts.txt"; done
      ;; esac
    ' sh "$variant"
  fi
  if [[ $variant == modem ]]; then
    "${inside[@]}" sh -ec '
      test -f /etc/dstar-radio.mmdvmhost && test ! -e /etc/dstar-radio.dstarrepeater
      : >/etc/dstar-radio.mmdvmhost
      echo mmdvmhost.service >/work/controller-active
      echo "Initial fixture: empty MMDVMHost marker; only mmdvmhost.service active"
    '
  fi
  if [[ $variant == controller || $variant == controller-reverse || $variant == controller-first* ]]; then
    "${inside[@]}" sh -ec '
      for proto in DCS DPlus DExtra; do echo "# QA host list" >"/usr/local/etc/${proto}_Hosts.txt"; done
      echo "operator-owned file" >/etc/dstar-radio.qa-keep
      if [ "$1" = controller-first ] || [ "$1" = controller-first-default ]; then
        rm -f /etc/dstar-radio.mmdvmhost /etc/dstar-radio.dstarrepeater
        : >/work/controller-active
        echo "First setup: both controller markers absent, neither controller active"
      elif [ "$1" = controller-reverse ]; then
        mv /etc/dstar-radio.mmdvmhost /etc/dstar-radio.dstarrepeater
        echo dstarrepeater.service >/work/controller-active
      else
        echo mmdvmhost.service >/work/controller-active
      fi
      echo "Initial active controller (fixture): $(cat /work/controller-active)"
    ' sh "$variant"
  fi
  if [[ $only == backup ]]; then ini_args=(-d extension=zip); fi
else
  php=(docker exec -d --user www-data "$name")
fi
"${php[@]}" sh -c 'cd /var/www/dashboard; exec php -n "$@" \
  -d session.save_path=/tmp/qa-sessions -d output_buffering=4096 -d display_errors=0 -d log_errors=1 \
   -d allow_url_fopen=0 -d default_socket_timeout=2 -d max_execution_time=15 -S 0.0.0.0:8080 > /tmp/qa-server.log 2>&1' sh "${ini_args[@]}" -d "auto_prepend_file=$prepend"
ip=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$name")
listen=TCP4-LISTEN:0
curl_tls=()
if [[ -n $menu ]]; then
  openssl req -x509 -nodes -newkey rsa:2048 -days 1 -keyout "$d/key" -out "$d/cert" \
    -subj /CN=127.0.0.1 -addext subjectAltName=IP:127.0.0.1 2>/dev/null
  QA_MENU_PIN=$(openssl x509 -in "$d/cert" -pubkey -noout | openssl pkey -pubin -outform der | openssl dgst -sha256 -binary | openssl base64 -A)
  export QA_MENU_PIN QA_MENU_ROOT="$root"
  curl_tls=(--cacert "$d/cert")
  listen="OPENSSL-LISTEN:0,cert=$d/cert,key=$d/key,verify=0,pf=ip4"
fi
socat -d -d "$listen,bind=127.0.0.1,fork" "TCP4:$ip:8080" 2>"$d/relay" &
relay=$!
for ((i=0;i<100;i++)); do
  port=$(sed -nE 's/.*listening on .*127\.0\.0\.1:([0-9]+).*/\1/p' "$d/relay" | head -1); [[ -z $port ]] || break; sleep .02
done
[[ $port =~ ^[0-9]+$ ]]; origin=http://127.0.0.1:$port
if [[ -n $menu ]]; then origin=https://127.0.0.1:$port; export PISTAR_CLASSIC_HTTPS_PORT="$port"; fi
probe='/admin/wifi.php?page=wpa_conf'
[[ $only != configure ]] || probe='/admin/configure.php'
[[ $only != power ]] || probe=/admin/power.php
[[ $only != update ]] || probe=/admin/update.php
[[ $only != upgrade ]] || probe=/admin/expert/upgrade.php
if [[ -n $quick_service ]]; then probe=/admin/expert/edit_${quick_service}.php; fi
if [[ $variant == full-dmrgateway ]]; then probe=/admin/expert/fulledit_dmrgateway.php; fi
if [[ $variant == full-wifi ]]; then probe=/admin/expert/fulledit_wpaconfig.php; fi
if [[ $variant == full-pistar-remote ]]; then probe=/admin/expert/fulledit_pistar-remote.php; fi
if [[ $variant == full-rssidat ]]; then probe=/admin/expert/fulledit_rssidat.php; fi
if [[ $variant == full-cron ]]; then probe=/admin/expert/fulledit_cron.php; fi
if [[ $variant == full-bmapikey ]]; then probe=/admin/expert/fulledit_bmapikey.php; fi
if [[ $variant == full-dapnetapi ]]; then probe=/admin/expert/fulledit_dapnetapi.php; fi
if [[ $variant == full-tgifapikey ]]; then probe=/admin/expert/fulledit_tgifapikey.php; fi
if [[ $variant == full-apikeys* ]]; then probe=/admin/expert/fulledit_apikeys.php; fi
if [[ $only == configure || $only == power || $only == update || $only == upgrade || -n $quick_service || $variant == full-* ]]; then
  expected=$(sha256sum "$root$probe" | cut -d' ' -f1)
  actual=$("${inside[@]}" sha256sum "/var/www/dashboard$probe" | cut -d' ' -f1)
  [[ $actual == "$expected" ]]
  echo "Handler: ${root#"$PWD/"}$probe sha256=$actual; PHP $("${inside[@]}" php -n -r 'echo PHP_VERSION;')"
fi
[[ $(curl "${curl_tls[@]}" --noproxy '*' --retry 4 --retry-all-errors --retry-delay 1 --max-time 8 -sS -o /dev/null -w '%{http_code}' "$origin$probe") == 401 ]] || { "${inside[@]}" tail -15 /tmp/qa-server.log; exit 1; }
echo "PASS unauthenticated request: 401; isolated network and private target configs"
rc=0
browser_script="$q/browser.py"
if [[ -n $bookworm ]]; then browser_script="$q/bookworm/menu.py"; fi
PYTHONDONTWRITEBYTECODE=1 python3 "$browser_script" "$origin" "$name" "$d/auth" "$only" "$variant" || rc=$?
if ((rc)); then "${inside[@]}" tail -22 /tmp/qa-server.log; fi
"${inside[@]}" sh -c 'cat /work/effects; grep -E "PHP (Fatal|Parse) error|PHP Warning|\\[(200|303|403|500)\\]: POST" /tmp/qa-server.log | tail -35 || :' 
"${inside[@]}" sh -c '! grep -Eq "^(DENIED|FAIL)" /work/effects && ! grep -Eq "PHP (Fatal|Parse) error|PHP Warning" /tmp/qa-server.log' || rc=1
if [[ $only == configure ]]; then
  # A 200 and a fresh value alone do not rule out a partial platform write.
  "${inside[@]}" sh -c '
    pattern="(sh: [0-9]+: .*: (not found|Permission denied)|Pi-Star configure.php:|config_writer:|sudo: .*not allowed)"
    case "$1" in
      auto-ap|auto-ap-dstar) expected="Pi-Star configure.php: AutoAP PSK rejected (must be 8-63 printable ASCII or 64-hex)" ;;
      auto-ap*-install-failure) expected="config_writer: install exit=1 for /etc/hostapd/hostapd.conf:" ;;
      *) expected="" ;;
    esac
    if [ -n "$expected" ]; then
      test "$(grep -Fc "$expected" /tmp/qa-server.log)" = 1 || exit 1
      grep -F "$expected" /tmp/qa-server.log
      if grep -E "$pattern" /tmp/qa-server.log | grep -Fv "$expected"; then exit 1; fi
    elif grep -E "$pattern" /tmp/qa-server.log; then exit 1; fi' sh "$variant" || rc=1
fi
exit "$rc"
