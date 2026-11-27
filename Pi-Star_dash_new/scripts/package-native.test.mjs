import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, statSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { auditNative } from './audit-native.mjs';
import { packageNative, readSettings } from './package-native.mjs';

function fixture(context) {
  const root = mkdtempSync(join(tmpdir(), 'gospot-native-'));
  context.after(() => rmSync(root, { recursive: true, force: true }));
  const dist = join(root, 'dist');
  mkdirSync(dist);
  writeFileSync(join(dist, 'index.html'), '<html>native</html>', { mode: 0o600 });
  writeFileSync(join(dist, 'runtime.json'), '{"backend":"demo"}');
  return { root, dist, output: join(root, 'output') };
}

test('native package needs only frontend assets and preserves the demo input', context => {
  const setup = fixture(context);
  const release = packageNative(setup);
  const ui = join(release, 'ui');
  assert.equal(readFileSync(join(ui, 'index.html'), 'utf8'), '<html>native</html>');
  assert.deepEqual(JSON.parse(readFileSync(join(ui, 'runtime.json'))), { backend: 'gospot-go', wizardPort: Number(readSettings().MANAGEMENT_PORT) });
  assert.equal(JSON.parse(readFileSync(join(setup.dist, 'runtime.json'))).backend, 'demo');
  assert.equal(statSync(join(ui, 'index.html')).mode & 0o777, 0o644);
  assert.equal(auditNative(release), 2);
});

for (const [name, content] of [['page.php', 'text'], ['page.phtml', 'text'], ['page.inc', 'text'], ['hidden.js', '<?php echo 1;'], ['hidden.html', '<?= "secret" ?>']]) {
  test(`reject ${name} before publishing`, context => {
    const setup = fixture(context);
    writeFileSync(join(setup.dist, name), content);
    assert.throws(() => packageNative(setup), /PHP|Legacy/);
  });
}

test('reject symlinks, PHP dependencies and classic directories', context => {
  const setup = fixture(context);
  symlinkSync('/etc/passwd', join(setup.dist, 'external.js'));
  assert.throws(() => packageNative(setup), /Symlink/);
  mkdirSync(join(setup.root, 'DEBIAN'));
  writeFileSync(join(setup.root, 'DEBIAN/control'), 'Package: test\nDepends: php8.2-fpm\n');
  assert.throws(() => auditNative(setup.root), /PHP dependency/);
  mkdirSync(join(setup.root, 'classic'));
  const legacy = join(setup.output, 'classic');
  mkdirSync(legacy, { recursive: true });
  writeFileSync(join(legacy, 'page.html'), 'classic');
  assert.throws(() => auditNative(setup.output), /Legacy/);
});
