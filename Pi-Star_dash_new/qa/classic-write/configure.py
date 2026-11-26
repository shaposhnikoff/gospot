"""Edit the station callsign through the stock MMDVMHost configuration form."""

import re
from subprocess import check_output
from urllib.parse import parse_qsl, urlsplit

from full_ui import View, equal, ELEMENT
from smoke import wait


PAGE = "/admin/configure.php"
OLD, NEW = "N0DEMO", "QA7TEST"
CALL = '#config input[type="text"][name="confCallsign"]'
COLOR = '#config select[name="dmrColorCode"]'
TOWN = '#config input[name="confDesc1"]'
ZONE = '#config select[name="systemTimezone"]'
LANG = '#config select[name="dashboardLanguage"]'
MODEM = '#config select[name="confHardware"]'
# Use the Apply below General Configuration (there are several in #config).
APPLY = '#config table:has(input[name="confCallsign"]) + div input[type="button"][onclick="submitform()"]'


def form_entries(view):
    # Keep DOM order, empty values and duplicate names (e.g. OFF + checked ON).
    # Read successful controls; the real submit is performed only by clicking Apply.
    return view.js("""
      const f = document.getElementById('config');
      if (!f || f.method !== 'post' || f.action !== arguments[0]) return null;
      return Array.from(new FormData(f));
    """, view.url)


def check_page(view, phase):
    # The legacy handler can emit Working and Done *before* a write error,
    # and PHP can return a partial page with status 200. Don't log form values.
    diagnostic = view.js(r"""
      return document.body.innerText.match(/(?:^|\n)\s*(ERROR\b|(?:PHP )?(?:Fatal error|Parse error|Warning|Notice|Deprecated):)|Unable to (?:read source|write) configuration file|403 Forbidden/)?.[0]?.trim() || '';
    """)
    if diagnostic:
        raise AssertionError(f"Configure {phase} response contained {diagnostic!r}")


def check_editor(view, expected, mode="MMDVM", first_setup=False):
    check_page(view, "GET")
    equal("Configure visible callsign",
          view.js("const e=document.querySelector(arguments[0]); return e && visible(e) ? e.value : null", CALL),
          expected)
    equal("Configure controller and mode-specific controls", view.js("""
      return [document.querySelector('#config [name=controllerSoft]:checked')?.value,
              !!document.querySelector('#config input[type=checkbox][name=MMDVMModeDMR]')];
    """), [mode, mode=="MMDVM" and not first_setup])


def check_language(view, french):
    code, general, control, button, label = (
        ("french_fr", "Configuration générale", "Contrôle logiciel", "Appliquer les modifications", "Langage de la console:")
        if french else ("english_uk", "General Configuration", "Control Software", "Apply Changes", "Dashboard Language:"))
    result = view.js("""
      const field=document.querySelector(arguments[0]), apply=document.querySelector(arguments[1]);
      const headings=[...document.querySelectorAll('#config h2')].filter(visible).map(h=>h.innerText.trim());
      return [field.value, headings.includes(arguments[2]), headings.includes(arguments[3]), apply.value,
              field.closest('tr').querySelector('td a').firstChild.textContent.trim()];
    """, LANG, APPLY, general, control)
    equal("Selected language and rendered labels",result,[code,True,True,button,label])
    print(f"{'Fresh' if french else 'Initial'} GET: {code}: {general} | {control} | {button} | {label}",flush=True)


