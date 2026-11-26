// Unit + real-tree packaging: node --test scripts/package-overlay.test.mjs
// Also drive headless Chromium: CLASSIC_BROWSER=1 node --test scripts/package-overlay.test.mjs
import test from 'node:test';
import assert from 'node:assert/strict';
import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import { createServer } from 'node:http';
import { mkdtempSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs';
import { join, dirname, resolve } from 'node:path';
import { tmpdir } from 'node:os';
import { fileURLToPath } from 'node:url';
import { enhanceHead, packageOverlay } from './package-overlay.mjs';

const project = fileURLToPath(new URL('../', import.meta.url));
const upstream = resolve(project, '../Pi-Star_DV_Dash');
const skin = '<link rel="stylesheet" href="/css/pistar-modern.css" />';
const viewport = '<meta name="viewport" content="width=device-width, initial-scale=1" />';
const urls = { css: '/operator/classic/shell-fixture.css', js: '/operator/classic/shell-fixture.js' };
const doc = `<html><head>${skin}</head><body>café</body></html>`;
const text = (root, path) => readFileSync(join(root, path), 'latin1');

test('only full classic document heads are enhanced; keep original bytes and viewport', () => {
  const transform = (file, html) => enhanceHead(file, Buffer.from(html, 'latin1'), urls).toString('latin1');
  const fixture = `<?php echo '${doc}'; ?><!-- ${doc} --><script>const sample = '${doc}'</script>` +
    doc.replace('<head>', `<head>${viewport}`);
  const result = transform('index.html', fixture);
  assert.equal(result.replace(/<link rel="stylesheet" data-pistar-classic=.*?<\/script>/, ''), fixture);
  assert.equal(result.split(viewport).length, 2);
  assert.equal(transform('index.html', result), result);
  for (const file of ['api/last_heard.php', 'config/csrf.php', 'mmdvmhost/lh.php',
    'admin/wifi/vendor/index.html', 'admin/download_modem_log.php']) assert.equal(transform(file, doc), doc);
  for (const partial of [`<head>${skin}</head><body>`, `<?php echo '${doc}'; ?>`,
    `<!-- ${doc} -->`, `<textarea>${doc}</textarea>`, doc.replace('<head>', '<head><base href="//other.invalid/">')]) {
    assert.equal(transform('admin/configure.php', partial), partial);
  }
});

test('packaged classic tree: byte-level coverage and optional loopback browser', async t => {
  if (process.env.CLASSIC_DOCROOT) {
    assert.equal(process.env.CLASSIC_BROWSER, '1', 'CLASSIC_DOCROOT requires a browser run');
    return runBrowserTest(t, resolve(process.env.CLASSIC_DOCROOT));
  }
  const dist = mkdtempSync(join(tmpdir(), 'pistar-dist-'));
  t.after(() => rmSync(dist, {recursive:true, force:true}));
  writeFileSync(join(dist, 'index.html'), 'Operator fixture', {mode:0o600});
  const { root, enhanced } = await packageOverlay({ dist });
  assert.equal(statSync(join(root,'operator/index.html')).mode & 0o777, 0o644);
  t.after(() => rmSync(dirname(root), {recursive:true, force:true}));
  const assets = readdirSync(join(root, 'operator/classic'));
  assert.equal(assets.length, 2);
  const [css, js] = ['css', 'js'].map(ext => {
    const name = assets.find(file => file.endsWith('.' + ext));
    assert.ok(name);
    return '/operator/classic/' + name;
  });
  const tags = `<link rel="stylesheet" data-pistar-classic="" href="${css}" /><script type="module" defer src="${js}"></script>`;
  // Discover actual full documents independently of the packager's allowlist.
  const originals = [];
  for (const dir of ['', 'admin', 'admin/expert']) {
    for (const entry of readdirSync(join(upstream, dir), {withFileTypes:true})) {
      if (!entry.isFile() || !entry.name.endsWith('.php')) continue;
      const page = join(dir, entry.name);
      if (/<!doctype html\b/i.test(text(upstream, page))) originals.push(page);
    }
  }
  assert.deepEqual(enhanced.sort(), originals.sort());
  for (const page of [...enhanced, 'admin/index.php']) {
    const output = text(root, page);
    assert.equal(output.split(tags).length, 2, page);
    const stripped = output.replace(tags, '').replace(viewport, '').replace(skin, '');
    const original = text(upstream, page);
    if (page === 'admin/wifi.php' || /^admin\/expert\/fulledit_(dmrgateway|wpaconfig|pistar-remote|rssidat)\.php$/.test(page) || /^admin\/expert\/edit_(starnetserver|timeserver|dstarrepeater|mmdvmhost|(?:dmr|nxdn|ysf|p25|dapnet|ircddb)gateway)\.php$/.test(page) || page === 'admin/config_backup.php' ||
        page === 'admin/configure.php') {
      // Release-only write handlers intentionally correct lossy saves/restores.
      // Their save/reload behavior is covered by qa/classic-write/run.sh.
      assert.notEqual(stripped, original, page);
      const head = html => html.match(/<head>[\s\S]*?<\/head>/i)[0];
      const expectedHead = page.includes('expert')
        ? head(original).replace('charset=iso-8859-1','charset=UTF-8')
        : head(original).replace('wifi/functions.js?version=1.6','wifi/functions.js?version=classic-profile-1');
      assert.equal(head(stripped), expectedHead, page);
      assert.ok(stripped.includes('csrf_verify();') && stripped.includes('pistar_warnings_enforce_redirect();'), page);
      if (page === 'admin/config_backup.php') {
        const targets = s => s.match(/function backup_files\(\)[\s\S]+?\n\}/)[0];
        assert.equal(targets(stripped), targets(original), 'restore target list must match upstream');
      }
      if (page === 'admin/configure.php') {
        // The packaged callsign path must remain behind both upstream gates;
        // all four other configuration actions keep their actual POST forms.
        const forms = html => html.match(/<form\b[^>]*\bmethod="post"[^>]*>/gi);
        assert.deepEqual(forms(stripped), forms(original));
        assert.equal(statSync(join(root,'admin/callsign-apply.php')).mode & 0o777, 0o644);
        const stop = 'pistar_warnings_enforce_redirect();';
        const gates = html => html.slice(0,html.indexOf(stop)+stop.length);
        assert.equal(gates(stripped), gates(original));
      }
    } else if (page === 'admin/expert/fulledit_tgifapikey.php') {
      const submit = original.slice(original.indexOf('//after the form submit'), original.indexOf('//this is the function going to update'));
      assert.ok(stripped.includes(submit) && stripped.indexOf(submit) > stripped.indexOf('return $success;'));
      assert.equal(stripped.replace(submit, ''), original.replace(submit, ''), 'only TGIF submit order changes');
    } else assert.equal(stripped, original, page);
  }
  for (const page of ['config/csrf.php', 'api/last_heard.php', 'admin/admin.php', 'dstarrepeater/system.php']) {
    assert.ok(!text(root, page).includes('/operator/classic/'), page);
  }
  const headers = text(root, 'config/security_headers.php');
  assert.equal(headers, text(upstream, 'config/security_headers.php'));
  for (const policy of ['script', 'style']) assert.equal(headers.split(`${policy}-src 'self'`).length - 1, 3);

  await runBrowserTest(t, root);
});

