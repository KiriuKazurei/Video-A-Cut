import test from 'node:test';
import assert from 'node:assert/strict';
import { fetchAuditLogs, hasHumanAccessToken, setHumanAccessToken } from './src/api.ts';
import { skillApi } from './src/skillApi.ts';
import { endHumanSession } from './src/humanSession.ts';
import { REFRESH_DOMAINS } from './src/queryRefresh.ts';
import {
  consentAfterVisibility, historyMoves, humanUnlockMessage, isHumanUnlockRequired, projectDraft,
  rangeFromSegments, revisionLabel, runDetailKey, sameIds, sameRevisionDelivery, splitPlanSegment
} from './src/skillView.ts';
import { ApiError } from './src/api.ts';

test('403 on the audit probe is not treated as a missing route', async () => {
  const previous = globalThis.fetch;
  const urls = [];
  const writes = [];
  const priorStorage = globalThis.localStorage;
  globalThis.localStorage = { setItem: (key) => writes.push(key), getItem: () => null, removeItem() {}, clear() {}, key: () => null, length: 0 };
  try {
    globalThis.fetch = async (url, init = {}) => {
      urls.push(`${init.method || 'GET'} ${url}`);
      if (String(url).endsWith('/api/human-session') && init.method === 'POST') {
        return json({ unlocked: true });
      }
      if (String(url).includes('/api/v1/audit/logs')) {
        const headers = new Headers(init.headers);
        assert.equal(headers.get('X-VAC-Human-Token'), 'memory-token-not-stored-in-browser');
        return json({ error: { code: 'forbidden', message: 'human session required' } }, 403);
      }
      return json({ error: { code: 'not_found', message: 'missing' } }, 404);
    };
    await skillApi.unlockHumanSession('memory-token-not-stored-in-browser');
    assert.equal(hasHumanAccessToken(), true);
    await assert.rejects(() => fetchAuditLogs(5), (error) => error instanceof ApiError && error.status === 403 && isHumanUnlockRequired(error));
    assert.equal(urls.filter((item) => item.includes('audit')).length, 1);
    assert.equal(writes.length, 0);
    assert.match(humanUnlockMessage(), /403/);
  } finally {
    setHumanAccessToken('');
    globalThis.fetch = previous;
    if (priorStorage === undefined) delete globalThis.localStorage;
    else globalThis.localStorage = priorStorage;
  }
});

test('locking clears the memory token and protected query caches', async () => {
  const previous = globalThis.fetch;
  const removed = [];
  try {
    globalThis.fetch = async (url, init = {}) => {
      if (init.method === 'DELETE') return json({ locked: true });
      return json({ error: { code: 'forbidden', message: 'no' } }, 403);
    };
    setHumanAccessToken('still-in-memory');
    await endHumanSession({ removeQueries: async ({ queryKey }) => { removed.push(queryKey[0]); } });
    assert.equal(hasHumanAccessToken(), false);
    for (const key of ['audit', 'vu-projects', 'vu-run', 'vu-context']) assert.ok(removed.includes(key));
  } finally {
    setHumanAccessToken('');
    globalThis.fetch = previous;
  }
});

test('project drafts, history, delivery pairing and edit ranges stay on the skill contract', () => {
  const draft = projectDraft({
    id: 'p', name: '演示', flow: 'lightweight', version: 4, assets: ['a'], visible: ['a'],
    confirmed_sha: '', default_skills: { lightweight: 'skill-a' }, disabled_skills: ['skill-b'],
    external_authorized: true, transcription_authorized: false, created_at: '', updated_at: '',
    profile_id: 'preset', profile_revision: 2
  });
  assert.equal(draft.expected_version, 4);
  assert.equal(draft.default_skills.lightweight, 'skill-a');
  assert.deepEqual(draft.disabled_skills, ['skill-b']);
  assert.equal(sameIds(['b', 'a'], ['a', 'b']), true);
  assert.deepEqual(consentAfterVisibility(true, { external: true, transcription: true }), { external: false, transcription: false });
  const history = { cursor: { checkpoint_id: 'child', redo: [] }, checkpoints: [
    { id: 'root', parent_id: '', snapshot: {}, sha256: 'r' },
    { id: 'child', parent_id: 'root', snapshot: {}, sha256: 'c' }
  ] };
  assert.deepEqual(historyMoves(history), { undo: true, redo: false });
  assert.deepEqual(historyMoves({ ...history, cursor: { checkpoint_id: 'child', redo: ['old'] } }), { undo: true, redo: true });
  const paired = sameRevisionDelivery([
    { id: 'm', name: 'film.mp4', kind: 'mp4', size_bytes: 1, sha256: 'm', plan_version: 3 },
    { id: 'z', name: 'cut.zip', kind: 'premiere_package', size_bytes: 1, sha256: 'z', plan_version: 3 },
    { id: 'old', name: 'old.mp4', kind: 'mp4', size_bytes: 1, sha256: 'o', plan_version: 2 }
  ], 3);
  assert.equal(paired.aligned, true);
  assert.equal(paired.mp4.id, 'm');
  assert.equal(sameRevisionDelivery(paired.mp4 ? [paired.mp4] : [], 3).aligned, false);
  const plan = { body: '第一段\n\n第二段', version: 1, subtitles: false, segments: [
    { media_id: 'm', start: 0, end: 4, narration: '甲', tts_enabled: true, voice_offset: 0, voice_consent: true },
    { media_id: 'm', start: 4, end: 8, narration: '乙', tts_enabled: true, voice_offset: 0, voice_consent: false }
  ] };
  const split = splitPlanSegment(plan, 0);
  assert.equal(split.segments.length, 3);
  assert.equal(split.segments[1].voice_consent, false);
  assert.equal(split.body.split(/\n\s*\n/).length, 3);
  assert.equal(split.finalized_by, undefined);
  const windows = [
    { segment_index: 0, media_id: 'm', timeline_start: 0, timeline_end: 2, source_start: 10, source_end: 12 },
    { segment_index: 1, media_id: 'm', timeline_start: 2, timeline_end: 5, source_start: 12, source_end: 16 }
  ];
  assert.deepEqual(rangeFromSegments(windows, 0, 1), { timeline_start: 0, timeline_end: 5 });
  assert.throws(() => rangeFromSegments(windows, 0, 3));
  const key = runDetailKey('project', 'run', 'workbench');
  assert.equal(key[0], 'vu-run');
  assert.ok(REFRESH_DOMAINS.includes(key[0]));
  assert.equal(revisionLabel({ plan_version: 7 }, 0), '修订版本 7');
  assert.throws(() => runDetailKey('', 'run', 'steps'));
});

function json(body, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { 'content-type': 'application/json' } });
}
