"""Native Auto AP save and confirmation guard; verify bytes in the private chroot."""
import json
import secrets
from hashlib import sha256
from subprocess import check_output

from full_ui import View
from smoke import wait


def run(driver, origin, container, *, dstar, install_failure=False):
    path = '/admin/configure.php'
    config = '/etc/hostapd/hostapd.conf'
    def disk(*cmd):
        return check_output(['docker','exec',container,'chroot','/stand',*cmd])
    def posts():
        records = map(json.loads, disk('cat','/tmp/qa-flow').splitlines())
        return [r for r in records if r[:2]==[path,'POST']]
    def operations():
        return [s for s in disk('cat','/work/effects').decode().splitlines()
                if not s.startswith('read ') or s=='read cat '+config]

    v = View(driver, origin+path, 1366, 900, False)
    mode = 'DSTAR' if dstar else 'MMDVM'
    assert v.js("return document.querySelector('[name=controllerSoft]:checked').value")==mode
    form = '#autoApPassForm '
    first, confirm, button = [form+i for i in ('#psk1','#psk2','#submitpsk')]
    disabled = lambda: v.js("return document.querySelector(arguments[0]).disabled", button)
    assert disabled() and not posts()
    before = disk('cat',config)
    old = b'wpa_passphrase=BeforeFixtureOnly\n'
    assert before.count(old)==1 and before.count(b'\nwpa=1\n')==1
    key = secrets.token_hex(10)
    wanted = before.replace(old, b'wpa_passphrase='+key.encode()+b'\n').replace(b'\nwpa=1\n',b'\nwpa=2\n')
    if install_failure:
        stat_fmt = '%u:%g:%a %i %y %z'
        initial_stat = disk('stat','-c',stat_fmt,config)
    v.type_into(first,key)
    v.type_into(confirm,key)
    assert not disabled(), 'matching valid key did not enable Set PSK'
    v.click(button)
    wait('Auto AP save did not produce a validated POST',lambda: posts())
    saved = posts()
    if install_failure:
        assert len(saved)==1 and saved[0][2]==500 and saved[0][4], 'expected one CSRF-validated POST returning 500'
        assert v.js("return performance.getEntriesByType('navigation')[0].responseStatus") == 500
        assert v.js("return document.querySelector('[role=alert]')?.innerText") == 'Cannot save the Auto AP configuration.'
        assert disk('cat','/work/attempted-hostapd.sha256').split()[0].decode()==sha256(wanted).hexdigest(), 'unexpected staged contents'
        assert operations()==['mount -o remount,rw /','read cat '+config,'injected install exit=1 '+config,'mount -o remount,ro /'], 'wrong failure/cleanup trace'
        assert disk('cat',config)==before and disk('stat','-c',stat_fmt,config)==initial_stat, 'failed install changed hostapd.conf'
        print(f'PASS {mode}: staged file matched submitted PSK and wpa=2; Chromium POST 500, CSRF matched, visible save error',flush=True)
        print(f'PASS {mode}: original hostapd.conf byte-identical (SHA256 {sha256(before).hexdigest()}); uid/gid, mode, inode, mtime and ctime unchanged',flush=True)
        print(f'PASS {mode}: rw -> read -> injected install failure -> ro',flush=True)
        driver('/url',{'url':origin+path})
        assert v.field(first) and posts()==saved, 'fresh GET did not recover without resubmission'
        return
    assert len(saved)==1 and saved[0][2]==303 and saved[0][4], 'unexpected HTTP/CSRF result'
    expected_ops = ['mount -o remount,rw /','read cat '+config,'install '+config,'mount -o remount,ro /']
    assert operations()==expected_ops, 'unexpected Auto AP write window: '+str(operations())
    assert disk('cat',config)==wanted, 'hostapd bytes differ from expected PSK and WPA mode edits'
    assert disk('stat','-c','%u:%g:%a',config).strip()==b'0:0:600'
    print(f'PASS {mode}: Set PSK -> POST 303, valid CSRF; wpa_passphrase matches, wpa=2, other bytes preserved, root:root 0600; rw -> read -> install -> ro',flush=True)

    wait('Auto AP form did not return after save',lambda: v.field(button),timeout=20)
    v.type_into(first,key)
    v.type_into(confirm,key)
    assert not disabled()
    v.type_into(first,secrets.token_hex(12))
    assert disabled(), 'Set PSK was enabled with mismatching confirmation'
    stat = disk('stat','-c','%i %y %z',config)
    v.click(button)  # Real WebDriver click; a disabled button must do nothing.
    assert v.field(confirm) and disabled() and posts()==saved, 'mismatch submitted a second POST'
    assert disk('cat',config)==wanted and disk('stat','-c','%i %y %z',config)==stat, 'mismatch changed hostapd.conf'
    print(f'PASS {mode}: mismatched confirmation disabled Set PSK; 0 additional POSTs; hostapd bytes, inode, mtime and ctime unchanged',flush=True)

    # Matching is checked by the browser, but PSK length by the PHP handler.
    v.type_into(first, 'short')
    v.type_into(confirm, 'short')
    v.click(button)
    rejection = b'Pi-Star configure.php: AutoAP PSK rejected (must be 8-63 printable ASCII or 64-hex)'
    wait('missing server rejection of short PSK',lambda: len(posts())==2 and rejection in disk('cat','/tmp/qa-server.log'))
    assert posts()[1][2:]==[422,*saved[0][3:]], 'short PSK submission did not get HTTP 422 in same session with valid CSRF'
    assert disk('cat',config)==wanted and disk('stat','-c','%i %y %z',config)==stat
    assert operations()==expected_ops, 'invalid input caused privileged operations'
    print(f'PASS {mode}: matching 5-character PSK rejected (HTTP 422); zero additional remounts or installs; saved hostapd file unchanged',flush=True)
