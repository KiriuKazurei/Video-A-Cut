// 自动模式前端读模型的单元测试（node --experimental-strip-types --test auto_run_test.mjs）
import test from 'node:test';
import assert from 'node:assert/strict';
import {
  AUTO_PHASES, autoErrorText, autoPanelModel, autoPolling, autoPreflightBlockers, bandLabel, buildAutoRunBody, canStartAuto, phaseIndex, reasonLines
} from './src/autoRun.ts';

const view = (over = {}) => ({
  run_id: 'r1', auto_version: 3, state: 'active', scope: { external_egress: true, max_rounds: 1, max_total_rounds: 5, thresholds: {}, relaxed_thresholds: false },
  round: 0, max_rounds: 1, total_rounds: 0, max_total_rounds: 5, delivery_hold: false, latest_review: null,
  delivery: { delivered: false, package_ready: false, preview_is_current: false, spec: '720p' }, decision: null, delivery_hold_enforced: false, ...over
});

test('phases: seven human-readable steps map from the run stage', () => {
  assert.deepEqual([...AUTO_PHASES], ['识别画面', '确认场景', '撰写解说', '配音', '字幕', '混音', '导出成片']);
  assert.equal(phaseIndex('recognize'), 0);
  assert.equal(phaseIndex('scene_review'), 1);
  assert.equal(phaseIndex('sort'), 1);
  assert.equal(phaseIndex('draft_review'), 2);
  assert.equal(phaseIndex('tts'), 3);
  assert.equal(phaseIndex('export'), 6);
  assert.equal(phaseIndex('acceptance'), 7, 'acceptance = all seven done');
  assert.equal(phaseIndex(undefined), 0);
});

test('panel: active first pass / auto rerun round N / manual revise / failed step', () => {
  assert.equal(autoPanelModel(view()).kind, 'running');
  const re = autoPanelModel(view({ round: 2, total_rounds: 2, max_rounds: 2 }));
  assert.equal(re.kind, 'rerunning');
  assert.match(re.title, /第 2 轮/);
  const rv = autoPanelModel(view({ round: 0, total_rounds: 1 }));
  assert.equal(rv.kind, 'revising');
  const failed = autoPanelModel(view(), { status: 'failed', stage: 'tts' });
  assert.equal(failed.alert, 'warning');
  assert.equal(failed.canTakeOver, true);
  assert.equal(failed.live, false);
});

test('panel: decision / done / halted / revoked / cancelled', () => {
  const d = autoPanelModel(view({ state: 'awaiting_user_decision', decision: { reason: 'medium', can_export: true, can_revise: true } }));
  assert.equal(d.kind, 'decision');
  assert.equal(d.canRevoke, true);
  const ex = autoPanelModel(view({ state: 'awaiting_user_decision', halt_reason: 'exhausted', decision: { reason: 'exhausted', can_export: false, can_revise: true } }));
  assert.match(ex.title, /多轮/);
  assert.doesNotMatch(ex.description, /直接导出/, 'no export promise when can_export=false');
  assert.match(ex.description, /没有可导出的成片/);
  assert.match(autoPanelModel(view({ state: 'awaiting_user_decision', decision: { reason: 'exhausted', can_export: true, can_revise: true } })).description, /直接导出/);
  const done = autoPanelModel(view({ state: 'completed' }));
  assert.equal(done.kind, 'done');
  assert.equal(done.canRevoke, false);
  assert.match(done.description, /不等于人工验收/);
  for (const [reason, re] of [['human_takeover', /手动改动/], ['deadline', /时间上限/], ['decision_timeout', /没有导出/], ['not_improvable', /重剪能解决/], ['budget', /次数/], ['whatever_new', /已停止/]]) {
    const h = autoPanelModel(view({ state: 'halted', halt_reason: reason }));
    assert.equal(h.kind, 'halted');
    assert.equal(h.canTakeOver, true);
    assert.match(h.description, re, reason);
    assert.doesNotMatch(h.description, /halt|auto_|actor|[a-z]+_[a-z]+/i, `${reason} leaks jargon`);
  }
  assert.equal(autoPanelModel(view({ state: 'revoked' })).kind, 'revoked');
  assert.equal(autoPanelModel(view({ state: 'cancelled' })).kind, 'cancelled');
});

test('polling only while the run is live', () => {
  assert.equal(autoPolling('active'), true);
  assert.equal(autoPolling('awaiting_user_decision'), true);
  for (const s of ['completed', 'halted', 'revoked', 'cancelled', undefined]) assert.equal(autoPolling(s), false);
});

