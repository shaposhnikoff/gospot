#!/bin/bash
set -euo pipefail
umask 022
test -e /work/DMRGateway.ini && test -d /var/www/dashboard/config
mkdir -p /etc/wpa_supplicant /usr/local/etc /etc/hostapd
# Independent oracle, not obtained by executing the PHP allowlist.
map=/work/backup-map.tsv
: >"$map"
entry() { printf '%s\t%s\n' "${1##*/}" "$1" >>"$map"; }
for name in ircddbgateway mmdvmhost dstarrepeater dapnetgateway p25gateway m17gateway ysfgateway ysf2dmr dgidgateway nxdngateway dmrgateway mobilegps starnetserver timeserver pistar-remote; do
  [[ -s /etc/$name ]] || printf '[QA]\nConfig=%s\n' "$name" >"/etc/$name"
  entry "/etc/$name"
done
cp /work/DMRGateway.ini /etc/dmrgateway
printf 'network={\n\tssid="LabStation"\n\tkey_mgmt=NONE\n}\n' >/etc/wpa_supplicant/wpa_supplicant.conf
chmod 0600 /etc/wpa_supplicant/wpa_supplicant.conf
entry /etc/wpa_supplicant/wpa_supplicant.conf
for f in bmapi.key dapnetapi.key tgifapi.key; do
  printf '[QA]\nKey=fixture-only\n' >"/etc/$f"; chmod 600 "/etc/$f"; chown www-data:www-data "/etc/$f"; entry "/etc/$f"
done
touch /etc/dstar-radio.mmdvmhost; entry /etc/dstar-radio.mmdvmhost
printf 'php-qa\n' >/etc/hostname
printf '127.0.0.1 localhost php-qa\n' >/etc/hosts
printf '100 -90\n' >/usr/local/etc/RSSI.dat
for f in /etc/{hosts,hostname} /usr/local/etc/RSSI.dat /var/www/dashboard/config/{config.php,ircddblocal.php}; do entry "$f"; done
# Excluded, DHCP-only, and a mode-prefixed file the backup does not own.
printf '# local DHCP; no static address\noption domain_name_servers\n' >/etc/dhcpcd.conf
printf 'interface=wlan0\nssid=qa-ap\n' >/etc/hostapd/hostapd.conf
printf 'password=separate-qa-secret\nother=keep\n' >'/root/.Remote Control'
chmod 600 '/root/.Remote Control'
echo 'not a supported mode marker' >/etc/dstar-radio.qa-keep
echo '# pistar-upnp.service' >/etc/crontab
