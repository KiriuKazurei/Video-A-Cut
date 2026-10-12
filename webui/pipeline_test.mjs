import test from 'node:test';
import assert from 'node:assert/strict';
import { derivePipeline, locationCrumbs, stageOfRoute, STAGE_ORDER } from './src/pipeline.ts';

const asset = (o = {}) => ({ asset_id: 'a1', status: 'ingested', agent_visible: true, human_approved: false, locked: false, allowed_agents: [], artifacts: {}, created_at: '', updated_at: '', input_kind: 'edl_package', ...o });
const wf = (status, stage = 'scene_review') => ({ run_id: 'r1', asset_id: 'a1', base_revision_id: 'b', current_revision_id: 'c', status, stage, version: 1, content_mode: 'builtin' });
const ing = (state) => ({ run_id: 'i1', asset_id: 'a1', source_id: 's', state, stage: 'probe', version: 1, analysis_revision: 1, selection_revision: 1, created_at: '', updated_at: '' });
const R = (workspace, extra = {}) => ({ workspace, ...extra });
const run = (o) => derivePipeline({ assetCount: 1, route: R('resources'), ...o });
const st = (r, id) => r.stages.find((s) => s.id === id);

test('no assets: register a recording first', () => {
  const r = run({ asset: undefined, assetCount: 0, workflow: null });
  assert.equal(r.next.label, '去登记录像');
  assert.deepEqual(r.next.target.route, { workspace: 'editing', editingSection: 'import' });
  assert.equal(r.next.disabledReason, undefined);
  assert.equal(st(r, 'prepare').status, 'blocked');
});

test('raw recording: import states drive the next step; later stages blocked with a reason', () => {
  const raw = asset({ input_kind: 'raw_recording' });
  const table = [[undefined, '去导入与粗剪'], [[], '去导入切分'], [[ing('processing')], '查看导入进度'], [[ing('awaiting_review')], '去选段并生成短片'], [[ing('failed')], '查看导入失败原因'], [[ing('cancelled')], '重新导入切分']];
  for (const [runs, label] of table) {
    const r = run({ asset: raw, ingestRuns: runs, workflow: null });
    assert.equal(r.next.label, label);
    assert.equal(r.next.stage, 'import');
    assert.equal(st(r, 'import').next, true);
    assert.equal(st(r, 'prepare').status, 'blocked');
    assert.match(st(r, 'prepare').reason, /导入与粗剪/);
  }
  assert.equal(st(run({ asset: raw, ingestRuns: [ing('failed')] }), 'import').status, 'error');
  assert.equal(st(run({ asset: raw, ingestRuns: [ing('processing')] }), 'import').status, 'active');
});

test('EDL package without a workflow: go to preparation; blocked precisely by governance', () => {
  let r = run({ asset: asset(), workflow: null });
  assert.equal(st(r, 'import').status, 'done');
  assert.equal(r.next.label, '去运行准备');
  assert.equal(r.next.disabledReason, undefined);
  assert.equal(st(r, 'prepare').status, 'todo');
  assert.equal(st(r, 'review').status, 'blocked');
  r = derivePipeline({ assetCount: 1, route: R('settings', { settingsCategory: 'preparation' }), asset: asset(), workflow: null });
  assert.equal(r.next.label, '开始生成');
  assert.equal(st(r, 'prepare').here, true);
  r = run({ asset: asset({ agent_visible: false }), workflow: null });
  assert.match(r.next.disabledReason, /对 Agent 不可见/);
  assert.equal(st(r, 'prepare').status, 'blocked');
  r = run({ asset: asset({ locked: true }), workflow: null });
  assert.match(r.next.disabledReason, /已锁定/);
});

test('workflow loading is never treated as done', () => {
  const r = run({ asset: asset(), workflow: undefined });
  assert.equal(r.next.disabledReason, '正在读取内容流程状态…');
  for (const id of ['prepare', 'generate', 'review', 'delivery']) assert.equal(st(r, id).status, 'unknown');
});

test('workflow query failure offers a clickable reload instead of stuck reading', () => {
  const r = run({ asset: asset(), workflow: null, workflowError: 'network down' });
  assert.equal(r.next.label, '重新加载流程状态');
  assert.equal(r.next.action, 'retry-workflow');
  assert.equal(r.next.disabledReason, undefined);
  assert.match(r.next.hint, /network down/);
  assert.equal(st(r, 'prepare').status, 'error');
  assert.match(st(r, 'prepare').reason, /network down/);
});

