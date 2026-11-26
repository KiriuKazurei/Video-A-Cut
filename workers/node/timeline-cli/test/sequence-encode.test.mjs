import test from 'node:test';
import assert from 'node:assert/strict';
import { mkdir, readFile } from 'node:fs/promises';
import { join } from 'node:path';
import { normalizeSequenceEncode, sequenceEncodeThreads, sequenceEncodeTimeoutMs, sequenceFilterThreads, srtDocument } from '../src/sequence-encode.mjs';
import { renderSequence, sequenceFileHash } from '../src/sequence.mjs';
import { probeMedia, runTool } from '../src/media.mjs';

test('export timeout grows with duration and pixels and keeps the short-program floor', () => {
  const short = sequenceEncodeTimeoutMs({ duration: 2, width: 320, height: 180, filterCount: 4 });
  assert.equal(short, 360_000);
  const long = sequenceEncodeTimeoutMs({ duration: 30 * 60, width: 3840, height: 2160, filterCount: 8 });
  assert.ok(long > 360_000, `long export stayed at ${long}`);
  assert.ok(long <= 6 * 60 * 60_000);
  assert.equal(sequenceEncodeTimeoutMs({ duration: 10 * 60, width: 1920, height: 1080, preview: true }), 30 * 60_000);
  assert.equal(sequenceEncodeThreads(undefined, { preview: true }), 2);
  assert.equal(sequenceEncodeThreads(3), 3);
  assert.equal(sequenceEncodeThreads(99), 16);
  assert.equal(sequenceFilterThreads(8, { preview: true, explicit: true }), 1);
  assert.equal(sequenceFilterThreads(8, { explicit: true }), 4);
  assert.equal(sequenceFilterThreads(8), 1);
});

test('encode plan accepts platform presets and rejects mixed rate control', () => {
  const native = normalizeSequenceEncode(null);
  assert.equal(native.active, false);
  const vertical = normalizeSequenceEncode({ width: 1080, height: 1920, codec: 'h264', subtitles: 'sidecar', loudness: '-16', threads: 2 });
  assert.match(vertical.scale, /1080:1920/);
  assert.equal(vertical.subtitles, 'sidecar');
  assert.equal(srtDocument([{ start: 1.2, end: 3, text: '你好' }]), '1\n00:00:01,200 --> 00:00:03,000\n你好\n');
  assert.throws(() => normalizeSequenceEncode({ crf: 20, bitrate: '8M' }), /码率/);
  assert.throws(() => normalizeSequenceEncode({ codec: 'av1' }), /HEVC|H\.264/);
});

test('longer export honors resolution, sidecar subtitles and an explicit thread count', { timeout: 180000 }, async () => {
  assert.ok(process.env.VAC_TEST_OUTPUT);
  const root = join(process.env.VAC_TEST_OUTPUT, 'sequence-encode');
  await mkdir(root, { recursive: true });
  const source = join(root, 'long.mp4');
  await runTool('ffmpeg', ['-v', 'error', '-y', '-f', 'lavfi', '-i', 'color=c=red:s=320x180:r=30:d=12', '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000:duration=12', '-shortest', '-c:v', 'libx264', '-preset', 'ultrafast', '-threads', '1', '-c:a', 'aac', source], { timeoutMs: 60_000 });
  const sequence = {
    id: 's', width: 320, height: 180, fps: { num: 30, den: 1 }, sample_rate: 48000, tracks: [
      { id: 'v', kind: 'video', clips: [{ id: 'picture', resource_id: 'r', name: 'picture', stream_index: 0, at: 0, duration: 360, source_in: { value: 0, scale: 48000 }, source_out: { value: 48000 * 12, scale: 48000 }, speed: 1, effects: { scale: 1, x: 0, y: 0, crop_left: 0, crop_right: 0, crop_top: 0, crop_bottom: 0, fade_in: 0, fade_out: 0, transition: 0, gain_db: 0, subtitles: [{ text: '旁白', frame: 0, duration: 90 }] } }] },
      { id: 'a', kind: 'audio', clips: [{ id: 'sound', resource_id: 'r', name: 'sound', stream_index: 1, at: 0, duration: 360, source_in: { value: 0, scale: 48000 }, source_out: { value: 48000 * 12, scale: 48000 }, speed: 1, effects: { scale: 1, x: 0, y: 0, crop_left: 0, crop_right: 0, crop_top: 0, crop_bottom: 0, fade_in: 0, fade_out: 0, transition: 0, gain_db: 0 } }] }
    ], markers: [], groups: []
  };
  const resources = [{ id: 'r', kind: 'video', name: 'long', path: source, sha256: await sequenceFileHash(source), metadata: { duration: 12, streams: [{ index: 0, codec_type: 'video' }, { index: 1, codec_type: 'audio' }] } }];
  const output = join(root, 'vertical');
  const report = await renderSequence({
    sequence, resources, output_dir: output, content_version: 'long-vertical',
    encode: { width: 1080, height: 1920, codec: 'h264', crf: 28, subtitles: 'sidecar', loudness: '-16', threads: 1 }
  });
  assert.equal(report.frames, 360);
  assert.deepEqual([report.width, report.height], [1080, 1920]);
  const media = await probeMedia(join(output, 'program.mp4'));
  assert.deepEqual([media.video.width, media.video.height], [1080, 1920]);
  assert.ok(Math.abs(media.duration - 12) < 0.5, `duration ${media.duration}`);
  assert.match(await readFile(join(output, 'subtitles.srt'), 'utf8'), /旁白/);
  const plain = structuredClone(sequence);
  for (const track of plain.tracks) for (const clip of track.clips) {
    clip.duration = 30;
    clip.source_out = { value: 48000, scale: 48000 };
    delete clip.effects.subtitles;
  }
  const hevcDir = join(root, 'hevc');
  await renderSequence({ sequence: plain, resources, output_dir: hevcDir, content_version: 'hevc-short', encode: { codec: 'hevc', crf: 30, threads: 1 } });
  const codec = await runTool('ffprobe', ['-v', 'error', '-select_streams', 'v:0', '-show_entries', 'stream=codec_name', '-of', 'csv=p=0', join(hevcDir, 'program.mp4')], { timeoutMs: 15_000 });
  assert.match(codec, /hevc/);
});
