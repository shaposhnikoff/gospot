#!/usr/bin/env bash
# Build the shared libraries the Pi-Star PS4 binaries link against but Debian
# does not ship (libwiringPi, libwiringPiDev, libArduiPi_OLED), for armhf, in
# a container of the target Debian release. Output: radio/build/<suite>/lib/.
set -euo pipefail
cd "$(dirname "$0")"
suite=${1:-bookworm}
[[ $suite =~ ^(bookworm|trixie)$ ]] || { echo "usage: $0 [bookworm|trixie]" >&2; exit 2; }
out=$PWD/build/$suite/lib
rm -rf "$out"; mkdir -p "$out"

# Pinned sources (same wiringPi release Pi-Star installs).
wiringpi_rev=4639b7ac45ff87a9c2271a3d44f7fccb618c88ff # tag 3.14
oled_rev=4119148b051a6975816de2319eb6d11f6769499d     # hallard/ArduiPi_OLED

docker run --rm --platform linux/arm/v7 -v "$out:/out" \
  -e WIRINGPI_REV="$wiringpi_rev" -e OLED_REV="$oled_rev" \
  "debian:$suite-slim" bash -euo pipefail -c '
    apt-get update -qq
    apt-get install -y -qq --no-install-recommends build-essential git ca-certificates libi2c-dev >/dev/null
    fetch() { git init -q "$1"; git -C "$1" fetch -q --depth=1 "$2" "$3"; git -C "$1" checkout -q FETCH_HEAD; }
    fetch /src/wiringpi https://github.com/WiringPi/WiringPi.git "$WIRINGPI_REV"
    fetch /src/oled https://github.com/hallard/ArduiPi_OLED.git "$OLED_REV"
    make -s -C /src/wiringpi/wiringPi install >/dev/null
    make -s -C /src/wiringpi/devLib install >/dev/null
    # Upstream flags omit -marm; Debian armhf compilers default to Thumb.
    make -s -C /src/oled CCFLAGS="-O2 -marm -march=armv6zk -mfpu=vfp -mfloat-abi=hard" >/dev/null
    ldconfig
    cp -a /usr/local/lib/libwiringPi.so* /usr/local/lib/libwiringPiDev.so* /usr/local/lib/libArduiPi_OLED.so* /out/
    chown -R '"$(id -u):$(id -g)"' /out
  '
ls -l "$out"
