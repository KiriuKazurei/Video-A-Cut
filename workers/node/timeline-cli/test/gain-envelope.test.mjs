import test from 'node:test';
import assert from 'node:assert/strict';
import { gainEnvelopeExpression, audioGainFilters } from '../src/sequence.mjs';

test('constant gain path unchanged when no keyframes', () => {
  const f = audioGainFilters({ gain_db: -3 }, { gain_db: 6 }, 2, 0, 2);
  assert.match(f, /volume=-3dB/);
  assert.match(f, /volume=6dB:enable=/);
  assert.equal(gainEnvelopeExpression({ gain_db: 6 }, 2), null);
});

test('keyframe envelope produces varying volume expression', () => {
  const effects = { gain_db: 0, gain_keyframes: [{ t: 0, db: 0 }, { t: 0.5, db: 6 }, { t: 1, db: -12 }] };
  const expr = gainEnvelopeExpression(effects, 10);
  assert.ok(expr.includes('6'));
  assert.ok(expr.includes('-12'));
  assert.ok(expr.includes('if(lt(t\\,'));
  const f = audioGainFilters({ gain_db: 0 }, effects, 10, 0, 10);
  assert.match(f, /eval=frame/);
  assert.match(f, /pow\(10/);
  assert.ok(f.includes('\\,'));
  assert.ok(!f.includes('volume=0dB:enable='));
});
