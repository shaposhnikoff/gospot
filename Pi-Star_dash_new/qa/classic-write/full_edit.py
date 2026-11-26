"""Edit one line via the real Full Edit textarea; never print its contents."""
from hashlib import sha256
from subprocess import check_output
from urllib.parse import parse_qs

from expert import navigation
from full_ui import View, equal


def run(driver, origin, container, *, editor):
    def inspect(*args):
        return check_output(['docker','exec',container,*args],timeout=10)
    wifi = editor == 'wpaconfig'
    path = {'wpaconfig':'/etc/wpa_supplicant/wpa_supplicant.conf', 'cron':'/etc/crontab',
            'rssidat':'/usr/local/etc/RSSI.dat'}.get(editor, '/etc/'+editor)
    mode = b'0:0:600\n' if wifi else b'0:0:644\n'
    before = inspect('cat',path)
    equal('initial uid:gid:mode',inspect('stat','-c','%u:%g:%a',path),mode)
    if wifi:
        old, new = b'\tpriority=47', b'\tpriority=48'
        assert before.count(b'network={')==2 and b'\tpriority=8\n' in before
        units = []
        change = 'priority 47 → 48, standby priority 8 unchanged'
    elif editor == 'pistar-remote':
        old, new = b'callsign=N0DEMO', b'callsign=N0TEST'
        assert b'[keeper]\n'+old+b'\n' in before and b'enabled=false\n' in before
        units = ['pistar-remote']
        change = 'keeper/callsign N0DEMO → N0TEST'
    elif editor == 'rssidat':
        old, new = b'2000    -100', b'2000    -103'
        units = []
        change = 'raw reading 2000: -100 → -103 dBm'
    elif editor == 'cron':
        old, new = b'15 4 * * * nobody /bin/true', b'25 4 * * * nobody /bin/true'
        units = []
        change = 'only the daily test job moved from 04:15 to 04:25'
    else:
        assert editor == 'dmrgateway'
        old, new = b'TGRewrite=2,92,2,92,1', b'TGRewrite=2,92,2,310,1'
        units = ['mmdvmhost','dmrgateway']
        change = 'second TGRewrite destination 92 → 310'
    assert before.count(b'\n'+old+b'\n') == 1 and b'\r' not in before
    expected = before.replace(b'\n'+old+b'\n', b'\n'+new+b'\n')
    page = f'fulledit_{editor}.php'
    v = View(driver, origin+'/admin/expert/', 1366, 900, False)
    navigation(v, 'GET')
    v.click(f'a[href="{page}"]')
    v.url += page
    navigation(v, 'GET')
    area = 'textarea[name=data]'
    def contents():
        return v.js('return document.querySelector(arguments[0]).value', area).encode()
    assert contents() == before, 'Full Edit did not display the original file'
    token = v.js('return document.querySelector("textarea").form.elements.csrf_token.value')
    assert token
    v.js('''const a=document.querySelector(arguments[0]), i=a.value.indexOf(arguments[1]);
            a.focus(); a.setSelectionRange(i,i+arguments[1].length);''', area, old.decode())
    v.cdp('Input.insertText', {'text':new.decode()})
    assert contents() == expected, 'more than the selected line changed before submit'
    v.click('form[name=test] input[type=submit]')
    posted = parse_qs(navigation(v, 'POST')['postData'],keep_blank_values=True)
    assert posted['csrf_token']==[token] and posted['data']==[expected.decode().replace('\n','\r\n')]
    if editor == 'cron':
        equal('save confirmation',v.js("return document.querySelector('#cronSaveOk')?.innerText.trim()"),'Crontab saved.')
        print('PASS POST confirmation: Crontab saved.', flush=True)
    driver('/url', {'url':v.url})
    navigation(v, 'GET')
    assert contents() == expected, 'fresh GET did not display the edited file'
    print('PASS Full Edit: one POST 200 with original CSRF; fresh GET 200: '+change,flush=True)
    saved = inspect('cat',path)
    restarts = [f'systemctl restart {unit}.service' for unit in units]
    write = 'sed -i -e 1r:<staged> -e d /etc/crontab' if editor=='cron' else 'install '+path
    equal('write and restart order',inspect('cat','/work/effects').decode().splitlines(),
          ['mount -o remount,rw /',write,'mount -o remount,ro /',*restarts])
    equal('uid:gid:mode',inspect('stat','-c','%u:%g:%a',path),mode)
    stage=['cp','chown','chmod']
    commands=inspect('cat','/work/priv-commands').decode().splitlines()
    transaction = ['mount','sed','mount'] if editor=='cron' else stage+['mount','install','mount']
    equal('all privileged commands across GET/POST/GET',commands,stage+transaction+['systemctl']*len(units)+stage)
    print(f"PASS root:root {'0600' if wifi else '0644'}; privileged command journal: "+' '.join(commands),flush=True)
    for label,data in [('expected',expected),('installed',saved)]:
        print(f'{label}: bytes={len(data)} LF={data.count(bytes([10]))} CRLF={data.count(bytes([13,10]))} SHA256={sha256(data).hexdigest()}',flush=True)
    assert saved == expected, 'file differs outside the single requested edit'
    print(f'PASS {path}: all other file bytes preserved including comments, quoting and final newline',flush=True)
