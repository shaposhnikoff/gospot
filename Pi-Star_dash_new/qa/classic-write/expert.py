"""Exercise the stock PHP quick editor via Chromium and its real /etc copy."""

from difflib import unified_diff
from hashlib import sha256
from subprocess import check_output
from urllib.parse import parse_qs

from full_ui import View, equal
from smoke import wait


PAGE = "/admin/expert/edit_dmrgateway.php"
FIELD = "General[RptPort]"
SELECTOR = f'form[method="post"] input[name="{FIELD}"]'
OLD, NEW = "62032", "62132"


def read_config(container):
    return check_output(["docker", "--host", "unix:///var/run/docker.sock", "exec",
                         container, "cat", "/etc/dmrgateway"], timeout=10)


def navigation(view, method):
    """Identify the actual document transaction, excluding CSS and other requests."""
    events = []
    def received():
        events.extend(view.events())
        requests = [e["params"] for e in events if e["method"] == "Network.requestWillBeSent"
                    and e["params"]["type"] == "Document"
                    and e["params"]["request"]["url"] == view.url
                    and e["params"]["request"]["method"] == method]
        if not requests:
            return None
        equal(f"Expert {method} document requests", len(requests), 1)
        req = requests[0]
        statuses = [e["params"]["response"]["status"] for e in events
                    if e["method"] == "Network.responseReceived"
                    and e["params"]["requestId"] == req["requestId"]]
        if not statuses:
            return None
        equal(f"Expert {method} HTTP status", statuses, [200])
        return req["request"]
    return wait(f"Missing Expert {method} document response", received)


def run(driver, origin, container):
    def effects():
        return check_output(["docker", "exec", container, "cat", "/work/effects"],timeout=10)
    initial_effects = effects()
    original = read_config(container)
    old_line = f"\nRptPort={OLD}\n".encode()
    equal("unique fixture RptPort line", original.count(old_line), 1)
    for sentinel in (b"# Standby network:", b"TGRewrite=1,91,1,91,1\nTGRewrite=2,92,2,92,1\n",
                     b'Name="QA standby"\n', b'Options="StartRef=4000;RelinkTime=15"\n',
                     b"Debug=off\n"):
        if sentinel not in original:
            raise AssertionError(f"Expert /etc/dmrgateway fixture missing {sentinel!r}")
    expected = original.replace(old_line, f"\nRptPort={NEW}\n".encode())

    view = View(driver, origin.rstrip("/") + PAGE, 1366, 900, False)
    navigation(view, "GET")
    physical = 'input[data-section="General"][data-key="RptPort"]'
    selector = physical if view.field(physical) else SELECTOR
    equal("Expert original RptPort", view.js("return document.querySelector(arguments[0])?.value", selector), OLD)
    field_name = view.js("return document.querySelector(arguments[0]).name", selector)
    token = view.js("""return document.querySelector(arguments[0]).form
                       .querySelector('input[type=hidden][name=csrf_token]')?.value""", selector)
    if not token:
        raise AssertionError("Expert form missing session token")
    view.type_into(selector, NEW)
    view.click('form[method="post"] input[type="submit"]')
    request = navigation(view, "POST")
    submitted = parse_qs(request["postData"])
    if submitted.get("csrf_token") != [token]:
        raise AssertionError("Expert POST did not carry the form's session token")
    equal("Expert submitted RptPort", submitted.get(field_name), [NEW])

    # A refresh on the POST response could write twice. Force a fresh GET;
    # the PHP page must recopy /etc/dmrgateway rather than echo POST values.
    view.cdp("Network.setCacheDisabled", {"cacheDisabled": True})
    driver("/url", {"url": view.url})
    navigation(view, "GET")
    equal("Expert RptPort after fresh GET",
          view.js("return document.querySelector(arguments[0])?.value", selector), NEW)
    saved = read_config(container)
    if saved != expected:
        # The fixture contains only shipped defaults and synthetic test data.
        # Show changes to comments, duplicate keys, quoting and blank lines.
        diff = unified_diff(expected.decode().splitlines(keepends=True),
                            saved.decode().splitlines(keepends=True), "expected", "/etc/dmrgateway")
        raise AssertionError("Expert preservation: a one-field edit changed other bytes:\n" + "".join(diff))
    print(f"Expert: POST 200 with original CSRF; fresh GET shows RptPort {OLD} → {NEW}; "
          f"all unrelated bytes preserved; expected SHA256={sha256(expected).hexdigest()}; "
          f"installed SHA256={sha256(saved).hexdigest()}")
    if selector == physical:
        before_effects=effects()
        equal("Expert save operations", before_effects, initial_effects +
              b'mount -o remount,rw /\ninstall /etc/dmrgateway\nmount -o remount,ro /\nsystemctl restart dmrgateway.service\n')
        view.click('form[method="post"] input[type="submit"]')
        navigation(view,"POST")
        equal("no-op Apply retains exact file",read_config(container)==saved,True)
        equal("no-op Apply does not reinstall or restart",effects()==before_effects,True)
        print("Expert: unchanged Apply performed no install, remount or restart")