def run(driver, origin, container, *, color=None, initial_color='3', edit_call=True, location=False, timezone=False, controller=False, language=False, first_setup=False, modem=False, view=None):
    if (urlsplit(origin).hostname != "127.0.0.1" or
            not re.fullmatch("pstar-classic-write-[a-f0-9]+", container)):
        raise ValueError("Configure requires a loopback classic-write fixture")

    def php_log():
        return check_output(["docker", "exec", container, "chroot", "/stand", "cat", "/tmp/qa-server.log"],
                            timeout=10)

    start = len(php_log())
    if modem:
        # Count the modem prompt per document; still show the native browser dialog.
        driver("/goog/cdp/execute", {"cmd":"Page.addScriptToEvaluateOnNewDocument","params":{"source":"""
          window.qaModemPrompts=0;
          const originalAlert=window.alert;
          window.alert=function(message) {
            if (String(message).includes('re-select your modem')) window.qaModemPrompts++;
            return originalAlert.call(window,message);
          };
        """}})
    if view is None: view = View(driver, origin.rstrip("/") + PAGE, 1366, 900, False)
    if modem:
        driver("/alert/accept", {})
        wait("Initial modem editor did not finish loading",lambda: view.js("return document.readyState === 'complete'"))
        equal("Initial modem prompt",view.js("return window.qaModemPrompts"),1)
    frame = view.cdp("Page.getFrameTree", {})["frameTree"]["frame"]["id"]
    initial_mode = "DSTAR" if controller == "MMDVM" and not first_setup else "MMDVM"
    check_editor(view, OLD, initial_mode, first_setup)
    if first_setup:
        assert not view.field('#config select[name="confHardware"]'), "unexpected modem selector before setup"
        print("Initial GET: default MMDVM radio selected; no configured modem or MMDVM mode controls",flush=True)
    if initial_mode == "DSTAR":
        assert view.field('#config select[name="confDStarModuleSuffix"]'), "Initial D-Star form missing"
        print("Initial GET: DSTAR selected, D-Star module selector visible; MMDVM mode switches absent",flush=True)
    before = form_entries(view)
    if not before or [v for k, v in before if k == "confCallsign"] != [OLD]:
        raise AssertionError("Configure: missing or ambiguous original callsign in #config")
    tokens = [v for k, v in before if k == "csrf_token"]
    if len(tokens) != 1 or not re.fullmatch("[a-f0-9]{64}", tokens[0]):
        raise AssertionError("Configure: missing session token in #config")

    changes = {}
    if modem:
        equal("Unconfigured modem selector",[v for k,v in before if k=="confHardware"],[""])
        widget = MODEM+' + .nice-select'
        view.click(widget)
        view.type_into(widget+' .nice-select-search',"stick")
        view.click(widget+' li[data-value="zumspotusb"]')
        changes["confHardware"] = "zumspotusb"
        print("Initial GET: hardware unset; native select-modem warning observed and accepted",flush=True)
    if language:
        check_language(view, False)
        assert view.field(LANG), "Dashboard language select not visible"
        option = view.js("return document.querySelector(arguments[0])", LANG+' option[value="french_fr"]')
        driver(f"/element/{option[ELEMENT]}/click", {})
        changes["dashboardLanguage"] = "french_fr"
    if first_setup and controller == initial_mode:
        equal("Untouched default controller", [v for k,v in before if k=="controllerSoft"], ["MMDVM"])
    elif controller:
        view.click(f'#config input[name="controllerSoft"][value="{controller}"]')
        driver("/alert/accept", {})
        changes["controllerSoft"] = controller
    if timezone:
        equal("Original timezone", [v for k,v in before if k=="systemTimezone"], ["UTC"])
        equal("Timezones offered", view.js("return Array.from(document.querySelector(arguments[0]).options,o=>o.value)", ZONE),
              ["UTC", "Europe/Berlin"])
        assert view.field(ZONE), "System TimeZone must be visible"
        option = view.js("return document.querySelector(arguments[0])", ZONE+' option[value="Europe/Berlin"]')
        driver(f"/element/{option[ELEMENT]}/click", {})
        changes["systemTimezone"] = "Europe/Berlin"
    if location:
        equal("Original gateway town", [v for k,v in before if k=="confDesc1"], ["Demo"])
        changes["confDesc1"] = "North Ridge"
        view.type_into(TOWN, changes["confDesc1"])
    if edit_call:
        view.type_into(CALL, NEW)
        changes["confCallsign"] = NEW
    if color is not None:
        equal("Original Color Code", [v for k,v in before if k=="dmrColorCode"], [initial_color])
        assert view.field(COLOR), "DMR Color Code must be visible"
        option = view.js("return document.querySelector(arguments[0])", COLOR+f' option[value="{color}"]')
        driver(f"/element/{option[ELEMENT]}/click", {})
        changes["dmrColorCode"] = color
    expected = [[k, changes.get(k, v)] for k, v in before]
    if form_entries(view) != expected:
        raise AssertionError(f"Configure: expected edits only to {list(changes)}")
    view.cdp("Network.setCacheDisabled", {"cacheDisabled": True})
    view.click(APPLY)
    if first_setup:
        # With POST/redirect/GET the select-modem alert can precede the first
        # DOM read after the click (instead of following the progress page).
        def initial_response_ready():
            try:
                # The old document may still be 'complete'. Only the fresh
                # configured form has a hardware selector, after its inline alert.
                return view.js("return document.readyState === 'complete' && !!document.querySelector(arguments[0])", MODEM)
            except RuntimeError as exc:
                if "unexpected alert open" in str(exc) and "re-select your modem" in str(exc):
                    print("Dashboard prompted to select modem hardware",flush=True)
                    return False
                raise
        wait("First setup response did not finish loading",initial_response_ready, timeout=25)
    check_page(view, "Apply")
    if not view.field(CALL):
        equal("Configure progress response", view.js("""
          return all('th').some(e => text(e) === 'Working...')
              && /applying your configuration changes/i.test(document.body.innerText);
        """), True)
        # Stock PHP navigates by JS after 7.5s; a PRG handler has already
        # navigated. Let the browser do both naturally, never refresh a POST.
        def editor_ready():
            try:
                return view.js("return document.readyState === 'complete' && !!document.querySelector(arguments[0])", CALL)
            except RuntimeError as exc:
                # ChromeDriver auto-dismisses and reports a JS alert on the next
                # command. No hardware was selected during a controller-only setup.
                if first_setup and "unexpected alert open" in str(exc) and "re-select your modem" in str(exc):
                    print("Fresh GET: dashboard prompted to select modem hardware",flush=True)
                    return False
                raise
        wait("Configure: progress page did not return to the editor",editor_ready,timeout=25)
    check_editor(view, NEW if edit_call else OLD, controller or "MMDVM")

    # Exclude the nested Wi-Fi document. A PRG reuses the POST requestId:
    # its status is attached to the following GET as redirectResponse.
    documents = []
    for e in view.events():
        p = e["params"]
        if p.get("type") != "Document" or p.get("frameId") != frame:
            continue
        if e["method"] == "Network.requestWillBeSent":
            if "redirectResponse" in p and documents:
                documents[-1]["status"] = p["redirectResponse"]["status"]
            r = p["request"]
            documents.append(dict(method=r["method"], url=r["url"], id=p["requestId"],
                                  data=r.get("postData"), status=None))
        elif (e["method"] == "Network.responseReceived" and documents
              and p["requestId"] == documents[-1]["id"]):
            documents[-1]["status"] = p["response"]["status"]
    equal("Configure browser navigation", [d["method"] for d in documents], ["GET", "POST", "GET"])
    first, post, last = documents
    for d in documents:
        equal("Configure document destination", d["url"].split("?", 1)[0], view.url)
    equal("Configure initial/fresh GET status", [first["status"], last["status"]], [200, 200])
    if post["status"] not in (200, 302, 303):
        raise AssertionError(f"Configure: unexpected POST status {post['status']}")
    if location or timezone or controller or language or modem:
        equal("Full Apply POST (no scoped action selector)", post["url"], view.url)
    posted = parse_qsl(post["data"] or "", keep_blank_values=True)
    if posted != [(k, re.sub(r"\r\n|\r|\n", "\r\n", v)) for k, v in expected]:
        raise AssertionError("Configure: native POST differs from the original full form plus intended edits")
    print(f"Configure: visible Apply sent {len(posted)} controls, edits={changes}; "
          f"GET 200 → POST {post['status']} → GET 200, callsign={NEW if edit_call else OLD}", flush=True)
    if language:
        check_language(view, True)
    if modem:
        saved = view.js("""const s=document.querySelector(arguments[0]); return [s.value,
                            s.selectedOptions[0].textContent.trim(),window.qaModemPrompts]""",MODEM)
        equal("Fresh GET modem and warning count",saved,["zumspotusb","ZUMspot - USB Stick",0])
        equal("Fresh GET visible modem label",view.js("return document.querySelector(arguments[0]).innerText.trim()",
                                                     MODEM+' + .nice-select .current'),saved[1])
        print(f"Fresh GET: Hardware={saved[0]} ({saved[1]}); select-modem warnings={saved[2]}",flush=True)
    if controller == "DSTAR":
        assert view.field('#config select[name="confDStarModuleSuffix"]'), "D-Star module selector missing"
        print("Fresh GET: DSTAR selected, D-Star module selector visible; MMDVM mode switches absent",flush=True)
    elif controller == "MMDVM":
        print("Fresh GET: MMDVM selected, MMDVM mode switches present",flush=True)
    if color is not None:
        saved = view.js("return document.querySelector(arguments[0]).value", COLOR)
        print(f"Fresh GET: DMR Color Code={saved} (requested {color})", flush=True)
        equal("Fresh GET Color Code", saved, color)
    if location:
        saved = view.js("return document.querySelector(arguments[0]).value", TOWN)
        equal("Fresh GET Gateway Town", saved, changes["confDesc1"])
        print(f"Fresh GET: Gateway Town={saved}", flush=True)
    if timezone:
        saved = view.js("return document.querySelector(arguments[0]).value", ZONE)
        equal("Fresh GET System TimeZone", saved, "Europe/Berlin")
        print(f"Fresh GET: System TimeZone={saved}", flush=True)

    diag = re.search(rb"PHP (?:Fatal error|Parse error|Warning|Notice|Deprecated)\b|"
                     rb"(?:config_writer|configure\.php|csrf_verify): ", php_log()[start:])
    if diag:
        raise AssertionError(f"Configure: {diag[0].decode().strip()} diagnostic in /tmp/qa-server.log")
