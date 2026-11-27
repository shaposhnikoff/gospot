"""Read-only integration checks; run *on* the pilot Pi (Python standard library)."""
import json
import re
import socket
import unittest
from urllib.parse import urljoin
from urllib.request import ProxyHandler, build_opener

http = build_opener(ProxyHandler({}))
base = "http://192.168.210.194:8789/"


def get(url):
    with http.open(url, timeout=5) as response:
        return response.read()


class Installed(unittest.TestCase):
    def test_assets(self):
        html = get(base).decode()
        self.assertIn('id="app"', html)
        assets = re.findall(r'(?:src|href)="([^"]+\.(?:js|css))"', html)
        self.assertGreaterEqual(len(assets), 2)
        for asset in assets:
            self.assertGreater(len(get(urljoin(base, asset))), 100)

    def test_runtime(self):
        self.assertEqual(json.loads(get(base + "runtime.json"))["backend"], "gospot-go")

    def test_status_matches_collector(self):
        staged = json.loads(get(base + "api/status"))
        live = json.loads(get("http://127.0.0.1:8787/api/status"))
        self.assertEqual(staged["Node"], socket.gethostname())
        self.assertEqual(staged["Radio"], live["Radio"])
        for component in ("Modem", "Master"):
            self.assertIn(staged[component]["State"], ("up", "down", "unknown"))
        self.assertIsInstance(staged["HeardAvailable"], bool)


if __name__ == "__main__":
    unittest.main(verbosity=2)
