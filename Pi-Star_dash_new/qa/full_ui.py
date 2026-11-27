#!/usr/bin/env python3
"""Chrome regression suite for the operator dashboard (run from Pi-Star_dash_new).

  python3 qa/full_ui.py --url http://192.168.10.11:5173/ --expect demo
  python3 qa/full_ui.py --url http://192.168.210.194:8789/
  python3 qa/full_ui.py --fixture dist
  python3 qa/full_ui.py --operator-fixture build/release-XXXXXX/docroot

Requires the same Chrome/chromedriver setup as smoke.py. The fixture serves a
built dashboard with a synthetic Go status on an ephemeral loopback port. Live
tests follow only same-document navigation; console action links are inspected
in the DOM, never opened. No credentials or configuration API are needed.
"""

import argparse
from contextlib import nullcontext
import ipaddress
import json
from pathlib import Path
import re
import sys
from urllib.parse import urlsplit

from go_fixture import serve
from smoke import HELPERS, browser, check_view, wait


ELEMENT = "element-6066-11e4-a52e-4f735466cecf"
VIEWPORTS = [(1366, 900, False), (390, 844, True)]
PI = "192.168.210.194"
PRIVATE = tuple(ipaddress.ip_network(cidr) for cidr in
                ("10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"))


def dashboard_url(value):
    try:
        u = urlsplit(value)
        if (u.scheme not in ("http", "https") or u.path not in ("", "/")
                or u.query or u.fragment or "@" in u.netloc or "\\" in value
                or re.search(r"\s", value) or u.port == 0):
            raise ValueError
        if u.hostname != "localhost":
            ip = ipaddress.ip_address(u.hostname)
            if not ip.is_loopback and not any(ip in network for network in PRIVATE):
                raise ValueError
        return u._replace(path="/").geturl()
    except (TypeError, ValueError):
        raise argparse.ArgumentTypeError("expected a loopback or private IP dashboard URL, e.g. http://192.168.210.194:8789/") from None


def equal(name, actual, expected):
    if actual != expected:
        raise AssertionError(f"{name}: expected {expected!r}, got {actual!r}")


class View:
    def __init__(self, command, url, width, height, mobile):
        self.command, self.url, self.width = command, url, width
        self.cdp("Emulation.setDeviceMetricsOverride", {
            "width": width, "height": height, "deviceScaleFactor": 1, "mobile": mobile})
        self.events()  # Discard responses from the previous navigation.
        self.command("/url", {"url": url})

    def cdp(self, cmd, params):
        return self.command("/goog/cdp/execute", {"cmd": cmd, "params": params})

    def js(self, script, *args):
        return self.command("/execute/sync", {"script": HELPERS + script, "args": args})

    def events(self):
        return [json.loads(entry["message"])["message"]
                for entry in self.command("/log", {"type": "performance"})]

    def resource(self, path, events):
        # Compare the exact response the browser used, not a potentially newer
        # sample from an independent GET (the Go collector refreshes every 5s).
        matches = [e["params"] for e in events if e["method"] == "Network.responseReceived"
                   and e["params"]["response"]["url"] == self.url + path]
        if not matches:
            raise AssertionError(f"dashboard did not load /{path}")
        params = matches[-1]
        equal(f"GET /{path}", params["response"]["status"], 200)
        body = self.cdp("Network.getResponseBody", {"requestId": params["requestId"]})
        return json.loads(body["body"])

    def layout(self):
        sizes = self.js("""return [innerWidth, document.documentElement.clientWidth,
          Math.max(document.body.scrollWidth, document.documentElement.scrollWidth)]""")
        if abs(self.width - sizes[0]) > 1 or sizes[2] > sizes[1] + 1:
            raise AssertionError(f"horizontal overflow: inner/client/content widths {sizes}")

    def field(self, selector):
        return self.js("return all(arguments[0]).find(visible) || null", selector)

    def click(self, selector):
        element = self.field(selector)
        if not element:
            raise AssertionError(f"missing visible {selector}")
        self.command(f"/element/{element[ELEMENT]}/click", {})

    def nav(self, fragment):
        self.click(f'nav a[href="#{fragment}"]')
        wait(f"no active #{fragment} navigation", lambda: self.js("""
          const a = document.querySelector('nav a[aria-current="page"]');
          return location.href === arguments[0] && a?.hash === location.hash && visible(a);
        """, self.url + "#" + fragment))

    def type_into(self, selector, value):
        element = self.field(selector)
        if not element:
            raise AssertionError(f"missing visible {selector}")
        path = f"/element/{element[ELEMENT]}"
        # Ctrl+A then replace using native keyboard events, including input.
        keys = "\ue009a\ue000" + (value or "\ue003")
        self.command(path + "/value", {"text": keys, "value": list(keys)})


