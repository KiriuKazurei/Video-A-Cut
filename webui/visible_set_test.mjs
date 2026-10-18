// 可见素材确认的变异测试（node --experimental-strip-types --test visible_set_test.mjs）
import test from 'node:test';
import assert from 'node:assert/strict';
import { enterScriptAllowed, startGenerationAllowed } from './src/visibleSet.ts';
import { decisionHeadline, autoPanelModel, buildAutoRunBody, canStartAuto } from './src/autoRun.ts';
import { derivePipeline } from './src/pipeline.ts';

const asset = () => ({
  asset_id: 'a1', status: 'ingested', agent_visible: true, human_approved: false, locked: false,
  allowed_agents: [], artifacts: {}, created_at: '', updated_at: '', input_kind: 'edl_package'
});

test('unconfirmed visible set cannot enter the script or start generation', () => {
  assert.equal(enterScriptAllowed({ confirmed: false, assetVisible: true, hasWorkflow: false }), false);
  assert.equal(enterScriptAllowed({ confirmed: true, assetVisible: false, hasWorkflow: false }), false);
  assert.equal(enterScriptAllowed({ confirmed: true, assetVisible: true, hasWorkflow: false }), true);
  assert.equal(enterScriptAllowed({ confirmed: false, assetVisible: true, hasWorkflow: true }), true, 'an in-progress run stays reachable');
  assert.equal(startGenerationAllowed(false), false);
  assert.equal(startGenerationAllowed(true), true);
});

test('pipeline sends an unconfirmed library to the resource page before preparation', () => {
  const blocked = derivePipeline({
    asset: asset(), assetCount: 1, route: { workspace: 'resources' }, workflow: null, visibleConfirmed: false
  });
  assert.equal(blocked.next.label, '去确认可见素材');
  assert.deepEqual(blocked.next.target.route, { workspace: 'resources' });
  assert.equal(blocked.stages.find((s) => s.id === 'prepare').status, 'blocked');
  const running = derivePipeline({
    asset: asset(), assetCount: 1, route: { workspace: 'editing', editingSection: 'content' },
    workflow: { run_id: 'r', asset_id: 'a1', base_revision_id: 'b', current_revision_id: 'c', status: 'running', stage: 'recognize', version: 1, content_mode: 'configured' },
    visibleConfirmed: false
  });
  assert.equal(running.next.label, '查看生成进度');
  const hidden = derivePipeline({
    asset: { ...asset(), agent_visible: false }, assetCount: 1, route: { workspace: 'resources' }, workflow: null, visibleConfirmed: false
  });
  assert.match(hidden.next.disabledReason, /对 Agent 不可见/);
});

test('auto authorization carries the visible-set fingerprint only when the page has one', () => {
  const base = {
    profileId: 'p', revision: 1, expectedAssetVersion: 'av', brief: '集锦', maxRounds: 1,
    relaxConfirmed: false, noticeSha: 'abc', external: false, idempotencyKey: 'k'
  };
  assert.equal(buildAutoRunBody(base).authorization.visible_set_sha, undefined);
  assert.equal(buildAutoRunBody({ ...base, visibleSetSha: 'ff' }).authorization.visible_set_sha, 'ff');
  const ready = { authorized: true, profileOk: true, noticeReady: true, preflightOk: true, busy: false, relaxOn: false, relaxConfirmed: false };
  assert.equal(canStartAuto(ready), true);
  assert.equal(canStartAuto({ ...ready, visibleReady: false }), false);
});

test('a changed visible set stops on a decision card in plain language', () => {
  const line = decisionHeadline('visible_set_changed');
  assert.match(line.title, /可见/);
  assert.match(line.description, /确认/);
  assert.doesNotMatch(`${line.title} ${line.description}`, /[a-z]+_[a-z]+/);
  const panel = autoPanelModel({
    run_id: 'r', auto_version: 2, state: 'awaiting_user_decision', halt_reason: 'visible_set_changed',
    scope: { external_egress: false, max_rounds: 1, max_total_rounds: 5, thresholds: {}, relaxed_thresholds: false },
    round: 0, max_rounds: 1, total_rounds: 0, max_total_rounds: 5, delivery_hold: true, latest_review: null,
    delivery: { delivered: false, package_ready: false, preview_is_current: false, spec: '720p' },
    decision: { reason: 'visible_set_changed', can_export: false, can_revise: false }, delivery_hold_enforced: false
  });
  assert.equal(panel.kind, 'decision');
  assert.match(panel.title, /可见/);
});
