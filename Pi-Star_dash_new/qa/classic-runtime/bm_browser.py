"""BM groups: browser form -> HTTPS mutation -> profile after timed GET."""
import json
from configparser import ConfigParser
import re
import subprocess
import sys
from pathlib import Path
from urllib.parse import parse_qs

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from full_ui import View
from smoke import browser, wait

origin, container, scenario = sys.argv[1:]
fail_qso = scenario=='drop-qso-503'
simplex = scenario.endswith('-simplex')
kind = 'drop-qso' if fail_qso else scenario.removesuffix('-simplex')
form_slot = '2' if simplex else '1'
api_slot = '0' if simplex else '1'
assert kind in ('static','drop-dynamic','drop-qso')
assert re.fullmatch(r'http://127\.0\.0\.1:\d+',origin) and re.fullmatch(r'pstar-operator-[0-9a-f]{12}',container)
url = origin+'/admin/index.php'
api = '/v2/device/1234567'
form = 'form:has(input[name=tgSubmit]) '
baseline = ['BM QA','1234567','TG91(2)','None']
if simplex:
    baseline[2:] = ['TG91','TG701 TG702' if kind=='drop-dynamic' else 'TG701']
elif kind!='static':
    baseline[2:] = ['TG91(2) TG92(1)','TG701(1) TG801(2) TG702(1)']
expected_writes = []

def read_fixture(path):
    return subprocess.check_output(['docker','--host','unix:///var/run/docker.sock','exec',container,'cat',path],text=True,timeout=10)

def audit():
    return [json.loads(line) for line in read_fixture('/tmp/bm-audit').splitlines()]

def check_routes(stage, expected):
    routes = json.loads(read_fixture('/tmp/bm-routes.json'))
    assert routes==expected, (stage,routes)
    print(f'Fixture active call routes {stage}: {routes}',flush=True)

with browser(target_origin=origin) as driver:
    if simplex:
        cfg = ConfigParser()
        cfg.read_string(read_fixture('/etc/mmdvmhost'))
        assert cfg['General']['Duplex']=='0' and cfg['DMR Network']['Slot1']=='0' and cfg['DMR Network']['Slot2']=='1'
        print('Mounted /etc/mmdvmhost: Duplex=0 Slot1=0 Slot2=1',flush=True)
    v = View(driver,url,1366,900,False)
    def row():
        return v.js("return all('#bmConnects td').map(e=>e.innerText.trim().replace(/\\s+/g,' '))")
    wait('missing BM baseline',lambda: row()==baseline)
    print(f'Initial profile: {row()}',flush=True)
    if kind=='drop-qso':
        initial_routes = {'0':'QA-CALL-000'} if simplex else {'1':'QA-CALL-101','2':'QA-CALL-202'}
        expected_routes = initial_routes.copy()
        if not fail_qso: del expected_routes[api_slot]
        check_routes('before submit',initial_routes)
    for action in {'static':('ADD','DEL'),'drop-dynamic':('DROP',),'drop-qso':('QSO',)}[kind]:
        token = v.js('return document.querySelector(arguments[0]).form.elements.csrf_token.value',form+'input[name=tgNr]')
        assert re.fullmatch(r'[0-9a-f]{64}',token)
        v.click(form+f'input[name=TS][value="{form_slot}"]')
        fields = {'csrf_token':[token],'TS':[form_slot]}
        if action in ('DROP','QSO'):
            submit, caption, endpoint = ('dropDyn','Drop All Dynamic','dropDynamicGroups') if action=='DROP' else ('dropQso','Drop QSO','dropCallRoute')
            fields.update(tgNr=[''],TGmgr=['ADD'])
            fields[submit] = [caption]
        else:
            submit = 'tgSubmit'
            v.type_into(form+'input[name=tgNr]','777')
            v.click(form+f'input[name=TGmgr][value="{action}"]')
            fields.update(tgNr=['777'],TGmgr=[action],tgSubmit=['Set Static'])
        v.events()
        v.click(form+f'input[name={submit}]')
        message = 'BrandMeister APIv2: '+('No Response' if fail_qso else 'OK')
        wait('missing '+message,lambda: v.js('return all("td").some(e=>e.innerText.trim()===arguments[0])',message))
        posts = [r for e in v.events() if e['method']=='Network.requestWillBeSent' and (r:=e['params']['request'])['method']=='POST']
        assert len(posts)==1 and posts[0]['url']==url
        assert parse_qs(posts[0]['postData'],keep_blank_values=True)==fields
        assert v.js("return performance.getEntriesByType('navigation')[0].responseStatus")==200
        adding = action=='ADD'
        if action in ('DROP','QSO'):
            expected_writes.append(dict(method='GET',path=f'{api}/action/{endpoint}/{api_slot}',body=None,auth=True,status=503 if fail_qso else 200))
        else:
            expected_writes.append(dict(method='POST' if adding else 'DELETE',path=api+('/talkgroup/' if adding else f'/talkgroup/{api_slot}/777'),
                                        body={'slot':api_slot,'group':'777'} if adding else None,auth=True,status=200))
        profile_read = dict(method='GET',path=api+'/profile',body=None,auth=False,status=200)
        before_reload = audit()
        assert [r for r in before_reload if r!=profile_read]==expected_writes
        if action=='QSO':
            check_routes('after command',expected_routes)
        expected = baseline.copy()
        if adding: expected[2] += ' TG777' if simplex else ' TG777(1)'
        if action=='DROP': expected[3] = 'None' if simplex else 'TG801(2)'
        wait('BM form/profile did not refresh',lambda: v.field(form+'input[name=tgSubmit]') and row()==expected,timeout=15)
        after = audit()
        assert after[:len(before_reload)]==before_reload and after[len(before_reload):]
        assert all(r==profile_read for r in after[len(before_reload):])
        if action=='QSO':
            check_routes('after reload',expected_routes)
        label = (caption if action in ('DROP','QSO') else action+' TG777') + f' form TS={form_slot}, API slot={api_slot}'
        print(f'PASS {label}: one browser POST 200 with original CSRF; {message}; '
              f'refreshed row={row()}; total action requests still {len(expected_writes)}',flush=True)
