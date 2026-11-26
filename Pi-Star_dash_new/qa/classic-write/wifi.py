"""Exercise the original Wi-Fi editor using the browser session and container filesystem."""

from hashlib import pbkdf2_hmac, sha256
from pathlib import Path
import secrets
import subprocess
from urllib.parse import parse_qs

from full_ui import View, equal
from smoke import wait


CONF = "/etc/wpa_supplicant/wpa_supplicant.conf"
WIRED = "/etc/network/interfaces"
EXISTING = ["Fixture Existing", "fixture-old-passphrase"]
NEW = ["Fixture Added", "fixture-new-passphrase"]
SENTINELS = ("freq_list=2412 2437 2462", "bssid=02:00:00:00:00:01", "priority=47")


def run(driver, origin, container, *, delete=False, rotate_psk=False):
    def inspect(*args):
        return subprocess.check_output(["docker", "exec", "--user", "0", container, *args],
                                       stdin=subprocess.DEVNULL, timeout=15)

    def missing_settings(config):
        lines = {line.strip() for line in config.decode().splitlines()}
        return [line for line in SENTINELS if line not in lines]

    before = inspect("cat", CONF)
    wired = inspect("cat", WIRED)
    if missing_settings(before):
        raise AssertionError("Wi-Fi preservation fixture is missing required original settings")
    url = origin.rstrip("/") + "/admin/wifi.php?page=wpa_conf"
    v = View(driver, url, 1366, 900, False)

    def editor():
        return v.js("""
          const f = document.getElementById('wpa_conf_form');
          if (!f || f.action !== location.href || f.method !== 'post') return null;
          return {csrf: /^[a-f0-9]{64}$/.test(f.elements.csrf_token.value),
                  country: f.elements.wifiCountryCode.value,
                  count: f.elements.Networks.value,
                  profiles: all('input[name^="ssid"]', f).map(e =>
                      [e.value, f.elements['psk'+e.name.slice(4)].value])};
        """)

    def check_editor(profiles):
        expected = dict(csrf=True, country="GB", count=str(len(profiles)), profiles=profiles)
        if editor() != expected:
            raise AssertionError(f"Wi-Fi form did not display the expected {len(profiles)} profile(s), GB and CSRF field")

    if rotate_psk:
        standby = ["Fixture Standby", "fixture-standby-passphrase"]
        check_editor([EXISTING, standby])
        equal("initial mode/uid/gid", inspect("stat","-c","%a:%u:%g",CONF), b"600:0:0\n")
        prefix, target, other = before.split(b'\nnetwork={')
        assert b'ssid="Fixture Standby"\n' in other
        ssid, old_key = EXISTING
        key = secrets.token_hex(16)
        def key_lines(passphrase):
            hashed = pbkdf2_hmac("sha1", passphrase.encode(), ssid.encode(), 4096, 32).hex()
            return f'\t#psk="{passphrase}"\n\tpsk={hashed}\n'.encode()
        old_lines, new_lines = key_lines(old_key), key_lines(key)
        assert target.count(old_lines)==1, "unexpected original WPA key"
        expected_file = b'\nnetwork={'.join((prefix,target.replace(old_lines,new_lines,1),other))
        initial_form = v.js("""const f=document.getElementById('wpa_conf_form');
            return Array.from(new FormData(f, f.elements.SaveWPAPSKSettings));""")
        v.type_into('#wpa_conf_form input[name="psk0"]', key)
        check_editor([[ssid,key],standby])
    elif delete:
        block = Path(__file__).with_name('wifi-delete.block').read_bytes()
        assert before.count(block)==1 and before.count(b'network={')==2
        expected_file = before.replace(block,b'',1)
        equal("initial mode/uid/gid", inspect("stat","-c","%a:%u:%g",CONF), b"600:0:0\n")
        check_editor([["Fixture Remove", ""], EXISTING])
        print("Initial GET: two profiles, Fixture Remove (row 0) and Fixture Existing (row 1)",flush=True)
        v.click('#Networkbox0 input[value="Delete"]')
        check_editor([EXISTING])
    else:
        check_editor([EXISTING])
        v.click('#wpa_conf_form input[value="Add Network"]')
        v.type_into('input[name="ssid1"]', NEW[0])
        v.type_into('input[name="psk1"]', NEW[1])
        check_editor([EXISTING, NEW])
    token = v.js("return document.getElementById('wpa_conf_form').elements.csrf_token.value")
    v.events()
    v.click('input[name="SaveWPAPSKSettings"]')
    # The POST renders the old form and then navigates to wlan0_info via JS.
    # Inspect the actual POST response via performance events, without logging
    # the request body (it contains the form token and fixture passphrases).
    events = []
    def post_responses():
        events.extend(v.events())
        posts = {e["params"]["requestId"] for e in events if e["method"] == "Network.requestWillBeSent"
                 and e["params"]["request"]["url"] == url and e["params"]["request"]["method"] == "POST"}
        return [e["params"]["response"]["status"] for e in events
                if e["method"] == "Network.responseReceived" and e["params"]["requestId"] in posts]
    equal("Wi-Fi save POST responses", wait("missing Wi-Fi POST response", post_responses), [200])
    if delete or rotate_psk:
        requests = [r for e in events if e["method"]=="Network.requestWillBeSent"
                    and (r:=e["params"]["request"])["method"]=="POST" and r["url"]==url]
        assert len(requests)==1
        posted = parse_qs(requests[0]["postData"],keep_blank_values=True)
        assert posted.get('csrf_token')==[token]
        if rotate_psk:
            assert posted=={k:[key if k=='psk0' else val] for k,val in initial_form}, 'unexpected Wi-Fi POST fields'
            print("PASS Save: one POST 200 with original CSRF; only psk0 changed across all form fields",flush=True)
        else:
            assert posted.get('Networks')==['1']
            assert posted.get('ssid1')==[EXISTING[0]] and not {'ssid0','psk0'}.intersection(posted)
            print("PASS Delete + Save: one POST 200, original CSRF, remaining SSID submitted as row 1",flush=True)

    # wifi.php prints success without checking install's status; verify the
    # destination itself, not the success message or the /tmp staging file.
    installed = inspect("cat", CONF)
    equal("Wi-Fi installed file mode/uid/gid", inspect("stat", "-c", "%a:%u:%g", CONF), b"600:0:0\n")
    if inspect("cat", WIRED) != wired:
        raise AssertionError("Wi-Fi save changed /etc/network/interfaces")

    driver("/url", {"url": url})  # Fresh GET from disk, never replay the POST.
    if rotate_psk:
        check_editor([[ssid,key],standby])
        assert installed==expected_file, 'installed file differs from independent two-line PSK edit'
        print(f"PASS fresh GET: new PSK for {ssid}; {standby[0]} retains original PSK; "
              "disk matches independent PBKDF2-HMAC-SHA1 (SSID salt, 4096 iterations, 32 bytes)",flush=True)
        print(f"PASS {CONF}: only #psk and psk lines changed; second block and all other bytes identical; "
              f"root:root 0600; expected/actual file SHA256={sha256(installed).hexdigest()}",flush=True)
        return
    if delete:
        check_editor([EXISTING])
        assert installed==expected_file, 'Wi-Fi deletion changed bytes outside the selected block'
        print(f"PASS fresh GET: only Fixture Existing; {CONF} {len(before)} -> {len(installed)} bytes; "
              f"exactly {len(block)} selected block bytes removed; expected/actual SHA256={sha256(installed).hexdigest()}; "
              "root:root 0600, wired config unchanged",flush=True)
        return
    check_editor([EXISTING, NEW])
    ssid, psk = NEW
    digest = pbkdf2_hmac("sha1", psk.encode(), ssid.encode(), 4096, 32).hex()
    block = (f'network={{\n\t#ssid="{ssid}"\n\tssid={ssid.encode().hex()}\n\t#psk="{psk}"\n'
             f'\tpsk={digest}\n\tid_str="1"\n\tpriority=99\n\tscan_ssid=1\n}}\n\n').encode()
    if block.rstrip() not in installed:
        raise AssertionError("Wi-Fi installed file does not contain the expected new WPA profile and derived PSK")
    if installed != before + block:
        missing = missing_settings(installed)
        raise AssertionError("Wi-Fi save and fresh GET passed, but the original WPA configuration was rewritten"
                             + (": lost/changed " + ", ".join(missing) if missing else ""))
    print("Wi-Fi: save and fresh GET passed; WPA hash, root:root 0600, original WPA bytes and wired config verified.")
