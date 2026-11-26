#!/usr/bin/env python3
"""Run against a local dashboard: python3 qa/smoke.py [http://127.0.0.1:5173/].

Requires Chrome/Chromium and a compatible chromedriver on PATH. Override their
locations with CHROME_BIN and CHROMEDRIVER if necessary.
"""

import argparse
from contextlib import contextmanager
import ipaddress
import json
import os
import re
import shutil
import socket
import subprocess
import sys
import time
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import ProxyHandler, Request, build_opener


def local_url(value):
    try:
        u = urlsplit(value)
        _ = u.port  # Validate before handing the address to the browser.
        if (u.scheme in ("http", "https") and u.hostname and not u.username
                and not u.password and "\\" not in value and not re.search(r"\s", value)
                and (u.hostname == "localhost" or ipaddress.ip_address(u.hostname).is_loopback)):
            return value
    except ValueError:
        pass
    raise argparse.ArgumentTypeError("expected an HTTP(S) URL on loopback")


def wait(description, check, timeout=8):
    end = time.monotonic() + timeout
    while True:
        result = check()
        if result:
            return result
        if time.monotonic() >= end:
            raise AssertionError(description)
        time.sleep(0.12)


@contextmanager
def browser(*, target_origin=None, tls_pin=None):
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        port = s.getsockname()[1]
    http = build_opener(ProxyHandler({}))

    def request(method, path, body=None):
        req = Request(f"http://127.0.0.1:{port}{path}", method=method,
                      data=None if body is None else json.dumps(body).encode(),
                      headers={"Content-Type": "application/json"})
        try:
            with http.open(req, timeout=45) as response:
                return json.load(response)["value"]
        except HTTPError as e:
            raise RuntimeError(e.read().decode(errors="replace")) from e

    driver = os.environ.get("CHROMEDRIVER", "chromedriver")
    with subprocess.Popen([driver, f"--port={port}"], stdout=subprocess.DEVNULL) as proc:
        def ready():
            if proc.poll() is not None:
                raise RuntimeError(f"chromedriver exited with status {proc.returncode}")
            try:
                return request("GET", "/status").get("ready")
            except URLError:
                return False

        session = None
        try:
            wait("chromedriver did not start", ready)
            options = {"args": [
                "--headless=new", "--disable-dev-shm-usage", "--disable-background-networking",
                "--no-first-run", "--window-size=1366,900",
                # Loopback bypasses the proxy; external requests go nowhere.
                "--proxy-server=http://127.0.0.1:1",
                "--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE localhost, EXCLUDE 127.*, EXCLUDE [::1]",
            ]}
            if target_origin:
                # full_ui.py also tests explicit LAN addresses. Only the dashboard
                # origin needs direct access (wizard/action links are never opened).
                dst = urlsplit(target_origin)
                options["args"][-1] += f", EXCLUDE {dst.hostname}"
                options["args"].append(f"--proxy-bypass-list={dst.netloc}")
            if os.geteuid() == 0:
                options["args"].append("--no-sandbox")
            if tls_pin:
                assert urlsplit(target_origin).hostname == '127.0.0.1' and re.fullmatch(r'[A-Za-z0-9+/]{43}=',tls_pin)
                options['args'].append('--ignore-certificate-errors-spki-list='+tls_pin)
            binary = os.environ.get("CHROME_BIN")
            snap_chrome = "/snap/chromium/current/usr/lib/chromium-browser/chrome"
            if not binary and shutil.which("chromium") == "/snap/bin/chromium" and os.path.isfile(snap_chrome):
                # Already inside the driver's snap: use Chrome, not the snap launcher.
                binary = snap_chrome
            if binary:
                options["binary"] = binary
            caps = {"browserName": "chrome", "goog:chromeOptions": options}
            if target_origin:
                caps["goog:loggingPrefs"] = {"performance": "ALL"}
            session = request("POST", "/session", {"capabilities": {"alwaysMatch": caps}})["sessionId"]
            yield lambda path, body: request("POST", f"/session/{session}{path}", body)
        finally:
            try:
                if session:
                    request("DELETE", f"/session/{session}")
            finally:
                proc.terminate()


# Only inspect rendered UI; do not read component state or fixture APIs.
HELPERS = """
const all = (q, root=document) => [...root.querySelectorAll(q)];
const visible = e => e.getClientRects().length && getComputedStyle(e).visibility !== 'hidden';
const test = id => document.querySelector(`[data-testid="${id}"]`);
const text = e => e && visible(e) ? e.innerText.trim().replace(/\\s+/g, ' ') : '';
const button = label => all('button,[role="button"],[role="tab"]')
  .find(e => visible(e) && text(e).toLowerCase() === label.toLowerCase());
const picker = () => test('scenario') || all('select').find(s => visible(s) &&
  [...s.options].some(o => ['online','api-offline','master-timeout'].includes(o.value)));
const rows = () => all('[data-testid="heard-row"], [data-testid="heard-table"] tbody tr').filter(visible);
const states = () => ['modem', 'master'].map(k => test('status-' + k)?.dataset.state);
const stamp = () => test('last-success')?.querySelector('time[datetime]')?.dateTime;
"""


