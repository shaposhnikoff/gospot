"""Independent disk and service oracle for the classic callsign browser save."""

from collections import Counter
from dataclasses import dataclass, replace
from hashlib import sha256
from io import BytesIO
from pathlib import PurePosixPath
from subprocess import check_output
import tarfile


# Include the real Apply targets outside /etc (radio timer units, UPnP helper,
# dashboard PHP); the directories are inventoried even when files are absent.
ROOTS = ("/etc", "/root", "/usr/local/etc", "/usr/local/sbin",
         "/usr/lib/systemd/system", "/var/www/dashboard/config")
LOCAL = "/var/www/dashboard/config/ircddblocal.php"
TZ_FILE = "/etc/timezone"
TZ_PHP = "/var/www/dashboard/config/config.php"
LANG_PHP = "/var/www/dashboard/config/language.php"
MODEM_MARKER = "/etc/dstar-radio.mmdvmhost"

# Each left side is a whole physical line *in the indicated section*. In
# particular, "N0DEMO" in another setting, comment, bridge or secret is data.
# The APRS-IS passcode for QA7TEST is the fixed test vector 1188.
def _call(section=b"General"):
    return {(section, b"Callsign=N0DEMO"): b"Callsign=QA7TEST"}

APRS_DESCRIPTION = {(b"APRS", b"Description=N0DEMO_Pi-Star_ND"): b"Description=QA7TEST_Pi-Star_ND"}
BRIDGE_IDENTITY = {**_call(b"YSF Network"),
                   (b"Info", b"Description=N0DEMO_Pi-Star"): b"Description=QA7TEST_Pi-Star"}
LEGACY_APRS = {
    (b"aprs.fi", b"Password=13528"): b"Password=1188",
    (b"aprs.fi", b"Description=N0DEMO_Pi-Star"): b"Description=QA7TEST_Pi-Star",
}
CALL_FILES = {
    "/etc/ircddbgateway": {
        (b"", b"gatewayCallsign=N0DEMO"): b"gatewayCallsign=QA7TEST",
        (b"", b"dplusLogin=N0DEMO  "): b"dplusLogin=QA7TEST ",
        (b"", b"repeaterCall1=N0DEMO"): b"repeaterCall1=QA7TEST",
    },
    "/etc/dstarrepeater": {
        (b"", b"callsign=N0DEMO B"): b"callsign=QA7TESTB",
        (b"", b"gateway=N0DEMO G"): b"gateway=QA7TESTG",
        (b"", b"beaconText=N0DEMO B"): b"beaconText=QA7TESTB",
    },
    "/etc/timeserver": {(b"", b"callsign=N0DEMO"): b"callsign=QA7TEST"},
    "/etc/starnetserver": {
        (b"", b"callsign=N0DEMO"): b"callsign=QA7TEST",
        (b"", b"ircddbUsername=N0DEMO"): b"ircddbUsername=QA7TEST",
    },
    "/etc/aprsgateway": {
        **_call(),
        (b"APRS-IS", b"Password=13528"): b"Password=1188",
    },
    "/etc/mmdvmhost": _call(),
    "/etc/ysfgateway": {
        **_call(), **APRS_DESCRIPTION,
        (b"Info", b"Name=N0DEMO_Pi-Star"): b"Name=QA7TEST_Pi-Star",
    },
    "/etc/p25gateway": _call(),
    "/etc/m17gateway": _call(),
    "/etc/nxdngateway": {**_call(), **APRS_DESCRIPTION},
    "/etc/ysf2dmr": {**BRIDGE_IDENTITY, **LEGACY_APRS},
    "/etc/ysf2nxdn": {**BRIDGE_IDENTITY, **LEGACY_APRS},
    "/etc/ysf2p25": BRIDGE_IDENTITY,
    "/etc/dmr2ysf": _call(b"YSF Network"),
    LOCAL: {(b"", b"$callsign='N0DEMO';"): b"$callsign='QA7TEST';"},
}

