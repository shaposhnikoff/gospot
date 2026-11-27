import test from 'node:test';
import assert from 'node:assert/strict';
import type { Dashboard, Row } from './classic';
import { accept, ago, asDashboard, clock, fail, initialConn, isLost, kioskView, recent, resume, span, LOST_AFTER_MS, RECENT_CALLS } from './kiosk';

const T0 = Date.parse('2026-11-27T06:00:00Z');

const row = (x: Partial<Row>): Row => ({
  time: '2026-11-27T05:50:00Z', clock: '07:50:00 Nov 27th', mode: 'DMR Slot 2', callsign: 'UT3UDX', target: 'TG 255', source: 'RF',
  duration: 3.2, loss: null, ber: 0.1, rssi: -83, live: false, suffix: '', note: '', name: 'Max', targetName: 'UKRAINE', ...x,
});

const dash = (heard: Row[], x: Partial<Dashboard> = {}): Dashboard => ({
  Hostname: 'pistar3', Callsign: 'UT3UDX', Version: 'v1', PiStar: '4.3', Controller: 'mmdvmhost', Modes: [], Networks: [],
  TRX: { kind: 'listening', mode: '', label: 'Listening' }, TX: '433.800000 MHz', RX: '433.800000 MHz', Firmware: '', TCXO: '',
  DStar: null, DMR: null, YSF: null, P25: null, NXDN: null, M17: null, POCSAG: null,
  Heard: heard, Local: heard.filter(r => r.source === 'RF'), HeardReady: true, TimeZone: 'EET', Now: new Date(T0).toISOString(), ...x,
});

// A JSON round trip, as the browser receives it.
const wire = (d: Dashboard) => JSON.parse(JSON.stringify(d)) as unknown;

test('asDashboard accepts the Go shape and rejects anything else', () => {
  assert.ok(asDashboard(wire(dash([row({})]))));
  assert.ok(asDashboard(wire(dash([], { Heard: null, Local: null }))));
  // Rows without the name fields (an older backend) are still calls.
  const old = wire(dash([row({})])) as { Heard: Record<string, unknown>[] };
  delete old.Heard[0].name; delete old.Heard[0].targetName;
  assert.ok(asDashboard(old));
  for (const bad of [null, [], 'x', { Now: 'x' }, { ...dash([]), Now: 'not a date' }, { ...dash([]), TRX: null },
    { ...dash([]), Heard: [{ callsign: 1 }] }, { ...dash([]), Heard: {} }, { ...dash([row({ duration: 'x' as never })]) }]) {
    assert.equal(asDashboard(bad), null, JSON.stringify(bad));
  }
});

test('connecting until the first reply; a failure before any data is lost', () => {
  const c = initialConn(T0);
  assert.deepEqual(kioskView(c, T0), { state: 'connecting' });
  assert.equal(kioskView(fail(c, T0 + 100), T0 + 100).state, 'lost');
  // Invalid JSON counts as a failed poll.
  assert.equal(accept(c, { nope: true }, T0).failures, 1);
});

test('idle: listening, last call N min ago from the server clock', () => {
  // The client clock is 90 s behind the board; the board's Now is used.
  const client = T0 - 90_000;
  const c = accept(initialConn(client), wire(dash([row({})])), client);
  const v = kioskView(c, client + 1000);
  assert.equal(v.state, 'idle');
  if (v.state !== 'idle') return;
  // 05:50:00 + 3.2 s ended 9 min 56.8 s before 06:00:01.
  assert.equal(v.ago, '9 min ago');
  assert.equal(v.offline, false);
  assert.equal(v.history, true);
  assert.deepEqual(v.station, { callsign: 'UT3UDX', freq: '433.800000 MHz' });
  assert.equal(v.last?.callsign, 'UT3UDX');
  assert.equal(v.last?.name, 'Max');
  assert.equal(v.last?.signal, 'S9+10dB (-83)');
  const none = kioskView(accept(initialConn(T0), wire(dash([])), T0), T0);
  assert.ok(none.state === 'idle' && none.ago === null && none.last === null && none.recent.length === 0);
  const off = kioskView(accept(initialConn(T0), wire(dash([], { TRX: { kind: 'offline', mode: '', label: 'OFFLINE' }, HeardReady: false })), T0), T0);
  assert.ok(off.state === 'idle' && off.offline && !off.history);
  const duplex = kioskView(accept(initialConn(T0), wire(dash([], { TX: '438.800000 MHz', RX: '431.200000 MHz' })), T0), T0);
  assert.ok(duplex.state === 'idle' && duplex.station.freq === 'TX 438.800000 MHz · RX 431.200000 MHz');
});

