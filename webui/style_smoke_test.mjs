import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';

const css = readFileSync(new URL('./src/styles.css', import.meta.url), 'utf8');
const timelineCss = readFileSync(new URL('./src/timeline.css', import.meta.url), 'utf8');
const theme = readFileSync(new URL('./src/theme.ts', import.meta.url), 'utf8');
const codeClips = readFileSync(new URL('./src/codeClips.ts', import.meta.url), 'utf8');
const legacyTimeline = readFileSync(new URL('./src/components/Timeline.tsx', import.meta.url), 'utf8');
const scriptWorkbench = readFileSync(new URL('./src/components/ScriptWorkbench.tsx', import.meta.url), 'utf8');

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

test('workspace content does not enter with translate, and lists do not fade in', () => {
  assert.doesNotMatch(css, /vac-page-in|vac-item-in|vac-fade-in/);
  assert.doesNotMatch(timelineCss, /vac-page-in|vac-item-in|vac-fade-in/);
  assert.doesNotMatch(css + timelineCss, /@keyframes\b/);
  assert.doesNotMatch(css, /\.page:not\(\[hidden\]\)\s*\{[^}]*animation:\s*(?!none)/);
  assert.doesNotMatch(css, /\.ant-list-item[^{]*\{[^}]*animation:\s*(?!none)/);
  assert.doesNotMatch(css, /\.asset-card\s*\{[^}]*animation:/);
});

test('accent, code identity, and legacy audio bars stay on the approved tokens', () => {
  assert.match(css, /--vac-accent:\s*#5492FF/i);
  assert.match(css, /--vac-clip-code:\s*#3A7D86/i);
  assert.match(theme, /export const ACCENT = '#5492FF'/);
  assert.match(theme, /export const CLIP_CODE = '#3A7D86'/);
  assert.match(codeClips, /var\(--vac-clip-code\)/);
  assert.doesNotMatch(codeClips, /--vac-accent|--vac-clip-effect|status-cyan|warning|success|danger/);
  const audio = legacyTimeline.match(/audio:\s*\{[^}]+\}/)?.[0] ?? '';
  assert.match(audio, /--vac-clip-audio/);
  assert.doesNotMatch(audio, /status-cyan|warning|success|danger/);
});

test('script insert controls are ant design buttons, not custom chips', () => {
  assert.doesNotMatch(scriptWorkbench, /<button[^>]*className="script-chip/);
  assert.doesNotMatch(scriptWorkbench, /<button[^>]*className="script-cheat-row/);
  assert.match(scriptWorkbench, /<Button size="small" disabled=\{!canEdit\} onClick=\{\(\) => place\(SUBTITLE_INSERT\)\}>字幕<\/Button>/);
  assert.match(scriptWorkbench, /<Button key=\{row\.label\} size="small" block className="script-cheat-row"/);
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
