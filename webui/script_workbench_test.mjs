import test from 'node:test';
import assert from 'node:assert/strict';
import { scriptTokens, initialScriptSegment } from './src/scriptWorkbench.ts';
import { handleSSEEvent, REFRESH_DOMAINS, SUPPORTED_SSE_EVENTS } from './src/queryRefresh.ts';

test('script highlighting preserves all text, escaped braces and quoted closing brace', () => {
  const source = '自然语言\\{文本\\} {@片段.mp4}\n\n{"括号 } 与 \\"引号\\"", 字号:32px,}{白屏}';
  const tokens = scriptTokens(source);
  assert.equal(tokens.map(t => t.text).join(''), source);
  assert.equal(tokens.filter(t => t.kind === 'directive').length, 3);
  for (const invalid of ['{未知}', '{黑屏', '{{白屏}}']) assert.equal(scriptTokens(invalid)[0].kind, 'invalid');
});
test('forced media has an empty narration until a person or Agent actually writes it', () => {
  const media = [{ id: 'm', name: '片段.mp4', asset_id: 'a', in: 1, out: 3 }];
  const paragraph = { index: 0, instruction: '介绍抽卡', video: '片段.mp4' };
  assert.deepEqual(initialScriptSegment(paragraph, media), { index: 0, narration: '', media_id: 'm', start: 1, end: 3 });
  assert.equal(initialScriptSegment({ ...paragraph, video: '隐藏素材.mp4' }, media).media_id, '');
});
test('script events and reconnect refresh authoritative script data', async () => {
  const keys = [];
  await handleSSEEvent({ invalidateQueries: async ({queryKey}) => { keys.push(queryKey[0]); } }, 'script.changed', { script_id: 's' });
  for (const key of ['scripts', 'script-workbench', 'script-revisions', 'script-media', 'script-delivery', 'visible-set']) {
    assert.ok(keys.includes(key)); assert.ok(REFRESH_DOMAINS.includes(key));
  }
  assert.ok(SUPPORTED_SSE_EVENTS.includes('script.changed'));
  assert.ok(SUPPORTED_SSE_EVENTS.includes('script.rendered'));
});
