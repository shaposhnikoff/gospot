import assert from 'node:assert/strict';
import { execFileSync, spawn } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { request as httpsRequest } from 'node:https';
import { createServer } from 'node:net';
import { tmpdir, arch } from 'node:os';
import { join, resolve } from 'node:path';
import { setTimeout as delay } from 'node:timers/promises';

const root = resolve(process.argv[2]);
// Paths come from the packaged gospot.conf, which must equal the repository copy.
const packagedSettings = readFileSync(join(root, 'etc/gospot/gospot.conf'), 'utf8');
assert.equal(packagedSettings, readFileSync(new URL('../gospot.conf', import.meta.url), 'utf8'));
const settings = Object.fromEntries(packagedSettings.split('\n').map(l => l.trim())
  .filter(l => l && !l.startsWith('#')).map(l => [l.slice(0, l.indexOf('=')), l.slice(l.indexOf('=') + 1)]));
const binary = join(root, settings.DASHBOARD_LIB_DIR, 'panel');
const ui = join(root, settings.DASHBOARD_UI_DIR);
const work = mkdtempSync(join(tmpdir(), 'gospot-package-test-'));
// The unpacked package lives under `root`; point the copy of the settings there.
const testSettings = join(work, 'gospot.conf');
writeFileSync(testSettings, packagedSettings
  .replace(/^DASHBOARD_UI_DIR=.*$/m, 'DASHBOARD_UI_DIR=' + ui)
  .replace(/^DASHBOARD_LIB_DIR=.*$/m, 'DASHBOARD_LIB_DIR=' + join(root, settings.DASHBOARD_LIB_DIR)));
const children = [];
const runner = arch() === 'arm' ? binary : 'qemu-arm-static';
const prefix = arch() === 'arm' ? [] : [binary];

async function freePort() {
  const server = createServer();
  await new Promise((done, reject) => server.once('error', reject).listen(0, '127.0.0.1', done));
  const port = server.address().port;
  await new Promise(done => server.close(done));
  return port;
}

function start(args, env = process.env) {
  const child = spawn(runner, [...prefix, ...args], { env, stdio: ['ignore', 'ignore', 'pipe'] });
  let failure = '';
  child.stderr.on('data', chunk => failure += chunk.toString());
  child.on('error', error => failure = error.message);
  children.push(child);
  return () => {
    if (failure || child.exitCode !== null) throw Error(`Native server failed: ${failure}`);
  };
}

async function ready(probe, check) {
  for (let attempt = 0; attempt < 100; attempt++) {
    check();
    try { return await probe(); } catch { await delay(100); }
  }
  throw Error('Native server did not start');
}

try {
  const port = await freePort();
  const origin = `http://127.0.0.1:${port}`;
  const check = start(['-config', testSettings, '-operator-listen', `127.0.0.1:${port}`]);
  await ready(() => fetch(origin + '/health'), check);
  const index = readFileSync(join(ui, 'index.html'));
  assert.deepEqual(Buffer.from(await (await fetch(origin)).arrayBuffer()), index);
  const runtime = await (await fetch(origin + '/runtime.json')).json();
  assert.equal(runtime.backend, 'gospot-go');
  assert.equal(runtime.classicHttpsPort, undefined);
  const resources = readdirSync(join(ui, 'assets')).map(name => './assets/' + name);
  assert(resources.length > 0);
  for (const resource of resources) {
    const response = await fetch(new URL(resource, origin));
    assert.equal(response.status, 200);
    assert.deepEqual(Buffer.from(await response.arrayBuffer()), readFileSync(join(ui, resource)));
  }
  assert.equal((await fetch(origin + '/not-installed.php')).status, 404);
  assert.equal((await fetch(origin + '/', { method: 'POST' })).status, 405);
  const redirect = await fetch(origin + '/admin/configure.php', { redirect: 'manual' });
  assert.equal(redirect.headers.get('location'), `https://127.0.0.1:${settings.MANAGEMENT_PORT}/manage/#configuration`);
  console.log(`PASS exact ARM binary: native operator HTML and ${resources.length} resources match package bytes; runtime, redirects and method restrictions verified`);

  const password = randomBytes(24).toString('hex');
  writeFileSync(join(work, 'admin-password'), password, { mode: 0o600 });
  const key = join(work, 'key.pem');
  const cert = join(work, 'cert.pem');
  execFileSync('openssl', ['req', '-x509', '-nodes', '-newkey', 'rsa:2048', '-days', '1', '-subj', '/CN=127.0.0.1', '-addext', 'subjectAltName=IP:127.0.0.1', '-keyout', key, '-out', cert], { stdio: 'ignore' });
  const adminPort = await freePort();
  const checkAdmin = start(['-config', testSettings, '-admin-listen', `127.0.0.1:${adminPort}`, '-tls-cert', cert, '-tls-key', key], { ...process.env, CREDENTIALS_DIRECTORY: work });
  function management(path, authenticated) {
    return new Promise((done, reject) => {
      const request = httpsRequest({ hostname: '127.0.0.1', port: adminPort, path, ca: readFileSync(cert),
        headers: authenticated ? { Authorization: 'Basic ' + Buffer.from('gospot:' + password).toString('base64') } : {} }, response => {
        const chunks = [];
        response.on('data', chunk => chunks.push(chunk));
        response.on('end', () => done({ status: response.statusCode, body: Buffer.concat(chunks) }));
      });
      request.setTimeout(3000, () => request.destroy(Error('HTTPS timeout')));
      request.on('error', reject);
      request.end();
    });
  }
  const unauthorized = await ready(() => management('/manage/', false), checkAdmin);
  assert.equal(unauthorized.status, 401);
  const authorized = await management('/manage/', true);
  assert.equal(authorized.status, 200);
  assert.deepEqual(authorized.body, index);
  for (const resource of resources) {
    const result = await management('/manage/' + resource.slice(2), true);
    assert.equal(result.status, 200);
    assert.deepEqual(result.body, readFileSync(join(ui, resource)));
  }
  console.log('PASS exact ARM binary: authenticated HTTPS management HTML/assets match package; anonymous request rejected; no PHP process or radio command used');
  if (process.env.NATIVE_BROWSER === '1') {
    const { checkOperator } = await import('../qa/native_package_browser.mjs');
    await checkOperator(origin, work);
  }
} finally {
  await Promise.all(children.map(child => new Promise(done => {
    if (child.exitCode !== null || !child.pid) return done();
    child.once('exit', done);
    child.kill('SIGTERM');
  })));
  rmSync(work, { recursive: true, force: true });
}
