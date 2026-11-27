#!/usr/bin/env python3
"""Kiosk (/kiosk) check against the real Go panel on loopback.

Builds an isolated fixture (pistar.conf copy with every absolute path moved
under a temporary root, an MMDVMHost INI, MMDVM-<UTC date>.log with calls,
DMRIds.dat and TGList_BM.txt), starts the panel and the operator listener,
and drives headless Chromium through idle -> on air -> connection lost ->
recovery at 1920x1080, 1280x720 and 1080x1920. Saves one screenshot per
state and resolution and fails on page overflow, missing names, polling
while hidden or no recovery. Nothing outside the temporary root is touched.

  python3 qa/kiosk_screens.py --out DIR [--dist dist]
"""

import argparse
import base64
from datetime import datetime, timedelta, timezone
import json
import re
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import time
from urllib.request import ProxyHandler, build_opener

sys.path.insert(0, str(Path(__file__).resolve().parent))
from smoke import browser, wait  # noqa: E402

ROOT = Path(__file__).resolve().parents[1]
SIZES = [(1920, 1080), (1280, 720), (1080, 1920)]
HTTP = build_opener(ProxyHandler({}))

MMDVM_INI = """[General]
Callsign=UT3UDX
Id=2550430
Duplex=0
RFModeHang=10
NetModeHang=3
[Info]
RXFrequency=433800000
TXFrequency=433800000
[Modem]
RXFrequency=433800000
TXFrequency=433800000
[D-Star]
Enable=1
[DMR]
Enable=1
ColorCode=1
[System Fusion]
Enable=1
[DMR Network]
Enable=1
Type=Direct
Address=127.0.0.1
Slot1=1
Slot2=1
"""

# ID TAB callsign TAB first name, as pistar.uk's DMRIds.dat.
DMR_IDS = """2550430\tUT3UDX\tMax
2550431\tUR5ABC\tOleksandr
2550432\tUT7XYZ\tIryna
2550433\tUS0QRP\tBohdan
2341234\tM0KSK\tKate
2341235\tG4XYZ\tGeorge
3100001\tW1AW\tHiram
2620001\tDL1ABC\tJürgen
2620002\tDG1ABC\tAnke
2080001\tF4ABC\tCamille
2130001\tSP3XYZ\tKrzysztof Wojciechowski-Nowak
"""

TG_LIST = """# Dest ID;Option;Name;Description
#
# Option: TG:0, REF:1, PC:2
#
91;0;WORLD-WIDE;TG91
255;0;UKRAINE;TG255
2550;0;UKRAINE-NATIONAL;TG2550
25506;0;ZHYTOMYRSKA-OBL;TG25506
262;0;DEUTSCHLAND;TG262
9990;2;PARROT;PC9990
"""


def stamp(t):
    return t.strftime("%Y-%m-%d %H:%M:%S.") + f"{t.microsecond // 1000:03d}"


def dmr(start, slot, src, frm, to, seconds, ber=0.0, loss=0, rssi=-75):
    end = start + timedelta(seconds=seconds)
    direction = "RF" if src == "RF" else "network"
    lines = [f"M: {stamp(start)} DMR Slot {slot}, received {direction} voice header from {frm} to {to}"]
    if src == "RF":
        lines.append(f"M: {stamp(end)} DMR Slot {slot}, received RF end of voice transmission from {frm} to {to}, "
                     f"{seconds:.1f} seconds, BER: {ber:.1f}%, RSSI: {rssi}/{rssi}/{rssi} dBm")
    else:
        lines.append(f"M: {stamp(end)} DMR Slot {slot}, received network end of voice transmission from {frm} to {to}, "
                     f"{seconds:.1f} seconds, {loss}% packet loss, BER: {ber:.1f}%")
    return lines


