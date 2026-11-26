#!/usr/bin/python3 -I
"""Two-operation fixture connector. Only the shipped native helper writes service INIs."""
import json
import os
from pathlib import Path
import pwd
import re
import stat
import subprocess
import sys

helper='/usr/local/libexec/pistar-config'
host=Path('/etc/mmdvmhost')
gw=Path('/etc/dmrgateway')
work=Path('/work')
revision=work/'native-revision'
uid=pwd.getpwnam('www-data').pw_uid

def checked(value):
    if not value: raise ValueError('unsupported or stale edit')

def fields(data, section, pattern):
    current=None
    for i,line in enumerate(data.splitlines(keepends=True)):
        if line.startswith(b'['): current=line.strip()[1:-1]
        m=re.fullmatch(pattern,line)
        if current==section and m: yield i,m

def publish(path,data,mode):
    tmp=work/('view-'+path.name)
    tmp.write_bytes(data)
    os.chown(tmp,0,pwd.getpwnam('www-data').pw_gid)
    tmp.chmod(mode)
    os.replace(tmp,path)

def refresh(settings):
    # The complete classic form also needs old Pi-Star-specific sections.
    # In this fixture the native helper supplies ColorCode; Expert reads the
    # gateway's full physical text, including indexed rules and comments.
    raw=(work/'host-form-template').read_bytes().splitlines(keepends=True)
    [(i,_)]=fields(b''.join(raw),b'DMR',rb'ColorCode=(\d+)\n')
    raw[i]=f"ColorCode={settings['cc']}\n".encode()
    publish(host,b''.join(raw),0o644)
    publish(gw,Path('/etc/DMRGateway.ini').read_bytes(),0o640)
    revision.write_text(settings['revision'])

def main(args):
    s=json.loads(subprocess.check_output([helper,'services-read']))
    if args==['refresh']:
        refresh(s); return
    checked(len(args)==2 and revision.read_text()==s['revision'])
    src,dst=args
    checked(dst in (str(host),str(gw)) and re.fullmatch(r'/tmp/'+('pistar-cfg-' if dst==str(host) else 'pistar-edit-')+r'[A-Za-z0-9]{6}',src))
    with os.fdopen(os.open(src,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK),'rb') as f:
        st=os.fstat(f.fileno())
        checked(stat.S_ISREG(st.st_mode) and st.st_uid==uid and st.st_size<=1048576)
        candidate=f.read().splitlines(keepends=True)
    original=Path(dst).read_bytes().splitlines(keepends=True)
    checked(len(original)==len(candidate))
    [index]=[i for i,(a,b) in enumerate(zip(original,candidate)) if a!=b]
    if dst==str(host):
        [(i,m)]=fields(b''.join(candidate),b'DMR',rb'ColorCode=(\d{1,2})\n')
        checked(index==i)
        s['cc']=int(m[1])
    else:
        checked(b''.join(original)==Path('/etc/DMRGateway.ini').read_bytes())
        for n,(i,m) in enumerate(fields(b''.join(candidate),b'DMR Network 1',rb'(TGRewrite\d*=)([0-9,]+)\n')):
            if i==index:
                checked(original[i]==m[1]+s['rules'][n].encode()+b'\n')
                s['rules'][n]=m[2].decode()
                break
        else: raise ValueError('not a Network 1 TG rule')
    subprocess.run([helper,'services-apply'],input=json.dumps(s).encode(),stdout=subprocess.DEVNULL,check=True)
    refresh(json.loads(subprocess.check_output([helper,'services-read'])))
    with (work/'effects').open('a') as log: print('native services-apply',dst,file=log)

if __name__=='__main__': main(sys.argv[1:])
