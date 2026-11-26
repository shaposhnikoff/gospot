"""Run the pilot's real static server and bundle on loopback; only radio status is synthetic."""
from contextlib import contextmanager
from http.server import ThreadingHTTPServer
import importlib.util
import json
from pathlib import Path
from threading import Thread
from urllib.parse import urlsplit

from full_ui import View, equal
from smoke import wait

@contextmanager
def stage(script, dist):
    spec = importlib.util.spec_from_file_location('pilot_stage',script)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    module.ROOT = Path(dist).resolve()
    class Handler(module.Handler):
        def status(self, endpoint='/api/status'):
            stub = {'Node':'pistar3-fixture','UpdatedAt':'2026-01-01T00:00:00Z',
                    'Radio':dict(Callsign='N0DEMO',ID='1234567',RX='',TX='',Operation='Simplex',CC='3',Slots='TS2'),
                    'HeardAvailable':True,'Heard':[]}
            stub['Modem'] = stub['Master'] = dict(Detail='fixture',Source='fixture',At='',State='unknown')
            self.reply(200,json.dumps(stub).encode(),'application/json')
    with ThreadingHTTPServer(('127.0.0.1',0),Handler) as http:
        t=Thread(target=http.serve_forever,daemon=True)
        t.start()
        try: yield f'http://127.0.0.1:{http.server_port}/'
        finally: http.shutdown(); t.join()

def open_menu(driver, start, section, width):
    v=View(driver,start,width,844 if width==390 else 900,width==390)
    wait('pilot did not read the status fixture',lambda:v.field('[data-testid=last-success] time'))
    v.nav('console')
    v.click('[data-testid=console-filter-'+('config' if section=='configuration' else 'quick')+']')
    return v

def enter(driver, start, origin, section, width):
    v=open_menu(driver,start,section,width)
    path='/admin/configure.php' if section=='configuration' else '/admin/expert/'
    selector=f'[data-testid=console-item-{section}] a'
    equal('menu destination',v.js('return document.querySelector(arguments[0]).href',selector),origin+path)
    v.events()
    v.click(selector)
    v.url=origin+path
    equal('actual page after clicking',v.js('return location.href'),v.url)
    if section=='configuration':
        labels=v.js("return all('#config h2').filter(visible).map(text)")
        assert {'Control Software','MMDVMHost Configuration','General Configuration','DMR Configuration'}<=set(labels)
    else:
        labels=v.js("return all('.header b').filter(visible).map(text)")
        assert {'Quick Edit:','Full Edit:','Tools:'}<=set(labels)
    print(f'PASS {width}px: operator / → Все функции Pi-Star → {section} → HTTPS {path}; sections={labels}',flush=True)
    return v

if __name__=='__main__':
    import sys
    from smoke import browser
    # Use the captured *installed* server. Observe its redirect without ever
    # following the LAN address from the fixture.
    from http.client import HTTPConnection
    with stage(*sys.argv[1:]) as start, browser(target_origin=start) as driver:
        v=open_menu(driver,start,'configuration',1366)
        link=v.js("return document.querySelector('[data-testid=console-item-configuration] a').href")
        assert link==start+'admin/configure.php'
        c=HTTPConnection(urlsplit(start).netloc)
        c.request('GET',urlsplit(link).path); res=c.getresponse()
        assert (res.status,res.getheader('Location'))==(302,'https://192.168.210.194:8788/manage/#settings')
        print('REPRODUCED native Configuration menu → 302 to /manage/#settings (not followed)')
        v.click('[data-testid=console-filter-quick]')
        assert v.field('[data-testid=console-item-expert]') and not v.field('[data-testid=console-item-expert] a')
        print('REPRODUCED native Expert entry is text only')
