"""The container has no host system bus. Only two generated QA unit names are reachable."""
import os
from pathlib import Path
import re
import socketserver
import subprocess
import sys

directory, prefix = Path(sys.argv[1]), sys.argv[2]
assert re.fullmatch(r"pstar-write-qa-[a-z0-9]+", prefix)

class Bridge(socketserver.StreamRequestHandler):
    def handle(self):
        self.connection.settimeout(4)
        message=self.rfile.readline(80).decode()
        match=re.fullmatch(r"(dmrgateway|mmdvmhost)\.service (ok|fail)\n",message)
        if not match: return
        role, expected=match.groups()
        unit=f"{prefix}-{role}.service"
        fault=directory.parent / ("fault-"+role)
        if expected=="fail": fault.touch()
        try:
            proc=subprocess.run(["systemctl","--user","restart",unit], timeout=8)
            info=subprocess.check_output(["systemctl","--user","show",unit,"-p","ExecMainStatus","-p","ActiveState","-p","Result"],text=True)
            print(unit,info.replace("\n"," ").strip(),flush=True)
            if proc.returncode==0 and "ActiveState=active\n" in info and "ExecMainStatus=0\n" in info:
                self.wfile.write(b"ok\n")
            elif proc.returncode and "ActiveState=failed\n" in info and "ExecMainStatus=42\n" in info:
                self.wfile.write(b"fail\n")
        finally:
            fault.unlink(missing_ok=True)

os.umask(0o077)
with socketserver.UnixStreamServer(str(directory/"control"),Bridge) as server:
    server.serve_forever()
