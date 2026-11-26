"""Round-trip the stock backup through Chromium using only the disposable fixture."""

from hashlib import sha256
from io import BytesIO
from pathlib import Path
import json
import re
from subprocess import check_output
from tempfile import TemporaryDirectory
from urllib.parse import urlsplit
from zipfile import ZipFile
from zlib import crc32

from full_ui import ELEMENT, View, equal
from smoke import wait


PAGE = "/admin/config_backup.php"
FILE = 'input[type="file"][name="fileToUpload"]'
CHANGE = ("/etc/mmdvmhost", "/etc/dmrgateway", "/etc/wpa_supplicant/wpa_supplicant.conf")
PHP = ("/var/www/dashboard/config/config.php", "/var/www/dashboard/config/ircddblocal.php")
KEEP = ("/etc/hostapd/hostapd.conf", "/etc/dhcpcd.conf", "/etc/dstar-radio.qa-keep",
        "/root/.Remote Control", "/etc/passwd")


def same_bytes(label, actual, expected):
    # Byte comparison, without printing config contents or fixture credentials on failure.
    if actual != expected:
        raise AssertionError(f"{label}: bytes differ (got {len(actual)}, expected {len(expected)})")


def form(view):
    token = view.js("""
      const f = document.querySelector(arguments[0])?.form;
      // Two buttons named "action" shadow HTMLFormElement.action.
      if (!f || new URL(f.getAttribute('action'), document.baseURI).href !== location.href || f.method !== 'post' ||
          f.enctype !== 'multipart/form-data' ||
          !['download','restore'].every(a => all(`button[name=action][value=${a}]`, f).some(visible)))
        return null;
      return new FormData(f).get('csrf_token');
    """, FILE)
    if not re.fullmatch("[0-9a-f]{64}", token or ""):
        diagnostic=view.js("""return {url:location.pathname,title:document.title,formCount:document.forms.length,
            serverText:document.forms.length ? '' : document.body.innerText.slice(0,180)}""")
        raise AssertionError(f"Backup: native multipart form or session token missing: {diagnostic}")
    return token


