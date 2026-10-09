# Windows 更新器（0.1.1）

本版本提供应用内手动检查、下载进度、签名验证及确认后安装重启。
React 展示状态；Rust 使用 Tauri updater 插件下载和验证签名，并先停止 Go 与 Worker。
媒体和业务请求仍由原有本机 HTTP 接口处理。

## 使用

在桌面顶部点击“检查更新”，查看版本和更新说明，下载后点击“安装并重启”。
下载期间可继续使用，网络错误或签名错误不会停止后台。
安装前原生对话框会提示当前任务数量；“稍后安装”保留当前任务和已下载更新。
确认后沿用 Worker 最多 15 秒、Go 最多 5 秒的停止流程，释放 Job Object 和数据锁，
再启动 NSIS 安装器。安装到当前程序目录，并重启程序。

正式数据独立于程序目录，不迁移或覆盖数据库。更新时保留自定义配置路径或数据目录，
以及已完成结果。正在运行的任务会停止，启动后的恢复行为继续遵循原有业务规则。
`--desktop-config <绝对路径>` 与 `--data-dir <绝对路径>` 可用于持久化启动方式；
环境变量 `VAC_DESKTOP_CONFIG` 与 `VAC_DATA_DIR` 也会转为重启参数。
外部适配器凭据应使用用户级环境变量或原有后台配置；安装器重启经 Explorer 启动，
不会继承调用终端临时设置的其他环境变量。

更新来源固定为当前仓库 GitHub Releases 的 `latest.json`：

`https://github.com/KiriuKazurei/Video-A-Cut/releases/latest/download/latest.json`

生产构建只接受 HTTPS，内置公钥验证更新包。签名失败不允许安装。
只有调试构建允许 `VAC_UPDATE_TEST_ENDPOINT` 指定有端口的 `127.0.0.1` HTTP 测试源；
该配置无法覆盖签名公钥。调试程序不能构建为发布安装包。
参考 [Tauri updater 官方文档](https://v2.tauri.app/plugin/updater/)。

## 密钥与构建

本地已生成独立更新签名密钥：`build/desktop-signing/update.key`，公钥为同路径 `.pub`。
私钥及生成日志只授权当前用户读取，位于 Git 忽略目录，不进入安装包或前端。
请将私钥备份到自己控制的安全位置；后续版本必须使用同一密钥签名。
这是更新包签名，不是 Windows Authenticode 代码签名证书。

已准备好 Node/npm、Go、Rust/MSVC、Cargo 依赖后运行：

```powershell
pwsh -NoProfile -File scripts/Build-TauriDesktop.ps1 -Installer
```

默认读取本地密钥，也支持官方 `TAURI_SIGNING_PRIVATE_KEY` 与
`TAURI_SIGNING_PRIVATE_KEY_PASSWORD` 环境变量。构建使用固定 Cargo.lock 和离线 Go/Cargo。
初次安装包构建可能下载 NSIS 构建工具；桌面运行时不下载工具、WebView2 或模型。
WebView2 必须预装。NSIS 安装模式为当前用户，无管理员安装要求。
安装语言依据系统自动选择，不显示额外语言选择框，避免阻塞应用内更新。

输出 `build/desktop/app/` 可运行目录及 `build/desktop/updates/0.1.1/` 中三个发布文件：

- `Video Auto Cut_0.1.1_x64-setup.exe`：安装包，也用于后续应用内更新。
- `Video Auto Cut_0.1.1_x64-setup.exe.sig`：更新包签名。
- `latest.json`：版本、更新说明、安装包链接和签名。

## GitHub Releases 发布

每次发布同时修改 `desktop/tauri.conf.json`、`desktop/Cargo.toml`、
`desktop/package.json` 的版本，并更新相应锁文件。必须提高版本号。
构建后用以下命令补充更新说明并重新生成清单：

```powershell
$release = 'build/desktop/updates/0.1.1'
pwsh -NoProfile -File scripts/New-TauriUpdateManifest.ps1 -Version 0.1.1 `
  -Installer "$release/Video Auto Cut_0.1.1_x64-setup.exe" `
  -OutputPath "$release/latest.json" -Notes '加入签名更新器，保留已有业务数据。'
```

在 `KiriuKazurei/Video-A-Cut` 创建正式 Release，标签 `v0.1.1`，
上传这三个文件并将该 Release 标为 Latest。清单已使用对应标签的 GitHub 下载地址。
只发布安装包不会使应用内检查生效；`latest.json` 必须作为同一 Release 的附件上传。
预发布不作为当前稳定更新源。以后性能优化、前端补丁、Rust、Go 和 Worker 变更均通过
新的完整签名安装包交付，用户在应用内确认升级即可。

当前仅生成本地发布文件，未创建公开 Release。发布前 GitHub 更新源可能返回 404，
应用会显示检查失败；这不会影响业务运行。既有不含更新器的 0.1.0 程序需先替换到
本版本，后续版本才能通过该更新通道获取。

## 验收

验收脚本：`scripts/verify-tauri-updater.mjs`；本地隔离复制目录 `.run-data/updater/`。
使用调试构建的 0.1.0 作为带更新器的升级基线，下载实际签名的生产 0.1.1 NSIS 安装包。
2026-10-09 的实际 Windows/WebView2 验收 11/11 通过，结果保存于
`.run-data/updater/results.json`，截图为 `update-downloaded.png` 与 `updated-ready.png`。

| 项目 | 结果 |
|---|---|
| 真实窗口、Go/Worker 就绪，历史资产可访问 | 通过 |
| 同版本、更新源 HTTP 错误、非 HTTPS 安装包拒绝 | 通过，业务继续运行 |
| 安装包被修改一字节 | 签名拒绝，未停止业务 |
| 下载进度与重复操作互斥 | 通过，真实安装包 15,686,830 字节 |
| 有 TTS 任务时取消原生安装确认 | 会话保持，TTS 自然完成，结果保留 |
| 安装确认期间请求后台重启或退出 | 受维护状态保护 |
| 确认安装后旧 Go/Worker 退出并释放锁 | 通过 |
| 实际 NSIS 安装 0.1.1 并自动重启 | 通过，可见窗口重新打开 |
| 中文/空格路径、自定义配置、历史资产、TTS 记录 | 重启后保留，Worker 在线 |
| 更新后的生产程序受控退出 | 通过，无本次进程残留 |

TypeScript/Vite 和 Rust Release 构建通过，前端回归 35/35、Rust 测试 5/5。
静态测试记录位于 `.run-data/updater-web-tests.log` 与 `updater-rust-tests.log`。
实际升级暴露并修复了首次 NSIS 语言选择框阻塞的问题，修复后的安装包完成以上验收。
验收使用隔离数据库；NSIS 的两项测试注册记录已按安装路径核验后移除，未碰正式安装。

最后一轮升级测试占用从 467.840 MiB 增至 498.858 MiB，低于 500 MiB 上限。
每轮均检查测试占用，前期失败的 JSON 报告也保留。公开 GitHub 下载尚未验收，
不能将本地签名更新成功等同于公开发布完成。