# Service effects are compared as multisets *within* phases. Only the modeled
# active consumers should restart. An installed but inactive bridge stays off.
CHECKED_UNITS = frozenset(f"{name}.service" for name in (
    "ircddbgateway", "dstarrepeater", "timeserver", "starnetserver", "aprsgateway",
    "mmdvmhost", "ysfgateway", "ysf2dmr", "ysf2nxdn", "ysf2p25", "dmr2ysf",
    "p25gateway", "nxdngateway", "m17gateway",
))
ACTIVE_UNITS = {"mmdvmhost.service"}

# Unrelated controls exercise more than an empty /etc with callsign targets.
SENTINELS = (
    "/etc/dmrgateway", "/etc/dstar-radio.mmdvmhost", "/etc/dmr2nxdn", "/etc/nxdn2dmr",
    "/etc/wpa_supplicant/wpa_supplicant.conf", "/usr/local/sbin/pistar-upnp.service",
    "/usr/local/etc/DMR_Hosts.txt", "/var/www/dashboard/config/language.php",
)


@dataclass(frozen=True)
class Entry:
    kind: str
    mode: str
    uid: str
    gid: str
    links: str
    link_target: str
    inode: str
    mtime: str
    ctime: str


@dataclass
class Snapshot:
    entries: dict
    hashes: dict
    configs: dict
    effects: bytes


def _read(container, *args):
    return check_output(["docker", "exec", container, "chroot", "/stand", *args], timeout=30)


def snapshot(container):
    """Read the isolated trees. Reads may update atime; never compare it."""
    raw = _read(container, "find", *ROOTS, "-printf",
                r"%p\0%y\0%m\0%U\0%G\0%n\0%l\0%D:%i\0%T@\0%C@\0")
    fields = raw.decode(errors="surrogateescape").split("\0")
    if fields.pop() or len(fields) % 10:
        raise AssertionError("configure: incomplete find inventory")
    entries = {fields[i]: Entry(*fields[i+1:i+10]) for i in range(0, len(fields), 10)}

    hashes = {}
    for record in _read(container, "find", *ROOTS, "-type", "f", "-exec",
                        "sha256sum", "-z", "--", "{}", "+").split(b"\0"):
        if record:
            digest, path = record.split(b"  ", 1)
            hashes[path.decode(errors="surrogateescape")] = digest.decode("ascii")
    if set(hashes) != {p for p, e in entries.items() if e.kind == "f"}:
        raise AssertionError("configure: file tree changed during snapshot")

    configs = {}
    names = [p[1:] for p in (*CALL_FILES, "/etc/dmrgateway", TZ_FILE, TZ_PHP, LANG_PHP, MODEM_MARKER) if p in hashes]
    if names:
        packet = _read(container, "tar", "-C", "/", "--no-recursion", "--hard-dereference",
                       "-cf", "-", "--", *names)
        with tarfile.open(fileobj=BytesIO(packet), mode="r:") as archive:
            for item in archive:
                path = "/" + item.name
                data = archive.extractfile(item).read()
                if sha256(data).hexdigest() != hashes[path]:
                    raise AssertionError(f"configure: {path} changed during snapshot")
                configs[path] = data
    return Snapshot(entries, hashes, configs, _read(container, "cat", "/work/effects"))


def _edit_lines(path, data, edits):
    """Substitute exact fixture lines at named sections, retaining every other byte."""
    seen = Counter()
    output = bytearray()
    section = b""
    for raw in data.splitlines(keepends=True):
        line = raw.rstrip(b"\r\n")
        if line.startswith(b"[") and line.endswith(b"]"):
            section = line[1:-1]
        slot = section, line
        if slot in edits:
            seen[slot] += 1
            output.extend(edits[slot] + raw[len(line):])
        else:
            output.extend(raw)
    for slot in edits:
        if seen[slot] != 1:
            key = slot[1].split(b"=", 1)[0]
            raise AssertionError(f"configure fixture: {path} [{slot[0]!r}] {key!r}: "
                                 f"expected one original line, found {seen[slot]}")
    return bytes(output)


