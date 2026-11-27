"""Local, read-only HTTP fixture for full_ui.py; exercises the built SPA."""

from contextlib import contextmanager
from datetime import datetime, timezone
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
import json
from threading import Thread
from urllib.parse import urlsplit


@contextmanager
def serve(dist, *, operator=False):
    dist = dist.resolve()
    entry = "/operator/" if operator else "/"
    if not (dist / entry.strip("/") / "index.html").is_file():
        raise ValueError(f"build the dashboard first: {dist}/index.html missing")
    case = {"state": "online"}

    class Dashboard(SimpleHTTPRequestHandler):
        def __init__(self, *args):
            super().__init__(*args, directory=str(dist))

        def log_message(self, *args):
            pass

        def do_GET(self):
            path = urlsplit(self.path).path
            if operator:
                if path.startswith("/operator/"):
                    return super().do_GET()
                if self.path != "/api/last_heard.php?num_transmissions=20":
                    return self.send_error(404)
                state = case["state"]
                if state == "api-offline":
                    return self.send_error(503)
                if state == "invalid":
                    payload = [{"callsign": "incomplete"}]
                elif state == "idle":
                    payload = []
                else:
                    payload = [dict(time_utc="2026-01-15 12:00:00", mode="DMR Slot 2",
                                    callsign="QA7RF" if state == "online" else "QA7NEW",
                                    callsign_suffix="", target="TG 99", src="RF", duration="3.0",
                                    loss="0%", bit_error_rate="0.2%", rssi="")]
            elif path == "/runtime.json":
                payload = {"backend": "gospot-go", "wizardPort": 8788}
            elif path == "/api/status":
                state = case["state"]
                if state == "api-offline":
                    self.send_error(503)
                    return
                now = datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")
                missing = state == "journal-unavailable"
                payload = {
                    "UpdatedAt": now, "Node": "qa-go-node",
                    "Radio": {"Callsign": "QA7NODE", "ID": "0099991", "RX": "435.125", "TX": "430.125",
                              "Operation": "Duplex", "CC": "0", "Slots": "TS1 / TS2",
                              "Modes": None if missing else ["DMR", "P25"]},
                    "Modem": {"State": "unknown" if missing else "up", "Detail": "QA modem", "Source": "systemd", "At": now},
                    "Master": {"State": "unknown" if missing else "down", "Detail": "QA master", "Source": "journal", "At": now},
                    "HeardAvailable": not missing, "Heard": None if missing else
                    ([] if state == "idle" else [{"Source": "RF", "At": now, "From": "QA7RF", "To": "TG 99", "Slot": "2"}]),
                }
            else:
                return super().do_GET()
            body = json.dumps(payload).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            self.wfile.write(body)

    with ThreadingHTTPServer(("127.0.0.1", 0), Dashboard) as server:
        thread = Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            yield f"http://127.0.0.1:{server.server_port}{entry}", case
        finally:
            server.shutdown()
            thread.join()
