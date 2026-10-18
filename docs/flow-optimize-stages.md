# 流程优化阶段目标与验收点

仓库 KiriuKazurei/Video-A-Cut。本文是本流的验收合同。交接步骤见 `docs/flow-optimize-handoff.md`。进度见 `docs/flow-optimize-progress.md`。

## 当前分支与验收范围

- 2026-10-18 用户已授权将各开发功能整合到 `agent/develop`；F0/F1/F2 与本地修复已本地合入，原分支全部保留，main 不变，未推送或发行。
- `agent/flow-optimize@810fa67` 和 `agent/script-workbench` 是本轮来源；当前源码在 `.run-data/worktrees/agent-develop`。
- 本轮仅执行最简编译、相关功能和本机运行检查，不做基线、备份或浏览器冒烟。旧阶段完整验收项是后续验收要求，不能写成本轮已全部执行。
- 人工全流程等待用户确认。F3/F4、真实模型/Premiere 和发行验收未因源码合并自动完成；详见 [整合记录](开发分支合并-2026-10-18.md)。

## 总目标

主路径四段：导入 → 选素材（Agent 可见）→ 脚本 → 成片 / 站内预览 / 进 PR。

脚本段两种写法：人写，或 Agent 先看可见素材再生成。TTS 与字幕可选。

脚本工作台不是蓝图，不是节点图。功能入口沿用现有布局：资源管理、编辑（导入与粗剪 / 内容编辑 / 审查）、导出、TTS、设置。不新增顶层工作区，不新增顶层页签。

此处「进 PR」指把交付包导入 Premiere，不是 GitHub PR，也不外发。

TTS 保留专门分页 `#/tts`（音色、语气、频谱台、时间轴按轴调语气）。脚本段只决定是否启用 TTS、是否启用字幕，以及挂接哪些段落。细调留在 TTS 专页。

## Gate 门禁（四条，贯穿 F2 及以后）

G1. 「Agent 可见素材」与「脚本定稿」两个确认点，自动模式不得静默跳过。自动模式只在已授权范围内走。缺确认即停在决策卡。

G2. 两条脚本路径各自可独立验收：路径 A 人写脚本；路径 B Agent 先看素材再生成。生成稿可编辑、可否决、可重新生成。

G3. TTS 与字幕关闭不挡成片。开启时能从脚本段跳到 TTS 专页并对上时间线。专页改动（音色 / 语气 / 频谱 / 轴）能回写成片。

G4. 交付物与 `delivery.zip` / 720p 对齐，或明确替换并写入文档与 UI 文案。

样式门禁：遵守 `docs/ui-style-spec.md`。`cd webui && npm run lint:style` 必须通过（已纳入 `npm run test:unit`）。不过则 Gate 不放行。

## 口径（已定，F2 起照做）

C1. G1 口径【和零已定 A，2026-10-17】：启动时的全局授权即对「可见集 + 脚本定稿」的人工授权。授权页必须逐项列出范围并留痕。超范围（可见集变化、生成稿被改 / 重生成）时自动模式停决策卡，需重新授权或人工确认。新增确认动作一律进人工白名单。`system:auto-review` 与 `agent:` 不能代点。

C2. 兼容：已有素材默认「Agent 可见」状态等于现状（入库即可见）。进行中的 run 按现状视为已确认，不得被卡死。非自动 run 的 JSON 形状（golden）与 B1–B4 的 API 保持兼容。新增字段只增不改。有改动逐项在交审时列出。

C3. 「过期」：可见集变化后，下游脚本标记过期但不删除。已定稿稿件作废为「待重新定稿」，需人工再确认。已有成片与交付包标记失效，不可继续下载。重新渲染后生成新版本。

C4. 依赖：F1 → F2 → F3 → F4。F3 依赖 F2。F4 依赖 F3。F0 已先交审并合入。

## 阶段

### F0 基线与债务（已完成）

