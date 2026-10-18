# Video Auto Cut

游戏录屏的预处理、自动剪辑和人工精修交接工具。Go 管理业务事实，Python/Node 执行任务，React 提供工作区，Tauri 提供 Windows 桌面集成。

当前说明核对日期：2026-10-17；应用版本取 `desktop/tauri.conf.json`，目前为 0.1.1。当前源码为本地 `agent/main@e582f2f` 合并结果及 `agent/merge-followup` 未提交修复，尚未推送；稳定分支 `main` 保持 `0f4a894`。本次实际源码目录是 `E:\AI\Video Auto Cut\.run-data\worktrees\agent-main`；原根目录的旧测试分支保留未提交测试目录改造，不能把两处源码或验收结果混用。

- [当前工作与待办](docs/当前工作与待办.md)：唯一执行清单，包含待验项目。
- [当前项目状态](docs/当前项目状态.md)：分支、工程检查、真实运行与人工验收的边界。
- [当前架构与接口](docs/当前架构与接口.md)、[前端完整接口清单](docs/前端接口清单与重构交接.md)、[自动模式说明](docs/自动模式说明.md)。
- [运行与测试](docs/当前运行与测试.md)、[历史文档索引](docs/历史文档索引.md)。旧报告只证明其记载版本的结果。

## 当前能力

受控录屏导入和选段、处理预设及外发授权、场景与解说审查、SAPI/固定 TTS 模板、字幕与混音、XML/OTIO/ZIP 交付、MP4 预览和 ASS 样式派生已实现。分步生成默认开启；自动模式需要显式授权，按自评结果完成、等待用户决定、有限重剪或中止。自动自评不会写入人工验收通过。

本轮已有 Go short、离线自动模式 E2E、前端和浏览器 mock 证据；新增路径使用真实 Worker/FFmpeg 交付 1280×720、8 秒成片并检查中文抽帧。模型/TTS 响应仍来自本机测试桩。真实模型质量、阈值校准、外发提供方、长素材、剪辑软件人工导入及当前候选桌面发行仍按待办验收。不能沿用 10 月 9 日桌面包替新前端背书。

## 工程入口

| 目录 | 职责 | 说明 |
|---|---|---|
| control-plane | SQLite、业务事务、REST/SSE/MCP、队列和自动驱动 | [架构](docs/当前架构与接口.md) |
| webui | 五工作区、审查、授权和交付 | [WebUI](webui/README.md) |
| workers/python | 导入、识别、排序、解说、TTS、字幕、混音 | [Python Worker](workers/python/README.md) |
| workers/node/export-worker | 导出、预览、ASS 派生 Worker | [Node Worker](workers/node/export-worker/README.md) |
| workers/node/timeline-cli | 本地时间线导出与真实渲染内核 | [CLI](workers/node/timeline-cli/README.md) |
| desktop | Tauri 窗口、后台监督、文件对话框和签名更新 | [桌面](desktop/README.md) |
| contracts | 固定输入、摘要和产物协议 | [MP4](contracts/mp4-preview/v1/README.md)、[ASS](contracts/delivery-variant/v1/README.md) |

Go 是唯一事实来源，REST/SSE/MCP 共用 service；Worker 不打开 SQLite。SSE 不重放历史，连接和重连都要重新获取 REST 快照。预览“720p”是最大尺寸且不放大小素材。构建、启动、测试和发行是不同操作，具体入口见运行与测试文档。本文不构成推送、发布、外发或清理授权。

## 最新审查状态

代码审查发现的 CR01（自定义协议重定向）和 CR02（自动授权版本继承）已修复并回归通过；CR03（时间线键盘审批对象）仍待修。Python 148 项、前端 67 项与自动模式浏览器 20 组通过，详见 [代码审查与复验](docs/代码审查-2026-10-16.md)。
