#!/usr/bin/env bash
# qemu-boot.sh IMAGE [BOARDS]: boot fresh copies of a built image in
# QEMU (raspi2b, the Raspberry Pi 2 B of the target boards, with the image's
# own kernel7, device tree and initramfs) and probe each "board" from inside
# a throw-away Docker container: operator panel, management login, first-boot
# credentials, unit states. Nothing listens on host ports. Prints certificate
# and SSH host key fingerprints so several boards can be compared; never
# prints passwords or keys. Paths, names and ports come from the settings
# file named in image/settings.path.
set -euo pipefail
here=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)
conf=$(realpath -e "$here/$(cat "$here/settings.path")")

if [[ ${1:-} == --inside ]]; then
	# --- In the container: /work/disk.img (read-only), /work/out, /conf. The
	# forwarded ports are those of the guest, in the container's own network
	# namespace, so nothing else on the host can collide with them. ---
	shift
	board=$1
	# shellcheck source=/dev/null
	source /conf
	export DEBIAN_FRONTEND=noninteractive
	apt-get update -qq
	apt-get install -y -qq --no-install-recommends qemu-system-arm qemu-utils mtools fdisk curl openssl \
		openssh-client sshpass ca-certificates >/dev/null
	run=/run/board; mkdir -p "$run"
	cp --sparse=always /work/disk.img "$run/disk.img"
	size=$(stat -c %s "$run/disk.img") pow=$((1 << 30))
	while ((pow < size)); do pow=$((pow * 2)); done
	qemu-img resize -q -f raw "$run/disk.img" "$pow"  # SD card sizes must be powers of two
	start=$(sfdisk -d "$run/disk.img" | sed -n 's/^[^ ]*1 : start= *\([0-9]*\),.*/\1/p')
	[[ -n $start ]] || { echo "FAIL no boot partition"; exit 1; }
	boot="$run/disk.img@@$((start * 512))"
	for f in kernel7.img bcm2709-rpi-2-b.dtb cmdline.txt; do mcopy -n -i "$boot" "::$f" "$run/$f"; done
	initrd=()
	if mcopy -n -i "$boot" ::initramfs7 "$run/initramfs7" 2>/dev/null; then initrd=(-initrd "$run/initramfs7"); fi
	append="$(tr -d '\n' <"$run/cmdline.txt") console=ttyAMA0,115200 dwc_otg.fiq_fsm_enable=0"
	echo "board $board: kernel7.img, bcm2709-rpi-2-b.dtb${initrd:+, initramfs7}; cmdline: $append"
	fwd="hostfwd=tcp:127.0.0.1:22-:22,hostfwd=tcp:127.0.0.1:${OPERATOR_PORT}-:${OPERATOR_PORT},hostfwd=tcp:127.0.0.1:${MANAGEMENT_PORT}-:${MANAGEMENT_PORT}"
	op=http://127.0.0.1:$OPERATOR_PORT mg=https://127.0.0.1:$MANAGEMENT_PORT
	qemu-system-arm -M raspi2b -m 1G -smp 4 -kernel "$run/kernel7.img" -dtb "$run/bcm2709-rpi-2-b.dtb" "${initrd[@]}" \
		-drive "file=$run/disk.img,if=sd,format=raw" -append "$append" \
		-netdev "user,id=net0,$fwd" -device usb-net,netdev=net0 \
		-display none -serial "file:/work/out/console-$board.log" -monitor none &
	qemu=$!
	trap 'kill $qemu 2>/dev/null || true' EXIT
	t0=$(date +%s)
	waited() { echo "$(( $(date +%s) - t0 )) s"; }
	code() { curl -s -o /dev/null -w '%{http_code}' --max-time 20 "$@" || true; }
	until [[ $(code "$op/") == 200 && $(code -k "$mg/manage/") == 401 ]]; do
		kill -0 $qemu 2>/dev/null || { echo "FAIL QEMU exited"; exit 1; }
		(( $(date +%s) - t0 < 3600 )) || { echo "FAIL board $board not up within 60 min"; exit 1; }
		sleep 15
	done
	failures=0
	check() { local what=$1; shift; if "$@"; then echo "PASS $what"; else echo "FAIL $what"; failures=$((failures + 1)); fi; }
	echo "board $board: operator panel and management service answered after $(waited)"
	check "GET :$OPERATOR_PORT/ operator panel -> 200" test "$(code "$op/")" = 200
	check "GET :$MANAGEMENT_PORT/manage/ without credentials -> 401" test "$(code -k "$mg/manage/")" = 401
	printf 'user = "%s:%s"\n' "$GOSPOT_LINUX_USER" "$IMAGE_DEFAULT_PASSWORD" >"$run/login"
	check "GET :$MANAGEMENT_PORT/manage/ as $GOSPOT_LINUX_USER with the factory password -> 200" test "$(code -k -K "$run/login" "$mg/manage/")" = 200
	printf 'user = "%s:wrong-password"\n' "$GOSPOT_LINUX_USER" >"$run/wrong"
	check "GET :$MANAGEMENT_PORT/manage/ with a wrong password -> 401" test "$(code -k -K "$run/wrong" "$mg/manage/")" = 401
	check "GET http:// on the management port is refused" bash -c '[[ $(curl -s -o /dev/null -w "%{http_code}" --max-time 20 "$1/manage/") != 200 ]]' _ "${mg/https/http}"
	cert=$(openssl s_client -connect "127.0.0.1:$MANAGEMENT_PORT" -servername "$IMAGE_HOSTNAME" </dev/null 2>/dev/null | openssl x509)
	echo "board $board: TLS $(openssl x509 -noout -fingerprint -sha256 <<<"$cert")"
	echo "board $board: TLS $(openssl x509 -noout -subject <<<"$cert"); $(openssl x509 -noout -ext subjectAltName <<<"$cert" | tail -n +2 | tr -d ' ')"
	check "certificate issued for $IMAGE_HOSTNAME" bash -c 'openssl x509 -noout -subject <<<"$1" | grep -q "CN *= *$2"' _ "$cert" "$IMAGE_HOSTNAME"
	ssh-keyscan -T 20 127.0.0.1 2>/dev/null >"$run/known_hosts"
	ssh-keygen -lf "$run/known_hosts" | sed "s/^/board $board: SSH host key /"
	check "SSH host keys present" test -s "$run/known_hosts"
	export SSHPASS=$IMAGE_DEFAULT_PASSWORD
	remote() { sshpass -e ssh -o UserKnownHostsFile="$run/known_hosts" -o StrictHostKeyChecking=yes \
		-o PreferredAuthentications=password -o PubkeyAuthentication=no "$GOSPOT_LINUX_USER@127.0.0.1" "$@"; }
	check "SSH login as $GOSPOT_LINUX_USER with the factory password" remote true
	remote "hostname; . /etc/os-release; echo \$PRETTY_NAME; uname -rm; dpkg-query -W -f='\${Package} \${Version}\n' $RADIO_PACKAGE $DASHBOARD_PACKAGE" | sed "s/^/board $board: /"
	check "hostname $IMAGE_HOSTNAME" test "$(remote hostname)" = "$IMAGE_HOSTNAME"
	check "first boot finished ($IMAGE_FIRSTBOOT_UNIT, marker)" remote "systemctl is-active --quiet $IMAGE_FIRSTBOOT_UNIT && test -e $IMAGE_FIRSTBOOT_DONE"
	remote "systemctl show -p ExecMainStatus -p ActiveEnterTimestampMonotonic $IMAGE_FIRSTBOOT_UNIT; ls -l $WIZARD_CONFIG_DIR | tail -n +2 | sed 's/^\\([^ ]*\\) .* \\([^ ]*\\)\$/\\1 \\2/'" | sed "s/^/board $board: /"
	check "dashboard services active" remote "systemctl is-active --quiet ${IMAGE_ENABLE_UNITS//,/ }"
	# Radio units: the services the radio package ships, except those run by
	# timers the image enables.
	timer_services=$(tr , '\n' <<<"$IMAGE_ENABLE_UNITS" | sed -n 's/\.timer$/.service/p')
	units=$(remote "dpkg -L $RADIO_PACKAGE" | sed -n "s|^$SYSTEMD_UNIT_DIR/\([^/]*\.service\)$|\1|p" | grep -vxF "${timer_services:-/}")
	check "radio units ($(wc -w <<<"$units")) disabled and inactive" remote "for u in $(tr '\n' ' ' <<<"$units"); do [ \"\$(systemctl is-enabled \$u)\" = disabled ] && ! systemctl is-active --quiet \$u || { echo \"  \$u\"; exit 1; }; done"
	remote "systemctl --failed --no-legend --plain; systemctl is-system-running" | sed "s/^/board $board: systemd: /" || true
	failed=$(remote "systemctl --failed --no-legend --plain" || true)
	check "no failed units" test -z "$failed"
	check "no PHP, nginx or php-fpm processes" remote "! pgrep -a 'php|nginx'"
	remote "echo \"\$SSHPASS\" | sudo -S -p '' poweroff" 2>/dev/null </dev/null || true
	echo "board $board: $failures failed checks"
	exit $((failures > 0))
