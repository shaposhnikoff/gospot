#!/usr/bin/python3 -I
"""Test-only stand-in for TGIF inside the isolated QA container.

Listens on the hosts and ports of TGIF_API_URL (HTTPS, the Static/Dynamic TG
API) and TGIF_SESSION_URL (legacy sessions/update) from the stand's copy of
pistar.conf, keeps a little TG state and appends every request it receives to
a JSON-lines log. Authorization is logged only as "token" (the Bearer token
stored in TGIF_API_KEY_FILE), "" (absent) or "other", never as its value.
"""
import argparse
import json
import re
import ssl
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--settings", required=True)
parser.add_argument("--cert", required=True)
parser.add_argument("--key", required=True)
parser.add_argument("--log", required=True)
parser.add_argument("--delay", type=float, default=0, help="seconds before answering a GET of the API")
args = parser.parse_args()

settings = {}
with open(args.settings, encoding="utf-8") as f:
    for line in f:
        line = line.strip()
        if line and not line.startswith("#"):
            k, _, v = line.partition("=")
            settings[k] = v
api, session = urlsplit(settings["TGIF_API_URL"]), urlsplit(settings["TGIF_SESSION_URL"])
assert api.scheme == "https" and api.hostname == "127.0.0.1", api
assert session.scheme == "http" and session.hostname == "127.0.0.1", session

lock = threading.Lock()
# Numbers deliberately mixed with numeric strings, as TGIF may send them.
state = {"static": [{"slot": "1", "talkgroup": "91"}, {"slot": 2, "talkgroup": 235}],
         "dynamic": {"ts1_talkgroup": "0", "ts2_talkgroup": 9990}}


def token():
    try:
        with open(settings["TGIF_API_KEY_FILE"], encoding="utf-8") as f:
            m = re.search(r"^apikey=(\S+)$", f.read(), re.M)
            return m.group(1) if m else ""
    except OSError:
        return ""


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def reply(self, status, body, kind="application/json"):
        data = body if isinstance(body, bytes) else json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def handle_any(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = self.rfile.read(length).decode() if length else ""
        auth = self.headers.get("Authorization")
        auth = "" if auth is None else ("token" if token() and auth == "Bearer " + token() else "other")
        with lock, open(args.log, "a", encoding="utf-8") as log:
            print(json.dumps({"scheme": self.server.scheme, "method": self.command, "path": self.path,
                              "body": body, "auth": auth}), file=log)
        if self.server.scheme == "http":
            if re.fullmatch(re.escape(session.path) + r"[0-9]+/[01]/[0-9]+", self.path) and self.command == "GET":
                return self.reply(200, b"200", "text/plain")
            return self.reply(404, b"404", "text/plain")
        m = re.fullmatch(re.escape(api.path) + r"(static|dynamic)-talkgroups/([0-9]+)", self.path)
        if not m:
            return self.reply(404, {"ok": False, "error": "not_found"})
        if auth != "token":
            return self.reply(401, {"ok": False, "error": "invalid_token"})
        kind = m.group(1)
        if self.command == "GET":
            time.sleep(args.delay)
            with lock:
                if kind == "static":
                    return self.reply(200, {"ok": True, "memberships": state["static"]})
                return self.reply(200, {"ok": 1, **state["dynamic"]})
        try:
            req = json.loads(body)
            slot, tg = int(req["slot"]), int(req["talkgroup"])
        except (ValueError, KeyError, TypeError):
            return self.reply(400, {"ok": False, "error": "bad_request"})
        if slot not in (1, 2):
            return self.reply(400, {"ok": False, "error": "unsupported_slot"})
        with lock:
            if kind == "dynamic" and self.command == "POST":
                state["dynamic"][f"ts{slot}_talkgroup"] = 0 if tg == 4000 else tg
                return self.reply(200, {"ok": True})
            if kind == "static" and self.command in ("POST", "DELETE"):
                state["static"] = [s for s in state["static"] if (int(s["slot"]), int(s["talkgroup"])) != (slot, tg)]
                if self.command == "POST":
                    state["static"].append({"slot": str(slot), "talkgroup": tg})
                return self.reply(200, {"ok": True})
        return self.reply(405, {"ok": False, "error": "method"})

    do_GET = do_POST = do_DELETE = handle_any


def serve(scheme, port):
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    server.scheme = scheme
    if scheme == "https":
        ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        ctx.load_cert_chain(args.cert, args.key)
        server.socket = ctx.wrap_socket(server.socket, server_side=True)
    server.serve_forever()


threading.Thread(target=serve, args=("http", session.port), daemon=True).start()
serve("https", api.port)