def check_view(command, url, width, height, mobile):
    def js(script, *args):
        return command("/execute/sync", {"script": HELPERS + script, "args": args})

    command("/goog/cdp/execute", {"cmd": "Emulation.setDeviceMetricsOverride", "params": {
        "width": width, "height": height, "deviceScaleFactor": 1, "mobile": mobile}})
    command("/url", {"url": url})
    wait("scenario picker missing (expected a visible select with mock scenario options)",
         lambda: js("return !!picker()"))

    def layout():
        metrics = js("""return [innerWidth, document.documentElement.clientWidth,
          Math.max(document.documentElement.scrollWidth, document.body.scrollWidth)]""")
        if abs(metrics[0] - width) > 1 or metrics[2] > metrics[1] + 1:
            raise AssertionError(f"{width}px viewport: inner/client/content widths = {metrics}")

    def click(el, label):
        if not el:
            raise AssertionError(f"missing {label!r}")
        command(f"/element/{el['element-6066-11e4-a52e-4f735466cecf']}/click", {})

    def choose(state):
        option = js("""const s = picker();
          return s && [...s.options].find(o => o.value === arguments[0]) || null""", state)
        click(option, f"scenario option {state}")
        wait(f"picker did not select {state}", lambda: js("return picker()?.value") == state)

    def check_states(modem, master):
        wait(f"expected modem={modem}, master={master}", lambda: js("return states()") == [modem, master])

    def sources():
        return js("return rows().map(r => r.dataset.source)")

    def snapshot():
        return js("return [stamp(), text(test('callsign')), states(), rows().map(text)]")

    auto = js("return test('auto-refresh')")
    if js("return test('auto-refresh')?.getAttribute('aria-checked')") != "true":
        raise AssertionError("auto-refresh should start enabled")
    click(auto, "auto-refresh")
    wait("auto-refresh switch did not turn off",
         lambda: js("return test('auto-refresh').getAttribute('aria-checked')") == "false")
    choose("online")
    check_states("up", "up")
    wait("online snapshot has no callsign or timestamp",
         lambda: js("return !!stamp() && !['', '—'].includes(text(test('callsign')))"))
    last = js("return stamp()")
    click(js("return test('refresh')"), "manual refresh")
    wait("manual refresh did not advance last success", lambda: (js("return stamp()") or "") > last)
    layout()
    original = sources()
    if set(original) != {"RF", "Net"}:
        raise AssertionError(f"expected online journal with RF and Net sources, got {original}")
    for label, source in [("RF", "RF"), ("Сеть", "Net"), ("Все", "all")]:
        click(js("return test('filter-' + arguments[1]) || button(arguments[0])", label, source), label)
        expected = original if source == "all" else [v for v in original if v == source]
        wait(f"{label}: expected rows {expected} and active button", lambda:
             sources() == expected and js("return button(arguments[0])?.getAttribute('aria-pressed')", label) == "true")
        layout()

    for state, modem, master, empty in [
        ("master-timeout", "up", "down", None),
        ("modem-down", "down", "up", None),
        ("idle", "up", "up", r"передач пока нет|нет передач"),
        ("journal-unavailable", "unknown", "unknown", r"журнал недоступен"),
    ]:
        choose(state)
        check_states(modem, master)
        if empty:
            wait(f"{state}: expected empty journal /{empty}/", lambda:
                 not sources() and re.search(empty, js("return text(test('heard-empty'))"), re.I))
        elif sources() != original:
            raise AssertionError(f"{state}: journal rows missing")
        layout()

    choose("online")
    check_states("up", "up")
    wait("online journal did not recover", lambda: sources() == original)
    saved = snapshot()
    choose("api-offline")
    wait("API outage did not show a stale-data warning", lambda: js("return text(test('stale-banner'))"))
    if snapshot() != saved:
        raise AssertionError("API outage changed last successful timestamp, callsign, states, or rows")
    layout()
    choose("online")
    wait("stale banner or stale timestamp after recovery", lambda:
         not js("return text(test('stale-banner'))") and (js("return stamp()") or "") > saved[0])
    check_states("up", "up")
    click(js("return test('auto-refresh')"), "auto-refresh")
    wait("auto-refresh did not turn back on",
         lambda: js("return test('auto-refresh').getAttribute('aria-checked')") == "true")
    layout()
    print(f"PASS {width}x{height}: six scenarios and recovery")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("url", nargs="?", type=local_url, default="http://127.0.0.1:5173/")
    args = parser.parse_args()
    with browser() as command:
        for width, height, mobile in [(1366, 900, False), (390, 844, True)]:
            try:
                check_view(command, args.url, width, height, mobile)
            except Exception as e:
                raise RuntimeError(f"{width}x{height}: {e}") from e


if __name__ == "__main__":
    try:
        main()
    except (AssertionError, OSError, RuntimeError) as exc:
        sys.exit(f"FAIL: {exc}")