def displayed_frequency(value):
    if not value:
        return "—"
    n = float(value)
    return f"{(n / 1e6 if n >= 1e6 else n):.5f} МГц"


def live_overview(v, status):
    radio = status["Radio"]
    actual = v.js("""return {
      scenario: !!picker(), stale: text(test('stale-banner')),
      callsign: text(test('callsign')), id: text(document.querySelector('.hero-meta b')),
      modes: all('.modes span').map(text),
      fields: Object.fromEntries(all('.parameters dt').map(e => [text(e), text(e.nextElementSibling)])),
      states: states(),
      labels: ['modem','master'].map(k => text(test('status-' + k)?.querySelector('.pill'))),
      stamp: stamp(),
      heard: rows().map(r => [r.dataset.source, text(r.querySelector('strong')), text(r.querySelector('small'))]),
      empty: text(test('heard-empty'))
    }""")
    equal("live scenario picker", actual["scenario"], False)
    equal("live stale warning", actual["stale"], "")
    equal("last successful status timestamp", actual["stamp"], status["UpdatedAt"])
    equal("callsign", actual["callsign"], radio["Callsign"] or "—")
    equal("DMR ID", actual["id"], radio["ID"] or "—")
    equal("radio modes", actual["modes"], radio["Modes"] or [])
    fields = {"Узел": status["Node"], "Режим работы": radio["Operation"],
              "Приём · RX": displayed_frequency(radio["RX"]), "Передача · TX": displayed_frequency(radio["TX"]),
              "Цветовой код": radio["CC"], "Таймслоты": radio["Slots"]}
    for k, value in fields.items():
        if k not in actual["fields"] or actual["fields"][k] not in ([value] if value else ["", "—"]):
            raise AssertionError(f"{k}: expected {value!r}, got {actual['fields'].get(k)!r}")
    states = [status[k]["State"] for k in ("Modem", "Master")]
    equal("modem/master states", actual["states"], states)
    labels = {"up": "На связи", "down": "Нет связи", "unknown": "Неизвестно"}
    equal("visible modem/master labels", actual["labels"], [labels[s] for s in states])
    heard = status["Heard"] or []
    equal("heard sources/routes", actual["heard"], [[r["Source"], r["From"], "→ " + r["To"]]
          for r in heard if status["HeardAvailable"]])
    if not status["HeardAvailable"] or not heard:
        label = "Передач пока нет" if status["HeardAvailable"] else "Журнал недоступен"
        if label not in actual["empty"]:
            raise AssertionError(f"expected journal state {label!r}: {actual['empty']!r}")