fi

[[ $# -ge 1 && -f $1 ]] || { echo "Usage: $0 IMAGE.img[.xz] [BOARDS]" >&2; exit 2; }
img=$(realpath -e "$1") boards=${2:-1}
# shellcheck source=/dev/null
source "$conf"
work=$(mktemp -d "${BUILD_PARENT:-$IMAGE_BUILD_PARENT}/$IMAGE_NAME-qemu.XXXXXXXX")
trap 'rm -f "$work/disk.img"' EXIT
case $img in
	*.xz) xz -dc -- "$img" >"$work/disk.img" ;;
	*) cp --sparse=always -- "$img" "$work/disk.img" ;;
esac
mkdir -p "$work/out"
echo "Image: $img"
echo "Image SHA-256: $(sha256sum <"$img" | cut -d' ' -f1)"
pids=()
for n in $(seq 1 "$boards"); do
	docker run --rm --platform "linux/$(dpkg --print-architecture)" --name "$IMAGE_NAME-qemu-${work##*.}-${n}" \
		-v "$work/disk.img:/work/disk.img:ro" -v "$work/out:/work/out" -v "$conf:/conf:ro" -v "$here/qemu-boot.sh:/qemu-boot.sh:ro" \
		debian:trixie bash /qemu-boot.sh --inside "$n" >"$work/out/board-$n.log" 2>&1 &
	pids+=($!)
done
rc=0
for n in $(seq 1 "$boards"); do wait "${pids[$((n - 1))]}" || rc=1; cat "$work/out/board-$n.log"; done
if (( boards > 1 )); then
	tls=$(grep -h 'TLS sha256 Fingerprint' "$work/out"/board-*.log | sed 's/.*Fingerprint=//' | sort -u | wc -l)
	ssh=$(grep -h 'SSH host key' "$work/out"/board-*.log | awk '{ print $6 }' | sort | uniq -d | wc -l)
	if [[ $tls == "$boards" ]]; then echo "PASS $boards boards have $tls different TLS certificates"; else echo "FAIL boards share a TLS certificate"; rc=1; fi
	if [[ $ssh == 0 ]]; then echo "PASS no SSH host key is shared between boards"; else echo "FAIL $ssh SSH host keys shared"; rc=1; fi
fi
echo "Serial console logs: $work/out/console-*.log"
exit $rc
