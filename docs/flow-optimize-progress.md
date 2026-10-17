# 流程优化进度

仓库 KiriuKazurei/Video-A-Cut。本文只说明进度，不合并任何分支。

## 版本

| 项 | 值 |
| --- | --- |
| 当前分支 | `cursor/f1-agent-visible-8ba2` |
| Pull Request | https://github.com/KiriuKazurei/Video-A-Cut/pull/8 |
| 合并目标 | `agent/flow-optimize`（等和零合并，本轮不合并） |
| F0 已在基线 | `agent/flow-optimize` @ `6f3b879`（Merge PR #7） |
| 本轮开始前 | `eb6452e5f31f11ee0345978e59b62bcbf7f9fcca` |
| 本轮 HEAD | 与本文件在同一提交。提交说明：`style(webui): turn off leftover shadows and record flow progress`。完整四十位 SHA 写在 PR #8 描述首行。提交哈希覆盖文件内容，所以文件里不写入自身哈希；核对命令是 `git rev-parse origin/cursor/f1-agent-visible-8ba2`，结果应与 PR 首行一致。 |

## 已完成

### F0（已合进 agent/flow-optimize）

F0 在 PR #7，合并提交 `6f3b879`。已清：Node brief 哈希空测、reconcile 锁内重判竞态、playwright 安装说明、阶段文档入库。范围和仍存活的债务写在 `docs/flow-optimize-stages.md` 的「F0 范围说明」。

### F1 业务（本分支，尚未合并）

在现有资源页区分「入库」和「Agent 可见」，可批量切换。进入脚本或开始一次新的生成之前，必须由人工确认当前可见集。

- 手动模式和自动模式都走这道门。自动模式的全局授权就是对当前可见集和脚本定稿的人工授权，请求带 `authorization.visible_set_sha`，并与当前指纹一致。
- `VisibleSetSHA` 是已排序的 Agent 可见素材 id 的 sha256。可见集变化后，进行中且 scope 里已有指纹的自动 run 停在决策卡 `visible_set_changed`。
- 脚本过期：可见集变化后 `finalized` → `needs_refinalize`，`draft` → `expired`，行保留。已有成片和交付包失效（`deliveryBlocked`），新的 export 成功后才能再下载。
- 已有素材不改写 `agent_visible`。F1 之前、scope 里没有 `visible_set_sha` 的进行中自动 run 不因此被卡住。
- 文案、路由、信息架构和上述门禁在后续视觉改动里没有改。

### 视觉

- 留白 token：`--vac-page-x` 28px、`--vac-page-y` 20px、`--vac-section-gap` 24px、`--vac-stack-gap` 20px；卡片与表格内边距一并放开。
- 顶栏页签改为分段滑块（Lucian UX/UI Prompt #295）。位移动效只加在 `.app-nav.seg-nav::before`。`prefers-reduced-motion: reduce` 时滑块瞬时到位。液态玻璃已去掉。
- 中性灰硬性规范：`docs/ui-style-spec.md`（`webui/README.md` 有链接）。窗口 `#1E1E1E`、面板 `#252525`、控件 `#2F2F2F`、边框 `#3A3A3A`、主文字 `#E6E6E6`、次文字 `#9A9A9A`、强调色 `#4C8DFF`。后续 UI 不遵守则 Gate 不放行。
- 属性表和同类描述表行高为字号 × 1.25（`--vac-line: 1.25`）。状态监控说明仍是 1.65。
- `npm run lint:style` 已纳入 `npm run test:unit`。

### 本轮对 Gate 条件的修复（相对 `eb6452e`）

条件 1，阴影：

- `siderTheme` 是 `inherit: false`，带不上 `appTheme` 的按钮覆写。侧栏按钮因此仍画出 `box-shadow: 0 2px 0 0`。现已在 `siderTheme` 和 `appTheme` 写上 `Button: { primaryShadow: 'none', defaultShadow: 'none', dangerShadow: 'none' }`。
- Slider 把手的 `0 0 0 2px` 环、Tabs 滚动提示的 inset 阴影、激活页签和 Spin 文案的 `text-shadow`，组件 token 关不掉。`webui/src/styles.css` 把这些运行时值强制为 `none`。滑块把手改用 2px 实色 `outline`（`--vac-accent`），避免把手消失。规范里不给这些阴影开豁免。
- 1440 宽页面实测：资源页上 38 个按钮（含右侧栏主按钮、默认按钮、左侧栏和顶栏）的 `box-shadow` 均为 `none`，悬停和焦点同样是 `none`。TTS 页滑块 `::before` / `::after` 的 `box-shadow` 为 `none`，把手 `outline` 为 2px `#4C8DFF`。编辑导入页激活页签 `text-shadow` 为 `none`，页签滚动提示的 `box-shadow` 为 `none`。Spin 文案的规则同样强制为 `none`。

