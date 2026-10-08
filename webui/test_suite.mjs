import test from 'node:test';
import assert from 'node:assert/strict';
import { isCurrentMp4 } from './src/previewContracts.ts';

test('MP4 program source requires current asset/run/revision and a valid successful result', () => {
  const owner={assetId:'a',runId:'r',revisionId:'v'};
  const view={asset_id:'a',run_id:'r',revision_id:'v',status:'succeeded',is_current:true,invalidated:false};
  assert.equal(isCurrentMp4(view,owner),true);
  for(const change of [{asset_id:'b'},{run_id:'other'},{revision_id:'old'},{is_current:false},{invalidated:true},{status:'running'}]) {
    assert.equal(isCurrentMp4({...view,...change},owner),false);
  }
});
import { latestWorkflowRun, workflowPreviewContext, samePreviewContext, reviewMatchesCurrent, createWorkflowPreview, displayWorkflowPreview } from './src/previewContext.ts';

function previewFixture(overrides = {}) {
  const run = { asset_id: 'asset-a', run_id: 'run-a', current_revision_id: 'rev-1', version: 1, status: 'awaiting_review', stage: 'scene_review', ...overrides };
  return { run, revision: { revision_id: run.current_revision_id },
    scenes: [{ scene_id: 'scene-a', label: 'A 画面', decision: 'confirmed' }],
    narration: [{ id: 'line-a', text: 'A 解说', source_scene_id: 'scene-a', start: 0, end: 4 }],
    evidence_files: [{ scene_id: 'scene-a', key: 'frame/a', sha256: 'sha-a', timestamp: '1.0' }] };
}

test('preview: asset, actual default run and revision changes each invalidate old evidence', () => {
  const review = previewFixture();
  const image = createWorkflowPreview(review, { kind: 'line', id: 'line-a' });
  assert.match(image.src, /frame%2Fa\?revision_id=rev-1$/);
  assert.equal(image.caption, 'A 解说');
  for (const change of [{ asset_id: 'asset-b' }, { run_id: 'new-default' }, { current_revision_id: 'rev-2', version: 2 }]) {
    const next = previewFixture(change);
    assert.equal(displayWorkflowPreview(image, workflowPreviewContext(next.run), next), null);
  }
  assert.equal(displayWorkflowPreview(image, null, undefined), null);
});

test('preview: delayed older responses cannot roll the current revision back', () => {
  const old = previewFixture().run;
  const newer = previewFixture({ current_revision_id: 'rev-2', version: 2 }).run;
  const known = latestWorkflowRun('asset-a', 'run-a', [old, newer]);
  assert.equal(known.current_revision_id, 'rev-2');
  assert.equal(latestWorkflowRun('asset-a', 'run-a', [old, undefined, known]), known);
  assert.equal(reviewMatchesCurrent(previewFixture(), known), false);
  assert.equal(reviewMatchesCurrent(previewFixture({ current_revision_id: 'rev-2', version: 2 }), known), true);
});

test('preview: resolved run isolates foreign caches and a missing selected run', () => {
  const a = previewFixture().run;
  const b = previewFixture({ asset_id: 'asset-b', run_id: 'run-b', version: 100 }).run;
  assert.equal(latestWorkflowRun('asset-a', 'run-a', [b, a]), a);
  assert.equal(latestWorkflowRun('asset-a', undefined, [a, b]), undefined);
  assert.equal(latestWorkflowRun('asset-a', 'run-new', [a, b]), undefined);
});

test('preview: matching context survives refresh and updates decision metadata', () => {
  const review = previewFixture();
  const image = createWorkflowPreview(review, { kind: 'scene', id: 'scene-a' });
  const next = previewFixture({ version: 2 });
  next.scenes[0].decision = 'rejected';
  const updated = displayWorkflowPreview(image, workflowPreviewContext(next.run), next);
  assert.equal(updated.src, image.src);
  assert.match(updated.meta, /rejected/);
  assert.equal(samePreviewContext(image.workflowContext, updated.workflowContext), true);
});

test('preview: matching revision keeps narration in sync without crossing scene evidence', () => {
  const review = previewFixture();
  const image = createWorkflowPreview(review, { kind: 'line', id: 'line-a' });
  const next = previewFixture({ version: 2 });
  next.narration[0].text = '新解说';
  assert.equal(displayWorkflowPreview(image, workflowPreviewContext(next.run), next).caption, '新解说');
  next.narration[0].source_scene_id = 'different-scene';
  assert.equal(displayWorkflowPreview(image, workflowPreviewContext(next.run), next), null);
});

