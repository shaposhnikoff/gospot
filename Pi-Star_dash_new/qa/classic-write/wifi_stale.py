"""Exercise two independently loaded Wi-Fi forms in one Chromium/PHP session."""
from hashlib import sha256
import json
from subprocess import check_output
from urllib.parse import parse_qs

from full_ui import ELEMENT, View
from smoke import wait
from wifi import CONF


def run(driver, origin, container, *, auth):
    path = '/admin/wifi.php?page=wpa_conf'
    url = origin + path
    def inspect(*args):
        return check_output(['docker','exec',container,*args], timeout=10)

    before = inspect('cat',CONF)
    assert before.count(b'\ncountry=GB\n')==1
    expected = before.replace(b'\ncountry=GB\n', b'\ncountry=DE\n')
    revision = sha256(before).hexdigest()
    updated = sha256(expected).hexdigest()
    def metadata():
        return inspect('stat','-c','%u:%g:%a %i %y %z',CONF)
    def fields(v):
        return v.js("""const f=document.getElementById('wpa_conf_form').elements;
                       return [f.wifiRevision.value,f.wifiCountryCode.value,f.csrf_token.value]""")

    first = View(driver,url,1366,900,False)
    first_id = first.cdp('Target.getTargetInfo',{})['targetInfo']['targetId']
    second_id = driver('/window/new',{'type':'tab'})['handle']
    assert first_id != second_id
    driver('/window',{'handle':second_id})
    first.cdp('Network.enable',{})
    first.cdp('Network.setCacheDisabled',{'cacheDisabled':True})
    first.cdp('Network.setExtraHTTPHeaders',{'headers':{'Authorization':'Basic '+auth}})
    second = View(driver,url,1366,900,False)
    token = fields(second)[2]
    original = [revision,'GB',token]
    assert token and fields(second)==original
    driver('/window',{'handle':first_id})
    assert fields(first)==original
    print(f'Two Chromium tabs: same initial wifiRevision={revision}, country=GB, same CSRF',flush=True)

    def save(v, country):
        option = v.js("return document.querySelector(arguments[0])",
                      f'#wpa_conf_form select[name=wifiCountryCode] option[value="{country}"]')
        driver(f'/element/{option[ELEMENT]}/click',{})
        assert fields(v)==[revision,country,token]
        v.events()
        v.click('input[name=SaveWPAPSKSettings]')
        events = []
        def got_post_response():
            events.extend(v.events())
            requests = [e['params'] for e in events if e['method']=='Network.requestWillBeSent'
                        and e['params']['request']['url']==url and e['params']['request']['method']=='POST']
            if not requests: return False
            assert len(requests)==1
            req = requests[0]
            data = parse_qs(req['request']['postData'])
            assert data['wifiRevision']==[revision] and data['wifiCountryCode']==[country] and data['csrf_token']==[token]
            return [e['params']['response']['status'] for e in events if e['method']=='Network.responseReceived'
                    and e['params']['requestId']==req['requestId']]
        assert wait('missing Wi-Fi save response',got_post_response)==[200]

    save(first,'DE')
    assert inspect('cat',CONF)==expected
    stat = metadata()
    assert stat.startswith(b'0:0:600 ')
    effects = inspect('cat','/work/effects')
    assert effects.splitlines().count(('install '+CONF).encode())==1
    print(f'PASS tab 1: POST 200; disk exactly GB -> DE, SHA256={updated}; installs=1',flush=True)

    driver('/window',{'handle':second_id})
    assert fields(second)==original, 'second tab did not retain its original form'
    save(second,'FR')
    message = 'WiFi configuration NOT saved. Configuration changed. Reload before saving.'
    wait('missing visible stale-revision rejection',lambda: second.js('return document.body.innerText.includes(arguments[0])',message))
    assert inspect('cat',CONF)==expected and metadata()==stat
    assert inspect('cat','/work/effects')==effects, 'stale form performed privileged operations'
    driver('/url',{'url':url})
    assert fields(second)==[updated,'DE',token]
    receipts = [r for line in inspect('cat','/tmp/qa-flow').splitlines()
                if (r:=json.loads(line))[0]==path]
    assert [r[1:3] for r in receipts]==[['GET',200],['GET',200],['POST',200],['POST',200],['GET',200]]
    assert len({r[3] for r in receipts})==1 and all(r[4] for r in receipts if r[1]=='POST')
    print(f'PASS tab 2: stale GB -> FR, POST 200, both session CSRF checks valid; "{message}"',flush=True)
    print(f'PASS fresh GET: country=DE, wifiRevision={updated}; byte-identical to first save, '
          'root:root 0600, inode/mtime/ctime unchanged; additional installs/remounts/interface operations=0',flush=True)