def _verify_disk(before, after, wanted, links=None):
    links = links or {}
    problems = []
    paths = set(before.entries)
    delta = paths ^ set(after.entries)
    if delta:
        problems.append(f"added or removed paths: {sorted(delta)}")
    changed = {p for p in before.hashes.keys() | after.hashes.keys()
               if before.hashes.get(p) != after.hashes.get(p)}
    if changed != set(wanted):
        problems.append(f"changed wrong files: missing {sorted(wanted.keys()-changed)}, "
                        f"unexpected {sorted(changed-wanted.keys())}")
    for path, expected in wanted.items():
        if path not in after.configs:
            problems.append(f"missing saved file {path}")
            continue
        actual = after.configs[path]
        if expected != actual:
            # Do not print passwords or cookies even in a synthetic fixture.
            i = next((n for n, (x, y) in enumerate(zip(expected, actual)) if x != y),
                     min(len(expected), len(actual)))
            line = expected.count(b"\n", 0, i) + 1
            problems.append(f"{path} differs at byte {i}, expected line {line}")

    parents = {str(PurePosixPath(p).parent) for p in wanted}
    for path in sorted(paths.intersection(after.entries)):
        old, new = before.entries[path], after.entries[path]
        if path in wanted:
            if (new.kind, new.mode, new.uid, new.gid) != ("f", "644", "0", "0"):
                problems.append(f"saved {path} must be a root:root 0644 regular file")
            # install(1) can replace the inode for a changed regular file.
            new = replace(new, inode=old.inode, mtime=old.mtime, ctime=old.ctime)
        elif path in links:
            if (old.kind, old.link_target, new.kind, new.link_target) != ("l", links[path][0], "l", links[path][1]):
                problems.append(f"unexpected symlink transition for {path}")
            new = replace(new, link_target=old.link_target, inode=old.inode, mtime=old.mtime, ctime=old.ctime)
        elif path in parents:
            # A child installation may update directory bookkeeping.
            new = replace(new, mtime=old.mtime, ctime=old.ctime)
        if new != old:
            fields = [k for k in vars(old) if getattr(old, k) != getattr(new, k)]
            problems.append(f"{path}: changed {', '.join(fields)}")
    if problems:
        raise AssertionError("configure disk:\n  " + "\n  ".join(problems))


def _verify_effects(before, after, units, files):
    if not after.startswith(before):
        raise AssertionError("configure: effects trace was truncated or replaced")
    rows = []
    for line in after[len(before):].decode("ascii").splitlines():
        text = " ".join(line.split())
        if text.startswith("read "):
            text = text.removeprefix("read ")
            if not text.startswith("systemctl "):
                continue  # GET probes: versions, Wi-Fi status, picklists, etc.
        rows.append(text)

    # Pin multiplicities as well as the transaction boundaries. Diff only
    # command names/units/paths that can occur on this disposable stand.
    checks = [f"systemctl is-active --quiet {u}" for u in units]
    installs = [f"install {p}" for p in files]
    restarts = [f"systemctl restart {u}" for u in ACTIVE_UNITS]
    phases = [checks, ["mount -o remount,rw /"], installs, ["sync"],
              ["mount -o remount,ro /"], restarts]
    for phase in phases:
        received, rows = rows[:len(phase)], rows[len(phase):]
        if Counter(received) != Counter(phase):
            raise AssertionError(f"configure effects: expected {sorted(phase)}, got {sorted(received)}")
    if rows:
        raise AssertionError(f"configure effects: unexpected extra operations: {rows}")


