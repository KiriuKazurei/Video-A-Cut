# Video Auto Cut — Agent 预处理与精修交接工作流控制站

「Agent 粗剪 + 人工精修」的游戏录屏视频加工流水线控制站。

**当前阶段：P1 骨架搭建**（Go 控制面 service/store/queue + 事件总线）。

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
webui/             React WebUI（Phase 2）
workers/           Python + Node Worker（Phase 3）
docs/              设计文档
```

## 开发

```bash
go build ./...
go test -count=1 ./...
```
