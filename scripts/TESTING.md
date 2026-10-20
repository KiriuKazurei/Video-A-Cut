# 测试目录、版本与简档

更新：2026-10-20。本文为测试维护入口。实际开发源码在 `agent/vu-skill` 的 `.run-data/worktrees/codex-skill-harness`；原根目录仍是旧工作区。多工作区约束见 [当前运行与测试](../docs/当前运行与测试.md)；执行清单见 [当前工作与待办](../docs/当前工作与待办.md)。

## AVP 工程重构（当前验证）

基线 `agent/vu-skill@ef0880b` 加保留的未提交改动。实现契约见 [AVP 工程与资源工作区](../docs/AVP工程文件与资源工作区.md)。沿用 Go/package、前端原单元/样式与 `verify-tauri-desktop.mjs`，没有建立按任务复制的独立套件；媒体复用 `workers/node/timeline-cli/fixtures/media/clip_01.mp4`，小型编码/音频夹具只在版本化输出生成。

| 层 | 已确认结果与证据 |
|---|---|
| 权限与隔离 | 无人工 Token/Cookie/解锁/预览票据；HTTP Range 206、工程 A/B 直接 ID 404、过期请求 409、旧授权流程 410、首页日志可读。MCP 12 工具、当前工程资源发现/读取及 Worker 既有鉴权回归通过 |
| 文件与任务 | 原子覆盖失败保留原件及 dirty、损坏文件不切换、外部媒体缺失/重定位、恢复副本、独立另存、全新 SQLite 从 AVP 重建通过；切换取消并等待 HTTP 请求及 Worker 空闲确认通过 |
| 脚本与交付 | 无绑定直接导入、有绑定要求选择、仅脚本保留待关联、当前素材重关联、本地文件替换、AVS 导出再导入通过。真实 Go→Python→本机确定性模型夹具→Go→Node/FFmpeg 生成/定稿/渲染往返通过；真实媒体抽帧/两版交付/检查/历史预览/另存为后输出引用通过。无关素材和显示标题修改保持成片，相关内容变化使依赖失效 |
| 前端工程 | 完整 `npm run test:unit`（含样式检查）、TypeScript、生产 Vite 构建通过；未把单元/类型检查当成可见窗口证明 |
| 桌面工程 | Rust 1.98.1、官方 single-instance 2.4.5，offline/locked debug 构建通过；当前 Go 二进制与运行清单、生产前端嵌入真实 WebView |
| 可见桌面 | 最新 **22/22**：首页 HWND/桌面桥、原生新建/导入/保存/另存/打开、六类预览、逐帧/标记/备注、脚本双击/原 AVS 保留、未保存/窗口关闭取消、待执行任务提示/取消后切换、A/B 隔离、单实例转交、文件框取消、待关联导入/重绑定/AVS 导出及取消、缺失占位重定位、FFv1/TIFF 实际 FFmpeg 代理、独立另存、正常退出和新进程从 AVP 参数重开。键盘菜单及剪贴板往返通过，无页面异常。`custom/avp-desktop-ui/results.json`、home/resource-preview/workspace-final.png。桌面使用经窗口 HTTP 创建的待执行 MCP 任务验证切换分支，不调用模型；执行中 Worker 退出另由服务用例验证 |
| 完整回归 | 最新当前源码 **Go 全部包通过**：root 14.851 秒、API 22.249 秒、MCP 32.229 秒、service 428.607 秒、store 7.303 秒，其余全部通过。使用原根短版本化输出、显式当前源码 WorkingDirectory、-p=1/-count=1/-timeout=600s，未跳过失败用例。Go vet 全包、Rust fmt 检查通过。早期 180 秒超时及嵌套长路径失败分别保留 custom/avp-first-full-regression.md、avp-full-regression.md，不改写失败事实 |
| 人工与外部 | 人工观感、真实外部 LLM/转录/云配音质量、Premiere 实机、安装包/OS 文件关联安装/发行更新未验；本地夹具不调用云端、不下载模型 |

所有 `custom/...` 证据相对当前工作树的 `.run-data/tests/0.1.1/artifacts/`。原根目录共享 Cargo 构建缓存仍位于根目录对应版本，不将旧根源码的测试替代当前实现。

