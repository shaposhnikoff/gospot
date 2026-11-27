import test from 'node:test';
import assert from 'node:assert/strict';
import { callsign, target, rssi, lossStyle, berStyle, loss, ber, duration, liveSeconds, modeLabel, cellStyle, type Row } from './classic';

const row = (x: Partial<Row>): Row => ({ time: '2026-11-27T06:00:00Z', clock: '', mode: 'DMR Slot 2', callsign: 'UT3UDX', target: 'TG 91', source: 'Net', duration: 1.2, loss: 0, ber: 0, rssi: null, live: false, ...x });

test('callsign links follow lh.php', () => {
  assert.deepEqual(callsign('2550430'), { kind: 'id', text: '2550430', href: 'https://database.radioid.net/database/view?id=2550430' });
  assert.deepEqual(callsign('1234'), { kind: 'text', text: '1234' });
  assert.deepEqual(callsign('CQCQCQ'), { kind: 'text', text: 'CQCQCQ' });
  const c = callsign('UT3UDX-1');
  assert.equal(c.kind, 'call');
  assert.equal(c.text, 'UT3UDX');
  assert.ok(c.kind === 'call' && c.gps === 'https://aprs.fi/#!call=UT3UDX*');
  assert.equal(callsign('<b>X1').kind, 'call'); // rendered as text by Svelte, never as HTML
});

test('target, mode and number formats', () => {
  assert.equal(target('9'), ' '.repeat(7) + '9');
  assert.equal(target('TG 91'), 'TG 91');
  assert.equal(target('CQCQCQ  '), 'CQCQCQ  ');
  assert.equal(modeLabel('DMR Slot 2'), 'DMR TS2');
  assert.equal(duration(row({ duration: 3 })), '3.0');
  assert.equal(loss(row({ loss: 3 })), '3%');
  assert.equal(loss(row({ source: 'RF', loss: null })), '0%');
  assert.equal(ber(row({ ber: 0.25 })), '0.3%');
  assert.equal(liveSeconds(row({}), '2026-11-27T06:00:05Z'), '5+');
  assert.equal(liveSeconds(row({}), '2026-11-27T07:00:00Z'), '∞');
});

test('loss/BER colours and S-meter follow functions.php', () => {
  assert.equal(lossStyle(row({ loss: 0 })), '');
  assert.equal(lossStyle(row({ loss: 1 })), 'background:#1d1;');
  assert.equal(lossStyle(row({ loss: 3 })), 'background:#fa0;');
  assert.equal(lossStyle(row({ loss: 4 })), 'background:#f33;');
  assert.equal(berStyle(row({ ber: 1.5 })), 'background:#1d1;');
  assert.equal(berStyle(row({ ber: 2 })), 'background:#fa0;');
  assert.equal(berStyle(row({ ber: 5 })), 'background:#f33;');
  assert.equal(rssi(row({ rssi: -83 })), 'S9+10dB (-83)');
  assert.equal(rssi(row({ rssi: -99 })), 'S8 (-99)');
  assert.equal(rssi(row({ rssi: -140 })), 'S1 (-140)');
  assert.equal(cellStyle('warn'), 'background:#ff9; color:#030; width:50%;');
});
