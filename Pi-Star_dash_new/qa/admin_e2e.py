#!/usr/bin/env python3
"""Native /manage/ Chromium E2E against synthetic data over loopback HTTPS.

From Pi-Star_dash_new: npm run build && python3 qa/admin_e2e.py
Or pass --dist PATH to test a specific Vite build. Requires Python 3, openssl,
Chromium and matching chromedriver (see CHROME_BIN / CHROMEDRIVER in smoke.py).
No target URL option. No Pi, Go service, privileged helper or device files used.
"""

import argparse
from contextlib import closing
from http.client import HTTPSConnection
import json
from pathlib import Path
import ssl
import sys
from tempfile import TemporaryDirectory
from urllib.parse import urlsplit

from fixtures.admin_https import (
    ADMIN_PASS, ARCHIVE_PASS, BASIC, CONFIG, INITIAL, LOG_TEXT, NETWORK_PASS, RECEIPT, ROWS, TOKEN, ZIP, serve,
)
from fixtures.chromium_https import chromium
from full_ui import ELEMENT, View, equal, tid
from smoke import wait


IMPORTED = dict(callsign="QA7IMPORT", radio_id=1234567, frequency=438250000,
                uart="/dev/ttyUSB2", claim=98765, master="new.qa.invalid", port=62032)


def backup(cfg):
    return {"scope": "pi-star.station-settings", "version": 1, "config": cfg}


def cdp(command, cmd, **params):
    return command("/goog/cdp/execute", {"cmd": cmd, "params": params})


class Admin(View):
    def settings(self, key):
        self.click(tid("settings-" + key))

    def notice(self, key, fragment):
        wait(f"expected {key} message containing {fragment!r}", lambda:
             fragment in self.js("return text(test(arguments[0]))", "settings-" + key))

    def ready(self, cfg):
        wait("editable form never showed expected server config", lambda: self.js("""
          return !test('settings-apply')?.matches(':disabled') &&
            Object.entries(arguments[0]).every(([k,v]) => test('settings-'+k)?.value === String(v))
        """, cfg))

    def disabled(self, key):
        equal(f"disabled settings-{key}",
              self.js("return test(arguments[0])?.matches(':disabled')", "settings-" + key), True)

    def upload(self, path, contents):
        path.write_text(json.dumps(contents), encoding="utf-8")
        self.choose_file(tid("settings-import-file"), path)

    def choose_file(self, selector, path):
        el = self.js("return document.querySelector(arguments[0])", selector)
        self.command(f"/element/{el[ELEMENT]}/value", {"text": str(path)})

    def check_private_state(self):
        # Only inspect visible text and persisted browser state (not Svelte internals).
        equal("secret in displayed page or web storage", self.js("""
          const s = [document.body.innerText, location.href, JSON.stringify(localStorage),
                     JSON.stringify(sessionStorage), document.cookie].join('\\n');
          return arguments[0].some(secret => s.includes(secret))
        """, [ADMIN_PASS, BASIC, NETWORK_PASS, ARCHIVE_PASS, TOKEN, RECEIPT]), False)


def audit(v, origin, state):
    equal("fixture protocol violations", state.violations, [])
    events = v.events()
    if any(e["method"] == "Network.webSocketCreated" for e in events):
        raise AssertionError("unexpected WebSocket connection")
    requests = [e["params"]["request"] for e in events if e["method"] == "Network.requestWillBeSent"]
    for e in events:
        if e["method"] == "Page.downloadWillBegin":
            requests.append({"method": "GET", "url": e["params"]["url"], "headers": {}})
    if not any(r["url"] == origin + CONFIG for r in requests):
        raise AssertionError("no browser network evidence for the config read")
    for r in requests:
        url = urlsplit(r["url"])
        if url.scheme == "blob" and r["url"].startswith("blob:" + origin + "/"):
            continue  # Local export download.
        # CDP emits the proposed 303 target even for redirect:manual. The
        # adapter independently rejects any actual GET to either target.
        redirect = r["method"] == "GET" and url.path == "/" and url.query in ("done=apply", "done=rollback")
        if (f"{url.scheme}://{url.netloc}" != origin or (url.query and not redirect)
                or any(s in r["url"] for s in (ADMIN_PASS, BASIC, NETWORK_PASS, ARCHIVE_PASS, TOKEN, RECEIPT))):
            raise AssertionError("unexpected request destination or data in URL")
        if any(s in str(r["headers"]) for s in (NETWORK_PASS, ARCHIVE_PASS)):
            raise AssertionError("fixture password in a request header")
        if r.get("hasPostData") and (r["method"] != "POST" or url.path not in ("/", "/manage/api/restore")):
            raise AssertionError("unexpected request body")
    equal("blocked page egress attempts", v.js("return window.qaBlockedConnections"), [])
    v.check_private_state()