def _verify_timezone_effects(before, after):
    assert after.startswith(before), "timezone trace was truncated"
    ops = [row.removeprefix("read ") for row in after[len(before):].decode().splitlines()
           if not row.startswith("read ") or row.startswith("read systemctl ")]
    services = Counter(row.split()[1] for row in ops if row.startswith("systemctl "))
    print("Service requests (modeled):", dict(sorted(services.items())), flush=True)
    expected = ["mount -o remount,rw /", "timedatectl set-timezone Europe/Berlin",
                "install "+TZ_FILE, "install "+TZ_PHP]
    end = ["mount -o remount,ro /"]
    if ops not in [expected+["sync"]*n+end for n in (1,3)]:
        raise AssertionError("timezone Apply made operations outside the expected timezone update; see effects trace")


def _verify_controller(container,before,target):
    assert target in ("DSTAR","MMDVM"), "invalid controller target"
    source, dest = ("dstarrepeater","mmdvmhost") if target=="MMDVM" else ("mmdvmhost","dstarrepeater")
    old, new = f"/etc/dstar-radio.{source}", f"/etc/dstar-radio.{dest}"
    keep = "/etc/dstar-radio.qa-keep"
    assert old in before.hashes and new not in before.entries and keep in before.hashes, "bad controller fixture"
    after = snapshot(container)
    issues = []
    marker_ok = (old not in after.entries and after.hashes.get(new) == before.hashes[old]
                 and (after.entries[new].kind, after.entries[new].mode, after.entries[new].uid, after.entries[new].gid)
                 == ("f","644","0","0"))
    print(f"Markers {old}/{new}: present/absent -> "
          f"{'present' if old in after.entries else 'absent'}/{'present' if new in after.entries else 'absent'}; "
          f"modem config transferred byte-for-byte, root:root 0644={marker_ok}",flush=True)
    if not marker_ok: issues.append("controller markers did not transfer cleanly")

    paths = (before.entries.keys() | after.entries.keys()) - {old,new}
    contents = sorted(p for p in paths if before.hashes.get(p) != after.hashes.get(p))
    metadata = sorted(p for p in paths if before.entries.get(p)!=after.entries.get(p) and p!="/etc")
    print(f"Other regular-file content differences (including removals): {contents}; "
          f"other metadata/path differences: {metadata}",flush=True)
    if contents or metadata: issues.append("collateral filesystem changes")
    for p in ("/etc/dmrgateway","/etc/mmdvmhost","/etc/dstarrepeater","/etc/wpa_supplicant/wpa_supplicant.conf",keep):
        print(f"{p}: " + ("missing" if p not in after.entries else
              "same bytes" if before.hashes[p]==after.hashes[p] else "different bytes"),flush=True)
    assert after.effects.startswith(before.effects)
    trace = after.effects[len(before.effects):].decode().splitlines()
    ops = [r for r in trace if r.startswith("systemctl ")]
    stop, start = f"systemctl stop {source}.service", f"systemctl start {dest}.service"
    move = f"mv -nT -- {old} {new}"
    if not (stop in trace and move in trace and start in trace and
            trace.index(stop)<trace.index(move)<trace.index(start)):
        issues.append("missing or misordered controller handoff")
    if marker_ok and before.entries[old].inode != after.entries[new].inode:
        issues.append("controller marker was not renamed in-place")
    print(f"Expected handoff: {stop} -> marker rename -> {start}; "
          f"actual stop/move/start trace indices: {[trace.index(r) if r in trace else None for r in (stop,move,start)]}; "
          f"service calls={dict(Counter(r.split()[1] for r in ops))}; "
           f"old controller start requested={('systemctl start '+source+'.service') in ops}; "
          f"UPnP disable requested={'systemctl disable pistar-upnp.timer' in ops}",flush=True)
    if ops != [stop,start]: issues.append("unexpected service sequence: "+", ".join(ops))
    modeled = _read(container,"cat","/work/controller-active").decode().strip()
    if modeled != dest+".service": issues.append("incorrect final modeled service state")
    print(f"Final active controller (fixture): {modeled}; same marker inode={marker_ok and before.entries[old].inode==after.entries[new].inode}",flush=True)
    if issues: raise AssertionError("; ".join(issues))


