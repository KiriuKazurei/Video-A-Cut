import test from 'node:test';
import assert from 'node:assert/strict';
import { TOUR_STORAGE_KEY, hasSeenTour, markTourSeen, resetTour, shouldAutoShowTour, tourOverride } from './src/tour.ts';

const memory = () => { const m = new Map(); return { getItem: (k) => m.get(k) ?? null, setItem: (k, v) => void m.set(k, String(v)), removeItem: (k) => void m.delete(k), m }; };

test('first launch shows the tour, then never again after it is marked seen', () => {
  const s = memory();
  assert.equal(shouldAutoShowTour({ storage: s }), true);
  markTourSeen(s);
  assert.equal(s.m.get(TOUR_STORAGE_KEY), '1');
  assert.equal(hasSeenTour(s), true);
  assert.equal(shouldAutoShowTour({ storage: s }), false);
  resetTour(s);
  assert.equal(shouldAutoShowTour({ storage: s }), true);
});

test('storage key is versioned', () => { assert.match(TOUR_STORAGE_KEY, /^vac_tour_seen_v\d+$/); });

test('automation never auto-opens the tour unless ?tour=1', () => {
  assert.equal(shouldAutoShowTour({ storage: memory(), automated: true }), false);
  assert.equal(shouldAutoShowTour({ storage: memory(), automated: true, search: '?tour=1' }), true);
});

test('?tour=0 suppresses even on a first launch; query may live in the hash too', () => {
  assert.equal(shouldAutoShowTour({ storage: memory(), search: '?tour=0' }), false);
  assert.equal(shouldAutoShowTour({ storage: memory(), hash: '#/editing/import?tour=0' }), false);
  assert.equal(tourOverride('?x=1&tour=off'), 'off');
  assert.equal(tourOverride('', '#/resources'), null);
});

test('broken storage (privacy mode) does not throw and falls back to showing once', () => {
  const broken = { getItem() { throw new Error('denied'); }, setItem() { throw new Error('denied'); }, removeItem() { throw new Error('denied'); } };
  assert.equal(shouldAutoShowTour({ storage: broken }), true);
  assert.doesNotThrow(() => markTourSeen(broken));
  assert.doesNotThrow(() => resetTour(broken));
  assert.equal(shouldAutoShowTour({ storage: null }), true);
});
