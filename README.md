# Video Auto Cut

VAC 是面向 Agent 的视频工程工作站。保存创作脚本并交给外部 Agent，由它根据素材选择口播剪辑、画面摘要、源码绘制或组合路线。用户可直接在编辑页检查与修改成片。Windows Tauri 桌面提供完整功能，浏览器用于开发预览。

| 文档 | 用途 |
|---|---|
| [项目文档](docs/项目文档.md) | 项目定位、当前基线、技术栈、运行和开发沿革 |
| [功能文档](docs/功能文档.md) | 各模块职责、现有能力、复用入口及Agent开发限制 |
| [计划文档](docs/计划文档.md) | 当前工作、问题、下一步和待验事项 |
| [接口文档](docs/接口文档.md) | REST/MCP/IPC/SSE及完整参数/类型索引 |
| [架构与功能详解](docs/架构与功能详解.md) | 工程持久化、模块、Worker、渲染与桌面机制 |
| [文档索引](docs/文档索引.md) | 全部说明的职责、开发交接和历史边界 |

当前源码版本为 **0.5.2**。Windows x64 安装版见 [0.5.1 GitHub Release](https://github.com/KiriuKazurei/Video-A-Cut/releases/tag/v0.5.1)。0.3.4 至 0.5.1 的生产更新、验签与自动重启已实测。

连接本机 Agent 时，在 **设置 → Agent 与 Skill → Agent MCP 连接** 复制 Codex 安装命令、连接地址或客户端配置。0.5.2 的 MCP 初始化会指引 Agent 通过 `skills.get` 读取 `vac-video-edit`，并用 `skills.read_resource` 读取选路和交付参考。然后从脚本工作台复制创作任务。 [VAC 视频编辑 Skill](skills/vac-video-edit/SKILL.md) · [脚本语法教程](docs/user-guide/脚本语法教程.md)

测试与验证证据见[TESTING](scripts/TESTING.md)，仓库约束见[AGENTS](AGENTS.md)。
