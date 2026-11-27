"""radio/sbin/gospot-watchdog: service checks, DStarRepeater failures and log
housekeeping.

The script is loaded as a module and driven with temporary markers, logs, a
fake /proc, a command recorder, a fixed clock and a fake disk usage; the
program and its handler also run for real with GOSPOT_RADIO_PATHS and a
recording systemctl on PATH. Log lines copy the Pi-Star PS4 formats."""
from datetime import datetime, timedelta
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
SCRIPT = HERE / "sbin" / "gospot-watchdog"
HANDLER = HERE / "sbin" / "gospot-watchdog.service"


def load(path, name):
    loader = importlib.machinery.SourceFileLoader(name, str(path))
    spec = importlib.util.spec_from_loader(name, loader)
    module = importlib.util.module_from_spec(spec)
    loader.exec_module(module)
    return module


watchdog = load(SCRIPT, "gospot_watchdog")
sys.dont_write_bytecode = True

NOW = datetime(2026, 11, 27, 12, 0, 0)
DAY = NOW.strftime("%Y-%m-%d")
MMDVM_PROCESSES = ("MMDVMHost:mmdvmhost.service,DMRGateway:dmrgateway.service,YSFGateway:ysfgateway.service,"
                   "DGIdGateway:dgidgateway.service,P25Gateway:p25gateway.service,M17Gateway:m17gateway.service,"
                   "NXDNGateway:nxdngateway.service,YSFParrot:ysfparrot.service,P25Parrot:p25parrot.service,"
                   "NXDNParrot:nxdnparrot.service,YSF2DMR:ysf2dmr.service,YSF2NXDN:ysf2nxdn.service,"
                   "YSF2P25:ysf2p25.service,DMR2YSF:dmr2ysf.service,DMR2NXDN:dmr2nxdn.service,"
                   "DAPNETGateway:dapnetgateway.service")
MMDVM_NAMES = [p.split(":")[0] for p in MMDVM_PROCESSES.split(",")]
REMOTE_ON = "[enable]\nenabled=true\n[keeper]\ncallsign=M1ABC\n"


def restart(unit, before=2, after=10):
    return [["systemctl", "stop", unit], ["systemctl", "start", unit]], [before, after]


def dstarrepeater(text, ago=10):
    return f"M: {(NOW - timedelta(seconds=ago)):%Y-%m-%d %H:%M:%S}: {text}\n"


class Recorder:
    def __init__(self):
        self.calls, self.status = [], "gospot-remote is running as PID 42\n"

    def __call__(self, argv, capture=False):
        self.calls.append(list(argv))
        if argv[-1] == "status":
            return (0, self.status) if self.status is not None else (127, "")
        return 0, ""