```powershell
$source = 'E:/AI/Video Auto Cut/.run-data/worktrees/codex-skill-harness'
$runner = "$source/scripts/TestWorkspace.ps1"
$goRunner = 'E:/AI/Video Auto Cut/scripts/TestWorkspace.ps1'
& $goRunner -Action Run -Suite custom -Executable go -WorkingDirectory "$source/control-plane" -Arguments @('test','./...','-p=1','-vet=off','-count=1','-timeout=600s')
$npmCLI = Join-Path (Split-Path (Get-Command node).Source) 'node_modules/npm/bin/npm-cli.js'
& $runner -Action Run -Suite custom -Executable node -WorkingDirectory "$source/webui" -Arguments @($npmCLI,'run','test:unit')
& $runner -Action Run -Suite custom -Executable node -WorkingDirectory "$source/webui" -Arguments @('node_modules/typescript/bin/tsc','--noEmit')
& $runner -Action Run -Suite custom -Executable python -WorkingDirectory $source -Arguments @('scripts/check-documentation.py','--root',$source)
& $runner -Action Run -Suite custom -Executable python -WorkingDirectory $source -Arguments @('scripts/verify-script-workbench-docs.py')
$env:VAC_DESKTOP_EXECUTABLE = 'E:/AI/Video Auto Cut/.run-data/tests/0.1.1/cache/cargo/debug/video-auto-cut-desktop.exe'
& $runner -Action Run -Suite custom -Executable node -WorkingDirectory $source -Arguments @('scripts/verify-tauri-desktop.mjs','9223','launch',"$source/.run-data/tests/0.1.1/artifacts/custom/avp-desktop-ui","$source/.run-data/tests/0.1.1/artifacts/custom/avp-desktop/desktop.json")
```

桌面入口必须自己在管理器内部启动及退出，不能先启动窗口再绕过在用进程保护。测试用 production overlay 的 `frontendDist` 使用相对 desktop 目录的 `../.run-data/tests/0.1.1/artifacts/custom/avp-ui-dist`；Windows 绝对路径会被当成 URL。测试配置只替换打包资源来源，运行 Go/四类 Worker；业务数据根独立于源码资源根，使用原根目录版本化 `custom/avp-desktop-runtime/data`。正式打包继续使用原 Build-TauriDesktop.ps1，不把此 overlay 当发行包。

Windows 下管理器直接启动可执行文件，npm 使用已安装 Node 的 npm-cli.js；直接传 npm 曾因其为 shell 包装而启动失败，改用该入口后 45+47 共 92 项及样式检查通过。嵌套工作树输出前缀过长会使既有长名称 Go/Node 用例触及 Windows 路径限制。Go 全包复验改用原根 E:/AI/Video Auto Cut/scripts/TestWorkspace.ps1，保持上述显式 $source/control-plane WorkingDirectory；仍只写原根 .run-data/tests/0.1.1。失败三项与最新工程用例在短输出下 40.333 秒通过，未跳过断言；不需要更改系统长路径策略。

原窗口人工会话/隐藏撤权和角色可见集的桌面断言已退休，改为免凭证窗口、工程资源归属和真实文件往返。下文旧 verify-vu-runtime.ps1 治理/确认流程及浏览器录制结果保留历史范围，不是 AVP 工程模式验收入口。早期配置/控件/菜单/CDP 探针失败、真实粘贴及保存期间退出的问题见 custom/avp-desktop-first-failures.md；补充输入框就绪及原生可见文件名控件定位后最终 22 项通过，失败不能改写成当时通过。桌面目录 rust-toolchain.toml 固定已经实际构建的 1.98.1，无需更改系统默认 Rust。

用户指定旧数据清理已完成：仅原根 live-dev 的 control.db 项目/资产关联与指定 prj 生成目录，删除前重新核对 2 项目/2 资产、进程/路径/重解析点/Git 边界；原始录像 SHA、导入原件以外的固定样本、全局服务商/预设及其他工作树保留。相同 DB/WAL/SHM/指定目录口径 178865989→49541120 字节，释放 **129324869 字节**；完整性检查 ok。`custom/avp-cleanup.json` 记录细项，未提供旧项目迁移入口。

通过 scripts/TestWorkspace.ps1 的 custom、WorkingDirectory 和 ArgumentsJson 选择实际源码。原根目录管理入口的内置 suite 指向旧源码，不能替 develop 背书。输出使用 VAC_TEST_ROOT/VAC_TEST_OUTPUT，版本来自 desktop/tauri.conf.json；TEMP/TMP/TMPDIR、Go/npm 缓存与 Python 禁写缓存统一设置。

测试管理遵循全局 `test-governance` Skill：维护现有套件与共享样本，按所属套件复用缓存、替换可再生输出，保留仍需定位的最小失败证据。自 2026-10-19 起取消固定容量上限及按容量中止测试、自动清空 artifacts/cache 的行为；历史容量事件保留当时事实，旧上限不再适用。`Measure` 和运行前后统计仅报告 `version/total_bytes/total_MiB`，不再返回 `limit_bytes/within_limit`。