def calls(now):
    t = lambda minutes: now - timedelta(minutes=minutes)  # noqa: E731
    lines = []
    lines += dmr(t(71), 2, "Net", "W1AW", "TG 91", 22.4, loss=0)
    lines += dmr(t(64), 2, "Net", "DL1ABC", "TG 262", 9.1, loss=4, ber=0.6)
    lines += [f"M: {stamp(t(58))} D-Star, received RF header from G4XYZ   /ID51 to CQCQCQ  ",
              f"M: {stamp(t(58) + timedelta(seconds=6.3))} D-Star, received RF end of transmission from G4XYZ   /ID51 to CQCQCQ  , "
              "6.3 seconds, BER: 0.4%, RSSI: -91/-91/-91 dBm"]
    lines += [f"M: {stamp(t(51))} YSF, received network data from DG1ABC     to ALL        at DG1ABC    ",
              f"M: {stamp(t(51) + timedelta(seconds=11.7))} YSF, received network end of transmission from DG1ABC     to ALL       , "
              "11.7 seconds, 1% packet loss, BER: 0.0%"]
    lines += dmr(t(44), 1, "Net", "F4ABC", "TG 91", 4.2, loss=0)
    lines += dmr(t(37), 2, "RF", "US0QRP", "TG 2550", 3.8, ber=2.6, rssi=-108)
    lines += dmr(t(30), 2, "Net", "SP3XYZ", "TG 91", 18.0, loss=0)
    lines += dmr(t(24), 2, "Net", "2340999", "TG 9990", 2.0, loss=0)
    lines += dmr(t(18), 2, "RF", "UT7XYZ", "TG 25506", 7.6, ber=0.0, rssi=-63)
    lines += dmr(t(12), 2, "Net", "M0KSK", "TG 255", 12.5, loss=0, ber=0.1)
    lines += dmr(t(7), 2, "RF", "UR5ABC", "TG 255", 5.4, ber=0.3, rssi=-83)
    return lines


def free_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def fixture(root, dist, now):
    """Copy pistar.conf with every absolute path below root."""
    conf, ports = [], {"PANEL_PORT": free_port(), "OPERATOR_PORT": free_port(), "MANAGEMENT_PORT": free_port()}
    for line in (ROOT / "pistar.conf").read_text().splitlines():
        key, sep, value = line.partition("=")
        if not sep or line.startswith("#"):
            conf.append(line)
            continue
        if key in ports:
            value = str(ports[key])
        elif key == "DASHBOARD_UI_DIR":
            value = str(dist)
        elif key == "RADIO_LOG_SOURCE":
            value = "files"
        elif value.startswith("/"):
            value = str(root) + value
        conf.append(f"{key}={value}")
    (root / "pistar.conf").write_text("\n".join(conf) + "\n")
    files = {
        "etc/mmdvmhost": MMDVM_INI, "etc/dstar-radio.mmdvmhost": "", "etc/hostname": "pistar-kiosk\n",
        "etc/pistar-release": "[Pi-Star]\nVersion = 4.3.0\n",
        "usr/local/etc/DMRIds.dat": DMR_IDS, "usr/local/etc/TGList_BM.txt": TG_LIST,
        f"var/log/pi-star/MMDVM-{now:%Y-%m-%d}.log": "\n".join(calls(now)) + "\n",
    }
    for name, body in files.items():
        path = root / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(body)
    return root / "pistar.conf", ports, root / f"var/log/pi-star/MMDVM-{now:%Y-%m-%d}.log"


def get_json(url):
    with HTTP.open(url, timeout=3) as r:
        return json.load(r)