async function runBrowserTest(t, root) {
  await t.test('browser: stock Live Logs polling, iframe, search and confirmed native POST',
    { skip: process.env.CLASSIC_BROWSER !== '1' }, async t => {
    const assets = readdirSync(join(root, 'operator/classic'));
    const [js, css] = ['js', 'css'].map(ext => {
      const matches = assets.filter(name => name.endsWith('.' + ext));
      assert.equal(matches.length, 1);
      return '/operator/classic/' + matches[0];
    });
    t.diagnostic(`Browser source: ${root}; assets: ${js}, ${css}`);
    // The server serves inert fixture responses only. Use the packaged Live Logs
    // document's literal HTML, including its actual inline poller + jQuery,
    // and the stock power form with only PHP_SELF and csrf_field() substituted.
    const logs = text(root, 'admin/live_modem_log.php').match(/<!DOCTYPE html[\s\S]*?<\/html>/i)[0]
      .replace(/<\?php[\s\S]*?\?>/g, '');
    const power = text(root, 'admin/power.php');
    const head = power.match(/<!DOCTYPE html[\s\S]*?<body>/i)[0].replace(/<\?php[\s\S]*?\?>/g, '');
    const wifiHead = text(root, 'admin/wifi.php').match(/echo '(<!DOCTYPE html[^']*?<body>)'/)[1];
    const form = power.match(/<form action=[\s\S]*?<\/form>/)[0]
      .replace(/<\?php echo htmlspecialchars\(\$_SERVER\["PHP_SELF"\]\); \?>/, '/admin/power.php')
      .replace('<?php csrf_field(); ?>', '<input type="hidden" name="csrf_token" value="fixture-token">')
      .replace(/<\?php[\s\S]*?\?>/g, '');
    const posts = [], polls = [];
    const fixtures = new Map([
      [js, 'text/javascript'], [css, 'text/css'], ['/css/pistar-modern.css', 'text/css'],
      ['/jquery.min.js', 'text/javascript'], ['/jquery-timing.min.js', 'text/javascript'],
    ]);
    const server = createServer(async (req, res) => {
      const path = req.url;
      res.setHeader('Content-Security-Policy', "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self'; connect-src 'self'; frame-ancestors 'self'");
      res.setHeader('X-Content-Type-Options', 'nosniff');
      if (fixtures.has(path)) {
        res.setHeader('Content-Type', fixtures.get(path));
        return res.end(readFileSync(join(root, path)));
      }
      res.setHeader('Content-Type', 'text/html; charset=utf-8');
      if (path === '/admin/power.php' && req.method === 'POST') {
        let body = ''; for await (const chunk of req) body += chunk;
        posts.push({cookie:req.headers.cookie, origin:req.headers.origin, body});
        const ok = req.headers.cookie === 'session=local' && new URLSearchParams(body).get('csrf_token') === 'fixture-token';
        res.statusCode = ok ? 200 : 403;
        return res.end(ok ? '<p id="receipt">Local fixture received POST</p>' : 'Forbidden');
      }
      if (path === '/admin/power.php') {
        res.setHeader('Set-Cookie', 'session=local; Path=/; HttpOnly; SameSite=Strict');
        return res.end(head + form + '<iframe src="/admin/wifi.php?page=wlan0_info"></iframe></body></html>');
      }
      if (path === '/admin/wifi.php?page=wlan0_info') return res.end(wifiHead + 'Wi-Fi fixture</body></html>');
      if (path === '/admin/live_modem_log.php') return res.end(logs);
      if (path === '/admin/live_modem_log.php?ajax') {
        polls.push(req.headers['x-requested-with']);
        return res.end(`Fixture line ${polls.length}<br>`);
      }
      res.statusCode = 404; res.end();
    });
    await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
    t.after(() => {server.closeAllConnections(); server.close();});
    const origin = `http://127.0.0.1:${server.address().port}`;
    // Reuse the repository's isolated loopback ChromeDriver setup.
    const script = `
import sys
from smoke import browser, wait
origin = sys.argv[1]
with browser(target_origin=origin) as command:
    js = lambda s: command("/execute/sync", {"script": s, "args": []})
    go = lambda p: command("/url", {"url": origin + p})
    root = "document.querySelector('#pistar-classic-shell').shadowRoot"
    command("/goog/cdp/execute", {"cmd":"Emulation.setDeviceMetricsOverride",
        "params":{"width":390,"height":844,"deviceScaleFactor":1,"mobile":True}})
    go('/admin/power.php')
    assert js("return innerWidth") == 390
    assert js("return getComputedStyle(" + root + ".querySelector('nav')).backgroundColor") == 'rgb(39, 35, 49)'
    assert js("return document.forms.length === 1 && document.querySelectorAll('input').length === 1")
    print('PASS mobile viewport 390px, shell stylesheet and original form')
    assert js("const d=document.querySelector('iframe').contentDocument; return d.body.innerText.includes('Wi-Fi fixture') && !d.getElementById('pistar-classic-shell')")
    print('PASS loaded Wi-Fi iframe has no duplicate shell')
    assert js("return [..."+root+".querySelectorAll('a')].every(a => a.origin === location.origin)")
    print('PASS shell links are same-origin')
    js(root + ".querySelector('summary').click()")
    js("const input=" + root + ".querySelector('input'); input.value='живой журнал'; input.dispatchEvent(new Event('input',{bubbles:true}));")
    wait('catalog link missing', lambda: js("return !!"+root+".querySelector('li a[href=\\"/admin/live_modem_log.php\\"]')"))
    print('PASS catalog search: живой журнал -> /admin/live_modem_log.php')
    js(root+".querySelector('li a').click()")
    wait('stock AJAX poller did not append two lines', lambda: js("return document.querySelector('#tail')?.innerText.includes('Fixture line 2')"))
    assert js("return "+root+".querySelector('a[aria-current]').pathname") == '/admin/live_modem_log.php'
    assert js("return performance.getEntriesByType('resource').every(e=>new URL(e.name).origin === location.origin)")
    print('PASS Live Logs appended second fixture line; active link and resource origins verified')
    go('/admin/wifi.php?page=wlan0_info')
    assert js("return !!"+root+".querySelector('nav')")
    print('PASS standalone Wi-Fi document has navigation')
    for block_module in (False, True):
        if block_module:
            command("/goog/cdp/execute", {"cmd":"Network.enable","params":{}})
            command("/goog/cdp/execute", {"cmd":"Network.setBlockedURLs","params":{"urls":["*/operator/classic/*.js"]}})
        go('/admin/power.php')
        assert js("return !!document.getElementById('pistar-classic-shell')") != block_module
        button = js("return document.querySelector('button[value=reboot]')")
        command("/element/"+button["element-6066-11e4-a52e-4f735466cecf"]+"/click", {})
        command("/alert/accept", {})
        wait('native POST did not navigate', lambda: js("return !!document.getElementById('receipt')"))
        print('PASS confirmed native POST received with shell module ' + ('blocked' if block_module else 'loaded'))
`;
    const {stdout} = await promisify(execFile)('python3', ['-B', '-c', script, origin], { cwd: join(project, 'qa'), timeout: 60_000 });
    stdout.trim().split('\n').forEach(line => t.diagnostic(line));
    assert.ok(polls.length >= 2 && polls.every(value => value === 'XMLHttpRequest'));
    t.diagnostic(`PASS server counted ${polls.length} AJAX polls with XMLHttpRequest header`);
    const expected = { cookie: 'session=local', origin, body: 'csrf_token=fixture-token&action=reboot' };
    assert.deepEqual(posts, [expected, expected]);
    t.diagnostic('PASS server verified both POSTs: same-origin, session cookie, CSRF field and action=reboot (inert receipt)');
  });
}
