"""radio/gospot-radio-migrate on fixture trees: a board with the pistar-radio
layout ends up with the gospot-radio one, nothing is lost, owners, modes and
untouched bytes stay, and a second run changes nothing."""
import hashlib
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
SCRIPT = HERE / "gospot-radio-migrate"
REBRAND = HERE / "gospot-rebrand"
SETTINGS = HERE.parent / "Pi-Star_dash_new" / "gospot.conf"

MMDVMHOST = (b"[General]\r\nCallsign=M1ABC\r\n[Info]\r\nURL=http://www.mw0mwz.co.uk/pi-star/\r\n"
             b"Description=M1ABC_Pi-Star\r\n[Log]\r\nDisplayLevel=1\r\nFilePath=/var/log/pi-star\r\n"
             b"FileRoot=MMDVM\r\nOther=/var/log/pi-starfish\r\n")
YSFGATEWAY = b"[Log]\nFilePath = /var/log/pi-star/\nFileRoot=YSFGateway\n# FilePath=/var/log/pi-star\n"
RELEASE = b"[Pi-Star]\nVersion = 4.3-native\nHardware = RPi\n"
MOTD = (b"#!/bin/bash\n# Pi-Star MOTD Generator.\n"
        b"pistarCurVersion=$(awk -F \"= \" '/Version/ {print $2}' /etc/pistar-release)\n"
        b"echo \"Welcome to Pi-Star: v${pistarCurVersion}\"\n")
FAKE_SYSTEMCTL = """#!/bin/sh
echo "$*" >>"$(dirname "$0")/calls"
if [ "$1 $2 $3" = "is-active --quiet pistar-watchdog.service" ] && ! grep -qx "stop pistar-watchdog.service" "$(dirname "$0")/stopped" 2>/dev/null; then exit 0; fi
[ "$1" = stop ] && echo "$*" >>"$(dirname "$0")/stopped"
case "$1" in is-active) exit 3;; esac
exit 0
"""


def settings():
    out = {}
    for line in SETTINGS.read_text().splitlines():
        if "=" in line and not line.startswith("#"):
            k, v = line.split("=", 1)
            out[k] = v
    return out


def snapshot(root):
    """Every path under root: (type, mode, uid, gid, content hash or link target)."""
    out = {}
    for dirpath, dirs, files in os.walk(root):
        for name in dirs + files:
            p = os.path.join(dirpath, name)
            st = os.lstat(p)
            if os.path.islink(p):
                what = "link:" + os.readlink(p)
            elif os.path.isdir(p):
                what = "dir"
            else:
                what = hashlib.sha256(Path(p).read_bytes()).hexdigest()
            out[os.path.relpath(p, root)] = (what, st.st_mode, st.st_uid, st.st_gid, st.st_ino)
    return out


