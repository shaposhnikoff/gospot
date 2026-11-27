#!/usr/bin/env bash
# Rebuild the Pi-Star D-Star daemons (ircddbgatewayd, timeserverd,
# starnetserverd, remotecontrold, timercontrold, aprs/text/voicetransmitd,
# dstarrepeaterd) for armhf against the target release's wxWidgets 3.2; the
# PS4 builds need wxWidgets 3.0, which Bookworm and Trixie no longer ship.
# Revisions match the PS4 builds (20240107_PS4) so /etc/ircddbgateway and
# /etc/dstarrepeater keep the Pi-Star format. Output: radio/build/<suite>/dstar/.
set -euo pipefail
cd "$(dirname "$0")"
suite=${1:-bookworm}
[[ $suite =~ ^(bookworm|trixie)$ ]] || { echo "usage: $0 [bookworm|trixie]" >&2; exit 2; }
# shellcheck source=../Pi-Star_dash_new/pistar.conf
source ../Pi-Star_dash_new/pistar.conf
out=$PWD/build/$suite/dstar
rm -rf "$out"; mkdir -p "$out"

ircddb_rev=d057f79bc4c29e9e41f7118db8910f77770b8812   # g4klx/ircDDBGateway 2023-07-23, before MQTT
starnet_rev=5a489bff1ed5b59b4a9d88f2da36ae95cef42c93  # last revision with StarNetServer
dstar_rev=fea6b5a87c4e8e94c1a060669fd3c0b57d08956f    # g4klx/DStarRepeater, wxWidgets generation

docker run --rm --platform linux/arm/v7 -v "$out:/out" \
  -e IRCDDB_REV="$ircddb_rev" -e STARNET_REV="$starnet_rev" -e DSTAR_REV="$dstar_rev" \
  -e IRCDDB_DATA="$DSTAR_IRCDDB_DATA_DIR" -e DSTAR_DATA="$DSTAR_REPEATER_DATA_DIR" -e LOGS="$PISTAR_LOG_DIR" \
  "debian:$suite-slim" bash -euo pipefail -c '
    apt-get update -qq
    apt-get install -y -qq --no-install-recommends build-essential git ca-certificates libwxgtk3.2-dev \
      libasound2-dev libusb-1.0-0-dev >/dev/null
    fetch() { git init -q "$1"; git -C "$1" fetch -q "$2" "$3"; git -C "$1" checkout -q FETCH_HEAD; }
    fetch /src/ircddb https://github.com/g4klx/ircDDBGateway.git "$IRCDDB_REV"
    fetch /src/starnet https://github.com/g4klx/ircDDBGateway.git "$STARNET_REV"
    fetch /src/dstar https://github.com/g4klx/DStarRepeater.git "$DSTAR_REV"
    jobs=$(nproc)
    flags="BUILD=release LOGDIR=$LOGS CONFDIR=/etc"
    make -s -j"$jobs" -C /src/ircddb $flags DATADIR="$IRCDDB_DATA" >/dev/null
    make -s -j"$jobs" -C /src/starnet $flags DATADIR="$IRCDDB_DATA" Common/Common.a ircDDB/IRCDDB.a >/dev/null
    make -s -j"$jobs" -C /src/starnet/StarNetServer $flags DATADIR="$IRCDDB_DATA" >/dev/null
    make -s -j"$jobs" -C /src/dstar $flags DATADIR="$DSTAR_DATA" DStarRepeater/dstarrepeaterd >/dev/null
    mkdir -p /out/bin /out/ircddbgateway /out/dstarrepeater
    cp /src/ircddb/ircDDBGateway/ircddbgatewayd /src/ircddb/TimeServer/timeserverd /src/ircddb/RemoteControl/remotecontrold \
       /src/ircddb/TimerControl/timercontrold /src/ircddb/APRSTransmit/aprstransmitd /src/ircddb/TextTransmit/texttransmitd \
       /src/ircddb/VoiceTransmit/voicetransmitd /src/starnet/StarNetServer/starnetserverd /src/dstar/DStarRepeater/dstarrepeaterd /out/bin/
    cp /src/ircddb/Data/*.ambe /src/ircddb/Data/*.indx /out/ircddbgateway/
    cp /src/dstar/Data/*.ambe /src/dstar/Data/*.indx /out/dstarrepeater/ 2>/dev/null || true
    strip /out/bin/*
    for b in /out/bin/*; do ldd "$b" | grep -q "not found" && { echo "unresolved libraries in $b" >&2; exit 1; }; done
    ldd /out/bin/ircddbgatewayd | grep -o "libwx[^ ]*" | sort -u > /out/wx-libs
    chown -R '"$(id -u):$(id -g)"' /out
  '
ls -l "$out/bin"; cat "$out/wx-libs"
