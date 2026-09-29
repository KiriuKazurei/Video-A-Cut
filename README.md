# Video Auto Cut — Agent 预处理与精修交接工作流控制站

「Agent 粗剪 + 人工精修」的游戏录屏视频加工流水线控制站。

**当前阶段：二、三阶段交付与验收通过。** Go 控制面已有 service/store/queue、REST/SSE、受控文件读取、同源 WebUI 托管及 MCP Agent 入口；六阶段 Python Worker 与 Node 导出 Worker 已跑通依赖链。真实录屏样包通过帧、采样、语音位置和音乐压低的自动测量，用户确认 Premiere 中新版视频、字幕和音频正常；三阶段浏览器任务操作及窄窗布局也已复验。

- 设计文档：[`docs/项目开发文档.md`](docs/项目开发文档.md)（定稿，开发落地依据）
- 原始方案：[`视频剪辑流程方案.txt`](视频剪辑流程方案.txt)
- 实施计划：[`.hermes/plans/`](.hermes/plans/)

## 核心原则

- Go 控制面是**唯一事实来源**；REST / SSE / MCP 共用同一个 `service` 层
- `edl.json` 为中枢，扩展为多轨时间线模型（视频 / 游戏音 / 人声 / 字幕 / 音乐）
- 文件交接为主（xmeml / OTIO → Premiere / Resolve 人工精修），实时 MCP 为辅
- Agent 不直接读盘，可见性由控制面唯一裁决点过滤

## 目录

```text
control-plane/     Go 控制面（internal/model, store, service, queue, events）
webui/             React WebUI（二阶段控制台）
workers/           Python + Node Worker（Phase 3）
docs/              设计文档
```

## 开发

```bash
go -C control-plane build ./...
go -C control-plane test -count=1 ./...
go -C control-plane vet ./...
go -C control-plane fmt ./...
```

Go 提供 REST 控制面、SSE 事件流及可选的同源 WebUI 托管。SSE 不提供历史重放：消费者需要在每次打开事件流时获取 REST 快照，并在自动重连后再次同步。受控文件导入、媒体读取和交付 ZIP 需配置 `delivery_root`；运行方式见 `webui/README.md`。二阶段的浏览器预览、试听、下载、重连及窄窗操作已获用户手动验收，记录在 `docs/二阶段验收记录-2026-09-29.md`。

时间线交付 CLI 位于 `workers/node/timeline-cli/`：已可用 FFmpeg/FFprobe 对本地媒体执行切点与同步校验，导出含片段的 OTIO/XML、分轨 WAV 和 SRT；Premiere XML 导入及轨道结构已由用户确认。待后期校对项见 `docs/一阶段开发文档.md`。二阶段范围和交接见 `docs/二阶段开发文档.md`。三阶段 MCP 配置、已建骨架和后续开发门槛见 `docs/三阶段开发文档.md`。
