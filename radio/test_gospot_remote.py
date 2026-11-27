"""radio/sbin/gospot-remote: RF keeper commands from Pi-Star logs.

The script is loaded as a module and driven with temporary configs, logs, a
fake /proc, a command recorder and a fixed clock; the program and its
handler also run for real against a recording systemctl on PATH. Log lines
copy the formats of the Pi-Star PS4 MMDVMHost, YSFGateway and DStarRepeater
builds."""
from datetime import datetime, timedelta, timezone
import importlib.machinery
import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
SCRIPT = HERE / "sbin" / "gospot-remote"
HANDLER = HERE / "sbin" / "gospot-remote.service"


def load(path, name):
    loader = importlib.machinery.SourceFileLoader(name, str(path))
    spec = importlib.util.spec_from_loader(name, loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


remote = load(SCRIPT, "gospot_remote")
sys.dont_write_bytecode = True

NOW = datetime(2026, 11, 27, 12, 0, 0)
DAY = NOW.strftime("%Y-%m-%d")

REMOTE_CONF = """[banner]
# Pi-Star Remote config file

[enable]
# Is the Pi-Star Remote Enabled? (true|false)
enabled=true

[keeper]
callsign=M1ABC

[d-star]
svckill=SVCKILL
svcrestart=SVCRSTRT
reboot=REBOOTPI
shutdown=SHUTDOWN
hostfiles=HOSTFILE
getip=GETIP
wifissid=WIFISSID
wifirssi=WIFIRSSI
8Ball=8BALL

[dmr]
svckill=9999999
svcrestart=9999998
reboot=9999997
shutdown=9999996
hostfiles=9999995

[ysf]
svckill=99999
svcrestart=99998
reboot=99997
shutdown=99996
hostfiles=99995

[p25]
svckill=65531
svcrestart=65532
reboot=65533
shutdown=65534
hostfiles=65535
"""

MMDVMHOST_CONF = """[General]
Callsign=M1ABC
Id=1234567
[Log]
DisplayLevel=1
FileLevel=2
FilePath={log}
FileRoot=MMDVM
[D-Star]
Enable=1
Module=B
"""

YSFGATEWAY_CONF = """[General]
Callsign=M1ABC
[Log]
FilePath={log}
FileRoot=YSFGateway
"""

UNITS = {
    "REMOTE_MMDVMHOST_STOP_UNITS": "mmdvmhost.timer,gospot-watchdog.timer,mmdvmhost.service,gospot-watchdog.service",
    "REMOTE_DSTARREPEATER_STOP_UNITS":
        "dstarrepeater.timer,gospot-watchdog.timer,dstarrepeater.service,gospot-watchdog.service",
    "REMOTE_DMR_RESTART_UNITS": "mmdvmhost.service,dmrgateway.service",
    "REMOTE_P25_RESTART_UNITS": "mmdvmhost.service,dmrgateway.service",
    "REMOTE_DSTAR_RESTART_UNITS": "ircddbgateway.service,mmdvmhost.service",
    "REMOTE_YSF_RESTART_UNITS": "ysfgateway.service,mmdvmhost.service",
    "REMOTE_DSTARREPEATER_RESTART_UNITS": "ircddbgateway.service,dstarrepeater.service",
}
MMDVM_STOP = [["systemctl", "stop", u] for u in UNITS["REMOTE_MMDVMHOST_STOP_UNITS"].split(",")]
IP_OUTPUT = """1: lo    inet 127.0.0.1/8 scope host lo\\       valid_lft forever preferred_lft forever
2: eth0    inet6 2001:db8::5/64 scope global dynamic\\       valid_lft 86000sec preferred_lft 14000sec
2: eth0    inet 192.168.1.10/24 brd 192.168.1.255 scope global dynamic eth0\\       valid_lft 86000sec
"""
IW_OUTPUT = """Connected to 11:22:33:44:55:66 (on wlan0)
\tSSID: HamNet
\tfreq: 2412
\tsignal: -52 dBm
\ttx bitrate: 65.0 MBit/s
"""


def stamp(t, ms=True):
    return t.strftime("%Y-%m-%d %H:%M:%S") + (".%03d" % (t.microsecond // 1000) if ms else "")


def mmdvm(text, ago=10):
    return f"M: {stamp(NOW - timedelta(seconds=ago))} {text}\n"


def dstarrepeater(text, ago=10):
    return f"M: {stamp(NOW - timedelta(seconds=ago), ms=False)}: {text}\n"


def dstar_header(call="M1ABC", your="SVCKILL", suffix="1234"):
    return f"D-Star, received RF header from {call:<8.8}/{suffix:<4.4} to {your:<8.8}"


def repeater_header(call="M1ABC", your="SVCKILL"):
    return (f"Radio header decoded - My: {call:<8.8}/{'M':<4.4}  Your: {your:<8.8}  "
            f"Rpt1: M1ABC  B  Rpt2: M1ABC  G  Flags: 40 00 00")


def ysf_header(call="M1ABC"):
    return f"YSF, received RF header from {call:<10.10} to DG-ID 0"


class Recorder:
    """Records commands; ip and iw answer with canned output."""

    def __init__(self, root):
        self.root, self.calls, self.ip, self.iw = root, [], IP_OUTPUT, IW_OUTPUT

    def __call__(self, argv, capture=False):
        self.calls.append(list(argv))
        if argv[0].endswith("/ip"):
            return 0, self.ip
        if argv[0].endswith("/iw"):
            return (0, self.iw) if self.iw is not None else (1, "")
        return 0, ""


class Fixture(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = root = Path(tmp.name)
        for d in ("etc", "bin", "sbin", "log", "proc", "sys"):
            (root / d).mkdir()
        (root / "etc" / "gospot-remote").write_text(REMOTE_CONF)
        (root / "etc" / "mmdvmhost").write_text(MMDVMHOST_CONF.format(log=root / "log"))
        (root / "etc" / "ysfgateway").write_text(YSFGATEWAY_CONF.format(log=root / "log"))
        (root / "bin" / "texttransmitd").write_text("")
        (root / "sys" / "wlan0").symlink_to(root / "proc")
        self.paths = {
            "GOSPOT_BIN_DIR": str(root / "bin"), "GOSPOT_SBIN_DIR": str(root / "sbin"),
            "GOSPOT_LOG_DIR": str(root / "log"), "CONF_GOSPOT_REMOTE": str(root / "etc" / "gospot-remote"),
            "CONF_MMDVMHOST": str(root / "etc" / "mmdvmhost"), "CONF_YSFGATEWAY": str(root / "etc" / "ysfgateway"),
            "IP_BIN": "/usr/bin/ip", "IW_BIN": "/usr/sbin/iw", "SYSFS_NET_DIR": str(root / "sys"),
            "WIFI_INTERFACE": "wlan0", **UNITS,
        }
        self.processes("MMDVMHost", "YSFGateway")
        self.clock = NOW
        self.sleeps, self.logged = [], []
        patcher = mock.patch.object(remote, "log", self.logged.append)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.new_remote()

    def new_remote(self):
        """A remote that starts afresh, with no log files."""
        for f in (self.root / "log").iterdir():
            f.unlink()
        self.rec = Recorder(self.root)
        self.remote = remote.Remote(remote.Settings(self.paths), run=self.rec, sleep=self.sleeps.append,
                                    clock=lambda: self.clock, proc_dir=str(self.root / "proc"))
        self.remote.root_readonly = lambda: False

    def processes(self, *names):
        proc = self.root / "proc"
        for d in proc.iterdir():
            (d / "comm").unlink()
            d.rmdir()
        for pid, name in enumerate(names, 100):
            (proc / str(pid)).mkdir()
            (proc / str(pid) / "comm").write_text(name + "\n")

    def append(self, name, *lines):
        with open(self.root / "log" / f"{name}-{DAY}.log", "a") as f:
            f.write("".join(lines))

    def poll(self):
        self.remote.poll()
        return self.rec.calls

    def text(self, message):
        return [str(self.root / "bin" / "texttransmitd"), "-text", message, "M1ABC  B"]


class DMR(Fixture):
    def test_every_command_for_a_group_or_private_call(self):
        cases = {
            "9999999": MMDVM_STOP,
            "9999998": [["systemctl", "restart", "mmdvmhost.service"], ["systemctl", "restart", "dmrgateway.service"]],
            "9999997": [["systemctl", "reboot"]],
            "9999996": [["systemctl", "poweroff"]],
            "9999995": [[str(self.root / "sbin" / "HostFilesUpdate.sh")]],
        }
        for tg, expected in cases.items():
            for to in (f"TG {tg}", tg):
                with self.subTest(to=to):
                    self.new_remote()
                    self.append("MMDVM", mmdvm(f"DMR Slot 2, received RF voice header from M1ABC to {to}"))
                    self.assertEqual(self.poll(), expected)
        self.assertEqual(self.sleeps, [30, 30])  # after each restart, as in Pi-Star

    def test_ignores_other_callsigns_targets_and_network_traffic(self):
        self.append("MMDVM",
                    mmdvm("DMR Slot 2, received RF voice header from M1ABD to TG 9999999"),
                    mmdvm("DMR Slot 2, received RF voice header from M1ABCD to TG 9999999"),
                    mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 99999990"),
                    mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 999999"),
                    mmdvm("DMR Slot 2, received network voice header from M1ABC to TG 9999999"),
                    mmdvm("DMR Slot 2, received RF end of voice transmission from M1ABC to TG 9999999, "
                          "1.2 seconds, BER: 0.0%"),
                    mmdvm("DMR Slot 2, received RF late entry from M1ABC to TG 9999999"))
        self.assertEqual(self.poll(), [])

    def test_hostfiles_remounts_a_read_only_root_first(self):
        self.remote.root_readonly = lambda: True
        self.append("MMDVM", mmdvm("DMR Slot 1, received RF voice header from M1ABC to TG 9999995"))
        self.assertEqual(self.poll(), [["mount", "-o", "remount,rw", "/"],
                                       [str(self.root / "sbin" / "HostFilesUpdate.sh")]])

    def test_commented_command_is_not_configured(self):
        conf = self.root / "etc" / "gospot-remote"
        conf.write_text(REMOTE_CONF.replace("shutdown=9999996", "#shutdown=9999996"))
        self.new_remote()
        self.append("MMDVM", mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 9999996"))
        self.assertEqual(self.poll(), [])
        conf.write_text(REMOTE_CONF.replace("shutdown=9999996", "shutdown=ALL"))
        self.assertNotIn("shutdown", remote.Settings(self.paths).commands["dmr"])


class P25(Fixture):
    def test_every_command(self):
        cases = {
            "65531": MMDVM_STOP,
            "65532": [["systemctl", "restart", "mmdvmhost.service"], ["systemctl", "restart", "dmrgateway.service"]],
            "65533": [["systemctl", "reboot"]],
            "65534": [["systemctl", "poweroff"]],
            "65535": [[str(self.root / "sbin" / "HostFilesUpdate.sh")]],
        }
        for tg, expected in cases.items():
            with self.subTest(tg=tg):
                self.new_remote()
                self.append("MMDVM", mmdvm(f"P25, received RF voice transmission from M1ABC to TG {tg}"))
                self.assertEqual(self.poll(), expected)

    def test_ignores_other_callsigns_and_network(self):
        self.append("MMDVM", mmdvm("P25, received RF voice transmission from G4KLX to TG 65531"),
                    mmdvm("P25, received network transmission from M1ABC to TG 65531"))
        self.assertEqual(self.poll(), [])


class DStarViaMMDVMHost(Fixture):
    def test_service_commands_are_announced_first(self):
        cases = {
            "SVCKILL": ("Shutting down at keeper request", MMDVM_STOP, [5]),
            "SVCRSTRT": ("Restarting services at keeper request",
                         [["systemctl", "restart", "ircddbgateway.service"],
                          ["systemctl", "restart", "mmdvmhost.service"]], [5, 30]),
            "REBOOTPI": ("Rebooting OS at keeper request", [["systemctl", "reboot"]], [5]),
            "SHUTDOWN": ("Shutting down OS at keeper request", [["systemctl", "poweroff"]], [5]),
            "HOSTFILE": ("Updating Hostfiles at keeper request",
                         [[str(self.root / "sbin" / "HostFilesUpdate.sh")]], [5]),
        }
        for ur, (message, expected, sleeps) in cases.items():
            with self.subTest(ur=ur):
                self.new_remote()
                self.sleeps.clear()
                self.append("MMDVM", mmdvm(dstar_header(your=ur)))
                self.assertEqual(self.poll(), [self.text(message)] + expected)
                self.assertEqual(self.sleeps, sleeps)

    def test_status_answers(self):
        self.append("MMDVM", mmdvm(dstar_header(your="GETIP"), 40), mmdvm(dstar_header(your="WIFISSID"), 30),
                    mmdvm(dstar_header(your="WIFIRSSI"), 20))
        self.assertEqual(self.poll(), [
            ["/usr/bin/ip", "-o", "addr", "show", "scope", "global"], self.text("IP: 192.168.1.10"),
            ["/usr/sbin/iw", "dev", "wlan0", "link"], self.text("SSID: HamNet"),
            ["/usr/sbin/iw", "dev", "wlan0", "link"], self.text("RSSI: -52 dBm")])

    def test_ipv6_only_and_no_address(self):
        self.rec.ip = "2: eth0    inet6 2001:db8::5/64 scope global dynamic\\  valid_lft forever\n"
        self.append("MMDVM", mmdvm(dstar_header(your="GETIP"), 40))
        self.assertEqual(self.poll()[-1], self.text("IP: 2001:db8::5"))
        self.rec.ip = ""
        self.append("MMDVM", mmdvm(dstar_header(your="GETIP"), 5))
        self.assertEqual(self.poll()[-1], self.text("IP: none"))

    def test_wifi_not_connected_or_absent(self):
        self.rec.iw = "Not connected.\n"
        self.append("MMDVM", mmdvm(dstar_header(your="WIFISSID"), 40))
        self.assertEqual(self.poll()[-1], self.text("WiFi: Not connected"))
        (self.root / "sys" / "wlan0").unlink()
        self.rec.calls.clear()
        self.append("MMDVM", mmdvm(dstar_header(your="WIFIRSSI"), 5))
        self.assertEqual(self.poll(), [self.text("WiFi: Not connected")])  # iw not even asked

    def test_magic_8ball(self):
        self.append("MMDVM", mmdvm(dstar_header(your="8BALL")))
        calls = self.poll()
        self.assertEqual(len(calls), 1)
        self.assertEqual(calls[0], self.text(calls[0][2]))
        self.assertIn(calls[0][2], remote.MAGIC_8BALL)

    def test_only_the_keeper_and_the_whole_ur_field(self):
        # Pi-Star 1.9 tested only "UR in line", so anyone could reboot the hotspot.
        self.append("MMDVM", mmdvm(dstar_header(call="G4KLX", your="SVCKILL")),
                    mmdvm(dstar_header(call="M1ABCD", your="SVCKILL")),
                    mmdvm(dstar_header(your="SVCKILLX")),
                    mmdvm(dstar_header(your="CQCQCQ")),
                    mmdvm("D-Star, received RF header for wrong repeater (M1ABC  C) from M1ABC   "),
                    mmdvm("D-Star, received network header from M1ABC   /1234 to SVCKILL  via REF001 C"),
                    mmdvm(f"D-Star, received RF late entry from M1ABC   /1234 to SVCKILL "))
        self.assertEqual(self.poll(), [])

    def test_lowercase_keeper_in_config(self):
        (self.root / "etc" / "gospot-remote").write_text(REMOTE_CONF.replace("callsign=M1ABC", "callsign=m1abc"))
        self.new_remote()
        self.append("MMDVM", mmdvm(dstar_header(your="REBOOTPI")))
        self.assertIn(["systemctl", "reboot"], self.poll())

    def test_without_texttransmitd_or_module_commands_still_run(self):
        (self.root / "bin" / "texttransmitd").unlink()
        self.new_remote()
        self.append("MMDVM", mmdvm(dstar_header(your="REBOOTPI")))
        self.assertEqual(self.poll(), [["systemctl", "reboot"]])
        (self.root / "bin" / "texttransmit").write_text("")  # the old Pi-Star name
        (self.root / "etc" / "mmdvmhost").write_text(MMDVMHOST_CONF.format(log=self.root / "log"))
        settings = remote.Settings(self.paths)
        self.assertEqual(settings.text_transmit, str(self.root / "bin" / "texttransmit"))
        (self.root / "etc" / "mmdvmhost").write_text("[Log]\nFilePath=%s\nFileRoot=MMDVM\n" % (self.root / "log"))
        self.assertIsNone(remote.Settings(self.paths).dstar_repeater)


class YSF(Fixture):
    def test_every_command_after_a_keeper_header(self):
        cases = {
            "99999": MMDVM_STOP,
            "99998": [["systemctl", "restart", "ysfgateway.service"], ["systemctl", "restart", "mmdvmhost.service"]],
            "99997": [["systemctl", "reboot"]],
            "99996": [["systemctl", "poweroff"]],
            "99995": [[str(self.root / "sbin" / "HostFilesUpdate.sh")]],
        }
        gateway_lines = ("Trying to find non existent YSF reflector with an id of {}",
                         "Received Connect to {} from M1ABC     ",
                         "Trying to find non existent reflector with an id of {}")
        for room, expected in cases.items():
            for shape in gateway_lines:
                with self.subTest(room=room, shape=shape):
                    self.new_remote()
                    self.append("MMDVM", mmdvm(ysf_header(), 12))
                    self.append("YSFGateway", mmdvm(shape.format(room), 10))
                    self.assertEqual(self.poll(), expected)

    def test_needs_the_keeper_header_and_the_gateway(self):
        self.append("YSFGateway", mmdvm("Trying to find non existent YSF reflector with an id of 99997", 50))
        self.append("MMDVM", mmdvm(ysf_header("G4KLX"), 22))
        self.append("YSFGateway", mmdvm("Trying to find non existent YSF reflector with an id of 99997", 20))
        self.append("MMDVM", mmdvm(ysf_header(), 12))
        self.append("YSFGateway", mmdvm("Received Connect to 99997 from G4KLX     ", 10),
                    mmdvm("Received Connect to 12345 from M1ABC     ", 9),
                    mmdvm("Connect to 99997 - \"ROOM\" has been requested by M1ABC     ", 8))
        self.assertEqual(self.poll(), [])
        self.processes("MMDVMHost")  # YSFGateway not running
        self.append("YSFGateway", mmdvm("Trying to find non existent YSF reflector with an id of 99997", 5))
        self.assertEqual(self.poll(), [])
        self.processes("MMDVMHost", "YSFGateway")
        self.append("YSFGateway", mmdvm("Trying to find non existent YSF reflector with an id of 99997", 4))
        self.assertEqual(self.poll(), [["systemctl", "reboot"]])

    def test_a_header_seen_in_an_earlier_pass_still_counts(self):
        self.append("MMDVM", mmdvm(ysf_header(), 20))
        self.assertEqual(self.poll(), [])
        self.append("YSFGateway", mmdvm("Trying to find non existent YSF reflector with an id of 99997", 5))
        self.assertEqual(self.poll(), [["systemctl", "reboot"]])


class DStarRepeaterController(Fixture):
    def setUp(self):
        super().setUp()
        self.processes("dstarrepeaterd")

    def test_every_command(self):
        cases = {
            "SVCKILL": ("Shutting down at keeper request",
                        [["systemctl", "stop", u] for u in UNITS["REMOTE_DSTARREPEATER_STOP_UNITS"].split(",")], [5]),
            "SVCRSTRT": ("Restarting services at keeper request",
                         [["systemctl", "restart", "ircddbgateway.service"],
                          ["systemctl", "restart", "dstarrepeater.service"]], [5]),
            "REBOOTPI": ("Rebooting OS at keeper request", [["systemctl", "reboot"]], [5]),
            "SHUTDOWN": ("Shutting down OS at keeper request", [["systemctl", "poweroff"]], [5]),
            "HOSTFILE": ("Updating Hostfiles at keeper request",
                         [[str(self.root / "sbin" / "HostFilesUpdate.sh")]], [5]),
        }
        for log_name in ("dstarrepeaterd", "DStarRepeater"):
            for ur, (message, expected, sleeps) in cases.items():
                with self.subTest(log=log_name, ur=ur):
                    self.new_remote()
                    self.sleeps.clear()
                    self.append(log_name, dstarrepeater(repeater_header(your=ur)))
                    self.assertEqual(self.poll(), [self.text(message)] + expected)
                    self.assertEqual(self.sleeps, sleeps)

    def test_status_answers_and_strangers(self):
        self.append("dstarrepeaterd", dstarrepeater(repeater_header(your="GETIP"), 30),
                    dstarrepeater(repeater_header(call="G4KLX", your="REBOOTPI"), 20),
                    dstarrepeater(repeater_header(your="REBOOTPX"), 15),
                    dstarrepeater(repeater_header(your="WIFISSID"), 10))
        self.assertEqual(self.poll(), [
            ["/usr/bin/ip", "-o", "addr", "show", "scope", "global"], self.text("My IP: 192.168.1.10"),
            ["/usr/sbin/iw", "dev", "wlan0", "link"], self.text("SSID: HamNet")])

    def test_mmdvmhost_log_is_not_read_without_mmdvmhost(self):
        self.append("MMDVM", mmdvm(dstar_header(your="REBOOTPI")))
        self.assertEqual(self.poll(), [])


class Reading(Fixture):
    def test_each_line_acts_once(self):
        self.append("MMDVM", mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 9999997"))
        self.assertEqual(self.poll(), [["systemctl", "reboot"]])
        self.clock += timedelta(seconds=5)
        self.assertEqual(self.poll(), [["systemctl", "reboot"]])

    def test_repeats_within_the_cooldown_are_ignored(self):
        self.append("MMDVM", mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 9999998", 50),
                    mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 9999998", 40),
                    mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 9999999", 39),
                    mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 9999998", 15))
        restart = [["systemctl", "restart", "mmdvmhost.service"], ["systemctl", "restart", "dmrgateway.service"]]
        self.assertEqual(self.poll(), restart + MMDVM_STOP + restart)
        self.assertIn("dmr svcrestart repeated within 30 s, ignored", self.logged)

    def test_old_lines_and_missing_logs(self):
        self.assertEqual(self.poll(), [])  # no log files at all
        self.append("MMDVM", mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 9999997", 61),
                    "garbage line\n", "\n",
                    mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 9999997", -30))
        self.assertEqual(self.poll(), [])

    def test_partial_line_waits_for_its_end(self):
        line = mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 9999997")
        self.append("MMDVM", line[:40])
        self.assertEqual(self.poll(), [])
        self.append("MMDVM", line[40:])
        self.assertEqual(self.poll(), [["systemctl", "reboot"]])

    def test_new_day_and_truncated_log(self):
        self.append("MMDVM", mmdvm("DMR Slot 2, received RF voice header from G4KLX to TG 9", 30))
        self.poll()
        self.clock = NOW + timedelta(days=1)
        tomorrow = self.clock.strftime("%Y-%m-%d")
        with open(self.root / "log" / f"MMDVM-{tomorrow}.log", "w") as f:
            f.write(f"M: {stamp(self.clock - timedelta(seconds=3))} "
                    "DMR Slot 2, received RF voice header from M1ABC to TG 9999996\n")
        self.assertEqual(self.poll(), [["systemctl", "poweroff"]])
        with open(self.root / "log" / f"MMDVM-{tomorrow}.log", "w") as f:  # truncated, rewritten shorter
            f.write(f"M: {stamp(self.clock - timedelta(seconds=1))} "
                    "DMR Slot 2, received RF voice header from M1ABC to 9999997\n")
        self.assertEqual(self.poll()[-1], ["systemctl", "reboot"])

    def test_nothing_is_read_without_mmdvmhost(self):
        self.processes("YSFGateway", "MMDVMHost_Adafr")  # comm is cut to 15: "MMDVMHost_Adafr"
        self.append("MMDVM", mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 9999997"))
        self.assertEqual(self.poll(), [])
        self.assertFalse(remote.running(str(self.root / "missing"), "MMDVMHost"))

    def test_log_location_comes_from_mmdvmhost_config(self):
        other = self.root / "other"
        other.mkdir()
        (self.root / "etc" / "mmdvmhost").write_text(
            MMDVMHOST_CONF.format(log=other).replace("FileRoot=MMDVM", "FileRoot=Hotspot"))
        self.new_remote()
        with open(other / f"Hotspot-{DAY}.log", "w") as f:
            f.write(mmdvm("DMR Slot 2, received RF voice header from M1ABC to TG 9999997"))
        self.assertEqual(self.poll(), [["systemctl", "reboot"]])


class Settings(Fixture):
    def test_disabled_missing_or_without_keeper(self):
        conf = self.root / "etc" / "gospot-remote"
        self.assertTrue(remote.Settings(self.paths).enabled)
        conf.write_text(REMOTE_CONF.replace("enabled=true", "enabled=false"))
        self.assertFalse(remote.Settings(self.paths).enabled)
        conf.write_text(REMOTE_CONF.replace("callsign=M1ABC", "callsign="))
        self.assertFalse(remote.Settings(self.paths).enabled)
        conf.write_text("not an ini file\n")
        self.assertFalse(remote.Settings(self.paths).enabled)
        conf.unlink()
        self.assertFalse(remote.Settings(self.paths).enabled)

    def test_pi_star_default_config(self):
        """config_clean.zip member pistar-remote (the Pi-Star name), seeded as gospot-remote: disabled, four commands per mode."""
        zip_path = HERE.parent / "Pi-Star_v4_Binaries_Bin" / "config_clean.zip"
        if not zip_path.exists():
            self.skipTest("Pi-Star_v4_Binaries_Bin is not checked out here")
        import zipfile
        (self.root / "etc" / "gospot-remote").write_bytes(zipfile.ZipFile(zip_path).read("pistar-remote"))
        settings = remote.Settings(self.paths)
        self.assertFalse(settings.enabled)
        self.assertEqual(settings.keeper, "M1ABC")
        self.assertEqual(settings.commands["d-star"], {"svckill": "SVCKILL", "svcrestart": "SVCRSTRT",
                                                       "reboot": "REBOOTPI"})
        self.assertEqual(settings.commands["p25"], {"svckill": "65531", "svcrestart": "65532", "reboot": "65533"})

    def paths_file(self, body, mode=0o644):
        f = self.root / "paths"
        f.write_text(body)
        f.chmod(mode)
        return str(f)

    def test_paths_file(self):
        body = "".join(f"{k}={v}\n" for k, v in self.paths.items())
        self.assertEqual(remote.load_paths(self.paths_file("# comment\n\n" + body)), self.paths)
        for bad, mode in ((body, 0o664), (body.replace("GOSPOT_LOG_DIR=/", "GOSPOT_LOG_DIR="), 0o644),
                          (body + "X='y'\n", 0o644), (body.replace("WIFI_INTERFACE=wlan0", "WIFI_INTERFACE=../x"), 0o644),
                          (body.replace("mmdvmhost.timer,", "mmdvmhost.timer;reboot,"), 0o644),
                          ("".join(l for l in body.splitlines(True) if not l.startswith("IW_BIN")), 0o644)):
            with self.subTest(bad=bad, mode=oct(mode)), self.assertRaises(SystemExit):
                remote.load_paths(self.paths_file(bad, mode))
        with self.assertRaises(SystemExit):
            remote.load_paths(str(self.root / "missing"))


class Commands(Fixture):
    def test_run_command(self):
        self.assertEqual(remote.run_command(["true"]), (0, ""))
        self.assertEqual(remote.run_command(["sh", "-c", "echo hi"], capture=True), (0, "hi\n"))
        self.assertEqual(remote.run_command(["false"])[0], 1)
        self.assertIn("false exited with 1", self.logged)
        self.assertEqual(remote.run_command([str(self.root / "missing")]), (127, ""))


class Program(Fixture):
    """The script and its handler as programs, with GOSPOT_RADIO_PATHS."""

    def setUp(self):
        super().setUp()
        fake = self.root / "fakebin"
        fake.mkdir()
        (fake / "systemctl").write_text('#!/bin/sh\nprintf "%s\\n" "$*" >>"$FAKE_SYSTEMCTL_LOG"\n')
        (fake / "systemctl").chmod(0o755)
        (fake / "MMDVMHost").symlink_to("/bin/sleep")
        self.env = dict(os.environ, GOSPOT_RADIO_PATHS=self.paths_file(), PATH=f"{fake}:{os.environ['PATH']}",
                        FAKE_SYSTEMCTL_LOG=str(self.root / "systemctl.log"))
        sbin = self.root / "sbin"
        for src in (SCRIPT, HANDLER):
            (sbin / src.name).write_bytes(src.read_bytes())
            (sbin / src.name).chmod(0o755)

    def paths_file(self):
        f = self.root / "paths"
        f.write_text("".join(f"{k}={v}\n" for k, v in self.paths.items()))
        f.chmod(0o644)
        return str(f)

    def program(self, *args, name="gospot-remote"):
        # Output goes to a file: a daemon the handler starts keeps it open.
        with tempfile.TemporaryFile("w+") as out:
            done = subprocess.run([str(self.root / "sbin" / name), *args], env=self.env, stdout=out,
                                  stderr=subprocess.STDOUT, text=True, timeout=30)
            out.seek(0)
            done.stdout = done.stderr = out.read()
        return done

    def test_enabled_flag(self):
        self.assertEqual(self.program("--enabled").returncode, 0)
        (self.root / "etc" / "gospot-remote").write_text(REMOTE_CONF.replace("enabled=true", "enabled=false"))
        self.assertEqual(self.program("--enabled").returncode, 1)
        done = self.program()
        self.assertEqual(done.returncode, 0)
        self.assertIn("not enabled", done.stdout)

    def test_once_acts_on_a_fresh_line_without_a_shell(self):
        sleeper = subprocess.Popen([str(self.root / "fakebin" / "MMDVMHost"), "30"])
        self.addCleanup(sleeper.wait)
        self.addCleanup(sleeper.kill)
        now = datetime.now(timezone.utc).replace(tzinfo=None)
        with open(self.root / "log" / f"MMDVM-{now:%Y-%m-%d}.log", "w") as f:
            f.write(f"M: {stamp(now)} DMR Slot 2, received RF voice header from M1ABC to TG 9999999\n")
        done = self.program("--once")
        self.assertEqual(done.returncode, 0, done.stderr)
        self.assertEqual((self.root / "systemctl.log").read_text().splitlines(),
                         [" ".join(c[1:]) for c in MMDVM_STOP])

    def test_handler_respects_enabled_and_manages_the_daemon(self):
        (self.root / "etc" / "gospot-remote").write_text(REMOTE_CONF.replace("enabled=true", "enabled=false"))
        done = self.program("start", name="gospot-remote.service")
        self.assertEqual((done.returncode, done.stdout), (0, "gospot-remote is disabled\n"))
        self.assertIn("is not running", self.program("status", name="gospot-remote.service").stdout)
        (self.root / "etc" / "gospot-remote").write_text(REMOTE_CONF)
        self.assertIn("started as PID", self.program("start", name="gospot-remote.service").stdout)
        self.addCleanup(self.program, "stop", name="gospot-remote.service")
        deadline = time.monotonic() + 10
        while "is running" not in self.program("status", name="gospot-remote.service").stdout:
            self.assertLess(time.monotonic(), deadline, "gospot-remote did not start")
            time.sleep(0.1)
        self.assertIn("already running", self.program("start", name="gospot-remote.service").stdout)
        self.assertIn("Killing", self.program("stop", name="gospot-remote.service").stdout)
        self.assertIn("is not running", self.program("status", name="gospot-remote.service").stdout)
        self.assertIn("Usage", self.program("bogus", name="gospot-remote.service").stdout)


if __name__ == "__main__":
    unittest.main()
