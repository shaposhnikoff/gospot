import test from 'node:test';
import assert from 'node:assert/strict';
import { wordwrap, tgifCell, nextTGIFRefresh } from './tgif';

test('wordwrap matches PHP wordwrap(..., 15) without cutting', () => {
  // php -r 'echo json_encode(explode("\n", wordwrap("TG91(1) TG235(2) TG9990(1) ", 15, "\n")));'
  assert.deepEqual(wordwrap('TG91(1) TG235(2) TG9990(1) ', 15), ['TG91(1)', 'TG235(2)', 'TG9990(1) ']);
  // PHP ends this one with a break; a trailing <br /> in a cell renders nothing.
  assert.deepEqual(wordwrap('TG91(1) TG92(2) ', 15), ['TG91(1) TG92(2)']);
  assert.deepEqual(wordwrap('TG1(1) TG2(2) ', 15), ['TG1(1) TG2(2) ']);
  assert.deepEqual(wordwrap('TG12345678(1) ', 15), ['TG12345678(1) ']);
  assert.deepEqual(wordwrap('TG123456789012(1) TG1(2) ', 15), ['TG123456789012(1)', 'TG1(2) ']);
  assert.deepEqual(wordwrap('', 15), []);
});

test('TGIF cells fall back as tgif_static_links.php does', () => {
  assert.deepEqual(tgifCell(['TG91(1)', 'TG235(2)'], 'ok'), ['TG91(1)', 'TG235(2) ']);
  assert.deepEqual(tgifCell([], 'ok'), ['None']);
  assert.deepEqual(tgifCell(null, 'ok'), ['None']);
  assert.deepEqual(tgifCell([], 'unavailable'), ['Unavailable']);
  assert.deepEqual(tgifCell(null, 'denied'), ['Token too old']);
});

test('TGIF refresh: one 15 s retry after a failure, then 180 s', () => {
  assert.deepEqual(nextTGIFRefresh(true, false), { delay: 180000, retried: false });
  assert.deepEqual(nextTGIFRefresh(false, false), { delay: 15000, retried: true });
  assert.deepEqual(nextTGIFRefresh(false, true), { delay: 180000, retried: true });
  assert.deepEqual(nextTGIFRefresh(true, true), { delay: 180000, retried: true });
});