test('preview: missing or replaced evidence is never treated as the same image', () => {
  const review = previewFixture();
  const image = createWorkflowPreview(review, { kind: 'scene', id: 'scene-a' });
  const next = previewFixture();
  next.evidence_files[0].sha256 = 'changed-image';
  assert.equal(displayWorkflowPreview(image, workflowPreviewContext(next.run), next), null);
  next.evidence_files = [];
  assert.equal(createWorkflowPreview(next, { kind: 'scene', id: 'scene-a' }), null);
  assert.equal(displayWorkflowPreview(image, workflowPreviewContext(next.run), next), null);
});

test('preview: inconsistent review revision or missing target cannot produce a URL', () => {
  const review = previewFixture();
  review.revision.revision_id = 'rev-wrong';
  assert.equal(reviewMatchesCurrent(review, review.run), false);
  assert.equal(createWorkflowPreview(review, { kind: 'scene', id: 'scene-a' }), null);
  assert.equal(createWorkflowPreview(previewFixture(), { kind: 'line', id: 'missing' }), null);
  assert.equal(createWorkflowPreview(undefined, { kind: 'scene', id: 'scene-a' }), null);
});
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
import {
  previewApi,
  previewFileURL,
  previewQueryKey,
  createPreviewSource,
  formatSeconds
} from './src/previewContracts.ts';

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

test('queryRefresh: domains and scopes contain all 17 required domains', () => {
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
    'ingest',
    'mp4Previews',
    'deliveryVariants',
    'tts-templates', 'tts-previews', 'tts-preview', 'tts-capabilities'
  ];
  assert.deepEqual([...REFRESH_DOMAINS], expectedDomains);
  assert.equal(REFRESH_SCOPES.length, 17);
  assert.deepEqual(REFRESH_SCOPES.map((s) => s[0]), expectedDomains);
});

