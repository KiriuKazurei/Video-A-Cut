import { createHash } from 'node:crypto';
import assert from 'node:assert/strict';
import { access, mkdir, mkdtemp, readFile, rename, rm, writeFile } from 'node:fs/promises';
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

test('export keeps sampled evidence and rejects a forged scene reference', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-pipeline-evidence-'));
  t.after(async () => { const { rm } = await import('node:fs/promises'); await rm(root, { recursive: true, force: true }); });
  const source = join(root, 'source');
  await mkdir(join(source, 'samples'), { recursive: true });
  const edl = await readEdl(fixture);
  const bytes = Buffer.from('jpeg-evidence');
  const digest = createHash('sha256').update(bytes).digest('hex');
  await writeFile(join(source, 'samples', 'clip_0_frame.jpg'), bytes);
  await writeFile(join(source, 'samples', 'evidence-manifest.json'), JSON.stringify({
    schema_version: 1,
    model_version: 'test-model',
    sampling_config_version: 'sampling-v1',
    frames: [{ sha256: digest, path: 'samples/clip_0_frame.jpg', source_name: 'frame.jpg' }]
  }));
  edl.scenes = [{ index: 0, label: '关卡', evidence_frames: [digest] }];
  const edlPath = join(source, 'edl.json');
  await writeFile(edlPath, JSON.stringify(edl));
  const ports = Object.fromEntries(['cuts', 'timebase', 'tracks', 'sync', 'formats', 'audio', 'subtitles']
    .map((name) => [name, { async run({ stagingDir }) {
      if (name === 'sync') return { ok: true, issues: [] };
      if (name === 'formats') {
        await writeFile(join(stagingDir, 'timeline.otio'), 'fixture');
        return [{ kind: 'otio', path: 'timeline.otio' }];
      }
      return [];
    } }]));
  const outputDir = join(root, 'delivery');
  const manifest = await buildDelivery({ edl, edlPath, outputDir, ports });
  assert.equal(manifest.artifacts.some((item) => item.kind === 'evidence_manifest'), true);
  const copied = await readFile(join(outputDir, 'samples', 'clip_0_frame.jpg'));
  assert.equal(createHash('sha256').update(copied).digest('hex'), digest);
  const forgedDir = join(root, 'forged');
  edl.scenes[0].evidence_frames = ['invented-reference'];
  await assert.rejects(buildDelivery({ edl, edlPath, outputDir: forgedDir, ports }), /invented-reference/);
  await missing(forgedDir);
});

test('ingested packages carry source-map and provenance and map the final timeline by segment_id', async (t) => {
  const root = await mkdtemp(join(tmpdir(), 'vac-pipeline-ingest-'));
  t.after(async () => { await rm(root, { recursive: true, force: true }); });
  const source = join(root, 'source');
  await mkdir(source, { recursive: true });
  const edl = await readEdl(fixture);
  const clip = edl.video[0];
  // Two clips in reversed order, as after a sort stage; mapping must follow segment_id.
  edl.video = [
    { ...clip, in: 0, out: 2, timeline_in: 0, ingest: { segment_id: 'seg_0002', frame_quota: 2 } },
    { ...clip, in: 0.5, out: 1.5, timeline_in: 2, ingest: { segment_id: 'seg_0001', frame_quota: 3 } }
  ];
  edl.game_audio = [];
  await writeFile(join(source, 'source-map.json'), JSON.stringify({ schema_version: 1, source_id: 'src_1', source_sha256: 'a'.repeat(64),
    segments: [{ segment_id: 'seg_0001', source_start_us: 1_000_000, source_end_us: 3_000_000 },
      { segment_id: 'seg_0002', source_start_us: 40_000_000, source_end_us: 41_990_000 }] }));
  await writeFile(join(source, 'ingest-provenance.json'), JSON.stringify({ schema_version: 1, run_id: 'ing_1' }));
  const edlPath = join(source, 'edl.json');
  await writeFile(edlPath, JSON.stringify(edl));
  const ports = Object.fromEntries(['cuts', 'timebase', 'tracks', 'sync', 'formats', 'audio', 'subtitles']
    .map((name) => [name, { async run({ stagingDir }) {
      if (name === 'sync') return { ok: true, issues: [] };
      if (name === 'formats') {
        await writeFile(join(stagingDir, 'timeline.otio'), 'fixture');
        return [{ kind: 'otio', path: 'timeline.otio' }];
      }
      return [];
    } }]));
  const outputDir = join(root, 'delivery');
  const manifest = await buildDelivery({ edl, edlPath, outputDir, ports });
  const kinds = manifest.artifacts.map((item) => item.kind);
  for (const kind of ['source_map', 'ingest_provenance', 'ingest_timeline']) assert.equal(kinds.includes(kind), true, kind);
  const timeline = JSON.parse(await readFile(join(outputDir, 'ingest-timeline.json'), 'utf8'));
  assert.deepEqual(timeline.segments.map((row) => [row.segment_id, row.timeline_in, row.source_start_us, row.source_end_us]),
    [['seg_0002', 0, 40_000_000, 41_990_000], ['seg_0001', 2, 1_500_000, 2_500_000]]);
  assert.equal(timeline.segments[0].timeline_out, 2, 'CFR padding remains on the output timeline');

  edl.video[1].ingest.segment_id = 'seg_9999';
  await writeFile(edlPath, JSON.stringify(edl));
  const unknown = join(root, 'unknown');
  await assert.rejects(buildDelivery({ edl, edlPath, outputDir: unknown, ports }), /seg_9999/);
  await missing(unknown);
});

test('Windows publish retries transient locks and preserves a competing destination', async () => {
  const {publishNewDirectory}=await import('../src/publish.mjs');
  const root=await mkdtemp(join(tmpdir(),'vac-publish-'));
  try {
    const source=join(root,'source'),target=join(root,'target');await mkdir(source);await writeFile(join(source,'proof'),'complete');
    let attempts=0;
    await publishNewDirectory(source,target,{platform:'win32',sleepImpl:async()=>{},renameImpl:async(a,b)=>{if(++attempts<3)throw Object.assign(Error('sharing violation'),{code:'EPERM'});await rename(a,b)}});
    assert.equal(attempts,3);assert.equal(await readFile(join(target,'proof'),'utf8'),'complete');
    const second=join(root,'second'),racing=join(root,'racing');await mkdir(second);await writeFile(join(second,'proof'),'retained');
    await assert.rejects(publishNewDirectory(second,racing,{platform:'win32',renameImpl:async()=>{throw Object.assign(Error('locked'),{code:'EPERM'})},sleepImpl:async()=>{await mkdir(racing);await writeFile(join(racing,'proof'),'other delivery')}}),{code:'EEXIST'});
    assert.equal(await readFile(join(racing,'proof'),'utf8'),'other delivery');assert.equal(await readFile(join(second,'proof'),'utf8'),'retained');
    let failures=0;
    await assert.rejects(publishNewDirectory(second,join(root,'never'),{platform:'win32',renameImpl:async()=>{failures++;throw Object.assign(Error('locked'),{code:'EPERM'})},sleepImpl:async()=>{}}),{code:'EPERM'});
    assert.equal(failures,6);
  } finally {await rm(root,{recursive:true,force:true})}
});
