"""First save in a standalone credential editor. Never log form/INI values."""
import secrets
from subprocess import check_output
from urllib.parse import parse_qs

from expert import navigation
from full_ui import View, equal


def run(driver, origin, container, *, editor):
    secret = 'fixture-' + secrets.token_hex(24)
    if editor in ('bmapikey','tgifapikey'):
        path, section, initial = {'bmapikey':'/etc/bmapi.key','tgifapikey':'/etc/tgifapi.key'}[editor], 'key', ''
        if editor == 'tgifapikey':
            secret = '.'.join(('TGIFSTG1', secrets.token_hex(8), secrets.token_hex(8), secrets.token_hex(24)))
        fields = {'apikey':secret}
        expected = b'[key]\napikey=' + secret.encode() + b'\n\n'
    else:
        assert editor == 'dapnetapi'
        path, section, initial = '/etc/dapnetapi.key', 'DAPNETAPI', '0'
        fields = {'USER':'N0DEMO','PASS':secret,'TRXAREA':'qa-area'}
        expected = b'[DAPNETAPI]\nUSER=N0DEMO\nPASS='+secret.encode()+b'\nTRXAREA=qa-area\n\n'
    def inspect(*args):
        return check_output(['docker', 'exec', container, *args], timeout=10)

    base = origin + '/admin/expert/'
    view = View(driver, base, 1366, 900, False)
    navigation(view, 'GET')
    for page in ('fulledit_apikeys.php', f'fulledit_{editor}.php'):
        view.click(f'a[href="{page}"]')
        view.url = base + page
        navigation(view, 'GET')
    inspect('test', '!', '-e', path)
    def selector(k):
        return f'[name="{section}[{k}]"]'
    def value(k):
        return view.js('return document.querySelector(arguments[0]).value', selector(k))
    assert all(value(k)==initial for k in fields), 'unexpected first-use form'
    token = view.js('return document.querySelector("form[method=post]").elements.csrf_token.value')
    assert token
    for k,v in fields.items():
        view.type_into(selector(k),v)
    view.click('form[method=post] input[type=submit]')
    post = parse_qs(navigation(view, 'POST')['postData'], keep_blank_values=True)
    assert post.get('csrf_token') == [token] and all(post.get(f'{section}[{k}]')==[v] for k,v in fields.items()), 'wrong form or CSRF'
    driver('/url', {'url':view.url})
    navigation(view, 'GET')
    assert all(value(k)==v for k,v in fields.items()), 'fresh GET values differ'
    assert inspect('cat', path) == expected, 'installed INI bytes differ from expected'
    equal('installed owner:group:mode', inspect('stat','-c','%U:%G:%a',path), b'www-data:www-data:600\n')
    effects = inspect('cat','/work/effects').decode().splitlines()
    equal('mutation log', effects, ['mount -o remount,rw /','install '+path,'mount -o remount,ro /'])
    commands = inspect('cat','/work/priv-commands').decode().splitlines()
    equal('all privileged commands', commands,
          ['chown','chmod','chown','chmod','mount','install','mount','cp','chown','chmod'])
    for log in ('/tmp/qa-server.log', '/tmp/qa-flow', '/work/effects', '/work/priv-commands'):
        assert secret.encode() not in inspect('cat',log), 'synthetic credential present in fixture log'
    if editor == 'dapnetapi':
        inspect('test','!','-e','/etc/dapnetapi')
        print('PASS /etc/dapnetapi (without .key) not created', flush=True)
    print(f'PASS standalone {editor}: first GET left file absent; POST 200 with original CSRF; '
          f'fresh GET 200 matched {", ".join(fields)}', flush=True)
    print(f'PASS {path}: all {len(expected)} bytes match the independent INI oracle; '
          f'{expected.count(bytes([10]))} LF, www-data:www-data 0600; secret omitted', flush=True)
    print('PASS privileged command journal: '+' '.join(commands)+'; no restart', flush=True)
