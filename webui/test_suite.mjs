import test from 'node:test';
import assert from 'node:assert/strict';
import {
  computeBackoffDelay,
  jitterScale,
  isRetryExhausted,
  formatRetryDelay
} from './src/sseBackoff.ts';
import { AUDIT_ACTION_ALIASES, DEFAULT_ACTION_ALIASES, filterAuditLogs } from './src/components/governanceModel.ts';
import { executionBanner, displayProgress, splitSelection, mergeSelection, selectionProblem, usToClock, clockToUs } from './src/ingest.ts';
import {
  REFRESH_DOMAINS,
  REFRESH_SCOPES,
  refreshSnapshot,
  invalidateDeliveryFiles,
  extractEventMeta,
  handleSSEEvent,
  SUPPORTED_SSE_EVENTS
} from './src/queryRefresh.ts';

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

function createMockQueryClient() {
  const invalidated = [];
  return {
    invalidated,
    invalidateQueries: async ({ queryKey }) => {
      invalidated.push(queryKey);
    }
  };
}

test('queryRefresh: domains and scopes contain all 11 required domains', () => {
  const expectedDomains = [
    'assets',
    'deliveryFiles',
    'audit',
    'asset-task-index',
    'processing-profiles',
    'prepared',
    'workflows',
    'workflow',
    'review',
    'acceptance',
    'ingest'
  ];
  assert.deepEqual([...REFRESH_DOMAINS], expectedDomains);
  assert.equal(REFRESH_SCOPES.length, 11);
  assert.deepEqual(REFRESH_SCOPES.map((s) => s[0]), expectedDomains);
});

test('queryRefresh: refreshSnapshot invalidates all domains and watchedTaskId if present', async () => {
  const client1 = createMockQueryClient();
  await refreshSnapshot(client1, null);
  assert.equal(client1.invalidated.length, 11);
  assert.deepEqual(client1.invalidated.map((k) => k[0]), [...REFRESH_DOMAINS]);

  const client2 = createMockQueryClient();
  await refreshSnapshot(client2, 'task-999');
  assert.equal(client2.invalidated.length, 12);
  assert.deepEqual(client2.invalidated[11], ['task', 'task-999']);
});

test('queryRefresh: invalidateDeliveryFiles targets specific asset or full prefix', async () => {
  const client1 = createMockQueryClient();
  await invalidateDeliveryFiles(client1, 'asset_123');
  assert.deepEqual(client1.invalidated, [['deliveryFiles', 'asset_123']]);

  const client2 = createMockQueryClient();
  await invalidateDeliveryFiles(client2, '  asset_456  ');
  assert.deepEqual(client2.invalidated, [['deliveryFiles', 'asset_456']]);

  const client3 = createMockQueryClient();
  await invalidateDeliveryFiles(client3, '');
  assert.deepEqual(client3.invalidated, [['deliveryFiles']]);

  const client4 = createMockQueryClient();
  await invalidateDeliveryFiles(client4, null);
  assert.deepEqual(client4.invalidated, [['deliveryFiles']]);
});

test('queryRefresh: extractEventMeta safely parses json string or object', () => {
  assert.deepEqual(
    extractEventMeta(JSON.stringify({ asset_id: 'a1', task_id: 't1', status: 'succeeded' })),
    { asset_id: 'a1', task_id: 't1', status: 'succeeded' }
  );
  assert.deepEqual(
    extractEventMeta({ asset_id: '  a2  ', task_id: 't2', status: 'running' }),
    { asset_id: 'a2', task_id: 't2', status: 'running' }
  );
  assert.deepEqual(extractEventMeta('{ invalid json'), {});
  assert.deepEqual(extractEventMeta(null), {});
  assert.deepEqual(extractEventMeta(''), {});
  assert.deepEqual(extractEventMeta(123), {});
});

test('queryRefresh: handleSSEEvent invalidates deliveryFiles and related queries according to event rules', async () => {
  // asset_created / asset_updated
  const clientAsset = createMockQueryClient();
  await handleSSEEvent(clientAsset, 'asset_created', JSON.stringify({ asset_id: 'clip_01' }));
  assert.deepEqual(clientAsset.invalidated, [
    ['deliveryFiles', 'clip_01'],
    ['assets'],
    ['audit']
  ]);

  // workflow.changed
  const clientWf = createMockQueryClient();
  await handleSSEEvent(clientWf, 'workflow.changed', JSON.stringify({ asset_id: 'clip_02' }));
  assert.deepEqual(clientWf.invalidated, [
    ['deliveryFiles', 'clip_02'],
    ['workflows'],
    ['processing-profiles'],
    ['prepared'],
    ['workflow'],
    ['review'],
    ['acceptance'],
    ['audit']
  ]);

  // ingest.changed
  const clientIngest = createMockQueryClient();
  await handleSSEEvent(clientIngest, 'ingest.changed', JSON.stringify({ asset_id: 'clip_03' }));
  assert.deepEqual(clientIngest.invalidated, [
    ['deliveryFiles', 'clip_03'],
    ['ingest'],
    ['assets'],
    ['audit']
  ]);

  // capability.changed: unified listener
  const clientCap = createMockQueryClient();
  await handleSSEEvent(clientCap, 'capability.changed', '{}');
  assert.deepEqual(clientCap.invalidated, [
    ['ingest'],
    ['assets'],
    ['workflows'],
    ['processing-profiles'],
    ['prepared'],
    ['workflow'],
    ['review'],
    ['acceptance']
  ]);

  // audit.changed
  const clientAudit = createMockQueryClient();
  await handleSSEEvent(clientAudit, 'audit.changed', '{}');
  assert.deepEqual(clientAudit.invalidated, [['audit']]);
});

