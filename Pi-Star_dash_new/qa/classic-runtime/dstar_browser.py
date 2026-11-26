"""Exercise the inline D-Star Link Manager and inspect its recorded command, never a live reflector."""
from pathlib import Path
import re
import subprocess
import sys
from urllib.parse import parse_qs

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from full_ui import ELEMENT, View
from smoke import browser, wait

origin, container = sys.argv[1:]
assert re.fullmatch(r'http://127\.0\.0\.1:\d+',origin) and re.fullmatch(r'pstar-operator-[0-9a-f]{12}',container)
form = "form:has(input[name=dstrMgrSubmit])"
with browser(target_origin=origin) as driver:
    v = View(driver,origin+'/admin/',1366,900,False)
    expected = []
    for verb in ('LINK','UNLINK'):
        wait('missing Link Manager',lambda: v.field(form))
        for name, value in [('Module','N0DEMO C'),('RefName','DCS023'),('Letter','Q')]:
            option = v.js("return Array.from(document.querySelector(arguments[0]).options).find(o=>o.value===arguments[1])",
                          form+f' select[name={name}]',value)
            driver(f'/element/{option[ELEMENT]}/click',{})
        v.click(form+f' input[name=Link][value={verb}]')
        token = v.js('return document.querySelector(arguments[0]).elements.csrf_token.value',form)
        assert re.fullmatch(r'[0-9a-f]{64}',token)
        v.events()
        v.click(form+' input[name=dstrMgrSubmit]')
        cmd = 'sudo remotecontrold "N0DEMO C" '+('link never "DCS023 Q"' if verb=='LINK' else 'unlink')
        wait('missing command receipt',lambda: v.js('return Array.from(document.querySelectorAll("td")).some(c=>c.innerText.trim()===arguments[0])','QA recorded: '+cmd))
        reqs = [r for e in v.events() if e['method']=='Network.requestWillBeSent'
                and (r:=e['params']['request'])['method']=='POST']
        assert len(reqs)==1 and reqs[0]['url']==origin+'/admin/index.php'
        assert parse_qs(reqs[0]['postData'])=={'Module':['N0DEMO C'],'RefName':['DCS023'],'Letter':['Q'],
                'Link':[verb],'dstrMgrSubmit':['Request Change'],'csrf_token':[token]}
        assert v.js("return performance.getEntriesByType('navigation')[0].responseStatus")==200
        expected.append(cmd)
        actual = subprocess.check_output(['docker','--host','unix:///var/run/docker.sock','exec',container,'cat','/tmp/remotecontrold-audit'],text=True)
        assert actual.splitlines()==expected
        print(f'PASS {verb}: one POST /admin/index.php, CSRF matched, HTTP 200; Module=N0DEMO C, RefName=DCS023, Letter=Q; Command Output and stub log: {cmd}',flush=True)
