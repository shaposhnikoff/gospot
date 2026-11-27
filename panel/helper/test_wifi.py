"""Wi-Fi commands of pistar-configure (admin/wifi.php) on isolated fixtures.

Every program the helper runs (nmcli, wpa_cli, ip, iw) is a recorder that
logs its argv and prints canned output; sysfs is a temporary directory."""
import hashlib
import importlib.machinery
import importlib.util
import json
import os
from pathlib import Path
import shutil
import stat
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
HELPER = Path(__file__).with_name("pistar-configure")
TMP = Path(tempfile.mkdtemp())
BIN = TMP / "bin"
BIN.mkdir()
WIFI_KEYS = {"WPA_SUPPLICANT_CONF": TMP / "etc" / "wpa_supplicant.conf", "DASHBOARD_STATE_DIR": TMP / "state",
             "SYSFS_NET_DIR": TMP / "sys", "WIFI_REGDOM_CONF": TMP / "etc" / "regdom.conf",
             "WPA_CTRL_DIR": TMP / "run" / "wpa_supplicant",
             "NMCLI_BIN": BIN / "nmcli", "WPA_CLI_BIN": BIN / "wpa_cli", "IP_BIN": BIN / "ip", "IW_BIN": BIN / "iw"}
(TMP / "etc").mkdir()
lines = []
for line in (ROOT / "Pi-Star_dash_new" / "pistar.conf").read_text().splitlines():
    key = line.partition("=")[0]
    if key in WIFI_KEYS:
        line = f"{key}={WIFI_KEYS[key]}"
    lines.append(line)
SETTINGS = TMP / "pistar.conf"
SETTINGS.write_text("\n".join(lines) + "\n")
os.chmod(SETTINGS, 0o644)

# One recorder for every tool: argv goes to calls.jsonl, the reply comes from
# outputs.json keyed by "name arg arg ...", failures from fail.json.
RECORDER = f"""#!{sys.executable}
import json, sys
from pathlib import Path
d = Path({str(TMP)!r})
name = Path(sys.argv[0]).name
with open(d / "calls.jsonl", "a") as f:
    f.write(json.dumps([name, *sys.argv[1:]]) + "\\n")
key = " ".join([name, *sys.argv[1:]])
fail = json.loads((d / "fail.json").read_text()) if (d / "fail.json").exists() else []
if any(key.startswith(p) for p in fail):
    sys.exit(1)
outputs = json.loads((d / "outputs.json").read_text()) if (d / "outputs.json").exists() else {{}}
sys.stdout.write(outputs.get(key, ""))
"""
for name in ("nmcli", "wpa_cli", "ip", "iw"):
    (BIN / name).write_text(RECORDER)
    os.chmod(BIN / name, 0o755)

os.environ["PISTAR_SETTINGS"] = str(SETTINGS)
loader = importlib.machinery.SourceFileLoader("configure_wifi_helper", str(HELPER))
helper = importlib.util.module_from_spec(importlib.util.spec_from_loader(loader.name, loader))
loader.exec_module(helper)
helper.WIFI_SLEEP.update(scan=0, reset=0)


def independent_psk(ssid, passphrase):
    # Computed here, not with the helper: PBKDF2-HMAC-SHA1, 4096 rounds, 32 bytes.
    return hashlib.pbkdf2_hmac("sha1", passphrase.encode("ascii"), ssid.encode("utf-8"), 4096, 32).hex()


# Test-only secrets; never printed. Each must stay absent from replies and errors.
OLD_FIRST, OLD_SECOND, NEW_SECOND, NEW_THIRD = "first-old-pass", "second-old-pass", "second-new-pass", "third-new-pass"
FIRST_HASH, SECOND_HASH = independent_psk("QA-First", OLD_FIRST), independent_psk("QA-Second", OLD_SECOND)
FIRST_BLOCK = ("network={\n\t#ssid=\"QA-First\"\n\tssid=" + "QA-First".encode().hex() + "\n"
               f"\t#psk=\"{OLD_FIRST}\"\n\tpsk={FIRST_HASH}\n\tid_str=\"0\"\n\tpriority=100\n\tscan_ssid=1\n}}\n")
