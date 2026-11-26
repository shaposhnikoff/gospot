#!/bin/sh
# Run as root in the disposable container, before starting the PHP server.
set -eu
mkdir -p /etc/wpa_supplicant /etc/network
conf=/etc/wpa_supplicant/wpa_supplicant.conf
umask 077
cat >"$conf" <<'EOF'
ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1
ap_scan=1
fast_reauth=1
country=GB
freq_list=2412 2437 2462

EOF
# Delete the first row so the survivor still has suffix 1 at submission.
if [ "${1:-}" = delete ]; then cat /qa-write/wifi-delete.block >>"$conf"; fi
cat >>"$conf" <<'EOF'
network={
	#ssid="Fixture Existing"
	ssid=46697874757265204578697374696e67
	#psk="fixture-old-passphrase"
	psk=7e677c69fc506151f8455f45b26a548cc5ea623d6022ae41ae04a888c1d266fb
	id_str="0"
	bssid=02:00:00:00:00:01
	priority=47
	scan_ssid=1
}

EOF
if [ "${1:-}" = psk ]; then
    cat >>"$conf" <<'EOF'
network={
	# Leave this profile in its original quoted format.
	ssid="Fixture Standby"
	psk="fixture-standby-passphrase"
	id_str="8"
	priority=8
}
EOF
fi
chmod 600 "$conf"
# An independent configuration file that the Wi-Fi editor must not change.
if [ ! -e /etc/network/interfaces ]; then
    cat >/etc/network/interfaces <<'EOF'
# Keep the wired fixture independent of Wi-Fi saves.
auto lo
iface lo inet loopback
EOF
fi
