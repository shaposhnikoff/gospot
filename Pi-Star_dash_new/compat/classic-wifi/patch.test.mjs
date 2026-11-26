import assert from 'node:assert/strict';
import test from 'node:test';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { runInNewContext } from 'node:vm';
import { patchWifi } from './patch.mjs';

test('patch only reviewed packaged sources; keep stable indexes through delete/add/scan', t => {
  const root = mkdtempSync(join(tmpdir(), 'classic-wifi-test-'));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, 'admin/wifi'), { recursive: true });
  const upstream = new URL('../../../Pi-Star_DV_Dash/admin/', import.meta.url);
  const page = join(root, 'admin/wifi.php'), script = join(root, 'admin/wifi/functions.js');
  const original = readFileSync(new URL('wifi.php', upstream), 'utf8');
  writeFileSync(page, original);
  writeFileSync(script, readFileSync(new URL('wifi/functions.js', upstream)));
  assert.throws(() => patchWifi(root), /Unexpected classic/);
  writeFileSync(page, original.replace('</head>', '<link rel="stylesheet" href="/css/pistar-modern.css" /></head>'));
  patchWifi(root);
  assert.throws(() => patchWifi(root), /Unexpected classic/);
  const packaged = readFileSync(page, 'utf8');
  assert.deepEqual(packaged.match(/sudo [^'\n]+/g)?.filter(s => !original.includes(s)), []);
  assert.ok(packaged.includes('classic_wifi_save($doc, $_POST)'));
  assert.ok(!packaged.includes('$config = "ctrl_interface='));
  assert.ok(packaged.includes('id="wpa_conf_form" onsubmit="UpdateNetworks()"'));

  const rows = new Map();
  const next = { value: 3, dataset: { next: '3' } };
  const box = {
    getElementsByTagName: () => [...rows.values()],
    removeChild(el) { rows.delete(el.id); },
    insertAdjacentHTML(where, html) {
      assert.equal(where, 'beforeend');
      const id = Number(/id="Networkbox(\d+)"/.exec(html)[1]);
      assert.equal(rows.has(id), false);
      rows.set(id, { id, parentNode: box });
    },
  };
  for (let id = 0; id < 3; id++) rows.set(id, { id, parentNode: box });
  const document = { getElementById(id) {
    if (id === 'networkbox') return box;
    if (id === 'Networks') return next;
    return rows.get(Number(id.match(/\d+$/)[0]));
  } };
  runInNewContext(readFileSync(script, 'utf8') +
    "\nCheckSSID(field); CheckPSK(field); DeleteNetwork(1); AddNetwork(); AddScanned('scanned'); UpdateNetworks();",
    { document, field: {value: 'a'.repeat(32), style: {}, setCustomValidity(s) { assert.equal(s, ''); }} });
  assert.deepEqual([...rows.keys()], [0, 2, 3, 4]);
  assert.equal(rows.get(4).value, 'scanned');
  assert.equal(next.value, 4);
});