SECOND_BLOCK = f"network={{\n    ssid=\"QA-Second\"\n    psk={SECOND_HASH}\n    key_mgmt=WPA-PSK\n    priority=99\n}}\n"
HEADER = "ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev\nupdate_config=1\nap_scan=1\nfast_reauth=1\ncountry=GB\n\n"
FIXTURE = HEADER + FIRST_BLOCK + "\n" + SECOND_BLOCK + "\n# kept verbatim\n"
SECRETS = (OLD_FIRST, OLD_SECOND, NEW_SECOND, NEW_THIRD, FIRST_HASH, SECOND_HASH)

UUID1, UUID2, UUID3, UUID4 = ("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222",
                              "33333333-3333-4333-8333-333333333333", "44444444-4444-4444-8444-444444444444")
NM_FIELDS = "connection.id,802-11-wireless.ssid,802-11-wireless.mode,802-11-wireless-security.key-mgmt"


def nm_detail(uuid):
    return f"nmcli -t -e no -f {NM_FIELDS} connection show uuid {uuid}"


NM_OUTPUTS = {
    "nmcli -t -f UUID,TYPE connection show": f"{UUID1}:802-11-wireless\n{UUID2}:802-11-wireless\n{UUID3}:802-3-ethernet\n{UUID4}:802-11-wireless\n",
    nm_detail(UUID1): "connection.id:Home\n802-11-wireless.ssid:QA-Home\n802-11-wireless.mode:infrastructure\n802-11-wireless-security.key-mgmt:wpa-psk\n",
    nm_detail(UUID2): "connection.id:Cafe\n802-11-wireless.ssid:QA:Cafe\n802-11-wireless.mode:infrastructure\n802-11-wireless-security.key-mgmt:\n",
    nm_detail(UUID4): "connection.id:Hotspot\n802-11-wireless.ssid:QA-AP\n802-11-wireless.mode:ap\n802-11-wireless-security.key-mgmt:wpa-psk\n",
}


class WifiBase(unittest.TestCase):
    backend = "wpa_supplicant"

    def setUp(self):
        helper._SETTINGS["WIFI_BACKEND"] = self.backend
        for name in ("calls.jsonl", "outputs.json", "fail.json"):
            (TMP / name).unlink(missing_ok=True)
        for p in (WIFI_KEYS["WPA_SUPPLICANT_CONF"], WIFI_KEYS["WIFI_REGDOM_CONF"]):
            p.unlink(missing_ok=True)
        shutil.rmtree(WIFI_KEYS["SYSFS_NET_DIR"], ignore_errors=True)
        WIFI_KEYS["SYSFS_NET_DIR"].mkdir()

    def outputs(self, mapping):
        (TMP / "outputs.json").write_text(json.dumps(mapping))

    def failing(self, prefixes):
        (TMP / "fail.json").write_text(json.dumps(prefixes))

    def calls(self):
        path = TMP / "calls.jsonl"
        return [json.loads(l) for l in path.read_text().splitlines()] if path.exists() else []

    def interface(self, operstate="up"):
        d = WIFI_KEYS["SYSFS_NET_DIR"] / "wlan0"
        (d / "statistics").mkdir(parents=True)
        (d / "operstate").write_text(operstate + "\n")
        (d / "address").write_text("b8:27:eb:00:00:01\n")
        for name, value in (("rx_packets", 120), ("rx_bytes", 567890), ("tx_packets", 80), ("tx_bytes", 2048)):
            (d / "statistics" / name).write_text(f"{value}\n")

    def assertNoSecret(self, value):
        text = value if isinstance(value, str) else json.dumps(value)
        for secret in SECRETS:
            self.assertNotIn(secret, text)


