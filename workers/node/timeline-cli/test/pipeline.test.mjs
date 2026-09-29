import assert from 'node:assert/strict';
import { access, mkdtemp, readFile, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import test from 'node:test';
import { readEdl, validateEdl, EdlError } from '../src/edl.mjs';
import { buildDelivery, PipelineError } from '../src/pipeline.mjs';

const fixture = new URL('../fixtures/minimal.edl.json', import.meta.url);
const missing = async (path) => assert.rejects(access(path), { code: 'ENOENT' });

test('structural validation accepts the documented EDL and rejects invalid spans', async () => {
  const edl = await readEdl(fixture);
  assert.equal(edl.timeline.fps, 60);
  assert.throws(() => validateEdl({ ...edl, video: [{ ...edl.video[0], out: 0 }] }),
    (error) => error instanceof EdlError && error.message.includes('video[0].out'));
});

test('pipeline invokes every port in order, verifies files and publishes manifest', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-pipeline-'));
  t.after(async () => { const { rm } = await import('node:fs/promises'); await rm(root, { recursive: true, force: true }); });
  const outputDir = join(root, 'delivery');
  const calls = [];
  const edl = await readEdl(fixture);
  const ports = {
    cuts: { async run() { calls.push('cuts'); return { clips: 1 }; } },
    timebase: { async run({ cuts }) { assert.equal(cuts.clips, 1); calls.push('timebase'); return { fps: 60 }; } },
    tracks: { async run({ timebase }) { assert.equal(timebase.fps, 60); calls.push('tracks'); return {}; } },
    sync: { async run() { calls.push('sync'); return { ok: true, issues: [] }; } },
    formats: { async run({ stagingDir }) {
      calls.push('formats');
      await writeFile(join(stagingDir, 'timeline.otio'), 'fixture');
      return [{ kind: 'otio', path: 'timeline.otio' }];
    } },
    audio: { async run() { calls.push('audio'); return []; } },
    subtitles: { async run() { calls.push('subtitles'); return []; } }
  };
  const manifest = await buildDelivery({ edl, edlPath: fixture.pathname, outputDir, ports });
  assert.deepEqual(calls, ['cuts', 'timebase', 'tracks', 'sync', 'formats', 'audio', 'subtitles']);
  assert.equal(manifest.status, 'ready_for_import_validation');
  assert.deepEqual(JSON.parse(await readFile(join(outputDir, 'delivery-manifest.json'), 'utf8')).artifacts,
    [{ kind: 'edl', path: 'edl.json' }, { kind: 'otio', path: 'timeline.otio' }]);
  assert.equal((await readFile(join(outputDir, 'edl.json'), 'utf8')).includes('clip_01.mp4'), true);
});

test('unimplemented or failed verification never publishes a delivery directory', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-pipeline-fail-'));
  t.after(async () => { const { rm } = await import('node:fs/promises'); await rm(root, { recursive: true, force: true }); });
  const edl = await readEdl(fixture);
  const outputDir = join(root, 'delivery');
  await assert.rejects(buildDelivery({ edl, outputDir, ports: {} }), PipelineError);
  await missing(outputDir);
  const ports = Object.fromEntries(['cuts', 'timebase', 'tracks', 'sync', 'formats', 'audio', 'subtitles']
    .map((name) => [name, { async run() { return name === 'sync' ? { ok: false, issues: ['drift'] } : []; } }]));
  await assert.rejects(buildDelivery({ edl, outputDir, ports }), /sync verification did not pass/);
  await missing(outputDir);
});

test('artifact paths cannot escape staging or claim the manifest name', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-pipeline-path-'));
  t.after(async () => { const { rm } = await import('node:fs/promises'); await rm(root, { recursive: true, force: true }); });
  const edl = await readEdl(fixture);
  const outputDir = join(root, 'delivery');
  const ports = Object.fromEntries(['cuts', 'timebase', 'tracks', 'sync', 'formats', 'audio', 'subtitles']
    .map((name) => [name, { async run() {
      if (name === 'sync') return { ok: true, issues: [] };
      if (name === 'formats') return [{ kind: 'otio', path: '../outside.otio' }];
      return [];
    } }]));
  await assert.rejects(buildDelivery({ edl, outputDir, ports }), /unsafe or duplicate artifact path/);
  await missing(outputDir);
});
