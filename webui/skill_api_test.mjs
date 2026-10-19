import test from 'node:test';
import assert from 'node:assert/strict';
import { skillApi } from './src/skillApi.ts';
import { createSkillEdit, skillPreviewSource } from './src/skillEditing.ts';
import { handleSSEEvent, REFRESH_DOMAINS, SUPPORTED_SSE_EVENTS } from './src/queryRefresh.ts';

test('Skill client preserves CAS/idempotency and AbortSignal, and encodes object identifiers', async () => {
  const previous = globalThis.fetch;
  const controller = new AbortController();
  const request = { name: 'media.list', arguments: {}, expected_version: 3, idempotency_key: 'retry-key' };
  try {
    globalThis.fetch = async (url, init) => {
      assert.equal(url, '/api/skill-runs/run%2Fid/tools');
      assert.equal(init.signal, controller.signal);
      assert.deepEqual(JSON.parse(init.body), request);
      return new Response(JSON.stringify({ run: { version: 5 }, result: [] }), { status: 200 });
    };
    const result = await skillApi.tool('run/id', request, controller.signal);
    assert.equal(result.run.version, 5);
  } finally { globalThis.fetch = previous; }
});

test('Skill SSE invalidates project-scoped queries; payload is never treated as authoritative data', async () => {
  const invalidated = [];
  const client = { invalidateQueries: async ({ queryKey }) => { invalidated.push(queryKey[0]); } };
  for (const event of ['project.changed','skill.changed','skill.run.changed']) {
    assert(SUPPORTED_SSE_EVENTS.includes(event));
    await handleSSEEvent(client, event, { project_id: 'p', state: 'succeeded' });
  }
  for (const key of ['vu-projects','vu-visible','vu-run','vu-context']) {
    assert(REFRESH_DOMAINS.includes(key));
    assert(invalidated.includes(key));
  }
});

test('local edits bind the displayed film revision/hash and reject a historical-only selection', () => {
  const current = { artifact: { id: 'film', sha256: 'sha-current' }, is_current: true,
    available: true, exportable: true, historical: false, checkpoint_id: '' };
  const view = { run: { version: 12, plan: { version: 3 } }, previews: [current] };
  assert.deepEqual(createSkillEdit(view, '修改开头', { at_seconds: 2.5 }, 'once'), {
    expected_version: 12, plan_version: 3, artifact_id: 'film', artifact_sha256: 'sha-current',
    prompt: '修改开头', at_seconds: 2.5, idempotency_key: 'once',
  });
  const old = { ...current, is_current: false, historical: true, exportable: false, checkpoint_id: 'cp' };
  assert.throws(() => createSkillEdit({ ...view, previews: [old] }, 'change', { segment_index: 0 }, 'key'));
  assert.equal(skillPreviewSource('run', old).historical, true);
  assert.match(skillPreviewSource('run', old).src, /checkpoints\/cp\/artifacts\/film$/);
});
