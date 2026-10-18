# 流程优化进度

仓库 KiriuKazurei/Video-A-Cut。本文只记进度。不合并任何分支。

## 版本

| 项 | 值 |
| --- | --- |
| 当前基线 | `agent/flow-optimize` |
| 基线 tip | `ec7555d1b4e0e46ff0cffd8e80d83a53aa696532` |
| 该提交 | PR #8 的合并提交：`Merge pull request #8 from KiriuKazurei/cursor/f1-agent-visible-8ba2` |
| PR #8 | https://github.com/KiriuKazurei/Video-A-Cut/pull/8 已合进 `agent/flow-optimize`（2026-10-17） |
| F0 | PR #7，合并提交 `6f3b879` |
| 核对 | `git rev-parse agent/flow-optimize` 应等于上表 tip。本文随后的文档提交不改变这条基线的含义 |

## 交接

开发交接中，F2 起由其它 Agent 接手。

开工读 `docs/flow-optimize-handoff.md`，验收口径以 `docs/flow-optimize-stages.md` 为准。界面遵守 `docs/ui-style-spec.md`。

## 已完成

### F0（已合进 agent/flow-optimize）

F0 在 PR #7，合并提交 `6f3b879`。已清：Node brief 哈希空测、reconcile 锁内重判竞态、playwright 安装说明、阶段文档入库。范围和仍存活的债务写在 `docs/flow-optimize-stages.md` 的「Gate 非阻塞挂账」。

### F1（已合进 agent/flow-optimize，PR #8）

在现有资源页区分「入库」和「Agent 可见」，可批量切换。进入脚本或开始一次新的生成之前，必须由人工确认当前可见集。

- 手动模式和自动模式都走这道门。自动模式的全局授权就是对当前可见集和脚本定稿的人工授权。请求带 `authorization.visible_set_sha`，并与当前指纹一致。授权页逐项列出范围（`grants`：`visible_set`、`script_finalize`，以及每条可见素材）。
- `VisibleSetSHA` 是已排序的 Agent 可见素材 id 的 sha256（id 用换行拼接后取哈希）。可见集变化后，进行中且 scope 里已有指纹的自动 run 停在决策卡 `visible_set_changed`。恢复动作是 `confirm_visible_set`，只接受 `human:`。`system:auto-review` 与 `agent:` 不能代点。
- 脚本过期：可见集变化后 `finalized` → `needs_refinalize`，`draft` → `expired`，行保留，`stale_reason` 为 `visible_set_changed`。已有成片和交付包失效（`deliveryBlocked`），新的 export 成功后才能再下载。
- 已有素材不改写 `agent_visible`。F1 之前、scope 里没有 `visible_set_sha` 的进行中自动 run 不因此被卡住。
- 文案、路由、信息架构在 F1 视觉改动里没有改。

已有接口（F2 在上面加，不要另做一套可见集）：

- `GET /api/visible-set`
- `POST /api/visible-set/confirm`
- `POST /api/assets/visibility`
- `POST /api/assets/{id}/script-entry`
- `GET /api/assets/{id}/scripts`
- `POST /api/scripts/{id}/finalize`

### 视觉规范（已合进，随 PR #8）

硬性规范是 `docs/ui-style-spec.md`（`webui/README.md` 有链接）。窗口 `#1E1E1E`、面板 `#252525`、控件 `#2F2F2F`、边框 `#3A3A3A`、主文字 `#E6E6E6`、次文字 `#9A9A9A`、强调色 `#4C8DFF`。

- 留白 token：`--vac-page-x` 28px、`--vac-page-y` 20px、`--vac-section-gap` 24px、`--vac-stack-gap` 20px。
- 顶栏页签是分段滑块。位移动效只加在 `.app-nav.seg-nav::before`。`prefers-reduced-motion: reduce` 时滑块瞬时到位。液态玻璃已去掉。
- 属性表和同类描述表行高为字号 × 1.25（`--vac-line: 1.25`）。状态监控说明仍是 1.65。
- `npm run lint:style` 已纳入 `npm run test:unit`。后续 UI 不遵守则 Gate 不放行。

PR #8 合入前的 Gate 复审（相对 `eb6452e`，提交 `3026f1d`）已处理阴影和 lint 漏报。细节留在 PR #8 描述里。本进度不再把它当成未合并工作。

## 待完成

- 开发交接中，F2 起由其它 Agent 接手。顺序是 F2 → F3 → F4。目标、验收、依赖在 `docs/flow-optimize-stages.md`。
- 功能分支从 `agent/flow-optimize` 的当前 tip 开。合进 `agent/flow-optimize` 之前交 Gate。合入由和零批准。接手 Agent 不自行合并，不改 `main`。
- 时间轴自绘、幽灵片段、停靠面板仍是未来计划，没有开工，不挡 F2–F4 验收。
- Gate 非阻塞挂账，可选清，不挡 F2 开工。清单在 `docs/flow-optimize-stages.md`「Gate 非阻塞挂账」：F0 存活变异、真并发 reconcile、SSE publish、`visible_set_sha` → 400 的 HTTP API 测、Badge 描边。
- 历史挂账仍然有效：
  - Premiere A08 仍是 pending。要在 Windows 上把交付包导入 Premiere 后再记结果。它不挡 F2、F3 开工，也不挡本交接文档。R1 还不能写成完全通过。步骤在 `docs/真实业务验收执行交接-2026-10-07.md` 第 0.6 节。
  - 真字体和 720p 真渲染要在有真实字体的机器上跑。云电脑没有微软雅黑等字体，F4 不能在这里空过。历史基线里与字体 / 本机路径相关的失败（如 `TestVariantRealWorkerChain` 找不到 `msyh.ttc`、`TestPreviewRealWorkerChain`、`TestPhase7ControlExampleLoads` 的 `E:\VAC\delivery`）不要当成已通过。