def _verify_language_effects(before, after):
    assert after.startswith(before), "language trace was truncated"
    services = [s.removeprefix("read ") for s in after[len(before):].decode().splitlines()
                if s.removeprefix("read ").startswith("systemctl ")]
    print(f"systemctl calls: {len(services)}; {dict(sorted(Counter(s.split()[1] for s in services).items()))}",flush=True)
    if services: raise AssertionError("dashboardLanguage must not invoke systemctl")


def _verify_first_controller(container,before,target):
    mmdvm, dstar = "/etc/dstar-radio.mmdvmhost", "/etc/dstar-radio.dstarrepeater"
    keep = "/etc/dstar-radio.qa-keep"
    selected, inactive, unit = {"DSTAR": (dstar,mmdvm,"dstarrepeater.service"),
                                "MMDVM": (mmdvm,dstar,"mmdvmhost.service")}[target]
    assert not {mmdvm,dstar} & before.entries.keys() and keep in before.hashes, "invalid initial marker fixture"
    after = snapshot(container)
    created = sorted(after.entries.keys()-before.entries.keys())
    removed = sorted(before.entries.keys()-after.entries.keys())
    issues = []
    print(f"Initial markers: neither present. Created paths: {created}; removed paths: {removed}",flush=True)
    marker = after.entries.get(selected)
    if not marker or (marker.kind,marker.mode,marker.uid,marker.gid)!=("f","644","0","0") or created!=[selected] or removed:
        issues.append("wrong marker type/owner/mode or unexpected path changes")
    if marker and marker.kind=="f":
        print(f"{selected}: {_read(container,'stat','-c','%s bytes, %U:%G %a',selected).decode().strip()}; "
              f"{inactive} present={inactive in after.entries}",flush=True)
    other = before.entries.keys() | after.entries.keys()
    contents = sorted(p for p in other-{selected} if before.hashes.get(p)!=after.hashes.get(p))
    metadata = sorted(p for p in other-{selected,"/etc"} if before.entries.get(p)!=after.entries.get(p))
    print(f"Other content differences: {contents}; other metadata/path differences: {metadata}",flush=True)
    print("DMRGateway byte-identical:", before.configs["/etc/dmrgateway"]==after.configs.get("/etc/dmrgateway"),
          "; qa-keep preserved:",before.hashes[keep]==after.hashes.get(keep),flush=True)
    if contents or metadata: issues.append("collateral configuration changes")
    assert after.effects.startswith(before.effects)
    trace = after.effects[len(before.effects):].decode().splitlines()
    ops = [row for row in trace if row.startswith("systemctl ")]
    print(f"Service requests: {dict(Counter(r.split()[1] for r in ops))}; "
          f"start D-Star={'systemctl start dstarrepeater.service' in ops}; "
          f"start MMDVM={'systemctl start mmdvmhost.service' in ops}",flush=True)
    start = "systemctl start "+unit
    if start not in trace or not any(row in ("touch "+selected,"install "+selected) for row in trace[:trace.index(start)]):
        issues.append(f"{unit} not started after marker creation")
    if ops != [start]:
        issues.append("expected exactly one activation: "+start)
    modeled = _read(container,"cat","/work/controller-active").decode().strip()
    print(f"Final active controller (fixture): {modeled}",flush=True)
    if modeled != unit: issues.append("wrong final active controller in fixture")
    if issues: raise AssertionError("; ".join(issues))