test('review text: band labels, known reasons only, no raw codes', () => {
  assert.equal(bandLabel('high'), '高'); assert.equal(bandLabel('medium'), '中'); assert.equal(bandLabel('low'), '低'); assert.equal(bandLabel('x'), '未评');
  const lines = reasonLines({ review_id: 'x', round: 0, score: 0.7, band: 'medium', reasons: [{ code: 'low_confidence' }, { code: 'low_confidence' }, { code: 'secret_internal_code' }, { code: 'tts_incomplete' }] });
  assert.deepEqual(lines, ['部分画面的识别把握偏低', '有解说句子没有成功配音']);
  assert.deepEqual(reasonLines(null), []);
  assert.deepEqual(reasonLines({ review_id: 'x', round: 0, score: 0, band: 'low', reasons: null }), []);
});

test('start gate: authorization first; relaxing thresholds needs its own confirmation', () => {
  const ready = { authorized: true, profileOk: true, noticeReady: true, preflightOk: true, busy: false, relaxOn: false, relaxConfirmed: false };
  assert.equal(canStartAuto(ready), true);
  assert.equal(canStartAuto({ ...ready, authorized: false }), false, 'no start without 全局授权');
  assert.equal(canStartAuto({ ...ready, noticeReady: false }), false, 'no start before the notice is shown');
  assert.equal(canStartAuto({ ...ready, preflightOk: false }), false);
  assert.equal(canStartAuto({ ...ready, busy: true }), false);
  assert.equal(canStartAuto({ ...ready, relaxOn: true, relaxValue: 0.8 }), false, 'relaxed but not confirmed');
  assert.equal(canStartAuto({ ...ready, relaxOn: true, relaxValue: 0.8, relaxConfirmed: true }), true);
  assert.equal(canStartAuto({ ...ready, relaxOn: true, relaxValue: 0.79, relaxConfirmed: true }), false, 'below the hard floor 0.80');
  assert.equal(canStartAuto({ ...ready, relaxOn: true, relaxValue: undefined, relaxConfirmed: true }), false);
  assert.equal(canStartAuto({ ...ready, relaxOn: true, relaxValue: 0.85 }), true, 'not actually looser → no confirmation needed');
});

test('request body: relax_thresholds only when confirmed and truly looser; notice hash and egress flag carried', () => {
  const base = { profileId: 'p', revision: 2, expectedAssetVersion: 'av', brief: '  做个集锦  ', maxRounds: 2, relaxConfirmed: false, noticeSha: 'abc', external: true, idempotencyKey: 'k1' };
  const b = buildAutoRunBody(base);
  assert.deepEqual(b, { profile_id: 'p', revision: 2, expected_asset_version: 'av', brief: '做个集锦', scope: { max_rounds: 2 }, authorization: { accept_external_egress: true, notice_sha256: 'abc' }, idempotency_key: 'k1' });
  assert.equal('relax_thresholds' in b.scope, false);
  const unconfirmed = buildAutoRunBody({ ...base, relaxHighScore: 0.8 });
  assert.equal('thresholds' in unconfirmed.scope, false, 'unconfirmed relax never reaches the server');
  const relaxed = buildAutoRunBody({ ...base, relaxHighScore: 0.8, relaxConfirmed: true });
  assert.deepEqual(relaxed.scope, { max_rounds: 2, thresholds: { high_score_min: 0.8 }, relax_thresholds: true });
  const same = buildAutoRunBody({ ...base, relaxHighScore: 0.85, relaxConfirmed: true });
  assert.deepEqual(same.scope, { max_rounds: 2 });
  assert.equal(buildAutoRunBody({ ...base, external: false }).authorization.accept_external_egress, false);
});

test('preflight: external_authorization does not block (the global authorization grants it); others do', () => {
  const checks = [
    { code: 'external_authorization', status: 'blocked', message: '未授权外发' },
    { code: 'asset_governance', status: 'blocked', message: '素材对 Agent 不可见' },
    { code: 'worker_vision', status: 'passed', message: 'ok' }
  ];
  assert.deepEqual(autoPreflightBlockers(checks), ['素材对 Agent 不可见']);
  assert.deepEqual(autoPreflightBlockers(undefined), []);
});

test('errors become plain Chinese, never the backend sentence', () => {
  const e = (status, code, message) => Object.assign(new Error(message), { status, code });
  assert.match(autoErrorText(e(409, 'conflict', 'service: auto version conflict')), /刚刚发生了变化/);
  assert.match(autoErrorText(e(409, 'invalid_state', 'service: auto run is halted')), /无法这样操作/);
  assert.match(autoErrorText(e(400, 'argument', 'thresholds high_score_min are looser than the defaults; confirm with relax_thresholds')), /明确确认/);
  assert.match(autoErrorText(e(400, 'argument', 'service: notice_sha256 does not match')), /重新阅读/);
  assert.match(autoErrorText(e(500, 'internal', 'sql: boom')), /稍后再试/);
  assert.doesNotMatch(autoErrorText(e(500, 'internal', 'sql: boom')), /sql/);
});
