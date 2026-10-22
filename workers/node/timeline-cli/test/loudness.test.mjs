import test from 'node:test';
import assert from 'node:assert/strict';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { bucketPlan, encodeLevel, decodeLevelDb, LoudnessAccumulator, loudness, decodeArgs, MAX_BUCKETS } from '../src/loudness.mjs';
const media = join(dirname(fileURLToPath(import.meta.url)), '..', 'fixtures', 'media');

test('bucket plan stays bounded for hour-long recordings', () => {
  assert.equal(bucketPlan(10).samples, 160);
  const hour = bucketPlan(3600); assert.ok(3600 / hour.bucket_seconds <= MAX_BUCKETS + 1);
  const day = bucketPlan(24 * 3600); assert.ok(24 * 3600 / day.bucket_seconds <= MAX_BUCKETS + 1);
  assert.throws(() => bucketPlan(0)); assert.throws(() => bucketPlan(NaN));
});
test('level encoding maps dBFS floor..0 onto bytes', () => {
  assert.equal(encodeLevel(0), 0); assert.equal(encodeLevel(1), 255); assert.equal(encodeLevel(2), 255); assert.equal(encodeLevel(0.0001), 0);
  assert.ok(Math.abs(decodeLevelDb(encodeLevel(0.5)) - -6.02) < 0.3);
});
test('accumulator computes peak, RMS and clipping per bucket', () => {
  const acc = new LoudnessAccumulator(4);
  acc.push(Float32Array.from([0.5, -0.5, 0.5])); acc.push(Float32Array.from([-0.5, 1, 0, 0, 0, 0.1]));
  const out = acc.finish();
  const peak = [...Buffer.from(out.peak, 'base64')], rms = [...Buffer.from(out.rms, 'base64')];
  assert.equal(out.buckets, 3); assert.equal(peak[0], encodeLevel(0.5)); assert.equal(rms[0], encodeLevel(0.5));
  assert.equal(peak[1], 255); assert.equal(rms[1], encodeLevel(0.5)); assert.equal(peak[2], encodeLevel(0.1));
  assert.equal(out.clipped_buckets, 1);
});
test('loudness streams injected PCM without buffering it', async () => {
  const r = await loudness({ path: join(media, 'voice.wav'), stream_index: 0, duration: 1 }, { channels: async () => 1, decode: async (_p, _s, on) => { for (let i = 0; i < 50; i++) on(new Float32Array(160).fill(i % 2 ? 0.25 : 0)); } });
  assert.equal(r.buckets, 50); assert.equal(r.version, 2); assert.equal(r.encoding, 'u8-dbfs');
});
test('real ffmpeg decode of fixture produces non-silent envelope', { timeout: 60000 }, async () => {
  const r = await loudness({ path: join(media, 'voice.wav'), stream_index: 0, duration: 2 });
  const peak = Buffer.from(r.peak, 'base64'); assert.ok(r.buckets > 10); assert.ok([...peak].some(v => v > 100));
});

test('stereo keeps channels: peak is max across channels, RMS is power mean', () => {
  const acc = new LoudnessAccumulator(2, 2);
  acc.push(Float32Array.from([1, 0, 1])); acc.push(Float32Array.from([0]));
  const out = acc.finish();
  assert.equal(out.buckets, 1); assert.equal(out.clipped_buckets, 1);
  assert.equal(Buffer.from(out.peak, 'base64')[0], 255);
  assert.equal(Buffer.from(out.rms, 'base64')[0], encodeLevel(Math.sqrt(0.5)));
});
test('decode args never downmix multichannel sources', () => {
  const args = decodeArgs('/x.wav', 1, 2);
  assert.equal(args[args.indexOf('-ac') + 1], '2');
});
test('real stereo file with only the left channel at full scale is flagged as clipping', { timeout: 60000 }, async () => {
  const path = join(mkdtempSync(join(tmpdir(), 'vac-loud-')), 'left.wav');
  execFileSync('ffmpeg', ['-v', 'error', '-y', '-f', 'lavfi', '-i', 'aevalsrc=exprs=if(lt(mod(t*100\\,1)\\,0.5)\\,1\\,-1)|0:s=48000:d=1', '-c:a', 'pcm_f32le', path]);
  const r = await loudness({ path, stream_index: 0, duration: 1 });
  assert.equal(r.channels, 2); assert.ok(r.clipped_buckets > 10, `clipped ${r.clipped_buckets}`);
});
