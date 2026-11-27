import test from 'node:test';
import assert from 'node:assert/strict';
import { JitterBuffer, MULAW, decodeMulaw, mulawSample, parseEvent, parseGroups, refusal, socketURL } from './hoseline';
import { startAt, PREBUFFER } from './hoseline';

test('μ-law decoding matches G.711', () => {
  // python3.12 -c 'import audioop,struct; print(struct.unpack("<h", audioop.ulaw2lin(bytes([c]), 2))[0])'
  const known: Record<number, number> = {
    0x00: -32124, 0x01: -31100, 0x0f: -16764, 0x10: -15996, 0x55: -716, 0x7e: -8, 0x7f: 0,
    0x80: 32124, 0x81: 31100, 0xd5: 716, 0xfe: 8, 0xff: 0,
  };
  for (const [code, sample] of Object.entries(known)) assert.equal(mulawSample(Number(code)), sample, `code ${code}`);
  for (let c = 0; c < 128; c++) assert.equal(mulawSample(c) + mulawSample(c | 0x80), 0, `symmetry ${c}`);
  assert.equal(MULAW.length, 256);
  assert.deepEqual([...decodeMulaw(new Uint8Array([0x80, 0xff, 0x00, 0xd5]))], [32124 / 32768, 0, -32124 / 32768, 716 / 32768]);
});

const ramp = (from: number, n: number) => Array.from({ length: n }, (_, i) => from + i);
const read = (b: JitterBuffer, n: number, step = 1) => Array.from({ length: n }, () => b.next(step));

test('jitter buffer holds the start of a call, then plays in order', () => {
  const b = new JitterBuffer(4, 16);
  b.push([1, 2, 3]);
  assert.deepEqual(read(b, 3), [0, 0, 0], 'silent below the prebuffer');
  assert.equal(b.buffered, 3);
  b.push([4, 5]);
  assert.deepEqual(read(b, 5), [1, 2, 3, 4, 5]);
  assert.equal(b.playing, false, 'an underrun buffers again');
  b.push([6, 7, 8]);
  assert.deepEqual(read(b, 2), [0, 0], 'refills to the prebuffer after an underrun');
  b.push([9]);
  assert.deepEqual(read(b, 4), [6, 7, 8, 9]);
});

test('jitter buffer plays a short call out on flush', () => {
  const b = new JitterBuffer(1600, 8000);
  b.push([0.25, 0.5]);
  assert.equal(b.next(), 0);
  b.flush();
  assert.deepEqual(read(b, 3), [0.25, 0.5, 0]);
  b.push([0.75]);
  assert.equal(b.next(), 0, 'the next call is buffered again');
});

test('jitter buffer drops the oldest audio when full', () => {
  const b = new JitterBuffer(2, 4);
  b.push(ramp(1, 6));
  assert.equal(b.dropped, 2);
  assert.deepEqual(read(b, 4), [3, 4, 5, 6]);
  b.push(ramp(1, 3));
  b.reset();
  assert.equal(b.buffered, 0);
  assert.equal(b.next(), 0);
});

test('jitter buffer interpolates when the output runs above 8 kHz', () => {
  const b = new JitterBuffer(3, 16);
  b.push([0, 1, 2, 3]);
  assert.deepEqual(read(b, 6, 0.5), [0, 0.5, 1, 1.5, 2, 2.5]);
});

test('relay events and groups are validated', () => {
  assert.deepEqual(parseEvent('{"type":"listening","tg":91}'), { type: 'listening', tg: 91 });
  assert.deepEqual(parseEvent('{"type":"start","source":2340001}'), { type: 'start', source: 2340001 });
  assert.deepEqual(parseEvent('{"type":"start"}'), { type: 'start', source: null });
  assert.deepEqual(parseEvent('{"type":"end"}'), { type: 'end' });
  assert.deepEqual(parseEvent('{"type":"error","message":"Hoseline disconnected"}'), { type: 'error', message: 'Hoseline disconnected' });
  for (const bad of ['', '[]', 'null', '{"type":"listening","tg":"91"}', '{"type":"other"}']) assert.equal(parseEvent(bad), null, bad);
  assert.deepEqual(parseGroups({ groups: [91, 2504], listeners: 1, max: 3 }), { groups: [91, 2504], listeners: 1, max: 3 });
  assert.equal(parseGroups({ groups: ['91'], listeners: 0, max: 3 }), null);
  assert.match(refusal({ groups: [91], listeners: 3, max: 3 }), /Too many listeners: all 3 Hoseline slots/);
  assert.equal(refusal({ groups: [91], listeners: 1, max: 3 }), 'Hoseline relay unavailable');
  assert.equal(refusal(null), 'Hoseline relay unavailable');
});

test('the socket stays on the page origin', () => {
  assert.equal(socketURL({ protocol: 'http:', host: 'gospot:8789' }, false, 91), 'ws://gospot:8789/api/hoseline?tg=91');
  assert.equal(socketURL({ protocol: 'https:', host: 'gospot:8788' }, true, 2504), 'wss://gospot:8788/manage/api/hoseline?tg=2504');
});

test('fallback scheduling plays frames back to back and rebuffers after a gap', () => {
  assert.equal(startAt(10, 0), 10 + PREBUFFER);      // first frame: prebuffer
  assert.equal(startAt(10, 10.5), 10.5);             // queued: continue seamlessly
  assert.equal(startAt(10, 10.01), 10 + PREBUFFER);  // underrun: rebuffer
});
