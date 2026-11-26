#!/bin/bash
# Invoked by chroot /stand; all paths here refer to the private dashboard copy.
set -euo pipefail
umask 022
seed=/qa-write/configure
web=/var/www/dashboard
[[ ! -e /proc/self && -f $seed/etc/mmdvmhost && -f $web/admin/configure.php && -x /usr/local/sbin/qa-priv ]]
install -d /sys/class/thermal/thermal_zone0 /sys/class/net/wlan0 /usr/local/etc /etc/wpa_supplicant
echo 40200 >/sys/class/thermal/thermal_zone0/temp

# Fixed configured-node input (not a setup POST). MMDVMHost uses a direct
# master; the separate inactive DMRGateway config retains GoDMRServer routing.
install -m644 "$seed"/etc/* /etc/
install -m644 /qa-base/pistar-release /etc/
install -m644 /qa-base/disabled /etc/dmr2nxdn
install -m644 /qa-base/disabled /etc/nxdn2dmr
install -m644 /qa-base/DMR_Hosts.txt /usr/local/etc/
printf 'fixture.invalid:14580\n' >/usr/local/etc/APRSHosts.txt
printf 'configure-qa\n' >/etc/hostname
printf '127.0.0.1 localhost configure-qa %s\n' "$(uname -n)" >/etc/hosts
printf 'UTC\n' >/etc/timezone
printf '# pistar-upnp.service\n' >/etc/crontab
printf '# $DAEMON -a $ipVar 80 80 TCP\n# $DAEMON -a $ipVar 10022 10022 UDP\n# $DAEMON -a $ipVar 22 22 TCP\n' >/usr/local/sbin/pistar-upnp.service
install -d /lib/systemd/system
for service in mmdvmhost dstarrepeater; do
    printf '[Timer]\nOnStartupSec=30\n' >"/lib/systemd/system/$service.timer"
done
printf 'country=GB\nnetwork={\n\tssid="LabStation"\n\tkey_mgmt=NONE\n}\n' >/etc/wpa_supplicant/wpa_supplicant.conf
chmod 600 /etc/wpa_supplicant/wpa_supplicant.conf

# Use stock rewritten-assignment formatting from a configured node.
sed -Ei "s/^\\\$callsign[[:space:]]*=[[:space:]]*'[^']*';/\\\$callsign='N0DEMO';/" "$web/config/ircddblocal.php"
sed -Ei "s/^\\\$pistarLanguage[[:space:]]*=[[:space:]]*'english_uk';/\\\$pistarLanguage='english_uk';/" "$web/config/language.php"
# Runtime write targets use the deployed owner/mode, not the checkout owner's
# metadata carried by cp -a into the private web root.
chown root:root "$web/config/"{ircddblocal,language,config}.php
chmod 644 "$web/config/"{ircddblocal,language,config}.php
for cmd in platformDetect.sh uptime timedatectl systemctl DMRGateway P25Gateway NXDNGateway YSFGateway ifconfig iwconfig iw; do
    install -m755 "$seed/probe" "/usr/local/bin/$cmd"
done