def _verify_modem(container,before):
    assert before.configs[MODEM_MARKER]==b"" and "/etc/dstar-radio.dstarrepeater" not in before.entries
    host="/etc/mmdvmhost"
    wanted={MODEM_MARKER:b"[Modem]\nHardware=zumspotusb\n\n",
            host:_edit_lines(host,before.configs[host],{(b"Modem",k+b"=/dev/ttyAMA0"):k+b"=/dev/ttyACM0"
                                                        for k in (b"Port",b"UARTPort")})}
    after=snapshot(container)
    print("Modem marker:",after.configs.get(MODEM_MARKER),flush=True)
    print("MMDVMHost matches original except [Modem] Port and UARTPort -> /dev/ttyACM0:",
          after.configs.get(host)==wanted[host],flush=True)
    changed=sorted(p for p in before.hashes.keys()|after.hashes.keys() if before.hashes.get(p)!=after.hashes.get(p))
    print("Regular files with changed contents:",changed,flush=True)
    assert after.effects.startswith(before.effects)
    ops=[r for r in after.effects[len(before.effects):].decode().splitlines() if r.startswith("systemctl ")]
    print("Service calls:",dict(sorted(Counter(r.split()[1] for r in ops).items())),
          "; D-Star start requested:", "systemctl start dstarrepeater.service" in ops,flush=True)
    issues=[]
    try: _verify_disk(before,after,wanted)
    except AssertionError as exc: issues.append(str(exc))
    service_trace = [r.removeprefix("read ") for r in after.effects[len(before.effects):].decode().splitlines()
                     if r.removeprefix("read ").startswith("systemctl ")]
    if service_trace != ["systemctl is-active --quiet mmdvmhost.service","systemctl restart mmdvmhost.service"]:
        issues.append("unexpected service operations for the modem edit (see trace)")
    if issues: raise AssertionError("; ".join(issues))