def config_flow(v, state, files):
    v.ready(INITIAL)
    v.layout()
    equal("blank masked network password", v.js("""
        const e = test('settings-password'); return [e.type, e.value]"""), ["password", ""])
    v.type_into(tid("settings-port"), "65536")
    v.settings("apply")
    v.notice("error", "Порт")
    v.type_into(tid("settings-password"), NETWORK_PASS)
    v.settings("export")
    download = files / ("pi-star-" + INITIAL["callsign"] + ".json")
    wait("export did not download", download.is_file)
    equal("export exact saved public data, excluding dirty fields and secrets",
          json.loads(download.read_text()), backup(INITIAL))
    v.check_private_state()

    file = files / "import.json"
    v.upload(file, backup(dict(IMPORTED, password=NETWORK_PASS)))
    v.notice("error", "семь")
    equal("invalid import has no preview", v.field(tid("settings-preview")), None)
    v.upload(file, backup(IMPORTED))
    v.notice("status", "Сравните")
    pairs = v.js("return all('tbody tr', test('settings-preview')).map(r => all('td',r).map(text))")
    equal("preview shows all saved/imported pairs", pairs,
          [[str(INITIAL[k]), str(IMPORTED[k])] for k in INITIAL])
    v.disabled("apply")
    v.layout()
    v.click(tid("settings-preview") + " button:last-child")
    equal("preview cancel preserved draft", v.js("return test('settings-port').value"), "65536")
    v.upload(file, backup(IMPORTED))
    v.notice("status", "Сравните")
    v.settings("use-preview")
    v.ready(IMPORTED)
    equal("import cleared previous password", v.js("return test('settings-password').value"), "")
    equal("validation, export, import and preview did not POST", len(state.writes), 0)

    v.type_into(tid("settings-password"), NETWORK_PASS)
    v.settings("apply")
    v.notice("status", "применены и перечитаны")
    v.ready(IMPORTED)
    expected = dict(action="apply", csrf=TOKEN, password=NETWORK_PASS,
                    **{k: str(val) for k, val in IMPORTED.items()})
    equal("exact authenticated apply body", state.writes == [expected], True)
    equal("apply cleared password", v.js("return test('settings-password').value"), "")
    v.settings("rollback")
    v.disabled("apply")
    v.click(tid("settings-confirm-rollback") + " + button")
    v.ready(IMPORTED)
    equal("cancel rollback did not POST", len(state.writes), 1)
    v.settings("rollback")
    v.settings("confirm-rollback")
    v.notice("status", "восстановлены и перечитаны")
    v.ready(INITIAL)
    equal("confirmed rollback exact form", state.writes == [expected, dict(action="rollback", csrf=TOKEN)], True)


