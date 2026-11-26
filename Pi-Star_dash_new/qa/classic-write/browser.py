"""Run each real form in a separate authenticated browser session and check cross-file isolation."""
import base64
import json
from hashlib import sha256
from pathlib import Path
import re
import subprocess
import sys
import traceback
import os
from contextlib import ExitStack
from urllib.parse import urlsplit

sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
from smoke import browser
from importlib import import_module

origin, container, credential, only, variant=sys.argv[1:]
configure_args={'color':{'color':'12','zero':'0'}.get(variant),
                'edit_call':variant in ('color','default'),
                 'location':variant=='location','timezone':variant=='timezone','language':variant=='language',
                 'first_setup':variant in ('controller-first','controller-first-default'),
                 'modem':variant=='modem',
                 'controller':{'controller':'DSTAR','controller-reverse':'MMDVM','controller-first':'DSTAR',
                               'controller-first-default':'MMDVM'}.get(variant)} if only=='configure' else {}
assert urlsplit(origin).hostname=='127.0.0.1' and re.fullmatch('pstar-classic-write-[a-f0-9]+',container)
wpa='/etc/wpa_supplicant/wpa_supplicant.conf'
read=['docker','exec',container]+(['chroot','/stand'] if only in ('backup','configure') else [])+['cat']
def digest(path):
    return sha256(subprocess.check_output([*read,path])).hexdigest()
