import { test } from 'node:test';
import assert from 'node:assert/strict';
import { asSnapshot, serviceOrigin } from './runtime';
import { fetchSnapshot } from './data';

test('navigation uses the configured port and current host, including IPv6', () => {
  assert.equal(serviceOrigin('http://127.0.0.1:8790', 'https:', 8791), 'https://127.0.0.1:8791');
  assert.equal(serviceOrigin('https://[::1]:8791', 'http:', 8790), 'http://[::1]:8790');
  for (const port of [undefined, null, '8788', 0, -1, 65536, 1.5])
    assert.equal(serviceOrigin('http://localhost:8790', 'https:', port), null);
});

test('Go: empty journal can be available; null slices do not break rendering', async () => {
  const x = await fetchSnapshot('idle');
  const parsed = asSnapshot({...x, Heard:null, Radio:{...x.Radio, Modes:null}});
  assert.deepEqual(parsed.Heard, []);
  assert.equal(parsed.HeardAvailable, true);
  assert.deepEqual(parsed.Radio.Modes, []);
});
test('reject malformed data rather than showing artificial healthy status', async () => {
  const x = await fetchSnapshot('online');
  for (const bad of [{}, { ...x, Master:{...x.Master, State:'success'} },
    {...x,Heard:[{...x.Heard[0],Source:'???'}]}, {...x,HeardAvailable:'true'}]) {
    assert.throws(()=>asSnapshot(bad));
  }
  assert.deepEqual(asSnapshot(x), x);
});