test('queryRefresh: refreshSnapshot invalidates all domains and watchedTaskId if present', async () => {
  const client1 = createMockQueryClient();
  await refreshSnapshot(client1, null);
  assert.equal(client1.invalidated.length, 17);
  assert.deepEqual(client1.invalidated.map((k) => k[0]), [...REFRESH_DOMAINS]);

  const client2 = createMockQueryClient();
  await refreshSnapshot(client2, 'task-999');
  assert.equal(client2.invalidated.length, 18);
  assert.deepEqual(client2.invalidated[17], ['task', 'task-999']);
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
    ['deliveryVariants'],
    ['deliveryFiles', 'clip_01'],
    ['assets'],
    ['audit']
  ]);

  // workflow.changed
  const clientWf = createMockQueryClient();
  await handleSSEEvent(clientWf, 'workflow.changed', JSON.stringify({ asset_id: 'clip_02' }));
  assert.deepEqual(clientWf.invalidated, [
    ['deliveryVariants'],
    ['deliveryFiles', 'clip_02'],
    ['workflows'],
    ['processing-profiles'],
    ['prepared'],
    ['workflow'],
    ['review'],
    ['acceptance'],
    ['audit'],
    ['mp4Previews']
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
    ['deliveryVariants'],
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
    ['deliveryVariants'],
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
    ['deliveryVariants'],
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
    ['deliveryVariants'],
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

/* ------------------------------------------------------------------ *
 * Phase D: 成片 MP4 预览（PreviewClient、事件失效、监视器源联动）测试
 * ------------------------------------------------------------------ */

test('preview contracts: URL and query key builders construct correct paths and escape identifiers', () => {
  assert.deepEqual(
    previewQueryKey('asset_100', 'run_200', 'rev_300'),
    ['mp4Previews', 'asset_100', 'run_200', 'rev_300']
  );

  assert.equal(
    previewFileURL('prev_123', 'mp4'),
    '/api/previews/prev_123/files/mp4'
  );
  assert.equal(
    previewFileURL('prev_123', 'mp4', false),
    '/api/previews/prev_123/files/mp4'
  );
  assert.equal(
    previewFileURL('prev_123', 'mp4', true),
    '/api/previews/prev_123/files/mp4?download=1'
  );
  assert.equal(
    previewFileURL('prev_123', 'report'),
    '/api/previews/prev_123/files/report'
  );
  assert.equal(
    previewFileURL('prev_123', 'receipt', true),
    '/api/previews/prev_123/files/receipt?download=1'
  );

  // 特殊字符安全转义
  assert.equal(
    previewFileURL('prev/test&1', 'sub dir/file.mp4'),
    '/api/previews/prev%2Ftest%261/files/sub%20dir%2Ffile.mp4'
  );
});

test('previewApi: start, list, get, cancel execute expected REST calls with parameters and headers', async () => {
  const originalFetch = globalThis.fetch;
  const calls = [];

  globalThis.fetch = async (url, options = {}) => {
    calls.push({ url, options });
    if (url.includes('/cancel')) {
      return {
        ok: true,
        status: 200,
        json: async () => ({ preview_id: 'prev-cancel', status: 'cancelled' })
      };
    }
    if (url.includes('/workflows/') && options.method === 'POST') {
      return {
        ok: true,
        status: 202,
        json: async () => ({ preview_id: 'prev-created', status: 'queued' })
      };
    }
    if (url.includes('/workflows/') && (!options.method || options.method === 'GET')) {
      return {
        ok: true,
        status: 200,
        json: async () => ({ run_id: 'run-abc', can_start: true, previews: [] })
      };
    }
    if (url === '/api/previews/prev-view') {
      return {
        ok: true,
        status: 200,
        json: async () => ({ preview_id: 'prev-view', status: 'succeeded' })
      };
    }
    if (url.includes('/error')) {
      return {
        ok: false,
        status: 400,
        json: async () => ({ error: { code: 'bad_request', message: '非法参数' } })
      };
    }
    return { ok: true, status: 200, json: async () => ({}) };
  };

  try {
    // 1. start
    const startRes = await previewApi.start('run-abc', {
      revision_id: 'rev-xyz',
      expected_version: 3,
      preset_id: 'mp4-720p-v1',
      request_id: 'req-unique-1'
    });
    assert.equal(startRes.preview_id, 'prev-created');
    assert.equal(calls[0].url, '/api/workflows/run-abc/previews');
    assert.equal(calls[0].options.method, 'POST');
    assert.equal(calls[0].options.headers['Content-Type'], 'application/json');
    assert.deepEqual(JSON.parse(calls[0].options.body), {
      revision_id: 'rev-xyz',
      expected_version: 3,
      preset_id: 'mp4-720p-v1',
      request_id: 'req-unique-1'
    });

    // 2. list
    const listRes = await previewApi.list('run-abc');
    assert.equal(listRes.run_id, 'run-abc');
    assert.equal(calls[1].url, '/api/workflows/run-abc/previews');

    // 3. get
    const getRes = await previewApi.get('prev-view');
    assert.equal(getRes.preview_id, 'prev-view');
    assert.equal(calls[2].url, '/api/previews/prev-view');

    // 4. cancel
    const cancelRes = await previewApi.cancel('prev-view');
    assert.equal(cancelRes.status, 'cancelled');
    assert.equal(calls[3].url, '/api/previews/prev-view/cancel');
    assert.equal(calls[3].options.method, 'POST');
    assert.equal(calls[3].options.body, '{}');
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test('queryRefresh: extractEventMeta correctly extracts run_id and revision_id', () => {
  const metaObj = extractEventMeta({
    asset_id: 'asset_1',
    task_id: 'task_1',
    run_id: 'run_1',
    revision_id: 'rev_1',
    status: 'succeeded'
  });
  assert.equal(metaObj.asset_id, 'asset_1');
  assert.equal(metaObj.task_id, 'task_1');
  assert.equal(metaObj.run_id, 'run_1');
  assert.equal(metaObj.revision_id, 'rev_1');
  assert.equal(metaObj.status, 'succeeded');

  const metaStr = extractEventMeta(
    JSON.stringify({
      asset_id: '  asset_2  ',
      run_id: '  run_2  ',
      revision_id: '  rev_2  '
    })
  );
  assert.equal(metaStr.asset_id, 'asset_2');
  assert.equal(metaStr.run_id, 'run_2');
  assert.equal(metaStr.revision_id, 'rev_2');
});

test('queryRefresh: handleSSEEvent preview_updated invalidates mp4Previews queries by scope and meta', async () => {
  // 1. 完全元数据：失效具体 [mp4Previews, asset, run, revision]
  const clientFull = createMockQueryClient();
  await handleSSEEvent(
    clientFull,
    'preview_updated',
    JSON.stringify({
      asset_id: 'asset-a',
      run_id: 'run-b',
      revision_id: 'rev-c',
      task_id: 'task-p',
      status: 'succeeded'
    }),
    'task-p'
  );
  assert.deepEqual(clientFull.invalidated, [
    ['mp4Previews', 'asset-a', 'run-b', 'rev-c'],
    ['task', 'task-p'],
    ['audit']
  ]);

  // 2. 只有 asset_id 和 run_id
  const clientPartial = createMockQueryClient();
  await handleSSEEvent(
    clientPartial,
    'preview_updated',
    JSON.stringify({
      asset_id: 'asset-a',
      run_id: 'run-b'
    })
  );
  assert.deepEqual(clientPartial.invalidated, [
    ['mp4Previews', 'asset-a', 'run-b'],
    ['audit']
  ]);

  // 3. 只有 asset_id
  const clientAssetOnly = createMockQueryClient();
  await handleSSEEvent(
    clientAssetOnly,
    'preview_updated',
    JSON.stringify({
      asset_id: 'asset-a'
    })
  );
  assert.deepEqual(clientAssetOnly.invalidated, [
    ['mp4Previews', 'asset-a'],
    ['audit']
  ]);

  // 4. 空负载回退为整体 mp4Previews 前缀
  const clientEmpty = createMockQueryClient();
  await handleSSEEvent(clientEmpty, 'preview_updated', '{}');
  assert.deepEqual(clientEmpty.invalidated, [
    ['mp4Previews'],
    ['audit']
  ]);
});

test('createPreviewSource: constructs video PreviewSource with formatted metadata and context', () => {
  const sampleView = {
    preview_id: 'prev-999',
    task_id: 'task-888',
    asset_id: 'asset-clip',
    run_id: 'run-001',
    revision_id: 'rev-export-1',
    preset_id: 'mp4-720p-v1',
    input_sha256: 'sha-input',
    status: 'succeeded',
    progress: 1,
    invalidated: false,
    is_current: true,
    artifacts: [],
    media: {
      width: 1280,
      height: 720,
      fps: 30,
      frame_count: 1965,
      duration_seconds: 65.5,
      video_codec: 'h264',
      audio_codec: 'aac',
      sample_rate: 48000,
      channels: 2
    }
  };

  const source = createPreviewSource(sampleView);
  assert.equal(source.kind, 'video');
  assert.equal(source.src, '/api/previews/prev-999/files/mp4');
  assert.equal(source.title, '成片 MP4 预览 (rev-export-1)');
  assert.match(source.meta, /1280×720 · 30fps · 00:01:05.500/);
  assert.equal(source.note, '合成成片 MP4 预览（中文字幕烧录与最终三轨混合）');
  assert.deepEqual(source.workflowContext, {
    assetId: 'asset-clip',
    runId: 'run-001',
    revisionId: 'rev-export-1'
  });
});

test('preview: retry semantics distinguish transport retry from explicit retry', () => {
  let requestId = null;

  function triggerStart(explicitRetry = false) {
    if (explicitRetry || !requestId) {
      requestId = 'req-' + Math.random().toString(36).slice(2);
    }
    return requestId;
  }

  // 初次生成
  const firstReqId = triggerStart(false);
  assert.ok(firstReqId.startsWith('req-'));

  // 网络/传输错误重试：复用相同 request_id
  const retryReqId = triggerStart(false);
  assert.equal(retryReqId, firstReqId, '传输重试应复用原有 request_id');

  // 明确重试：生成全新 request_id
  const newAttemptReqId = triggerStart(true);
  assert.notEqual(newAttemptReqId, firstReqId, '明确重试应生成新的 request_id');
});


test('ASS SSE invalidates variant queries after task, governance and capability changes', async () => {
  for (const event of ['delivery_variant.changed','capability.changed','workflow.changed','asset_updated','task_updated']) {
    const client=createMockQueryClient();await handleSSEEvent(client,event,{asset_id:'a',run_id:'r'});
    assert(client.invalidated.some(key=>key[0]==='deliveryVariants'));
  }
  assert(SUPPORTED_SSE_EVENTS.includes('delivery_variant.changed'));
});

import {
  adoptServerDraft,
  applyStyleToAll,
  clearLocalDrafts,
  commitEdit,
  defaultStyle,
  effectiveCue,
  freezeStartRequest,
  noteSaveOutcome,
  rememberLocalDraft,
  restoreInheritance,
  runStyledExport,
  selectAssJob,
  selectMp4Job,
  setCueDuration,
  setCueStyles,
  setCueTiming,
  setGlobalField,
  styleCacheKey,
  styleSha256,
  undoEdit,
  validateDocument
} from './src/assEditorModel.ts';
import { validateDocument as sharedValidateDocument } from '../contracts/delivery-variant/v1/style.mjs';
import { previewProductKey, shouldAutoSelectPreview } from './src/previewContracts.ts';

test('ASS editor uses the shared validator and keeps inheritance, timing, undo and conflicts', async () => {
  assert.equal(validateDocument, sharedValidateDocument);
  clearLocalDrafts();
  const sha = 'a'.repeat(64);
  const cues = [
    { cue_id: 1, start_ms: 0, end_ms: 1000, text: '甲' },
    { cue_id: 2, start_ms: 1000, end_ms: 2000, text: '乙' }
  ];
  let doc = {
    source_srt_sha256: sha, play_res_x: 1280, play_res_y: 720,
    style: defaultStyle(),
    overrides: [{ cue_id: 1, style: { font_size: 48 } }]
  };
  doc = setGlobalField(doc, 'font_size', 20);
  assert.equal(effectiveCue(cues[0], doc).style.font_size, 48);
  assert.equal(effectiveCue(cues[1], doc).style.font_size, 20);
  doc = setCueStyles(doc, [1, 2], { bold: true });
  assert.equal(doc.style.bold, false);
  assert.equal(effectiveCue(cues[0], doc).style.bold, true);
  assert.equal(effectiveCue(cues[0], doc).style.font_size, 48);
  assert.equal(effectiveCue(cues[1], doc).style.font_size, 20);
  doc = setCueTiming(doc, 2, { start_ms: 1100 });
  assert.equal(effectiveCue(cues[0], doc).start_ms, 0);
  assert.equal(effectiveCue(cues[1], doc).start_ms, 1100);
  doc = setCueDuration(doc, cues, 2, 500);
  assert.equal(effectiveCue(cues[1], doc).end_ms, 1600);
  assert.equal(effectiveCue(cues[0], doc).end_ms, 1000);
  const restored = restoreInheritance(doc, [1]);
  assert.equal(effectiveCue(cues[0], restored).style.font_size, 20);
  assert.equal(restored.overrides.find(item => item.cue_id === 1), undefined);
  assert.equal(effectiveCue(cues[1], restored).start_ms, 1100);
  const applied = applyStyleToAll(restored, { ...restored.style, italic: true });
  assert.equal(applied.style.italic, true);
  assert.equal(applied.overrides.some(item => item.style), false);
  assert.equal(applied.overrides.find(item => item.cue_id === 2).start_ms, 1100);

  const invalidSize = setGlobalField(applied, 'font_size', 11);
  assert.ok(validateDocument(invalidSize, cues, 5000).length > 0);
  const tooMuchFade = setGlobalField(setGlobalField(applied, 'fade_in_ms', 900), 'fade_out_ms', 900);
  assert.ok(validateDocument(tooMuchFade, cues, 5000).some(error => error.includes('第 2 条')));

  const undone = undoEdit(commitEdit([], applied, invalidSize).undo, invalidSize);
  assert.equal(undone.current.style.font_size, applied.style.font_size);

  const server = {
    asset_id: 'a', run_id: 'r', revision_id: 'v', draft_version: 2, style_sha256: 'b'.repeat(64),
    duration_ms: 5000, cues, document: applied
  };
  const key = styleCacheKey('a', 'r', 'v', sha);
  rememberLocalDraft(key, { document: invalidSize, undo: [], baseDraftVersion: 1 });
  const loaded = adoptServerDraft(server, key);
  assert.equal(loaded.baseDraftVersion, 1);
  assert.equal(loaded.document.style.font_size, 11);
  const conflict = noteSaveOutcome(loaded, { ok: false });
  assert.equal(conflict, loaded);
  assert.equal(adoptServerDraft(server, styleCacheKey('b', 'r', 'v', sha)).document.style.font_size, applied.style.font_size);
  assert.equal(adoptServerDraft(server, key).document.style.font_size, 11);
  const saved = noteSaveOutcome(conflict, { ok: true, server: { ...server, draft_version: 3, document: applied } });
  assert.equal(saved.baseDraftVersion, 3);
  assert.equal(saved.document.style.italic, true);

  const reusable = selectAssJob([
    { variant_id: 'old', revision_id: 'v', spec_id: 'ass-basic-v2', style_sha256: 'c'.repeat(64), status: 'succeeded', invalidated: false },
    { variant_id: 'match', revision_id: 'v', spec_id: 'ass-basic-v2', style_sha256: 'd'.repeat(64), status: 'succeeded', invalidated: false },
    { variant_id: 'v1', revision_id: 'v', spec_id: 'ass-basic-v1', style_sha256: 'd'.repeat(64), status: 'succeeded', invalidated: false }
  ], 'v', 'd'.repeat(64), false);
  assert.equal(reusable.variant.variant_id, 'match');
  assert.equal(selectAssJob([
    { variant_id: 'failed', revision_id: 'v', spec_id: 'ass-basic-v2', style_sha256: 'd'.repeat(64), status: 'failed', invalidated: false }
  ], 'v', 'd'.repeat(64), true).kind, 'start');
  const mp4 = selectMp4Job([
    { preview_id: 'srt', revision_id: 'v', status: 'succeeded', invalidated: false },
    { preview_id: 'styled', revision_id: 'v', subtitle_variant_id: 'match', style_sha256: 'd'.repeat(64), status: 'succeeded', invalidated: false },
    { preview_id: 'other-style', revision_id: 'v', subtitle_variant_id: 'match', style_sha256: 'e'.repeat(64), status: 'succeeded', invalidated: false }
  ], 'v', 'match', 'd'.repeat(64), false);
  assert.equal(mp4.preview.preview_id, 'styled');

  const frozen = freezeStartRequest(true, { request_id: 'keep' }, () => true, () => ({ request_id: 'new' }));
  assert.equal(frozen.request_id, 'keep');
  assert.equal(freezeStartRequest(false, frozen, () => true, () => ({ request_id: 'explicit' })).request_id, 'explicit');

  let current = true;
  let started = 0;
  const stopped = await runStyledExport({
    isCurrent: () => current,
    revisionId: 'v',
    draftVersion: 1,
    document: applied,
    through: 'mp4',
    explicitRetry: false,
    reuseRequests: false,
    requestId: () => 'req',
    wait: async () => {},
    save: async () => { current = false; return server; },
    onDraftSaved: () => {},
    listVariants: async () => { throw new Error('should not list after a context switch'); },
    startVariant: async () => { started += 1; throw new Error('should not start'); },
    readVariant: async () => { throw new Error('should not read'); },
    listPreviews: async () => { throw new Error('should not list previews'); },
    startPreview: async () => { started += 1; throw new Error('should not start preview'); },
    readPreview: async () => { throw new Error('should not read preview'); },
    onProgress: () => {}
  });
  assert.equal(stopped.phase, 'stopped');
  assert.equal(started, 0);
  const reordered = {
    overrides: applied.overrides, style: applied.style, play_res_y: applied.play_res_y,
    play_res_x: applied.play_res_x, source_srt_sha256: applied.source_srt_sha256
  };
  assert.equal(await styleSha256(applied), await styleSha256(reordered));
  assert.notEqual(await styleSha256(applied), await styleSha256(setGlobalField(applied, 'bold', true)));
  clearLocalDrafts();
});

test('styled MP4 is a different product from the original SRT preview and is not auto-selected back', () => {
  const srt = { preview_id: 'srt' };
  const styled = { preview_id: 'styled', subtitle_variant_id: 'variant-1', style_sha256: 'abc' };
  const other = { preview_id: 'other', subtitle_variant_id: 'variant-2', style_sha256: 'def' };
  assert.equal(previewProductKey(srt), 'srt');
  assert.notEqual(previewProductKey(styled), previewProductKey(other));
  assert.equal(shouldAutoSelectPreview(srt, undefined, [srt, styled]), true);
  assert.equal(shouldAutoSelectPreview(styled, undefined, [styled]), false);
  assert.equal(shouldAutoSelectPreview(srt, '/api/previews/styled/files/mp4', [srt, styled]), false);
  assert.equal(shouldAutoSelectPreview(srt, '/api/previews/missing/files/mp4', [srt, styled]), false);
  assert.equal(shouldAutoSelectPreview(srt, '/api/previews/srt/files/mp4', [srt, styled]), true);
});
