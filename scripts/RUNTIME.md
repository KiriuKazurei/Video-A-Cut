# 本机运行入口

核对日期：2026-10-18。当前整合源码为 agent/develop，目录 `E:\AI\Video Auto Cut\.run-data\worktrees\agent-develop`。Go 是业务事实来源，Rust 桌面和 PowerShell 入口负责监督进程；依赖预装 Node >=22.12、Python >=3.11（含 sqlite3）、FFmpeg/ffprobe、前端构建产物，桌面另需 WebView2。启动不自动安装工具/模型或替用户批准外发。

从所选源码工作区根目录启动本机业务环境：

```powershell
pwsh -NoProfile -File scripts/Start-VideoAutoCut.ps1 -DataDirectory 'E:\VideoAutoCutData' -DevelopmentBuild
pwsh -NoProfile -File scripts/Stop-VideoAutoCut.ps1 -DataDirectory 'E:\VideoAutoCutData'
```

DevelopmentBuild 需要本地 Go 工具链与缓存依赖。可先显式执行 Build-ControlPlaneRuntime.ps1，再提供 ControlBinary 及匹配 .runtime.json。单个 Go 可执行文件不等于完整应用，必须配对同一版本的前端、Worker 和协议。桌面打包入口见 [桌面说明](../desktop/README.md)，已有打包代码不能代表当前源码已重新发行。

control.db、delivery 和 sessions 保存在独立业务数据目录。runtime.lock 防止多个监督器共用数据库；启动核对迁移版本、SQLite 一致性与旧会话身份，并通过 SQLite backup 保存一致备份。ProfileFile 只在缺少版本时引导建立预设，已有不可变版本不被文件编辑覆盖。

停止入口写入请求，不等于进程已退出。监督器排空 Worker，必要时在期限后停止已核实的所属进程；PID 重用不允许杀死其他进程。Go 退出会终止本次会话。日志/config 可含业务路径和端点，不能当作已脱敏支持包。

升级/回滚先停止已核实进程，保留新业务数据及 WAL；将匹配备份和交付树恢复到新的恢复目录，不以旧库覆盖新业务数据。清理测试、重启业务、构建和发布须分开处理。当前工程、旧桌面验收与本轮未执行项统一见 [当前状态](../docs/当前项目状态.md)。

本轮使用新构建 Go、前端和独立测试数据完成本机运行检查，相关接口与正常退出通过；未执行本节业务监督器的完整人工流程，也未重新打包桌面。脚本工作台直接调用 Python/Node 的资源路径与后续打包边界见 [当前架构](../docs/当前架构与接口.md)。
