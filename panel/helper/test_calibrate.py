"""pistar-calibrate and pistar-jitter, run as programs against fake tools.

The helpers take every tool location from pistar.conf; the tests write their
own settings file (PISTAR_SETTINGS is honoured for unprivileged runs only) and
put a recording systemctl first in PATH."""
import os
from pathlib import Path
import signal
import subprocess
import tempfile
import threading
import time
import unittest

HERE = Path(__file__).resolve().parent
FIXTURES = HERE.parents[1] / "Pi-Star_dash_new" / "qa" / "fixtures"
CALIBRATE = HERE / "pistar-calibrate"
JITTER = HERE / "pistar-jitter"

SYSTEMCTL = """#!/bin/bash
printf '%s\\n' "$*" >>"$FAKE_SYSTEMCTL_LOG"
if [[ $1 == is-active ]]; then grep -qxF -- "$3" "$FAKE_SYSTEMCTL_ACTIVE"; exit $(( $? ? 3 : 0 )); fi
exit 0
"""

MMDVMHOST = """[General]
Callsign=DEMO1
[Modem]
Protocol=uart
UARTPort=/dev/ttyAMA0
# UARTPort=/dev/ttyACM0
UARTSpeed=460800
RXOffset=100
[Info]
RXFrequency=435000000
"""


def wait_for(what, check, timeout=10):
    end = time.monotonic() + timeout
    while time.monotonic() < end:
        value = check()
        if value:
            return value
        time.sleep(0.02)
    raise AssertionError("timed out waiting for " + what)