def run(driver, origin, container):
    if (urlsplit(origin).hostname != "127.0.0.1" or
            not re.fullmatch("pstar-classic-write-[a-f0-9]+", container)):
        raise ValueError("Backup requires a loopback classic-write fixture")
    docker = ["docker", "exec", container, "chroot", "/stand"]

    def read(path):
        return check_output([*docker, "cat", "--", path], timeout=10)

    def inventory(exclude):
        # Compare the entire configuration trees, not only the named sentinels.
        # Ignore atime/mtime; include contents, links, permission bits and identity.
        code = r'''
        $out=[];
        foreach (["/etc","/root","/usr/local/etc","/var/www/dashboard/config"] as $root) {
          foreach (new RecursiveIteratorIterator(new RecursiveDirectoryIterator(
            $root, FilesystemIterator::SKIP_DOTS), RecursiveIteratorIterator::SELF_FIRST) as $f) {
            $p=$f->getPathname(); $s=lstat($p);
            $out[$p]=[$s["mode"],$s["uid"],$s["gid"],is_link($p) ? readlink($p) :
              (is_file($p) ? hash_file("sha256",$p) : null)];
          }
        }
        echo json_encode($out, JSON_THROW_ON_ERROR);
        '''
        result=json.loads(check_output([*docker,"php","-n","-r",code],timeout=15))
        return {k:v for k,v in result.items() if k not in exclude}

    targets = {}
    for line in read("/work/backup-map.tsv").decode().splitlines():
        name, path = line.split("\t")
        if name != Path(path).name or not Path(path).is_absolute() or name in targets:
            raise AssertionError(f"Backup fixture: invalid map entry {name!r}")
        targets[name] = path
    for path in (*CHANGE, *PHP):
        equal(f"Backup fixture target {path}", targets.get(Path(path).name), path)
    for path in KEEP:
        equal(f"Backup fixture excludes {path}", Path(path).name in targets, False)
    originals = {name: read(path) for name, path in targets.items()}
    untouched = {path: read(path) for path in KEEP}
    others = inventory(set(targets.values()))
    if re.search(rb"(?m)^\s*static\s+ip_address\s*=", untouched["/etc/dhcpcd.conf"]):
        raise AssertionError("Backup fixture requires dynamic DHCP")

    view = View(driver, origin.rstrip("/") + PAGE, 1366, 900, False)
    session_token = form(view)
    view.cdp("Network.setCacheDisabled", {"cacheDisabled": True})

    def fresh_get():
        # Explicit URL navigation, never a refresh of the restore POST response.
        driver("/url", {"url": view.url})
        if form(view) != session_token:
            raise AssertionError("Backup: fresh GET lost the original form session")

    # Snap Chromium's /tmp is private; the browser and driver must see the same ZIP.
    shared = Path.home() / "snap/chromium/common"
    shared.mkdir(parents=True, exist_ok=True)
    with TemporaryDirectory(dir=shared, prefix="classic-backup-") as folder:
        view.cdp("Browser.setDownloadBehavior", {"behavior": "allow", "downloadPath": folder})
        view.click('button[name="action"][value="download"]')
        matches = wait("Backup: browser did not download the ZIP",
                       lambda: list(Path(folder).glob("*.zip")), timeout=30)
        equal("Backup completed downloads", len(matches), 1)
        archive_path = matches[0]
        downloaded = archive_path.read_bytes()
        if not downloaded.startswith(b"PK\x03\x04"):
            raise AssertionError("HTML, warnings or other bytes before ZIP payload")
        with ZipFile(BytesIO(downloaded)) as z:
            equal("Backup exact ZIP entries (including multiplicity)", sorted(z.namelist()), sorted(targets))
            equal("Backup ZIP CRC check", z.testzip(), None)
            members = {}
            for entry in z.infolist():
                name = entry.filename
                equal(f"Backup {name} CRC vs source", entry.CRC, crc32(originals[name]))
                members[name] = z.read(entry)
                same_bytes(f"Backup {name} vs {targets[name]}", members[name], originals[name])
        print(f"Backup: downloaded {len(targets)} entries; ZIP bytes, names, CRCs and sources verified.")

        fresh_get()
        for path, data in untouched.items():
            same_bytes(f"Backup download changed sentinel {path}", read(path), data)
        assert others==inventory(set(targets.values())), "download changed the configuration trees"
        for path in CHANGE:
            check_output([*docker, "sh", "-ec", 'printf "\\n# QA post-download edit\\n" >> "$1"',
                          "backup-fixture", path], timeout=10)
            same_bytes(f"Backup deliberate edit to {path}", read(path),
                       originals[Path(path).name] + b"\n# QA post-download edit\n")

        file_input = view.field(FILE)
        driver(f"/element/{file_input[ELEMENT]}/value", {"text": str(archive_path)})
        # Hash the File actually selected in Chromium before clicking the native submitter.
        selected = view.js("""
          const [file] = document.querySelector(arguments[0]).files;
          return file.arrayBuffer().then(b => crypto.subtle.digest('SHA-256', b))
            .then(hash => Array.from(new Uint8Array(hash), x => x.toString(16).padStart(2,'0')).join(''));
        """, FILE)
        equal("Backup selected ZIP SHA-256", selected, sha256(downloaded).hexdigest())
        view.click('button[name="action"][value="restore"]')
        equal("PHP received identical browser download",read("/tmp/qa-upload.sha256").decode(),sha256(downloaded).hexdigest())
        report = view.js("return document.querySelector('.contentwide pre')?.innerText || ''").splitlines()
        for line in (f"Your .zip file was uploaded and unpacked ({len(targets)} files).",
                     f"Installed {len(targets)} file(s).", "Configuration Restore Complete."):
            if line not in report:
                raise AssertionError(f"Backup restore report missing {line!r}")
        fresh_get()

    requests = [e["params"]["request"] for e in view.events()
                if e["method"] == "Network.requestWillBeSent" and e["params"]["request"]["url"] == view.url]
    equal("Backup browser request sequence", [r["method"] for r in requests],
          ["GET", "POST", "GET", "POST", "GET"])
    for name, path in targets.items():
        same_bytes(f"Backup restored {path}", read(path), members[name])
    after=inventory(set(targets.values()))
    changed=[p for p in sorted(others.keys()|after.keys()) if others.get(p)!=after.get(p)]
    if changed: raise AssertionError(f"Restore altered non-members: {', '.join(changed)}")
    for path, data in untouched.items(): same_bytes(f"Backup unrelated sentinel {path}", read(path), data)
    print(f"PASS restored ZIP SHA256 {sha256(downloaded).hexdigest()}; {len(others)} non-member filesystem entries unchanged (including modes, owners and symlink targets)")
    print(f"Backup: restore reported {len(targets)} installed; fresh GET succeeded. "
          f"All destinations match the ZIP (including both PHP files), {len(CHANGE)} deliberate "
          f"edits reversed and {len(KEEP)} non-archived sentinels byte-identical.", flush=True)