class WpaSupplicantTest(WifiBase):
    def setUp(self):
        super().setUp()
        self.conf = WIFI_KEYS["WPA_SUPPLICANT_CONF"]
        self.conf.write_text(FIXTURE)
        os.chmod(self.conf, 0o600)

    def state(self):
        return helper.wifi_read()

    def save(self, networks, country="GB", revision=None):
        return helper.wifi_write({"revision": revision or self.state()["revision"], "country": country, "networks": networks})

    def test_psk_hash_matches_reference(self):
        # IEEE 802.11i-2004 H.4.1 test vector, and wpa_passphrase when installed.
        self.assertEqual(helper.wpa_psk("IEEE", "password"), "f42c6fc52df0ebef9ebb4b90b38a5f902e83fe1b135a70e23aed762e9710a12e")
        self.assertEqual(helper.wpa_psk("QA-Second", NEW_SECOND), independent_psk("QA-Second", NEW_SECOND))
        tool = shutil.which("wpa_passphrase")
        if tool:
            out = subprocess.run([tool, "QA-Second", NEW_SECOND], capture_output=True, text=True, check=True).stdout
            self.assertIn("\tpsk=" + independent_psk("QA-Second", NEW_SECOND) + "\n", out)

    def test_read_lists_networks_without_keys(self):
        state = self.state()
        self.assertEqual(state["backend"], "wpa_supplicant")
        self.assertEqual(state["country"], "GB")
        self.assertEqual(state["networks"], [{"id": "0", "ssid": "QA-First", "secured": True},
                                             {"id": "1", "ssid": "QA-Second", "secured": True}])
        self.assertEqual(state["revision"], hashlib.sha256(FIXTURE.encode()).hexdigest())
        self.assertEqual(len(state["countries"]), 249)
        self.assertEqual(state["status"], {"present": False, "up": False})
        self.assertNoSecret(state)
        self.assertEqual(self.calls(), [])

    def test_delete_first_removes_only_that_block(self):
        reply = self.save([{"id": "1", "ssid": "QA-Second", "psk": ""}])
        self.assertEqual(self.conf.read_text(), HEADER + SECOND_BLOCK + "\n# kept verbatim\n")
        self.assertEqual(stat.S_IMODE(self.conf.stat().st_mode), 0o600)
        self.assertEqual(reply, {"ok": True, "message": "Wifi Settings Updated Successfully", "note": "Interface wlan0 is not present"})
        self.assertEqual(self.calls(), [])  # no interface: nothing to reconnect
        self.assertEqual(self.state()["networks"], [{"id": "0", "ssid": "QA-Second", "secured": True}])

    def test_delete_restores_private_mode_and_reconnects(self):
        os.chmod(self.conf, 0o644)
        self.interface()
        reply = self.save([{"id": "1", "ssid": "QA-Second", "psk": ""}])
        self.assertEqual(stat.S_IMODE(self.conf.stat().st_mode), 0o600)
        self.assertNotIn("note", reply)
        ctrl = str(WIFI_KEYS["WPA_CTRL_DIR"])
        self.assertEqual([c for c in self.calls() if c[0] == "wpa_cli"], [["wpa_cli", "-p", ctrl, "-i", "wlan0", "reconfigure"]])

    def test_auto_ap_interface_is_left_alone(self):
        self.interface()
        (WIFI_KEYS["SYSFS_NET_DIR"] / "wlan0_ap").mkdir()
        self.save([{"id": "1", "ssid": "QA-Second", "psk": ""}])
        self.assertEqual([c for c in self.calls() if c[0] == "wpa_cli"], [])

    def test_psk_change_rewrites_only_the_key_line(self):
        reply = self.save([{"id": "0", "ssid": "QA-First", "psk": ""}, {"id": "1", "ssid": "QA-Second", "psk": NEW_SECOND}])
        expected = FIXTURE.replace(f"    psk={SECOND_HASH}\n", f"    psk={independent_psk('QA-Second', NEW_SECOND)}\n")
        self.assertNotEqual(expected, FIXTURE)
        self.assertEqual(self.conf.read_text(), expected)  # first block, header and trailer byte-identical
        self.assertEqual(stat.S_IMODE(self.conf.stat().st_mode), 0o600)
        self.assertNoSecret(reply)
        self.assertNotIn(NEW_SECOND, self.conf.read_text())

    def test_psk_change_drops_classic_plaintext_comment(self):
        self.save([{"id": "0", "ssid": "QA-First", "psk": NEW_THIRD}, {"id": "1", "ssid": "QA-Second", "psk": ""}])
        block = FIRST_BLOCK.replace(f"\t#psk=\"{OLD_FIRST}\"\n", "").replace(FIRST_HASH, independent_psk("QA-First", NEW_THIRD))
        self.assertEqual(self.conf.read_text(), HEADER + block + "\n" + SECOND_BLOCK + "\n# kept verbatim\n")
        self.assertNotIn(OLD_FIRST, self.conf.read_text())

    def test_raw_hex_key_is_stored_as_given(self):
        key = "AB" * 32
        self.save([{"id": "0", "ssid": "QA-First", "psk": ""}, {"id": "1", "ssid": "QA-Second", "psk": key}])
        self.assertIn("    psk=" + key.lower() + "\n", self.conf.read_text())

    def test_ssid_change_needs_the_psk_again(self):
        before = self.conf.read_bytes()
        with self.assertRaises(helper.Error) as ctx:
            self.save([{"id": "0", "ssid": "QA-Renamed", "psk": ""}, {"id": "1", "ssid": "QA-Second", "psk": ""}])
        self.assertIn("network 0: enter the PSK again", str(ctx.exception))
        self.assertEqual(self.conf.read_bytes(), before)
        self.save([{"id": "0", "ssid": "QA-Renamed", "psk": NEW_THIRD}, {"id": "1", "ssid": "QA-Second", "psk": ""}])
        block = (FIRST_BLOCK.replace('#ssid="QA-First"', '#ssid="QA-Renamed"')
                 .replace("ssid=" + "QA-First".encode().hex(), "ssid=" + "QA-Renamed".encode().hex())
                 .replace(f"\t#psk=\"{OLD_FIRST}\"\n", "").replace(FIRST_HASH, independent_psk("QA-Renamed", NEW_THIRD)))
        self.assertEqual(self.conf.read_text(), HEADER + block + "\n" + SECOND_BLOCK + "\n# kept verbatim\n")

    def test_add_network_and_country(self):
        self.save([{"id": "0", "ssid": "QA-First", "psk": ""}, {"id": "1", "ssid": "QA-Second", "psk": ""},
                   {"ssid": "QA-Third", "psk": NEW_THIRD}, {"ssid": "QA Open", "psk": ""}], country="DE")
        third = ("network={\n\t#ssid=\"QA-Third\"\n\tssid=" + "QA-Third".encode().hex() + "\n"
                 f"\tpsk={independent_psk('QA-Third', NEW_THIRD)}\n\tid_str=\"2\"\n\tpriority=98\n\tscan_ssid=1\n}}\n")
        opened = ("network={\n\t#ssid=\"QA Open\"\n\tssid=" + "QA Open".encode().hex() + "\n"
                  "\tkey_mgmt=NONE\n\tid_str=\"3\"\n\tpriority=97\n\tscan_ssid=1\n}\n")
        expected = FIXTURE.replace("country=GB\n", "country=DE\n") + "\n" + third + "\n" + opened + "\n"
        self.assertEqual(self.conf.read_text(), expected)
        nets = self.state()["networks"]
        self.assertEqual([(n["ssid"], n["secured"]) for n in nets],
                         [("QA-First", True), ("QA-Second", True), ("QA-Third", True), ("QA Open", False)])
        self.assertEqual(self.state()["country"], "DE")

    def test_missing_country_line_is_inserted_before_the_blocks(self):
        self.conf.write_text(FIXTURE.replace("country=GB\n", ""))
        self.assertEqual(self.state()["country"], "JP")  # wifi.php default
        self.save([{"id": "0", "ssid": "QA-First", "psk": ""}, {"id": "1", "ssid": "QA-Second", "psk": ""}], country="FR")
        self.assertEqual(self.conf.read_text(), FIXTURE.replace("country=GB\n", "country=FR\n"))

    def test_missing_file_is_created_private(self):
        self.conf.unlink()
        self.assertEqual(self.state()["networks"], [])
        self.save([{"ssid": "QA-New", "psk": NEW_THIRD}], country="US")
        ctrl = WIFI_KEYS["WPA_CTRL_DIR"]
        self.assertEqual(self.conf.read_text(),
                         f"ctrl_interface=DIR={ctrl} GROUP=netdev\nupdate_config=1\nap_scan=1\nfast_reauth=1\ncountry=US\n\n"
                         "network={\n\t#ssid=\"QA-New\"\n\tssid=" + "QA-New".encode().hex() + "\n"
                         f"\tpsk={independent_psk('QA-New', NEW_THIRD)}\n\tid_str=\"0\"\n\tpriority=100\n\tscan_ssid=1\n}}\n\n")
        self.assertEqual(stat.S_IMODE(self.conf.stat().st_mode), 0o600)

    def test_stale_revision_is_refused_without_writing(self):
        before, inode = self.conf.read_bytes(), self.conf.stat().st_ino
        with self.assertRaises(helper.Stale):
            self.save([{"id": "1", "ssid": "QA-Second", "psk": ""}], revision="0" * 64)
        self.assertEqual(self.conf.read_bytes(), before)
        self.assertEqual(self.conf.stat().st_ino, inode)
        self.assertEqual(self.calls(), [])

    def test_invalid_input_is_refused(self):
        before = self.conf.read_bytes()
        keep = {"id": "1", "ssid": "QA-Second", "psk": ""}
        for networks, country, why in (
            ([{"ssid": "", "psk": ""}], "GB", "network 0: SSID"),
            ([{"ssid": "x" * 33, "psk": ""}], "GB", "network 0: SSID"),
            ([{"ssid": "QA\nnetwork={", "psk": ""}], "GB", "network 0: SSID"),
            ([keep, {"ssid": "QA-Short", "psk": "seven77"}], "GB", "network 1: PSK"),
            ([keep, {"ssid": "QA-Long", "psk": "p" * 64}], "GB", "network 1: PSK"),
            ([keep, {"ssid": "QA-Utf", "psk": "пароль-пароль"}], "GB", "network 1: PSK"),
            ([keep], "gb", "country code"),
            ([keep], "ZZ", "country code"),
            ([keep], "GBR", "country code"),
            ([keep] * 2, "GB", "repeated"),
            ([{"id": "7", "ssid": "QA", "psk": ""}], "GB", "unknown"),
            ([{"ssid": "QA", "psk": "x" * 8}] * 33, "GB", "network count"),
        ):
            with self.subTest(why=why):
                with self.assertRaises(helper.Error) as ctx:
                    self.save(networks, country)
                self.assertIn(why, str(ctx.exception))
                self.assertNoSecret(str(ctx.exception))
                self.assertNotIn("пароль", str(ctx.exception))
        self.assertEqual(self.conf.read_bytes(), before)

    def test_scan_and_actions_report_a_missing_interface(self):
        for call in (helper.wifi_scan, lambda: helper.wifi_action("reset"), lambda: helper.wifi_action("down"),
                     lambda: helper.wifi_action("up")):
            with self.assertRaises(helper.Error) as ctx:
                call()
            self.assertEqual(str(ctx.exception), "Interface wlan0 is not present")
        self.assertEqual(self.calls(), [])

    def test_scan(self):
        self.interface()
        ctrl = str(WIFI_KEYS["WPA_CTRL_DIR"])
        base = f"wpa_cli -p {ctrl} -i wlan0"
        self.outputs({base + " scan": "OK\n", base + " scan_results":
                      "bssid / frequency / signal level / flags / ssid\n"
                      "aa:bb:cc:dd:ee:01\t2437\t-45\t[WPA2-PSK-CCMP][ESS]\tQA-Scan\n"
                      "aa:bb:cc:dd:ee:02\t5180\t-70\t[ESS]\t\n"})
        self.assertEqual(helper.wifi_scan(), {"networks": [
            {"bssid": "aa:bb:cc:dd:ee:01", "freq": "2437", "signal": "-45 dBm", "security": "[WPA2-PSK-CCMP][ESS]", "ssid": "QA-Scan"},
            {"bssid": "aa:bb:cc:dd:ee:02", "freq": "5180", "signal": "-70 dBm", "security": "[ESS]", "ssid": ""}]})
        self.assertEqual(self.calls(), [["wpa_cli", "-p", ctrl, "-i", "wlan0", "scan"], ["wpa_cli", "-p", ctrl, "-i", "wlan0", "scan_results"]])

    def test_actions(self):
        self.interface()
        ctrl = str(WIFI_KEYS["WPA_CTRL_DIR"])
        self.assertEqual(helper.wifi_action("up"), {"ok": True, "message": "Interface already up"})
        self.assertEqual(helper.wifi_action("reset")["message"], "WiFi adapter reset")
        self.assertEqual(helper.wifi_action("down")["message"], "Interface wlan0 is down")
        wpa, link = ["wpa_cli", "-p", ctrl, "-i", "wlan0"], ["ip", "link", "set", "dev", "wlan0"]
        self.assertEqual([c for c in self.calls() if c[:2] != ["ip", "-o"] and c[1] != "dev"],
                         [wpa + ["reconfigure"], link + ["down"], link + ["up"], wpa + ["scan"], link + ["down"]])
        self.failing(["ip link set dev wlan0 down"])
        with self.assertRaises(helper.Error) as ctx:
            helper.wifi_action("down")
        self.assertEqual(str(ctx.exception), "Interface wlan0: the down command failed")
        with self.assertRaises(helper.Error):
            helper.wifi_action("restart")

    def test_status(self):
        self.interface()
        self.outputs({
            "ip -o -4 addr show dev wlan0": "3: wlan0    inet 192.168.1.50/24 brd 192.168.1.255 scope global dynamic wlan0\\       valid_lft 86000sec\n",
            "iw dev wlan0 link": "Connected to aa:bb:cc:dd:ee:01 (on wlan0)\n\tSSID: QA-Second\n\tfreq: 2437\n\tRX: 1 bytes (1 packets)\n"
                                 "\tsignal: -48 dBm\n\ttx bitrate: 72.2 MBit/s\n",
            "iw dev wlan0 info": "Interface wlan0\n\tifindex 3\n\ttype managed\n\ttxpower 31.00 dBm\n"})
        self.assertEqual(self.state()["status"], {
            "present": True, "up": True, "mac": "b8:27:eb:00:00:01", "rxPackets": 120, "rxBytes": 567890, "txPackets": 80,
            "txBytes": 2048, "ip": "192.168.1.50", "netmask": "255.255.255.0", "bssid": "aa:bb:cc:dd:ee:01",
            "ssid": "QA-Second", "freq": "2437", "signal": "-48 dBm", "bitrate": "72.2 MBit/s", "txPower": "31.00 dBm"})


