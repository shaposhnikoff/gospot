"""Native Power form: correlate confirm/cancel, PHP receipts and stub invocations."""
import json
import subprocess
from collections import Counter
from urllib.parse import parse_qs

from full_ui import View
from smoke import wait


def run(driver, origin, container):
    path = '/admin/power.php'
    url = origin + path
    def lines(file):
        return subprocess.check_output(['docker','exec',container,'cat',file]).decode().splitlines()
    def posts():
        return [r for l in lines('/tmp/qa-flow') if (r:=json.loads(l))[:2]==[path,'POST']]
    def effects():
        return lines('/work/effects')
    v = View(driver,url,1366,900,False)
    assert not posts() and not effects()
    for action, command in [('reboot','sudo reboot'), ('shutdown','sudo shutdown -h now')]:
        button = f'form button[name=action][value={action}]'
        token = v.js("return document.querySelector('form input[name=csrf_token]').value")
        n, base = len(posts()), effects()
        v.events()
        v.click(button)
        driver('/alert/dismiss',{})
        assert v.field(button) and len(posts())==n and effects()==base
        def requests(events):
            return [e['params']['request'] for e in events if e['method']=='Network.requestWillBeSent'
                    and e['params']['request']['url']==url and e['params']['request']['method']=='POST']
        assert not requests(v.events())
        print(f'PASS cancel {action}: native confirm dismissed; form retained; POST=0, CSRF not sent, no new HTTP response or commands',flush=True)
        v.click(button)
        driver('/alert/accept',{})
        wait('missing power receipt',lambda: len(posts())>n)
        assert len(posts())==n+1 and posts()[-1][2]==200 and posts()[-1][4]
        response = action.capitalize()+' command has been sent to your Pi'
        wait('missing action-specific PHP response',lambda: v.js('return document.body.innerText.includes(arguments[0])',response))
        assert v.js("return performance.getEntriesByType('navigation')[0].responseStatus")==200
        submitted=requests(v.events())
        assert len(submitted)==1 and parse_qs(submitted[0]['postData'])=={'action':[action],'csrf_token':[token]}
        # The prep chain and the power command run in separate background shells.
        expected=['sudo sync']*3+['sudo mount -o remount,ro /',command]
        wait('missing stub calls',lambda: len(effects())>=len(base)+len(expected))
        trace=effects()[len(base):]
        assert Counter(trace)==Counter(expected) and [s for s in trace if s!=command]==expected[:-1], trace
        print(f'PASS confirm {action}: POST=1, action={action}, session CSRF matched, HTTP 200, "{response}"; stub trace: {trace}',flush=True)
        driver('/url',{'url':url})  # fresh GET, no resubmission
    assert len(posts())==2 and len(effects())==10
