"""Rotate one credential on the combined page; never log form or INI values."""
from secrets import token_hex
from subprocess import check_output, run as execute, DEVNULL
from urllib.parse import parse_qs

from expert import navigation
from full_ui import View, equal


def run(driver, origin, container, *, kind):
    def tgif():
        return '.'.join(('TGIFSTG1', token_hex(8), token_hex(8), token_hex(24)))
    def inspect(*args):
        return check_output(['docker','exec',container,*args],timeout=10)
    def ini(section, fields):
        if section == 'dapnet':
            return ('[DAPNETAPI]\nUSER={USER}\nPASS={PASS}\nTRXAREA={TRXAREA}\n\n'.format(**fields)).encode()
        return ('[key]\napikey={apikey}\n\n'.format(**fields)).encode()

    paths = {'bm':'/etc/bmapi.key','dapnet':'/etc/dapnetapi.key','tgif':'/etc/tgifapi.key'}
    titles = {'bm':'BrandMeister API Key','dapnet':'DAPNET API Credentials','tgif':'TGIF Static Talkgroups API Key'}
    sections = {'bm':'key','dapnet':'DAPNETAPI','tgif':'key'}
    initial = {'bm':{'apikey':token_hex(24)},'tgif':{'apikey':tgif()},
               'dapnet':{'USER':'QAUSER','PASS':token_hex(24),'TRXAREA':'qa-area'}}
    if kind=='dapnet':
        new = {'USER':'QAUSER2','PASS':token_hex(24),'TRXAREA':'qa-area-2'}
    else:
        new = {'apikey':tgif() if kind=='tgif' else token_hex(24)}
    target = paths[kind]
    before = {p:ini(k,initial[k]) for k,p in paths.items()}
    def metadata(path):
        return inspect('stat','-c','%U:%G:%a:%i:%y:%z',path)
    for path,data in before.items():
        execute(['docker','exec','-i','--user','www-data',container,'tee',path],
                input=data,stdout=DEVNULL,check=True,timeout=10)
        assert inspect('cat',path)==data, 'invalid fixture contents'
    stats = {p:metadata(p) for p in before}
    assert all(s.startswith(b'www-data:www-data:600:') for s in stats.values())

    view = View(driver,origin+'/admin/expert/',1366,900,False)
    navigation(view,'GET')
    view.click('a[href="fulledit_apikeys.php"]')
    view.url += 'fulledit_apikeys.php'
    navigation(view,'GET')
    def form(kind):
        return f'form:has(input[name=apikeyTarget][value="{kind}"])'
    def selector(k,field):
        return form(k)+f' input[name="{sections[k]}[{field}]"]'
    def verify_fields(values):
        for k,fields in values.items():
            for field,val in fields.items():
                assert view.js('return document.querySelector(arguments[0]).value',
                               selector(k,field))==val, 'credential display mismatch'
    verify_fields(initial)
    scope = form(kind)
    csrf = view.js('return document.querySelector(arguments[0]).elements.csrf_token.value',scope)
    assert csrf
    for field,val in new.items():
        view.type_into(selector(kind,field),val)
    submit = scope+' input[name=apikeySave]'
    caption = view.js('return document.querySelector(arguments[0]).value',submit)
    view.click(submit)
    post = parse_qs(navigation(view,'POST')['postData'],keep_blank_values=True)
    section = sections[kind]
    assert post=={'csrf_token':[csrf], 'apikeyTarget':[kind], section:[section], 'apikeySave':[caption],
                  **{f'{section}[{field}]':[val] for field,val in new.items()}}, 'incorrect combined form POST'
    assert view.js('return document.querySelector(".contentwide [role=status]")?.textContent.trim()') == titles[kind]+' saved.'
    driver('/url',{'url':view.url})
    navigation(view,'GET')
    verify_fields({**initial,kind:new})
    print('PASS combined fulledit_apikeys.php: three populated sections; one POST 200 '
          f'with original CSRF and apikeyTarget={kind}; saved confirmation; fresh GET 200 matched all fields',flush=True)
    expected = ini(kind,new)
    assert inspect('cat',target)==expected, 'installed INI bytes differ'
    equal('installed owner/group/mode',inspect('stat','-c','%U:%G:%a',target),b'www-data:www-data:600\n')
    for p,data in before.items():
        if p!=target:
            assert inspect('cat',p)==data and metadata(p)==stats[p], 'unselected credential file changed'
    equal('privileged command sequence',inspect('cat','/work/priv-commands').splitlines(),[b'mount',b'install',b'mount'])
    equal('save effects',inspect('cat','/work/effects').decode().splitlines(),
          ['mount -o remount,rw /','install '+target,'mount -o remount,ro /'])
    for log in ('/tmp/qa-server.log','/tmp/qa-flow','/work/effects','/work/priv-commands'):
        recorded = inspect('cat',log)
        secrets = [initial['bm']['apikey'],initial['dapnet']['PASS'],initial['tgif']['apikey'],
                   new['PASS'] if kind=='dapnet' else new['apikey'],csrf]
        assert not any(s.encode() in recorded for s in secrets), 'secret logged'
    print(f'PASS {target}: all {len(expected)} bytes match; www-data:www-data 0600. '
          f'{", ".join(p for p in before if p!=target)}: original bytes, owner/mode, inode, mtime and ctime unchanged. '
          'Exactly one install, no restart; secrets omitted.',flush=True)