- 目标：在 `agent/flow-optimize` 上清 B3/B4 测试债：Node brief 哈希不匹配测试空转、对账 `auto_version` 竞态等存活变异；记录 playwright 首次安装步骤。
- 验收：对应测试能真实失败（变异后红）；`go build` / `vet` / `gofmt`、`go test -race`、webui check / build / 单测与 smoke 保持现有基线或更好；不新增 FAIL。
- 落点：PR #7，合并提交 `6f3b879`。仍存活的项见文末「Gate 非阻塞挂账」，不挡 F2。

### F1 素材段（已完成）

- 目标：在现有素材 / 资源页内，明确「入库」与「Agent 可见」两层状态，可批量切换。进入脚本前有可见素材确认。
- 验收（G1）：未确认可见素材时，手动与自动模式都不能进入脚本段或开始一次新的生成。改动可见集合后下游脚本标记为过期。有 UI 与 API 测试。
- 落点：PR #8，合并提交 `ec7555d1b4e0e46ff0cffd8e80d83a53aa696532`。视觉硬性规范同时入库。F2 复用这些门，不要重做可见集。

F1 已经落地、F2 必须接着用的行为：

- 指纹：已排序的 Agent 可见素材 id，换行拼接，sha256。实现在 `control-plane/internal/service/visible_set.go` 的 `fingerprintVisibleSet`。
- 确认、进入脚本、定稿、批量改可见性都要求 `human:`。HTTP 层 actor 固定为 `human:webui`。
- 脚本状态只有四个：`draft`、`finalized`、`expired`、`needs_refinalize`。可见集变化时 `finalized` → `needs_refinalize`，`draft` → `expired`，`stale_reason=visible_set_changed`。行保留。
- 自动 run 的 scope 写入 `visible_set_sha`、`visible_asset_ids`、`script_finalize=true`。指纹变化则停在 `visible_set_changed`，人用 `confirm_visible_set` 恢复。scope 里没有指纹的旧 run 不因此被停。
- 交付失效走 `delivery_invalidations`。新的 export 成功后 `ClearDeliveryInvalidation` 才恢复下载。
- 前端门在 `webui/src/visibleSet.ts`：`enterScriptAllowed`、`startGenerationAllowed`。进行中的流程（已有 run）仍可进入内容页，不因这次确认被拦住。

### F2 脚本工作台（已合入 agent/develop，待人工全流程，2026-10-18）

用户追加的花括号语法、高亮、Agent Skill、文字联网查证和可预渲染的 Premiere 画面效果，详见 [脚本工作台说明](script-workbench-syntax.md)。这是对本节范围的明确扩展；本节后续验收条款继续有效，TTS 实际合成仍留 F3。

依赖：F1 已合入。未确认可见集不能进脚本，这道门保持。F2 不重做可见集，不改四个脚本状态的含义。

目标：脚本段用现有编辑页布局。提供路径 A（人写、按段挂素材）与路径 B（Agent 先看可见素材再出稿）。稿件按段保存，可编辑、否决、重生成、定稿。

布局：

- 放在现有「编辑 › 内容编辑」`#/editing/content`。`webui/src/workspaces/EditingWorkspace.tsx` 已在这里用 `scriptOpen` 拦住未确认可见集。工作台接在这个门后面。
- 沿用现有页签和左右栏。不新增顶层入口。不做蓝图画布，不做节点图。
- 现有场景审查、解说批准、导出页先留着。F2 加上按段脚本和定稿门。不要在 F2 删审查页，也不要改写 recognize → export 的阶段名。

路径 A（人写）：

- 人按段写脚本。每段有顺序、正文、所挂素材 id。
- 只能挂当前已确认可见集里的素材。不可见素材不能被选中，也不能被保存进去。
- 保存后仍是 `draft`，直到人定稿。

路径 B（Agent 先看再生成）：

- Agent 只能读当前已确认的可见集，不能读不可见素材。
- 先看素材再出稿。出稿写入 `draft`，不是定稿。
- 人可以改、否决、要求重新生成。
- 定稿必须是人工动作。`POST /api/scripts/{id}/finalize` 已拒绝非 `human:`。F2 新增的保存、否决、重生成、重新定稿同样拒绝 `system:auto-review` 与 `agent:`（`ErrForbidden`）。
- 变异测试：去掉人工 actor 检查后，对应测试变红。路径 B 单独有这条测试。

定稿与自动模式（G1、G2）：