auth=base64.b64encode(b'qa:'+Path(credential).read_bytes().strip()).decode()
failed=[]
scenarios = [('Backup','backup','/etc/hostapd/hostapd.conf')] if only=='backup' else [('Wi-Fi','wifi','/etc/dmrgateway'),('Expert','expert',wpa)]
if only=='configure': scenarios = [('Configuration','configure',wpa)]
if only=='power': scenarios = [('Power','power',wpa)]
if only in ('update','upgrade'): scenarios = [(only.capitalize(),only,wpa)]
for label,module,other in scenarios:
    if only not in ('both',module): continue
    auto_ap=variant.startswith('auto-ap')
    install_failure=auto_ap and variant.endswith('-install-failure')
    if only in ('update','upgrade'): configure_args={'action':only}
    if only=='wifi' and variant=='delete': configure_args={'delete':True}
    if only=='wifi' and variant=='psk': configure_args={'rotate_psk':True}
    stale_wifi=only=='wifi' and variant=='stale'
    if stale_wifi: configure_args={'auth':auth}
    fn=import_module('wifi_stale' if stale_wifi else 'auto_ap' if auto_ap else 'updates' if only in ('update','upgrade') else module).run
    combined = {'full-apikeys':'tgif','full-apikeys-bm':'bm','full-apikeys-dapnet':'dapnet'}
    full = variant in combined or variant in ('full-dmrgateway','full-wifi','full-pistar-remote','full-rssidat','full-cron','full-bmapikey','full-dapnetapi','full-tgifapikey')
    if full:
        fn=import_module('full_edit').run
        full_editor = 'wpaconfig' if variant=='full-wifi' else variant.removeprefix('full-')
        configure_args={'editor':full_editor}
        if variant in ('full-bmapikey','full-dapnetapi','full-tgifapikey'):
            fn=import_module('credential_editor').run
        if variant in combined:
            full_editor='apikeys'
            fn=import_module('combined_keys').run
            configure_args={'kind':combined[variant]}
        if variant=='full-wifi': other='/etc/network/interfaces'
    gateway_quick = only=='expert' and variant in ('nxdn','ysf','p25','dapnet','mmdvmhost','dstarrepeater','ircddbgateway','timeserver','starnetserver')
    if gateway_quick:
        fn=import_module('expert_gateway').run
        service = variant+'gateway' if variant in ('nxdn','ysf','p25','dapnet') else variant
        configure_args={'variant':variant,'service':service}
    before=digest(other)
    oracle=None; baseline=None; attempted_verify=False
    try:
        oracle=import_module('configure_oracle') if only=='configure' and not auto_ap else None
        baseline=oracle.snapshot(container) if oracle else None
        with ExitStack() as stack:
            driver=stack.enter_context(browser(target_origin=origin,tls_pin=os.getenv('QA_MENU_PIN')))
            for cmd,params in [('Network.enable',{}),('Network.setCacheDisabled',{'cacheDisabled':True}),
                               ('Network.setExtraHTTPHeaders',{'headers':{'Authorization':'Basic '+auth}})]:
                driver('/goog/cdp/execute',{'cmd':cmd,'params':params})
            driver('/goog/cdp/execute',{'cmd':'Page.addScriptToEvaluateOnNewDocument','params':{'source':"""
              addEventListener('error', event => { if(event.error) sessionStorage.setItem('qa:js-error', '1') });
              addEventListener('unhandledrejection', () => sessionStorage.setItem('qa:js-error', '1'));
            """}})
            if os.getenv('QA_MENU_PIN'):
                from pilot_menu import stage, enter
                root=Path(os.environ['QA_MENU_ROOT'])
                entry=stack.enter_context(stage(root.parent/'staging/server.py',root/'operator'))
                def from_menu():
                    return enter(driver,entry,origin,'configuration' if only=='configure' else 'expert',int(os.environ['QA_MENU_WIDTH']))
                configure_args['view']=from_menu()
                if only=='expert': configure_args['reopen']=from_menu
            fn(driver, origin, container, **({'dstar': variant.startswith('auto-ap-dstar'),
                                             'install_failure': install_failure} if auto_ap else configure_args))
            assert driver('/execute/sync',{'script':"return sessionStorage.getItem('qa:js-error')",'args':[]}) is None, f'{label}: browser script error during edit/save'
        assert digest(other)==before, f'{label} unexpectedly changed {other}'
        route={'Wi-Fi':'/admin/wifi.php?page=wpa_conf','Expert':'/admin/expert/edit_dmrgateway.php','Backup':'/admin/config_backup.php','Configuration':'/admin/configure.php','Power':'/admin/power.php','Update':'/admin/update.php','Upgrade':'/admin/expert/upgrade.php'}[label]
        if gateway_quick: route=f'/admin/expert/edit_{service}.php'
        if full: route=f'/admin/expert/fulledit_{full_editor}.php'
        route_log=[rec for line in subprocess.check_output([*read,'/tmp/qa-flow']).splitlines()
                   if (rec:=json.loads(line))[0]==route]
        if gateway_quick or full:
            assert [rec[1:3] for rec in route_log]==[['GET',200],['POST',200],['GET',200]]
        flow=[rec for rec in route_log if rec[2]==200 or (only=='configure' and rec[1:3]==['POST',303])
              or (install_failure and rec[1:3]==['POST',500])]
        assert any(method=='POST' and valid and any(r[1]=='GET' and r[3]==sid for r in flow[:i])
                   and any(r[1]=='GET' and r[3]==sid for r in flow[i+1:])
                   for i,(_,method,_,sid,valid) in enumerate(flow)), f'{label}: missing GET / validated POST / GET in one session'
        if only=='backup':
            posts=[r for r in flow if r[1]=='POST']
            assert len(posts)==2 and all(r[4] and r[3]==posts[0][3] for r in posts), 'expected two validated POSTs in same PHP session'
            print('PASS ZIP download and restore: both POST 200 with CSRF tokens from the same valid PHP session')
        if oracle:
            posts=[r for r in route_log if r[1]=='POST']
            assert len(posts)==1 and posts[0][2] in (200,303) and posts[0][4], 'expected a single valid configure Apply'
            call,count=json.loads(subprocess.check_output([*read,'/tmp/qa-configure-receipt']))
            assert call==('QA7TEST' if configure_args['edit_call'] else 'N0DEMO'), 'PHP received an unexpected callsign'
            print(f'Configure: PHP received confCallsign={call} ({count} POST fields)',flush=True)
            print(f'Configure: CSRF matched; POST {posts[0][2]} between two GETs in the same session', flush=True)
            attempted_verify=True
            oracle.verify(container,baseline, **{k:v for k,v in configure_args.items() if k!='view'})
        postcode=next(r[2] for r in flow if r[1]=='POST')
        print(f'PASS {label}: GET 200 → POST {postcode} (session token matched) → GET 200 in same session')
        print(f'PASS {label}: unrelated file {other} SHA256 unchanged ({before[:16]}...)',flush=True)
    except Exception:
        failed.append(label); traceback.print_exc()
        if oracle and baseline is not None and not attempted_verify:
            # Keep the disk/service evidence even if the final UI assertion failed.
            try: oracle.verify(container,baseline, **{k:v for k,v in configure_args.items() if k!='view'})
            except Exception: traceback.print_exc()
if failed: sys.exit('FAILED: '+', '.join(failed))