def config_faults(v, state):
    state.write_fault = "forbidden"
    v.settings("apply")
    v.notice("error", "Сеанс изменился")
    for key in ("apply", "rollback", "import", "export"):
        v.disabled(key)
    state.read_fault = "invalid"
    v.settings("reload")
    v.notice("error", "Не удалось прочитать настройки")
    v.disabled("apply")
    v.settings("reload")
    v.ready(INITIAL)

    state.write_fault = "mismatch"
    v.settings("apply")
    v.notice("error", "отличаются от отправленных")
    actual = dict(INITIAL, callsign="QA7OTHER")
    v.ready(actual)
    state.read_fault = "invalid"
    v.settings("apply")
    v.notice("error", "перечитать параметры не удалось")
    v.disabled("apply")
    v.settings("reload")
    v.ready(actual)
    equal("no silent POST retries", [w["action"] for w in state.writes],
          ["apply", "rollback", "apply", "apply", "apply"])
    equal("password only in the intended apply form", sum(w.get("password") == NETWORK_PASS for w in state.writes), 1)
    v.layout()


def logs(v, state, files):
    v.nav("logs")

    def messages():
        return v.js("return all('[data-testid=log-view] tbody tr td:last-child').map(text)")

    all_messages = [row["message"] for row in ROWS]
    wait("missing authenticated log rows", lambda: messages() == all_messages)
    v.click(tid("log-pause"))
    equal("paused log polling", v.js("return test('log-pause').getAttribute('aria-pressed')"), "true")
    v.layout()
    option = v.js("return [...test('log-source').options].find(o => o.value === 'DMRGateway')")
    v.command(f"/element/{option[ELEMENT]}/click", {})
    equal("log source filter", messages(), all_messages[-1:])
    v.type_into(tid("log-filter"), "no-event-matches")
    equal("empty log filter", messages(), [])
    v.click(tid("log-download"))
    downloaded = files / "pi-star-radio-log.txt"
    wait("authenticated log download missing", downloaded.is_file)
    equal("download contains both sources regardless of display filter", downloaded.read_text(), LOG_TEXT)
    v.type_into(tid("log-filter"), "WARN")
    equal("case-insensitive log level filter", messages(), all_messages[-1:])

    def alert():
        return v.js("return text(document.querySelector('[data-testid=log-view] [role=alert]'))")

    state.log_fault = 503
    v.click(tid("log-refresh"))
    wait("log stale snapshot warning missing", lambda: "последний" in alert())
    equal("last valid rows retained after outage", messages(), all_messages[-1:])
    state.log_fault = 403
    v.click(tid("log-refresh"))
    wait("log auth error missing", lambda: "Требуется вход" in alert())
    equal("auth failure cleared previous snapshot", messages(), [])
    state.log_fault = None
    v.click(tid("log-pause"))
    wait("log snapshot did not recover", lambda: not alert() and messages() == all_messages[-1:])
    v.layout()


def archives(v, state, files):
    v.nav("backup")
    root = tid("station-backup")
    choose = root + " input[type=file]"
    wait("archive CSRF session not ready", lambda: v.field(choose + ":enabled"))
    v.layout()
    v.click(root + ' a[download]')
    private = files / "Pi-Star-PRIVATE.zip"
    wait("archive did not download", private.is_file)
    equal("private download is the exact fixture ZIP", private.read_bytes() == ZIP, True)

    def click(label):
        el = v.js("return button(arguments[0])", label)
        if not el:
            raise AssertionError(f"missing archive button: {label}")
        v.command(f"/element/{el[ELEMENT]}/click", {})

    def message(role, fragment):
        wait(f"expected archive {role}: {fragment}", lambda: fragment in v.js(
            "return text(document.querySelector(arguments[0]))", root + f' [role="{role}"]'))

    def preview():
        v.choose_file(choose, private)
        click("Проверить архив")
        message("status", "Архив проверен")

    bad = files / "invalid.zip"
    bad.write_bytes(b"")
    v.choose_file(choose, bad)
    message("alert", "непустой ZIP")
    equal("empty archive rejected locally", state.restore_actions, [])
    bad.write_bytes(b"qa invalid archive")
    v.choose_file(choose, bad)
    click("Проверить архив")
    message("alert", "Архив отклонён")
    preview()
    equal("preview changed files", v.js("return all(arguments[0]).map(text)", root + " li"),
          ["MMDVM-Host.ini — без изменений", "DMRGateway.ini — будет заменён"])
    v.layout()
    v.check_private_state()
    click("Закрыть предпросмотр")
    message("status", "отменено")
    equal("cancel never submitted archive apply", state.restore_actions, ["preview", "preview"])
    preview()
    state.restore_fault = 422
    click("Подтвердить восстановление")
    message("alert", "Архив отклонён")
    equal("stale preview cannot apply again", v.js("return !!button('Подтвердить восстановление')"), False)
    state.restore_fault = 403
    click("Проверить архив")
    message("alert", "Сеанс изменился")
    equal("expired archive session locked", v.field(choose + ":disabled") is not None, True)
    state.restore_fault = None
    click("Обновить сеанс")
    wait("archive session not restored", lambda: v.field(choose + ":enabled"))
    preview()
    click("Подтвердить восстановление")
    message("status", "Файлы восстановлены")
    preview()
    equal("identical archive has no apply action", v.js("return !!button('Подтвердить восстановление')"), False)
    equal("explicit archive requests", state.restore_actions,
          ["preview", "preview", "preview", "apply", "preview", "preview", "apply", "preview"])