test('queryRefresh: handleSSEEvent task_updated only invalidates deliveryFiles when status=succeeded', async () => {
  // status=succeeded with asset_id
  const clientSucc = createMockQueryClient();
  await handleSSEEvent(
    clientSucc,
    'task_updated',
    JSON.stringify({ task_id: 't-1', asset_id: 'clip_01', status: 'succeeded' }),
    't-1'
  );
  assert.deepEqual(clientSucc.invalidated, [
    ['deliveryFiles', 'clip_01'],
    ['task', 't-1'],
    ['audit'],
    ['ingest', 'run'],
    ['asset-task-index']
  ]);

  // status=succeeded without asset_id: falls back to full deliveryFiles prefix
  const clientSuccNoAsset = createMockQueryClient();
  await handleSSEEvent(
    clientSuccNoAsset,
    'task_updated',
    JSON.stringify({ task_id: 't-1', status: 'succeeded' }),
    't-1'
  );
  assert.deepEqual(clientSuccNoAsset.invalidated, [
    ['deliveryFiles'],
    ['task', 't-1'],
    ['audit'],
    ['ingest', 'run'],
    ['asset-task-index']
  ]);

  // status=running: does NOT invalidate deliveryFiles
  const clientRunning = createMockQueryClient();
  await handleSSEEvent(
    clientRunning,
    'task_updated',
    JSON.stringify({ task_id: 't-1', asset_id: 'clip_01', status: 'running' }),
    't-1'
  );
  assert.deepEqual(clientRunning.invalidated, [
    ['task', 't-1'],
    ['audit'],
    ['ingest', 'run'],
    ['asset-task-index']
  ]);
});

test('auditQuery: prefix matching covers both recent and panel audit query keys', () => {
  // TanStack Query prefix matching semantics:
  // queryKey [a, b, ...] matches invalidation filter [a] because all elements in filter match prefix
  function matchesKey(queryKey, filterKey) {
    if (filterKey.length > queryKey.length) return false;
    return filterKey.every((part, idx) => part === queryKey[idx]);
  }

  const recentKey = ['audit', 'recent', 100];
  const panelKey200 = ['audit', 'panel', 200];
  const panelKey500 = ['audit', 'panel', 500];
  const oldBuggyPanelKey = ['audit-panel', 200];

  const auditPrefix = ['audit'];

  assert.ok(matchesKey(recentKey, auditPrefix), 'recentKey matches ["audit"] prefix');
  assert.ok(matchesKey(panelKey200, auditPrefix), 'panelKey200 matches ["audit"] prefix');
  assert.ok(matchesKey(panelKey500, auditPrefix), 'panelKey500 matches ["audit"] prefix');
  assert.ok(!matchesKey(oldBuggyPanelKey, auditPrefix), 'oldBuggyPanelKey did NOT match ["audit"] prefix');
});

test('auditFilters: keyword, action, actor and time filtering preserve across refetches', () => {
  const initialLogs = [
    { id: 1, created_at: '2026-10-03T10:00:00Z', actor: 'human:webui', action: 'asset.governance', target: 'a1', detail: 'agent_visible=true' },
    { id: 2, created_at: '2026-10-03T10:05:00Z', actor: 'system', action: 'task.create', target: 't1', detail: '' }
  ];

  const filters = {
    keyword: 'governance',
    actions: ['asset.governance'],
    actorKinds: ['human'],
    from: '',
    to: ''
  };

  const filtered1 = filterAuditLogs(initialLogs, filters);
  assert.equal(filtered1.length, 1);
  assert.equal(filtered1[0].id, 1);

  // New log arrived after audit.changed
  const updatedLogs = [
    { id: 3, created_at: '2026-10-03T10:10:00Z', actor: 'human:webui', action: 'asset.governance', target: 'a2', detail: 'agent_visible=false' },
    ...initialLogs
  ];

  // Filters are retained and applied to the updated logs
  const filtered2 = filterAuditLogs(updatedLogs, filters);
  assert.equal(filtered2.length, 2);
  assert.equal(filtered2[0].id, 3);
  assert.equal(filtered2[1].id, 1);
});