class Fixture(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.root = root = Path(tmp.name)
        for d in ("etc", "sbin", "proc", "var/log/gospot"):
            (root / d).mkdir(parents=True)
        self.logs = root / "var" / "log" / "gospot"
        (root / "etc" / "mmdvmhost").write_text(f"[Log]\nFilePath={self.logs}\nFileRoot=MMDVM\n")
        (root / "etc" / "gospot-remote").write_text("[enable]\nenabled=false\n")
        self.paths = {
            "GOSPOT_SBIN_DIR": str(root / "sbin"), "GOSPOT_LOG_DIR": str(self.logs),
            "SYSTEM_LOG_DIR": str(root / "var" / "log"),
            "MARKER_MMDVMHOST": str(root / "etc" / "dstar-radio.mmdvmhost"),
            "MARKER_DSTARREPEATER": str(root / "etc" / "dstar-radio.dstarrepeater"),
            "CONF_MMDVMHOST": str(root / "etc" / "mmdvmhost"), "CONF_GOSPOT_REMOTE": str(root / "etc" / "gospot-remote"),
            "UNIT_MMDVMHOST": "mmdvmhost.service", "UNIT_DSTARREPEATER": "dstarrepeater.service",
            "UNIT_GOSPOT_REMOTE": "gospot-remote.service",
            "WATCHDOG_DSTARREPEATER_PROCESSES": "dstarrepeaterd:dstarrepeater.service",
            "WATCHDOG_MMDVMHOST_PROCESSES": MMDVM_PROCESSES,
            "WATCHDOG_COMMON_PROCESSES": "ircddbgatewayd:ircddbgateway.service,timeserverd:timeserver.service",
        }
        self.processes("ircddbgatewayd", "timeserverd")
        self.usage, self.clock, self.sleeps, self.logged = 50, NOW, [], []
        patcher = mock.patch.object(watchdog, "log", self.logged.append)
        patcher.start()
        self.addCleanup(patcher.stop)
        self.rec = Recorder()
        self.dog = watchdog.Watchdog(self.paths, run=self.rec, sleep=self.sleeps.append, clock=lambda: self.clock,
                                     proc_dir=str(root / "proc"), disk=lambda path: self.usage)

    def processes(self, *names):
        proc = self.root / "proc"
        for d in proc.iterdir():
            (d / "comm").unlink()
            d.rmdir()
        for pid, name in enumerate(names, 100):
            (proc / str(pid)).mkdir()
            (proc / str(pid) / "comm").write_text(name[:15] + "\n")

    def marker(self, name):
        (self.root / "etc" / f"dstar-radio.{name}").write_text("")

    def check(self):
        self.rec.calls.clear()
        self.sleeps.clear()
        self.dog.check()
        return self.rec.calls


class Services(Fixture):
    def test_running_services_are_left_alone(self):
        self.assertEqual(self.check(), [])
        self.marker("mmdvmhost")
        self.processes("ircddbgatewayd", "timeserverd", *MMDVM_NAMES)
        self.assertEqual(self.check(), [])

    def test_without_markers_only_the_common_services(self):
        self.processes()
        calls, sleeps = restart("ircddbgateway.service")
        calls2, sleeps2 = restart("timeserver.service")
        self.assertEqual(self.check(), calls + calls2)
        self.assertEqual(self.sleeps, sleeps + sleeps2)

    def test_mmdvmhost_services_in_pi_star_order(self):
        self.marker("mmdvmhost")
        self.processes("ircddbgatewayd", "timeserverd", "MMDVMHost", "DMRGateway")
        expected = []
        for name in MMDVM_NAMES[2:]:
            expected += restart(name.lower() + ".service")[0]
        self.assertEqual(self.check(), expected)
        self.assertEqual(self.sleeps, [2, 10] * len(MMDVM_NAMES[2:]))

    def test_dstarrepeater_controller(self):
        self.marker("dstarrepeater")
        self.assertEqual(self.check(), restart("dstarrepeater.service")[0])
        self.processes("ircddbgatewayd", "timeserverd", "dstarrepeaterd")
        self.assertEqual(self.check(), [])

    def test_similar_process_names_do_not_count(self):
        self.processes("ircddbgateway", "timeserverd", "timeserverd2")
        self.assertEqual(self.check(), restart("ircddbgateway.service")[0])
        self.assertFalse(watchdog.running(str(self.root / "missing"), "timeserverd"))


class Remote(Fixture):
    def test_enabled_remote_that_is_not_running_is_restarted(self):
        (self.root / "etc" / "gospot-remote").write_text(REMOTE_ON)
        self.rec.status = "gospot-remote is not running\n"
        handler = [str(self.root / "sbin" / "gospot-remote.service"), "status"]
        self.assertEqual(self.check(), [handler] + restart("gospot-remote.service")[0])
        self.assertEqual(self.sleeps, [2, 10])

    def test_running_disabled_or_missing(self):
        (self.root / "etc" / "gospot-remote").write_text(REMOTE_ON)
        handler = [str(self.root / "sbin" / "gospot-remote.service"), "status"]
        self.assertEqual(self.check(), [handler])
        self.rec.status = None  # handler missing
        self.assertEqual(self.check(), [handler])
        (self.root / "etc" / "gospot-remote").write_text(REMOTE_ON.replace("true", "false"))
        self.assertEqual(self.check(), [])
        (self.root / "etc" / "gospot-remote").write_text("enabled=true\n")  # no section: not an INI file
        self.assertEqual(self.check(), [])
        self.assertTrue(any(m.startswith("cannot parse") for m in self.logged))
        (self.root / "etc" / "gospot-remote").unlink()
        self.assertEqual(self.check(), [])


class DStarRepeaterLog(Fixture):
    def setUp(self):
        super().setUp()
        self.marker("dstarrepeater")
        self.processes("ircddbgatewayd", "timeserverd", "dstarrepeaterd")

    def write(self, *lines, name="dstarrepeaterd"):
        with open(self.logs / f"{name}-{DAY}.log", "a") as f:
            f.write("".join(lines))

    def test_modem_failures_restart_with_a_pause(self):
        for failure in ("Bad DVAP header", "Cannot open the D-Star modem"):
            with self.subTest(failure=failure):
                self.write(dstarrepeater("Radio header decoded - My: M1ABC   /M     Your: CQCQCQ    Rpt1: x", 20),
                           dstarrepeater(failure, 15), dstarrepeater(failure, 5))
                self.assertEqual(self.check(), restart("dstarrepeater.service")[0])
                self.assertEqual(self.sleeps, [30, 270])  # one restart for both lines
                self.assertEqual(self.check(), [])         # and the lines count once

    def test_no_space_trims_the_log_and_restarts(self):
        old = [dstarrepeater(f"line {i}", 4000) for i in range(80)]
        self.write(*old, dstarrepeater("No space to write data", 30))
        self.assertEqual(self.check(), [["systemctl", "restart", "dstarrepeater.service"]])
        self.assertEqual(self.sleeps, [60])
        kept = (self.logs / f"dstarrepeaterd-{DAY}.log").read_text().splitlines(True)
        self.assertEqual(kept, old[-49:] + [dstarrepeater("No space to write data", 30)])
        self.assertEqual(self.check(), [])  # the kept lines are not looked at again
        self.write(dstarrepeater("No space to write the header", 1))
        self.assertEqual(self.check(), [["systemctl", "restart", "dstarrepeater.service"]])

    def test_old_lines_older_log_name_and_no_log(self):
        self.assertEqual(self.check(), [])  # no log yet
        self.write(dstarrepeater("Bad DVAP header", 301), dstarrepeater("Bad DVAP header", -30),
                   "Bad DVAP header without a time\n", name="DStarRepeater")
        self.assertEqual(self.check(), [])
        self.write(dstarrepeater("Bad DVAP header", 2), name="DStarRepeater")
        self.assertEqual(self.check(), restart("dstarrepeater.service")[0])

    def test_newer_log_name_wins(self):
        self.write(dstarrepeater("Bad DVAP header", 5), name="DStarRepeater")
        self.write(dstarrepeater("all good", 5))
        self.assertEqual(self.check(), [])

    def test_not_checked_without_the_marker(self):
        (self.root / "etc" / "dstar-radio.dstarrepeater").unlink()
        self.write(dstarrepeater("Bad DVAP header", 5))
        self.assertEqual(self.check(), [])


class Housekeeping(Fixture):
    def files(self):
        var = self.root / "var" / "log"
        day_old = time.time() - 2 * 86400
        made = {}
        for rel, old in (("syslog.1", False), ("messages.1.gz", False), ("syslog", False), ("daemon.log.2.gz", False),
                         ("apt/history.log.3.gz", True), ("apt/eipp.log.xz", True), ("nginx/error.log", True),
                         ("journal/x/system.journal", True), ("gospot/MMDVM-2026-11-25.log", True),
                         ("gospot/DMRGateway-2026-11-27.log", False), ("gospot/notes.txt", True),
                         ("nested/old.gz", True)):
            path = var / rel
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("x\n")
            if old:
                os.utime(path, (day_old, day_old))
            made[rel] = path
        (var / "link.1").symlink_to(made["syslog"])
        return made

    def test_full_volume_is_cleaned_like_pi_star(self):
        made = self.files()
        self.marker("mmdvmhost")
        self.processes("ircddbgatewayd", "timeserverd", *MMDVM_NAMES)
        lines = [f"M: {DAY} 11:00:{i % 60:02d}.000 line {i}\n" for i in range(700)]
        (self.logs / f"MMDVM-{DAY}.log").write_text("".join(lines))
        self.usage = 91
        self.assertEqual(self.check(), [["systemctl", "restart", "mmdvmhost.service"]])
        self.assertEqual(self.sleeps, [60])
        gone = {"syslog.1", "messages.1.gz", "apt/history.log.3.gz", "gospot/MMDVM-2026-11-25.log", "nested/old.gz"}
        for rel, path in made.items():
            self.assertEqual(path.exists(), rel not in gone, rel)
        self.assertTrue((self.root / "var" / "log" / "link.1").is_symlink())
        self.assertEqual((self.logs / f"MMDVM-{DAY}.log").read_text(), "".join(lines[-500:]))

    def test_dstarrepeater_logs_are_trimmed_and_restarted(self):
        self.marker("dstarrepeater")
        self.processes("ircddbgatewayd", "timeserverd", "dstarrepeaterd")
        for name in ("dstarrepeaterd", "DStarRepeater"):
            (self.logs / f"{name}-{DAY}.log").write_text("".join(f"l{i}\n" for i in range(600)))
        self.usage = 95
        self.assertEqual(self.check(), [["systemctl", "restart", "dstarrepeater.service"]])
        self.assertEqual(self.sleeps, [60])
        for name in ("dstarrepeaterd", "DStarRepeater"):
            self.assertEqual(len((self.logs / f"{name}-{DAY}.log").read_text().splitlines()), 500)

    def test_nothing_happens_up_to_90_percent(self):
        made = self.files()
        self.usage = 90
        self.assertEqual(self.check(), [])
        self.assertTrue(all(p.exists() for p in made.values()))

    def test_missing_log_dir(self):
        for f in self.logs.iterdir():
            f.unlink()
        self.logs.rmdir()
        self.usage = 99
        self.assertEqual(self.check(), [])

    def test_disk_percent_and_trim_helpers(self):
        st = mock.Mock(f_blocks=1000, f_bfree=200, f_bavail=150)
        with mock.patch.object(watchdog.os, "statvfs", return_value=st):
            self.assertEqual(watchdog.disk_percent("/"), 800 * 100 // 950 + 1)
        st.f_blocks = st.f_bfree = st.f_bavail = 0
        with mock.patch.object(watchdog.os, "statvfs", return_value=st):
            self.assertEqual(watchdog.disk_percent("/"), 0)
        self.assertGreater(watchdog.disk_percent(str(self.root)), 0)
        f = self.root / "t.log"
        f.write_text("a\nb\nc")
        self.assertEqual(watchdog.keep_last_lines(str(f), 2), 4)
        self.assertEqual(f.read_text(), "b\nc\n")
        self.assertIsNone(watchdog.keep_last_lines(str(self.root / "missing"), 2))


class Paths(Fixture):
    def paths_file(self, body, mode=0o644):
        f = self.root / "paths"
        f.write_text(body)
        f.chmod(mode)
        return str(f)

    def test_paths_file(self):
        body = "".join(f"{k}={v}\n" for k, v in self.paths.items())
        self.assertEqual(watchdog.load_paths(self.paths_file("# c\n" + body)), self.paths)
        for bad, mode in ((body, 0o646),
                          (body.replace("SYSTEM_LOG_DIR=/", "SYSTEM_LOG_DIR=./"), 0o644),
                          (body.replace("UNIT_GOSPOT_REMOTE=gospot-remote.service", "UNIT_GOSPOT_REMOTE=x"), 0o644),
                          (body.replace("timeserverd:timeserver.service", "timeserverd"), 0o644),
                          (body.replace("dstarrepeaterd:", "dstar repeaterd:"), 0o644),
                          (body + "lower=x\n", 0o644)):
            with self.subTest(bad=bad, mode=oct(mode)), self.assertRaises(SystemExit):
                watchdog.load_paths(self.paths_file(bad, mode))
        link = self.root / "paths-link"
        link.symlink_to(self.paths_file(body))
        with self.assertRaises(SystemExit):
            watchdog.load_paths(str(link))


class Commands(Fixture):
    def test_run_command(self):
        self.assertEqual(watchdog.run_command(["true"]), (0, ""))
        self.assertEqual(watchdog.run_command(["sh", "-c", "echo hi"], capture=True), (0, "hi\n"))
        self.assertEqual(watchdog.run_command(["false"])[0], 1)
        self.assertIn("false exited with 1", self.logged)
        self.assertEqual(watchdog.run_command([str(self.root / "missing")]), (127, ""))


class Program(Fixture):
    """The script and its handler as programs, with GOSPOT_RADIO_PATHS."""

    def setUp(self):
        super().setUp()
        fake = self.root / "fakebin"
        fake.mkdir()
        (fake / "systemctl").write_text('#!/bin/sh\nprintf "%s\\n" "$*" >>"$FAKE_SYSTEMCTL_LOG"\n')
        (fake / "systemctl").chmod(0o755)
        for name in ("ircddbgatewayd", "timeserverd"):
            (fake / name).symlink_to("/bin/sleep")
            sleeper = subprocess.Popen([str(fake / name), "60"])
            self.addCleanup(sleeper.wait)
            self.addCleanup(sleeper.kill)
        paths = self.root / "paths"
        paths.write_text("".join(f"{k}={v}\n" for k, v in self.paths.items()))
        paths.chmod(0o644)
        self.env = dict(os.environ, GOSPOT_RADIO_PATHS=str(paths), PATH=f"{fake}:{os.environ['PATH']}",
                        FAKE_SYSTEMCTL_LOG=str(self.root / "systemctl.log"))
        for src in (SCRIPT, HANDLER):
            (self.root / "sbin" / src.name).write_bytes(src.read_bytes())
            (self.root / "sbin" / src.name).chmod(0o755)

    def program(self, *args, name="gospot-watchdog"):
        # Output goes to a file: a daemon the handler starts keeps it open.
        with tempfile.TemporaryFile("w+") as out:
            done = subprocess.run([str(self.root / "sbin" / name), *args], env=self.env, stdout=out,
                                  stderr=subprocess.STDOUT, text=True, timeout=60)
            out.seek(0)
            done.stdout = out.read()
        return done

    def test_once_with_everything_running(self):
        (self.root / "etc" / "gospot-remote").write_text(REMOTE_ON)
        remote_handler = self.root / "sbin" / "gospot-remote.service"
        remote_handler.write_text("#!/bin/sh\necho gospot-remote is running as PID 1\n")
        remote_handler.chmod(0o755)
        done = self.program("--once")
        self.assertEqual(done.returncode, 0, done.stdout)
        self.assertFalse((self.root / "systemctl.log").exists(), done.stdout)

    def test_bad_paths_file_stops_the_program(self):
        self.env["GOSPOT_RADIO_PATHS"] = str(self.root / "missing")
        done = self.program("--once")
        self.assertEqual(done.returncode, 1)
        self.assertIn("cannot read", done.stdout)

    def test_handler_manages_the_daemon(self):
        self.assertIn("is not running", self.program("status", name="gospot-watchdog.service").stdout)
        self.assertIn("is not running", self.program("stop", name="gospot-watchdog.service").stdout)
        self.assertIn("started as PID", self.program("start", name="gospot-watchdog.service").stdout)
        self.addCleanup(self.program, "stop", name="gospot-watchdog.service")
        deadline = time.monotonic() + 10
        while "is running" not in self.program("status", name="gospot-watchdog.service").stdout:
            self.assertLess(time.monotonic(), deadline, "gospot-watchdog did not start")
            time.sleep(0.1)
        first = self.program("status", name="gospot-watchdog.service").stdout.split()[-1]
        self.assertIn("already running", self.program("start", name="gospot-watchdog.service").stdout)
        self.assertIn("Killing", self.program("restart", name="gospot-watchdog.service").stdout)
        deadline = time.monotonic() + 10
        while True:
            status = self.program("status", name="gospot-watchdog.service").stdout
            if "is running" in status and status.split()[-1] != first:
                break
            self.assertLess(time.monotonic(), deadline, "gospot-watchdog did not restart")
            time.sleep(0.1)
        self.assertIn("Killing", self.program("stop", name="gospot-watchdog.service").stdout)
        self.assertIn("is not running", self.program("status", name="gospot-watchdog.service").stdout)
        self.assertFalse((self.root / "systemctl.log").exists())  # still settling for 90 s, no checks yet


if __name__ == "__main__":
    unittest.main()
