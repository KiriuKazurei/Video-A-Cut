import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const css = readFileSync(new URL('./src/styles.css', import.meta.url), 'utf8');
const theme = readFileSync(new URL('./src/theme.ts', import.meta.url), 'utf8');

test('reduced motion disables the tab thumb transition', () => {
  assert.match(css, /@media\s*\(\s*prefers-reduced-motion:\s*reduce\s*\)\s*\{[^}]*\.app-nav\.seg-nav::before\s*\{[^}]*transition:\s*none\s*!important/);
});

test('description rows use a 1.25 line height', () => {
  assert.match(css, /--vac-line:\s*1\.25\s*;/);
  assert.match(css, /\.ant-descriptions\s+\.ant-descriptions-item-label[\s\S]*line-height:\s*var\(--vac-line\)\s*!important/);
});

test('segmented nav keeps the Lucian slider radii on the real selectors', () => {
  assert.match(css, /\.app-nav\.seg-nav\s*\{[^}]*border-radius:\s*12px/);
  assert.match(css, /\.app-nav\.seg-nav::before\s*\{[^}]*border-radius:\s*9px/);
});

test('sider theme turns button shadows off without inheriting appTheme', () => {
  const sider = theme.slice(theme.indexOf('export const siderTheme'));
  assert.match(sider, /inherit:\s*false/);
  assert.match(sider, /primaryShadow:\s*'none'/);
  assert.match(sider, /defaultShadow:\s*'none'/);
  assert.match(sider, /dangerShadow:\s*'none'/);
  assert.match(css, /\.ant-slider[^{]*\.ant-slider-handle::after[\s\S]*box-shadow:\s*none\s*!important/);
  assert.match(css, /\.ant-tabs-tab-active\s+\.ant-tabs-tab-btn[\s\S]*text-shadow:\s*none\s*!important/);
  assert.match(css, /\.ant-tabs-nav-wrap::before[\s\S]*box-shadow:\s*none\s*!important/);
});
