import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert';

console.log('--- Phase 4 键盘快捷键系统与交互增强断言测试 ---');

const projectRoot = 'E:/AI/Video Auto Cut';
const webuiSrc = path.join(projectRoot, 'webui/src');

// 1. 验证 hooks/useKeyboardShortcuts.ts
const shortcutsHookPath = path.join(webuiSrc, 'hooks/useKeyboardShortcuts.ts');
assert(fs.existsSync(shortcutsHookPath), 'useKeyboardShortcuts.ts 必须存在');
const shortcutsContent = fs.readFileSync(shortcutsHookPath, 'utf8');
assert(shortcutsContent.includes('export function useKeyboardShortcuts'), '必须导出 useKeyboardShortcuts hook');
assert(shortcutsContent.includes('export function isTypingContext'), '必须导出 isTypingContext 防护函数');
assert(shortcutsContent.includes('export function toggleMediaPlayback'), '必须导出 toggleMediaPlayback 媒体控制函数');
assert(shortcutsContent.includes('export function seekMedia'), '必须导出 seekMedia 步进函数');
assert(shortcutsContent.includes("e.key === '1'"), '必须支持 1 键工作区导航');
assert(shortcutsContent.includes("e.key === '2'"), '必须支持 2 键工作区导航');
assert(shortcutsContent.includes("e.key === '3'"), '必须支持 3 键工作区导航');
assert(shortcutsContent.includes("e.key === '4'"), '必须支持 4 键工作区导航');
assert(shortcutsContent.includes("e.key === '['"), '必须支持 [ 键二级分区切换');
assert(shortcutsContent.includes("e.key === ']'"), '必须支持 ] 键二级分区切换');
assert(shortcutsContent.includes("e.code === 'Space'"), '必须支持 Space 播放/暂停');
assert(shortcutsContent.includes("e.key === '?'"), '必须支持 ? 键调出帮助');
console.log('✓ useKeyboardShortcuts.ts 契约与键位绑定验证通过');

// 2. 验证 components/KeyboardShortcutsModal.tsx
const modalPath = path.join(webuiSrc, 'components/KeyboardShortcutsModal.tsx');
assert(fs.existsSync(modalPath), 'KeyboardShortcutsModal.tsx 必须存在');
const modalContent = fs.readFileSync(modalPath, 'utf8');
assert(modalContent.includes('export function KeyboardShortcutsModal'), '必须导出 KeyboardShortcutsModal 组件');
assert(modalContent.includes('键盘快捷键速查'), '必须包含速查标题');
assert(modalContent.includes('SHORTCUTS'), '必须定义快捷键清单');
console.log('✓ KeyboardShortcutsModal.tsx 模态框验证通过');

// 3. 验证 App.tsx 集成
const appPath = path.join(webuiSrc, 'App.tsx');
const appContent = fs.readFileSync(appPath, 'utf8');
assert(appContent.includes('useKeyboardShortcuts'), 'App.tsx 必须调用 useKeyboardShortcuts');
assert(appContent.includes('KeyboardShortcutsModal'), 'App.tsx 必须挂载 KeyboardShortcutsModal');
assert(appContent.includes('isShortcutsHelpOpen'), 'App.tsx 必须管理快捷键帮助状态');
assert(appContent.includes('QuestionCircleOutlined'), 'App.tsx 必须包含问号帮助按钮图标');
console.log('✓ App.tsx 快捷键 hook 与帮助弹窗挂载验证通过');

// 4. 验证构建产物
const distHtml = path.join(projectRoot, 'webui/dist/index.html');
assert(fs.existsSync(distHtml), 'webui/dist/index.html 必须存在');
console.log('✓ 静态构建产物验证通过');

console.log('\n>>> Gate 2: Phase 4 键盘系统全部回归与连通性断言通过 <<<');
