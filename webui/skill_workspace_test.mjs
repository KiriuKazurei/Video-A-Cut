import test from 'node:test';
import assert from 'node:assert/strict';
import { fetchAuditLogs, setProjectSession } from './src/api.ts';
import { skillApi } from './src/skillApi.ts';
import { pauseMediaPlayers } from './src/humanSession.ts';
import { projectApi } from './src/projectApi.ts';
import { REFRESH_DOMAINS } from './src/queryRefresh.ts';
import {
  consentAfterVisibility, historyMoves, humanUnlockMessage, isHumanUnlockRequired, projectDraft,
  rangeFromSegments, revisionLabel, runDetailKey, sameIds, sameRevisionDelivery, splitPlanSegment
} from './src/skillView.ts';
import { ApiError } from './src/api.ts';

test('window requests use project scope without human credentials', async () => {
  const previous = globalThis.fetch;
  const urls = [];
  try {
    setProjectSession('current-session');
    globalThis.fetch = async (url, init = {}) => {
      urls.push(String(url)); const headers = new Headers(init.headers);
      assert.equal(headers.get('X-VAC-Human-Token'), null);
      assert.equal(headers.get('X-VAC-Project-Session'), 'current-session');
      return json([]);
    };
    await fetchAuditLogs(5);
    assert.equal(urls.length, 1);
    assert.ok(!urls.some(url => url.includes('human-session')));
  } finally { setProjectSession(''); globalThis.fetch = previous; }
});

test('project file commands retain explicit save and switch decisions', async () => {
  const previous = globalThis.fetch;
  try {
    globalThis.fetch = async (url, init) => {
      assert.match(String(url), /workspace\/open$/);
      assert.deepEqual(JSON.parse(init.body), {path:'C:/project.avp', decision:'save', stop_tasks:true, expected_session:'old'});
      return json({id:'new'});
    };
    assert.equal((await projectApi.command('open', {path:'C:/project.avp', decision:'save', stop_tasks:true, expected_session:'old'})).id,'new');
  } finally { globalThis.fetch = previous; }
});

test('switching stops and detaches media players', () => {
  const previous = globalThis.document; const mediaClass = globalThis.HTMLMediaElement; const calls = [];
  class Media { pause(){calls.push('pause')} removeAttribute(key){calls.push(key)} load(){calls.push('load')} }
  globalThis.HTMLMediaElement = Media; globalThis.document = {querySelectorAll: () => [new Media()]};
  try { pauseMediaPlayers(); assert.deepEqual(calls,['pause','src','load']); }
  finally { if (previous === undefined) delete globalThis.document; else globalThis.document = previous; if (mediaClass === undefined) delete globalThis.HTMLMediaElement; else globalThis.HTMLMediaElement = mediaClass; }
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
