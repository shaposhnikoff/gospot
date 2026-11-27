import test from 'node:test';
import assert from 'node:assert/strict';
import { scan, BerMeter, chart, offsetOf, startFrequency, validOffset, WINDOW } from './calibration';

test('scan reads MMDVMCal output like calibration.php', () => {
  const s = scan('MMDVMCal 20240101\r\nVersion: 1, description: MMDVM_HS_Hat\r\n');
  assert.equal(s.version, true);
  assert.equal(s.mode, null);
  assert.equal(s.frequency, null);
  // Banners at the start of the chunk count too ("\n" + data).
  assert.equal(scan('BER Test Mode (FEC) for D-Star\r\n').mode, 'DStar');
  assert.equal(scan('x\r\nBER Test Mode (FEC) for DMR Simplex\r\n').mode, 'DMR');
  // The 1031 Hz pattern mode is not one of the five buttons.
  assert.equal(scan('BER Test Mode (1031 Hz Test Pattern) for DMR Simplex\r\n').mode, null);
  // Later checks win, whatever the order in the text.
  assert.equal(scan('BER Test Mode (FEC) for NXDN\r\nBER Test Mode (FEC) for YSF\r\n').mode, 'NXDN');
  assert.equal(scan('TX frequency: 435000050\r\nNew frequency: 435000100 Hz\r\n').frequency, 435000100);
  assert.equal(scan('New frequency step: 50 Hz\r\nEnter frequency (current 435000000 Hz):').frequency, null);
  assert.equal(scan('Starting GoSpot Services...\r\nFinnished...\r\n').finished, true);
  const ber = scan('D-Star audio FEC BER % (errs): 2.083% (1/48)\r\nD-Star audio FEC BER % (errs): 0.000% (0/48)\r\nD-Star voice end received, total frames: 2');
  assert.deepEqual([ber.frames, ber.errors, ber.bits, ber.eot], [2, 1, 96, true]);
});

test('offsets and the start frequency', () => {
  assert.equal(offsetOf(435000150, 435000000), '150');
  assert.equal(offsetOf(434999900, 435000000), '-100');
  assert.equal(startFrequency(435000000, -250), '434999750');
  for (const v of ['0', '-250', '150', '999999']) assert.equal(validOffset(v), true, v);
  for (const v of ['', '1.5', '1e3', '--1', '1234567', '5;reboot']) assert.equal(validOffset(v), false, v);
});

test('BER figures update every N seconds and restart after a voice end', () => {
  const m = new BerMeter();
  assert.equal(m.tick(2), false, 'no bits yet: nothing counts');
  m.add(scan('% (1/48)\n% (0/48)'));
  assert.equal(m.tick(2), false, 'first second of two');
  m.add(scan('% (2/48)'));
  assert.equal(m.tick(2), true);
  assert.deepEqual(m.current, { frames: '3', bits: '144', errors: '3', ber: '2.08%' });
  assert.deepEqual(m.total, { frames: '3', bits: '144', errors: '3', ber: '2.08%', seconds: '2' });
  assert.deepEqual(m.points, [[0, 0], [2, 3 / 144 * 100]]);
  m.add(scan('% (0/48)\nvoice end received, total'));
  assert.equal(m.tick(5), true, 'a voice end forces an update');
  assert.deepEqual(m.current, { frames: '1', bits: '48', errors: '0', ber: '0.00%' });
  assert.deepEqual(m.total, { frames: '4', bits: '192', errors: '3', ber: (3 / 192 * 100).toFixed(2) + '%', seconds: '3' });
  m.add(scan('% (48/48)'));
  assert.equal(m.tick(1), true);
  assert.deepEqual(m.total, { frames: '1', bits: '48', errors: '48', ber: '100.00%', seconds: '1' }, 'totals restarted');
});

test('chart keeps three minutes in a 0..5 % range', () => {
  const empty = chart([[0, 0]], 100, 50);
  assert.equal(empty.line, 'M0.0,50.0');
  const c = chart([[0, 0], [5, 2.5], [10, 9]], 100, 50);
  assert.equal(c.line, 'M0.0,50.0 L50.0,25.0 L100.0,0.0');
  assert.equal(c.area, c.line + ' L100.0,50 L0.0,50 Z');
  const points: [number, number][] = Array.from({ length: 300 }, (_, i) => [i, 1]);
  const late = chart(points, 100, 50);
  assert.deepEqual([late.xmin, late.xmax], [299 - WINDOW, 299]);
  assert.ok(late.line.startsWith('M0.0,40.0'));
});