class Fixture(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = root = Path(tmp.name)
        for d in ("bin", "sbin", "fakebin", "log"):
            (root / d).mkdir()
        (root / "bin" / "MMDVMCal").symlink_to(FIXTURES / "fake-mmdvmcal")
        (root / "sbin" / "pistar-jittertest").symlink_to(FIXTURES / "fake-jittertest")
        (root / "fakebin" / "systemctl").write_text(SYSTEMCTL)
        (root / "fakebin" / "systemctl").chmod(0o755)
        (root / "active").write_text("pistar-watchdog.service\nmmdvmhost.service\n")
        (root / "mmdvmhost").write_text(MMDVMHOST)
        self.settings = {
            "CONF_MMDVMHOST": root / "mmdvmhost", "PISTAR_BIN_DIR": root / "bin", "PISTAR_SBIN_DIR": root / "sbin",
            "DASHBOARD_STATE_DIR": root / "state", "UNIT_MMDVMHOST": "mmdvmhost.service",
            "CALIBRATION_HOLD_UNITS": "pistar-watchdog.timer,pistar-watchdog.service,mmdvmhost.timer",
            "CALIBRATION_WARMUP": "0", "CALIBRATION_MAX_RUNTIME": "60",
            "PISTAR_JITTER_LOG": root / "log" / "pi-star_icmptest.log", "JITTER_TIMEOUT": "20",
        }
        self.write_settings()
        self.env = dict(os.environ, PATH=f"{root / 'fakebin'}:/usr/bin:/bin", PISTAR_SETTINGS=str(root / "pistar.conf"),
                        FAKE_SYSTEMCTL_LOG=str(root / "systemctl.log"), FAKE_SYSTEMCTL_ACTIVE=str(root / "active"),
                        FAKE_MMDVMCAL_LOG=str(root / "input"))
        self.env.pop("FAKE_JITTER_SLEEP", None)

    def write_settings(self, mode=0o600, **changes):
        self.settings.update(changes)
        conf = self.root / "pistar.conf"
        conf.write_text("# test settings\n" + "".join(f"{k}={v}\n" for k, v in self.settings.items()))
        conf.chmod(mode)

    def systemctl(self):
        p = self.root / "systemctl.log"
        return p.read_text().splitlines() if p.exists() else []

    def read(self, name):
        p = self.root / name
        return p.read_text() if p.exists() else ""


class Session:
    """pistar-calibrate run with its stdin and stdout attached to the test."""

    def __init__(self, test):
        self.proc = subprocess.Popen([str(CALIBRATE), "run"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                     stderr=subprocess.PIPE, env=test.env)
        test.addCleanup(self.kill)
        self.out = b""
        self.reader = threading.Thread(target=self._read, daemon=True)
        self.reader.start()

    def _read(self):
        while chunk := self.proc.stdout.read1(4096):
            self.out += chunk

    def text(self):
        return self.out.decode(errors="replace")

    def send(self, data):
        self.proc.stdin.write(data)
        self.proc.stdin.flush()

    def wait(self, timeout=20):
        code = self.proc.wait(timeout)
        self.reader.join(5)
        return code

    def kill(self):
        if self.proc.poll() is None:
            self.proc.kill()
            self.proc.wait()
        for f in (self.proc.stdin, self.proc.stdout, self.proc.stderr):
            try:
                f.close()
            except OSError:
                pass


HELD = ["is-active --quiet pistar-watchdog.timer", "is-active --quiet pistar-watchdog.service",
        "is-active --quiet mmdvmhost.timer", "stop pistar-watchdog.service", "stop mmdvmhost.service"]
RESTORED = ["start pistar-watchdog.service", "restart mmdvmhost.service"]


class CalibrateTest(Fixture):
    def test_session_relays_allowlisted_keys_and_restores_on_eof(self):
        s = Session(self)
        wait_for("MMDVMCal version", lambda: "Version: 1" in s.text())
        self.assertEqual(self.systemctl(), HELD, "held units before MMDVMCal starts")
        self.assertEqual(self.read("input.args"), "460800 /dev/ttyAMA0\n")
        self.assertTrue((self.root / "state" / "calibration-held.json").exists())
        s.send(b"k")
        wait_for("D-Star mode", lambda: "BER Test Mode (FEC) for D-Star" in s.text())
        # Anything outside the allowlist never reaches MMDVMCal.
        s.send(b"x;rm -r /\x03\x1bH")
        s.send(b"e")
        s.send(b"435000150\n")
        wait_for("new frequency", lambda: "New frequency: 435000150 Hz" in s.text())
        self.assertEqual(self.read("input"), "ke435000150\n")
        # The dashboard going away closes stdin: quit MMDVMCal and restore.
        s.proc.stdin.close()
        self.assertEqual(s.wait(), 0)
        self.assertEqual(self.read("input"), "ke435000150\nq")
        self.assertEqual(self.systemctl(), HELD + RESTORED)
        self.assertIn("Starting Pi-Star Services...\r\nFinnished...\r\n", s.text())
        self.assertFalse((self.root / "state" / "calibration-held.json").exists())

    def test_quit_key_ends_the_session(self):
        s = Session(self)
        wait_for("MMDVMCal version", lambda: "Version: 1" in s.text())
        s.send(b"q")
        self.assertEqual(s.wait(), 0)
        self.assertEqual(self.read("input"), "q")
        self.assertEqual(self.systemctl(), HELD + RESTORED)

    def test_sigterm_restores(self):
        s = Session(self)
        wait_for("MMDVMCal version", lambda: "Version: 1" in s.text())
        s.proc.send_signal(signal.SIGTERM)
        self.assertEqual(s.wait(), 0)
        self.assertEqual(self.systemctl(), HELD + RESTORED)
        self.assertTrue(s.text().endswith("Finnished...\r\n"))

    def test_runtime_limit_restores(self):
        self.write_settings(CALIBRATION_MAX_RUNTIME="1")
        s = Session(self)
        self.assertEqual(s.wait(), 0)
        self.assertEqual(self.read("input"), "q")
        self.assertEqual(self.systemctl(), HELD + RESTORED)

    def test_warmup_runs_mmdvmcal_twice(self):
        self.write_settings(CALIBRATION_WARMUP="1")
        s = Session(self)
        wait_for("second MMDVMCal", lambda: self.read("input.args").count("\n") == 2)
        self.assertEqual(self.read("input"), "q", "the first run is told to quit")
        s.proc.stdin.close()
        self.assertEqual(s.wait(), 0)
        self.assertEqual(self.systemctl(), HELD + RESTORED)

    def test_one_session_at_a_time(self):
        s = Session(self)
        wait_for("MMDVMCal version", lambda: "Version: 1" in s.text())
        before = self.systemctl()
        second = subprocess.run([str(CALIBRATE), "run"], env=self.env, stdin=subprocess.DEVNULL,
                                capture_output=True, timeout=10)
        self.assertEqual(second.returncode, 3)
        self.assertIn(b"Another instance is already running", second.stdout)
        # restore while a session runs leaves it alone
        r = subprocess.run([str(CALIBRATE), "restore"], env=self.env, capture_output=True, timeout=10)
        self.assertEqual((r.returncode, r.stdout), (0, b""))
        self.assertEqual(self.systemctl(), before)
        s.proc.stdin.close()
        self.assertEqual(s.wait(), 0)

    def test_restore_after_a_dead_session(self):
        r = subprocess.run([str(CALIBRATE), "restore"], env=self.env, capture_output=True, timeout=10)
        self.assertEqual((r.returncode, r.stdout, self.systemctl()), (0, b"nothing to restore\n", []))
        s = Session(self)
        wait_for("MMDVMCal version", lambda: "Version: 1" in s.text())
        s.kill()  # SIGKILL: no chance to restore
        subprocess.run(["pkill", "-KILL", "-f", str(self.root / "bin" / "MMDVMCal")], check=False)
        self.assertEqual(self.systemctl(), HELD)
        r = subprocess.run([str(CALIBRATE), "restore"], env=self.env, capture_output=True, timeout=10)
        self.assertEqual((r.returncode, r.stdout), (0, b"restored\n"))
        self.assertEqual(self.systemctl(), HELD + RESTORED)
        self.assertFalse((self.root / "state" / "calibration-held.json").exists())

    def test_invalid_modem_port_stops_nothing(self):
        (self.root / "mmdvmhost").write_text(MMDVMHOST.replace("UARTPort=/dev/ttyAMA0", "UARTPort=/dev/tty;reboot"))
        r = subprocess.run([str(CALIBRATE), "run"], env=self.env, stdin=subprocess.DEVNULL, capture_output=True, timeout=10)
        self.assertEqual(r.returncode, 1)
        self.assertIn(b"no valid [Modem] UARTPort", r.stdout)
        self.assertEqual(self.systemctl(), [])
        self.assertEqual(self.read("input.args"), "")

    def test_untrusted_settings_are_refused(self):
        self.write_settings(mode=0o620)
        for verb in ("run", "restore"):
            r = subprocess.run([str(CALIBRATE), verb], env=self.env, stdin=subprocess.DEVNULL, capture_output=True, timeout=10)
            self.assertNotEqual(r.returncode, 0)
            self.assertIn(b"unsafe settings file", r.stderr)
        self.assertEqual(self.systemctl(), [])

    def test_unknown_verb(self):
        r = subprocess.run([str(CALIBRATE), "start"], env=self.env, capture_output=True, timeout=10)
        self.assertEqual(r.returncode, 2)
        self.assertEqual(self.systemctl(), [])


class JitterTest(Fixture):
    def jitter(self, *args):
        return subprocess.run([str(JITTER), *args], env=self.env, stdin=subprocess.DEVNULL, capture_output=True, timeout=30)

    def test_run_writes_the_log(self):
        log = Path(self.settings["PISTAR_JITTER_LOG"])
        log.write_text("previous run\n")
        r = self.jitter("run", "DMR+")
        self.assertEqual((r.returncode, r.stdout), (0, b"started\n"), r.stderr)
        self.assertEqual(log.read_text().splitlines()[0], "DMR+_Host_1\t\t\tAvg RTT: 21.00ms\tPeak Deviation: 0.10ms")
        self.assertEqual(len(log.read_text().splitlines()), 3)
        self.assertEqual(log.stat().st_mode & 0o777, 0o644)

    def test_groups_are_an_allowlist(self):
        for args in (("run", "BM;reboot"), ("run",), ("run", "BM", "x"), ("start", "BM"), ("run", "bm")):
            r = self.jitter(*args)
            self.assertEqual(r.returncode, 2, args)
        self.assertFalse(Path(self.settings["PISTAR_JITTER_LOG"]).exists())

    def test_timeout(self):
        self.write_settings(JITTER_TIMEOUT="1")
        self.env["FAKE_JITTER_SLEEP"] = "30"
        start = time.monotonic()
        r = self.jitter("run", "HB")
        self.assertLess(time.monotonic() - start, 10)
        self.assertEqual(r.returncode, 124)

    def test_log_symlink_is_replaced_not_followed(self):
        victim = self.root / "victim"
        victim.write_text("keep\n")
        log = Path(self.settings["PISTAR_JITTER_LOG"])
        log.symlink_to(victim)
        r = self.jitter("run", "BM")
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(victim.read_text(), "keep\n")
        self.assertFalse(log.is_symlink())
        self.assertIn("BM_Host_3", log.read_text())

    def test_untrusted_settings_are_refused(self):
        self.write_settings(mode=0o602)
        r = self.jitter("run", "BM")
        self.assertEqual(r.returncode, 1)
        self.assertIn(b"unsafe settings file", r.stderr)
        self.assertFalse(Path(self.settings["PISTAR_JITTER_LOG"]).exists())


if __name__ == "__main__":
    unittest.main()