条件 2，`lint:style`：

- 补上此前漏报：行内 `backdropFilter`、字符串 `borderRadius` / `50%` / 长手圆角、`drop-shadow`、`strokeColor` 百分比键渐变、命名色、`oklch`、`%23` 十六进制。
- 去掉误报：`#face-btn` 不再当成颜色，`argb(` 不再匹配 `rgb(`，一行 CSS 没有分号也不再误伤。圆角豁免改为精确选择器：`.app-nav.seg-nav`（12px）、其菜单项和 `::before`（9px）、`.stage-dot` 与 `.stage-next-dot`（50%）。`.seg-nav-card`、单独的 `.seg-nav`、`.stage-dot-extra` 不再放行。
- 构造样例在 `webui/style_lint_test.mjs`，随 `npm run test:unit` 运行。

建议项：

- `webui/style_smoke_test.mjs`：`prefers-reduced-motion` 关掉页签滑块过渡、描述表 `line-height` 为 1.25、分段页签类名与圆角、侧栏按钮阴影 token。
- 规范写明：Ant Design 语义 Tag / Badge 可以用预设色（含 blue / cyan）；交互和焦点强调色只有 `#4C8DFF`。
- `docs/flow-optimize-stages.md` 的「未来开发计划」写明：这些计划不改变 F1–F4 的验收点，也不改变 C1–C4 的口径。

本轮检查：`tsc --noEmit` 通过；`npm run test:unit` 为 38 + 43，其中含上述 lint 样例与 smoke；`lint:style` 通过（21 个色板色值）。`.run-data` 仍为 33353 字节，未超过 500 MiB。

### 已写入文档、尚未实现的计划

`docs/flow-optimize-stages.md`「未来开发计划」已记录，本阶段没有做：

- 时间轴自绘（轨道头、缩略图条、波形、吸附、多选）
- Agent 幽灵片段（提案式编辑，接受或拒绝后才生效）
- 可停靠、可拖动的面板骨架

这三项不改变 F1–F4 的验收点，也不改变 C1–C4 的口径。

## 待完成

- 等 Gate 对本轮条件修复复审通过后，由和零把 PR #8 合并进 `agent/flow-optimize`。本分支不自行合并，也不改 `main` / `agent/main`。
- F2 及以后按 `docs/flow-optimize-stages.md`：F2 脚本工作台（路径 A 人写、路径 B Agent 出稿）、F3 TTS 与字幕挂接、F4 成片 / 站内预览 / 交付。顺序是 F1 → F2 → F3 → F4。
- Gate 此前的非阻塞项，本轮仍未做：
  - AutoConfirmScene 预算 `ErrForbidden` 与 `errAutoBudgetSpent` 的差别
  - `haltBudgetSpent` 状态检查
  - `ErrArgument` / `ErrInvalidState` → `preview_unavailable`
  - read-model D2 / D4 / D5 / D8 / D13
  - reconcile 之后的 SSE publish 缺测
  - `reconcileHumanFinished` 吞掉 `ErrConflict`
  - N7a / N13 / N14 / R22 / R28 未重跑
  - 真 goroutine 并发 reconcile
  - `visible_set_sha` → 400 的 HTTP API 测试。服务层 `TestAutoRunParksWhenTheVisibleSetChanges` 已断言缺少 `VisibleSetSHA` 得到 `model.ErrArgument`。HTTP 侧 `TestAutoRunRequiresNoticeHash` 只覆盖 `notice_sha256` 缺失或错误返回 400，请求里一直带合法的 `visible_set_sha`，没有单独断言缺失或错误的 `visible_set_sha` 返回 400。
- 时间轴自绘、幽灵片段、停靠面板仍是未来计划，没有开工。
- 历史挂账仍然有效：
  - Premiere A08 仍是 pending。要在 Windows 上把交付包导入 Premiere 后再记结果。它不挡本轮推送，R1 也还不能写成完全通过。步骤在 `docs/真实业务验收执行交接-2026-10-07.md` 第 0.6 节。
  - 真字体和 720p 真渲染要在有真实字体的机器上跑。云电脑没有微软雅黑等字体，F4 不能在这里空过。历史基线里与字体 / 本机路径相关的失败（如 `TestVariantRealWorkerChain` 找不到 `msyh.ttc`、`TestPreviewRealWorkerChain`、`TestPhase7ControlExampleLoads` 的 `E:\VAC\delivery`）本轮没有改，也不要当成已通过。
