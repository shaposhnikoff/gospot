#!/usr/bin/python3 -I
"""Test-only stand-in for hose.brandmeister.network inside the isolated QA container.

Serves HOSELINE_PAGE_URL (a page publishing a spotter token the way the real
page does) and the spotter WebSocket at HOSELINE_SPOTTER_URL from the stand's
pistar.conf, on one HTTPS port with the stand certificate. A spotter client
must send the token, Origin of the page and the "spotter" subprotocol, then
subscribe with msgpack [1, [TG]]; it then gets calls from --source on a loop:
[11, call, source, TG], μ-law audio [20, <bin>] in real time, [13, call].
Every connection, subscription and close is appended to a JSON-lines log.
Standard library only (the container has no msgpack or websockets module).
"""
import argparse
import base64
import hashlib
import json
import math
import ssl
import struct
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlsplit

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--settings", required=True)
parser.add_argument("--cert", required=True)
parser.add_argument("--key", required=True)
parser.add_argument("--log", required=True)
parser.add_argument("--source", type=int, default=2340001, help="DMR ID of the talker")
args = parser.parse_args()

settings = {}
with open(args.settings, encoding="utf-8") as f:
    for line in f:
        line = line.strip()
        if line and not line.startswith("#"):
            k, _, v = line.partition("=")
            settings[k] = v
page, spotter = urlsplit(settings["HOSELINE_PAGE_URL"]), urlsplit(settings["HOSELINE_SPOTTER_URL"])
assert page.scheme == "https" and page.hostname == "127.0.0.1", page
assert spotter.scheme == "wss" and spotter.netloc == page.netloc, spotter
ORIGIN = f"https://{page.netloc}"
TOKEN = "QaStandHoselineToken.0123456789"
GUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"
lock = threading.Lock()
ids = iter(range(1, 1 << 30))


def log(**record):
    with lock, open(args.log, "a", encoding="utf-8") as out:
        print(json.dumps(record), file=out)


def pack(v):
    """msgpack for non-negative ints, bytes and lists: all the spotter sends here."""
    if isinstance(v, bytes):
        return (b"\xc4" + struct.pack(">B", len(v)) if len(v) < 256 else b"\xc5" + struct.pack(">H", len(v))) + v
    if isinstance(v, list):
        return bytes([0x90 | len(v)]) + b"".join(pack(x) for x in v)
    for limit, head, fmt in ((0x7F, b"", ">B"), (0xFF, b"\xcc", ">B"), (0xFFFF, b"\xcd", ">H"), (0xFFFFFFFF, b"\xce", ">I")):
        if v <= limit:
            return head + struct.pack(fmt, v)
    return b"\xcf" + struct.pack(">Q", v)


def ulaw(sample):
    """G.711 μ-law encoding of a signed 16-bit sample."""
    sign = 0x80 if sample < 0 else 0
    sample = min(abs(sample), 32635) + 0x84
    exponent = min(7, max(0, (sample >> 7).bit_length() - 1))
    return ~(sign | (exponent << 4) | ((sample >> (exponent + 3)) & 0x0F)) & 0xFF


# 60 ms frames of a quiet 440 Hz tone (480 samples at 8 kHz).
TONE = [bytes(ulaw(int(6000 * math.sin(2 * math.pi * 440 * (n * 480 + i) / 8000))) for i in range(480)) for n in range(4)]


class Socket:
    def __init__(self, rfile, wfile):
        self.rfile, self.wfile, self.wlock = rfile, wfile, threading.Lock()

    def send(self, op, payload=b""):
        n = len(payload)
        head = bytes([0x80 | op]) + (bytes([n]) if n < 126 else b"\x7e" + struct.pack(">H", n))
        with self.wlock:
            self.wfile.write(head + payload)
            self.wfile.flush()

    def frame(self):
        b0, b1 = self.rfile.read(2)
        n = b1 & 0x7F
        if n == 126:
            n = struct.unpack(">H", self.rfile.read(2))[0]
        elif n == 127:
            n = struct.unpack(">Q", self.rfile.read(8))[0]
        if not b1 & 0x80 or n > 65536:
            raise ValueError("unmasked or oversized client frame")
        mask = self.rfile.read(4)
        data = bytes(c ^ mask[i % 4] for i, c in enumerate(self.rfile.read(n)))
        return b0 & 0x0F, data


