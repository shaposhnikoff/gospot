"""Single-field gateway Quick Edit with independent byte and command oracles."""
from difflib import unified_diff
from hashlib import sha256
from subprocess import check_output
from urllib.parse import parse_qsl

from expert import navigation
from full_ui import ELEMENT, View, equal
from smoke import wait


def run(driver, origin, container, *, variant, service, view=None, reopen=None):
    section, key, before, after = {
        'nxdn': ('Network', 'InactivityTimeout', '10', '17'),
        'ysf': ('YSF Network', 'ReloadTime', '60', '75'),
        'p25': ('Network', 'InactivityTimeout', '10', '25'),
        'dapnet': ('DAPNET', 'SuppressTimeWhenBusy', '1', '0'),
        'mmdvmhost': ('DMR', 'ColorCode', '3', '12'),
        'dstarrepeater': ('dstarrepeater', 'beaconText', 'N0DEMO B', 'QA Beacon B'),
        'ircddbgateway': ('ircddbgateway', 'description1', 'Demo', 'North Pier'),
        'timeserver': ('timeserver', 'sendB', '1', '0'),
        'starnetserver': ('starnetserver', 'ircddbUsername', 'N0DEMO', 'N0TEST'),
    }[variant]
    def inspect(*args):
        return check_output(['docker','exec',container,*args],timeout=10)
    conf = '/etc/'+service
    original = inspect('cat',conf)
    if variant == 'p25':
        assert b'; Site note: secondary host list contains local reflectors.\n' in original
    if variant == 'dapnet':
        assert b'; Site note: local paging port.\n' in original
    if variant == 'dstarrepeater':
        assert b'; Site note: keep the module suffix.\n' in original and b'[dstarrepeater]' not in original
    if variant == 'ircddbgateway':
        assert b'reflector1=NONE\n' in original and b'dplusLogin=N0DEMO  \n' in original
    if variant == 'timeserver':
        assert b'; Modules used for site time announcements.\n' in original
    if variant == 'starnetserver':
        assert b'callsign=N0DEMO\n' in original and b'; Local StarNet login.\n' in original
    old = f'\n{key}={before}\n'.encode()
    equal('unique fixture key',original.count(old),1)
    expected = original.replace(old,f'\n{key}={after}\n'.encode())
    v = view or View(driver,origin+'/admin/expert/',1366,900,False)
    navigation(v,'GET')
    page = f'edit_{service}.php'
    if variant == 'starnetserver':
        # Original Expert header omits this page. Follow the packaged shell's
        # search result from Expert, without synthesizing a destination URL.
        def shadow(selector):
            return wait('missing Expert shell '+selector,lambda:v.js(
                "return document.querySelector('#pistar-classic-shell')?.shadowRoot.querySelector(arguments[0])",selector))[ELEMENT]
        driver('/element/'+shadow('summary')+'/click',{})
        driver('/element/'+shadow('input[type=search]')+'/value',{'text':'StarNetServer'})
        driver('/element/'+shadow(f'a[href="/admin/expert/{page}"]')+'/click',{})
    else:
        v.click(f'a[href="{page}"]')
    v.url += page
    navigation(v,'GET')
    selector = f'input[name="{section}[{key}]"], input[data-section="{section}"][data-key="{key}"]'
    def field(prop):
        return v.js('return document.querySelector(arguments[0])[arguments[1]]',selector,prop)
    equal('initial '+key,field('value'),before)
    name = field('name')
    form = v.js('return Array.from(new FormData(document.querySelector(arguments[0]).form))',selector)
    assert sum(k=='csrf_token' and bool(val) for k,val in form)==1
    v.type_into(selector,after)
    if variant == 'timeserver':
        equal('TimeServer submit label',v.js("return document.querySelector('form[method=post] input[type=submit]').value"),'Save Changes')
    v.click('form[method="post"] input[type=submit]')
    request = navigation(v,'POST')
    assert parse_qsl(request['postData'],keep_blank_values=True)==[(k,after if k==name else val) for k,val in form]
    if reopen:
        v=reopen()
        v.click(f'a[href="{page}"]')
        v.url+=page
    else:
        driver('/url',{'url':v.url})
    navigation(v,'GET')
    equal('fresh GET '+key,field('value'),after)
    print(f'PASS Expert → {service}: Apply POST 200 with original CSRF; only {section}/{key} changed ({before} → {after}); fresh GET 200 shows {after}',flush=True)
    equal('exact operations',inspect('cat','/work/effects').decode().splitlines(),[
        'mount -o remount,rw /','install '+conf,'mount -o remount,ro /',f'systemctl restart {service}.service'])
    equal('installed mode uid gid',inspect('stat','-c','%a:%u:%g',conf),b'644:0:0\n')
    saved = inspect('cat',conf)
    print('Expected SHA256='+sha256(expected).hexdigest()+'; installed SHA256='+sha256(saved).hexdigest(),flush=True)
    if saved!=expected:
        def public_lines(data):
            return [line.partition('=')[0]+'=<redacted>\n' if line.startswith(('AuthKey=','Password=','remotePassword=')) else line
                    for line in data.decode().splitlines(True)]
        raise AssertionError('Unexpected fixture bytes:\n'+''.join(unified_diff(
            public_lines(expected),public_lines(saved),'expected',conf)))
    print(f'PASS {conf}: only intended bytes changed, root:root 0644; only {service} restart requested',flush=True)
