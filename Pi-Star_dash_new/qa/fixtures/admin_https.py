"""In-memory browser adapter, NOT the Go handler/helper or a test of device writes.

Mirrors the HTTP surface consumed by the native UI. The canonical config wire
contract and server-side security tests live in panel/wizard{,_test}.go.
"""

from base64 import b64encode
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from io import BytesIO
import json
from pathlib import Path
import re
import ssl
import subprocess
from threading import Thread
from urllib.parse import parse_qs
from zipfile import ZipFile, ZipInfo


CONFIG = "/manage/api/config"
LOG = "/manage/api/log"
ROWS = [
    dict(at="2026-01-22T12:34:56Z", source="MMDVMHost", level="message", message="Opening the MMDVM"),
    dict(at="2026-01-22T12:34:55Z", source="DMRGateway", level="warning", message="Master is closing down"),
]
LOG_TEXT = "".join("\t".join(row[k] for k in ("at", "source", "level", "message")) + "\n" for row in ROWS)
INITIAL = dict(callsign="QA7INIT", radio_id=1234555, frequency=435125000,
               uart="/dev/ttyAMA0", claim=91234, master="master.qa.invalid", port=62031)
ADMIN_PASS = "qa-admin-browser-only"
NETWORK_PASS = "qa-network-canary-764"
TOKEN = "71aef004" * 8
RECEIPT = "ab78cd12" * 8
ARCHIVE_PASS = "qa-private-archive-canary"
COOKIE = "__Host-gospot-csrf=" + TOKEN
BASIC = "Basic " + b64encode(("admin:" + ADMIN_PASS).encode()).decode()
CSP = ("default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; "
       "form-action 'self'; frame-ancestors 'none'; base-uri 'none'")


def sample_archive():
    # Browser transport fixture ONLY. Not a signed Go/helper archive. Fixed
    # timestamps/bytes, no host INIs, archive parsing, signing key or services.
    buf = BytesIO()
    with ZipFile(buf, "w") as archive:
        for name, value in [("MMDVM-Host.ini", "[General]\nCallsign=QA7ARCH\n"),
                            ("DMRGateway.ini", "[QA]\nPassword=" + ARCHIVE_PASS + "\n")]:
            archive.writestr(ZipInfo(name), value)
    return buf.getvalue()


ZIP = sample_archive()


class State:
    def __init__(self):
        self.config = INITIAL.copy()
        self.previous = None
        self.read_fault = None
        self.write_fault = None
        self.log_fault = None
        self.restore_actions = []
        self.restore_fault = None
        self.archive_changed = True
        self.writes = []
        self.requests = []
        self.violations = []


