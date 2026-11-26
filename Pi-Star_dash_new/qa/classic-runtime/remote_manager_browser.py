"""Drive packaged link managers and verify each command at the stub boundary."""
import configparser
import json
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import parse_qs

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from full_ui import ELEMENT, View, equal
from smoke import browser, wait

origin, container, manager = sys.argv[1:]
assert re.fullmatch(r'http://127\.0\.0\.1:\d+', origin) and re.fullmatch(r'pstar-operator-[a-f0-9]{12}', container)
def read(path):
    return subprocess.check_output(['docker','--host','unix:///var/run/docker.sock','exec',container,'cat',path],text=True,timeout=10)
def config(path):
    cfg = configparser.ConfigParser()
    cfg.read_string(read(path))
    return cfg
def audit():
    return [json.loads(line) for line in read('/tmp/remote-command-audit').splitlines()]

section, fixture_port, host, label, link, unlink = {
    'ysf': ('System Fusion Network','16073','YSF54321','QA-YSF',['LinkYSF','54321'],['UnLink']),
    'p25': ('P25 Network','16074','31415','127.0.0.1',['TalkGroup','31415'],['TalkGroup','9999']),
    'nxdn': ('NXDN Network','16075','23456','127.0.0.1',['TalkGroup','23456'],['TalkGroup','unlink']),
    'm17': ('M17 Network','16076','M17-QAT','M17-QAT',['Reflector','M17-QAT','Q'],['Reflector','unlink']),
}[manager]
assert config('/etc/mmdvmhost')[section]['Enable']=='1'
remote = config(f'/etc/{manager}gateway')['Remote Commands']
port = remote['Port']
assert remote['Enable']=='1' and port==fixture_port
print(f'Fixture: {section} enabled; {manager.upper()}Gateway Remote Commands enabled, port={port}',flush=True)
url = origin+'/admin/index.php'
submit = manager+'MgrSubmit'
host_field = manager+'LinkHost'
form = f'form:has(input[name={submit}])'
expected = []
with browser(target_origin=origin) as driver:
    v = View(driver,url,1366,900,False)
    for action in ('LINK','UNLINK'):
        wait('missing '+manager+' manager',lambda:v.field(form))
        option = v.js('return document.querySelector(arguments[0])',form+f' select[name={host_field}] option[value="{host}"]')
        assert option and v.js('return arguments[0].textContent.includes(arguments[1])',option,label)
        driver(f'/element/{option[ELEMENT]}/click',{})
        room_fields = {}
        if manager == 'm17':
            room = v.js('return Array.from(document.querySelector(arguments[0]).options).find(o=>o.value==="Q")',
                        form+' select[name=m17LinkRoom]')
            assert room
            driver(f'/element/{room[ELEMENT]}/click',{})
            room_fields['m17LinkRoom']=['Q']
        v.click(form+f' input[name=Link][value={action}]')
        token = v.js('return document.querySelector(arguments[0]).elements.csrf_token.value',form)
        assert re.fullmatch(r'[0-9a-f]{64}',token)
        equal('command history before submit',audit(),expected)
        v.events()
        v.click(form+f' input[name={submit}]')
        args = [port, *(link if action=='LINK' else unlink)]
        receipt = {'cwd':'/var/log/pi-star','argv':['/usr/local/bin/RemoteCommand',*args]}
        expected.append(receipt)
        text = 'QA recorded: '+json.dumps(receipt,separators=(',',':'))
        wait('missing '+manager+' Command Output',lambda:v.js(
            'return Array.from(document.querySelectorAll("td")).some(c=>c.innerText.trim()===arguments[0])',text))
        requests = [r for e in v.events() if e['method']=='Network.requestWillBeSent'
                    and (r:=e['params']['request'])['method']=='POST']
        assert len(requests)==1 and requests[0]['url']==url
        assert parse_qs(requests[0]['postData'])=={'csrf_token':[token],host_field:[host],**room_fields,
                'Link':[action],submit:['Request Change']}
        equal('document status',v.js("return performance.getEntriesByType('navigation')[0].responseStatus"),200)
        equal('one new stub invocation',audit(),expected)
        selection = f'host={host}' + (', room=Q' if room_fields else '')
        print(f'PASS {action}: one POST /admin/index.php 200 with original CSRF, {selection}; '
              f'Command Output matches stub argv: RemoteCommand {" ".join(args)}',flush=True)
        wait('manager did not return after timed reload',lambda:v.field(form))
        equal('reload issued no additional command',audit(),expected)
        print(f'PASS {action}: form returned after automatic reload; total RemoteCommand calls still {len(expected)}',flush=True)
