"""Read-only HTTP frontend for pistar3. Uses only the Python standard library."""
import http.client
import json
import os
import re
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit


# Resolve the release once: files and server always come from the same build.
ROOT = Path(__file__).resolve().parent / "dist"
TYPES = {
    ".html": "text/html; charset=utf-8", ".js": "text/javascript",
    ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png",
    ".ico": "image/x-icon", ".woff2": "font/woff2", ".webp": "image/webp",
}
MAX_STATUS = 1024 * 1024
# Set only when an authenticated HTTPS classic docroot is installed on this node.
CLASSIC_PORT = int(os.environ.get("PISTAR_CLASSIC_HTTPS_PORT", "0"))
if not 0 <= CLASSIC_PORT <= 65535:
    raise ValueError("Invalid PISTAR_CLASSIC_HTTPS_PORT")
PUBLIC_ALIASES = {"/index.php": "/#overview", "/mmdvmhost/lh.php": "/#heard-all",
                  "/mmdvmhost/localtx.php": "/#heard-rf", "/mmdvmhost/repeaterinfo.php": "/#overview"}
ADMIN_ALIASES = {"/admin/": "", "/admin/index.php": "", "/admin/configure.php": "#settings",
                 "/admin/expert/": "#expert",
                 "/admin/live_modem_log.php": "#logs", "/admin/config_backup.php": "#backup",
                 "/admin/sysinfo.php": "#system", "/admin/download_modem_log.php": "api/log/download"}


class Handler(BaseHTTPRequestHandler):
    def setup(self):
        self.request.settimeout(10)
        super().setup()

    def log_message(self, fmt, *args):
        # Never log client-supplied URLs, credentials, or status bodies.
        pass

    def reply(self, code, body=b"", content_type="text/plain"):
        self.send_response(code)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("X-Content-Type-Options", "nosniff")
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(body)

    def status(self, endpoint="/api/status"):
        conn = http.client.HTTPConnection("127.0.0.1", 8787, timeout=3)
        try:
            # No incoming headers, cookies, query, or body go upstream.
            # http.client does not follow redirects or use environment proxies.
            conn.request("GET", endpoint, headers={"Accept": "application/json"})
            res = conn.getresponse()
            if res.status == 503 and endpoint.startswith("/api/last_heard.php"):
                return self.reply(503,b"[]","application/json")
            if res.status != 200:
                return self.reply(502)
            body = res.read(MAX_STATUS + 1)
            if len(body) > MAX_STATUS or not isinstance(json.loads(body), dict if endpoint == "/api/status" else list):
                return self.reply(502)
        except (OSError, http.client.HTTPException, ValueError, RecursionError):
            return self.reply(502)
        finally:
            conn.close()
        self.reply(200, body, "application/json")

    def do_GET(self):
        # BaseHTTPRequestHandler normalizes leading slashes in self.path.
        target = self.requestline.split()[1]
        if target in PUBLIC_ALIASES or target in ADMIN_ALIASES:
            loc = PUBLIC_ALIASES.get(target)
            if loc is None:
                loc = "https://192.168.210.194:8788/manage/" + ADMIN_ALIASES[target]
            self.send_response(302)
            self.send_header("Location", loc)
            self.send_header("Content-Length","0")
            self.end_headers()
            return
        if re.fullmatch(r"/api/last_heard\.php(?:\?num_transmissions=\d{1,3})?", target):
            return self.status(target)
        if target == "/runtime.json":
            settings = {"backend":"pistar-go","wizardPort":8788}
            if CLASSIC_PORT:
                settings["classicHttpsPort"] = CLASSIC_PORT
            return self.reply(200, json.dumps(settings).encode(), "application/json")
        if target == "/api/status":
            return self.status()
        if not target.startswith("/") or target.startswith("//") or "#" in target:
            return self.reply(404)
        path = urlsplit(target).path
        if path == "/":
            path = "/index.html"
        if not re.fullmatch(r"/[A-Za-z0-9_-][A-Za-z0-9_.-]*(/[A-Za-z0-9_-][A-Za-z0-9_.-]*)*", path):
            return self.reply(404)
        candidate = (ROOT / path[1:]).resolve()
        mime = TYPES.get(candidate.suffix)
        if not mime or not candidate.is_relative_to(ROOT) or not candidate.is_file():
            return self.reply(404)
        self.reply(200, candidate.read_bytes(), mime)


if __name__ == "__main__":
    ThreadingHTTPServer(("192.168.210.194", 8789), Handler).serve_forever()
