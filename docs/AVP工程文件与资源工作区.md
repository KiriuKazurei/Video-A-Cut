# AVP 工程文件与资源工作区

本轮实现以 `agent/vu-skill` 工作树和原有未提交改动为基线。当前执行状态和实际证据分别见[待办](当前工作与待办.md)、[测试维护入口](../scripts/TESTING.md)。工程检查、可见桌面、外部模型质量和人工观感分别记载。

## 底层框架

一个窗口只有一个打开的工程。Go 持有当前工作副本；正式保存的 UTF-8 `.avp` JSON 是已保存内容的依据。SQLite 保存活动工程的可编辑投影、运行索引及全局预设、服务商配置和审计。切换工程在同一事务中替换允许的工程表，保留全局配置。解析或保存失败时保留当前工程和未保存内容。

`format: "avp"`、`schema_version: 1` 的文档保存工程身份、六类资源、完整脚本、时间线/EDL、设置、备注、入出点、工作区状态及运行和交付引用。稳定 ID 连接各对象。原素材通过 `source.relative_path`、`original_path`、SHA-256、大小和修改时间引用；导入预览不复制原素材。已完成产物保留外部文件引用，文件缺失时保留编辑数据并显示缺失状态。

手动保存先在正式文件同目录写完整临时文件并同步，再原子替换。另存为建立独立工程身份，继续引用原素材及已有产物。每 30 秒维护当前有修改工程的一份恢复副本；界面草稿定期同步到 Go 工作副本。首页提供恢复入口，最近工程保留 10 项。

读取文件不发起媒体生成或服务商调用。打开新文件先校验格式与运行投影，再保存或放弃当前内容；有任务时明确停止并等待所属进程/Worker 退出确认。取消或保存失败停留在原工程。重新打开文件把未完成任务置为停止或待恢复状态，不自动重放。