小版本更新后 Prepare；同版本按需显式 Clear；Clear -CacheOnly 保留 artifacts。清理前归每版本一份 Markdown，最多 3 版，未知修复版本/提交写未记录。清理必须验证进程退出、绝对路径、父子重解析点和 Git 跟踪边界，不删除源码、固定样本、业务数据或密钥，清理后报告同口径前后占用及释放量。正常失败保留退出码、摘要及输出，不触发清空。

本次治理验证（0.1.1，2026-10-19）：根目录入口（旧工作区 0f4a894 加本地改动）和本工作树入口（agent/vu-skill@ef0880b 加本地改动）分别执行 `pwsh -File scripts/TestWorkspace.ps1 -Action Run -Suite workspace`，各 **36 项通过**，不将旧源码检查代替当前分支结果。覆盖运行中稀疏文件增长至 501 MiB 并持续 6 秒、占用只读统计、Prepare 保留输出/缓存、失败退出码 7 和摘要/证据保留；原版本轮换、3 版简档、未知目录、重解析点、在用进程及显式 Clear/CacheOnly 保护仍通过。两处日志分别位于各自 `.run-data/tests/0.1.1/artifacts/workspace/output.log`；大尺寸稀疏夹具验证后已由所属套件移除，不新增永久媒体或独立套件。本轮仅验证测试基础设施与文档，未重复产品构建/模型/UI 验收。

2026-10-18 的验证范围保留在 [整合记录](../docs/开发分支合并-2026-10-18.md)，不作为当前全部能力的通过结论。前端修复历史沿用 [独立校验记录](../docs/前端与接口独立校验-2026-10-19.md)；本页最新全流程复验另验证真实媒体与生产页面衔接，不代表模型质量验收。

## MCP / Skill 测试覆盖与入口

| 能力 | 维护套件 / 共享依赖 | 本轮处理 |
|---|---|---|
| 测试工作区生命周期 | [test-workspace.ps1](../test-support/test-workspace.ps1)；原 workspace 套件、Node/Python、Windows 稀疏文件 | 扩展现有套件：运行中增长至 501 MiB 并持续超过旧采样间隔，验证完成、Measure/Prepare 保留输出、失败退出码/摘要/日志保留；继续覆盖版本轮换、3 版简档、未知目录、重解析点、在用进程与显式 Clear/CacheOnly |
| MCP 身份、工具发现、资产/角色隔离、租约和权限撤销 | [mcpserver](../control-plane/internal/mcpserver/server_test.go)、security_test.go、tasks_test.go；共用 loopFixture | 保留现有覆盖；原完整包通过；新增能力纳入同包 |
| 官方 SDK 外部接入及剪辑交付 | [skill_run_test.go](../control-plane/internal/mcpserver/skill_run_test.go)；Go SDK v1.8.0、Python/Node/FFmpeg/ffprobe、现有 clip_01.mp4 | 原用例参数化内部 Bearer / 桌面免凭证；覆盖哈希契约分发、原生图片、两版渲染、定向修改、人工门和名称/来源日志；复用原样本，不新增独立套件 |
| Skill 预算/幂等/撤销重做、真实媒体和内置模式 | [service 既有 Skill 测试](../control-plane/internal/service/skill_harness_test.go)、skill_history_test.go、skill_lightweight_test.go、skill_agent_test.go | 保留服务层独立职责；内置 ASR/模型由本机夹具响应，不是真实 Provider |
| 服务装配与真实进程 | [mcp_integration_test.go](../control-plane/mcp_integration_test.go)、[verify-vu-runtime.ps1](verify-vu-runtime.ps1) | 原探针已扩展为实际 HTTP/MCP 连续导入/治理/确认/提案/人工定稿/三版 MP4/Premiere/解码与 Range/片段及时间点修改/历史预览/撤销重做/取消/日志；人工操作不再仅直接调用 service。失败写最小 JSON，子进程自动退出 |
| 无凭证连接与协议安全 | 现有 server_test.go / security_test.go / config_test.go | 增加 4 版本 × 两种模式、回环/Origin/代理/错误凭证拒绝、关闭/未知会话、同名不同连接隔离、仅 10 个外部工具的回归；现有安全测试保留 |
| 真实成片与生产页面衔接 | [verify-skill-ui-completion.mjs](verify-skill-ui-completion.mjs) 的 --real-flow 模式；当前生产 Vite 构建、Edge、正常开发环境 playwright-core | 复用既有浏览器入口，读上项实际运行的测试数据库副本/登记媒体，不拦截接口；默认模式保留原组件转换夹具测试，两者职责分开 |