@contextmanager
def serve(dist, tmp):
    if not (dist / "index.html").is_file():
        raise ValueError(f"build first: missing {dist}/index.html")
    state = State()
    cert, key = (tmp / name for name in ("cert.pem", "key.pem"))
    subprocess.run(["openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-days", "1",
                    "-subj", "/CN=127.0.0.1", "-addext", "subjectAltName=IP:127.0.0.1",
                    "-keyout", str(key), "-out", str(cert)], check=True,
                   stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(cert, key)

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def reply(self, status, content=b"", mime="text/plain", headers=()):
            self.send_response(status)
            for k, v in [("Content-Type", mime), ("Content-Length", str(len(content))),
                         ("Content-Security-Policy", CSP), ("Cache-Control", "no-store"),
                         ("Referrer-Policy", "no-referrer"), *headers]:
                self.send_header(k, v)
            self.end_headers()
            self.wfile.write(content)

        def authenticated(self):
            state.requests.append((self.command, self.path))
            if self.headers.get("Authorization") != BASIC:
                self.reply(401, headers=[("WWW-Authenticate", 'Basic realm="GoSpot"')])
                return False
            return True

        def json(self, data, headers=()):
            self.reply(200, json.dumps(data).encode(), "application/json", headers)

        def do_GET(self):
            if not self.authenticated():
                return
            if self.path == "/":
                # Only seeds Chromium's native Basic-auth cache at root scope.
                return self.reply(200, b"fixture login")
            if self.path == CONFIG:
                fault, state.read_fault = state.read_fault, None
                cfg = dict(state.config)
                if fault == "invalid":
                    cfg["radio_id"] = "not-an-integer"
                return self.json(dict(config=cfg, csrf=TOKEN), [
                    ("Set-Cookie", COOKIE + "; Secure; HttpOnly; Path=/; SameSite=Strict")])
            if self.path == LOG:
                if state.log_fault:
                    return self.reply(state.log_fault)
                return self.json({"rows": ROWS})
            if self.path == LOG + "/download":
                return self.reply(200, LOG_TEXT.encode(), "text/plain; charset=utf-8", [
                    ("Content-Disposition", 'attachment; filename="gospot-radio-log.txt"')])
            if self.path == "/manage/api/backup":
                return self.reply(200, ZIP, "application/zip", [
                    ("Content-Disposition", 'attachment; filename="GoSpot-PRIVATE.zip"')])
            if self.path == "/manage/":
                return self.reply(200, (dist / "index.html").read_bytes(), "text/html; charset=utf-8")
            if re.fullmatch(r"/manage/assets/[\w.-]+\.(js|css)", self.path):
                f = dist / "assets" / Path(self.path).name
                if f.is_file():
                    return self.reply(200, f.read_bytes(),
                                      "text/css" if f.suffix == ".css" else "text/javascript")
            if self.path != "/favicon.ico":
                state.violations.append("unexpected GET")
            self.reply(404)

        def do_POST(self):
            if not self.authenticated():
                return
            if self.path == "/manage/api/restore":
                action = self.headers.get("X-GoSpot-Action")
                if (self.headers.get("Origin") != origin or self.headers.get("Content-Type") != "application/zip"
                        or self.headers.get("X-GoSpot-CSRF") != TOKEN or action not in ("preview", "apply")
                        or COOKIE not in self.headers.get("Cookie", "").split("; ")
                        or self.headers.get("X-GoSpot-Receipt") != (RECEIPT if action == "apply" else None)):
                    state.violations.append("unexpected restore origin/headers")
                    return self.reply(403)
                state.restore_actions.append(action)
                n = int(self.headers.get("Content-Length", 0))
                if n > 513 * 1024 or self.rfile.read(n) != ZIP:
                    return self.reply(422)
                if state.restore_fault:
                    return self.reply(state.restore_fault)
                if action == "preview":
                    return self.json(dict(receipt=RECEIPT, changed=["DMRGateway.ini"] if state.archive_changed else []))
                state.archive_changed = False
                return self.json({"ok": True})
            if (self.path != "/" or self.headers.get("Origin") != origin
                    or self.headers.get("Content-Type") != "application/x-www-form-urlencoded"
                    or COOKIE not in self.headers.get("Cookie", "").split("; ")):
                state.violations.append("unexpected POST path/origin/content-type/cookie")
                return self.reply(403)
            length = int(self.headers.get("Content-Length", 0))
            if not 0 < length <= 8192:
                return self.reply(400)
            form = parse_qs(self.rfile.read(length).decode(), keep_blank_values=True)
            action = form.get("action", [None])[0]
            keys = {"action", "csrf"}
            if action == "apply":
                keys.update([*INITIAL, "password"])
            if (set(form) != keys or any(len(v) != 1 for v in form.values())
                    or form.get("csrf") != [TOKEN] or action not in ("apply", "rollback")):
                state.violations.append("unexpected form shape or CSRF")
                return self.reply(400)
            form = {k: v[0] for k, v in form.items()}
            state.writes.append(form)
            fault, state.write_fault = state.write_fault, None
            if fault == "forbidden":
                return self.reply(403)
            if action == "apply":
                state.previous = state.config
                state.config = {k: type(v)(form[k]) for k, v in INITIAL.items()}
                if fault == "mismatch":
                    state.config["callsign"] = "QA7OTHER"
            elif state.previous is not None:
                state.config, state.previous = state.previous, None
            else:
                return self.reply(400)
            self.reply(303, headers=[("Location", "/?done=" + action)])

    with ThreadingHTTPServer(("127.0.0.1", 0), Handler) as server:
        origin = f"https://127.0.0.1:{server.server_port}"
        server.socket = context.wrap_socket(server.socket, server_side=True)
        thread = Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            yield origin, state
        finally:
            server.shutdown()
            thread.join()
