# 流程优化进度

仓库 KiriuKazurei/Video-A-Cut。本文记录实际进度；2026-10-18 本轮分支合并已由用户授权，全部原分支保留。

## 版本

| 项 | 值 |
| --- | --- |
| 当前整合分支 | `agent/develop` |
| 远程功能输入 | `agent/flow-optimize@810fa67199d2c2876da721c62af92d48d148a327` |
| F1 代码点 | `ec7555d1b4e0e46ff0cffd8e80d83a53aa696532`，PR #8；810fa67 另含交接文档合并 |
| F2 整合 | `agent/develop@d67e71d`，已本地提交；原 agent/script-workbench 保留 |
| PR #8 | https://github.com/KiriuKazurei/Video-A-Cut/pull/8 已合进 `agent/flow-optimize`（2026-10-17） |
| F0 | PR #7，合并提交 `6f3b879` |
| 远程核对 | 2026-10-18 已逐个 fetch 11 个分支；本地未推送 |

## 当前交接

2026-10-18：F0/F1、F2 和本地修复已整合，最简验证通过：Go API 3/service 17/语法 3、前端 22+8、Python 8、Node 渲染 1 及类型/样式/前后端构建和本机进程运行。跨语言使用本机模型桩；不代替质量。人工全流程等待用户确认。详见 [整合记录](开发分支合并-2026-10-18.md)。

## 本次合并前的 F2 实施记录（历史）

2026-10-18：F2 首版实现与功能验证完成，等待用户 Gate 复审；新增语法和使用方式见 [脚本工作台说明](script-workbench-syntax.md)。Go 当前权威、人工定稿、四态脚本和非自动流程 JSON 边界保持。

- 已实现：指令/实际旁白分离、可见视频绑定、强制字幕与字号/系统字体、高亮、黑白淡变和交叉溶解；保存/修订/否决/生成/定稿；script_changed 暂停和带精确版本的人工 confirm_script；本机画面/Premiere 包。
- 功能验证：Go 工作台定向 8 项（包含旧/新 scope 恢复、精确审计、真实 Go/Python/本机模型桩/Node/FFmpeg 往返）、人工定稿门变异、Go vet 通过；scriptsyntax 3、model 2、store 44、API 98 项通过。Python 6+5、Node ASS/字体/渲染 21、前端 84、类型/样式/生产构建、浏览器 7 组及 Skill 官方校验通过。
- 真实画面：720p、3.666667 秒/110 帧；中文字幕/溶解抽帧已查看，黑 Y=16、白 Y=235，红蓝混合 U=165/V=177；XML 引用的视频与 final.mp4 哈希一致。
- 全套限制：最新 service short 为 230 通过、10 跳过、3 项 Windows 快照目录 rename 拒绝访问。随后这三个用例各重复 3 次全部通过；根因尚未确认，挂 B04 非阻塞调查，不称本次全套零失败。
- 文档：101 个注册路由/102 个展开接口、102 个处理器位置、10 份关联文档/30 个本地链接校验通过。未提交/推送/合并，用户 Gate 尚未通过。
- 待人工/后续：真实模型知识与出稿质量、Premiere 实机导入、独立桌面包；TTS 合成/专页回写留 F3。未对外发送真实素材，浏览器模型为夹具。
- 测试空间：冷编译多次触发 500 MiB 后按规则归简档并清空生成物/cache。最终采用 API→service 逐包编译、统一入口 Clear -CacheOnly，再执行二进制；28 项清理保护通过，最终占用约 133.5 MiB。旧具体日志路径已失效；最终证据重建于 `.run-data/tests/0.1.1/artifacts/custom/script-workbench-20261018/`。

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

- 本轮合并和最简验证已完成；人工全流程等待用户确认。B04 根因、真实模型/Premiere 与发行包验收仍待处理；F3 → F4 未启动。目标、验收、依赖在 `docs/flow-optimize-stages.md`。
- 当前工作区为 agent/develop；所有原分支保留。后续新增开发与合入按用户下一步指示，不自动推进 main 或公开远程。
- 时间轴自绘、幽灵片段、停靠面板仍是未来计划，没有开工，不挡 F2–F4 验收。
- Gate 非阻塞挂账，可选清，不挡 F2 开工。清单在 `docs/flow-optimize-stages.md`「Gate 非阻塞挂账」：F0 存活变异、真并发 reconcile、SSE publish、`visible_set_sha` → 400 的 HTTP API 测、Badge 描边。
- 历史挂账仍然有效：
  - Premiere A08 仍是 pending。要在 Windows 上把交付包导入 Premiere 后再记结果。它不挡 F2、F3 开工，也不挡本交接文档。R1 还不能写成完全通过。步骤在 `docs/真实业务验收执行交接-2026-10-07.md` 第 0.6 节。
  - 真字体和 720p 真渲染要在有真实字体的机器上跑。云电脑没有微软雅黑等字体，F4 不能在这里空过。历史基线里与字体 / 本机路径相关的失败（如 `TestVariantRealWorkerChain` 找不到 `msyh.ttc`、`TestPreviewRealWorkerChain`、`TestPhase7ControlExampleLoads` 的 `E:\VAC\delivery`）不要当成已通过。