从 PowerShell 7（启用 profile）执行；以下是当前工作区的最短复验命令，所有输出/临时媒体/数据库仍由原根入口定向至 `.run-data/tests/0.1.1/`：

```powershell
$source = 'E:/AI/Video Auto Cut/.run-data/worktrees/codex-skill-harness'
$runner = 'E:/AI/Video Auto Cut/scripts/TestWorkspace.ps1'
# 按改动影响选择包；可组合回归，不再为旧容量上限强制拆分。
& $runner -Action Run -Suite custom -Executable go -WorkingDirectory "$source/control-plane" -Arguments @('test','-count=1','-p=1','-gcflags=all=-dwarf=false','-ldflags=-s -w','./internal/mcpserver')
& $runner -Action Run -Suite custom -Executable go -WorkingDirectory "$source/control-plane" -Arguments @('test','-count=1','-p=1','-gcflags=all=-dwarf=false','-ldflags=-s -w','./internal/service','-run','Test(Skill|Submit)')
& $runner -Action Run -Suite custom -Executable go -WorkingDirectory "$source/control-plane" -Arguments @('build','-p=1','-gcflags=all=-dwarf=false','-ldflags=-s -w','-o','E:/AI/Video Auto Cut/.run-data/tests/0.1.1/artifacts/custom/vac-control-plane.exe','.')
& $runner -Action Run -Suite custom -Executable 'C:/Program Files/PowerShell/7/pwsh.exe' -WorkingDirectory $source -Arguments @('-File',"$source/scripts/verify-vu-runtime.ps1")
& $runner -Action Run -Suite custom -Executable node -WorkingDirectory "$source/webui" -Arguments @('node_modules/vite/bin/vite.js','build','--outDir','E:/AI/Video Auto Cut/.run-data/tests/0.1.1/artifacts/custom/vu-ui-review-dist','--emptyOutDir')
& $runner -Action Run -Suite custom -Executable node -WorkingDirectory $source -Arguments @('scripts/verify-skill-ui-completion.mjs','--real-flow')
```

`--real-flow` 必须紧接完整运行探针（生成 vu-runtime-result.json、数据库及登记媒体），且需要现有 playwright-core/Edge；只在测试库副本操作，不触及业务库。单次 Vite 构建替换上述明确测试输出目录。按本次改动影响选择套件，共用依赖及固定媒体，避免重复构建和数据库副本；清理仅针对已确认归属且不再使用的可再生输出，保留待调查的失败证据。

## 2026-10-19 再次全流程复验（最新）

源码仍为 agent/vu-skill@ef0880b 加未提交修复；本轮只扩展原测试/探针和文档，未修改产品逻辑或安装依赖。使用共享 clip_01.mp4、本机 FFmpeg、确定性 Agent 提案；不是实际外部 LLM 判断。

| 层 | 实际结果 |
|---|---|
| 工程 | 当前 Go 二进制构建通过；生产 Vite 3153 模块/11.93 秒通过 |
| 连续 HTTP/MCP | 免独立凭证接入→HTTP 导入/角色治理/可见集确认→MCP 帧/摘要/计划→HTTP 人工定稿→检查/完成通过。三版影片分别 1.0 秒（计划 1）、片段修改后 .8 秒（撤销重做后计划 4）、成片时间点修改后 .7 秒（计划 5）；每版均核对 SHA、完整 FFmpeg 解码、Premiere ZIP 内容及工作台预览 Range 206，最终原始运行 succeeded |
| 人工与历史 | Agent 无法调用人工定稿；改稿后旧片历史预览可用；HTTP 撤销/重做恢复内容但不恢复定稿或退还预算；时间点映射为 timeline .3–.7 / source .5–.9；独立取消运行拒绝后续工具；来源/名称和日志人工边界通过 |
| 生产页面 | Edge 实际读取上述数据库副本/登记 MP4，无接口拦截；8/8 通过：真实运行加载、1280 宽/.7 秒影片解码播放、时间点提示词映射、计划保存、撤销、重做、重新定稿、无页面异常。浏览器副本在定稿后 queued 等待后续 Agent；原始 MCP 流程已完成三次交付 |
| SDK/安全 | 组合复验中 MCP 包已通过（28.592 秒），随后 service 因空间门中止；拆成单包后全部 TestSkill 服务回归通过（23.102 秒），覆盖本机 ASR/模型夹具、预算、定向编辑与历史恢复。原组合命令仍记录为容量中止，不改写成通过 |