- 生成稿必须经人确认后才是 `finalized`。自动模式遇到未定稿停在决策卡，不能把 `draft` 当成已定稿往下跑。
- 全局授权里的 `script_finalize` 只覆盖授权当时的可见集和当时的定稿。它不是「以后任何稿都算人点过」。
- 超范围停决策卡，沿用可见集那套留痕（`auto_decisions` + 人工决定）：
  - 可见集变化：已实现，`halt_reason=visible_set_changed`，恢复动作 `confirm_visible_set`。F2 不要换一套。
  - 生成稿被改或重新生成：停决策卡。`halt_reason` 用 `script_changed`。恢复动作用 `confirm_script`，只接受 `human:`。这是 C1 已定的「生成稿被改」，不是新的授权产品。已有同含义常量时用已有常量，并在交审说明里写明。
- 已定稿稿件的正文或分段被改、或路径 B 重新生成：状态回到 `needs_refinalize`（行保留），`stale_reason=script_changed`。不要新增第五个状态。人重新定稿后，自动 run 才能离开这张决策卡。
- F2 使用带 expected_version 的 `/workbench/finalize`，允许人工把 `needs_refinalize` 重新定稿。旧 F1 定稿路由不能用于绕过工作台校验。Agent 不能代点。

版本：

- 定稿前后都要能读回来。F1 的 `scripts` 表仍是当前行；F2 通过 0019 迁移增加工作台、修订历史和交付表。
- 每次保存和每次定稿留一份修订：稿 id、可见集指纹、状态、正文或分段、操作者、时间。当前行仍表示当前稿。覆盖写时不要只留最后一版。
- 新字段、新表只增不改。`workflow_runs` 的 JSON 保持不变。交审时逐项列出新增 API。

TTS 边界（F2 只记账，不做细调）：

- 稿或段上保存「启用 TTS」「启用字幕」，以及挂接哪些段落。
- F2 不要求跳到 `#/tts` 或实际 TTS 合成；用户 10-18 已追加特殊语法的实际画面/Premiere 交付，因此本轮需要验证预渲染成片效果。
- 音色、语气、频谱、按轴调语气不进脚本工作台。那些是 F3，页面仍是 TTS 专页。

验收清单（交 Gate 时逐条给证据）：

1. 路径 A 端到端：确认可见集 → 人写段并挂可见素材 → 保存为 `draft` → 人定稿 → 可读到定稿修订。
2. 路径 B 端到端：Agent 只看见可见素材 → 出稿为 `draft` → 人改 / 否决 / 重生成 → 人定稿。未定稿时自动模式停在决策卡。
3. 路径 B 定稿的变异测试：去掉 `human:` 检查后变红。`system:auto-review` 与 `agent:` 定稿、重生成确认均为 `ErrForbidden`。
4. 挂上不可见素材的保存被拒绝。
5. 改可见集后，旧规则仍在：`finalized` → `needs_refinalize`，`draft` → `expired`，行还在，下载仍被挡住。
6. 改已定稿正文或重生成后：`needs_refinalize`，自动 run 停在 `script_changed`，人重新定稿后才能继续。`confirm_script` 拒绝非 `human:`。
7. 定稿前、定稿后各能读回一版。
8. 非自动 run 的既有 JSON 形状不变。新增字段在交审说明中逐项列出。
9. `cd webui && npm run lint:style` 通过。新界面遵守 `docs/ui-style-spec.md`。

### F3 TTS 与字幕挂接

依赖：F2 的定稿稿，且段上已有「启用 TTS / 启用字幕」和挂接段落。F2 没合入前不开工。

目标：脚本段内只有「启用 TTS」「启用字幕」与段落挂接。开启 TTS 时有跳转入口到 TTS 专页 `#/tts`，并定位到对应段落和时间位置。

专页边界：音色、语气、频谱台、时间轴按轴调语气只在 TTS 专页做。不把这些控件搬进脚本工作台，也不新开顶层入口。

验收（G3）：

