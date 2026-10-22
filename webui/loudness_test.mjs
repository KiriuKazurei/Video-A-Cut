import test from 'node:test';
import assert from 'node:assert/strict';
import { buildLevels, pickLevel, columns, drawColumns, barHeight, loudnessRetry, loudnessRetryDelay, loudnessRefetchInterval, visibleSpan, sourceWindow, totalGain, loudnessDisplayGain, LOUDNESS_CONFLICT_RETRIES } from './src/loudness.ts';

const b64 = bytes => Buffer.from(Uint8Array.from(bytes)).toString('base64');
const env = (peak, rms = peak, bucket = 0.02) => ({ version: 1, stream_index: 1, duration: peak.length * bucket, bucket_seconds: bucket, buckets: peak.length, floor_db: -60, clip_db: -0.1, encoding: 'u8-dbfs', peak: b64(peak), rms: b64(rms), clipped_buckets: 0 });

test('pyramid halves until small, keeping max peak', () => {
  const peak = Array.from({ length: 1000 }, (_, i) => i % 256);
  const d = buildLevels(env(peak));
  assert.equal(d.levels[0].peak.length, 1000);
  assert.equal(d.levels[1].peak.length, 500);
  assert.equal(d.levels[1].peak[0], 1); assert.ok(d.levels.at(-1).peak.length <= 64);
  assert.equal(d.levels[1].bucketSeconds, 0.04);
  assert.equal(Math.max(...d.levels.at(-1).peak), 255);
});
test('level selection follows zoom without exceeding pixel width', () => {
  const d = buildLevels(env(new Array(200000).fill(10)));
  assert.equal(pickLevel(d.levels, 0.01), 0);
  assert.equal(pickLevel(d.levels, 0.02), 0);
  assert.equal(pickLevel(d.levels, 0.05), 1);
  const coarse = pickLevel(d.levels, 3600 / 800);
  assert.ok(d.levels[coarse].bucketSeconds <= 4.5 && d.levels[coarse + 1].bucketSeconds > 4.5);
});
test('columns honor trimmed source range and clip gain', () => {
  const peak = [...new Array(50).fill(0), ...new Array(50).fill(255)];
  const d = buildLevels(env(peak));
  const head = columns(d, 0, 1, 10), tail = columns(d, 1, 2, 10);
  assert.ok(head.every(c => c.peakDb === -Infinity));
  assert.ok(tail.every(c => Math.abs(c.peakDb) < 1e-9 && c.clipped));
  const quiet = columns(d, 1, 2, 10, -12);
  assert.ok(quiet.every(c => Math.abs(c.peakDb + 12) < 1e-9 && !c.clipped));
  assert.deepEqual(columns(d, 2, 1, 10), []);
});
test('hour-long envelope stays cheap per redraw', () => {
  const d = buildLevels(env(Array.from({ length: 180000 }, (_, i) => (i * 7) % 256)));
  const t = performance.now();
  for (let i = 0; i < 60; i++) columns(d, i * 10, i * 10 + 1800, 2400);
  assert.ok(performance.now() - t < 1500, 'sixty 2400px redraws');
});
test('drawing paints bottom-aligned bars and top clip ticks', () => {
  const calls = [];
  const ctx = { clearRect() {}, fillRect(...a) { calls.push([ctx.fillStyle, ...a]); }, fillStyle: '', globalAlpha: 1 };
  drawColumns(ctx, [{ peakDb: 0, rmsDb: -30, clipped: true }, { peakDb: -Infinity, rmsDb: -Infinity, clipped: false }], 40, -60);
  assert.equal(barHeight(-30, -60), 0.5);
  // full-scale peak: from y=0 height 40; half-scale RMS: from y=20 height 20
  assert.ok(calls.some(c => c[0] === '#9A9A9A' && c[1] === 0 && c[2] === 0 && c[4] === 40));
  assert.ok(calls.some(c => c[0] === '#E6E6E6' && c[1] === 0 && c[2] === 20 && c[4] === 20));
  assert.equal(calls.filter(c => c[0] === '#DC4446').length, 1);
  assert.ok(calls.some(c => c[0] === '#DC4446' && c[2] === 0 && c[4] === 2));
  assert.ok(!calls.some(c => c[1] === 1));
  // half-height peak (-30 dB) rises from bottom (y=20), not from the top
  const half = [];
  const ctx2 = { clearRect() {}, fillRect(...a) { half.push([ctx2.fillStyle, ...a]); }, fillStyle: '', globalAlpha: 1 };
  drawColumns(ctx2, [{ peakDb: -30, rmsDb: -45, clipped: false }], 40, -60);
  assert.ok(half.some(c => c[0] === '#9A9A9A' && c[2] === 20 && c[4] === 20), 'half-height peak bottom-aligned');
});
test('409 conflicts back off and retry several times; other errors retry once', () => {
  const conflict = { status: 409 };
  for (let i = 0; i < LOUDNESS_CONFLICT_RETRIES; i++) assert.equal(loudnessRetry(i, conflict), true);
  assert.equal(loudnessRetry(LOUDNESS_CONFLICT_RETRIES, conflict), false);
  assert.ok(LOUDNESS_CONFLICT_RETRIES >= 3);
  assert.equal(loudnessRetry(0, { status: 500 }), true); assert.equal(loudnessRetry(1, { status: 500 }), false);
  assert.equal(loudnessRetry(1, new Error('x')), false);
  assert.ok(loudnessRetryDelay(1) > loudnessRetryDelay(0)); assert.ok(loudnessRetryDelay(20) <= 15000);
  assert.ok(loudnessRefetchInterval('error') > 0); assert.equal(loudnessRefetchInterval('success'), false);
});
test('visible span covers only the viewport plus overscan', () => {
  // clip 12000px at lane x=0, viewport 981px wide incl. 148px heading, scrolled 5000px
  assert.deepEqual(visibleSpan(5000, 981, 0, 12000), [4744, 6089]);
  assert.deepEqual(visibleSpan(0, 981, 0, 12000), [0, 1089]);
  assert.deepEqual(visibleSpan(0, 981, 5000, 300), [0, 0]); // off-screen right
  assert.deepEqual(visibleSpan(20000, 981, 0, 300), [300, 300]); // off-screen left
  assert.deepEqual(visibleSpan(0, 981, 100, 300), [0, 300]);
});
test('source window maps pixel span through trim in/out', () => {
  assert.deepEqual(sourceWindow(10, 20, 1000, [0, 1000]), [10, 20]);
  assert.deepEqual(sourceWindow(10, 20, 1000, [250, 500]), [12.5, 15]);
});
test('total gain adds clip and track gain', () => {
  assert.equal(totalGain({ effects: { gain_db: -6 } }, { gain_db: 3 }), -3);
  assert.equal(totalGain({ effects: {} }, {}), 0);
});

test('columns accept per-pixel gain array', () => {
  const peak = new Array(100).fill(255);
  const d = buildLevels(env(peak));
  const cols = columns(d, 0, 2, 4, [0, -6, -12, -60]);
  assert.ok(Math.abs(cols[0].peakDb) < 1e-6);
  assert.ok(Math.abs(cols[1].peakDb + 6) < 1e-6);
  assert.equal(cols[0].clipped, true);
  assert.equal(cols[3].clipped, false);
});

test('loudnessDisplayGain ignores keyframe envelopes', () => {
  assert.equal(loudnessDisplayGain({ gain_db: -6, gain_keyframes: [{ t: 0, db: 12 }, { t: 1, db: -24 }] }, 3), -3);
  assert.equal(loudnessDisplayGain({ gain_db: 2 }, 0), 2);
  assert.notEqual(loudnessDisplayGain({ gain_db: 0, gain_keyframes: [{ t: 0, db: 12 }, { t: 1, db: 12 }] }, 0), 12);
});