test('on air: live call card with a running duration counter', () => {
  const live = row({ time: '2026-11-27T05:59:18Z', live: true, duration: null, ber: null, rssi: null, source: 'Net', callsign: '2341234', name: '', target: 'TG 91', targetName: 'WORLD-WIDE', mode: 'DMR Slot 1' });
  const client = T0 + 5_000; // client clock 5 s ahead of the board
  let c = accept(initialConn(client), wire(dash([live, row({})])), client);
  let v = kioskView(c, client);
  assert.equal(v.state, 'onair');
  if (v.state !== 'onair') return;
  assert.deepEqual({ ...v.call }, {
    callsign: '2341234', name: '', target: 'TG 91', targetName: 'WORLD-WIDE', mode: 'DMR', slot: 'TS1', source: 'Net',
    sourceLabel: 'Network', seconds: 42, elapsed: '0:42', kind: 'tx',
  });
  assert.equal(v.more, 0);
  // The counter advances with the client clock between polls.
  v = kioskView(c, client + 2_500);
  assert.ok(v.state === 'onair' && v.call.elapsed === '0:44');
  // The live row is not repeated in the list.
  assert.ok(v.state === 'onair' && v.recent.length === 1 && v.recent[0].callsign === 'UT3UDX');
  // RF call: received from a local radio.
  const rf = row({ time: '2026-11-27T05:59:59Z', live: true, duration: null, mode: 'D-Star', suffix: 'ID51', target: 'CQCQCQ  ', name: 'Ann', callsign: 'M1ABC' });
  c = accept(c, wire(dash([rf, live])), client + 3000);
  v = kioskView(c, client + 3000);
  assert.ok(v.state === 'onair');
  if (v.state !== 'onair') return;
  assert.equal(v.call.kind, 'rx');
  assert.equal(v.call.callsign, 'M1ABC/ID51');
  assert.equal(v.call.target, 'CQCQCQ');
  assert.equal(v.call.mode, 'D-Star');
  assert.equal(v.call.slot, '');
  assert.equal(v.call.sourceLabel, 'RF');
  assert.equal(v.more, 1);
  // The call ends: back to idle with "just now".
  c = accept(c, wire(dash([{ ...rf, live: false, duration: 1.5 }])), client + 4000);
  v = kioskView(c, client + 4000);
  assert.ok(v.state === 'idle' && v.ago === 'just now');
  assert.equal(clock(3725), '1:02:05');
  assert.equal(clock(-3), '0:00');
});

test('connection lost after 5 s without a reply, and recovery', () => {
  let c = accept(initialConn(T0), wire(dash([row({})])), T0);
  for (let t = 1; t <= 4; t++) c = fail(c, T0 + t * 1000);
  assert.equal(isLost(c, T0 + 4000), false, 'brief failures keep the last data');
  assert.equal(kioskView(c, T0 + 4000).state, 'idle');
  c = fail(c, T0 + LOST_AFTER_MS + 1000);
  const v = kioskView(c, T0 + LOST_AFTER_MS + 1000);
  assert.equal(v.state, 'lost');
  assert.ok(v.state === 'lost' && v.lostFor === '6 s' && v.recent.length === 1 && v.station?.callsign === 'UT3UDX');
  assert.ok((kioskView(c, T0 + 125_000) as { lostFor: string }).lostFor === '2 min');
  // The board answers again.
  c = accept(c, wire(dash([row({})])), T0 + 130_000);
  assert.equal(c.failures, 0);
  assert.equal(kioskView(c, T0 + 130_000).state, 'idle');
  // No failure, no lost state, however old the reply (a slow request).
  assert.equal(isLost(c, T0 + 300_000), false);
});

