"""Confirm the real Update/Upgrade forms, with only sudo replaced by a recorder."""
import json
import secrets
import subprocess
from urllib.parse import parse_qs, urlsplit

from full_ui import View
from smoke import wait


def verify_live_tail(view, container, action):
    """Read the stock poller's own responses; don't issue any extra HTTP requests."""
    ajax = view.url + '?ajax'
    marker = f'QA-{action}-{secrets.token_hex(8)}'
    logfile = f'/var/log/pi-star/pi-star_{action}.log'
    baseline = view.js("return document.querySelector('#tail').innerText.trim()")
    subprocess.run(['docker','exec','-i',container,'tee','-a',logfile],
                   input=(marker+'\n').encode(),stdout=subprocess.DEVNULL,check=True)
    requests, statuses, bodies = set(), {}, []

    def received():
        for event in view.events():
            p = event['params']
            rid = p.get('requestId')
            if event['method']=='Network.requestWillBeSent' and p['request']['url']==ajax:
                assert p['request']['method']=='GET' and p['type']=='XHR'
                requests.add(rid)
            elif rid in requests and event['method']=='Network.responseReceived':
                statuses[rid]=p['response']['status']
            elif rid in requests and event['method']=='Network.loadingFinished':
                assert statuses[rid]==200, 'unexpected AJAX status'
                res=view.cdp('Network.getResponseBody', {'requestId':rid})
                assert not res['base64Encoded']
                bodies.append(res['body'])
        return bodies

    expected = marker+'<br />\n'
    wait(f'{action}: no AJAX response with appended line', lambda: expected in received())
    wait(f'{action}: marker not rendered', lambda: marker in view.js("return document.querySelector('#tail').innerText"))
    tail = view.js("return document.querySelector('#tail').innerText.trim()")
    assert tail.splitlines()==[baseline,marker], f'{action}: unexpected #tail: {tail!r}'
    i = bodies.index(expected)
    wait(f'{action}: no follow-up AJAX poll', lambda: len(received())>i+1)
    assert bodies[i+1]=='' and bodies.count(expected)==1
    assert view.js("return document.querySelector('#tail').innerText.trim()")==tail
    print(f'PASS {action}: appended to {logfile}; Chromium XHR GET {urlsplit(ajax).path}?ajax '
          f'HTTP 200 body={expected!r}; #tail={tail!r}; next GET HTTP 200 body=\"\", row still appears once',flush=True)


def run(driver, origin, container, *, action):
    path = {'update':'/admin/update.php', 'upgrade':'/admin/expert/upgrade.php'}[action]
    url = origin + path
    def lines(file):
        return subprocess.check_output(['docker','exec',container,'cat',file]).decode().splitlines()
    def flow():
        return [r for line in lines('/tmp/qa-flow') if (r:=json.loads(line))[0]==path]
    def effects():
        return lines('/work/effects')
    view = View(driver,url,1366,900,False)
    button = 'form button[type=submit][name=confirm_update][value="1"]'
    token = view.js("return document.querySelector(arguments[0]).form.elements.csrf_token.value",button)
    initial = flow()
    assert token and len(initial)==1 and initial[0][1:3]==['GET',200] and effects()==[]
    print(f'PASS GET {path}: HTTP 200, Start {action.capitalize()} form, POST=0, commands=0',flush=True)
    view.events()
    view.click(button)
    heading = action.capitalize()+' Running'
    wait('missing running status', lambda: view.js("""return all('th').some(e=>text(e)===arguments[0]) &&
        text(document.querySelector('#tail')).includes(arguments[1])""", heading, 'Starting '+action))
    assert view.js("return performance.getEntriesByType('navigation')[0].responseStatus")==200
    requests = [r for e in view.events() if e['method']=='Network.requestWillBeSent'
                and (r:=e['params']['request'])['url']==url and r['method']=='POST']
    assert len(requests)==1 and parse_qs(requests[0]['postData'])=={'confirm_update':['1'],'csrf_token':[token]}
    expected = ['sudo truncate -s 0 /var/log/pi-star/pi-star_'+action+'.log','sudo /usr/local/sbin/pistar-'+action]
    wait('missing asynchronous updater command', lambda: len(effects())>=len(expected))
    assert effects()==expected
    verify_live_tail(view,container,action)
    assert effects()==expected and [r[1] for r in flow()]==['GET','POST'], 'polls repeated a launch or POST'
    driver('/url',{'url':url})  # Fresh GET, not a replay of the confirmation.
    assert view.field(button) and effects()==expected
    assert [r[1:] for r in flow()]==[['GET',200,initial[0][3],False],
                                      ['POST',200,initial[0][3],True],['GET',200,initial[0][3],False]]
    print(f'PASS {action}: one POST confirm_update=1, valid session CSRF, HTTP 200 "{heading}"; '
          f'stub trace: {expected}; fresh GET did not relaunch',flush=True)
