"""Compare rendered Quick Edit controls to the original admin, on identical synthetic INIs."""
from html.parser import HTMLParser
import json
from pathlib import Path
import subprocess
import sys

QA = Path(__file__).resolve().parent
PROJECT = QA.parents[1]
SOURCE = PROJECT.parent / 'Pi-Star_DV_Dash'
SERVICES = 'mmdvmhost dstarrepeater ircddbgateway timeserver starnetserver dmrgateway ysfgateway p25gateway nxdngateway dapnetgateway'.split()
IMAGE = 'php:8.2-cli@sha256:080193293786a2d9442806eca60635516120a30309b694c4292a56a4f8e2d353'


class Editor(HTMLParser):
    def __init__(self):
        super().__init__()
        self.fields, self.buttons = [], []
        self.post = self.csrf = False

    def handle_starttag(self, tag, attrs):
        a = dict(attrs)
        if tag == 'form':
            self.post = a.get('method', '').lower() == 'post' and not a.get('action')
        if tag != 'input' or not self.post:
            return
        name, value = a.get('name'), a.get('value', '')
        if name == 'csrf_token' and value:
            self.csrf = True
        if a.get('type') == 'submit':
            self.buttons.append(value)
        if a.get('type') == 'text':
            if 'data-section' in a:
                name = f"{a['data-section']}[{a['data-key']}]"
            self.fields.append((name, value, 'disabled' in a, 'readonly' in a))

    def handle_endtag(self, tag):
        if tag == 'form':
            self.post = False


def render(root):
    mounts = [(root, '/var/www/dashboard'), (QA, '/qa'), (QA/'fixtures/pistar-release','/etc/pistar-release')]
    mounts += [(PROJECT/'qa/classic-write/configure/etc'/name, '/etc/'+name) for name in SERVICES]
    args = ['docker','--host','unix:///var/run/docker.sock','run','--pull=never','--rm','--network','none',
            '--read-only','--cap-drop','ALL','--security-opt','no-new-privileges','-u','65534:65534',
            '--tmpfs','/tmp:rw,noexec,nosuid,nodev,size=12m','-e','QA_CATALOG=1','-w','/var/www/dashboard']
    for src, dst in mounts:
        args.extend(['--mount', f'type=bind,src={src},dst={dst},readonly'])
    shell = '''set -eu
php -n -d auto_prepend_file=/qa/probes.php -d session.save_path=/tmp -d display_errors=0 -d log_errors=1 \
 -d disable_functions=exec,shell_exec,system,passthru,popen,pclose,proc_open,pcntl_exec,dl \
 -d allow_url_fopen=0 -S 127.0.0.1:8080 >/tmp/log 2>&1 &
php -r '
 for ($i=0;$i<100 && !($s=@fsockopen("127.0.0.1",8080));$i++) usleep(10000);
 foreach(array_slice($argv,1) as $name){
    $body=file_get_contents("http://127.0.0.1:8080/admin/expert/edit_".$name.".php");
    if(!$body || !str_contains($http_response_header[0]," 200 ")) exit(1);
    echo json_encode($body,JSON_THROW_ON_ERROR),"\\n";
 }' "$@" || { cat /tmp/log >&2; exit 1; }
if grep -E 'PHP (Fatal|Parse) error|PHP (Warning|Notice|Deprecated):' /tmp/log >&2; then exit 1; fi'''
    lines = subprocess.check_output(args + [IMAGE,'sh','-ec',shell,'qa',*SERVICES],timeout=60).splitlines()
    assert len(lines) == len(SERVICES)
    editors = []
    for name, line in zip(SERVICES, lines):
        editor = Editor()
        editor.feed(json.loads(line))
        assert editor.csrf and editor.fields and editor.buttons, f'incomplete editor: {name}'
        editors.append(editor)
    return editors


if __name__ == '__main__':
    release = Path(sys.argv[1]).resolve(strict=True)
    assert release.parent.parent == PROJECT/'build' and release.name == 'docroot'
    print(f'Original: {SOURCE.name}; package: {release.parent.name}; identical synthetic settings; read-only GET',flush=True)
    total = 0
    for name, old, new in zip(SERVICES, render(SOURCE), render(release)):
        expected = list(old.fields)
        if name == 'ircddbgateway' and (release/'admin/expert/ircddbgateway-entry.php').is_file():
            # Stock parse_ini_file treats the literal NONE as empty; the
            # byte-preserving editor displays the setting actually on disk.
            idx = expected.index(('ircddbgateway[reflector1]', '', False, False))
            expected[idx] = ('ircddbgateway[reflector1]', 'NONE', False, False)
            print('ircDDBGateway: reflector1 displays the literal fixture value NONE instead of an empty field')
        if name == 'dmrgateway':
            # Upstream parse_ini_file collapses repeated keys to their last
            # value. The package also exposes the earlier physical rule.
            idx = expected.index(('DMR Network 1[TGRewrite0]', '2,93,2,93,1', False, False))
            expected.insert(idx, ('DMR Network 1[TGRewrite0]', '2,91,2,91,1', False, False))
            print('DMRGateway: packaged editor additionally exposes the first repeated TGRewrite0 rule')
        assert expected == new.fields and old.buttons == new.buttons, f'Quick Edit mismatch: {name}'
        total += len(old.fields)
        print(f'PASS {name}: {len(old.fields)} editable fields, {len(old.buttons)} Apply controls, CSRF present')
    print(f'PASS {len(SERVICES)} Quick Edit pages, all {total} original fields retained: section/key, order, flags and expected values verified')
