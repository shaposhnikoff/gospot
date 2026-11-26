"""Drive TG edits through the stock form; compare wire fields and refreshed table."""
import json
import re
import subprocess
import sys
from pathlib import Path
from urllib.parse import parse_qs

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from full_ui import View
from smoke import browser, wait

origin, container, kind = sys.argv[1:]
assert kind in ('static','dynamic')
dynamic = kind=='dynamic'
slot = '2' if dynamic else '1'
submit_name = 'tgif'+kind.capitalize()+'Modify'
def read_fixture(path):
    return subprocess.check_output(['docker','--host','unix:///var/run/docker.sock','exec',container,'cat',path],text=True)
assert re.fullmatch(r'http://127\.0\.0\.1:\d+',origin) and re.fullmatch(r'pstar-operator-[0-9a-f]{12}',container)
with browser(target_origin=origin) as driver:
    v = View(driver,origin+'/admin/',1366,900,False)
    field = 'input[name=tgifStaticTalkgroup]'
    submit = f'input[name={submit_name}]'
    def row():
        return v.js("return all('#tgifStaticConnects td').map(e=>e.innerText.trim().replace(/\\s+/g,' '))")
    base = ['tgif.network','1234567','TG91(1)','TG99(2)']
    wait('missing initial TGIF data',lambda: row()==base)
    print('Initial table:',row(),flush=True)
    for action in ('ADD','DEL'):
        adding = action=='ADD'
        feedback = ('set' if adding else 'unlinked') if dynamic else ('added' if adding else 'removed')
        token = v.js('return document.querySelector(arguments[0]).form.elements.csrf_token.value',field)
        assert re.fullmatch(r'[0-9a-f]{64}',token)
        v.type_into(field,'777')
        v.click('#rbTgifTS'+slot)
        v.click('#rbTgifAdd' if action=='ADD' else '#rbTgifDel')
        v.events()
        v.click(submit)
        wait('missing TGIF Manager feedback',lambda: v.js('return document.body.innerText.includes(arguments[0])',kind.capitalize()+' Talkgroup '+feedback+'.'))
        reqs = [p['request'] for e in v.events() if e['method']=='Network.requestWillBeSent'
                and (p:=e['params'])['request']['method']=='POST']
        assert len(reqs)==1 and reqs[0]['url']==origin+'/admin/index.php'
        assert parse_qs(reqs[0]['postData'])=={'csrf_token':[token],submit_name:['Set '+kind.capitalize()],
            'tgifStaticSlot':[slot],'tgifStaticTalkgroup':['777'],'tgifStaticAction':[action]}
        assert v.js("return performance.getEntriesByType('navigation')[0].responseStatus")==200
        # Let the real AJAX refresh and the manager's delayed navigation complete.
        expected = base.copy()
        if dynamic: expected[3] = 'TG777(2)' if adding else 'None'
        elif adding: expected[2] = 'TG91(1) TG777(1)'
        wait('missing refreshed TGIF form/table',lambda: v.field(submit) and row()==expected, timeout=15)
        if dynamic:
            static_json=json.loads(read_fixture('/tmp/tgif-last-static.json'))
            dynamic_json=json.loads(read_fixture('/tmp/tgif-last-dynamic.json'))
            assert static_json=={'ok':True,'memberships':[{'slot':1,'talkgroup':91}]}
            assert dynamic_json=={'ok':True,'ts1_talkgroup':0,'ts2_talkgroup':777 if adding else 0}
            print(f'GET readback: static={json.dumps(static_json)}; dynamic={json.dumps(dynamic_json)}',flush=True)
        print(f'PASS {action}: Chromium POST /admin/index.php 200, form CSRF matched, '
              f'Set {kind.capitalize()}, form TG=777 TS{slot}; feedback="{feedback}"; refreshed table={row()}',flush=True)
    trace = read_fixture('/tmp/tgif-audit')
    writes = [l for l in trace.splitlines() if 'fixture: GET ' not in l]
    transactions = [('POST',777),('POST',4000)] if dynamic else [('POST',777),('DELETE',777)]
    assert writes==[f'Local HTTPS fixture: {method} /v1/{kind}-talkgroups/1234567 {{"slot":{slot},"talkgroup":{tg}}}, test bearer matched -> 200'
                    for method,tg in transactions], 'unexpected local API write history'
