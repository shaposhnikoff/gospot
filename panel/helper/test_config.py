import importlib.machinery
import importlib.util
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch


# pistar-config reads its paths at import; use the repository settings file.
os.environ.setdefault("PISTAR_SETTINGS", str(Path(__file__).resolve().parents[2] / "Pi-Star_dash_new" / "pistar.conf"))
loader = importlib.machinery.SourceFileLoader("helper", str(Path(__file__).with_name("pistar-config")))
h = importlib.util.module_from_spec(importlib.util.spec_from_loader(loader.name, loader))
loader.exec_module(h)

HOST = """[General]
Callsign=UT3UDX
Id=2550430
Duplex=0
[Modem]
Protocol=uart
UARTPort=/dev/ttyAMA0
# UARTPort=/dev/ttyACM0
RXFrequency=433700000
TXFrequency=433700000
[Other]
RXFrequency=100
"""
GW = """[General]
Id=1234568
[DMR Network 1]
Id=1234568
Address=192.168.10.11
Port=62031
Password=oldsecret
[DMR Network 2]
Address=unchanged
Password=othersecret
"""


class HelperTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        root = Path(tmp.name)
        p = patch.multiple(h, STATE=root / "state", HOST=root / "host", GW=root / "gw")
        p.start()
        self.addCleanup(p.stop)
        h.HOST.write_text(HOST)
        h.HOST.chmod(0o640)
        h.GW.write_text(GW)
        h.GW.chmod(0o644)
        self.calls = []
        self.settings = dict(h.execute("read"), password="")

    def apply(self, **updates):
        return h.execute("apply", dict(self.settings, **updates), self.calls.append)

    def test_public_read_apply_and_rollback(self):
        public = h.execute("read")
        self.assertEqual(set(public), h.KEYS - {"password"})
        self.assertNotIn("secret", json.dumps(public))
        self.apply(claim=1234569, frequency=433750000)
        self.assertEqual(h.HOST.read_text(), HOST.replace("433700000", "433750000"))
        self.assertEqual(h.GW.read_text(), GW.replace("1234568", "1234569"))
        self.assertEqual(self.calls, ["dmrgateway.service", "mmdvmhost.service"])
        backup = h.STATE / "previous.json"
        self.assertEqual(json.loads(backup.read_text()), [HOST, GW])
        for path, mode in [(backup, 0o600), (h.HOST, 0o640), (h.GW, 0o600)]:
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), mode)
        h.execute("rollback", run=self.calls.append)
        self.assertEqual((h.HOST.read_text(), h.GW.read_text()), (HOST, GW))
        self.assertFalse(backup.exists())

    def test_password_only_and_noop(self):
        self.apply(password="newsecret")
        self.assertEqual(h.GW.read_text(), GW.replace("oldsecret", "newsecret"))
        self.apply()
        self.assertEqual(self.calls, ["dmrgateway.service"])
        h.execute("rollback", run=self.calls.append)
        self.assertEqual(self.calls, ["dmrgateway.service"] * 2)
        self.assertEqual(h.GW.read_text(), GW)

    def test_failure_restores_files_and_previous_backup(self):
        self.apply(master="dmr.local")
        backup = (h.STATE / "previous.json").read_bytes()
        def fail_once(unit):
            self.calls.append(unit)
            if len(self.calls) == 3:
                raise OSError()
        with self.assertRaisesRegex(h.Error, "restored"):
            h.execute("apply", dict(self.settings, radio_id=42), fail_once)
        self.assertEqual(self.calls[-4:], ["dmrgateway.service", "mmdvmhost.service"] * 2)
        self.assertEqual(h.HOST.read_text(), HOST)
        self.assertEqual(h.GW.read_text(), GW.replace("192.168.10.11", "dmr.local"))
        self.assertEqual((h.STATE / "previous.json").read_bytes(), backup)

    def test_invalid_settings_never_touch_ini(self):
        for updates in [dict(extra="key"), dict(port=True), dict(port=65536), dict(claim=0),
                        dict(radio_id="3"), dict(frequency=433.7), dict(password="x\nId=3"),
                        dict(uart="/dev/serial/by-id/../foo"), dict(master="999.1.1.1"),
                        dict(callsign="x\nPassword=y")]:
            with self.subTest(updates=updates), self.assertRaises(h.Error):
                self.apply(**updates)
        self.assertEqual(self.calls, [])
        self.assertEqual(h.GW.read_text(), GW)

    def test_ambiguous_ini_and_symlink_rejected(self):
        h.HOST.write_text(HOST.replace("[Modem]", "Id=42\n[Modem]"))
        with self.assertRaises(h.Error):
            self.apply(claim=42)
        h.GW.unlink()
        h.GW.symlink_to(h.HOST)
        with self.assertRaises(OSError):
            self.apply(claim=42)
        self.assertEqual(self.calls, [])
