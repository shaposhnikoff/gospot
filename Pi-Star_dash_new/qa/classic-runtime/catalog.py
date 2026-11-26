"""Inventory the actual installed catalog, then fetch reviewed local documents only."""
from collections import Counter, defaultdict
from html.parser import HTMLParser
from http.client import HTTPConnection
import json
from pathlib import Path
import re
from subprocess import check_output
import sys
from urllib.parse import urlsplit

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from full_ui import View
from smoke import browser, wait

origin, mode, container = sys.argv[1:]
host = urlsplit(origin)
assert host.hostname == "127.0.0.1" and re.fullmatch(r'pstar-operator-[0-9a-f]{12}', container)
with browser(target_origin=origin) as driver:
    v = View(driver, origin + "/operator/", 1366, 900, False)
    wait("integrated catalog missing", lambda: v.field('[data-testid="console-target"]'))
    items = v.js("""return all('.route-group li').map(e=>[e.dataset.testid.slice('console-item-'.length),
        e.querySelector('a')?.href ?? null])""")
assert len(items) == len(set(i for i,_ in items)) == 61
links = defaultdict(list)
for item, href in items:
    assert href is None or href.startswith(origin+'/')
    links[href[len(origin):] if href else None].append(item)

expected = {
    '/': ['N0RF' if mode=='mmdvmhost' else 'N0DSTAR','class="footer"'],
    '/api/last_heard.php': [],
    '/admin/': ['id="sysInfo"', 'N0RF' if mode=='mmdvmhost' else 'N0DSTAR'],
    '/admin/configure.php': ['id="config"', 'name="controllerSoft" value="'+('MMDVM' if mode=='mmdvmhost' else 'DSTAR')+'" checked'],
    '/admin/sysinfo.php': ['Pi-Star System Information','Binary','Version'],
    '/admin/live_modem_log.php': ['id="tail"','live_modem_log.php?ajax'],
    '/admin/admin.php': ['Service Status','pistar-keeper','name="RefName"'],
    '/admin/wifi.php?page=wlan0_info': ['ifconfig and iwconfig','wlan0_info'],
    '/admin/wifi.php?page=wpa_conf': ['id="wpa_conf_form"','name="wifiRevision"'],
    '/admin/config_backup.php': ['value="download"','value="restore"'],
    '/admin/power.php': ['value="reboot"','value="shutdown"'],
    '/admin/update.php': ['name="confirm_update"','action="/admin/update.php"'],
    '/admin/expert/upgrade.php': ['name="confirm_update"','action="/admin/expert/upgrade.php"'],
    '/admin/calibration.php': ['id="lblBaseFreq"','id="lblBER"','id="btnStart"'],
    '/admin/expert/': ['Expert Editors','edit_mmdvmhost.php'],
    '/admin/expert/ssh_access.php': ['SSH - Pi-Star','SSH Feature not yet installed'],
}
quick = {'mmdvmhost':'General[Callsign]', 'dstarrepeater':'dstarrepeater[callsign]',
         'ircddbgateway':'ircddbgateway[gatewayCallsign]', 'timeserver':'timeserver[callsign]',
         'starnetserver':'starnetserver[callsign]', 'dmrgateway':'data-key="RptPort"'}
for name in ('ysfgateway','p25gateway','nxdngateway','dapnetgateway'):
    quick[name]='General[Callsign]'
for name, field in quick.items():
    expected[f'/admin/expert/edit_{name}.php']=['<form','csrf_token',field]
for name, content in {'dmrgateway':'RptPort=62032','pistar-remote':'# QA pistar-remote','wpaconfig':'country=GB',
                      'cron':'# QA crontab','rssidat':'1 -100'}.items():
    expected[f'/admin/expert/fulledit_{name}.php']=['<textarea', content, 'csrf_token']
for name, field in {'apikeys':'name="apikeyTarget"','bmapikey':'key[apikey]',
                    'dapnetapi':'DAPNETAPI[USER]', 'tgifapikey':'key[apikey]'}.items():
    expected[f'/admin/expert/fulledit_{name}.php']=['<form', 'csrf_token', field]

conditional = {'Pages':'POCSAG disabled', 'pocsagFrequency':'POCSAG disabled', 'autoApPassForm':'no wlan device',
               'tgifStaticConnects':'no TGIF network/token'}
if mode=='mmdvmhost':
    conditional.update(cssConnects='D-Star network disabled',refLinks='D-Star network disabled')
else:
    conditional['bmConnects']='not an MMDVM controller'

class Anchors(HTMLParser):
    def __init__(self):
        super().__init__()
        self.targets=set()
    def handle_starttag(self, tag, attrs):
        for k,v in attrs:
            if k=='id' or (tag=='a' and k=='name'): self.targets.add(v)

def log():
    return check_output(['docker','--host','unix:///var/run/docker.sock','exec',container,'cat','/tmp/server.log']).decode()

counts=Counter()
pages={}
before=log()
print('result\tdestination\tcatalog_ids\tHTTP\texpected\tPHP diagnostics\treason')
for dest, ids in links.items():
    path, _, frag=(dest or '').partition('#')
    code='—'; diagnostics=[]; want=[]; result='PASS'; reason='matched'
    if path not in expected:
        result='SKIP' if not path else 'FAIL'
        reason='no link: jitter measurement starts on GET' if not path else 'not a reviewed read-only destination'
    else:
        if path not in pages:
            conn=HTTPConnection(host.hostname,host.port,timeout=8)
            try:
                conn.request('GET',path,headers={'User-Agent':'Desktop-fixture'})
                response=conn.getresponse()
                body=response.read().decode('utf-8',errors='replace')
                after=log()
                diagnostics=re.findall(r'PHP (?:Warning|Fatal error|Parse error|Deprecated|Notice):[^\n]*',after[len(before):])
                before=after
                anchors=Anchors(); anchors.feed(body)
                pages[path]=(response.status,body,anchors.targets,diagnostics,response.getheader('Content-Type',''))
            finally:
                conn.close()
        code,body,anchors,diagnostics,mime=pages[path]
        want=expected[path].copy()
        missing=[s for s in want if s not in body]
        if path == '/api/last_heard.php':
            calls=['N0RF','N0NET'] if mode=='mmdvmhost' else []
            want.append('JSON callsigns '+str(calls))
            try: valid='application/json' in mime and [r['callsign'] for r in json.loads(body)]==calls
            except (ValueError,KeyError,TypeError): valid=False
            if not valid: missing.append(want[-1])
        elif 'text/html' not in mime: missing.append('text/html')
        if frag: want.append('anchor #'+frag)
        if code!=200 or diagnostics or missing:
            result='FAIL'; reason=f'HTTP {code}; missing={missing}' if missing or code!=200 else 'PHP diagnostics'
        elif frag and frag not in anchors:
            result='SKIP' if frag in conditional else 'FAIL'
            reason=conditional.get(frag,'missing anchor')
    print('\t'.join(map(str,[result,dest or '(no URL)',','.join(ids),code,'; '.join(want) or '—',
                             ' | '.join(diagnostics) or ('none' if path in pages else '—'),reason])),flush=True)
    counts[result]+=1
print(f'{mode}: {len(items)} items, {len(pages)} distinct HTTP GETs; '+str(dict(counts)))
sys.exit(bool(counts['FAIL']))