def metric(url):
    with HTTP.open(url + "/metrics", timeout=3) as r:
        for line in r.read().decode().splitlines():
            if line.startswith("pistar_operator_requests_total "):
                return int(line.split()[1])
    raise AssertionError("no request counter")


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--out", required=True, type=Path)
    parser.add_argument("--dist", default=ROOT / "dist", type=Path)
    args = parser.parse_args()
    dist = args.dist.resolve()
    if not (dist / "index.html").is_file():
        raise SystemExit(f"build the dashboard first: {dist}/index.html missing")
    args.out.mkdir(parents=True, exist_ok=True)
    results = {"screenshots": [], "checks": []}

    with tempfile.TemporaryDirectory(prefix="kiosk-fixture-") as tmp:
        root = Path(tmp)
        now = datetime.now(timezone.utc)
        conf, ports, log = fixture(root, dist, now)
        panel = root / "pistar-panel"
        subprocess.run(["go", "build", "-o", str(panel), "."], cwd=ROOT.parent / "panel", check=True)
        # A process named MMDVMHost, so the dashboard reports "Listening".
        fake = root / "bin" / "MMDVMHost"
        fake.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy("/bin/sleep", fake)
        procs = []

        def start():
            procs.append(subprocess.Popen([str(panel), "-config", str(conf), "-listen", f"127.0.0.1:{ports['PANEL_PORT']}"]))
            procs.append(subprocess.Popen([str(panel), "-config", str(conf), "-operator-listen", f"127.0.0.1:{ports['OPERATOR_PORT']}"]))

        def stop():
            while procs:
                p = procs.pop()
                p.terminate()
                p.wait(5)

        modem = subprocess.Popen([str(fake), "900"])
        origin = f"http://127.0.0.1:{ports['OPERATOR_PORT']}"
        try:
            start()

            def heard():
                try:
                    d = get_json(origin + "/api/dashboard")
                except OSError:
                    return None
                rows = d.get("Heard") or []
                return d if len(rows) >= 11 and any(r.get("name") for r in rows) else None
            d = wait("panel did not load the fixture calls and names", heard, timeout=20)
            named = {r["callsign"]: (r["name"], r["targetName"]) for r in d["Heard"]}
            expect = {"UR5ABC": ("Oleksandr", "UKRAINE"), "UT7XYZ": ("Iryna", "ZHYTOMYRSKA-OBL"), "2340999": ("", "PARROT"), "G4XYZ": ("George", "")}
            for call, want in expect.items():
                if named.get(call) != want:
                    raise AssertionError(f"{call}: {named.get(call)} != {want}")
            results["checks"].append(f"/api/dashboard names: {expect}")

            with browser() as command:
                def js(script, *a):
                    return command("/execute/sync", {"script": script, "args": list(a)})

                def size(w, h):
                    command("/goog/cdp/execute", {"cmd": "Emulation.setDeviceMetricsOverride", "params": {
                        "width": w, "height": h, "deviceScaleFactor": 1, "mobile": False}})
                    time.sleep(0.4)

                def state():
                    return js("return document.querySelector('.kiosk')?.dataset.state || null")

                def layout(w, h):
                    m = js("""const d = document.documentElement, k = document.querySelector('.kiosk').getBoundingClientRect();
                      const cut = [...document.querySelectorAll('.callsign, .headline, .fact .v, .row .cs, .row .tg, .row .mode, .row .dur, .row .qc')]
                        .filter(e => e.scrollWidth > e.clientWidth + 1).map(e => e.className + ':' + e.textContent.trim());
                      const out = [...document.querySelectorAll('.kiosk, .kiosk > *, .card > *, .calls .row')].filter(e => {
                        const r = e.getBoundingClientRect();
                        return r.left < -0.5 || r.top < -0.5 || r.right > innerWidth + 0.5 || r.bottom > innerHeight + 0.5
                          || (e.matches('.kiosk, .kiosk > *, .card > *') && (e.scrollWidth > e.clientWidth + 1 || e.scrollHeight > e.clientHeight + 1));
                      }).map(e => e.className);
                      return {iw: innerWidth, ih: innerHeight, sw: d.scrollWidth, sh: d.scrollHeight, kw: k.width, kh: k.height, out,
                        overflowY: getComputedStyle(document.body).overflow, cut,
                        rows: document.querySelectorAll('.calls .row:not(.head):not(.filler)').length,
                        callsignPx: parseFloat(getComputedStyle(document.querySelector('.callsign, .headline')).fontSize),
                        rowPx: parseFloat(getComputedStyle(document.querySelector('.calls .row:not(.head)') || document.body).fontSize)}""")
                    if m["iw"] != w or m["ih"] != h or m["sw"] > w or m["sh"] > h or m["kw"] > w or m["kh"] > h or m["out"]:
                        raise AssertionError(f"{w}x{h} overflows: {m}")
                    if m["cut"]:
                        raise AssertionError(f"{w}x{h} truncated: {m['cut']}")
                    return m

                def shoot(name, w, h):
                    png = command("/goog/cdp/execute", {"cmd": "Page.captureScreenshot", "params": {"format": "png"}})["data"]
                    path = (args.out / f"kiosk-{name}-{w}x{h}.png").resolve()
                    path.write_bytes(base64.b64decode(png))
                    results["screenshots"].append(str(path))
                    m = layout(w, h)
                    results["checks"].append(f"{name} {w}x{h}: rows={m['rows']} big={m['callsignPx']}px list={m['rowPx']}px no overflow")

                size(*SIZES[0])
                command("/url", {"url": origin + "/kiosk"})
                wait("kiosk did not reach idle", lambda: state() == "idle", timeout=10)
                text = js("return document.querySelector('.card').innerText")
                if "Listening" not in text or not re.search(r"Last call [67] min ago", text):
                    raise AssertionError(f"idle card: {text!r}")
                for w, h in SIZES:
                    size(w, h)
                    shoot("idle", w, h)

                # Polling stops while the tab is hidden and resumes when shown.
                js("Object.defineProperty(document, 'hidden', {configurable: true, get: () => true}); document.dispatchEvent(new Event('visibilitychange'))")
                time.sleep(0.5)
                before = metric(origin)
                time.sleep(3)
                hidden = metric(origin) - before - 1  # the second /metrics read
                js("Object.defineProperty(document, 'hidden', {configurable: true, get: () => false}); document.dispatchEvent(new Event('visibilitychange'))")
                before = metric(origin)
                time.sleep(3)
                shown = metric(origin) - before - 1
                if hidden != 0 or shown < 2:
                    raise AssertionError(f"requests while hidden={hidden}, shown={shown}")
                results["checks"].append(f"hidden tab: {hidden} polls in 3 s; visible again: {shown} polls in 3 s")

                # A local RF call starts (no end line yet).
                start_at = datetime.now(timezone.utc) - timedelta(seconds=37)
                with log.open("a") as f:
                    f.write(f"M: {stamp(start_at)} DMR Slot 2, received RF voice header from UT3UDX to TG 25506\n")
                size(*SIZES[0])
                wait("kiosk did not show the call on air", lambda: state() == "onair", timeout=10)
                text = js("return document.querySelector('.card').innerText")
                for part in ("ON AIR", "UT3UDX", "Max", "TG 25506", "ZHYTOMYRSKA-OBL", "DMR", "TS2", "ON AIR · RF"):
                    if part not in text:
                        raise AssertionError(f"on-air card lacks {part!r}: {text!r}")
                first = js("return document.querySelector('.timer .v').textContent")
                time.sleep(2.2)
                second = js("return document.querySelector('.timer .v').textContent")
                if first == second:
                    raise AssertionError(f"duration counter stuck at {first}")
                results["checks"].append(f"on air: counter {first} -> {second}")
                for w, h in SIZES:
                    size(w, h)
                    shoot("onair", w, h)

                # The board goes away.
                stop()
                wait("kiosk did not report the lost connection", lambda: state() == "lost", timeout=12)
                for w, h in SIZES:
                    size(w, h)
                    shoot("lost", w, h)
                start()
                wait("kiosk did not recover", lambda: state() in ("onair", "idle"), timeout=15)
                results["checks"].append(f"recovered to {state()} after restart")
        finally:
            stop()
            modem.terminate()
            modem.wait(5)
    (args.out / "kiosk-results.json").write_text(json.dumps(results, indent=2) + "\n")
    print(json.dumps(results, indent=2))


if __name__ == "__main__":
    main()
