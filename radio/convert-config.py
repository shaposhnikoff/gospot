#!/usr/bin/python3 -I
"""Convert upstream MMDVM-Host.ini / DMRGateway.ini into Pi-Star PS4 configs.

Runs on the board as root (radio/migrate-board pipes it over ssh). Every other
line of the source files is kept verbatim; only these changes are made:
  * daemon mode and file logging in the Pi-Star log directory, no MQTT;
  * [DMR Network] gets Type/RemoteAddress/RemotePort (PS4 MMDVMHost) and the
    Address/Port keys Pi-Star scripts and dashboards read;
  * [Info] carries the frequencies, as in Pi-Star;
  * ID lookup files and RSSI mapping point at the Pi-Star data directory.
Usage: convert-config.py SRC_HOST SRC_GW DST_HOST DST_GW LOG_DIR HOSTS_DIR
"""
import os
import sys
import tempfile


class Ini:
    def __init__(self, text):
        self.lines = text.splitlines()

    def _section(self, name):
        start = None
        for i, line in enumerate(self.lines):
            s = line.strip()
            if s.startswith("[") and s.endswith("]"):
                if start is not None:
                    return start, i
                if s[1:-1] == name:
                    start = i
        return (start, len(self.lines)) if start is not None else (None, None)

    def get(self, section, key):
        start, end = self._section(section)
        if start is None:
            return None
        for line in self.lines[start + 1:end]:
            k, sep, v = line.partition("=")
            if sep and k.strip() == key and not line.lstrip().startswith(("#", ";")):
                return v.strip()
        return None

    def set(self, section, key, value):
        start, end = self._section(section)
        if start is None:
            if self.lines and self.lines[-1].strip():
                self.lines.append("")
            self.lines += [f"[{section}]", f"{key}={value}"]
            return
        for i in range(start + 1, end):
            line = self.lines[i]
            k, sep, _ = line.partition("=")
            if sep and k.strip() == key and not line.lstrip().startswith(("#", ";")):
                self.lines[i] = f"{key}={value}"
                return
        # Insert after the last non-blank line of the section.
        at = end
        while at > start + 1 and not self.lines[at - 1].strip():
            at -= 1
        self.lines.insert(at, f"{key}={value}")

    def drop_key(self, section, key):
        start, end = self._section(section)
        if start is None:
            return
        self.lines[start + 1:end] = [l for l in self.lines[start + 1:end]
                                     if l.partition("=")[0].strip() != key or l.lstrip().startswith(("#", ";"))]

    def drop_section(self, section):
        start, end = self._section(section)
        if start is not None:
            del self.lines[start:end]

    def text(self):
        return "\n".join(self.lines) + "\n"


def logging(ini, root, log_dir):
    ini.drop_key("Log", "MQTTLevel")
    ini.drop_section("MQTT")
    ini.set("Log", "DisplayLevel", "0")
    ini.set("Log", "FileLevel", "1")
    ini.set("Log", "FilePath", log_dir)
    ini.set("Log", "FileRoot", root)
    ini.set("Log", "FileRotate", "1")
    ini.set("General", "Daemon", "1")


def host(text, log_dir, hosts_dir):
    ini = Ini(text)
    logging(ini, "MMDVM", log_dir)
    for key in ("RXFrequency", "TXFrequency"):
        if ini.get("Info", key) is None and ini.get("Modem", key):
            ini.set("Info", key, ini.get("Modem", key))
    net = "DMR Network"
    if ini.get(net, "Enable") is not None:
        address = ini.get(net, "RemoteAddress") or ini.get(net, "GatewayAddress") or ini.get(net, "Address")
        port = ini.get(net, "RemotePort") or ini.get(net, "GatewayPort") or ini.get(net, "Port")
        if not address or not port:
            raise SystemExit("[DMR Network] has no gateway/master address")
        gateway = address == "127.0.0.1" and port == "62031"
        ini.set(net, "Type", "Gateway" if gateway else "Direct")
        for key, value in (("RemoteAddress", address), ("RemotePort", port), ("Address", address), ("Port", port)):
            ini.set(net, key, value)
    ini.set("DMR Id Lookup", "File", os.path.join(hosts_dir, "DMRIds.dat"))
    ini.set("NXDN Id Lookup", "File", os.path.join(hosts_dir, "NXDN.csv"))
    if ini.get("Modem", "RSSIMappingFile") is not None:
        ini.set("Modem", "RSSIMappingFile", os.path.join(hosts_dir, "RSSI.dat"))
    return ini.text()


def gateway(text, log_dir):
    ini = Ini(text)
    logging(ini, "DMRGateway", log_dir)
    return ini.text()


def write(path, data, like):
    st = os.stat(like)
    fd, tmp = tempfile.mkstemp(dir=os.path.dirname(path), prefix=".gospot-")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
        os.chown(tmp, 0, 0)
        os.chmod(tmp, 0o644 if st.st_mode & 0o004 else 0o640)
        os.replace(tmp, path)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)


def main():
    src_host, src_gw, dst_host, dst_gw, log_dir, hosts_dir = sys.argv[1:7]
    for dst in (dst_host, dst_gw):
        if os.path.exists(dst):
            raise SystemExit(f"{dst} already exists; not overwriting")
    with open(src_host, encoding="utf-8") as f:
        new_host = host(f.read(), log_dir, hosts_dir)
    with open(src_gw, encoding="utf-8") as f:
        new_gw = gateway(f.read(), log_dir)
    write(dst_host, new_host, src_host)
    write(dst_gw, new_gw, src_gw)
    print(f"wrote {dst_host} and {dst_gw}")


if __name__ == "__main__":
    main()
