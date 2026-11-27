"""radio/gospot-rebrand with radio/gospot-rename.table: the Pi-Star tools,
handlers and default configurations gospot-radio ships are rewritten for the
GoSpot layout, and anything the table neither renames nor allows is refused."""
import os
import subprocess
import tempfile
import unittest
import zipfile
from pathlib import Path

HERE = Path(__file__).resolve().parent
TOOL = HERE / "gospot-rebrand"
TABLE = HERE / "gospot-rename.table"
SETTINGS = HERE.parent / "Pi-Star_dash_new" / "gospot.conf"
SBIN = HERE.parent / "Pi-Star_Binaries_sbin"
ZIP = HERE.parent / "Pi-Star_v4_Binaries_Bin" / "config_clean.zip"
# Shipped by radio/build-deb besides the tool rules of the table.
HANDLERS = ("mmdvmhost dmrgateway ysfgateway ysfparrot p25gateway p25parrot nxdngateway nxdnparrot nxdn2dmr "
            "dapnetgateway m17gateway dgidgateway aprsgateway ysf2dmr ysf2nxdn ysf2p25 dmr2ysf dmr2nxdn mobilegps "
            "ircddbgateway timeserver timercontrol dstarrepeater").split()


def settings():
    out = {}
    for line in SETTINGS.read_text().splitlines():
        if "=" in line and not line.startswith("#"):
            k, v = line.split("=", 1)
            out[k] = v
    return out


def run(*args, data=b""):
    return subprocess.run([str(TOOL), *args], input=data, capture_output=True)


