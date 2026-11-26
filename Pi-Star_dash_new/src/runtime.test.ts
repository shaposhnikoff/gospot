import { test } from 'node:test';
import assert from 'node:assert/strict';
import { asSnapshot } from './runtime';
import { fetchSnapshot } from './data';

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
