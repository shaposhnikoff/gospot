import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import * as fs from 'node:fs';
import path from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';

const builder = fileURLToPath(new URL('build-classic.mjs', import.meta.url));
const prototype = path.dirname(path.dirname(builder));
const upstream = path.resolve(prototype, '../Pi-Star_DV_Dash');
const asset = 'css/pistar-modern.css';
const link = `<link rel="stylesheet" href="/${asset}" />`;

function write(file, data) {
  fs.mkdirSync(path.dirname(file), { recursive: true });
  fs.writeFileSync(file, data);
}

function fixture(t) {
  // Keep even test builds inside the local prototype. Only remove this test's
  // own mkdtemp directory; never clean up a caller-supplied output.
  const root = fs.mkdtempSync(path.join(prototype, '.classic-test-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const project = path.join(root, 'prototype');
  const script = path.join(project, 'scripts/build-classic.mjs');
  const css = path.join(project, 'compat/pistar-modern.css');
  write(script, fs.readFileSync(builder));
  write(css, 'body { color: navy; }\n');
  const source = path.join(root, 'Pi-Star_DV_Dash');
  const out = path.join(project, 'build/overlay/docroot');
  const run = (...args) => spawnSync(process.execPath, [script, ...args], { cwd: project, encoding: 'utf8' });
  return { project, source, out, run, css };
}

function ok(result) { assert.equal(result.status, 0, result.error?.message ?? result.stderr); }
function refused(result) { assert.equal(result.status, 1, result.stdout); }

function names(root, sub = '') {
  return fs.readdirSync(path.join(root, sub), { withFileTypes: true }).flatMap(item => {
    if (item.name === '.git') return [];
    const rel = path.join(sub, item.name);
    return item.isDirectory() ? [rel, ...names(root, rel)] : [rel];
  });
}

test('real upstream: complete route tree, exact PHP/asset parity except the final same-origin style', t => {
  const { out, run, css } = fixture(t);
  fs.copyFileSync(path.join(prototype, 'compat/pistar-modern.css'), css);
  ok(run('--source', upstream));
  const originals = names(upstream);
  assert.deepEqual(names(out).sort(), [...originals, asset].sort());
  assert.equal(fs.existsSync(path.join(out, '.git')), false);
  assert.deepEqual(fs.readFileSync(path.join(out, asset)), fs.readFileSync(css));
  for (const name of originals) {
    const src = path.join(upstream, name), dst = path.join(out, name);
    const s = fs.lstatSync(src), d = fs.lstatSync(dst);
    assert.equal(d.mode & 0o170000, s.mode & 0o170000, name);
    if (s.isSymbolicLink()) {
      assert.equal(fs.readlinkSync(dst), fs.readlinkSync(src), name);
    } else if (s.isFile()) {
      const before = fs.readFileSync(src), after = fs.readFileSync(dst);
      // This upstream revision has literal closing tags in templates, a PHP
      // echo in Wi-Fi, and the shared CSRF helper's 403. No PHP is executed.
      if (/\.(php|html?|phtml|inc)$/i.test(name) && /<\/head\s*>/i.test(before.toString('latin1'))) {
        assert.equal(after.toString('latin1'), before.toString('latin1').replace(/<\/head\s*>/gi, link + '$&'), name);
      } else {
        assert.deepEqual(after, before, name);
      }
    }
  }
  for (const route of ['/', '/admin/', '/admin/configure.php', '/admin/expert/', '/admin/wifi.php']) {
    const url = new URL(route, 'https://pistar.test');
    const file = path.join(out, url.pathname, route.endsWith('/') ? 'index.php' : '');
    const href = /<link rel="stylesheet" href="([^"]+)" \/><\/head>/i.exec(fs.readFileSync(file, 'latin1'))?.[1];
    assert.equal(new URL(href, url).href, `https://pistar.test/${asset}`, route);
  }
});

test('includes, dotfiles, empty directories, permissions and config preservation across builds', t => {
  const { source, out, run } = fixture(t);
  write(path.join(source, 'layout/head.inc'), '<head><title>Pi-Star</title></head>');
  write(path.join(source, 'index.php'), '<?php include "layout/head.inc"; ?>');
  write(path.join(source, 'partial.php'), '<?php /* example: </head> */ ?>' +
    '<script>const sample = "</head>";</script><div>RF</div>');
  write(path.join(source, 'layout/.git/config'), 'omit');
  write(path.join(source, '.htaccess'), 'Options -Indexes');
  write(path.join(source, 'config.php'), 'user-default');
  fs.chmodSync(path.join(source, 'config.php'), 0o640);
  fs.mkdirSync(path.join(source, 'empty'));
  fs.symlinkSync('../index.php', path.join(source, 'layout/home.php'));
  ok(run());
  ok(run());
  assert.equal(fs.readFileSync(path.join(out, 'layout/head.inc'), 'utf8'), `<head><title>Pi-Star</title>${link}</head>`);
  assert.deepEqual(fs.readFileSync(path.join(out, 'partial.php')), fs.readFileSync(path.join(source, 'partial.php')));
  assert.equal(fs.existsSync(path.join(out, 'layout/.git')), false);
  assert.deepEqual(names(out).sort(), [...names(source), 'css', asset].sort());
  assert.equal(fs.statSync(path.join(out, 'config.php')).mode & 0o777, 0o640);
  write(path.join(out, 'config.php'), 'user-owned');
  refused(run());
  assert.equal(fs.readFileSync(path.join(out, 'config.php'), 'utf8'), 'user-owned');
});

test('reject external/overlapping roots, redirected output, indirect link escapes and asset collisions', t => {
  const { source, project, out, run } = fixture(t);
  write(path.join(source, 'index.php'), '<head></head>');
  for (const dir of [project, source, '/']) refused(run('--out', dir));
  refused(run('--source', project));
  fs.symlinkSync(source, path.join(project, 'build'));
  refused(run());
  fs.unlinkSync(path.join(project, 'build'));
  fs.symlinkSync(source, path.join(project, 'alias'));
  refused(run('--source', path.join(project, 'alias')));
  fs.mkdirSync(path.join(source, 'sub'));
  fs.symlinkSync('..', path.join(source, 'sub/back'));
  fs.symlinkSync('sub/back/../outside', path.join(source, 'unsafe'));
  refused(run());
  fs.unlinkSync(path.join(source, 'unsafe'));
  write(path.join(source, asset), 'upstream theme');
  refused(run());
  assert.equal(fs.existsSync(out), false);
  assert.equal(fs.existsSync(path.join(source, 'overlay')), false);
});