class Migrate(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name) / "root"
        self.bin = Path(self.tmp.name) / "bin"
        self.bin.mkdir()
        (self.bin / "systemctl").write_text(FAKE_SYSTEMCTL)
        (self.bin / "systemctl").chmod(0o755)
        s = self.s = settings()
        keys = ("GOSPOT_LOG_DIR", "GOSPOT_RELEASE_FILE", "GOSPOT_CSS_FILE", "GOSPOT_KEEPER_LOG", "UNIT_GOSPOT_WATCHDOG",
                "UNIT_GOSPOT_REMOTE")
        paths = [f"{k}={s[k]}" for k in keys] + [f"{k}={v}" for k, v in s.items() if k.startswith("CONF_")]
        paths.append("RADIO_UNITS=mmdvmhost.service,dmrgateway.service")
        self.paths = Path(self.tmp.name) / "paths"
        self.paths.write_text("\n".join(paths) + "\n")
        self.table = Path(self.tmp.name) / "table"
        self.table.write_bytes(subprocess.run([str(REBRAND), "resolve", str(HERE / "gospot-rename.table"), str(SETTINGS)],
                                              check=True, capture_output=True).stdout)
        r = self.root
        self.put("var/log/pi-star/MMDVM-2026-11-27.log", b"M: 2026-11-27 10:00:00.000 MMDVMHost is running\n", 0o640)
        self.put("var/log/pi-star/DMRGateway-2026-11-27.log", b"M: gateway\n", 0o644)
        self.put("var/log/pi-star/Links.log", b"", 0o644)
        (r / "var/log/pi-star").chmod(0o775)
        self.put("etc/mmdvmhost", MMDVMHOST, 0o600)
        self.put("etc/ysfgateway", YSFGATEWAY, 0o644)
        self.put("etc/pistar-release", RELEASE, 0o644)
        self.put("etc/pistar-css.ini", b"[Background]\nPage=edf0f5\n", 0o644)
        self.put("etc/pistar-remote", b"# Pi-Star Remote config file\n[enable]\nenabled=false\n", 0o644)
        self.put("var/pistar-keeper/pistar-keeper.log", b"keeper line\n", 0o640)
        self.put("etc/update-motd.d/50-pistar-motd", MOTD, 0o755)
        for wants, unit in (("multi-user.target.wants", "pistar-watchdog.service"), ("timers.target.wants", "pistar-hostfiles.timer")):
            (r / "etc/systemd/system" / wants).mkdir(parents=True, exist_ok=True)
            os.symlink("/lib/systemd/system/" + unit, r / "etc/systemd/system" / wants / unit)

    def tearDown(self):
        self.tmp.cleanup()

    def put(self, rel, data, mode):
        p = self.root / rel
        p.parent.mkdir(parents=True, exist_ok=True)
        p.write_bytes(data)
        p.chmod(mode)

    def run_migration(self):
        env = dict(os.environ, GOSPOT_SYSTEMCTL=str(self.bin / "systemctl"))
        r = subprocess.run([str(SCRIPT), str(self.paths), str(self.table), str(self.root)], env=env,
                           capture_output=True, text=True)
        self.assertEqual(r.returncode, 0, r.stderr)
        return r.stdout

    def calls(self):
        p = self.bin / "calls"
        calls = p.read_text().splitlines() if p.exists() else []
        p.unlink(missing_ok=True)
        return [c for c in calls if not c.startswith("is-active")]

    def at(self, path):
        return self.root / path.lstrip("/")

    def test_old_layout_becomes_the_new_one(self):
        s, r = self.s, self.root
        before = snapshot(r)
        out = self.run_migration()
        after = snapshot(r)
        logs = self.at(s["GOSPOT_LOG_DIR"])
        # Logs: same inodes, bytes and modes in the new directory; the old one is gone.
        for name in ("MMDVM-2026-11-27.log", "DMRGateway-2026-11-27.log", "Links.log"):
            self.assertEqual(after[os.path.relpath(logs / name, r)], before["var/log/pi-star/" + name])
        self.assertFalse((r / "var/log/pi-star").exists())
        self.assertEqual(os.stat(logs).st_mode & 0o7777, 0o775)
        # Configurations: only the log path values change.
        new = s["GOSPOT_LOG_DIR"].encode()
        self.assertEqual(self.at(s["CONF_MMDVMHOST"]).read_bytes(),
                         MMDVMHOST.replace(b"FilePath=/var/log/pi-star\r\n", b"FilePath=" + new + b"\r\n"))
        self.assertEqual(os.stat(self.at(s["CONF_MMDVMHOST"])).st_mode & 0o7777, 0o600)
        self.assertEqual(self.at(s["CONF_YSFGATEWAY"]).read_bytes(),
                         YSFGATEWAY.replace(b"FilePath = /var/log/pi-star/", b"FilePath = " + new + b"/", 1))
        # Renamed Pi-Star files; the release file gets the GoSpot section name only.
        self.assertEqual(self.at(s["GOSPOT_RELEASE_FILE"]).read_bytes(), RELEASE.replace(b"[Pi-Star]", b"[GoSpot]"))
        for old, key in (("etc/pistar-css.ini", "GOSPOT_CSS_FILE"), ("etc/pistar-remote", "CONF_GOSPOT_REMOTE"),
                         ("var/pistar-keeper/pistar-keeper.log", "GOSPOT_KEEPER_LOG")):
            self.assertEqual(after[os.path.relpath(self.at(s[key]), r)], before[old], old)
            self.assertFalse((r / old).exists(), old)
        self.assertFalse((r / "var/pistar-keeper").exists())
        motd = r / "etc/update-motd.d/50-gospot-motd"
        self.assertEqual(motd.read_text(), "#!/bin/bash\n# Pi-Star MOTD Generator.\n"
                         f"gospotVersion=$(awk -F \"= \" '/Version/ {{print $2}}' {s['GOSPOT_RELEASE_FILE']})\n"
                         "echo \"Welcome to GoSpot: v${gospotVersion}\"\n")
        self.assertEqual(os.stat(motd).st_mode & 0o7777, 0o755)
        self.assertFalse((r / "etc/update-motd.d/50-pistar-motd").exists())
        # Units: dangling links of the removed package go, the gospot units take over.
        self.assertEqual(list((r / "etc/systemd/system").glob("*.wants/pistar-*")), [])
        self.assertEqual(self.calls(), [f"enable {s['UNIT_GOSPOT_WATCHDOG']}", "stop pistar-watchdog.service",
                                        "enable gospot-hostfiles.timer", "try-restart mmdvmhost.service dmrgateway.service",
                                        f"start {s['UNIT_GOSPOT_WATCHDOG']}"])
        # Nothing lost: every file's content is still somewhere.
        hashes = {v[0] for v in after.values()}
        lost = {k for k, v in before.items() if v[0] not in ("dir",) and not v[0].startswith("link:") and v[0] not in hashes
                and k not in ("etc/mmdvmhost", "etc/ysfgateway", "etc/pistar-release", "etc/update-motd.d/50-pistar-motd")}
        self.assertEqual(lost, set())
        self.assertNotIn("admin-password", out)
        # Second run: nothing to do, nothing changes.
        again = self.run_migration()
        self.assertEqual(again, "")
        self.assertEqual(self.calls(), [])
        self.assertEqual(snapshot(r), after)

    def test_existing_targets_are_kept(self):
        s = self.s
        logs = self.at(s["GOSPOT_LOG_DIR"])
        logs.mkdir(parents=True)
        (logs / "Links.log").write_bytes(b"new\n")
        self.at(s["GOSPOT_RELEASE_FILE"]).write_bytes(b"[GoSpot]\nVersion = 4.3-native\n")
        out = self.run_migration()
        self.assertIn(f"kept /var/log/pi-star/Links.log: {s['GOSPOT_LOG_DIR']}/Links.log already exists", out)
        self.assertIn(f"kept /etc/pistar-release: {s['GOSPOT_RELEASE_FILE']} already exists", out)
        self.assertEqual((self.root / "var/log/pi-star/Links.log").read_bytes(), b"")
        self.assertEqual((logs / "Links.log").read_bytes(), b"new\n")
        self.assertEqual((self.root / "etc/pistar-release").read_bytes(), RELEASE)
        self.assertTrue((logs / "MMDVM-2026-11-27.log").exists())
        before = snapshot(self.root)
        self.calls()
        self.run_migration()
        self.assertEqual(snapshot(self.root), before)
        self.assertEqual(self.calls(), [])

    def test_fresh_system_is_left_alone(self):
        for p in sorted(self.root.rglob("*"), reverse=True):
            p.unlink() if p.is_file() or p.is_symlink() else p.rmdir()
        (self.bin / "stopped").write_text("stop pistar-watchdog.service\n")  # no old unit is running
        self.assertEqual(self.run_migration(), "")
        self.assertEqual(self.calls(), [])
        self.assertEqual(list(self.root.rglob("*")), [])


if __name__ == "__main__":
    unittest.main()
