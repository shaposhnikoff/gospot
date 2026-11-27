import assert from 'node:assert/strict';
import { execFileSync, spawn } from 'node:child_process';
import { existsSync } from 'node:fs';
import { createServer } from 'node:net';
import { setTimeout as delay } from 'node:timers/promises';

export async function checkOperator(origin, { preview = false, container = '' } = {}) {
  const reservation = createServer();
  await new Promise(done => reservation.listen(0, '127.0.0.1', done));
  const port = reservation.address().port;
  await new Promise(done => reservation.close(done));
  const snap = '/snap/chromium/current/usr/lib/chromium-browser/chrome';
  const executable = process.env.CHROME_BIN || (existsSync(snap) ? snap : 'google-chrome');
  const browser = spawn(process.env.CHROMEDRIVER || 'chromedriver', ['--port=' + port], { stdio: 'ignore' });
  let session;
  async function transport(method, path, body) {
    const response = await fetch(`http://127.0.0.1:${port}` + path, {
      method, headers: { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(45000)
    });
    const result = await response.json();
    if (!response.ok) throw Error(result.value?.message || `WebDriver HTTP ${response.status}`);
    return result.value;
  }
  try {
    for (let attempt = 0; attempt < 100; attempt++) {
      try { if ((await transport('GET', '/status')).ready) break; } catch {}
      await delay(100);
    }
    const capabilities = { browserName: 'chrome', pageLoadStrategy: 'eager', 'goog:chromeOptions': {
      binary: executable,
      args: ['--headless=new', '--disable-dev-shm-usage', '--disable-background-networking', '--no-first-run', '--proxy-server=http://127.0.0.1:1', '--proxy-bypass-list=<-loopback>;' + new URL(origin).host, '--host-resolver-rules=MAP localhost 127.0.0.1, MAP * ~NOTFOUND, EXCLUDE 127.0.0.1']
    } };
    if (process.getuid?.() === 0) capabilities['goog:chromeOptions'].args.push('--no-sandbox');
    session = (await transport('POST', '/session', { capabilities: { alwaysMatch: capabilities } })).sessionId;
    const webdriver = (path, body) => transport('POST', '/session/' + session + path, body);
    const command = (cmd, params) => webdriver('/goog/cdp/execute', { cmd, params });
    async function evaluate(expression) {
      return webdriver('/execute/sync', { script: 'return ' + expression, args: [] });
    }
    async function until(expression) {
      for (let attempt = 0; attempt < 100; attempt++) {
        if (await evaluate(expression)) return;
        await delay(100);
      }
      throw Error('Navigation did not reach expected state: ' + expression);
    }
    await command('Page.addScriptToEvaluateOnNewDocument', { source: "localStorage.setItem('pistar.console.stock-origin.v1','https://other-board.invalid')" });
    for (const [width, height] of [[1366, 900], [390, 844]]) {
      await command('Emulation.setDeviceMetricsOverride', { width, height, deviceScaleFactor: 1, mobile: width < 500 });
      await webdriver('/url', { url: origin + '/#console' });
      let ready = false;
      for (let attempt = 0; attempt < 100; attempt++) {
        ready = await evaluate("!!document.querySelector('[data-testid=console-link-native]')");
        if (ready) break;
        await delay(100);
      }
      assert(ready, 'Native catalog did not mount');
      const state = await evaluate(`({
        width:innerWidth, overflow:document.documentElement.scrollWidth>innerWidth,
        items:document.querySelectorAll('[data-testid^=console-item-]').length,
        externalInput:!!document.querySelector('[data-testid=console-origin]'),
        iframe:!!document.querySelector('iframe'),
        external:[...document.querySelectorAll('[data-testid=console] a')].filter(link=>link.hostname!==location.hostname).length,
        config:document.querySelector('[data-testid=console-native-configuration]')?.getAttribute('href'),
        expert:document.querySelector('[data-testid=console-native-expert]')?.getAttribute('href')
      })`);
      assert.equal(state.width, width);
      assert.equal(state.overflow, false);
      assert.equal(state.externalInput, false);
      assert.equal(state.iframe, false);
      assert.equal(state.external, 0);
      assert.equal(state.config, '/admin/configure.php');
      assert.equal(state.expert, '/admin/expert/');
      assert(state.items >= 61);
      console.log(`PASS Chromium ${width}x${height}: ${state.items} catalog entries, same-board native navigation, no external/PHP fallback or iframe, no horizontal overflow`);
      if (preview) {
        const links = await evaluate("[...document.querySelectorAll('a[href]')].map(link => link.href)");
        assert(links.every(link => new URL(link).origin === origin), 'Preview link leaves the forwarded origin');
        await evaluate("document.querySelector('[data-testid=console-native-configuration]').click()");
        await until("!!document.querySelector('input[name=cc]') && !!document.querySelector('[data-testid=admin-back-operator]')");
        assert.equal(await evaluate('location.origin'), origin);
        assert.equal(await evaluate("document.querySelector('input[name=cc]').value"), '1');
        assert.equal(await evaluate('document.documentElement.scrollWidth > innerWidth'), false);
        await evaluate("document.querySelector('[data-testid=admin-back-operator]').click()");
        await until("!!document.querySelector('[data-testid=console-native-expert]')");
        await evaluate("document.querySelector('[data-testid=console-native-expert]').click()");
        await until("!!document.querySelector('a[href=\"#expert-dmrgateway\"]')");
        await evaluate("document.querySelector('a[href=\"#expert-dmrgateway\"]').click()");
        await until("!!document.querySelector('input[name=rule0]')");
        assert.equal(await evaluate('location.origin'), origin);
        await evaluate("document.querySelector('[data-testid=admin-back-operator]').click()");
        await until("!!document.querySelector('[data-testid=console-link-native]')");
        await evaluate("document.querySelector('[data-testid=console-link-native]').click()");
        await until("!!document.querySelector('[data-testid=settings-callsign]')");
        assert.equal(await evaluate("document.querySelector('[data-testid=settings-callsign]').value"), 'DEMO1');
        await evaluate("document.querySelector('[data-testid=admin-back-operator]').click()");
        await until("!!document.querySelector('[data-testid=console-link-wizard]')");
        await evaluate("document.querySelector('[data-testid=console-link-wizard]').click()");
        await until("!!document.querySelector('input[name=callsign]')");
        assert.equal(await evaluate('location.origin'), origin);
        assert.equal(await evaluate("document.querySelector('input[name=callsign]').value"), 'DEMO1');
        console.log(`PASS Chromium ${width}x${height}: catalog -> Configuration with real Color Code -> back -> Expert DMRGateway -> back -> station settings -> wizard; all transitions stay on ${origin}`);
      }
    }
    if (preview && container) {
      assert.match(container, /^pstar-write-qa-[a-z0-9]+$/);
      const before = execFileSync('docker', ['exec', container, 'cat', '/etc/MMDVM-Host.ini']);
      const gateway = execFileSync('docker', ['exec', container, 'cat', '/etc/DMRGateway.ini']);
      const metadata = () => execFileSync('docker', ['exec', container, 'stat', '-c', '%n %u:%g %a', '/etc/MMDVM-Host.ini', '/etc/DMRGateway.ini']);
      const originalMetadata = metadata();
      const events = () => execFileSync('docker', ['exec', container, 'cat', '/run/pistar-qa/events']).toString();
      const originalEvents = events();
      await webdriver('/url', { url: origin + '/manage/#configuration' });
      await until("!!document.querySelector('input[name=cc]')");
      await evaluate(`(()=>{const field=document.querySelector('input[name=cc]');field.value='7';field.dispatchEvent(new Event('input',{bubbles:true}));return true})()`);
      await evaluate("document.querySelector('button[type=submit]').click()");
      await until("document.body.textContent.includes('Настройки сохранены.')");
      const after = execFileSync('docker', ['exec', container, 'cat', '/etc/MMDVM-Host.ini']);
      assert.deepEqual(after, Buffer.from(before.toString().replace('ColorCode=1', 'ColorCode=7')));
      assert.deepEqual(execFileSync('docker', ['exec', container, 'cat', '/etc/DMRGateway.ini']), gateway);
      assert.deepEqual(metadata(), originalMetadata);
      assert.equal(events(), originalEvents + 'mmdvmhost.service ok\n');
      await webdriver('/url', { url: origin + '/manage/#configuration' });
      await until("document.querySelector('input[name=cc]')?.value==='7'");
      await evaluate(`(()=>{const field=document.querySelector('input[name=cc]');field.value='1';field.dispatchEvent(new Event('input',{bubbles:true}));return true})()`);
      await evaluate("document.querySelector('button[type=submit]').click()");
      await until("document.body.textContent.includes('Настройки сохранены.')");
      assert.deepEqual(execFileSync('docker', ['exec', container, 'cat', '/etc/MMDVM-Host.ini']), before);
      assert.deepEqual(metadata(), originalMetadata);
      assert.equal(events(), originalEvents + 'mmdvmhost.service ok\nmmdvmhost.service ok\n');
      console.log('PASS same-origin preview write: Chromium CSRF POST Color Code 1 -> 7, independent byte comparison of both fixture INIs and permissions, reopened 7, restored 1; exactly one isolated MMDVMHost restart per save');
    }
  } finally {
    if (session) await transport('DELETE', '/session/' + session);
    if (browser.exitCode === null && browser.pid) {
      await new Promise(done => { browser.once('exit', done); browser.kill('SIGTERM'); });
    }
  }
}
