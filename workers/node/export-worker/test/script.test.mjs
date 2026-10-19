import { test } from 'node:test';
import assert from 'node:assert/strict';
import { renderFixedScript } from '../src/script.mjs';

test('legacy exporter consumes exact finalized segments and their synthesized voice through shared renderer', async () => {
  let input;
  const snapshot = { script_id: 'scr', version: 3, visible_set_sha: 'sha',
    sources: { m: 'sources/source.mp4' }, workbench: { version: 3, script: { status: 'finalized', finalized_by: 'human:test' },
      segments: [{ media_id: 'm', start: 2, end: 5, narration: '定稿原文', subtitles_enabled: false }],
      document: { paragraphs: [{ effect: '白屏' }] }, sources: [], subtitles_enabled: true } };
  await renderFixedScript({ snapshot, edl: { voice: [{ id: 'vo1', src: 'voice/line.wav', start: .25 }] },
    edlPath: '/delivery/input/edl.json', cfg: { deliveryRoot: '/delivery' }, outputDir: '/delivery/output',
    resolveUnderRoot: async (_root, ref) => '/resolved/'+ref,
    render: async value => { input = value; return {}; } });
  assert.equal(input.script.version, 3);
  assert.equal(input.segments[0].start, 2);
  assert.equal(input.segments[0].narration, '定稿原文');
  assert.equal(input.segments[0].subtitles_enabled, false);
  assert.equal(input.segments[0].voice_offset, .25);
  assert.equal(input.document.paragraphs[0].effect, '白屏');
});
