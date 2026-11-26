"""Pipe to `ssh pistar3 python3 - --external` to test a temporary muted Firefox on the Pi."""
import json, os, socket, subprocess, sys, tempfile, time
from pathlib import Path

with tempfile.TemporaryDirectory() as tmp:
    with socket.socket() as s:
        s.bind(('127.0.0.1',0))
        port=s.getsockname()[1]
    Path(tmp,'user.js').write_text(f'user_pref("marionette.port",{port});\n'
                                   'user_pref("media.volume_scale","0.0");\n'
                                   'user_pref("network.proxy.type",0);\n'
                                   'user_pref("browser.defaultbrowser.notificationbar",false);\n')
    env=dict(os.environ, MOZ_HEADLESS="1", MOZ_CRASHREPORTER_DISABLE="1")
    proc=subprocess.Popen(['firefox-esr','--headless','--no-remote','--profile',tmp,'--marionette','about:blank'],
                          env=env,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
    try:
        deadline=time.monotonic()+65
        while True:
            try: sock=socket.create_connection(('127.0.0.1',port),timeout=1); break
            except OSError:
                if proc.poll() is not None or time.monotonic()>deadline: raise
                time.sleep(.25)
        sock.settimeout(45)
        r=sock.makefile('rb')
        def recv():
            n=b''
            while True:
                c=r.read(1)
                if c==b':': break
                if not c.isdigit(): raise RuntimeError('Marionette disconnected')
                n+=c
            return json.loads(r.read(int(n)))
        assert recv()['marionetteProtocol']==3
        seq=0
        def cmd(name, args):
            global seq
            seq+=1
            b=json.dumps([0,seq,'WebDriver:'+name,args]).encode()
            sock.sendall(str(len(b)).encode()+b':'+b)
            while True:
                msg=recv()
                if msg[:2]==[1,seq]: break
            if msg[2]: raise RuntimeError(str(msg[2])[:400])
            return msg[3]
        def script(js):
            return cmd('ExecuteScript',{'script':js,'args':[],'newSandbox':True})['value']
        def until(js):
            end=time.monotonic()+35
            while time.monotonic()<end:
                val=script(js)
                if val:return val
                time.sleep(.4)
            raise RuntimeError('Timed out: '+str(script('return [document.title,location.href,document.body?.innerText.slice(0,160)]')))
        cmd('NewSession', {'capabilities':{'alwaysMatch':{'pageLoadStrategy':'none'}}})
        cmd('Navigate',{'url':'http://192.168.210.194:8789/hoseline.html'})
        until("return document.querySelector('#external')?.href === 'https://hose.brandmeister.network/?subscribe=91'")
        assert not script("return !!document.querySelector('iframe')")
        print('PASS Raspberry Firefox: dashboard Hoseline page, TG91, no external frame before click',flush=True)
        if '--external' in sys.argv:
            until("return document.querySelector('#seen')?.textContent?.includes('pistar3')")
            print('PASS independent local Last Heard comparison',flush=True)
            url=script("return document.querySelector('#external').href")
            cmd('Navigate',{'url':url})
            state=until("return /Hoseline/i.test(document.title) && /player/i.test(document.body.innerText) && [document.title,location.search]")
            print('PASS official player rendered in Pi browser:',state,'(muted; audio not measured)',flush=True)
        cmd('DeleteSession',{})
    finally:
        proc.terminate()
        try: proc.wait(timeout=8)
        except subprocess.TimeoutExpired: proc.kill(); proc.wait()
