# Windows 桌面

核对日期：2026-10-16。源码应用版本为 0.1.1，版本基准在 tauri.conf.json。Tauri/Rust 管理窗口、托盘、原生文件对话框、后台进程和更新；Go 管理 SQLite 和业务，React 通过本机 HTTP REST/SSE/Range 读取媒体，不通过 IPC 传帧。

完整运行目录为 build/desktop/app，入口 video-auto-cut-desktop.exe；Go、.runtime.json、Worker 和协议资源须来自同一构建。Node >=22.12、Python >=3.11、FFmpeg/ffprobe 和 WebView2 需预装，程序不下载模型。10 月 9 日已有桌面包及验收是历史证据，不能代表新增自动模式与当前修复已经打包。

正式数据默认放在 `%LOCALAPPDATA%/io.kiriukazurei.video-auto-cut/data/`，独立于运行资源。参考 [desktop.example.json](desktop.example.json) 创建自己的配置，设置实际绝对路径与允许素材根目录；不接受 UNC/重解析路径。可用 VAC_DESKTOP_CONFIG、VAC_DATA_DIR 或 --desktop-config/--data-dir 指定配置/数据；不要在测试目录存正式数据。

关闭窗口隐藏至托盘；退出、故障、重启和更新都经过后台监督。Worker 排空与 Go 停止有各自期限，所属后代由 Windows Job Object 约束；UI 的 ready 不等于所有业务或人工验收完成。生产前端来源限 Tauri 配置，开发来源须显式配置，不放开通配来源。

## 构建与验证

依赖先行准备后，从源码根执行 `pwsh -NoProfile -File scripts/Build-TauriDesktop.ps1`。Debug 输出 app-debug；Installer 模式需要更新签名配置。Go/Cargo 使用本地固定依赖；缺少工具应报告。当前版本未因文档修正重新构建、启动或发布。

开发使用 Vite 严格端口，desktop_origin 配置为实际开发来源，再按本机已安装 Rust 工具链运行 desktop/Cargo.toml。历史曾验证的某个 Rust/Edge 版本不是本轮再次核验过的运行要求。

[更新流程](UPDATER.md) 区分代码、已有本地签名验收和新的公开发行；[首版验收记录](ACCEPTANCE.md) 保留原日期与范围。新版干净环境、真实更新源、系统托盘人工交互和完整业务素材验收仍见 [待办](../docs/当前工作与待办.md)。
