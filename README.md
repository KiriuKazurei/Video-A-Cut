# Video Auto Cut

游戏录屏的预处理、自动剪辑和人工精修交接工具。Go 管理业务事实，Python/Node 执行任务，React 提供工作区，Tauri 提供 Windows 桌面集成。

更新：2026-10-18；应用版本 0.1.1。当前整合分支 `agent/develop`，源码目录 `E:\AI\Video Auto Cut\.run-data\worktrees\agent-develop`。远程开发功能、测试目录改造、本地审查修复及脚本工作台已合入；功能整合提交 d67e71d。`agent/main` 已同步远程并保留本地历史，`main` 未动。原分支与原工作区保留，未推送或发行。

- [当前工作与待办](docs/当前工作与待办.md)、[当前项目状态](docs/当前项目状态.md)。
- [本轮分支比较、合并与验证](docs/开发分支合并-2026-10-18.md)。
- [架构与接口](docs/当前架构与接口.md)、[前端 HTTP/SSE/IPC 与状态契约](docs/前端接口清单与重构交接.md)。
- [脚本工作台与特殊语法](docs/script-workbench-syntax.md)、[自动模式](docs/自动模式说明.md)。
- [运行与测试](docs/当前运行与测试.md)、[历史文档索引](docs/历史文档索引.md)。

## 当前能力

受控录屏导入和选段、预设与外发许可、场景/解说审查、TTS 模板、字幕/混音、XML/OTIO/ZIP、MP4 预览和 ASS 派生已实现。默认分步生成；自动模式须明确授权，支持有限重剪、用户决定、放弃和撤销。

资源页区分入库与 Agent 可见，可见集确认后进入脚本。脚本工作台支持人写或先看可见素材再生成、花括号语法与高亮、视频绑定、强制字幕、黑白淡变/溶解、修订/否决/人工定稿和画面交付。脚本或可见集变化会使交付失效并暂停受影响自动流程；恢复必须确认相应范围或精确稿件版本。

F2 的 TTS 开关目前保存意图；脚本与既有 TTS 专页/完整自动执行链的接入留给 F3。真实模型质量、Premiere 实机导入、长素材和新桌面发行仍待验收。本轮最简编译、相关模块测试与本机运行已通过；人工全流程等待用户确认。

## 工程入口

| 目录 | 职责 |
|---|---|
| control-plane | SQLite、事务、REST/SSE/MCP、队列、可见集、脚本和自动驱动 |
| webui | 资源、编辑、导出、TTS、设置五工作区 |
| workers/python | 导入、识别、解说、脚本生成、TTS、字幕和混音 |
| workers/node/timeline-cli | 时间线、预览、ASS、脚本字幕/效果渲染与交付 |
| workers/node/export-worker | MCP 导出/预览/派生 Worker |
| desktop | Tauri 窗口、原生能力、后台监督和签名更新 |
| skills/vac-script-syntax | 项目脚本语法与操作约束 |

Go 是唯一事实来源。SSE 只通知变化，连接/重连重新读取 REST。Tauri 提供本机集成，业务仍走 Go HTTP。历史桌面包不能代表本轮源码；当前检查通过不等于人工内容或发行验收。