class NetworkManagerTest(WifiBase):
    backend = "networkmanager"

    def setUp(self):
        super().setUp()
        self.outputs(NM_OUTPUTS)

    def test_read_lists_client_profiles_without_secrets(self):
        state = helper.wifi_read()
        self.assertEqual(state["backend"], "networkmanager")
        self.assertEqual(state["networks"], [{"id": UUID1, "ssid": "QA-Home", "secured": True},
                                             {"id": UUID2, "ssid": "QA:Cafe", "secured": False}])
        self.assertEqual(state["country"], "JP")
        reads = [["nmcli", "-t", "-f", "UUID,TYPE", "connection", "show"]] + [
            ["nmcli", "-t", "-e", "no", "-f", NM_FIELDS, "connection", "show", "uuid", u] for u in (UUID1, UUID2, UUID4)]
        self.assertEqual(self.calls(), reads + [["iw", "reg", "get"]])
        self.assertFalse(any("-s" in c or "--show-secrets" in c for c in self.calls()))
        self.assertNoSecret(state)

    def test_country_from_file_or_regulatory_domain(self):
        self.outputs(dict(NM_OUTPUTS, **{"iw reg get": "global\ncountry DE: DFS-ETSI\n"}))
        self.assertEqual(helper.wifi_read()["country"], "DE")
        WIFI_KEYS["WIFI_REGDOM_CONF"].write_text("options cfg80211 ieee80211_regdom=GB\n")
        self.assertEqual(helper.wifi_read()["country"], "GB")

    def test_write_runs_exact_nmcli_commands(self):
        self.interface()
        state = helper.wifi_read()
        before = len(self.calls())
        reply = helper.wifi_write({"revision": state["revision"], "country": "GB", "networks": [
            {"id": UUID2, "ssid": "QA:Cafe", "psk": NEW_SECOND},       # open -> WPA
            {"ssid": "QA-New", "psk": NEW_THIRD},                      # added
            {"ssid": "QA-Open", "psk": ""},                            # added, open
        ]})                                                            # UUID1 deleted
        self.assertEqual(reply, {"ok": True, "message": "Wifi Settings Updated Successfully"})
        sec = "802-11-wireless-security"
        writes = self.calls()[before:]
        # Exact sequence after the revision re-read: modify, add, add, delete, country, connect.
        tail = [c for c in writes if c[:2] != ["nmcli", "-t"] and c[:3] != ["iw", "reg", "get"]]
        self.assertEqual(tail, [
            ["nmcli", "connection", "modify", "uuid", UUID2, f"{sec}.key-mgmt", "wpa-psk", f"{sec}.psk", independent_psk("QA:Cafe", NEW_SECOND)],
            ["nmcli", "connection", "add", "type", "wifi", "ifname", "wlan0", "con-name", "QA-New", "802-11-wireless.ssid", "QA-New",
             f"{sec}.key-mgmt", "wpa-psk", f"{sec}.psk", independent_psk("QA-New", NEW_THIRD)],
            ["nmcli", "connection", "add", "type", "wifi", "ifname", "wlan0", "con-name", "QA-Open", "802-11-wireless.ssid", "QA-Open"],
            ["nmcli", "connection", "delete", "uuid", UUID1],
            ["iw", "reg", "set", "GB"],
            ["nmcli", "device", "connect", "wlan0"],
        ])
        # The passphrase itself is never passed to nmcli, nor returned.
        for call in self.calls():
            self.assertNotIn(NEW_SECOND, " ".join(call))
            self.assertNotIn(NEW_THIRD, " ".join(call))
        self.assertNoSecret(reply)
        self.assertEqual(WIFI_KEYS["WIFI_REGDOM_CONF"].read_text(),
                         "# Wi-Fi country set by the Pi-Star dashboard\noptions cfg80211 ieee80211_regdom=GB\n")

    def test_unchanged_profiles_are_not_touched(self):
        state = helper.wifi_read()
        before = len(self.calls())
        helper.wifi_write({"revision": state["revision"], "country": "JP", "networks": [
            {"id": UUID1, "ssid": "QA-Home", "psk": ""}, {"id": UUID2, "ssid": "QA:Cafe", "psk": ""}]})
        self.assertEqual([c for c in self.calls()[before:] if c[:2] != ["nmcli", "-t"] and c[:3] != ["iw", "reg", "get"]], [])
        self.assertFalse(WIFI_KEYS["WIFI_REGDOM_CONF"].exists())

    def test_ssid_rename_of_secured_profile_needs_psk(self):
        state = helper.wifi_read()
        with self.assertRaises(helper.Error):
            helper.wifi_write({"revision": state["revision"], "country": "JP", "networks": [{"id": UUID1, "ssid": "QA-Renamed", "psk": ""}]})
        helper.wifi_write({"revision": state["revision"], "country": "JP", "networks": [{"id": UUID1, "ssid": "QA-Renamed", "psk": NEW_THIRD}]})
        self.assertIn(["nmcli", "connection", "modify", "uuid", UUID1, "802-11-wireless.ssid", "QA-Renamed",
                       "802-11-wireless-security.key-mgmt", "wpa-psk", "802-11-wireless-security.psk",
                       independent_psk("QA-Renamed", NEW_THIRD)], self.calls())

    def test_stale_revision_runs_no_change(self):
        with self.assertRaises(helper.Stale):
            helper.wifi_write({"revision": "0" * 64, "country": "GB", "networks": []})
        self.assertEqual([c for c in self.calls() if c[:2] != ["nmcli", "-t"] and c[:3] != ["iw", "reg", "get"]], [])
        self.assertFalse(WIFI_KEYS["WIFI_REGDOM_CONF"].exists())

    def test_refused_change_reports_without_secret(self):
        state = helper.wifi_read()
        self.failing(["nmcli connection add"])
        with self.assertRaises(helper.Error) as ctx:
            helper.wifi_write({"revision": state["revision"], "country": "JP", "networks": [
                {"id": UUID1, "ssid": "QA-Home", "psk": ""}, {"id": UUID2, "ssid": "QA:Cafe", "psk": ""}, {"ssid": "QA-New", "psk": NEW_THIRD}]})
        self.assertEqual(str(ctx.exception), "NetworkManager refused a change (0 of 1 applied); reload the page")
        self.assertNoSecret(str(ctx.exception))

    def test_nmcli_missing(self):
        self.failing(["nmcli -t -f UUID,TYPE"])
        with self.assertRaises(helper.Error) as ctx:
            helper.wifi_read()
        self.assertIn("NetworkManager is not available", str(ctx.exception))

    def test_scan_and_reset(self):
        self.interface()
        scan = "nmcli -t -f BSSID,FREQ,SIGNAL,SECURITY,SSID device wifi list --rescan yes ifname wlan0"
        self.outputs(dict(NM_OUTPUTS, **{scan: "AA\\:BB\\:CC\\:DD\\:EE\\:01:2437 MHz:70:WPA2:QA\\:Scan\nAA\\:BB\\:CC\\:DD\\:EE\\:02:5180 MHz:30::\n"}))
        self.assertEqual(helper.wifi_scan(), {"networks": [
            {"bssid": "AA:BB:CC:DD:EE:01", "freq": "2437", "signal": "70 %", "security": "WPA2", "ssid": "QA:Scan"},
            {"bssid": "AA:BB:CC:DD:EE:02", "freq": "5180", "signal": "30 %", "security": "None", "ssid": ""}]})
        helper.wifi_action("reset")
        self.assertEqual([c for c in self.calls() if c[:2] == ["nmcli", "device"]], [
            ["nmcli", "device", "disconnect", "wlan0"], ["nmcli", "device", "connect", "wlan0"],
            ["nmcli", "device", "wifi", "rescan", "ifname", "wlan0"]])

    def test_scan_without_interface(self):
        with self.assertRaises(helper.Error) as ctx:
            helper.wifi_scan()
        self.assertEqual(str(ctx.exception), "Interface wlan0 is not present")