def overview(v, expected):
    wait("overview did not render the first successful snapshot", lambda: v.js("return stamp()"))
    events = v.events()
    runtime = v.resource("runtime.json", events)
    backend = runtime.get("backend")
    if backend not in ("demo", "gospot-go"):
        raise AssertionError(f"unknown /runtime.json backend: {backend!r}")
    mode = "live" if backend == "gospot-go" else "demo"
    if expected:
        equal("runtime mode", mode, expected)
    if mode == "live":
        equal("Pi wizard port", runtime.get("wizardPort"), 8788)
        status = v.resource("api/status", events)
        live_overview(v, status)
        equal("live runtime badge", v.js("return text(document.querySelector('.demo-badge'))"),
              "В эфире · " + (status["Node"] or "Raspberry Pi"))
    else:
        equal("demo badge", v.js("return text(document.querySelector('.demo-badge'))"), "Локальный прототип")
        equal("demo callsign", v.js("return text(test('callsign'))"), "DEMO-NODE")
        equal("visible scenario picker", v.js("return !!picker() && !!visible(picker())"), True)
        if any(e["method"] == "Network.requestWillBeSent" and
               e["params"]["request"]["url"] == v.url + "api/status" for e in events):
            raise AssertionError("demo requested the live status endpoint")
    v.layout()
    same_row = v.js("""const r = k => test('status-' + k).getBoundingClientRect().top;
                       return Math.abs(r('modem') - r('master')) < 2""")
    equal("responsive overview cards share a row", same_row, v.width >= 640)
    return mode


def fixture_transitions(v, case):
    v.click('[data-testid="auto-refresh"]')
    equal("fixture polling paused", v.field('[role="switch"][aria-checked="false"]') is not None, True)
    for state in ["journal-unavailable", "idle", "api-offline", "online"]:
        previous = v.js("return [stamp(), text(test('callsign')), states(), rows().map(text), text(test('heard-empty'))]")
        v.events()
        case["state"] = state
        v.click('[data-testid="refresh"]')
        if state == "api-offline":
            wait("no live API stale warning", lambda: v.js("return /API узла недоступен/i.test(text(test('stale-banner')))"))
            equal("last successful snapshot after HTTP 503",
                  v.js("return [stamp(), text(test('callsign')), states(), rows().map(text), text(test('heard-empty'))]"),
                  previous)
        else:
            wait(f"{state}: did not display a new status", lambda: v.js("return stamp()") != previous[0])
            live_overview(v, v.resource("api/status", v.events()))
        v.layout()


def tid(name):
    return f'[data-testid="{name}"]'


def operator_feed(v, case):
    endpoint = "/api/last_heard.php?num_transmissions=20"
    def feed():
        return v.js("""const f=document.querySelector('.feed');
          return [text(f?.querySelector('p')), text(f?.querySelector('small')),
                  all('.feed tbody tr').map(r=>Array.from(r.cells,text))]""")

    def seen(status):
        url = urlsplit(v.url)
        expected = f"{url.scheme}://{url.netloc}{endpoint}"
        wait(f"missing HTTP {status} for {endpoint}", lambda: any(
            e["method"]=="Network.responseReceived" and e["params"]["response"]["url"]==expected and
            e["params"]["response"]["status"]==status for e in v.events()))

    unavailable = "Не удалось прочитать журнал."
    wait("cold-start outage", lambda: feed()==[unavailable, "", []])
    seen(503)
    print(f"PASS {v.width}px /operator/: first request HTTP 503, error without fictitious data",flush=True)
    for state in ("online", "api-offline", "recovered", "invalid", "idle", "online"):
        previous = feed()
        case["state"] = state
        if state in ("api-offline", "invalid"):
            wait(state, lambda: feed()==[unavailable+" Показан последний снимок.", *previous[1:]], timeout=13)
            seen(503 if state=="api-offline" else 200)
        else:
            if state == "idle":
                wait("empty journal", lambda: feed()[0]=="В журнале нет вызовов." and not feed()[2],timeout=13)
            else:
                call = "QA7RF" if state=="online" else "QA7NEW"
                row = ["2026-01-15 12:00:00","DMR Slot 2",call,"TG 99","RF","3.0","0%","0.2%","—"]
                wait(state, lambda: feed()[0]=="" and feed()[1].startswith("Получено:") and feed()[2]==[row],timeout=13)
            seen(200)
        v.layout()
        print(f"PASS {v.width}px {endpoint}: {state}; visible feed verified",flush=True)