def subscription(data):
    """The exact [1, [TG]] message: 0x92 0x01 0x91 <uint>."""
    if data[:3] != b"\x92\x01\x91":
        raise ValueError("not a subscription")
    head, rest = data[3], data[4:]
    if head <= 0x7F and not rest:
        return head
    sizes = {0xCC: ">B", 0xCD: ">H", 0xCE: ">I"}
    if head in sizes and len(rest) == struct.calcsize(sizes[head]):
        return struct.unpack(sizes[head], rest)[0]
    raise ValueError("unexpected talkgroup encoding")


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def reply(self, status, body, kind="text/plain"):
        self.send_response(status)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path, _, query = self.path.partition("?")
        if path == page.path and not query:
            body = (f'<!doctype html><title>Hoseline</title><script>var q1="x",t7="{TOKEN}";'
                    f'window.s=new SpotterClient("{settings["HOSELINE_SPOTTER_URL"]}",t7);</script>').encode()
            log(event="page")
            return self.reply(200, body, "text/html; charset=utf-8")
        if path == spotter.path:
            return self.spotter(parse_qs(query))
        return self.reply(404, b"not found")

    def spotter(self, query):
        protocols = [p.strip() for p in self.headers.get("Sec-WebSocket-Protocol", "").split(",")]
        conn = {"id": next(ids), "token": query.get("token") == [TOKEN], "origin": self.headers.get("Origin"),
                "spotter": "spotter" in protocols}
        log(event="open", **conn)
        if not conn["token"] or conn["origin"] != ORIGIN or not conn["spotter"] or self.headers.get("Upgrade", "").lower() != "websocket":
            return self.reply(403, b"forbidden")
        accept = base64.b64encode(hashlib.sha1((self.headers["Sec-WebSocket-Key"] + GUID).encode()).digest()).decode()
        self.send_response(101)
        self.send_header("Upgrade", "websocket")
        self.send_header("Connection", "Upgrade")
        self.send_header("Sec-WebSocket-Accept", accept)
        self.send_header("Sec-WebSocket-Protocol", "spotter")
        self.end_headers()
        self.wfile.flush()
        self.close_connection = True
        ws, stop, tg = Socket(self.rfile, self.wfile), threading.Event(), None
        try:
            op, data = ws.frame()
            tg = subscription(data) if op == 2 else None
            log(event="subscribe", id=conn["id"], tg=tg)
            if tg is None:
                return
            threading.Thread(target=self.calls, args=(ws, stop, tg), daemon=True).start()
            while True:
                op, data = ws.frame()
                if op == 9:
                    ws.send(10, data)
                elif op == 8:
                    ws.send(8, data[:2])
                    break
        except (OSError, ValueError):
            pass
        finally:
            stop.set()
            log(event="close", id=conn["id"], tg=tg)

    def calls(self, ws, stop, tg):
        try:
            call = 1000
            while not stop.wait(0.5):
                call += 1
                ws.send(2, pack([11, call, args.source, tg]))
                for n in range(25):  # 1.5 s of audio
                    if stop.wait(0.06):
                        return
                    ws.send(2, pack([20, TONE[n % 4]]))
                ws.send(2, pack([13, call]))
                log(event="call", tg=tg, call=call, frames=25)
        except OSError:
            pass


server = ThreadingHTTPServer(("127.0.0.1", page.port), Handler)
server.daemon_threads = True
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain(args.cert, args.key)
server.socket = ctx.wrap_socket(server.socket, server_side=True)
server.serve_forever()
