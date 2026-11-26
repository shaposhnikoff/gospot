import assert from 'node:assert/strict';
import { test } from 'node:test';
import { fetchSnapshot, scenarios } from './data';

test('the selector exposes each supported scenario once', () => {
  assert.deepEqual(scenarios.map(({ id }) => id).sort(), [
    'api-offline', 'idle', 'journal-unavailable', 'master-timeout', 'modem-down', 'online',
  ]);
  for (const { label } of scenarios) assert.match(label, /[А-Яа-яЁё]/);
});

test('online snapshot includes an ordered RF and network history with optional metrics', async () => {
  const snapshot = await fetchSnapshot('online');
  assert.equal(snapshot.Modem.State, 'up');
  assert.equal(snapshot.Master.State, 'up');
  assert.equal(snapshot.HeardAvailable, true);
  assert.ok(snapshot.Radio.Modes.includes('DMR'));
  assert.deepEqual(new Set(snapshot.Heard.map(row => row.Source)), new Set(['RF', 'Net']));
  let latest = snapshot.UpdatedAt;
  for (const row of snapshot.Heard) {
    assert.equal(new Date(row.At).toISOString(), row.At);
    assert.ok(row.At <= latest);
    assert.match(row.Slot, /^[12]$/);
    latest = row.At;
  }
  assert.ok(snapshot.Heard.some(row => row.Loss === 0));
  assert.ok(snapshot.Heard.some(row => row.Duration === undefined));
});

test('each link failure is independent and the journal retains earlier calls', async () => {
  for (const [scenario, failed, healthy] of [
    ['modem-down', 'Modem', 'Master'],
    ['master-timeout', 'Master', 'Modem'],
  ] as const) {
    const snapshot = await fetchSnapshot(scenario);
    assert.equal(snapshot[failed].State, 'down');
    assert.equal(snapshot[healthy].State, 'up');
    assert.equal(snapshot.HeardAvailable, true);
    assert.ok(snapshot.Heard.length > 0);
    assert.ok(snapshot.Heard.every(row => row.At < snapshot[failed].At));
  }
});

test('quiet operation is distinguishable from unavailable evidence', async () => {
  const idle = await fetchSnapshot('idle');
  const missing = await fetchSnapshot('journal-unavailable');
  assert.deepEqual(idle.Heard, []);
  assert.deepEqual(missing.Heard, []);
  assert.equal(idle.HeardAvailable, true);
  assert.equal(missing.HeardAvailable, false);
  for (const key of ['Modem', 'Master'] as const) {
    assert.equal(idle[key].State, 'up');
    assert.equal(missing[key].State, 'unknown');
  }
});

test('mutating a returned snapshot does not change later history or radio settings', async () => {
  const first = await fetchSnapshot('online');
  const history = structuredClone(first.Heard);
  first.Heard[0]!.From = 'CHANGED';
  first.Radio.Modes.length = 0;
  const next = await fetchSnapshot('online');
  assert.deepEqual(next.Heard, history);
  assert.deepEqual(next.Radio.Modes, ['DMR']);
});

test('offline rejects and a later online request succeeds', async () => {
  await assert.rejects(fetchSnapshot('api-offline'), /API недоступен/);
  assert.equal((await fetchSnapshot('online')).Master.State, 'up');
});
