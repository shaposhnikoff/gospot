"""One station, two stock forms, independent reads of both daemon input files."""
import base64
import json
import os
import re
import subprocess
import sys
from hashlib import sha256
from pathlib import Path
from urllib.parse import parse_qsl

qa=Path(__file__).resolve().parents[2]
sys.path[:0]=[str(qa),str(qa/'classic-write')]
from smoke import browser
from pilot_menu import enter, stage
from expert import navigation
import configure

origin,name,credential,_,_=sys.argv[1:]
assert re.fullmatch(r'https://127\.0\.0\.1:\d+',origin) and re.fullmatch(r'pstar-classic-write-[a-f0-9]+',name)
files=['/etc/MMDVM-Host.ini','/etc/DMRGateway.ini']
def run(*args,radio=False):
    cmd=['docker','exec',name,'chroot']
    if radio: cmd+=['--userspec=mmdvm:mmdvm']
    return subprocess.check_output(cmd+['/stand',*args])
def compare(label,want):
    for p,b in zip(files,want):
        actual=run('cat',p,radio=True)
        assert actual==b, (label,p,'unexpected bytes')
        print(f'{label}: {p} byte-exact sha256={sha256(actual).hexdigest()} {run("stat","-c","%U:%G %a",p).decode().strip()}',flush=True)
def replace(b,old,new):
    assert b.count(old)==1
    return b.replace(old,new)
original=[run('cat',p,radio=True) for p in files]
compare('Before',original)
host=replace(original[0],b'\nColorCode=1\n',b'\nColorCode=12\n')
gw=replace(original[1],b'\nTGRewrite0=2,91,2,91,1\n',b'\nTGRewrite0=2,91,2,310,1\n')
assert b'TGRewrite1=2,92,2,92,1' in gw
root=Path(os.environ['QA_MENU_ROOT'])
with stage(root.parent/'staging/server.py',root/'operator') as station, browser(target_origin=origin,tls_pin=os.environ['QA_MENU_PIN']) as driver:
    auth=base64.b64encode(b'qa:'+Path(credential).read_bytes().strip()).decode()
    for cmd,params in [('Network.enable',{}),('Network.setCacheDisabled',{'cacheDisabled':True}),('Network.setExtraHTTPHeaders',{'headers':{'Authorization':'Basic '+auth}})]:
        driver('/goog/cdp/execute',{'cmd':cmd,'params':params})
    def menu(which): return enter(driver,station,origin,which,1366)
    def editor():
        v=menu('expert')
        v.click('a[href="edit_dmrgateway.php"]')
        v.url+='edit_dmrgateway.php'
        navigation(v,'GET')
        return v
    select='input[data-section="DMR Network 1"][data-key="TGRewrite0"]'
    configure.run(driver,origin,name,color='12',initial_color='1',edit_call=False,view=menu('configuration'))
    assert menu('configuration').js('return document.querySelector(arguments[0]).value',configure.COLOR)=='12'
    compare('After Configuration + reopen',[host,original[1]])
    v=editor()
    assert v.js('return document.querySelector(arguments[0]).value',select)=='2,91,2,91,1'
    form=v.js('return Array.from(new FormData(document.querySelector(arguments[0]).form))',select)
    field=v.js('return document.querySelector(arguments[0]).name',select)
    v.type_into(select,'2,91,2,310,1')
    v.click('form[method=post] input[type=submit]')
    req=navigation(v,'POST')
    assert parse_qsl(req['postData'],keep_blank_values=True)==[(k,'2,91,2,310,1' if k==field else x) for k,x in form]
    assert editor().js('return document.querySelector(arguments[0]).value',select)=='2,91,2,310,1'
    compare('After Expert + reopen',[host,gw])
flow=[json.loads(l) for l in run('cat','/tmp/qa-flow').splitlines()]
for path,status in [('/admin/configure.php',303),('/admin/expert/edit_dmrgateway.php',200)]:
    events=[r for r in flow if r[0]==path]
    [post]=[r for r in events if r[1]=='POST']
    assert post[2]==status and post[4] and events[0][1:3]==events[-1][1:3]==['GET',200]
    assert len({r[3] for r in events})==1
    print(f'PASS {path}: original CSRF, one POST {status}, reopened by menu in same session')
assert run('stat','-c','%U:%G %a',*files).splitlines()==[b'mmdvm:mmdvm 644',b'mmdvm:mmdvm 600']
effects=run('cat','/work/effects').decode().splitlines()
assert [l for l in effects if l.startswith('native-helper-systemctl')]==[
    'native-helper-systemctl restart mmdvmhost.service','native-helper-systemctl restart dmrgateway.service']
print('PASS service-account reads and both full-file comparisons. Native helper restricts DMRGateway.ini 0644 → 0600 on first apply. Restarts modeled.')