探针第一次错误地在 Agent 改稿前要求旧片标为“历史”（当时原片仍属当前已定稿），失败信息为 Prior film cannot be previewed while editing；检查移到 scripts.propose 后，要求本身未放宽，三版链路通过。未据此修改产品。

空间事件：先定点回收本轮失败临时目录和已验证的重复下载 12977782 字节（仅版本生成物口径 416517817→403540035）。最后组合回归进入 service 时，统一口径达到 528934092 字节/504.431 MiB，入口按项目规则中止并自动归档清理至 82986158 字节/79.142 MiB，释放 445947934 字节。源码/固定样本/业务数据保留；临时 JSON、数据库、截图和测试依赖随 artifacts/cache 清除了，不能再作为当前可下载证据。实际通过与中止结论保留于本页、待办及原 0.1.1 简档；未知修复提交不猜测。完整 Rust/Tauri 发行、实际 Provider/外部 LLM、Premiere 实机与人工画面质量未验。

## 首次检查与修复回归（历史范围）

2026-10-19，版本 0.1.1，源码 `ef0880b` 加本地未提交前端修复/本轮测试文档改动：原 MCP 完整包 4.225 秒通过；扩展后 MCP / service 选定回归分别 15.962 / 23.715 秒通过；加入摘要及精确时长断言后的 SDK 媒体用例 13.33 秒通过。通过标准客户端实际生成 1.000 秒 MP4/Premiere，人工指定首段后生成 0.800 秒新版；共用固定媒体、无云模型调用。Go 构建和真实进程的现代协议发现/Skill 分发、人工日志鉴权与链校验通过。

**修复前失败事实**：三个 2025 年版本 initialize 200 后 tools/list 400；源帧只有 JSON base64、0 个原生图片块；工具契约及桌面接入不完整。这些初验事实保留溯源，不作为修复后的当前结论。

**当前实现及验证**：桌面通过回环 MCP 直接连接，无需独立凭证。SDK 四版本 × Worker/桌面 8 组握手/发现/调用通过；两个模式分别完成原生图片（1 个 image/jpeg 块、12940 字节，无重复 base64 文本）、摘要、两版 MP4/Premiere 与定向修改，合计 27.14 秒；MCP/config/service/API/装配相关回归 28.866/0.162/24.419/1.230/1.085 秒通过。Go build 与新二进制真实进程探针通过，旧版本协商正确并 tools/list 200，免凭证现代 Skill 资源分发、人工日志权限及名称/来源记录通过。Rust fmt 检查通过；完整 Rust/Tauri 构建和桌面发行包尚未验证，不将 Go 服务运行等同桌面安装验收。真实外部 LLM 自主判断/剪辑、真实转录 Provider、Premiere 实机与人工画面质量仍未验收。配置/契约和历史问题修复对应见 [MCP 交接说明](../docs/Skill-Harness接口与前端交接.md#mcp-agent-接入校验2026-10-19)。

官方 SDK 实际服务诊断复用现有 [probe](../control-plane/internal/mcpserver/probe/main.go)，默认免凭证，可传 `-url <mcp_url> -protocol 2025-11-25 -client-name <name> -source <source> -calls 'skills.get:{"id":"video-use"}'`；内部 Worker 另显式指定 `-token-env`。须通过同一测试入口执行 `go run -tags probe ...`，不要在测试目录外输出二进制。

本轮保留原套件和共享样本，没有退役测试、添加临时独立脚本、安装依赖或清理历史生成物。运行证据使用原 custom 输出与 `vu-runtime-result.json`，容量仍由统一入口维护；修改测试后复验相应范围即可，不按每次任务复制新套件。

最终完整回归：MCP 30.557 秒、config 0.185 秒、API 20.690 秒通过。根包初次失败在旧 `TestServeHTTPServesEveryEndpoint/audit_log`：匿名 GET /api/audit 实际 403，旧断言要求 200；保持产品权限，仅在原用例补充匿名 403 / 人工 token 200，根包最终 14.205 秒通过。修复版本 0.1.1，提交未记录（本轮尚未提交）。既有官方 SDK probe 已编译通过，前端 tsc --noEmit 与 Rust cargo fmt -- --check 通过；未创建替代套件来绕过失败。102 文档/486 链接/144 接口及 diff --check 通过；测试占用 450.676 MiB，未清理原测试生成物。