1. 两个开关全关时能出成片，时间线正确，不因为没开 TTS 或字幕而失败。
2. 开 TTS 后，从脚本段跳到 `#/tts`，落到对应段落和时间位置。
3. 在专页改音色 / 语气 / 频谱 / 轴，回到成片能看到这次更新。回写有测试。
4. 文档写明回写落到哪个修订。回写后旧预览与交付包失效，不能继续下载旧文件。失效沿用 F1 的 `deliveryBlocked`：新的 export 成功后才能再下载。
5. 字幕开关独立于 TTS。只开字幕、不开 TTS 时，行为在交审说明里写清，并有测试。
6. `lint:style` 通过。

### F4 成片、站内预览与交付

依赖：F3。F3 没合入前不开工。

目标：成片段做站内预览，一键进入 Premiere 交付，并说明交付物清单。

验收（G4）：

1. 站内预览与交付清单一致。交付物与 `delivery.zip` 和 720p 预览一致，或明确替换，并同时改文档和 UI 文案。
2. 「进 PR」是进 Premiere 的交付，不是 GitHub PR，也不外发。
3. 真渲染与字体烟测在有真实字体的机器上跑。云电脑没有微软雅黑等字体，不在这里空过。文档写明机器与执行人。
4. Premiere A08 仍待 Windows 实机导入。它不挡 F2、F3。F4 在没有这次导入记录时，不能把 R1 或 A08 写成完全通过。
5. `lint:style` 通过。

## 不做

不新增顶层入口。不把 TTS 细调并入脚本工作台。不改 `main`。不把时间轴自绘、幽灵片段、停靠面板当成 F2–F4 的验收条件。Premiere A08 仍待 Windows，不挡 F2、F3 和本交接。

## Gate 非阻塞挂账

下列项可选清。不挡 F2 开工，也不挡 F2–F4 的功能验收。清的时候单独交 Gate，不要夹进 F2 的功能差异里。

F0 仍存活的变异与缺测：

- `AutoConfirmScene` 预算 `ErrForbidden` 与 `errAutoBudgetSpent` 的差别
- `haltBudgetSpent` 状态检查
- `ErrArgument` / `ErrInvalidState` → `preview_unavailable`
- read-model D2 / D4 / D5 / D8 / D13
- `reconcileHumanFinished` 吞掉 `ErrConflict`
- N7a / N13 / N14 / R22 / R28 未重跑

另外四项：

- 真 goroutine 并发 reconcile（现有竞态覆盖不是真并发）
- reconcile 之后的 SSE publish 缺测
- `visible_set_sha` → 400 的 HTTP API 测。服务层 `TestAutoRunParksWhenTheVisibleSetChanges` 已断言缺少 `VisibleSetSHA` 得到 `model.ErrArgument`。HTTP 侧 `TestAutoRunRequiresNoticeHash` 只覆盖 `notice_sha256`。请求里一直带合法的 `visible_set_sha`，没有单独断言缺失或错误的 `visible_set_sha` 返回 400
- Badge 描边：antd Badge 计数或状态点上若仍有描边、外圈或阴影，可以清。语义 Tag / Badge 仍可用预设色。交互和焦点强调色只有 `#4C8DFF`。改动仍须过 `lint:style`

## 提交约定

本轮已按用户指示将开发功能合入 agent/develop，保留原分支。今后新增实施单独确定范围，并附目标、对应验收、实际检查和已知风险；不据本轮授权自动推送、发行或进入人工测试。

## 未来开发计划

以下还没开工。它们不挡 F2、F3、F4 的验收，也不改变 C1–C4。F2–F4 不要为了这些去改布局。

### 时间轴自绘

轨道头、缩略图条、波形、吸附、多选。

### Agent 幽灵片段

提案式编辑：接受或拒绝之后才生效。日志点击后跳到对应时间点。每一步都可以撤销。用户手动改过的片段，Agent 不得覆盖。

### 可停靠可拖动面板骨架

面板可以停靠，也可以拖动重排。这是以后的骨架，不在 F2–F4 落地。

## 样式

F2 及以后的界面改动遵守 `docs/ui-style-spec.md`。

合入前：

```bash
cd webui && npm run lint:style
```

`npm run test:unit` 会连带跑它。规范没写的颜色、阴影、渐变、模糊、自绘控件，Gate 不放行。