def verify(container, before, *, color=None, edit_call=True, location=False, timezone=False, controller=False, language=False, first_setup=False, modem=False):
    """Check the final GET's on-disk result against the independent before image."""
    if modem: return _verify_modem(container,before)
    if first_setup: return _verify_first_controller(container,before,controller)
    if controller: return _verify_controller(container,before,controller)
    missing = CALL_FILES.keys() - before.configs.keys()
    if missing:
        raise AssertionError(f"configure fixture lacks callsign targets: {sorted(missing)}")
    missing = set(SENTINELS) - before.hashes.keys()
    if missing:
        raise AssertionError(f"configure fixture lacks unrelated controls: {sorted(missing)}")
    edits = CALL_FILES.copy() if edit_call else {}
    links = {}
    if language:
        edits = {LANG_PHP: {(b"", b"$pistarLanguage='english_uk';"): b"$pistarLanguage='french_fr';"}}
    if timezone:
        edits = {TZ_FILE: {(b"", b"UTC"): b"Europe/Berlin"},
                 TZ_PHP: {(b"", b"date_default_timezone_set('UTC');"): b"date_default_timezone_set('Europe/Berlin');"}}
        links = {"/etc/localtime": ("/usr/share/zoneinfo/Etc/UTC", "/usr/share/zoneinfo/Europe/Berlin")}
    if location:
        # The fixture has no optional [Info] Name in m17gateway. DMR Network
        # Location flags and dmr2ysf's own Location are not town destinations.
        edits = {"/etc/"+n: {(b"Info", b'Location="Demo"'): b'Location="North Ridge"'}
                 for n in ("mmdvmhost","dmrgateway","ysf2dmr","ysf2nxdn","ysf2p25")}
        edits["/etc/nxdngateway"] = {(b"Info", b'Name="Demo"'): b'Name="North Ridge"'}
        edits["/etc/ircddbgateway"] = {(b"", k+b"=Demo"): k+b"=North Ridge"
                                       for k in (b"description1",b"description1_1")}
    if color is not None:
        edits["/etc/mmdvmhost"] = {**edits.get("/etc/mmdvmhost",{}),
                                   (b"DMR", b"ColorCode=3"): b"ColorCode="+color.encode("ascii")}
    wanted = {p: _edit_lines(p, before.configs[p], pairs) for p, pairs in edits.items()}
    after = snapshot(container)
    # A legacy submit can change collateral files AND restart unrelated units.
    # Keep both observations in a single failing browser run.
    errors = []
    if language:
        changed = sorted(p for p in before.hashes.keys() | after.hashes.keys() if before.hashes.get(p)!=after.hashes.get(p))
        print(f"Changed regular-file contents: {changed}; {LANG_PHP}: matches expected one-line edit="
              f"{after.configs.get(LANG_PHP)==wanted[LANG_PHP]}",flush=True)
    if timezone:
        for path in edits:
            print(f"{path}: expected timezone bytes match={after.configs[path] == wanted[path]}", flush=True)
        zone = after.entries["/etc/localtime"].link_target
        print(f"/etc/localtime -> {zone} (isolated timedatectl model)", flush=True)
        if _read(container,"cat","/etc/localtime") != _read(container,"cat","/usr/share/zoneinfo/Europe/Berlin"):
            errors.append("incorrect zoneinfo bytes")
        extra = sorted(p for p in before.hashes if p not in wanted and before.hashes[p]!=after.hashes.get(p))
        print(f"Unexpected content changes among {len(before.hashes)-len(wanted)} other regular files: {extra}",flush=True)
    if location:
        # Verify the actual fields even when bytewise comparisons elsewhere fail.
        for path, replacements in edits.items():
            try:
                _edit_lines(path, after.configs[path], {(sec,new):old for (sec,old),new in replacements.items()})
                print(f"Location confirmed: {path}: " + ", ".join(v.decode() for v in replacements.values()),flush=True)
            except AssertionError as exc:
                errors.append(str(exc))
        ops = [row.split()[1] for row in after.effects[len(before.effects):].decode().splitlines()
               if row.startswith("systemctl ")]
        print("Service requests (modeled):", dict(sorted(Counter(ops).items())), flush=True)
    if color is not None:
        dmr = after.configs["/etc/mmdvmhost"].split(b"[DMR]\n",1)[1].split(b"\n[",1)[0]
        print("/etc/mmdvmhost [DMR]:", [s.decode() for s in dmr.splitlines() if s.startswith(b"ColorCode=")],flush=True)
    if color is not None or location or timezone:
        path = "/etc/dmrgateway"
        def rewrites(snap):
            data = snap.configs[path].split(b"[DMR Network 1]\n",1)[1].split(b"\n[",1)[0]
            return [line.decode() for line in data.splitlines() if line.startswith(b"TGRewrite0=")]
        old, new = rewrites(before), rewrites(after)
        assert old == ["TGRewrite0=2,91,2,91,1", "TGRewrite0=2,93,2,93,1"], "invalid DMR fixture"
        ref = wanted.get(path, before.configs[path])
        print(f"DMR Network 1 TGRewrite0 before={old}, after={new}; /etc/dmrgateway "
              f"matches expected bytes={ref==after.configs[path]}", flush=True)
        if old != new: errors.append("Duplicate TGRewrite0 not preserved in original order")
    units = ({PurePosixPath(p).name+".service" for p in wanted} if location else
             CHECKED_UNITS if edit_call else {"mmdvmhost.service"})
    effects = ((_verify_language_effects, (before.effects, after.effects)) if language else
               (_verify_timezone_effects, (before.effects, after.effects)) if timezone else
               (_verify_effects, (before.effects, after.effects, units, wanted)))
    for check, args in ((_verify_disk, (before, after, wanted, links)), effects):
        try:
            check(*args)
        except AssertionError as exc:
            errors.append(str(exc))
    if errors:
        raise AssertionError("\n".join(errors))
    print(f"Configure: {sum(map(len, edits.values()))} exact line edits across "
          f"{len(wanted)} files; {len(before.entries)-len(wanted)-len(links)} other paths preserved. "
          f"Restarted: {'none' if timezone or language else ', '.join(sorted(ACTIVE_UNITS))}.")
