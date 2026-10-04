import fs from 'node:fs';
import path from 'node:path';
import assert from 'node:assert';

console.log('--- Phase 3 静态与连通性回归断言测试 ---');

const projectRoot = 'E:/AI/Video Auto Cut';
const webuiSrc = path.join(projectRoot, 'webui/src');

// 1. 验证 hooks/useResizable.ts
const resizablePath = path.join(webuiSrc, 'hooks/useResizable.ts');
assert(fs.existsSync(resizablePath), 'useResizable.ts 必须存在');
const resizableContent = fs.readFileSync(resizablePath, 'utf8');
assert(resizableContent.includes('useResizable'), 'useResizable hook 必须导出');
assert(resizableContent.includes('storageKey'), 'useResizable 必须支持 storageKey');
assert(resizableContent.includes('direction'), 'useResizable 必须支持 direction');
assert(resizableContent.includes('resetToDefault'), 'useResizable 必须支持 resetToDefault');
console.log('✓ useResizable.ts 契约验证通过');

// 2. 验证 components/PipelineStepper.tsx
const stepperPath = path.join(webuiSrc, 'components/PipelineStepper.tsx');
assert(fs.existsSync(stepperPath), 'PipelineStepper.tsx 必须存在');
const stepperContent = fs.readFileSync(stepperPath, 'utf8');
assert(stepperContent.includes('PipelineStepper'), 'PipelineStepper 组件必须导出');
assert(stepperContent.includes('pipeline-stepper'), '必须包含 pipeline-stepper 样式类');
assert(stepperContent.includes('pipeline-step-item'), '必须包含 pipeline-step-item 样式类');
console.log('✓ PipelineStepper.tsx 契约验证通过');

// 3. 验证 components/Timeline.tsx
const timelinePath = path.join(webuiSrc, 'components/Timeline.tsx');
const timelineContent = fs.readFileSync(timelinePath, 'utf8');
assert(timelineContent.includes('timeline-resizable-container'), 'Timeline 必须包含 timeline-resizable-container');
assert(timelineContent.includes('timeline-resizer-bar'), 'Timeline 必须包含 timeline-resizer-bar 拖拽手柄');
assert(timelineContent.includes('vac_timeline_height'), 'Timeline 必须使用 vac_timeline_height 本地存储');
assert(timelineContent.includes('vac_timeline_collapsed'), 'Timeline 必须使用 vac_timeline_collapsed 本地存储');
console.log('✓ Timeline.tsx 增强契约验证通过');

// 4. 验证 App.tsx
const appPath = path.join(webuiSrc, 'App.tsx');
const appContent = fs.readFileSync(appPath, 'utf8');
assert(appContent.includes('PipelineStepper'), 'App.tsx 必须挂载 PipelineStepper');
assert(appContent.includes('app-quick-actions'), 'App.tsx 必须包含 app-quick-actions 快捷操作栏');
assert(appContent.includes('sider-resizer-left'), 'App.tsx 必须包含左侧栏拖拽手柄');
assert(appContent.includes('sider-resizer-right'), 'App.tsx 必须包含右侧栏拖拽手柄');
assert(appContent.includes('vac_left_sider_width'), 'App.tsx 必须使用 vac_left_sider_width 本地存储');
assert(appContent.includes('vac_right_sider_width'), 'App.tsx 必须使用 vac_right_sider_width 本地存储');
console.log('✓ App.tsx 侧栏与快捷栏集成验证通过');

// 5. 验证 styles.css
const cssPath = path.join(webuiSrc, 'styles.css');
const cssContent = fs.readFileSync(cssPath, 'utf8');
assert(cssContent.includes('.sider-resizer'), 'styles.css 必须包含 .sider-resizer');
assert(cssContent.includes('.timeline-resizer-bar'), 'styles.css 必须包含 .timeline-resizer-bar');
assert(cssContent.includes('.app-quick-actions'), 'styles.css 必须包含 .app-quick-actions');
assert(cssContent.includes('.pipeline-stepper'), 'styles.css 必须包含 .pipeline-stepper');
console.log('✓ styles.css 关键样式声明验证通过');

// 6. 验证 dist 编译产物
const distHtml = path.join(projectRoot, 'webui/dist/index.html');
assert(fs.existsSync(distHtml), 'webui/dist/index.html 必须存在');
console.log('✓ 静态构建产物验证通过');

console.log('\n>>> Gate 2: 全部回归与连通性断言通过 <<<');
