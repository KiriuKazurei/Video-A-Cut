import assert from 'node:assert/strict';
import { access, mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { after, before, test } from 'node:test';
import { createAdapters } from '../adapters/index.mjs';
import { generateMediaFixture } from '../scripts/generate-media.mjs';
import { readEdl } from '../src/edl.mjs';
import { probeMedia } from '../src/media.mjs';
import { buildDelivery } from '../src/pipeline.mjs';
import { toUnits } from '../src/time.mjs';

let root;
let fixture;
before(async () => {
  root = await mkdtemp(join(tmpdir(), 'vac-real-media-'));
  fixture = await generateMediaFixture(join(root, 'source'));
});
after(async () => { if (root) await rm(root, { recursive: true, force: true }); });

async function stages(edl = fixture.edl) {
  const adapters = createAdapters();
  const context = { edl, edlPath: fixture.edlPath, stagingDir: join(root, 'unused'), outputDir: join(root, 'unused-output') };
  const cuts = await adapters.cuts.run(context);
  const timebase = await adapters.timebase.run({ ...context, cuts });
  const timeline = await adapters.tracks.run({ ...context, cuts, timebase });
  const verification = await adapters.sync.run({ ...context, cuts, timebase, timeline });
  return { cuts, timebase, timeline, verification };
}

function wavRms(buffer, startSeconds, endSeconds) {
  let data = -1;
  for (let offset = 12; offset + 8 < buffer.length;) {
    const size = buffer.readUInt32LE(offset + 4);
    if (buffer.toString('ascii', offset, offset + 4) === 'data') { data = offset + 8; break; }
    offset += 8 + size + (size % 2);
  }
  assert.ok(data >= 0, 'WAV has a PCM data chunk');
  const start = data + Math.floor(startSeconds * 48000) * 4;
  const end = data + Math.floor(endSeconds * 48000) * 4;
  let sum = 0;
  let count = 0;
  for (let offset = start; offset < end; offset += 2) {
    const sample = buffer.readInt16LE(offset);
    sum += sample * sample;
    count++;
  }
  return Math.sqrt(sum / count);
}

test('all seven real ports are present', () => {
  const names = ['cuts', 'timebase', 'tracks', 'sync', 'formats', 'audio', 'subtitles'];
  const adapters = createAdapters();
  assert.deepEqual(Object.keys(adapters).sort(), names.sort());
  for (const name of names) assert.equal(typeof adapters[name].run, 'function');
});

test('documented frame table matches the checked-in planning EDL', async () => {
  const edl = await readEdl(new URL('../fixtures/sample_project.edl.json', import.meta.url));
  const expected = JSON.parse(await readFile(new URL('../fixtures/sample_expected_frames.json', import.meta.url), 'utf8'));
  const cuts = await createAdapters().cuts.run({ edl });
  assert.equal(toUnits(cuts.total_duration, edl.timeline.fps), expected.sample.expected_timeline_frames);
  assert.deepEqual(cuts.video_cuts.map((cut) =>
    [toUnits(cut.in, 30), toUnits(cut.out, 30), toUnits(cut.timeline_in, 30), toUnits(cut.timeline_out, 30)]),
  expected.tracks.video.map((item) =>
    [item.in_frame, item.out_frame, item.timeline_in_frame, item.timeline_out_frame]));
});

test('synthetic media has the promised streams and exact duration', async () => {
  const first = await probeMedia(join(fixture.root, 'clip_01.mp4'));
  const voice = await probeMedia(join(fixture.root, 'voice.wav'));
  assert.equal(first.video.fps, 30);
  assert.equal(first.audio.sampleRate, 48000);
  assert.ok(Math.abs(first.video.duration - 3) < 0.02);
  assert.equal(voice.audio.duration, 0.8);
});

test('cuts, exact boundaries and tracks agree with the synthetic frame table', async () => {
  const { cuts, timebase, timeline, verification } = await stages();
  assert.deepEqual(cuts.video_cuts.map((item) => item.timeline_in), [0, 2]);
  assert.deepEqual(timebase.video.map((item) =>
    [item.in_frame, item.out_frame, item.timeline_in_frame, item.timeline_out_frame]),
  [[0, 60, 0, 60], [15, 60, 60, 105]]);
  assert.equal(timebase.total_frames, fixture.expectedFrames);
  assert.equal(timebase.total_samples, fixture.expectedSamples);
  assert.deepEqual(timeline.tracks.map((track) => track.kind),
    ['video', 'audio', 'audio', 'audio', 'subtitle']);
  assert.deepEqual(verification, { ok: true, issues: [] });
  assert.equal(toUnits(0.3333333333333333, 30), 10);
  assert.equal(toUnits(1.6666666666666667, 30), 50);
});

test('sync rejects an EDL voice span that disagrees with the actual WAV', async () => {
  const edl = structuredClone(fixture.edl);
  edl.voice[0].end = 1.7;
  const { verification } = await stages(edl);
  assert.equal(verification.ok, false);
  assert.ok(verification.issues.some((issue) => issue.includes('real audio duration')));
});

test('real media produces populated timelines and three decodable WAV stems', async () => {
  const outputDir = join(root, 'delivery');
  const manifest = await buildDelivery({
    edl: fixture.edl, edlPath: fixture.edlPath, outputDir, ports: createAdapters()
  });
  assert.equal(manifest.status, 'ready_for_import_validation');
  assert.equal(manifest.checks.ok, true);
  const paths = manifest.artifacts.map((item) => item.path);
  assert.ok(paths.includes('edit.xml') && paths.includes('timeline.otio'));
  assert.deepEqual(paths.filter((path) => /^audio_a\d\.wav$/.test(path)).sort(),
    ['audio_a1.wav', 'audio_a2.wav', 'audio_a3.wav']);
  // The stored edl.json references only files inside the package, so the
  // package can be re-imported and rebuilt.
  const stored = JSON.parse(await readFile(join(outputDir, 'edl.json'), 'utf8'));
  for (const track of ['video', 'game_audio', 'voice', 'music']) {
    for (const item of stored[track]) {
      assert.ok(item.src.startsWith('media/') && paths.includes(item.src), `${track} src ${item.src} not packaged`);
    }
  }
  assert.equal(paths.filter((path) => path.endsWith('.mp4')).length, 2);

  const otio = JSON.parse(await readFile(join(outputDir, 'timeline.otio'), 'utf8'));
  assert.equal(otio.tracks.children.length, 4);
  const videoClips = otio.tracks.children[0].children.filter((item) => item.OTIO_SCHEMA === 'Clip.1');
  assert.equal(videoClips.length, 2);
  assert.deepEqual(videoClips.map((item) => item.source_range.duration.value), [60, 45]);
  assert.equal(videoClips[1].source_range.start_time.value, 15);
  for (const track of otio.tracks.children) {
    for (const item of track.children.filter((part) => part.OTIO_SCHEMA === 'Clip.1')) {
      await access(fileURLToPath(item.media_reference.target_url));
    }
  }
  const xml = await readFile(join(outputDir, 'edit.xml'), 'utf8');
  assert.match(xml, /<xmeml version="4">/);
  assert.equal((xml.match(/<clipitem /g) || []).length, 5);
  assert.equal((xml.match(/<sourcetrack>/g) || []).length, 5);
  assert.equal((xml.match(/<track premiereTrackType="Stereo" currentExplodedTrackIndex="0" totalExplodedTrackCount="1">/g) || []).length, 3);
  assert.match(xml, /<timecode>.*?<displayformat>NDF<\/displayformat><\/timecode>/);
  assert.match(xml, /<pixelaspectratio>square<\/pixelaspectratio>/);
  assert.match(xml, /<channelcount>2<\/channelcount>/);
  assert.match(xml, /<duration>105<\/duration>/);
  for (const match of xml.matchAll(/<pathurl>([^<]+)<\/pathurl>/g)) {
    if (process.platform === 'win32') assert.match(match[1], /^file:\/\/localhost\/[A-Za-z]%3a\//);
    await access(fileURLToPath(match[1]));
  }
  for (const stem of ['audio_a1.wav', 'audio_a2.wav', 'audio_a3.wav']) {
    const info = await probeMedia(join(outputDir, stem));
    assert.equal(info.audio.sampleRate, 48000);
    assert.equal(info.audio.channels, 2);
    assert.equal(info.audio.duration, 3.5);
  }
  const voice = await readFile(join(outputDir, 'audio_a2.wav'));
  assert.equal(wavRms(voice, 0.2, 0.5), 0);
  assert.ok(wavRms(voice, 0.8, 1.1) > 100);
  assert.equal(wavRms(voice, 2, 2.3), 0);
  const music = await readFile(join(outputDir, 'audio_a3.wav'));
  assert.ok(wavRms(music, 0.8, 1.1) < wavRms(music, 0.2, 0.5) * 0.4,
    'music must duck under the voice interval');
});

test('a missing source fails before publishing a package', async () => {
  const edl = structuredClone(fixture.edl);
  edl.video[0].src = 'missing.mp4';
  await assert.rejects(buildDelivery({
    edl, edlPath: fixture.edlPath, outputDir: join(root, 'missing-output'), ports: createAdapters()
  }), /ffprobe exited/);
});
