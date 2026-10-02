import test from 'node:test';
import assert from 'node:assert/strict';
import {
  computeBackoffDelay,
  jitterScale,
  isRetryExhausted,
  formatRetryDelay
} from './src/sseBackoff.ts';
import { AUDIT_ACTION_ALIASES, DEFAULT_ACTION_ALIASES } from './src/components/governanceModel.ts';
import { executionBanner, displayProgress, splitSelection, mergeSelection, selectionProblem, usToClock, clockToUs } from './src/ingest.ts';

test('manual splitting preserves source coverage and merging refuses unselected gaps', () => {
  const original = [{segment_id:'a',start_us:500001,end_us:6000000}, {segment_id:'b',start_us:8000000,end_us:9000000}];
  const split = splitSelection(original, 'a', 3000001, 'manual_b');
  assert.equal(split.length, 3);
  assert.equal(split[0].end_us, split[1].start_us);
  assert.deepEqual(mergeSelection(split, 'a'), original);
  assert.throws(() => mergeSelection(original, 'a'), /首尾相接/);
  assert.throws(() => splitSelection(original, 'a', 500001, 'c'));
  assert.throws(() => splitSelection(original, 'a', 3000000, 'b'));
  assert.match(selectionProblem(splitSelection(original,'a',500002,'c'),10000000,60), /短于一帧/);
  assert.match(selectionProblem(split,10000000,60,{max_selected:2,max_output_us:10000000}), /最多选择/);
});

test('editing times preserves microseconds and never formats a 60-second remainder', () => {
  for (const us of [0,1,1000,500001,59999999,60000000,3599999999,3600000000]) assert.equal(clockToUs(usToClock(us)), us);
  assert.equal(usToClock(59999999), '00:59.999999');
});

test('ingest: real Go JSON shape drives running, waiting, disconnected and finished states', () => {
  const task = {task_id:'t', type:'segment', status:'running', progress:0.4};
  assert.equal(displayProgress(task, {status:'running', connection:'connected', checkpoints_done:2}).value, 0.4);
  assert.equal(displayProgress(task, {status:'allocated'}), null);
  assert.equal(executionBanner({status:'waiting_resource', wait_reason:'waiting_for_previous_execution'}), '等待旧执行退出');
  assert.match(executionBanner({status:'suspended_connection', connection:'suspended', checkpoints_done:2}), /2 个检查点/);
  assert.equal(executionBanner({status:'cleanup_blocked', cleanup:'blocked'}), '清理受阻');
  assert.match(displayProgress({...task,status:'succeeded',progress:1}, {status:'succeeded'}).label, /100%.*已完成/);
});

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
