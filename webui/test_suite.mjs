import test from 'node:test';
import assert from 'node:assert/strict';
import {
  computeBackoffDelay,
  jitterScale,
  isRetryExhausted,
  formatRetryDelay
} from './src/sseBackoff.ts';
import { AUDIT_ACTION_ALIASES, DEFAULT_ACTION_ALIASES } from './src/components/governanceModel.ts';

test('sseBackoff: computeBackoffDelay correctly calculates exponential backoff and respects maxDelay', () => {
  assert.equal(computeBackoffDelay(0, 1000, 30000), 1000);
  assert.equal(computeBackoffDelay(1, 1000, 30000), 2000);
  assert.equal(computeBackoffDelay(2, 1000, 30000), 4000);
  assert.equal(computeBackoffDelay(3, 1000, 30000), 8000);
  assert.equal(computeBackoffDelay(4, 1000, 30000), 16000);
  assert.equal(computeBackoffDelay(5, 1000, 30000), 30000); // capped
  assert.equal(computeBackoffDelay(10, 1000, 30000), 30000); // capped
  // edge cases
  assert.equal(computeBackoffDelay(-1, 1000, 30000), 1000);
  assert.equal(computeBackoffDelay(NaN, 1000, 30000), 1000);
});

test('sseBackoff: jitterScale bounds', () => {
  const min = jitterScale(0.5, () => 0);
  const max = jitterScale(0.5, () => 1);
  assert.equal(min, 0.5);
  assert.equal(max, 1.0);
});

test('sseBackoff: isRetryExhausted and formatRetryDelay', () => {
  assert.equal(isRetryExhausted(7, 8), false);
  assert.equal(isRetryExhausted(8, 8), true);
  assert.equal(isRetryExhausted(9, 8), true);

  assert.equal(formatRetryDelay(500), '500 毫秒');
  assert.equal(formatRetryDelay(1500), '1.5 秒');
  assert.equal(formatRetryDelay(12000), '12 秒');
});

test('governanceModel: audit aliases does not map cancel/retry to requeue', () => {
  assert.ok(!('CANCEL' in AUDIT_ACTION_ALIASES), 'CANCEL alias should not exist');
  assert.ok(!('RETRY' in AUDIT_ACTION_ALIASES), 'RETRY alias should not exist');
  assert.ok('TASK_REQUEUE' in AUDIT_ACTION_ALIASES, 'TASK_REQUEUE alias exists');
  assert.deepEqual(AUDIT_ACTION_ALIASES.TASK_REQUEUE, ['task.requeue']);
  assert.deepEqual(DEFAULT_ACTION_ALIASES, ['CREATE_TASK', 'TASK_REQUEUE', 'SYSTEM_EVENT']);
});
