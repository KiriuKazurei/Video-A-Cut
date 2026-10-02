# Video Auto Cut — Agent 预处理与精修交接工作流控制站

「Agent 粗剪 + 人工精修」的游戏录屏视频加工流水线控制站。

**新增服务商通用接入：OpenAI Chat Completions、Anthropic Messages、Google Gemini 原生格式。** 在处理预设中发现/选择模型并进行实际连接测试；识别/解说 Worker 消费固定协议配置。详见 [服务商接入报告](docs/服务商通用接入开发报告.md)。真实服务商、语音与最终交付仍需按报告中的范围验收。

**当前阶段：1–7 的主要功能全部实现；4–7 非人工工程内容已完成并复验，联合人工验收按用户要求后推。** 1–3 保留既有验收结论。已可从受控录屏入口或媒体包/EDL走到内容审查、语音/字幕/混音和 ZIP；真实模型内容质量及新版 Premiere 仍待统一人工校对。详见 [最新全项目进度](docs/全项目进度-2026-10-02.md)。

**第七阶段 A–H 工程已完成。** 统一执行身份、replace/reject、进程排空与内核资源锁、异常隔离、启动/运行重连、同/跨执行成果恢复和断点继续均已补齐。恢复型交付、改选段重跑、最终分轨同步、动态浏览器与局部重启通过，详见 [七阶段修复报告](docs/七阶段执行隔离与断点恢复修复报告.md)。浏览器证据为真实 Edge 自动交互，内容服务为本地 mock，人工验收状态仍为 pending。

以下开发文档仅保留在本地，`docs/` 不随 Git 仓库发布；文档链接适用于本地工作区。

- 设计文档：[`docs/项目开发文档.md`](docs/项目开发文档.md)（定稿，开发落地依据）
- 前端重构交接：[`docs/前端接口清单与重构交接.md`](docs/前端接口清单与重构交接.md)（全部 REST/SSE、页面功能、请求响应和状态约束；附 JSON 清单）
- 原始方案：[`视频剪辑流程方案.txt`](视频剪辑流程方案.txt)
- 第六阶段开发交接：[`docs/六阶段开发文档.md`](docs/六阶段开发文档.md)
- 第七阶段开发交接：[`docs/七阶段开发文档.md`](docs/七阶段开发文档.md)
- 第四、第五阶段人工待验：[`docs/四五阶段联合人工校验.md`](docs/四五阶段联合人工校验.md)
- 全项目人工验收：[`docs/全项目统一人工验收计划.md`](docs/全项目统一人工验收计划.md)（主要功能基本实现后统一执行；开发期间保留自动检查）
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

时间线交付 CLI 位于 `workers/node/timeline-cli/`：已可用 FFmpeg/FFprobe 对本地媒体执行切点与同步校验，导出含片段的 OTIO/XML、分轨 WAV 和 SRT；Premiere XML 导入及轨道结构已由用户确认。阶段记录见 `docs/一阶段开发文档.md`、`docs/二阶段开发文档.md`、`docs/三阶段开发文档.md`；内容分析的后续开发契约见 `docs/四阶段开发文档.md`。