test('workflow states map to generate / review / delivery', () => {
  const table = [
    [wf('running', 'recognize'), '查看生成进度', 'generate', { generate: 'active', review: 'blocked', delivery: 'blocked', prepare: 'done' }],
    [wf('awaiting_review', 'scene_review'), '去确认场景', 'review', { generate: 'done', review: 'todo', delivery: 'blocked' }],
    [wf('awaiting_review', 'draft_review'), '去批准解说', 'review', { review: 'todo' }],
    [wf('failed', 'narrate'), '查看失败并重试', 'review', { generate: 'error', review: 'error' }],
    [wf('cancelled'), '重新启动流程', 'prepare', { generate: 'todo', review: 'blocked' }],
    [wf('ready_for_acceptance', 'acceptance'), '去交付与验收', 'delivery', { generate: 'done', review: 'done', delivery: 'todo' }]
  ];
  for (const [w, label, stage, states] of table) {
    const r = run({ asset: asset(), workflow: w });
    assert.equal(r.next.label, label, `${w.status}/${w.stage}`);
    assert.equal(r.next.stage, stage);
    for (const [id, s] of Object.entries(states)) assert.equal(st(r, id).status, s, `${w.status}: ${id}`);
    assert.equal(r.stages.filter((s) => s.next).length, 1);
  }
  const done = run({ asset: asset({ status: 'exported' }), workflow: wf('ready_for_acceptance', 'acceptance') });
  assert.equal(done.next.label, '查看交付结果');
  assert.equal(st(done, 'delivery').status, 'done');
  // cancelled restart is also blocked by governance
  assert.match(run({ asset: asset({ locked: true }), workflow: wf('cancelled') }).next.disabledReason, /已锁定/);
});

test('blocked stages always carry a concrete reason; no next step is a dead end', () => {
  const cases = [[asset(), null], [asset({ agent_visible: false }), null], [asset(), wf('running')], [asset(), wf('failed')], [asset(), wf('cancelled')], [asset({ input_kind: 'raw_recording' }), null]];
  for (const [a, w] of cases) {
    const r = run({ asset: a, workflow: w, ingestRuns: [] });
    for (const s of r.stages) if (s.status === 'blocked' || s.status === 'error') assert.ok(s.reason && s.reason.length > 4, `${s.id} reason`);
    assert.ok(r.next.target, 'next has a target');
    assert.equal(r.stages.length, STAGE_ORDER.length);
  }
});

test('route → stage highlight is consistent with the stage targets', () => {
  assert.equal(stageOfRoute(R('editing', { editingSection: 'import' })), 'import');
  assert.equal(stageOfRoute(R('editing', { editingSection: 'content' })), 'review');
  assert.equal(stageOfRoute(R('editing', { editingSection: 'review' })), 'review');
  assert.equal(stageOfRoute(R('settings', { settingsCategory: 'preparation' })), 'prepare');
  assert.equal(stageOfRoute(R('settings', { settingsCategory: 'logs' })), undefined);
  assert.equal(stageOfRoute(R('export', { exportSection: 'tasks' })), 'generate');
  assert.equal(stageOfRoute(R('export', { exportSection: 'acceptance' })), 'delivery');
  assert.equal(stageOfRoute(R('resources')), undefined);
  assert.equal(stageOfRoute(R('tts')), undefined);
});

test('breadcrumbs: consistent shape, clickable parents, current last', () => {
  const c = locationCrumbs(R('editing', { editingSection: 'review' }), 'a1');
  assert.deepEqual(c.map((x) => x.label), ['编辑', '审查', 'a1']);
  assert.ok(c[0].target && c[1].target && c[2].target, 'asset crumb links back to resources');
  assert.deepEqual(c[2].target, { workspace: 'resources' });
  assert.deepEqual(locationCrumbs(R('resources'), 'a1').map((x) => x.label), ['资源管理', 'a1']);
  assert.deepEqual(locationCrumbs(R('export', { exportSection: 'tasks' }), undefined).map((x) => x.label), ['导出', '任务监控', '未选择资产']);
  assert.deepEqual(locationCrumbs(R('settings', { settingsCategory: 'preparation' }), 'a1').map((x) => x.label), ['设置', '运行准备', 'a1']);
  assert.deepEqual(locationCrumbs(R('settings', { settingsCategory: 'logs' }), 'a1').map((x) => x.label), ['设置', '日志与诊断']);
  assert.deepEqual(locationCrumbs(R('tts'), 'a1').map((x) => x.label), ['TTS', '声音调控台']);
  assert.equal(locationCrumbs(R('resources'), 'a1')[1].target, undefined, 'on resources the asset is the current location');
  assert.equal(locationCrumbs(R('settings', { settingsCategory: 'logs' }), 'a1')[1].target, undefined);
});
