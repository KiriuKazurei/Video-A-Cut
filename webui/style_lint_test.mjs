import assert from 'node:assert/strict';
import test from 'node:test';
import { lintText } from './scripts/lint-style.mjs';

const tokens = new Set(['#1E1E1E', '#FFFFFF', '#000000', '#5492FF', '#E6E6E6']);

function messages(filename, source) {
  return lintText(filename, source, tokens).map((item) => item.replace(/^.*?:\d+\s*/, ''));
}

test('lint catches the shapes Gate previously missed', () => {
  const cases = [
    ['a.tsx', `style={{ backdropFilter: 'blur(8px)' }}`, 'backdrop-filter'],
    ['a.tsx', `style={{ WebkitBackdropFilter: 'blur(4px)' }}`, 'backdrop-filter'],
    ['a.tsx', `style={{ borderRadius: '12px' }}`, '圆角'],
    ['a.tsx', `style={{ borderRadius: '50%' }}`, '圆角'],
    ['a.tsx', `style={{ borderTopLeftRadius: '16px' }}`, '圆角'],
    ['a.css', `.card{border-top-left-radius:20px}`, '圆角'],
    ['a.css', `.card{filter:drop-shadow(0 2px 4px #000)}`, 'drop-shadow'],
    ['a.tsx', `strokeColor={{ '0%': '#5492FF', '100%': '#FFFFFF' }}`, '渐变'],
    ['a.tsx', `strokeColor={{ from: '#108ee9', to: '#87d068' }}`, '渐变'],
    ['a.css', `.x{color:red}`, '命名色'],
    ['a.tsx', `style={{ color: 'white' }}`, '命名色'],
    ['a.css', `.x{background:oklch(0.7 0.1 200)}`, 'oklch'],
    ['a.tsx', `const svg = 'fill="%23ff00aa"'`, '色板以外'],
    ['a.css', `.seg-nav-card{border-radius:12px}`, '圆角'],
    ['a.css', `.seg-nav{border-radius:12px}`, '圆角'],
    ['a.css', `.stage-dot-extra{border-radius:50%}`, '圆角'],
    ['a.css', `.btn{box-shadow:0 2px 0 0}`, 'box-shadow'],
    ['a.css', `@keyframes vac-page-in { from { transform: translateY(6px); } }`, 'vac-page-in'],
    ['a.css', `@keyframes rise { from { transform: translateY(6px); } to { transform: none; } }`, 'translate'],
    ['a.css', `.page:not([hidden]) { animation: rise 200ms; }`, '入场'],
    ['a.css', `.asset-card { animation: rise 200ms; }`, '入场'],
    ['a.css', `.ant-list-item, .ant-alert { animation: vac-item-in 200ms; }`, 'vac-item-in'],
    ['src/codeClips.ts', `const palette=['--vac-accent','--vac-status-cyan','--vac-danger-text'];`, '身份色'],
    ['components/Timeline.tsx', `audio: { background: 'var(--vac-status-cyan)', borderColor: 'var(--vac-status-cyan)' }`, '音频条']
  ];
  for (const [file, source, needle] of cases) {
    const found = messages(file, source);
    assert.ok(found.some((item) => item.includes(needle)), `${source} 应检出 ${needle}，实际 ${found.join(' | ') || '无'}`);
  }
});

test('lint allows the shapes Gate previously false-flagged', () => {
  const cases = [
    ['a.css', `#face-btn{color:var(--vac-text)}`],
    ['a.ts', `function argb(alpha){ return alpha }`],
    ['a.css', `.ok{box-shadow:none}`],
    ['a.css', `.ok{border-radius:4px}`],
    ['a.css', `.ok{backdrop-filter:none}`],
    ['a.css', `.ok{text-shadow:none}`],
    ['a.tsx', `style={{ boxShadow: 'none', backdropFilter: 'none' }}`],
    ['a.css', `.app-nav.seg-nav{border-radius:12px}`],
    ['a.css', `.app-nav.seg-nav > .ant-menu-item,\n.app-nav.seg-nav > .ant-menu-submenu{border-radius:9px}`],
    ['a.css', `.app-nav.seg-nav::before{border-radius:9px}`],
    ['a.css', `.stage-dot{border-radius:50%}`],
    ['a.css', `.stage-next-dot{border-radius:50%}`],
    ['a.css', `.ok{color:#fff;background:#1E1E1E}`],
    ['a.tsx', `fill="%231E1E1E"`],
    ['a.css', `.ok{border-top-left-radius:4px}`],
    ['a.ts', `export const borderRadius = 3`],
    ['a.css', `.page:not([hidden]) { animation: none; display: flex; }`],
    ['a.css', `.app-nav.seg-nav::before { transform: translateX(var(--nav-thumb-x)); }`],
    ['src/codeClips.ts', `colors[id]='var(--vac-clip-code)'`],
    ['components/Timeline.tsx', `audio: { background: 'var(--vac-clip-audio)', borderColor: 'var(--vac-clip-audio)' }`],
    ['components/Timeline.tsx', `ok: { background: 'var(--vac-success)' }`]
  ];
  for (const [file, source] of cases) {
    const found = messages(file, source);
    assert.deepEqual(found, [], `${source} 不应报错，实际 ${found.join(' | ')}`);
  }
});