def console(v, mode):
    search = tid("console-search")
    v.nav("console")
    wait("console search missing", lambda: v.field(search))
    equal("overview hidden on console", v.field(tid("callsign")), None)
    v.layout()

    def ids(prefix):
        return v.js("""return all(`[data-testid^="${arguments[0]}"]`).filter(visible)
                     .map(el => el.dataset.testid.slice(arguments[0].length))""", "console-" + prefix + "-")

    groups = ids("group")
    samples = {"monitor": "heard", "networks": "tgif", "config": "radio", "wifi": "wifi-info",
               "service": "backup", "quick": "quick-dmrgateway", "files": "full-dmrgateway"}
    if not samples.keys() <= set(groups):
        raise AssertionError(f"incomplete console groups: {groups}")
    equal("group filters match sections", ids("filter"), ["all", *groups])
    all_items = ids("item")
    equal("unique console item IDs", len(all_items), len(set(all_items)))
    equal("console overview count", v.js("return text(document.querySelector('.catalog .count'))"),
          f"Найдено: {len(all_items)}")
    equal("catalog reflows into columns", v.js("""
      const [a,b] = all('[data-testid^="console-group-"] li');
      return Math.abs(a.getBoundingClientRect().top - b.getBoundingClientRect().top) < 2
    """), v.width >= 640)
    # A fresh dashboard origin must not point stock PHP actions at the Go server.
    if mode=="operator":
        equal("integrated origin", v.js("return test('console-target')?.querySelector('code').innerText"), v.js("return location.origin"))
        equal("stock link targets", v.js("""return all('.route-group li a').length>0 &&
            all('.route-group li a').every(a=>a.origin===location.origin) &&
            test('console-link-configuration').pathname==='/admin/configure.php'"""), True)
    else:
        equal("initial stock links", v.js("""return all('.route-group li a[href]:not([data-testid^="console-native-"])').length"""), 0)
    equal("ported routes on this backend", set(ids("native")),
          {"dashboard","heard","last-heard-api","admin","hardware","sysinfo","log","configuration","backup"} if mode=="live" else set())
    wizard = v.js("return test('console-link-wizard')?.href || null")
    host = urlsplit(v.url).hostname
    authority = f"[{host}]" if ":" in host else host
    equal("wizard destination", wizard, f"https://{authority}:8788/" if mode == "live" else None)
    if mode == "live":
        equal("Pi overview destination", v.js("return test('console-link-overview')?.href"), v.url + "#overview")
    for group in groups:
        v.click(tid("console-filter-" + group))
        equal(f"{group} filter", ids("group"), [group])
        equal(f"{group} pressed", v.js("return all('.group-controls [aria-pressed=true]').map(text)"),
              [v.js("return text(test(arguments[0]))", "console-filter-" + group)])
        if not ids("item") or group in samples and samples[group] not in ids("item"):
            raise AssertionError(f"incomplete {group} results")
        v.layout()

    v.click(tid("console-filter-all"))
    v.type_into(search, "tGiF")
    if not {"tgif", "tgif-static", "full-tgifapikey"} <= set(ids("item")) or "backup" in ids("item"):
        raise AssertionError("cross-group case-insensitive TGIF search did not narrow the catalog")
    v.click(tid("console-filter-networks"))
    equal("group + search intersection", set(ids("item")), {"tgif", "tgif-static"})
    v.type_into(search, "no-such-gospot-entry")
    equal("unmatched search", ids("item"), [])
    equal("empty results message", v.js("return text(document.querySelector('.no-results strong'))"), "Ничего не найдено")
    v.click(".no-results button")
    equal("reset clears search", v.js("return test('console-search').value"), "")
    equal("reset restores catalog", ids("item"), all_items)
    if mode == "demo":
        # Exercise browser-local origin settings. Do not visit the destination.
        origin = tid("console-origin")
        v.type_into(origin, v.url)
        v.click(tid("console-origin-save"))
        equal("reject self-referential backend", v.js("return test('console-origin').getAttribute('aria-invalid')"), "true")
        v.type_into(origin, "https://qa-stock.invalid")
        v.click(tid("console-origin-save"))
        equal("stock route construction", v.js("return test('console-link-sysinfo')?.href"),
              "https://qa-stock.invalid/admin/sysinfo.php")
    v.command("/refresh", {})  # Cold-load #console, including runtime detection.
    wait("console deep link did not survive reload", lambda: v.field(search))
    if mode == "demo":
        wait("stock address did not survive reload", lambda: v.field(tid("console-target")))
        v.click(tid("console-origin-clear"))
        equal("stock links after forgetting address", v.js("return all('.route-group li a').length"), 0)
    elif mode=="operator":
        equal("station status link",v.js("return document.querySelector('nav a').getAttribute('href')"),"/")
        v.command("/back", {})
        wait("Back lost operator catalog",lambda: v.js("return location.hash")=="" and v.field(search))
        v.command("/forward", {})
        wait("Forward lost console",lambda: v.js("return location.hash")=="#console" and v.field(search))
        v.layout()
        print(f"PASS {v.width}px /operator/: {len(groups)} groups, {len(all_items)} items, filters, search, reset, layout, reload and Back/Forward",flush=True)
        return
    else:
        wait("live console deep link lost wizard", lambda: v.field(tid("console-link-wizard")))
    v.nav("overview")
    wait("overview not restored", lambda: v.js("return stamp()"))
    v.command("/back", {})
    wait("Back did not return to console", lambda: v.field(search) and v.js("return location.hash") == "#console")
    v.command("/forward", {})
    wait("Forward did not restore overview", lambda: v.js("return stamp() && location.hash === '#overview'"))
    v.layout()


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    target = parser.add_mutually_exclusive_group()
    target.add_argument("--url", type=dashboard_url, default="http://127.0.0.1:5173/")
    target.add_argument("--fixture", metavar="DIST", type=Path, help="serve DIST with a synthetic Go backend")
    target.add_argument("--operator-fixture", metavar="DOCROOT", type=Path, help="serve packaged /operator/ with a synthetic Last Heard API")
    parser.add_argument("--expect", choices=["demo", "live"], help="fail if the detected backend differs")
    args = parser.parse_args()
    pi = urlsplit(args.url).hostname == PI
    if args.expect == "demo" and (args.fixture or pi):
        parser.error("the Pi and --fixture require the live backend")
    expected = "live" if args.fixture or pi else args.expect
    target = (serve(args.operator_fixture, operator=True) if args.operator_fixture else
              serve(args.fixture) if args.fixture else nullcontext((args.url, None)))
    with target as (url, case), browser(target_origin=url.rstrip("/")) as command:
        for width, height, mobile in VIEWPORTS:
            try:
                if args.operator_fixture:
                    case["state"] = "api-offline"
                v = View(command, url, width, height, mobile)
                if args.operator_fixture:
                    print(f"Testing {args.operator_fixture}/operator/ at {width}x{height}",flush=True)
                    operator_feed(v, case)
                    console(v, "operator")
                    continue
                mode = overview(v, expected)
                if case is not None:
                    fixture_transitions(v, case)
                console(v, mode)
                print(f"PASS {width}x{height}: {mode} overview, console, search, groups, navigation", flush=True)
                if mode == "demo":
                    check_view(command, url, width, height, mobile)
            except (AssertionError, RuntimeError, ValueError, KeyError) as exc:
                raise AssertionError(f"{width}x{height} {url}: {exc}") from exc


if __name__ == "__main__":
    try:
        main()
    except (AssertionError, OSError, RuntimeError, ValueError) as exc:
        sys.exit(f"FAIL: {exc}")
