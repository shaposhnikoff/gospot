"""Compare the API response actually used by the packaged Svelte app with its rendered feed."""
import json
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from full_ui import View, equal
from smoke import browser, wait

origin, mode = sys.argv[1:]
api = origin + "/api/last_heard.php?num_transmissions=20"
fields = "time_utc mode callsign target src duration loss bit_error_rate rssi".split()
with browser(target_origin=origin) as command:
    command("/goog/cdp/execute", {"cmd":"Network.enable","params":{}})
    v = View(command, origin + "/operator/", 1366, 900, False)
    wait("operator feed never reported a successful load", lambda: v.js("return text(document.querySelector('.feed small')).startsWith('Получено:')"))
    response = next(e["params"] for e in v.events() if e["method"] == "Network.responseReceived"
                    and e["params"]["type"] == "Fetch" and e["params"]["response"]["url"] == api)
    equal("PHP status", response["response"]["status"], 200)
    equal("PHP MIME type", response["response"]["mimeType"], "application/json")
    captured = v.cdp("Network.getResponseBody", {"requestId":response["requestId"]})
    equal("text JSON", captured["base64Encoded"], False)
    print("Chromium GET", api, "-> 200 application/json", captured["body"].strip())
    rows = json.loads(captured["body"])
    equal("fixture callsigns in order", [r["callsign"] for r in rows],
          ["N0RF","N0NET"] if mode=="mmdvmhost" else [])
    actual = v.js("""return {rows: all('.feed tbody tr').map(r=>Array.from(r.cells,text)),
                     message: text(document.querySelector('.feed p')),
                     error: !!document.querySelector('.feed [role=status]')}""")
    equal("rendered feed", actual, dict(rows=[[r[k] or "—" for k in fields] for r in rows],
          message="" if rows else "В журнале нет вызовов.", error=False))
    print("PASS", mode, "rendered DOM:", json.dumps(actual, ensure_ascii=False))
