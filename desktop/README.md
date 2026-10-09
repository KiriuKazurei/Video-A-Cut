# Windows 桌面首版

可运行目录为 `build/desktop/app/`，入口是 `video-auto-cut-desktop.exe`。
整个目录一起保留：Go 程序、版本清单和 `runtime/` 中的 Worker 都是运行资源。
版本 0.1.1 加入签名更新器，不包含 Node、Python、FFmpeg、WebView2 和模型。
首版业务验收见 [ACCEPTANCE.md](ACCEPTANCE.md)，更新器使用与发布见 [UPDATER.md](UPDATER.md)。

React 继续通过本机 HTTP 请求 Go 的 REST、SSE 和媒体 Range 接口。
Rust 管理窗口、托盘、原生文件对话框及后台进程；业务与 SQLite 仍由 Go 管理。
Go 以附带程序交付，参考 [Tauri 官方 sidecar 机制](https://v2.tauri.app/develop/sidecar/)。

## 启动

先在本机安装 Node >=22.12、Python >=3.11、FFmpeg/FFprobe 和 WebView2。
Python 需提供标准库 SQLite；使用既有其他模型/适配器时还需准备其原有本地依赖。
启动时检查本地工具，不下载工具或模型。

工具在 PATH 中时，可以直接运行入口程序。
正式数据默认位于 `%LOCALAPPDATA%/io.kiriukazurei.video-auto-cut/data/`，独立于程序资源。
默认没有录像素材根目录，使用原生录像选择前需要配置允许的根目录。

复制 [desktop.example.json](desktop.example.json)，修改数据目录、程序资源路径、工具路径和录像根目录，然后在仓库内运行：

```powershell
$env:VAC_DESKTOP_CONFIG = (Resolve-Path 'desktop/desktop.local.json').Path
Start-Process -FilePath 'build/desktop/app/video-auto-cut-desktop.exe' -WindowStyle Hidden
```

也可通过命令行指定配置；更新安装器重启时会保留该参数：

```powershell
& 'build/desktop/app/video-auto-cut-desktop.exe' --desktop-config (Resolve-Path 'desktop/desktop.local.json').Path
```

`desktop.local.json` 是复制后自行命名的配置文件，需要填写本机实际绝对路径。
工具可以是 PATH 中的名称，也可以是工具可执行文件的绝对路径。
其他运行路径必须为本机绝对路径，不接受 UNC、符号链接或重解析点。
数据目录必须独立于程序的 `runtime/` 目录。

也可以设置 `VAC_DATA_DIR` 指定已有数据目录；该目录有 `desktop.json` 时读取该文件。
`VAC_DESKTOP_CONFIG` 优先于此设置。
不会自动迁移业务数据，首次使用旧目录前需退出原来的服务。
已有数据库先进行版本兼容检查与启动前备份，再允许 Go 打开。
备份位于 `sessions/<会话 ID>/pre-start.db`；首次无数据库时不生成空备份。
旧 PowerShell 会话如仍有身份匹配的存活进程，会拒绝启动，不根据历史 PID 杀进程。

可选 `profile_file` 指定已有处理预设 JSON。
默认使用附带的 builtin 预设，只有不存在的第 1 版会首次创建；已有预设版本保持原值。
外部适配器凭据继续通过原有后台环境变量提供，不进入前端。
可选 `web_root` 指向 WebUI 构建目录，用于保留浏览器入口或同机对照，默认关闭。

## 窗口与后台

关闭主窗口会隐藏至托盘，任务继续运行。
托盘点击或“显示主窗口”恢复窗口；重复启动会恢复已有窗口。
“退出”会在任务运行或任务状态无法确认时弹出确认框。
确认后先通知 Worker 停止，最多等待 15 秒，再通过 stdin 通知 Go，最多等待 5 秒。
本次所属进程和后代加入 Windows Job Object，最终清理 FFmpeg 等后代。
Go 意外退出会停止本次 Worker，窗口显示故障，可选择重新启动。
Worker 沿用有限重试规则；永久错误或耗尽预算会显示停止状态。

原生选择返回素材根目录 ID 和相对路径，仍由 Go 按受控根目录规则验证。
视频直接由 WebView 从 Go 读取，不通过 IPC 传输帧。
原生下载以临时文件完整写入后再替换目标，保留原有媒体下载契约。
后台日志和会话诊断位于数据目录的 `sessions/`。

## 构建与开发

已验证 Rust `1.98.1-x86_64-pc-windows-msvc`、本机 MSVC、Go 和 Node 环境。
先显式准备 WebUI/npm、Worker/npm 和 Cargo 依赖，然后构建：

```powershell
pwsh -NoProfile -File scripts/Build-TauriDesktop.ps1
```

构建脚本使用离线 Go/Cargo 依赖，不安装缺失工具。
随源码保留的 Cargo.lock 固定已验证的 Tauri 2.11 依赖组合；更新依赖后需重新验证。
`-Debug` 输出 `build/desktop/app-debug/`。交付 Windows x64 可运行目录和签名 NSIS 安装包。
构建更新包使用 `-Installer`，具体签名与 GitHub Releases 发布步骤见 [UPDATER.md](UPDATER.md)。

开发模式先构建附带 Go 资源，配置 `desktop_origin` 为实际 Vite 地址
`http://127.0.0.1:5173`，并用严格端口启动 Vite：

```powershell
npm --prefix webui run dev -- --strictPort
# 在另一个终端设置自己的 VAC_DESKTOP_CONFIG：
$env:CARGO_TARGET_DIR = (Join-Path $PWD 'build/desktop')
cargo +1.98.1 run --offline --locked --manifest-path desktop/Cargo.toml --no-default-features
```

生产模式只允许 `http://tauri.localhost`。
Go 保留 Host 校验，明确处理 OPTIONS，不使用通配来源。
来源说明参考 [Tauri 官方迁移文档](https://v2.tauri.app/start/migrate/from-tauri-1/)。

顶部“检查更新”可检查、下载并验证签名，经确认后停止后台、安装并重启。
更新保留数据目录和已完成的结果；运行中的任务将停止。
自动定时检查、系统全局快捷键、界面和预览专项性能优化留待下一批。
原有页面导航及应用内快捷键继续使用。