class CommandLineTest(WifiBase):
    """The sudo entry point: JSON on stdout, exit 3 for a stale form, 2 for a refusal."""

    def run_helper(self, *args, stdin=b""):
        env = dict(os.environ, PISTAR_SETTINGS=str(SETTINGS))
        return subprocess.run([sys.executable, "-I", str(HELPER), *args], input=stdin, env=env,
                              capture_output=True, timeout=60)

    def test_exit_codes(self):
        conf = WIFI_KEYS["WPA_SUPPLICANT_CONF"]
        self.failing(["nmcli"])
        out = self.run_helper("wifi-read")  # the settings file names networkmanager
        self.assertEqual(out.returncode, 2)
        self.assertIn("NetworkManager is not available", json.loads(out.stdout)["error"])
        settings = SETTINGS.read_text()
        try:
            SETTINGS.write_text(settings.replace("WIFI_BACKEND=networkmanager", "WIFI_BACKEND=wpa_supplicant"))
            conf.write_text(FIXTURE)
            out = self.run_helper("wifi-read")
            self.assertEqual(out.returncode, 0)
            state = json.loads(out.stdout)
            self.assertNoSecret(out.stdout.decode())
            stale = self.run_helper("wifi-write", stdin=json.dumps({"revision": "0" * 64, "country": "GB", "networks": []}).encode())
            self.assertEqual((stale.returncode, json.loads(stale.stdout)["stale"]), (3, True))
            bad = self.run_helper("wifi-write", stdin=json.dumps({"revision": state["revision"], "country": "GB",
                                                                   "networks": [{"ssid": "QA", "psk": "short"}]}).encode())
            self.assertEqual(bad.returncode, 2)
            self.assertEqual(json.loads(bad.stdout)["error"], "network 0: PSK must be 8-63 printable ASCII characters or exactly 64 hex characters")
            self.assertEqual(self.run_helper("wifi-write", stdin=b"{not json").returncode, 2)
            self.assertEqual(conf.read_text(), FIXTURE)
            ok = self.run_helper("wifi-write", stdin=json.dumps({"revision": state["revision"], "country": "GB",
                                                                  "networks": [{"id": "1", "ssid": "QA-Second", "psk": ""}]}).encode())
            self.assertEqual(ok.returncode, 0, ok.stdout)
            self.assertEqual(conf.read_text(), HEADER + SECOND_BLOCK + "\n# kept verbatim\n")
            missing = self.run_helper("wifi-action", "reset")
            self.assertEqual((missing.returncode, json.loads(missing.stdout)), (2, {"error": "Interface wlan0 is not present"}))
            self.assertEqual(self.run_helper("wifi-action", "reboot").returncode, 2)
        finally:
            SETTINGS.write_text(settings)


if __name__ == "__main__":
    unittest.main()
