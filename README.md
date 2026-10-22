# Video Auto Cut

Agent 视频工程工作站：单工程 `.avp`、六类资源、`.avs` 脚本、MCP/API Agent、预览/配音/字幕及 MP4/Premiere 文件交付。完整功能以 Windows Tauri 桌面端为准，浏览器用于开发预览。

| 文档 | 内容 |
|---|---|
| [项目文档](docs/项目文档.md) | 项目目的、功能、技术栈、使用流程、运行方式、开发沿革与当前验证边界 |
| [计划文档](docs/计划文档.md) | 当前工作、下一步、已知问题和待验事项 |
| [接口文档](docs/接口文档.md) | REST/MCP/IPC/SSE、参数/响应、SDK 与公开数据类型 |
| [架构与功能详解](docs/架构与功能详解.md) | 工程保存/隔离、模块/组件、Worker、媒体协议、历史/日志及桌面实现 |

测试维护入口为 [scripts/TESTING.md](scripts/TESTING.md)，仓库操作约束为 [AGENTS.md](AGENTS.md)。机器接口索引、版本化 contracts、运行时 Skill 与上游许可证有独立职责，继续保留。

当前开发分支 `agent/timeline-audio-viz`，应用版本 `0.2.4`。已有本地工程/媒体/桌面验证不代表真实模型质量、人工观感或最新安装包/公开发行已经验收；实际状态以四份主文档和测试说明为准。