文件组织借鉴 [Premiere 工程与媒体的关系](https://helpx.adobe.com/premiere/desktop/organize-media/create-projects/start-projects.html)。桌面打开请求由 [Tauri 官方单实例插件](https://v2.tauri.app/plugin/single-instance/)转交到已有窗口，启动参数和原生文件选择使用相同的打开流程。

## 功能模块

- 窗口操作直接使用人工身份；没有人工 Token、Cookie、解锁步骤、人工会话有效期或预览票据。切页、隐藏和超时不改变窗口身份。MCP 保留 Agent 的工具范围和运行所有权，内部 Worker 保留 Bearer/角色/租约职责。
- 导入资源直接供当前工程的 Agent 使用。内容指纹用于快照、依赖与变化检测；逐项可见集勾选和确认退出桌面流程。
- 模型、转录和云端配音调用在服务商设置集中配置。预设外发许可和全局云端配音开关不写入工程，密钥通过全局设置/环境提供。
- 资源包括视频、音频、图片、SRT/ASS/VTT 字幕、TXT/Markdown 文稿及 `.avs` 脚本。生成结果在运行/交付界面管理，显式导入后进入资源列表。
- `.avs` 采用 UTF-8 JSON，保存正文、特殊语法、分段、素材绑定、配音和字幕设置。导入形成工程内副本。工程保存不改写原 `.avs`；工作台显式导出文件。
- 有视频/音频绑定时弹出关联窗口，可选择当前资源或本地文件。“仅导入脚本”不导入原媒体，保留待关联描述和分段位置。待关联脚本可以编辑，使用相关媒体生成或渲染前完成关联。
- 文件保存、显示名称、备注和布局修改不会使媒体任务失效。素材、脚本及时间线内容通过依赖指纹影响关联运行和交付。

## 对应接口

主要契约为 `AvpDocument`、`AvsDocument`、`ProjectResource`、`ProjectSession`，见 [Go 文档模型](../control-plane/internal/project/document.go)、[前端契约](../webui/src/projectContracts.ts)。完整 HTTP 清单见[接口索引](frontend-api-inventory.json)。

| 方法与路径 | 行为 |
|---|---|
| GET `/api/workspace` | 当前工作副本、未保存状态、最近工程、恢复入口与任务计数 |
| POST `/api/workspace/{action}` | new/open/recover/save/save-as/close/undo/redo |
| POST `/api/workspace/inspect-project` | 打开前验证，保持当前工作副本 |
| PATCH `/api/workspace/state` | 保存当前工作区、选中资源和侧栏布局 |
| GET/PUT `/api/workspace/providers` | 全局配音服务调用设置 |
| GET/POST `/api/projects/{id}/resources` | 当前工程资源列表及导入 |
| PATCH `/api/projects/{id}/resources/{resource}` | 名称、备注、入出点、移除及重新定位 |
| GET `/api/projects/{id}/resources/{resource}/content` | 文本/脚本内容和支持 Range 的源媒体/代理 |
| POST `/api/projects/{id}/resources/{resource}/preview` | 本地 FFmpeg 预览代理 |
| POST `/api/workspace/inspect-script` | 读取 `.avs` 的关联需求 |
| POST `/api/projects/{id}/scripts/import-file` | 导入并解析绑定选择 |
| PATCH `/api/projects/{id}/scripts/{script}/bindings` | 关联待绑定素材 |
| POST `/api/projects/{id}/scripts/{script}/export-file` | 显式导出 `.avs` |

`X-VAC-Project-Session` 是请求所属工作副本的标识，用于拒绝切换后的旧请求；它不是人工授权凭证。资源 ID、脚本、运行、交付及 SSE 使用当前工程归属检查。切换取消旧请求、停止播放器并重建界面/查询缓存。MCP 增加 `resources.list`、`resources.read`，只发现当前打开工程；文本/脚本及图片通过各自内容类型返回。

当前 HTTP 注册 156 个模式，服务商诊断展开后为 157 项；桌面自定义 IPC 为 15 个（原 11 个加下列 4 个）。完整清单区分工程新入口、兼容入口及已退休的授权流程。

| IPC / 事件 | 调用契约 |
|---|---|
| `project_dialog` | `{mode,defaultPath?}` → 绝对路径数组，取消返回空数组。mode 为 new-project/open-project/save-project/import-resources/relink-resource/save-script |
| `project_open_request` | 消费启动参数或官方单实例插件转交的一条待打开 `.avp` 路径 |
| `confirm_project_exit` | 前端已完成未保存/停止任务流程后，请求 Rust 正常停止后台并退出 |
| `clipboard_read_text` | Windows 原生读取 Unicode 剪贴板；编辑菜单向原焦点输入框粘贴，浏览器预览使用 Web Clipboard |
| `project-open-requested` / `project-close-requested` | 桌面通知前端打开或关闭；进入同一 ProjectWorkspaceProvider 文件流程 |
| `vac:edit-command` / `vac:request-exit` | 页面内命令：按焦点分派历史，或请求退出；不是跨进程授权接口 |

ProjectWorkspaceProvider 持有当前 Session、资源选中项、编辑器 flush 注册表和文件操作队列。资源备注、脚本草稿及布局先写入 Go 工作副本；Ctrl+S 同步所有注册编辑器后写正式文件。每 15 秒同步未提交界面草稿，Go 每 30 秒更新恢复副本。切换时取消查询和播放器，清除项目缓存并按 Session 重建工作区组件；布局/选中资源写入 AVP，服务商配置仍为全局。浏览器只保留开发预览，文件命令提示使用桌面窗口。

## 显示界面

文件菜单提供新建、打开、最近工程、保存、另存为、导入、导出脚本、关闭、设置与退出；编辑菜单根据焦点分派文本编辑、运行历史或资源历史；视图菜单切换工作区和布局；关于菜单包含版本、更新、快捷键和引导。支持 Ctrl+N/O/S/Shift+S/I。

资源工作区左侧是名称搜索、类型筛选及导入资源列表；中间保留预览、播放操作台和备注；右侧保留可调整宽度的元信息栏。视频/音频先播放源文件，失败时使用本地代理；图片可缩放，文本和脚本显示正文。入出点与备注写入工程，双击脚本进入工作台。移除只改变工程引用：未使用的资源退出列表；被脚本/运行时间线引用的资源保留“待重新关联”占位，可从左栏进入重新定位，并保留原稳定 ID。

## 数据清理与验证边界

本轮只清理用户指定的旧测试数据库中的项目/资产关联记录及 `prj_3d2b73989ec8d593` 生成目录，没有旧项目迁移入口。清理已核验原始录像 SHA、全局配置表、进程退出、重解析点和 Git 边界；同口径释放 129324869 字节。其他工作树、原件、固定样本和预设保留。详细结果见当前待办 AVP04。

真实窗口与人工验收的最终状态以测试维护入口及待办 AVP05 为准；本文件描述实现契约。
