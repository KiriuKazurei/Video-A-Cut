import test from 'node:test';
import assert from 'node:assert/strict';
import { buildLevels, pickLevel, columns, drawColumns, barHeight } from './src/loudness.ts';

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
test('drawing paints mirrored bars and clip ticks', () => {
  const calls = [];
  const ctx = { clearRect() {}, fillRect(...a) { calls.push([ctx.fillStyle, ...a]); }, fillStyle: '', globalAlpha: 1 };
  drawColumns(ctx, [{ peakDb: 0, rmsDb: -30, clipped: true }, { peakDb: -Infinity, rmsDb: -Infinity, clipped: false }], 40, -60);
  assert.equal(barHeight(-30, -60), 0.5);
  assert.ok(calls.some(c => c[1] === 0 && c[2] === 0 && c[4] === 40));
  assert.ok(calls.some(c => c[1] === 0 && c[2] === 10 && c[4] === 20));
  assert.equal(calls.filter(c => c[0] === '#DC4446').length, 2);
  assert.ok(!calls.some(c => c[1] === 1));
});
