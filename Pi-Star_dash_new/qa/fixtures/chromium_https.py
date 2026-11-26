"""Small WebDriver transport for a single HTTPS origin with ephemeral SPKI pin.

Page-scoped CDP certificate overrides are not sufficient for Chrome's
browser-process download manager. Pass the fixture key pin at startup
so authenticated downloads use the same TLS exception as ordinary page loads.
"""

from base64 import b64encode
from contextlib import contextmanager
from hashlib import sha256
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
from urllib.error import HTTPError, URLError
from urllib.parse import urlsplit
from urllib.request import ProxyHandler, Request, build_opener

from smoke import wait


@contextmanager
def chromium(origin, key):
    der = subprocess.check_output(["openssl", "pkey", "-in", str(key), "-pubout", "-outform", "DER"])
    pin = b64encode(sha256(der).digest()).decode()
    args = ["--headless=new", "--disable-dev-shm-usage", "--disable-background-networking", "--no-first-run",
            "--proxy-server=http://127.0.0.1:1", "--proxy-bypass-list=<-loopback>;" + urlsplit(origin).netloc,
            "--host-resolver-rules=MAP * ~NOTFOUND, EXCLUDE 127.0.0.1", "--ignore-certificate-errors-spki-list=" + pin]
    if os.geteuid() == 0:
        args.append("--no-sandbox")
    options = {"args": args}
    snap = Path("/snap/chromium/current/usr/lib/chromium-browser/chrome")
    if os.getenv("CHROME_BIN"):
        options["binary"] = os.environ["CHROME_BIN"]
    elif shutil.which("chromium") == "/snap/bin/chromium" and snap.is_file():
        options["binary"] = str(snap)
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        port = s.getsockname()[1]
    http = build_opener(ProxyHandler({}))

    def request(method, path, data=None):
        req = Request(f"http://127.0.0.1:{port}{path}", method=method,
                      data=json.dumps(data).encode() if data is not None else None,
                      headers={"Content-Type": "application/json"})
        try:
            with http.open(req, timeout=45) as r:
                return json.load(r)["value"]
        except HTTPError as e:
            raise RuntimeError(e.read().decode(errors="replace")) from e

    with subprocess.Popen([os.getenv("CHROMEDRIVER", "chromedriver"), f"--port={port}"],
                          stdout=subprocess.DEVNULL) as proc:
        def ready():
            if proc.poll() is not None:
                raise RuntimeError(f"chromedriver exited: {proc.returncode}")
            try:
                return request("GET", "/status").get("ready")
            except URLError:
                return False

        try:
            wait("chromedriver did not start", ready)
            caps = {"browserName": "chrome", "goog:chromeOptions": options, "goog:loggingPrefs": {"performance": "ALL"}}
            sid = request("POST", "/session", {"capabilities": {"alwaysMatch": caps}})["sessionId"]
            try:
                yield lambda path, data: request("POST", f"/session/{sid}{path}", data)
            finally:
                request("DELETE", f"/session/{sid}")
        finally:
            proc.terminate()
