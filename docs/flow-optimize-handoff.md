# 流程优化交接（F2 起）

仓库 KiriuKazurei/Video-A-Cut。2026-10-18：用户已授权本轮整合；F0/F1、F2 首版和本地修复已合入 agent/develop（功能提交 d67e71d），原分支保留。最简验证通过，人工全流程等待用户确认。原 810fa67 是本轮输入提交，已不是当前工作区指针。

验收合同是 `docs/flow-optimize-stages.md`。进度是 `docs/flow-optimize-progress.md`。界面硬性规范是 `docs/ui-style-spec.md`。本文只写开工顺序和已有落点，避免接手时重做 F0/F1。

## 当前交接

1. 当前源码为 `agent/develop`，目录 `E:\AI\Video Auto Cut\.run-data\worktrees\agent-develop`；不得在旧根目录或旧功能工作区中继续改本轮产品代码。
2. 本轮分支合并已获用户授权并完成，不删除原分支、不推送或发行。提交、比较与实际验证见 [合并记录](开发分支合并-2026-10-18.md)。
3. 先等待用户确认，再进入人工全流程；F3/F4 的实施未因本次合并自动启动。
4. F2 当前语法、接口与交付边界见 [工作台说明](script-workbench-syntax.md)。配音合成与原自动执行链完整挂接留 F3，真实模型/Premiere/桌面包仍待验。

## 主路径（不要改这条产品口径）

导入 → 选素材（Agent 可见）→ 脚本（人写，或 Agent 先看再生成；TTS / 字幕可选）→ 成片 / 站内预览 / 进 Premiere。

脚本工作台放在现有「编辑 › 内容编辑」`#/editing/content`。不是蓝图。不新增顶层页签。TTS 细调仍在 `#/tts`。

## 已完成，不要重做

### F0

PR #7，合并提交 `6f3b879`。Node brief 哈希空测、reconcile 锁内重判、playwright 安装说明、阶段文档入库。剩下的变异见阶段文档「Gate 非阻塞挂账」。

### F1

PR #8，已合进基线。入库与 Agent 可见分层、可见集确认、`VisibleSetSHA`、脚本过期、交付失效都已在库里。

| 行为 | 落点 |
| --- | --- |
| 指纹与确认门 | `control-plane/internal/service/visible_set.go` |
| 状态常量 | `control-plane/internal/model/visible.go` |
| 表 | `control-plane/internal/store/migrations/0018_visible_set.sql` |
| HTTP | `control-plane/internal/api/api.go` 中 `visible-set`、`script-entry`、`scripts`、`finalize` |
| 自动授权与停卡 | `control-plane/internal/service/auto.go`、`auto_driver.go` 的 `parkVisibleSet` |
| 前端门 | `webui/src/visibleSet.ts`、`webui/src/workspaces/EditingWorkspace.tsx` |
| 资源页确认 | `webui/src/components/AssetManager.tsx` |
| 授权页逐项列表 | `webui/src/components/AutoRunPanel.tsx` |

接口保持这些，F2 在上面加：

- `GET /api/visible-set`
- `POST /api/visible-set/confirm`
- `POST /api/assets/visibility`
- `POST /api/assets/{id}/script-entry`
- `GET /api/assets/{id}/scripts`
- `POST /api/scripts/{id}/finalize`

测试从这些文件延伸，不要另起一套可见集断言：

- `control-plane/internal/service/visible_set_test.go`
- `control-plane/internal/api/visible_set_test.go`
- `webui/visible_set_test.mjs`

### 视觉

`docs/ui-style-spec.md` 已生效。`cd webui && npm run lint:style` 是门禁，并已纳入 `npm run test:unit`。

## F2 开工时守住的口径

下面每条都在阶段文档里有验收条目。这里只列接手时最容易做反的点。

- 全局授权 = 人对「当前可见集 + 脚本定稿」的授权。授权页继续逐项列出范围并留痕。`script_finalize` 不覆盖之后被改过的稿。
- 可见集变化继续停在 `visible_set_changed`，人用 `confirm_visible_set` 恢复。不要换原因码。
- 生成稿被改或重生成：停决策卡，`halt_reason=script_changed`，恢复动作 `confirm_script`，只接受 `human:`。已定稿稿回到 `needs_refinalize`，`stale_reason=script_changed`。不要新增第五个脚本状态。
- 新的确认动作进人工白名单。`system:auto-review` 与任何 `agent:` 不能代点。路径 B 的定稿要有「去掉人工检查就变红」的测试。
- 路径 B 只能读已确认可见集。路径 A 只能挂这些素材。
- 定稿前后版本都要读得回。当前 `scripts` 行仍是当前稿。修订另存。
- `workflow_runs` 的 JSON 不改。新字段只增。交审时逐项列出。
- 脚本段保存 TTS/自动字幕意图与分段挂接；音色、语气、频谱、按轴调语气留给 F3。按用户 10-18 追加范围，F2 同时交付特殊语法对应的预渲染画面与字幕，实际配音合成仍不在本轮。
- 时间轴自绘、幽灵片段、停靠面板不挡 F2 验收。这三项不要夹进 F2。

## 非阻塞挂账（可选，单独做）

不挡 F2。清单与做法在 `docs/flow-optimize-stages.md`「Gate 非阻塞挂账」：

- F0 存活变异（预算错误差别、`haltBudgetSpent`、`preview_unavailable`、read-model D2/D4/D5/D8/D13、`reconcileHumanFinished` 吞 `ErrConflict`、N7a/N13/N14/R22/R28）
- 真并发 reconcile
- reconcile 后 SSE publish 缺测
- 缺失或错误的 `visible_set_sha` 在 HTTP 上返回 400（服务层已有 `ErrArgument`）
- Badge 描边（语义色可留，强调色仍是 `#4C8DFF`，改完过 `lint:style`）

## 测试与产物

本轮按用户要求仅做相关模块最简测试、类型/样式/构建与本机运行；均已通过，未跑基线或浏览器冒烟。今后新增实施仍须按实际改动选择检查。

测试产物总占用上限 500 MiB，统计口径在仓库 `AGENTS.md`。不要删测试源码、固定样本、原始录制和业务数据。

playwright 首次使用：在 `webui/` 执行 `npx playwright install chromium`。说明已在 F0 记下。

## 历史不挡 F2

- Premiere A08 待 Windows 实机。步骤在 `docs/真实业务验收执行交接-2026-10-07.md` 第 0.6 节。
- 真字体与 720p 真渲染不在没有微软雅黑的云电脑上空过。那是 F4 的机器条件，不是 F2 的门。