def test_size(dist, tmp, width, height):
    with serve(dist, tmp) as (origin, state), chromium(origin, tmp / "key.pem") as command:
        # Verify the fixture challenges before it releases any config.
        with closing(HTTPSConnection(urlsplit(origin).netloc, timeout=10,
                                     context=ssl.create_default_context(cafile=str(tmp / "cert.pem")))) as conn:
            conn.request("GET", CONFIG)
            res = conn.getresponse()
            equal("unauthenticated config challenge", (res.status, res.getheader("WWW-Authenticate")),
                  (401, 'Basic realm="Pi-Star"'))
            res.read()

        # Native challenge/cache on a dedicated fixture-only document. Subsequent
        # navigation has a clean URL and no injected Authorization header.
        command("/url", {"url": origin.replace("://", f"://admin:{ADMIN_PASS}@") + "/"})
        cdp(command, "Page.addScriptToEvaluateOnNewDocument", source="""
          window.qaBlockedConnections = [];
          document.addEventListener('securitypolicyviolation', e => {
            if (['connect-src','form-action','img-src','frame-src','script-src-elem'].includes(e.effectiveDirective)
                && e.blockedURI !== 'inline') window.qaBlockedConnections.push(e.effectiveDirective);
          });
        """)
        cdp(command, "Browser.setDownloadBehavior", behavior="allow", downloadPath=str(tmp))
        start = len(state.requests)
        v = Admin(command, origin + "/manage/", width, height, width < 600)
        config_flow(v, state, tmp)
        config_faults(v, state)
        equal("no redirect follow or unexpected config reads",
              [(m, p) for m, p in state.requests[start:] if p in (CONFIG, "/")],
              [("GET", CONFIG), ("POST", "/"), ("GET", CONFIG), ("POST", "/"), ("GET", CONFIG),
               ("POST", "/"), ("GET", CONFIG), ("GET", CONFIG), ("POST", "/"),
               ("GET", CONFIG), ("POST", "/"), ("GET", CONFIG), ("GET", CONFIG)])
        logs(v, state, tmp)
        archives(v, state, tmp)
        audit(v, origin, state)
        print(f"PASS {width}x{height}: HTTPS Basic auth, config, export/import, preview, "
              "apply/rollback, 403/invalid/stale reads, logs/TXT, private archive adapter, layout and egress", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--dist", type=Path, default=Path(__file__).resolve().parents[1] / "dist")
    args = parser.parse_args()
    # Snap Chromium has a private /tmp; both Chrome and Python need these files.
    shared = Path.home() / "snap/chromium/common"
    for width, height in ((1280, 900), (390, 844), (320, 640)):
        with TemporaryDirectory(prefix="admin-e2e-", dir=shared if shared.is_dir() else None) as folder:
            test_size(args.dist.resolve(), Path(folder), width, height)


if __name__ == "__main__":
    try:
        main()
    except (AssertionError, OSError, RuntimeError, ValueError) as exc:
        sys.exit(f"FAIL: {exc}")
