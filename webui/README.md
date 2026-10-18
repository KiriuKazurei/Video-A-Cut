# WebUI

核对日期：2026-10-18（agent/develop）。React/TypeScript、Vite、Ant Design、TanStack Query；业务仍由 Go 的 REST/SSE 处理，桌面能力通过 Tauri 桥接。完整接口见 [接口清单](../docs/前端接口清单与重构交接.md) 与 [JSON](../docs/frontend-api-inventory.json)。

2026-10-18：`#/editing/content` 新增脚本工作台；普通文字用于请求实际文稿，花括号用于强制视频、字幕和画面效果。语法、接口、修订与交付限制见 [脚本工作台说明](../docs/script-workbench-syntax.md)，Agent 使用 [项目 Skill](../skills/vac-script-syntax/SKILL.md)。当前分支实施结果及待验边界见 [流程优化进度](../docs/flow-optimize-progress.md)。

接手当前完整界面时，请先阅读 [前端接口清单与重构交接](../docs/前端接口清单与重构交接.md) 和 [机器可读接口清单](../docs/frontend-api-inventory.json)。文档覆盖录屏导入、服务商、预设、内容审查、交付、任务、审计与 SSE，并区分流程级取消/重试与尚未提供的单任务控制接口。

## 页面与状态

| 地址 | 内容 |
|---|---|
| #/resources | 资产库、预览、治理 |
| #/editing/import | 受控导入、粗剪选段、开始生成；默认分步，显式切换自动模式 |
| #/editing/content | 场景/解说改稿，保存新修订 |
| #/editing/review | 场景确认、解说批准、流程控制 |
| #/export/delivery | 原始交付、MP4、ASS 样式与派生 |
| #/export/acceptance | 人工验收记录 |
| #/export/tasks | 当前流程的阶段与任务；不是全局任务队列接口 |
| #/tts | 独立试听、模板和声音调节 |
| #/settings/{profiles,providers,preparation,logs,status} | 预设、服务商、预检、日志、状态 |

页面常驻并以 hidden 切换，组件仍须防止隐藏页响应快捷键。查询/预览归属绑定 asset、run、revision；版本切换使旧预览失效。SSE 打开和重连后重新获取 REST 快照，不依赖历史事件重放。

自动模式每个新会话默认分步，不读取旧的模式 localStorage。正在运行的自动任务仍显示进度；全局授权、放宽阈值确认、撤销授权和外发版本许可分别处理。决策卡可导出、提修改或经二次确认放弃；AI 自评不等于人工验收。

C/A 仅在审查卡片或时间线具有焦点时确认/批准；N 跳到下一待处理项。输入框、按钮、链接和模态框保护原有键盘行为；已完成卡片不回退操作其他项。新手引导现有 7 步，显示版本与 localStorage 标记由 `src/tour.ts` 管理。

通用任务只有创建和单任务读取；不存在通用 GET /api/tasks 或单任务取消/重试。工作流、导入、预览、TTS 和派生任务有各自专用控制接口。不要把两者混淆。

## 开发与检查

先准备 Node >=22.12 和已锁定 npm 依赖，再启动 Go。开发服务器 `npm run dev` 在本机监听，把 /api 代理到 Go；`VAC_API_TARGET` 可选择独立验收端口。生产构建由 Go 的 web_root 同源托管，桌面则从 Tauri 资源加载。`npm run check` 是类型检查，`npm run build` 是构建。

Windows 测试统一经 [管理入口](../scripts/TESTING.md)，在本轮分离工作区使用 custom + WorkingDirectory 指向这里；不要误跑原根目录的旧源码。`test:unit` 覆盖契约、桌面 URL、TTS、阶段推导、证据摘要、引导和自动模式。`test:browser-tts` 仍引用仓库外 /workspace/vac-demo，当前 Windows 工作区不能将它当作可用入口。

本轮合并后仅执行新增模块 22 项、queryRefresh 8 项、类型/样式/构建，均通过，未执行浏览器冒烟。以下浏览器结果属于此前版本。`auto_ui_smoke.mjs` 自带 mock 服务；`run-local-smokes.mjs` 为 quickstart/review 提供本地夹具。两者需明确 VAC_DIST、PLAYWRIGHT_CORE、CHROME 和受管理输出目录。本轮浏览器共 20+10+9 组通过，使用既有 Playwright 1.62.1 + Edge；锁定版 1.64.0 安装验收未执行。真实 Go 浏览器联调、可见桌面和人工内容质量分别记录。

视觉：硬性规范见 [`docs/ui-style-spec.md`](../docs/ui-style-spec.md)。全部操作控件使用 Ant Design 5（`antd` + `@ant-design/icons`，`ConfigProvider` 设 `zhCN` 与 `theme.darkAlgorithm`）。配色集中在 `src/theme.ts` 与 `styles.css` 的 `--vac-*`：窗口 `#1E1E1E`、面板 `#252525`、控件 `#2F2F2F`、边框 `#3A3A3A`、主文字 `#E6E6E6`、次文字 `#9A9A9A`、强调色 `#4C8DFF`。顶栏工作区页签是实色分段滑块（圆角槽 + 一块滑块，选中项加粗；位移动效只加在这块滑块上，对齐 Lucian #295）。监视器是普通实色外框，没有液态玻璃、背景模糊或高光。左右侧栏是 antd `Layout.Sider`：纯色底、直角、antd 分隔线；侧栏内容包在独立的 `ConfigProvider`（`siderTheme`，`inherit: false`）里。系统开启「减少动态效果」时滑块瞬时到位。描述表行高为字号的 1.25 倍。`npm run lint:style` 检查渐变、阴影、模糊和色板以外的颜色。

## 审查补充（2026-10-16）

2026-10-17：CR02 已修复。授权绑定素材、预设 ID、版本和 notice 摘要，变化时同步清除；刷新包含 auto-notice，发送前再次核对授权。67 项前端单元、类型/构建及自动模式浏览器 20 组通过，包含旧身份不能恢复授权、请求在途变更和显式重勾后启动。CR03 的时间线键盘审批对象仍待修，详见 [代码审查与复验](../docs/代码审查-2026-10-16.md)。
