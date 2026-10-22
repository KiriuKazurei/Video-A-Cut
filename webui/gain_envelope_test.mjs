import test from 'node:test';
import assert from 'node:assert/strict';
import {
  dbToY, yToDb, interpolateGain, insertKeyframe, deleteKeyframe, moveKeyframe,
  shiftGain, effectiveKeyframes, normalizeKeyframes, pixelGains, constantGainEffects,
  GAIN_TOP_DB, GAIN_BOTTOM_DB
} from './src/gainEnvelope.ts';

test('db↔y: top=+12, middle=0, bottom=−24', () => {
  assert.equal(dbToY(0, 40), 20);
  assert.equal(dbToY(GAIN_TOP_DB, 40), 0);
  assert.equal(dbToY(GAIN_BOTTOM_DB, 40), 40);
  assert.ok(Math.abs(yToDb(20, 40)) < 1e-9);
  assert.ok(Math.abs(yToDb(0, 40) - 12) < 1e-9);
  assert.ok(Math.abs(yToDb(40, 40) + 24) < 1e-9);
  assert.ok(Math.abs(yToDb(dbToY(6, 40), 40) - 6) < 1e-6);
  assert.ok(Math.abs(yToDb(dbToY(-12, 40), 40) + 12) < 1e-6);
});

test('interpolate / insert / delete / move keyframes', () => {
  const base = [{ t: 0, db: 0 }, { t: 1, db: 0 }];
  assert.equal(interpolateGain(base, 0.5), 0);
  const withMid = insertKeyframe(base, 0.5, 6);
  assert.equal(withMid.length, 3);
  assert.equal(interpolateGain(withMid, 0.25), 3);
  assert.equal(interpolateGain(withMid, 0.75), 3);
  const moved = moveKeyframe(withMid, 1, 0.25, 6);
  assert.equal(moved[1].t, 0.25);
  assert.equal(interpolateGain(moved, 0.125), 3);
  const deleted = deleteKeyframe(moved, 1);
  assert.equal(deleted.length, 2);
  assert.deepEqual(deleteKeyframe([{ t: 0, db: 3 }], 0), [{ t: 0, db: 3 }]); // keep ≥1
  assert.deepEqual(shiftGain(base, -3).map(p => p.db), [-3, -3]);
  assert.deepEqual(normalizeKeyframes([{ t: 0.5, db: 1 }, { t: 0.5, db: 2 }, { t: -1, db: 0 }]), [{ t: 0, db: 0 }, { t: 0.5, db: 2 }]);
});

test('effectiveKeyframes falls back to constant gain_db', () => {
  assert.deepEqual(effectiveKeyframes({ gain_db: -6 }), [{ t: 0, db: -6 }, { t: 1, db: -6 }]);
  assert.equal(effectiveKeyframes({ gain_db: 0, gain_keyframes: [{ t: 0, db: 3 }, { t: 1, db: -3 }] })[0].db, 3);
});

test('pixelGains applies keyframe envelope plus track gain', () => {
  const effects = { gain_db: 0, gain_keyframes: [{ t: 0, db: 0 }, { t: 1, db: 12 }] };
  const g = pixelGains(effects, -3, 100, 0, 100, 5);
  assert.equal(g.length, 5);
  assert.ok(Math.abs(g[0] - (-3 + 1.2)) < 0.5); // ~0.1 along clip → ~1.2 dB clip + −3 track
  assert.ok(g[4] > g[0]);
  const flat = pixelGains({ gain_db: 4 }, 1, 100, 0, 50, 2);
  assert.deepEqual(flat, [5, 5]);
});

test('inspector constantGainEffects clears keyframes', () => {
  const next = constantGainEffects({ gain_db: 0, gain_keyframes: [{ t: 0, db: 3 }, { t: 1, db: -3 }], fade_in: 0 }, -2);
  assert.equal(next.gain_db, -2);
  assert.equal(next.gain_keyframes, undefined);
  assert.equal(next.fade_in, 0);
});
