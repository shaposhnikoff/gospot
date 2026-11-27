import test from 'node:test';
import assert from 'node:assert/strict';
import { convertToChannel, convertToSecurity, ifconfigBytes, ssidOk, pskOk } from './wifi';

test('channels follow ConvertToChannel', () => {
  assert.equal(convertToChannel('2437'), '2.4GHz Ch6');
  assert.equal(convertToChannel('2484'), '2.4GHz Ch14');
  assert.equal(convertToChannel('5180'), '5.0GHz Ch36');
  assert.equal(convertToChannel('5825'), '5.0GHz Ch165');
  assert.equal(convertToChannel('2438'), 'Invalid Channel');
  assert.equal(convertToChannel('toString'), 'Invalid Channel');
});

test('security follows ConvertToSecurity and passes unknown flags through', () => {
  assert.equal(convertToSecurity('[WPA2-PSK-CCMP][ESS]'), 'WPA2-PSK (AES)');
  assert.equal(convertToSecurity('[ESS]'), 'None');
  assert.equal(convertToSecurity('[WEP][ESS]'), 'WEP');
  assert.equal(convertToSecurity('[WPA2-SAE-CCMP][ESS]'), '[WPA2-SAE-CCMP][ESS]');
  assert.equal(convertToSecurity('WPA2'), 'WPA2');
});

test('byte counters use the net-tools ifconfig format', () => {
  assert.equal(ifconfigBytes(undefined), '');
  assert.equal(ifconfigBytes(0), '0 (0.0 B)');
  assert.equal(ifconfigBytes(1023), '1023 (1023.0 B)');
  assert.equal(ifconfigBytes(2048), '2048 (2.0 KiB)');
  assert.equal(ifconfigBytes(567890), '567890 (554.5 KiB)');
  assert.equal(ifconfigBytes(5 * 1024 * 1024 + 512 * 1024), '5767168 (5.5 MiB)');
  assert.equal(ifconfigBytes(3 * 1024 ** 3), '3221225472 (3.0 GiB)');
});

test('SSID and PSK checks match the helper', () => {
  assert.ok(ssidOk('QA-Home'));
  assert.ok(ssidOk('x'.repeat(32)));
  assert.ok(ssidOk('Дом'));            // 6 bytes of UTF-8
  assert.ok(!ssidOk(''));
  assert.ok(!ssidOk('x'.repeat(33)));
  assert.ok(!ssidOk('Д'.repeat(17)));  // 34 bytes
  assert.ok(!ssidOk('a\nb'));
  assert.ok(pskOk(''));
  assert.ok(pskOk('eight888'));
  assert.ok(pskOk('p'.repeat(63)));
  assert.ok(pskOk('ab'.repeat(32)));
  assert.ok(!pskOk('seven77'));
  assert.ok(!pskOk('p'.repeat(64)));
  assert.ok(!pskOk('пароль-пароль'));
});
