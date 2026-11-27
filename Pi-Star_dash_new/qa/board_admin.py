"""Run locally on pistar3 via `sudo python3 -`. Never submit an apply or rollback."""
import re
import base64
import http.cookiejar
import io
import json
from pathlib import Path
import ssl
import urllib.error
import urllib.request as u
import zipfile

p = Path('/etc/pistar-wizard')
origin = 'https://192.168.210.194:8788'
ctx = ssl.create_default_context(cafile=str(p/'tls-cert.pem'))
api = u.build_opener(u.ProxyHandler({}), u.HTTPSHandler(context=ctx),
                     u.HTTPCookieProcessor(http.cookiejar.CookieJar()))
headers = {'Authorization': 'Basic '+base64.b64encode(b'pi-star:'+(p/'admin-password').read_bytes().strip()).decode()}

def req(path, *, extra=None, auth=True, data=None):
    return api.open(u.Request(origin+'/manage/'+path, data=data,
                              headers={**(headers if auth else {}), **(extra or {})}), timeout=15)

for name in ('', 'api/config', 'api/system', 'api/log', 'api/log/download','api/backup/classic','api/configure'):
    try:
        req(name, auth=False)
    except urllib.error.HTTPError as e:
        assert e.code==401
    else:
        raise AssertionError('expected auth for '+name)
print('PASS HTTPS authentication: all 7 admin routes')
cfg = json.load(req('api/config')); assert cfg['config']['callsign'] and 'password' not in cfg['config']
system = json.load(req('api/system')); assert system['hostname']=='pistar3' and system['memory']['totalBytes']>0
print('PASS system:',system['hostname'],system['model'],system['kernel'])
rows=json.load(req('api/log'))['rows']; assert isinstance(rows,list)
assert req('api/log/download').headers.get_content_type()=='text/plain'
print('PASS log and plain-text download;',len(rows),'sanitized rows')
with req('api/backup/classic') as r:
    assert r.headers.get_content_type()=='application/zip'
    assert re.search(r'filename="Pi-Star_Config_[^"]+\.zip"', r.headers.get('Content-Disposition',''))
    archive=r.read()
with zipfile.ZipFile(io.BytesIO(archive)) as z:
    assert {'mmdvmhost','dmrgateway'} <= set(z.namelist()) and not any(n.endswith('.php') for n in z.namelist())
print('PASS classic Backup download (archive kept only in memory; restore not called on the board)')
