import fcntl
import importlib.machinery
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest
from unittest.mock import patch
import warnings
import zipfile


loader = importlib.machinery.SourceFileLoader("archive_helper", str(Path(__file__).with_name("pistar-archive")))
archive = importlib.util.module_from_spec(importlib.util.spec_from_loader(loader.name, loader))
loader.exec_module(archive)
HOST = b"[General]\nCallsign=N0CALL\nId=1\n[Modem]\nUARTPort=/dev/ttyAMA0\nRXFrequency=435000000\nTXFrequency=435000000\n"
GW = b"[General]\nId=1\n[DMR Network 1]\nId=1\nAddress=master.invalid\nPort=62031\nPassword=fixture-secret\n"


class LocalArchiveTest(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        root = Path(temp.name)
        self.etc = root / "etc"
        self.etc.mkdir()
        self.state = root / "state"
        config = patch.multiple(archive, ETC=self.etc, STATE=self.state)
        config.start()
        self.addCleanup(config.stop)
        self.host, self.gw = [self.etc / n for n in archive.NAMES]
        self.host.write_bytes(HOST)
        self.gw.write_bytes(GW)
        self.host.chmod(0o640)
        self.gw.chmod(0o600)
        self.calls = []
        self.backup = self.call("backup")

    def call(self, verb, data=b""):
        return archive.execute(verb, data, self.calls.append)

    def approved(self, blob=None):
        if blob is None:
            blob = self.backup
        return json.loads(self.call("preview", blob))["receipt"].encode() + b"\n" + blob

    def alter(self):
        self.host.write_bytes(HOST.replace(b"N0CALL", b"LOCAL1"))
        self.gw.write_bytes(GW.replace(b"fixture-secret", b"updated-secret"))

    def test_private_export_and_verified_round_trip(self):
        with zipfile.ZipFile(io.BytesIO(self.backup)) as z:
            self.assertEqual(z.namelist(), list(archive.NAMES))
            self.assertEqual(z.read(archive.NAMES[1]), GW)
            self.assertIn(b"PRIVATE", z.comment)
        self.assertEqual((self.state / "archive.key").stat().st_mode & 0o777, 0o600)
        self.alter()
        previous = [p.read_text() for p in (self.host, self.gw)]
        meta = [(s.st_uid, s.st_gid, s.st_mode) for s in (self.host.stat(), self.gw.stat())]
        preview = self.call("preview", self.backup)
        self.assertNotIn(b"secret", preview)
        self.assertEqual(json.loads(preview)["changed"], list(archive.NAMES))
        self.assertEqual(previous, [p.read_text() for p in (self.host, self.gw)])
        self.assertFalse((self.state / "pending.json").exists())
        payload = self.approved()
        self.call("apply", payload)
        self.assertEqual(self.calls, list(reversed(archive.UNITS)))
        self.assertEqual([self.host.read_bytes(), self.gw.read_bytes()], [HOST, GW])
        self.assertEqual(meta, [(s.st_uid, s.st_gid, s.st_mode) for s in (self.host.stat(), self.gw.stat())])
        snapshot = self.state / "previous.json"
        self.assertEqual(json.loads(snapshot.read_bytes()), previous)
        self.assertEqual(snapshot.stat().st_mode & 0o777, 0o600)
        self.calls.clear()
        self.call("apply", self.approved())
        self.assertFalse(self.calls)
        self.assertEqual(json.loads(snapshot.read_bytes()), previous)

    def test_rejects_tamper_foreign_and_stale_without_writes(self):
        self.alter()
        approved = self.approved()
        self.host.write_bytes(self.host.read_bytes() + b"# later\n")
        with self.assertRaises(archive.Invalid):
            self.call("apply", approved)
        fresh = self.approved()
        other_blob = self.call("backup")
        with self.assertRaises(archive.Invalid):
            self.call("apply", fresh[:65] + other_blob)
        for blob in (self.backup.replace(b"fixture-secret", b"altered-secret"), self.backup[:-1],
                     self.backup + b"!", b"", b"\0" * (archive.ZIP_MAX + 1)):
            with self.assertRaises(archive.Invalid):
                self.call("preview", blob)
        (self.state / "archive.key").write_bytes(os.urandom(32))
        with self.assertRaises(archive.Invalid):
            self.call("preview", self.backup)
        self.assertFalse((self.state / "pending.json").exists())
        self.assertFalse(self.calls)

    def synthetic(self, entries):
        # Create signed malformed envelopes locally to exercise validation
        # behind the authentication check; the production CLI cannot do this.
        buf = io.BytesIO()
        with zipfile.ZipFile(buf, "w") as z:
            for name, data, mode, compression in entries:
                info = zipfile.ZipInfo(name)
                info.external_attr = mode << 16
                z.writestr(info, data, compress_type=compression)
            z.comment = archive.COMMENT + b"0" * 64
        prefix = buf.getvalue()[:-64]
        stats = [self.host.stat(), self.gw.stat()]
        return prefix + archive.sign((self.state / "archive.key").read_bytes(), stats, prefix)

    def test_rejects_noncanonical_members_and_invalid_configs(self):
        name, gw = archive.NAMES
        mode = stat.S_IFREG | 0o600
        good = [(name, HOST, mode, 0), (gw, GW, mode, 0)]
        for entries in (
                good[:1], good + [("extra", b"", mode, 0)], [good[0], good[0]],
                [("../" + name, HOST, mode, 0), good[1]], [("/etc/" + name, HOST, mode, 0), good[1]],
                [(name, HOST, stat.S_IFLNK | 0o600, 0), good[1]],
                [(name, b"a" * (archive.FILE_MAX * 6), mode, zipfile.ZIP_DEFLATED), good[1]],
                [(name, b"a" * (archive.FILE_MAX + 1), mode, 0), good[1]],
                [(name, b"[Modem]\n", mode, 0), good[1]],
                [good[0], (gw, GW + b"Password=also\n", mode, 0)],
                [(name, HOST + b"\0", mode, 0), good[1]],
                [(name, HOST + b"\n[bad\n", mode, 0), good[1]]):
            with self.subTest(names=[e[0] for e in entries]), warnings.catch_warnings():
                warnings.simplefilter("ignore", UserWarning)
                with self.assertRaises(archive.Invalid):
                    self.call("preview", self.synthetic(entries))

    def test_restart_failure_recovers_files_restarts_originals(self):
        self.alter()
        before = [p.read_bytes() for p in (self.host, self.gw)]
        def fail_once(unit):
            self.calls.append(unit)
            self.assertEqual(json.loads((self.state / "pending.json").read_bytes()), [b.decode() for b in before])
            if len(self.calls) == 2:
                raise OSError("synthetic")
        with self.assertRaises(OSError):
            archive.execute("apply", self.approved(), fail_once)
        self.assertEqual(self.calls, list(reversed(archive.UNITS)) * 2)
        self.assertEqual([p.read_bytes() for p in (self.host, self.gw)], before)
        self.assertFalse((self.state / "pending.json").exists())
        self.assertFalse((self.state / "previous.json").exists())

    def test_restore_repairs_malformed_current_ini_and_restarts_only_changed_service(self):
        self.host.write_bytes(b"[General\n")
        with self.assertRaises(archive.Invalid):
            self.call("backup")
        self.assertEqual(json.loads(self.call("preview", self.backup))["changed"], [archive.NAMES[0]])
        self.call("apply", self.approved())
        self.assertEqual(self.calls, [archive.UNITS[0]])
        self.assertEqual(self.host.read_bytes(), HOST)
        self.assertEqual(json.loads((self.state / "previous.json").read_bytes())[0], "[General\n")

    def test_second_replace_failure_recovers_first_without_restarting(self):
        self.alter()
        before = [p.read_bytes() for p in (self.host, self.gw)]
        approved = self.approved()
        real_replace = archive.replace
        def fail_target(path, data, *args):
            if path == self.gw and data == GW:
                raise OSError("synthetic")
            real_replace(path, data, *args)
        with patch.object(archive, "replace", fail_target), self.assertRaises(OSError):
            self.call("apply", approved)
        self.assertEqual([p.read_bytes() for p in (self.host, self.gw)], before)
        self.assertFalse((self.state / "pending.json").exists())
        self.assertFalse(self.calls)

    def test_interrupted_recovery_blocks_further_mutation(self):
        self.alter()
        payload = self.approved()
        def fail(unit):
            raise OSError("synthetic")
        with self.assertRaises(OSError):
            archive.execute("apply", payload, fail)
        self.assertTrue((self.state / "pending.json").exists())
        with self.assertRaises(OSError):
            self.call("apply", payload)
        self.assertFalse(self.calls)

    def test_symlinks_unsafe_mode_and_shared_lock(self):
        with (self.state / "lock").open("r+") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            with self.assertRaises(BlockingIOError):
                self.call("backup")
        self.host.chmod(0o666)
        with self.assertRaises(OSError):
            self.call("backup")
        self.host.unlink()
        self.host.symlink_to(self.gw)
        with self.assertRaises(OSError):
            self.call("backup")
        self.host.unlink()
        self.host.write_bytes(HOST)
        (self.state / "archive.key").unlink()
        (self.state / "archive.key").symlink_to(self.state / "missing")
        with self.assertRaises(OSError):
            self.call("backup")


if __name__ == "__main__":
    unittest.main()