class Rebrand(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmp = tempfile.TemporaryDirectory()
        cls.table = Path(cls.tmp.name) / "table"
        r = run("resolve", str(TABLE), str(SETTINGS))
        assert r.returncode == 0, r.stderr
        cls.table.write_bytes(r.stdout)
        cls.s = settings()

    @classmethod
    def tearDownClass(cls):
        cls.tmp.cleanup()

    def apply(self, text, name="x"):
        r = run("apply", str(self.table), name, data=text.encode())
        return r.returncode, r.stdout.decode(), r.stderr.decode()

    def test_resolve_takes_paths_from_the_settings_file(self):
        t = self.table.read_text()
        self.assertIn("literal\t/var/log/pi-star\t" + self.s["GOSPOT_LOG_DIR"] + "\n", t)
        self.assertIn("literal\t/etc/pistar-release\t" + self.s["GOSPOT_RELEASE_FILE"] + "\n", t)
        self.assertNotIn("${", t)
        bad = Path(self.tmp.name) / "empty.conf"
        bad.write_text("GOSPOT_LOG_DIR=/var/log/gospot\n")
        r = run("resolve", str(TABLE), str(bad))
        self.assertEqual(r.returncode, 1)
        self.assertIn(b"has no MODEM_FLASH_LOG", r.stderr)
        # An unresolved table is refused instead of writing "${KEY}" into a script.
        self.assertEqual(run("apply", str(TABLE), data=b"x\n").returncode, 1)

    def test_rules(self):
        code, out, err = self.apply("# Pi-Star Find Modem Tool.\n"
                                    "echo \"Stopping Pi-Star Services...\"\n"
                                    "systemctl stop pistar-watchdog.timer\n"
                                    "cd /var/log/pi-star/; pistar-link ref001_c\n"
                                    "curl http://www.pistar.uk/downloads/DMRIds.dat.gz --user-agent \"Pi-Star_${pistarCurVersion}\"\n"
                                    "Description=M1ABC_Pi-Star\nURL=http://www.mw0mwz.co.uk/pi-star/\n")
        self.assertEqual(code, 0, err)
        self.assertEqual(out, "# Pi-Star Find Modem Tool.\n"
                              "echo \"Stopping GoSpot Services...\"\n"
                              "systemctl stop gospot-watchdog.timer\n"
                              f"cd {self.s['GOSPOT_LOG_DIR']}/; gospot-link ref001_c\n"
                              "curl http://www.pistar.uk/downloads/DMRIds.dat.gz --user-agent \"Pi-Star_${gospotVersion}\"\n"
                              "Description=M1ABC_GoSpot\nURL=http://www.mw0mwz.co.uk/pi-star/\n")
        self.assertEqual(self.apply(out)[1], out, "idempotent")

    def test_unknown_occurrences_fail(self):
        for text in ("PATH=/opt/pistar-tools\n", "echo Pi-Star_Thing\n", "# see /usr/local/sbin/pistar-thing\n",
                     "x=PISTAR\n", "dir=/home/pi-star\n"):
            code, out, err = self.apply(text, "probe")
            self.assertEqual(code, 1, text)
            self.assertIn("probe:1: not in gospot-rename.table", err)

    def test_check_does_not_rewrite(self):
        r = run("check", str(self.table), "probe", data=b"systemctl stop pistar-watchdog.service\n")
        self.assertEqual((r.returncode, r.stdout), (1, b""))
        self.assertIn(b"probe:1: not in gospot-rename.table", r.stderr)
        r = run("check", str(self.table), data=b"# Pi-Star Find Modem Tool.\nsystemctl stop gospot-watchdog.service\n")
        self.assertEqual((r.returncode, r.stdout), (0, b""))

    def test_delete_block(self):
        first = "# workaround to check if user stuck on pistar-update v3.3 or v3.4, if yes then force update now"
        code, out, err = self.apply(f"echo a\n{first}\nif grep -q x /usr/local/sbin/pistar-update; then\n  sudo pkill pistar-update\nfi\necho b\nfi\n")
        self.assertEqual((code, out), (0, "echo a\necho b\nfi\n"), err)
        code, out, err = self.apply(f"{first}\nif true; then\n")
        self.assertEqual(code, 1)
        self.assertIn("no closing line", err)

    def test_bytes_pass_through(self):
        data = b"# caf\xe9\nFilePath=/var/log/pi-star\r\n"
        r = run("apply", str(self.table), data=data)
        self.assertEqual(r.returncode, 0)
        self.assertEqual(r.stdout, b"# caf\xe9\nFilePath=" + self.s["GOSPOT_LOG_DIR"].encode() + b"\r\n")

    def test_names(self):
        tools = run("tools", str(self.table)).stdout.decode().split("\n")
        self.assertIn("pistar-modemupgrade gospot-modemupgrade", tools)
        self.assertEqual(run("name", str(self.table), "pistar-remote").stdout, b"gospot-remote\n")
        self.assertEqual(run("name", str(self.table), "mmdvmhost").stdout, b"mmdvmhost\n")

    @unittest.skipUnless(SBIN.is_dir(), "Pi-Star_Binaries_sbin is not checked out here")
    def test_every_shipped_script(self):
        names = [line.split()[0] for line in run("tools", str(self.table)).stdout.decode().splitlines()]
        names += ["HostFilesUpdate.sh"] + [h + ".service" for h in HANDLERS]
        out = {}
        bin_dir = ZIP.parent
        for name in names + ["platformDetect.sh"]:
            src = bin_dir / name if name == "platformDetect.sh" else SBIN / name
            if not src.exists():
                continue
            code, text, err = self.apply(src.read_text(errors="surrogateescape"), name)
            self.assertEqual(code, 0, err)
            for old in ("/var/log/pi-star", "/etc/pistar-release", "pistar-watchdog", "pistarCurVersion"):
                self.assertNotIn(old, text, name)
            out[name] = text
        s = self.s
        self.assertIn(f">> {s['GOSPOT_JITTER_LOG']} 2>&1", out["pistar-jittertest"])
        self.assertIn("/var/lock/gospot-jitter.lock", out["pistar-jittertest"])
        self.assertIn(f">> {s['MODEM_FLASH_LOG']} 2>&1", out["pistar-modemupgrade"])
        self.assertIn(f"/Hardware/ {{print $2}}' {s['GOSPOT_RELEASE_FILE']})", out["pistar-modemupgrade"])
        self.assertIn("systemctl stop gospot-watchdog.service", out["pistar-mmdvmcal"])
        self.assertIn("gospot-bmapi addtg", out["pistar-bmapi"])
        self.assertEqual(out["HostFilesUpdate.sh"].count('--user-agent "Pi-Star_${gospotVersion}"'),
                         (SBIN / "HostFilesUpdate.sh").read_text().count('--user-agent "Pi-Star_${pistarCurVersion}"'))
        motd = out["pistar-motdgen"]
        self.assertIn("/etc/update-motd.d/50-gospot-motd", motd)
        self.assertIn("Welcome to GoSpot:", motd)
        self.assertIn('GoSpot is Pi-Star compatible; its tools start "gospot-".', motd)
        self.assertIn(f"LOGDIR={s['GOSPOT_LOG_DIR']}\n", out["mmdvmhost.service"])
        self.assertIn('gospot-oledtext.py "GoSpot" "Unconfigured"', out["mmdvmhost.service"])
        self.assertIn(f'grep "Version" {s["GOSPOT_RELEASE_FILE"]} | cut -c 11', out["dstarrepeater.service"])
        if "platformDetect.sh" in out:
            self.assertNotIn("git --work-tree=/usr/local/sbin", out["platformDetect.sh"])
            self.assertIn("Generic", out["platformDetect.sh"])
        # Attribution banners are kept as they are.
        self.assertIn("# Written for Pi-Star (http://www.mw0mwz.co.uk/pi-star) #", out["ircddbgateway.service"])

    @unittest.skipUnless(ZIP.is_file(), "Pi-Star_v4_Binaries_Bin is not checked out here")
    def test_every_default_config(self):
        with zipfile.ZipFile(ZIP) as z:
            for member in z.namelist():
                code, text, err = self.apply(z.read(member).decode(), member)
                self.assertEqual(code, 0, err)
                self.assertNotIn("/var/log/pi-star", text)
                if "FilePath=" in text:
                    self.assertIn("FilePath=" + self.s["GOSPOT_LOG_DIR"] + "\n", text)


if __name__ == "__main__":
    unittest.main()