test('a hidden tab stops polling; resuming does not flash "lost"', () => {
  let c = accept(initialConn(T0), wire(dash([row({})])), T0);
  const back = T0 + 600_000; // shown again after 10 minutes
  c = resume(c, back);
  c = fail(c, back + 1000);
  assert.equal(kioskView(c, back + 1000).state, 'idle');
  c = fail(c, back + 6000);
  assert.equal(kioskView(c, back + 6000).state, 'lost');
});

test('last calls: ten newest finished calls, POCSAG and live rows left out', () => {
  const rows: Row[] = [row({ live: true, duration: null, callsign: 'LIVE1' }), row({ mode: 'POCSAG', callsign: 'DAPNET', note: 'POCSAG Data', duration: null })];
  for (let i = 0; i < 15; i++) rows.push(row({ callsign: `C${i}ABC`, time: new Date(T0 - (i + 1) * 60_000).toISOString() }));
  const v = kioskView(accept(initialConn(T0), wire(dash(rows)), T0), T0);
  assert.ok(v.state === 'onair');
  assert.equal(v.recent.length, RECENT_CALLS);
  assert.deepEqual(v.recent.map(r => r.callsign), Array.from({ length: 10 }, (_, i) => `C${i}ABC`));
});

test('last-call rows: time, names, duration and loss/BER levels', () => {
  const rf = recent(row({}));
  assert.deepEqual({ clock: rf.clock, callsign: rf.callsign, name: rf.name, target: rf.target, targetName: rf.targetName, modeLabel: rf.modeLabel, duration: rf.duration, loss: rf.loss, ber: rf.ber, level: rf.level },
    { clock: '07:50:00', callsign: 'UT3UDX', name: 'Max', target: 'TG 255', targetName: 'UKRAINE', modeLabel: 'DMR TS2', duration: '3.2 s', loss: '', ber: '0.1%', level: 'good' });
  assert.equal(rf.quality, '0.1%');
  const net = recent(row({ source: 'Net', loss: 3, ber: 0, rssi: null }));
  assert.equal(net.loss, '3%');
  assert.equal(net.quality, '3% · 0.0%');
  assert.equal(net.level, 'warn');
  assert.equal(net.signal, '');
  assert.equal(recent(row({ source: 'Net', loss: 0, ber: 5 })).level, 'bad');
  assert.equal(recent(row({ ber: 0 })).level, '');
  const timeout = recent(row({ duration: null, ber: null, note: 'TOut' }));
  assert.equal(timeout.duration, 'TOut');
  assert.equal(timeout.ber, '??%');
  const data = recent(row({ note: 'DMR Data', duration: null, ber: null }));
  assert.ok(data.data && data.duration === 'Data' && data.ber === '' && data.quality === '—' && data.level === '');
  // Older backend without names; a bad clock string.
  const bare = recent({ ...row({}), name: undefined, targetName: undefined, clock: '' });
  assert.equal(bare.name, '');
  assert.equal(bare.targetName, '');
  assert.equal(bare.clock, '');
});

test('a new dashboard build asks the screen to reload once', () => {
  let c = accept(initialConn(T0), wire(dash([])), T0);
  assert.equal(c.reload, false);
  c = accept(c, wire(dash([])), T0 + 1000);
  assert.equal(c.reload, false);
  c = accept(c, wire(dash([], { Version: 'v2' })), T0 + 2000);
  assert.equal(c.reload, true);
});

test('intervals read naturally', () => {
  assert.equal(ago(2), 'just now');
  assert.equal(ago(42), '42 s ago');
  assert.equal(ago(60), '1 min ago');
  assert.equal(ago(3599), '59 min ago');
  assert.equal(ago(3600), '1 h ago');
  assert.equal(ago(3600 + 5 * 60 + 9), '1 h 5 min ago');
  assert.equal(span(-5), '0 s');
});
