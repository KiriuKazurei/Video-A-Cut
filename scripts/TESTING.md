# 测试维护说明

更新：2026-11-01。本文是既有测试权威入口，主文档为[项目](../docs/项目文档.md)、[计划](../docs/计划文档.md)、[接口](../docs/接口文档.md)及[架构功能详解](../docs/架构与功能详解.md)。统一多轨编辑沿用现有工程、媒体、前端、Worker 和原生桌面套件，不删除测试源码/样本，不执行产品数据清理。

## 1. 统一规则与源码身份

版本取 desktop/tauri.conf.json，当前0.3.2。测试生成物只放 `.run-data/tests/<完整版本号>/`；固定媒体在 `workers/node/timeline-cli/fixtures/media/`。原录像、正式业务数据、签名密钥、正常开发依赖与 test-support/legacy 恢复副本不属于测试生成物。

`scripts/TestWorkspace.ps1` 统一设置 VAC_TEST_ROOT/VAC_TEST_OUTPUT、TEMP/TMP/TMPDIR、Go/npm 缓存、禁 Python 写缓存及离线 Go。支持 go/python/webui/pipeline/timeline/exporter/guard/workspace/custom。前端完整单元用 npm run test:unit，内置 webui suite 只覆盖原基础子集，不把二者计数混用。

2026-10-29 当前整合源码为 `E:/AI/Video Auto Cut/.run-data/worktrees/integrate-remote-20261029`，分支 codex/integrate-remote-20261029，以 98ba0e0 合入 origin/agent/vu-skill@8511f2e，应用保持 0.3.2。原根和其他旧工作树保留不同源码与未提交内容，不能用旧根 HEAD 或端口健康证明当前代码。custom 的 WorkingDirectory 必须明确；摘要中 runner 所在仓库的 HEAD 与实际被测源码不同时，单独记录源码提交/改动和证据。

不设固定容量上限，不按容量自动终止或清空 artifacts/cache；Measure 仅报告占用。版本变化后 Prepare 按规则提炼旧版本简档并轮换；同版本需要清空时显式 Clear，CacheOnly 保留 artifacts。每版本一份极简 Markdown，最多最近 3 版，未知修复版本/提交写未记录。

任何清理先确认相关进程退出、目标属于明确测试根、目标/父链/子项无重解析点且无 Git 跟踪文件，PowerShell 使用 LiteralPath，并报告同口径前后占用。此条针对生成物；本轮用户单独授权的源文档删除按精确文档清单处理，不把它扩展到源码/固定样本。

### 2026-10-29 远程整合核查

使用既有套件，未新增独立测试或固定样本。前端全部 128 项、tsc --noEmit 和样式检查通过；证据为整合工作树 `.run-data/tests/0.3.2/artifacts/custom/output.log`。媒体沿用 sequence/script-workbench/preview-render 三文件，28 项全部通过，包含可编辑绘制 FrameSource 的透明混合/声音、正常成片、异常黑屏静音、主动衰减、自然停顿及检查失败仍交付；证据为原根 `.run-data/tests/0.3.2/int-node/0.3.2/artifacts/custom/output.log`。两处正常开发 node_modules 链接复用 timeline-audio-viz 已安装依赖，不是测试清理目标。

Go 全部包编译及默认预设/质量结果/租约/真实媒体交付定向测试通过（go test ./... -run 指定既有用例 -count=1 -timeout=240s，含未匹配用例包的编译，不代表全量 Go 用例通过）；统一入口为原根 TestWorkspace.ps1，WorkingDirectory 明确为本整合源码 control-plane，输出为原根 `.run-data/tests/0.3.2/int-go/0.3.2/`。摘要中的旧根 HEAD 仅代表 runner 所在仓库，实际被测源码身份以本段为准。文档/契约已通过：13 份、554 链接、184 HTTP、17 IPC、158 类型、29 SSE；证据为整合工作树 `.run-data/tests/0.3.2/doc-check/0.3.2/artifacts/custom/output.log`。

更新器仅做源码及公开源核查：latest.json GET 200，清单为 0.2.3，安装器与 .sig HEAD 200；未下载更新包或验证其密码学签名，未执行当前 0.3.2 的原生打包/安装/升级/回退、真实模型或人工验收。旧 0.1.0→0.1.1 验收仅保留历史边界。初次测试入口在受限环境的 CIM 进程查询失败，测试尚未开始；获得工具提升权限后按同一入口运行。没有清理其他工作树、原工程、媒体或历史测试数据。

### 2026-10-29 0.3.2 公开 Release 与更新器下载

源码为整合分支 f004655（已推送，Release 标签一致），本轮只维护既有 updater 验收脚本及文档。Build-TauriDesktop.ps1 -Installer -Version 0.3.2 的前端/Go/Rust release 编译通过；首次在 NSIS 3.11 工具下载时报 timeout: global，复用原 0.2.3 工作树已有且版本核验为 3.11 的 NSIS 缓存后重试完整构建退出 0。保留 build/desktop/build-installer-0.3.2.log 与 build-installer-retry-0.3.2.log。

Release ID 427079350，v0.3.2 已公开并设为 latest，三附件大小为安装器 74908962、.sig 428、清单 806 字节。安装器 SHA256 为 4d85d1c401e88fa372ddf51f1c6c317a86dbfce6ae4358f09bb055c6f9a1510c；公开 latest.json 实际返回 0.3.2，安装器和 .sig HEAD 200，远程标签精确为 f004655aa980d83cee3d045d233ad862ca2f581e。发布元数据为 build/desktop/release-result-0.3.2.json；签名私钥未上传。

使用当前工作树 TestWorkspace.ps1 的 custom 入口、StorageRoot=.run-data/tests/0.3.2/public-update，执行 node scripts/verify-tauri-updater.mjs --public-download <codex-skill-harness/build/desktop/release绝对目录> 0.3.2。复制 0.2.3 正式 exe 到独立测试目录，业务库独立，更新地址没有测试覆盖；正式 HTTPS 检查得到 available_version=0.3.2，完整下载 74908962 字节，phase=downloaded、ready_to_install=true、签名验证成功。Win32 探针 visible=true，截图实际检查了版本、下载进度和“安装并重启”按钮；后台停止及自身进程退出成功，入口退出 0。证据为当前工作树 .run-data/tests/0.3.2/public-update/0.3.2/artifacts/custom/updater-public/results.json、downloaded.png 及父目录 output.log。

本轮未触发 install_update，不宣称当前版本安装替换、升级后运行或回退通过；用户业务目录和原窗口未操作。正常开发缓存/签名目录/正式安装包保留，没有测试清理。按用户追加授权，全部记录和推送完成后通过系统 PowerShell 休眠。

### 2026-10-29 本地生成物清理

用户授权的一次清理已完成：55 个明确 Rust/Go 构建缓存、中间文件和调试副本，以及原根、integrate-remote-20261029、codex-skill-harness、timeline-audio-viz 四组版本化测试输出。使用既有 TestWorkspace.ps1 的 Prepare/Clear，嵌套 int-go/int-node/doc-check/public-update 的 summary 先合并到所属版本简档后移除空壳；每个测试根的 archives 最多三份。当前简档分别为原根 .run-data/tests/archives/0.3.2.md、整合工作树 .run-data/tests/archives/0.3.2.md、codex-skill-harness 的 0.2.3.md 和 timeline-audio-viz 的 0.3.2.md。

上文生成物路径作为历史证据位置保留说明，其测试日志、截图、数据库、缓存和编译运行副本本次已清除，不能当作仍可直接打开的文件。0.3.2 公开更新下载验签的 passed 结论已保留在计划及版本简档；未执行安装的边界不变。旧版 codex-skill-harness/build/desktop/release 下的 exe/runtime 是生成物，已清除，复跑 --public-download 前须从保留的 0.2.3 签名安装器重新准备独立旧版运行目录，不能直接使用该已清空路径。

保留全部工作树、正常 node_modules、源码/固定样本/test-support 恢复副本、原根 .run-data/development、accepted-projects、accepted-fixtures、签名密钥、build/desktop/app、updates 和 release/bundle 正式包。历史 .run-data 中的脚本/备份及无法确定用途的小目录保留，没有按扩展名扫描全仓删除。0.3.2 正式安装器 SHA256 清理后仍为 4d85d1c401e88fa372ddf51f1c6c317a86dbfce6ae4358f09bb055c6f9a1510c。

同一统计范围（原根 .run-data 非工作树子项、build，以及各工作树 build/.run-data/webui/dist）文件逻辑大小 20060982605 → 3312907797 字节，减少 16748074808 字节；磁盘可用空间 2635952128 → 16970043392 字节。逻辑大小与实际磁盘释放因文件系统分配/共享文件不同而不混用。只做文件和 Git 状态核验，未重新测试或构建，避免刚清理后再产生缓存；本次文档更新未提交/推送。

### 2026-10-29 统一诊断与扩展保留接口

实际源码为 codex/integrate-remote-20261029@76a572a 加本轮未提交改动。复用 api_test.go、skill_test.go、skill_api_test.mjs 和 settings_smoke.mjs；新增 service/diagnostics_test.go 只承担诊断版本语义的长期覆盖，仍属于现有 service 套件，没有新增独立runner或固定媒体。初始两个API用例因404失败，新增路由后通过；新增真实本机探测发现 n7.1.3 被截成1.3，版本用例复现失败后改为只读工具首行版本，避免误取编译器/lib版本。诊断用例覆盖无依赖、脱敏、不写业务/审计、配置模型零请求、健康心跳聚合、取消请求和下载响应；在线Skill能力 disabled/reserved，三POST固定501且不改注册表。

统一入口的完整 go test ./... -count=1 -timeout=600s 在18包通过（service 562.330s）；日志为原根 .run-data/tests/0.3.2/diag-red/0.3.2/full-go-output.log。之后仅修正新增版本解析并增加本机实际观察用例，最终 go test ./... -run 'TestSystemDiagnostics|TestOnlineSkillInterfaces|TestDiagnosticVersion' -count=1 -v -timeout=120s 全包编译和5个匹配用例通过；当前 output.log 记录 Node24.22.1/Python3.14.9/FFmpeg及FFprobe7.1.3，未配置Worker/模型明确warning/not_checked。red-output.log、version-red-output.log 与 local-probe-before-fix.log 保留先前差异。原根runner的HEAD不代表实际被测源码，WorkingDirectory已明确指向本整合工作树。

前端正式 npm run test:unit 最终65+64=129项通过，tsc、样式和生产Vite构建通过；使用系统PowerShell加载profile后的npm启动器。直接调用系统Node目录下另一个npm-cli出现用例前退出1（日志只有store-dir提示及exit1，没有业务断言失败），同版本测试输出下保留npm日志，不修改全局npm配置。最终生产构建及前端日志为当前工作树 .run-data/tests/0.3.2/diag-ui-fix/0.3.2/artifacts/custom/。

Chrome既有设置套件最终10组通过、pageerror为空：手动启动才查询、检查中状态、脱敏JSON下载、失败禁用下载与成功恢复、模型未验证标识、窄屏无横向溢出、既有草稿/保存重试/导航/减少动画/TTS。错误恢复需等下载重新可用和错误提示消失，不能仅读取上次快照。最初在减少动画和窄屏长页返回设置分类时出现body弹层负坐标；仅延长探针等待未解决。旧构建+原脚本8组通过、新构建+原脚本复现；将分类弹层挂在自身容器并对该高频选择关闭弹层动画后，恢复原探针，最终全套通过。此修复只影响设置分类选择器。证据为当前工作树 .run-data/tests/0.3.2/diag-browser/0.3.2/artifacts/custom/review-smokes/settings_smoke.mjs/ 中的 results.json、diagnostics.json、diagnostics-1440.png 和 diagnostics-800.png；截图已检查。first-failure.txt 和两个基线输出目录保留失败/对照范围。

接口/文档检查：190 HTTP、17 IPC、163模型、29 SSE，15份文档本地链接与契约检查通过。新增两个开发文档只读交接；FCPXML/Plugin实现、依赖安装作业、AI图像编辑作业均未实现或调用。未运行原生Tauri/安装器、未重新发布、未调用真实外部模型或执行人工验收；本轮测试会重新产生受管缓存，清理记录仅描述此前时点。

### 2026-10-30 脚本工作区与 Agent/MCP 入口（UX-AGENT01）

实际源码 codex/integrate-remote-20261029@a336f4f 加本轮未提交改动，版本0.3.2。复用 desktop_test.mjs、script_workbench_test.mjs、draw_page_test.mjs、script_workbench_smoke.mjs、settings_smoke.mjs 和 run-local-smokes.mjs；新增 ScriptAgentHandoff/ScriptWorkspace 是产品组件，没有新增独立测试套件、媒体或依赖。脚本浏览器入口从旧自建服务器迁到共享隔离服务器，随新工作区更新导航、出稿、人工定稿、草稿/冲突、取消、外发授权、高亮滚动与离线不补发覆盖。固定媒体未变，未清理测试输出或业务数据。

统一入口 TestWorkspace.ps1 的 custom 结果：

- 前端最终 `npm run check && npm run test:unit` 退出0，66+66=132项通过，样式通过；证据在本工作树 `.run-data/tests/agent-ux-final-unit/0.3.2/artifacts/custom/output.log`。受管Vite生产构建在 `agent-ux-build/0.3.2/artifacts/custom/ui-dist`，退出0；既有Ant Design use-client忽略警告和外部outDir提示保留，不称无警告。npm提示全局store-dir与可升级版本，本轮未修改npm配置或依赖。
- Chrome现有浏览器入口：脚本最终9组、设置最终11组及编辑器14组通过，pageerror为空。证据分别为本工作树 `.run-data/tests/agent-ux-script-final-fix/0.3.2/artifacts/custom/review-smokes/` 与 `.run-data/tests/agent-ux-mcp-final/0.3.2/artifacts/custom/review-smokes/`。1440/800脚本与MCP截图已查看，六项顶栏保持600px，原设置覆盖至640px无横向溢出。桌面MCP使用注入IPC桥夹具验证准确复制、停止卸载、重启端口更新和复制失败；未执行真正外部客户端安装或连接。
- 原根短路径runner执行当前源码control-plane：`go test ./internal/service -run 'Test(Project)?ScriptWorkbenchRealWorkerRoundTrip$' -count=1 -timeout=240s -v`，两项通过（10.22s/7.69s），真实Go→Python/FFmpeg→本机HTTP模型桩→Go→Node/FFmpeg，覆盖旧资产与当前工程。证据为原根 `.run-data/tests/agent-ux-go/0.3.2/artifacts/custom/output.log`，runner的旧根HEAD不代表被测源码。没有重跑完整Go、Python或Timeline套件。
- 接口190 HTTP/17 IPC/163模型/29 SSE，15份文档599链接及脚本工作台6文档568本地链接检查通过；证据为本工作树 `.run-data/tests/agent-ux-doc/` 与 `agent-ux-doc-contract/` 的0.3.2输出。

失败与修复边界：初次受限入口CIM无权限，提升后才实际运行。新增路由与Agent任务用例修复前失败；六项导航用实际opacity/滑轨边界断言复现export被隐藏，修正六等分后通过（agent-ux-route-red/handoff-red/nav-red保留）。外发探针最初提前读到旧旁白，改等本次修订；MCP探针原以为停止后设置仍挂载，按桌面整页卸载契约修正。扩展桌面桥夹具暴露共享静态测试服务器先写200再读缺失文件、继而重复写404导致headers-sent，已先读文件再写头并复验。离线探针原被route响应夹具绕过断网，改为断网期间放行到浏览器真实网络后验证不补发。前述失败日志保留在agent-ux-browser-final、agent-ux-mcp-fix和agent-ux-script-final等同版本目录，不记成产品模型质量通过。

本轮未验真实外部模型质量、原生Tauri剪贴板/安装器或人工体验，没有提交/推送/发布。官方Codex安装命令只读核对官方文档及本机CLI帮助，未执行安装、改Agent配置或发送任务给外部Agent。ui-ux-pro-max本机缺搜索脚本，沿用项目既有深色主题、固定顶栏与技能通用可访问性清单，没有声称取得数据库搜索结果。
### 2026-10-30 外部 Agent 状态 hook（AGENT-HOOK01）

源码仍为 codex/integrate-remote-20261029@a336f4f 加本轮及前轮未提交改动，版本0.3.2。新增service/agent_status_test.go承担连接观测状态机的长期职责，仍属于现有service套件；API/MCP/前端/SSE/浏览器覆盖维护在既有api_test.go、security_test.go、desktop_test.mjs、skill_api_test.mjs、test_suite.mjs和settings_smoke.mjs。没有新增独立runner、媒体或依赖，没有清理业务/测试文件。原刷新域断言随新增external-agent-status域更新，不与旧契约并行保留。

- 前端最终 `npm run check && npm run test:unit` 68+66=134项和样式通过。证据：本工作树 `.run-data/tests/agent-hook-ui-fix/0.3.2/artifacts/custom/output.log`。最终Vite构建退出0，产物在 `agent-hook-build/0.3.2/artifacts/custom/ui-dist`；保留既有Ant Design use-client忽略及outDir提示，没有声称无警告。
- 现有Chrome浏览器入口：设置12组、脚本9组通过，pageerror为空。状态用本机HTTP夹具验证无连接、最近活动、工具调用中、90秒静默待确认、读取失败隐藏旧状态、主动断开、5秒自动刷新和离开设置分类停止轮询；原MCP复制/停止/重启/失败及脚本离线边界仍通过。证据：本工作树 `.run-data/tests/agent-hook-browser/0.3.2/artifacts/custom/review-smokes/`。agent-status-connected.png / agent-status-unavailable.png与既有1440/800截图用于视觉复核。桌面桥为注入夹具，不算原生Tauri验收。
- 原根短路径受管runner、WorkingDirectory明确为当前源码control-plane：`go test ./internal/service -run TestExternalAgentStatus -count=1 -timeout=120s -v` 两项通过；随后 `go test ./internal/api ./internal/mcpserver -count=1 -timeout=240s` 两个完整包通过（31.514s、139.980s），包括真实HTTP/SSE与现有SDK协议。新覆盖检查并发完成/重复完成、256项边界、静默/过期/断开、迟到回包不复活会话、实际工具在途和失败、初始化/DELETE、会话header/参数不进入快照，以及事件不改变AVP revision/dirty。证据：原根 `.run-data/tests/agent-hook-go-fix/0.3.2/artifacts/custom/output.log`，原根HEAD仅为runner身份，不代表实际源码。
- 当前接口191 HTTP/17 IPC/165模型/30 SSE，15文档602链接检查通过；机器清单补录AGENT-STATUS-01及状态类型/SDK。文档入口为本工作树 `.run-data/tests/agent-hook-doc/0.3.2/`。

红灯及修复保留：agent-hook-red记录缺少service hook和GET404；agent-hook-ui-red记录前端缺少状态模块；agent-hook-green记录初次观测身份与原审计identity变量重名导致编译失败，改独立变量后通过。agent-hook-ui记录两个既有刷新域快照仍使用41个域而失败，新增领域后更新原断言，最终134通过。会话过期追加红灯保留在原根agent-hook-go-fix的expiry-red.log，修复后与最终整包共用该目录缓存；早期定向通过保留targeted-green.log。不把初次失败或夹具状态当真实客户端/模型质量验收。

本轮没有运行完整service/Go全仓、真实外部模型、原生安装器或用户客户端手动操作，没有提交/推送/发布。状态只证明服务端观测的MCP活动；不会判断客户端思考/审批或扫描外部进程。记录仅在进程内存在，连接静默显示待确认而非断言离线。
### 2026-10-30 Agent状态刷新周期（AGENT-REFRESH03）

后续用户要求将自动读取由5秒改为3分钟，替代AGENT-HOOK01最初频率。维护原skill_api_test.mjs和settings_smoke.mjs：事件只invalidate且refetchType=none，避免内部Worker的MCP事件仍导致频繁拉取；窗口聚焦不额外刷新，手动刷新立即读取。设置浏览器使用Playwright既有虚拟时钟验证179秒无提前读取、再推进1秒触发一次刷新，以及离开分类后推进180秒仍不读取，无需真实等待3分钟。

当前整合工作树未提交源码、0.3.2，经现有受管入口：npm run check、完整68+66=134项及样式/生产构建通过；证据在本工作树 `.run-data/tests/agent-refresh03/0.3.2/artifacts/custom/output.log` 和 ui-dist。浏览器settings12组通过、pageerror为空；证据 `agent-refresh03-browser/0.3.2/artifacts/custom/review-smokes/`。旧实现用新时钟断言复现179秒内3次读取而失败，红灯在agent-refresh03-browser-red对应output/log；事件断言也取得缺少refetchType的红灯，修复后纳入完整套件。沿用已有runner/夹具，没有新增独立套件、依赖或媒体。

当前用户Tauri dev保持运行，Vite日志记录hook/queryRefresh更新，UI文案同步3分钟频率；未重启后端、改MCP授权或重新发布。本次不重跑Go/API/MCP，因为未修改后端。此前5秒验收仅代表当时历史源码。
## 2. 常用工程检查

Windows 使用系统 PowerShell 7、UTF-8 和已安装依赖，保持 profile 设置。以下从实际源码根执行：

```powershell
$sourceRoot = (Get-Location).Path
$runner = Join-Path $sourceRoot 'scripts/TestWorkspace.ps1'
& $runner -Action Run -Suite python
& $runner -Action Run -Suite timeline
& $runner -Action Run -Suite exporter
$npmCLI = Join-Path (Split-Path (Get-Command node).Source) 'node_modules/npm/bin/npm-cli.js'
& $runner -Action Run -Suite custom -Executable node -WorkingDirectory "$sourceRoot/webui" -Arguments @($npmCLI,'run','test:unit')
& $runner -Action Run -Suite custom -Executable node -WorkingDirectory "$sourceRoot/webui" -Arguments @('node_modules/typescript/bin/tsc','--noEmit')
& $runner -Action Run -Suite custom -Executable go -WorkingDirectory "$sourceRoot/control-plane" -Arguments @('vet','./...')
& $runner -Action Run -Suite custom -Executable cargo -WorkingDirectory "$sourceRoot/desktop" -Arguments @('fmt','--','--check')
```

一般路径可使用 -Suite go。当前嵌套工作树的 Go 全包使用原根的短版本化输出，避免 Windows 测试目录/FFmpeg/Node 路径过长，源码仍明确指向当前工作树：

```powershell
$managerRoot = 'E:/AI/Video Auto Cut'
$sourceRoot = "$managerRoot/.run-data/worktrees/timeline-audio-viz"
& "$managerRoot/scripts/TestWorkspace.ps1" -Action Run -Suite custom -Version '0.3.2' -Executable go -WorkingDirectory "$sourceRoot/control-plane" -Arguments @('test','./...','-p=1','-vet=off','-count=1','-timeout=600s')
```

Go vet 单独执行，不跳过原失败用例。按变更选择现有包/命名用例；不要在相同结果通过后无理由重复全包。并发入口须避免共享测试根、端口、数据库和管理锁。正常失败保留退出码、摘要与日志。

## 3. 文档与接口契约

接口文档自动区域来自机器清单与真实 TypeScript AST。维护契约后显式生成，再运行现有检查：

```powershell
node scripts/update-interface-documentation.mjs --refresh-inventory --write
& $runner -Action Run -Suite custom -Executable node -WorkingDirectory $sourceRoot -Arguments @('scripts/update-interface-documentation.mjs','--check')
& $runner -Action Run -Suite custom -Executable python -WorkingDirectory $sourceRoot -Arguments @('-B','scripts/check-documentation.py','--root',$sourceRoot)
& $runner -Action Run -Suite custom -Executable python -WorkingDirectory $sourceRoot -Arguments @('-B','scripts/verify-script-workbench-docs.py')
```

源 SHA 按 LF 规范化计算，兼容 Git autocrlf 而仍检测实际内容变动。刷新只定位真实路由、处理器、SDK 和页面调用，不自动推断参数语义。检查精确路由集合、源 SHA、处理器位置、17 项桌面 IPC、HTTP 请求/响应索引、类型/SDK 自动区域及本地链接。文档迁移更新原检查对象，不退役 Go/前端/Worker 覆盖。运行时 Skill 文档若变更，同步嵌入/目录副本和版本，并执行相应 MCP/服务契约回归。

## 4. 媒体、MCP、浏览器及可见桌面入口

| 入口 | 目的与前置 |
|---|---|
| Go service/API/MCP 现有包 | 工程往返/隔离、CAS/幂等、Worker 边界、预算/历史、真实 FFmpeg 和本机模型夹具 |
| `workers/node/timeline-cli/test/` | 预览/ASS/脚本真实渲染、哈希和跨语言输入；复用固定 clip_01.mp4 |
| `control-plane/internal/project/timeline_test.go`、`service/project_timeline_test.go` | 多序列 v1/v2、整数帧/源采样、链接/锁定/波纹、CAS/幂等/整体撤销、重名引用与改名事务；扩展既有 service Skill 用例检查异步完成归属和撤销后重开去重 |
| `workers/node/timeline-cli/test/sequence.test.mjs` | 共用节目/导出合成，真实源流、保持音调、混音/波形、变换、效果作用范围、溶解素材余量及字幕；合成输入由本机 FFmpeg 确定性生成 |
| `workers/node/export-worker/test/` | owned task、取消/租约、输入/回执、摘要负向、恢复和发布 |
| `workers/python/tests/` | 模型协议/重定向、声音、脚本、导入隔离与恢复；默认不下载模型 |
| `scripts/verify-vu-runtime.ps1` | 现有真实 Go/Worker/MCP 连续流程；先按当前源码重建程序及匹配 runtime 清单 |
| `control-plane/internal/mcpserver/probe/main.go` | 官方 SDK 真实发现/调用；通过 managed custom 执行 go run -tags probe，内部 Worker 才显式给 token-env |
| `scripts/verify-skill-ui-completion.mjs --real-flow` | 基于运行探针产出的受管数据库副本/媒体和生产前端检查交互，夹具与真实后端范围分别记录 |
| `scripts/verify-tauri-desktop.mjs` | 可见 Tauri/WebView、原生文件框/菜单/剪贴板、媒体/脚本、关闭/保存、切换/重启；自己在管理器内部启动并退出 |
| `scripts/verify-tauri-updater.mjs --public-download <0.2.3-release目录> 0.3.2` | 通过统一 custom 入口运行；复制旧版桌面到独立测试目录，用正式 HTTPS 更新源检查/下载/验签并截取可安装状态，不执行安装；已有用户桌面实例时拒绝启动，停止并退出自身测试进程 |
| `scripts/Test-TauriStateGuard.ps1` / `test-support/test-workspace.ps1` | 现有运行/清理/版本轮换守卫；通过对应 suite，不按本轮再复制独立测试 |

生产 Vite 测试构建的 outDir 显式指向 VAC_TEST_OUTPUT 管理目录。浏览器使用已有 Playwright/本地Chrome，禁止Edge，不重新下载浏览器。没有真实窗口证据时，不把 CDP 端口、进程 Responding 或页面托管记为可见桌面通过。

可见桌面既有调用形状如下；exe、生产前端、Go runtime 清单、desktop.json 和独立测试业务数据须先配套准备，不能把本机历史生成物当作 fresh checkout 自带资源：

```powershell
$env:VAC_DESKTOP_EXECUTABLE = "$managerRoot/.run-data/tests/0.2.4/cache/cargo/debug/video-auto-cut-desktop.exe"
& $runner -Action Run -Suite custom -Executable node -WorkingDirectory $sourceRoot -Arguments @('scripts/verify-tauri-desktop.mjs','9223','launch',"$sourceRoot/.run-data/tests/0.2.4/artifacts/custom/desktop-native-ui","$sourceRoot/.run-data/tests/0.2.4/artifacts/custom/desktop-native/desktop.json")
```

Windows Tauri production overlay 的 frontendDist 使用相对 desktop 目录的路径，避免 E: 被当 URL；测试数据根独立于源码资源根。测试应自己在管理器内部启动和退出，不预启窗口绕过在用进程守卫。正式打包仍使用 Build-TauriDesktop.ps1，不以测试 overlay 发行。

统一编辑器的可见桌面检查已归入同一 `verify-tauri-desktop.mjs`：通过所属进程核验的 Win32 鼠标/键盘执行拖入、取消、移动、右键和快捷键，CDP 只读取坐标和状态；同时验证中文重名补全、绑定改名、实际 MCP 结构化编辑、节目/原生导出同哈希、紧凑布局和多序列保存重开。六类资源及工程隔离的原断言继续执行，旧双入口界面断言随产品更新。`VAC_DESKTOP_KEEP_FAILURE=1` 仅用于失败后保留窗口诊断，失败仍以非零状态退出。

本轮维护前端 `script_workbench_test.mjs` 的稳定绑定/UTF-16/吸附/链接断言，更新 `test_suite.mjs` 的 SSE 查询领域；Python 在既有 `test_skill_runtime.py` 内检查序列、引用和工具契约传给模型。AI 画面修改接口默认 501，未调用服务商；确定性媒体检查不代表外部模型质量或人工观感通过。

`webui/run-local-smokes.mjs` 继续服务生产构建，`review_shortcuts_smoke.mjs` 已更新为统一编辑器的 14 组路由夹具检查：链接选择、V/C、CAS 分割、输入/模态/隐藏页避让、标记/逐帧、右键、复制粘贴和序列历史。写请求只记录到本机夹具，不当成真实 Go 编辑验证。已不挂载的 quickstart 卡片检查退出当前默认入口，原测试源码保留溯源；当前任务启动由 Skill 前端/服务用例及原生窗口覆盖。旧 CR03 14/14 是旧候选结果，当前 14 组需另记实际结果。

本地 ASR 的默认单元/协议测试不下载模型；真实识别复用 service 现有 `TestProjectLocalTranscriptionRealRuntime`。仅在显式设置 VAC_ASR_TEST_MODEL（完整本地模型绝对目录）与 VAC_ASR_TEST_PYTHON（已安装 faster-whisper 的 Python）时执行，可选 VAC_ASR_TEST_AUDIO。未给音频时用 Windows 本机 SAPI 在 t.TempDir 生成语音；模型、依赖及原媒体不修改。用同一环境执行上面的 Go 全包，覆盖环境诊断、实际词时标、幂等重放、取消和跨工程拒绝；英文合成语音不能代替中文/长录像质量验收。桌面脚本同样读取这两个环境变量以检查原生选择、依赖诊断与预设持久化。

## 5. 最近适用结果

### 2026-10-25 DRAW02 图片素材（自动验收完成）

工程图片以稳定资源 ID 加入 DrawComposition.images；多个实例独立保存位置/尺寸、线性位移及帧区间、适配、透明度和自动/源码模式。当前通过：两引擎各自的自动层及源码引用共 4 段真实图片视频、位移像素检查；MCP 实际图片引用→绘制→入轨→分割/移动→35 帧导出；工程图片归属、移除占位、原件变化拒绝旧渲染、重新关联及 AVP 往返。完整 project/skill/api/mcpserver 包和 service 工程/绘制测试均通过；前端、类型/构建和图片浏览器操作已通过。最终完整 Timeline 74/74、Python 165/165、前端 124、类型/构建、原编辑器浏览器 14 组及图片绘制浏览器 8 组通过。补充同图多个实例和尺寸边界检查：移动实例改变位置，静态实例保持原处，图片以外像素保持背景；两引擎均通过。原生窗口与人工观感未验。

HyperFrames 自动层首轮只有图片、没有按帧移动，像素断言失败；原因是 DOMContentLoaded 后才追加动画，晚于引擎时间轴捕获。改为源脚本之后同步注册后，两引擎的 4 条图片路径全部通过，没有降低位移断言。Image adapter 缓存标记更新为 2，拒绝复用首轮旧像素结果。Skill 包更新为 draw 0.3.0 / video-use 0.1.0-vac.7；应用仍 0.2.4。

证据复用既有版本目录：源码工作树 custom/draw/images-{hyperframes,remotion}/{layer,source}/verification.json 及 first/later.png；原根短路径 custom/draw-images-packages.log、draw-images-service.log 和 draw-mcp-verification.json；浏览器沿用 run-local-smokes，记录图片点击/真实 HTML 拖入、同原件多实例与项目归属、参数保存、移除及底栏边界。图片原件保持不变。


### 2026-10-25 绘制执行增量（基础链路自动回归完成）

以远程 a23d138 的独立工作树为实际源码，版本沿用 0.2.4。当前通过：前端完整单元 64+59=123、类型/样式/生产构建；Timeline 全套 72/72（含两引擎真实渲染和实际像素运动、网络请求/弹窗拦截）；Exporter 53/53；浏览器绘制 7 项、原编辑器快捷键 14 组，合计 21 组且 errors=[]；文档校验通过（178 HTTP、17 IPC、147 类型、27 SSE）。Python 全包 165/165 通过。Go 共 18 个有测试包完成验证：17 包首轮通过；service 首轮 TestAutoConfirmSceneRequiresStageAndAwaitingReviewStatus/stage_is_scene_review_but_status_is_failed 在 snapshot rename 时返回 Access is denied，随后单项连续 3 次通过（13.292s）及 service 整包复验通过（508.170s）。组合回归已通过，首轮失败仍保留，不改生产快照发布逻辑，不推断根因已修复。原根短版本目录保留 go-full-first.log、go-snapshot-repeat.log、go-service-final.log。

真实 MCP 测试不是渲染桩：走 HTTP tools/call、空工程创建、读取 Skill 合同、源码保存、真实 Remotion 渲染、原生图片、独立 Agent 轨组入轨、15 帧分割、后半段移到第 20 帧、共享导出和 AVP 保存重开。测试断言导出为 320×180、35 帧，并读取像素确认移动留下的 5 帧黑场。证据在原根短路径 .run-data/tests/0.2.4/artifacts/custom/draw-mcp-verification.json 和 draw-edited.mp4；源码工作树同版本目录保留两引擎样例、browser fixture 结果、前端及 Timeline/Exporter 日志。浏览器 UI 使用媒体 Range 夹具，真实渲染与剪辑由 Node/MCP 层单独证明。原生 Tauri、安装包、外部模型质量和人工观感本轮未验，不继承旧候选的 39 项结论。

失败及修正（包含夹具和产品问题，分别保留证据）：最初 Remotion 检查遗漏未使用的禁止模块，已保留 imports 后审计；Windows preload 使用 file URL；空工程初始化同步资源指纹；HyperFrames 的 GUI Edge --version 超时改为本机已有 chrome-headless-shell；旧刷新领域断言补齐绘制缓存；浏览器夹具补齐视频 Range 和音量谱响应。未放宽帧数/像素/隔离断言，未用自动浏览器下载或云回退规避。截图复核另发现 1280×720 绘制时间轴受父容器高度影响被底栏裁切，已约束活动页面的 flex 高度并收缩预览；浏览器现断言帧控件完整位于底栏之上。源码区保留水平滚动，不把代码强行折成窄列。

本机系统 npm 11.19.1 内部 minipass-flush 继承类错误，使用官方 npm 11.6.2 的隔离 CLI（注册表 tarball SHA512 已核验），仅为本轮安装已锁定依赖及运行既有脚本，不改全局 npm 配置。固定 HyperFrames 0.8.193、Remotion 4.0.538、React 19.2.0、GSAP 3.13.0；直接使用的 esbuild/acorn/puppeteer-core 同样锁定。缺少浏览器时明确失败，不静默下载。

为避免 Windows 嵌套临时路径，Go 全包输出用原根的短版本目录。已核验旧 0.2.2 标记、Git 跟踪、进程及父链/子项重解析边界，按既有入口 Prepare 轮换。原根测试目录同口径在轮换前 207,527,606 字节、轮换后 14,797 字节，释放 207,512,809 字节（随后新测试会新增缓存）。原始录像、用户 AVP、业务库、旧恢复运行目录、固定样本、未提交源码及正常依赖保留；原 AVP SHA256 仍为 981E72F51760EC5B0D28C722C32DC9FA722D2040F821C3D24B9CD2B544F38822。


2026-10-28 REL023：当前工作区已按版本轮换 0.2.2 生成物，仅保留 archives/0.2.2.md；下文 0.2.2 输出路径为历史溯源，可能已不再存在。0.2.3 Timeline 全套 69 项、前端 96 项及类型检查、MCP 全包（37.918s）、Cargo 离线锁定元数据、版本一致性与文档契约通过；未重跑完整 Go、原生窗口或外部模型。未对原根及其他工作区执行清理。

2026-10-28 QC01（0.2.2 / agent/vu-skill@3ec5909 加未提交改动）：现有 Timeline 全套 69 项通过，含真实 FFmpeg 成片、正常声画、异常黑屏/静音及时间段、短音轨、无音轨/静音轨、自然停顿、重叠、变速、配音偏移、黑场淡变、空白区间、检测失败仍发布。短音轨结束误报和主动降低音量误报均先复现、修复后复验通过。Go 服务/Skill/MCP 定向通过，含真实 MP4 哈希绑定与黑屏警告后 review/finish 成功；未重跑完整 Go。前端 96 项、样式和类型检查通过，Vite 生产构建通过但报告依赖的 use client 忽略提示；首轮 npm 启动器在用例前退出 1，保留其 npm 日志，直接运行 package.json 同一 Node 入口后通过。检查结果是工程信号；未运行真实桌面窗口交互、用户业务工程、人工观看/听感或外部模型。新增检查的 30 秒超时、旧预览音轨探测 10 秒超时会返回未完成而非通过；未测试超长视频性能。

2026-10-28 CFG01（0.2.2 / agent/vu-skill@3ec5909 加未提交默认配置改动）：复用 control-plane/main_test.go 的启动集成测试，新增 2 项覆盖空库默认项/固定模板快照、重启保留用户修订与归档/自定义项、初始化失败事务回滚和重试。定向命令为统一入口 custom + go -C control-plane test -count=1 -run "TestAssemble(DefaultSeed|SeedsBoundDefaults)" .，已通过。最初缺少默认数据的预期失败、一次测试误以为预设列表过滤归档项的失败均已定位；现断言直接检查归档行，未改变列表契约。完整 Go 套件（嵌套工作树入口 -Suite go）16 个有测试包通过，service 包 1 项失败：TestAutoConfirmSceneRefusesWhatItCannotJustify 在快照目录 rename 时 Access is denied（526.494s，B04 同类现象），不能记全绿。原始结果保留于当前工作树 .run-data/tests/0.2.2/artifacts/go/output.log；随后按本说明原根短输出入口，以 go test ./internal/service -count=3 -run ^TestAutoConfirmSceneRefusesWhatItCannotJustify$ -timeout=120s 定向复跑，3 次通过（10.994s，日志在原根 .run-data/tests/0.2.2/artifacts/custom/output.log）。这不能覆盖先前完整套件失败，也未确认 B04 根因。文档检查 10 份/507 链接、171 HTTP/17 IPC/135 类型/27 SSE 通过；首次把说明写入自动生成区导致契约检查失败，移到人工维护区后复验通过。没有新增套件/固定媒体或删除旧覆盖；未执行桌面窗口、实际 SAPI 合成、云端模型或人工听感验收。

轮换同口径仅统计两个已删除的 0.2.1 目录：原根 7,214,871,405 → 0 字节，当前工作树 143,934,676 → 0 字节；合计旧目录逻辑占用减少 7,358,806,081 字节（约 6.85 GiB），不包含新 0.2.2 输出、简档和业务数据，不当作整个磁盘的净释放。

0.2.2 升版检查已通过：Tauri、Cargo/npm 锁文件及 MCP 版本一致；前端 96、类型/样式/生产构建、MCP 整包 34.330s、Cargo 离线锁定元数据、171 HTTP/17 IPC/135 类型/27 SSE 和本地文档链接检查通过。首轮 npm 启动器在执行用例前退出 1，保留 version-npm-launch-first.log；直接执行 package.json 同一组 Node 入口后通过，不修改全局 npm 配置。0.2.2 未重跑原生桌面和外部模型，以下 0.2.1 完整回归为历史基线。2026-10-22 已按管理器 Prepare 轮换两处 0.2.1 生成物，只保留各测试根 archives/0.2.1.md 的简档；以下旧日志和截图路径仅作历史溯源，已随轮换退出，不保证仍存在。原工程、录像、开发数据库及 live-dev/timeline-runtime-0.2.1 不在清理范围。

本轮用户协同测试的指定区间剪辑已验收（2026-10-21）：0.2.1 可见窗口与保留的开发数据库，真实官方 MCP SDK 常驻会话（Codex-cooperative-test，协议 2026-07-28）。12 项工具发现、工程/资源/可见集查询、video-use 包和契约读取成功。用户已导入指定 87.144 秒录像，当前工程 prj_5001495a34fa0b05 仅有资源 res_0ff95a97d525a902；可见设置页本地预设环境检查/保存和 MCP 创建/认领/实际转录已成功（96 个源时标词项，CPU int8）；低置信度与不通顺文本的模型质量未验收。12 张源帧已经原生 MCP 图像逐张读取。用户改为按实际唱出的语言删除英文演唱；当前整段 ASR 漏识歌词且没有分段语言标签，MCP 又未提供范围/音轨控制，不能以画面英文或转录空白代替英文演唱起止证据。自动识别英文唱段的质量测试尚未通过；用户已明确仅删除源时间 41–61 秒，保留其余 67.144 秒；MCP 两段草案已由用户在窗口定稿，运行 v39 / 计划 v1 的 human:webui 定稿及 0–41、61–87.144 秒区间经 MCP 核对一致，MCP 渲染、检查、4 张原生检查帧读取、review 和 finish 已成功，运行 v55 / succeeded；实测 67.133333 秒、1280×720 H.264、同计划 MP4/Premiere ZIP，画面检查通过；可见 WebView 播放从 39.5 秒跨接点到 41.862 秒，音视频均解码，82 帧/0 丢帧、无媒体错误，已暂停在 39.5 秒供用户试听。窗口布局发现 PREV01：视频高 584.625px、容器高 298.792px 并隐藏溢出，画面及控件被裁切；当前 DOM 临时指定 Grid 轨道后两者高度一致，完整画面和控件恢复，源码未修复，不记原始布局通过（exact-cut-preview-layout.json / exact-cut-player-visible.png / exact-cut-player-expanded.png）。2026-10-21 用户明确确认“这次剪辑符合预期”，本次人工指定区间的剪辑与拼接效果验收通过，范围不含自动听辨及未经修复的原始播放器布局（exact-cut-status.json / exact-cut-inspection.json / inspection-images-index.json / exact-cut-finished.json / exact-cut-player.json / exact-cut-player-expanded.png）；人工标记后的成片结果与自动听辨能力分别记录，具体候选与缺口见 english-vocal-observation.json。可见「导入与粗剪」错误提示该视频为 EDL 包，保留为观察项，未记通过。证据在当前版本 custom/cooperative/session.json、projects-current.json、resources-current.json、visible-current.json、video-use-current.json、video-use-contracts.json；启动器通过现有 TestWorkspace custom 执行，常驻期间管理器结果待结束，不记为完整测试通过。工程/业务数据使用原根 .run-data/development/live-dev/data，原录像只读，连接客户端是本轮临时工具，不新增独立测试套件。

历史 0.2.1 已推送产品基线为 `cadd51a61eaf785bb9e05f8de520cf3b921ab355`（0.2.1），后续交付回执只改文档/机器清单。2026-10-21 CHROME01 为本次提交的标题栏与底栏修改；以下结果来自提交前的对应工作副本，不将基线测试冒充为新代码通过。

| 范围/候选 | 真实结果 | 证据与限制 |
|---|---|---|
| 0.2.4 `agent/timeline-audio-viz` 升版工作副本 | 版本/锁文件与 MCP Implementation 元数据对齐；本地 webui tsc/build、相关单元与样式检查通过 | 本机工作树；未重跑完整 Go/Python/媒体/原生桌面或外部模型验收 |
| 0.2.2 提交前工作副本（代码提交 c5fccf4） | 版本/锁定元数据、前端 96、类型/样式/构建、MCP 整包及文档契约通过 | 原根 0.2.2 custom/version-web-unit-{a,b}.log、version-web-build.log、version-mcp.log、output.log；源码为当前工作树。没有重新记入完整 Go/Python/媒体/原生或人工验收 |
| 0.2.1 多轨工作副本，26e17fd + 未提交改动 | 完整 Go 17 个有测试包 + 1 个无测试包、vet 通过，service 399.261s；格式规范化后 MCP 整包再通过 31.722s | 原根短输出 custom/timeline-go-full-pass.log、timeline-mcp-final.log；AVP/AVS v2、真实完成入轨/去重/撤销重开、项目隔离与 Worker 边界纳入既有用例。未开启可选真实 ASR，不沿用历史模型质量 |
| 当前多轨前端/浏览器/桌面构建 | 前端 96（46+50）、样式、TS/生产构建、浏览器 14 组、Rust 5/fmt/build 通过 | custom/timeline-web-final.log、timeline-build-final.log、timeline-rust-final.log、timeline-ui-final-pass.log、review-smokes/review_shortcuts_smoke.mjs.log；浏览器为生产界面配本机路由夹具，真实窗口另记 |
| 当前 Python/Node 媒体 | Python 165/165、Timeline 58/58、Exporter 53/53 通过 | custom/timeline-python-final.log、timeline-node-final.log、timeline-exporter-final.log；真实多流、880Hz 变速保持音调、范围外音量、变换、真溶解/黑白淡变、字幕和波形测量见 custom/sequence/verification.json 与 effects-verification.json；不代表人工观感 |
| 当前多轨原生桌面 | 最终 39/39 通过；真实拖入/移动/右键/快捷键、中文绑定改名、MCP 紫色编辑与效果、Space、同哈希导出、多序列、非零播放头重开/刷新及原有六类/代理/隔离/关闭重启均通过 | 当前工作树 custom/timeline-editor-native-01/ui/results.json、native-final-pass.json 与原根 custom/timeline-native-final-pass.log；runtime-04 配最终生产前端。紧凑 V1/A1 中心和时间线底部均在底栏上方。原生鼠标/键盘及实际 MCP 不使用页面拖拽模拟；历史 38 项和首次失败保留，人工验收保持 pending |
| 当前多轨用户窗口恢复 | 原「新项目」在真实可见窗口恢复，资源 1 项、默认主序列 3V/3A、dirty=false，选择及源预览恢复；磁盘 AVP v1 哈希不变，工作副本按 v2 读取 | 原根 custom/timeline-editor/user-workspace-restored.json / user-window.png / user-window-native.png；原生标题 Video Auto Cut — 新项目，屏幕取色 #1E1E1E、底栏就绪、3 个 Worker 在线。启动前一致性备份 live-dev/control.before-timeline-0.2.1.db 保留；同版桌面/Go/清单复制至 live-dev/timeline-runtime-0.2.1 并校验哈希，常驻用户窗口不计作一个已结束的管理器测试 |
| 0.2.1 CHROME01 工作副本（2026-10-21） | 前端 93 项、样式/类型/生产构建、Rust 格式/5 项测试/桌面构建、文档契约及真实 Windows 桌面 31/31 通过；人工观感待验 | custom/window-chrome/*.log、window-chrome-native/ui/results.json 与 native-title-{home,project-dirty}.png；实际屏幕像素确认标题栏 #1E1E1E，覆盖清洁/未保存/关闭后的标题和底栏就绪/停止/重启。独立测试数据，未开启可选 ASR 推理；不重报 Go/Worker 全套或声音/模型质量 |
| CHROME01 时用户窗口恢复（历史） | 当时原「新项目」恢复，资源 1 项、dirty=false、工程文件 SHA256 未变；原生标题为 Video Auto Cut — 新项目，底栏就绪 | custom/window-chrome/user-workspace-restored.json / user-window-native.png；沿用原开发业务目录。当前多轨窗口恢复另见上行，不沿用当时截图验收新界面 |
| 0.2.1 cadd51a 完整 Go | 17 个有测试包通过，2 包无测试；显式启用已预装本地 ASR 实测 | root 14.824s、API 23.230s、MCP 37.350s、service 456.160s、store 7.103s；原根 0.2.1 custom/go-full.log，明确以当前工作树 control-plane 为源码 |
| 0.2.1 cadd51a 前端/Worker/契约 | 前端 93、Python 164、Timeline 54、Exporter 53、TS/构建/样式与接口检查全部通过 | 当前 0.2.1 custom/*-unit.log、typescript.log、documentation.log、script-docs.log 及 python/timeline/exporter/output.log；158 HTTP、122 类型、16 IPC、25 SSE。真实桌面另记 |
| 0.2.1 本地 ASR | Go→本机 faster-whisper 1.2.1 base CPU/int8 →22 个源词时标；诊断/取消/幂等重放/跨工程拒绝通过 | 真实测试已显式启用，非跳过；现有本地模型指纹参与缓存，未下载、修改模型或外发；Python 单元另覆盖 5 秒偏移、依赖/模型缺失、静音和失败不发布 |
| 0.2.1 Rust/Windows 桌面 | Rust 5/5、build/fmt 通过；真实可见桌面 26/26 通过 | custom/rust-unit.log、desktop-build.log、desktop-native.log、desktop-native-ui/results.json 及截图；关于页 0.2.1、ASR 原生选择/环境检查/保存重载、MCP 三种复制与重启新会话刷新，加既有六类预览/工程往返/隔离/排空/单实例。原剪贴板文本仅内存保留并在 finally 恢复 |
| 历史 `7dda64d` 完整 Go | 16 个有测试的包通过，另 2 包无测试文件 | root 13.849s、API 21.956s、MCP 36.430s、service 448.146s、store 5.884s；原根短输出 custom/output.log，实际源码 WorkingDirectory 为当前 control-plane |
| 历史 `7dda64d` 前端/Python/Node | 前端 92/样式、Python 159、Timeline 54、Exporter 53 全通过 | 当前工作树 artifacts/custom/branch-*.log、branch-consolidation-verification.json；TS/Go vet/Rust fmt/文档契约也通过 |
| 历史 AVP 可见桌面 | 22/22 通过 | custom/avp-desktop-ui/results.json 及截图；原生文件操作、六类预览、标记/备注、关联/重定位、待执行任务停止切换、单实例、关闭取消、保存退出队列及新进程参数重开 |
| AVP Go/Worker/媒体 | 往返、独立另存、全新库恢复、缺失/定位、AVS 关联、旧请求/Worker 排空、输出引用和依赖失效通过 | custom/avp-final-verification.json；真实 Go→Python→本机模型桩→Go→Node/FFmpeg，不是实际外部模型质量 |
| MCP 协议与媒体 | SDK 四版本 × Agent/Worker，原生图片、包资源哈希、三版 MP4/ZIP/完整解码/Range、定向改稿与历史回归通过 | 1.0/.8/.7 秒是确定性媒体夹具；Agent 无人工定稿/日志工具，窗口真实操作/旧服务级模拟人工分别记载 |
| 本轮文档整合，`6242cf7` + 本轮工作副本 | 文档/契约、样式、PowerShell 解析、保留边界通过；既有 MCP 全包通过 33.064s，Skill 包无独立测试文件 | documentation/documentation-check.json、retention-audit.json；script-workbench/doc-check.json；documentation-*.log。157 HTTP、15 IPC、119 类型、25 SSE；460 个本地链接、6 份工作台相关文档的 454 个链接。未重报产品全套/可见桌面/人工/发行验收 |
| 0.2.1 CR03 最终界面 | 类型/构建与既有审查快捷键浏览器 14/14 通过 | custom/review-smokes/review_shortcuts_smoke.mjs.log；真实生产 React、确定性接口夹具。场景/解说实际目标、已完成项、输入/按钮、修订/工程切换、隐藏页、模态框开关均覆盖；真实后端与原生窗口分别记录 |

0.2.1 行的 `custom/...` 相对当前工作树 `.run-data/tests/0.2.1/artifacts/`，此前历史行相对旧 0.1.1；共享 Cargo/Go 短输出位于原根对应版本。运行器本地结果可能已按规则轮换，报告存在不代表全部旧截图/数据库仍存在。

## 6. 已处理失败和未验边界

| 历史问题 | 处理事实 | 当前含义 |
|---|---|---|
| 多轨首轮旧契约/运行时限 | SSE 领域、MCP 数量及面包屑旧名更新；Go 240s 累计超时后按既有 600s 全套执行，未跳过原失败用例 | timeline-go-full-first.log、timeline-web-unit-first.log、timeline-web-navigation-first.log；最新全套通过，不宣称首轮无失败 |
| 多轨原生探针及菜单遮挡 | 启动夹具补 root_id；菜单定位限定实际 popup，产品执行菜单后关闭浮层；探针修正坐标数组、鼠标落点等待、模态关闭动画与输入焦点 | native-*-failure.json 和 timeline-native-*.log 保留；原生拖拽及中文绑定要求不放宽，完整最新结果见上表 |
| 多轨首次会话及播放头恢复 | 首次会话现恢复 selected_resource；新增刷新检查时保存的 113 帧被归零。仅过滤 timeupdate 未解决，事件追踪确认 loadedmetadata 前已归零；改用会话/序列所属的保存值及显式本地覆盖，去除默认零值与恢复 effect 的覆盖竞争 | native-reload-prompt-failure.json、native-reload-beforeunload-prompt.png、native-position-owner-failure.json、playhead-restore-trace.json、timeline-native-position-first.log 保留；浏览器 14 组包含非零播放头载入及刷新，最终原生 39 项也实际重开/刷新校验保存位置、选择和无虚假未保存。原始用户 AVP 未修改 |
| 多轨保活页面代理竞争 | 非零播放头原生恢复已通过，随后资源页 FFV1 代理收到 conflict；隐藏源监视器仍随所选资源请求媒体，可能竞争同一代理锁。现资源页和源监视器仅激活时装载媒体/文本，退出取消所属请求，同组件在状态提交前用请求引用防重复 | native-hidden-proxy-failure.json/png 与 timeline-native-hidden-proxy-first.log 保留；不放宽 Go 锁或添加泛用重试。浏览器检查隐藏源播放器不存在，最终原生使用新 FFV1 文件实际生成代理并播放，通过原失败用例和完整 39 项 |
| Python 首轮环境/Windows 失败 | 命令首次错指根 .venv 未执行，改用 workers/python/.venv 后 163/165；定向确认 launcher=41772 与 actual=26184，锁测试原先仅杀 venv 重定向器，现用既有 taskkill_tree 停止自己创建的树并验证真实持锁者退出。venv 行为见 [Python 官方说明](https://docs.python.org/3/library/venv.html) | 原失败、定向和完整复验保留 timeline-python-first.log / target-first.log / target-pass.log / final.log。生产锁未放宽；完整 165 项通过。目录发布 WinError 5 定向及整包均未再现，B04 仍观察，不猜测根因或加通用重试 |
| CHROME01 首轮测试配置和探针 | 先因测试 overlay 仍需不存在的旁载 Go 失败，改为开发显式路径且不打包复制；再将测试数据从资源根内移到原根独立版本目录。DWM 颜色属性无法经 Get 回读，改用原生截图真实屏幕取色并修正 DPI；新工程先保存初始化布局再核对清洁标题 | rust-first-failure.log、native-first-failure.json、native-color-readback-failure.json、native-title-fixture-failure.json 保留；不降低原生颜色/标题要求，最终既有套件 31/31 通过 |
| 0.2.1 本地 ASR 初次真实识别 | 既有 PyAV 不接受 faster-whisper 的 metadata_errors 参数，改从已有 FFmpeg PCM 直接生成 NumPy；词时间与概率显式转普通 float | 真实 Go/模型 22 词及全包回归通过；不通过更换/下载模型、云回退或伪造文本规避 |
| 0.2.1 MCP 原生复制缺口 | 首轮窗口发现 clipboard_write_text 未注册，补 HWND 所属的 Win32 Unicode 写入及错误反馈，探针等待异步写入后核对 | native 26/26 覆盖地址/TOML/JSON 和重启；原剪贴板内容不写测试结果。源码/单元通过没有被当成复制已验收 |
| 0.2.1 桌面探针启动/预设/重载夹具 | 启动 CDP 断连增加有界附加；新预设补既有必填声音并使用 revisions 接口；等待 Home aria-busy=false 和可见控件，避免会话返回重建后的旧节点 | 应用退出、运行故障与之后断连仍失败；未放宽产品校验，26/26 通过后保留最小首轮定位记录 |
| 0.2.1 首轮 Cargo zerofrom 编译退出 1 | rustc 未附明确错误；保留同版本 cargo-first-failure.log，原锁文件/依赖不变，以 -j 2 构建通过 | 不猜测根因，也不将首轮失败隐藏；当前 Windows 源码构建已通过 |
| Go 180 秒超时和嵌套长路径失败 | 改为短版本化输出、明确源码 cwd、600 秒超时，完整包不跳过原失败用例后通过 | 不把长路径误写成产品逻辑已无所有 Windows 共享锁风险；B04 观察仍见计划 |
| 旧人工 Token/日志/预览断言 | AVP 移除窗口凭证，原测试更新到新身份/项目隔离契约，MCP/Worker 拒绝用例保留 | 旧“匿名日志应 403/先解锁”不是当前窗口流程；仍校验 Agent 工具和跨工程访问 |
| MCP 三个旧版本 tools/list 400、仅 base64 图片、契约分发不足 | SDK 协商、原生 ImageContent 和固定包资源已修复并实际往返 | 不再列作代码缺口；不等于真实视觉模型判断质量通过 |
| 原生文件框选错控件、页面数据未加载就输入、退出后 CDP 查询 | 既有探针等待实际可见/可编辑状态、限定窗口/控件和关闭传输后复验通过 | 早期失败提炼于 avp-desktop-first-failures.md；未放宽产品要求 |
| 2026-10-19 旧 500 MiB 规则中止组合测试 | 当时中止及拆分通过分别记录；随后已取消固定容量限制，两处 workspace 守卫各 36 项通过 | 不把旧中止记为全套通过，也不把旧容量门当现行政策；原临时 JSON/数据库/截图有部分已清除 |

当前外部 LLM/转录/云声音质量、人工观感、目标剪辑软件、声明规模长素材及最新安装包/系统关联/公开更新仍按计划另验。2026-10-02～03 的真实 OpenAI、原交付 Premiere 导入/播放/编辑、迁移重链接与视听效果已有用户确认；Anthropic/Gemini 真实调用按用户要求排除。2026-10-09 的 ASS/StepAudio 样本人工确认和本地签名升级同样保留，以上均为旧候选的有限证据，不扩大到当前 AVP 全面验收。

追加旧编辑器浏览器回归时曾遇自动审批服务额度不足；后续审批恢复，已补验通过。旧测试仍查询改名前的“剃刀 C”和帮助文案，改为当前可访问名称及剃刀/波纹行为断言后，14 组全部通过。未删快捷键、复制/粘贴、撤销/重做、弹窗与隐藏工作区保护断言。

本轮最终增量以本地 a23d138+工作副本为准，未提交/推送；应用版本不变。验收索引在源码工作树 .run-data/tests/0.2.4/artifacts/custom/verification-summary.json。


## 2026-10-25 媒体导入与 Remotion 后台窗口增量

当前源码仍为 timeline-audio-viz / a23d138 + 未提交工作副本，应用 0.2.4，不执行提交/推送。MEDIA01 和 DRAW03 已实现并通过下列自动检查；不把早期 Go 18 包或 Python 165 项全量结果当作本次重新执行。

| 层 | 本轮实际结果与证据 |
|---|---|
| Go 工程/时间线/绘制/API/MCP | 四包受影响 TestProject/TestTimeline/TestSequence/TestDraw 回归通过；最新 RAW 历史/关联和依赖修改后 project/service 再验通过，project 模型整包通过。原根短输出 custom/media-go-regression.log、media-go-raw-history.log、media-project-model.log |
| 实际媒体 | 确定性 DNG 导入/并发缩略图及预览、曝光和灰度结果、150 帧共享序列合成、实际 Remotion 图片绘制、拒绝旧 recipe 渲染、AVP 往返、撤销/重做、RAW→PNG 关联均通过。MKV 1 路视频 + 2 路音频实际入轨索引 0/1/2、同一链接组；MP4/MOV/AVI/FLV、MP3/WAV、JPG/JPEG/JEPG/PNG/GIF/BMP/TIFF/WebP 实际导入和代理通过 |
| Python RAW | 新增独立长期职责 tests/test_raw_image.py，包含可复用的微型 Bayer DNG 构造器，不添加私有相机样本到 Git。配置 requirements-media.txt 后 3 项通过，含 Sony a7 III ARW 实际显影/曝光变化、源哈希不变。custom/media-raw-python.log、sony-arw-verification.json |
| 前端 | 既有完整单元 64+60=124、样式21色、类型与生产构建通过。custom/media-web-unit.log、media-web-build.log |
| 浏览器夹具 | 原编辑14组及绘制/资源9组通过；新 RAW 组实际操作曝光、应用、刷新重开和重置，图片 URL 跟随 recipe 变化；1280控件截图保留。custom/review-smokes/draw_smoke.mjs/results.json completed=true、raw-controls-1280.png。浏览器返回为夹具，不冒充实际 RAW 显影，真实像素由 Go/Python 检查承担 |
| 绘制/后台窗口 | 既有 draw.test.mjs 共7项通过；使用真实 Edge 的成功及源码故意失败都执行完成。Windows 观察实际子进程带 --headless=new，可见窗口0，捕获浏览器结束后无存活。custom/media-draw-final.log、draw/lifecycle/verification.json。部分既有像素用例命中已验证缓存，新的 RAW 实际绘制另由 Go 证明 |
| 契约 | 178 HTTP / 17 IPC / 150前端契约模型 / 27 SSE。更新自动清单后使用既有文档检查，不增加接口旁路 |

RAW 环境显式安装 workers/python/requirements-media.txt。当前专用 .venv 使用 Python 3.12.14、rawpy 0.27.0 / LibRaw 0.22.1、Pillow 12.0.0；运行期间不下载内容。官方 PyPI HTTPS 发生 EOF，系统代理仍失败；依赖通过清华 PyPI HTTPS 镜像安装，未关闭证书验证，原工作树/系统环境未改。公开 Sony 样本下载至当前版本 cache/media-fixtures，未放入用户工程或 Git；来源 [raw.pixls.us Sony ILCE-7M3](https://raw.pixls.us/data/Sony/ILCE-7M3/)，_DSC0009.ARW，SHA256 250784580ea527442c09004417bb0eead484f2bf3ee8f9121a776ac65bb50d0f，与库发布 filelist.sha256 一致。LibRaw 的具体机型/压缩模式覆盖仍以实片解码为准，未声称验证全部 Sony 相机。参数语义依据 [rawpy Params](https://letmaik.github.io/rawpy/api/rawpy.Params.html)。可通过 VAC_ARW_FIXTURE 显式指定已准备样本，不在测试或运行时隐式下载。

首轮定位证据：新 Go 夹具起初使用错误的 resource.insert 名称，随后缺失 clip.insert 的必填 track_id，修正夹具后通过，产品校验不放宽；前端 Alert 首次使用不存在的 title 属性，改为现有 message；浏览器 RAW 初次被工作区状态夹具恢复成旧列表，补同一夹具 state 接口；1.0 与 1 的显示格式改为数值断言，真实刷新/重置断言保留。首次文档检查因 rustfmt 后命令位置改变报过期，重新生成清单后复验。相关 first/failure/number-format 日志保留在同版本 custom 内。

用户截图中的旧 Remotion Bundle 窗口，当前两次枚举都未找到同名窗口或浏览器，因此没有终止未知用户进程，也不宣称关闭了未发现的旧窗口。修复的是已确认的旧 headless 模式缺陷及 finally 清理。最新原生 Tauri 文件框、安装包、用户原片和人工任务切换器/调色观感本轮未验。


## DRAW04 绘制布局与控件动效（2026-10-25，Chrome 自动验收完成）

使用 Product Design（插件0.1.56）的当前产品检查流程，无已保存设计上下文。源码仍为 timeline-audio-viz / a23d138+未提交工作副本，版本0.2.4；没有发布或推送，没有修改原工程和业务数据。

| 层 | 实际结果与证据 |
|---|---|
| 前端 | 首轮完整单元124及样式21色通过；最终类型/生产构建及样式检查通过。custom/draw-layout-unit.log、draw-layout-build-final.log、draw-layout-style-final.log |
| Chrome 夹具 | 本地 C:/Program Files/Google/Chrome/Application/chrome.exe。绘制11组、原编辑14组通过；包含专注预览往返、1024及1280窗口、固定导入位置、真实图片拖入、选中图层对比度≥4.5、键盘菜单、减少动态效果、保存/渲染版本及RAW控件。custom/draw-layout-chrome-final.log、review-smokes/review_shortcuts_smoke.mjs.log、draw_smoke.mjs/results.json（completed=true，记录浏览器路径） |
| 真实Go界面 | 当前Go二进制+最终WebUI，独立示例AVP引入前轮已校验的公开Sony a7 III ARW，实际输出30帧640×360视频；Chrome实际播放呈帧、缩略图解码、图片属性和专注预览往返通过，页面错误0。custom/draw-design-live-verification.json、draw-design-live-1440.png、draw-design-live-1280.png、draw-design-live-focus.png |
| 渲染生命周期 | Remotion Chrome 用例通过。观察器必须实际捕获正常与源码故意报错两个 Chrome，均带 --headless=new；可见窗口0，所有捕获PID结束后无存活。custom/draw-layout-chrome-lifecycle.log、draw/lifecycle/verification.json；不沿用旧Edge结果验收Chrome |
| 文档契约 | 沿用机器清单和既有检查，178 HTTP/17 IPC/150模型/27 SSE；布局未新建API。最终检查见 custom/draw-layout-documentation.log |

视觉复核以本任务捕获的1280×720源代码/预览/输出状态比较，源码栏由约240px增至约320px，时间区168px压到104px，右栏导入入口固定在可见底部；深色主题/既有颜色和时间token保留。初次1440截图预览未加载，不作视觉验收；真实Go初次抓拍仍显示原生媒体加载提示，也未采纳。最终实际播放呈帧、缩略图完成解码并等待控件稳定后重新截图，已逐张打开核对。截图不构成全面辅助技术合规或人工动效手感验收。

初次 reduced-motion 断言发现拖入态更高优先级 transform 覆盖 none；改为 .draw-studio .draw-drop-feedback 后原断言通过，未降低要求。续验曾因审批额度不足而未执行构建/浏览器与预览构建，服务恢复后已补齐。Chrome生命周期探针先把退出时空命令行覆盖有效值，继而标准输入完成信号过早结束采样；现保留有效命令行，并使用版本化文件显式结束全程观察。尝试的模块spawn拦截不能观察已导入的浏览器启动，已移除，最终直接用真实Windows/CIM观察证明，不能以空快照通过。首轮失败日志保留 custom/draw-layout-*-failure.log / reduced-motion-first.log。

用户明确只使用本地Chrome、禁止Edge。run-local-smokes.mjs选择已安装Chrome或显式CHROME并拒绝Edge，两个浏览器子入口也校验实际可执行文件。未安装或使用新的浏览器，不依赖内置Browser。

独立预览有意保留：原根 .run-data/tests/0.2.4/artifacts/custom/draw-design-preview/preview.json 记录进程、地址和数据根，Go服务与WebUI均为本轮源码。示例工程及原件只读引用不涉及原用户AVP；后续临时文件也限定 preview/temp。该Go服务是供用户查看的交互预览，不计作意外残留；测试创建的Chrome/Remotion浏览器必须退出。预览不使用业务服务商凭据。若之后清理/轮换测试目录，先核验并关闭这个明确归属的预览服务。原生Tauri、安装包、用户原片及人工手感未在本轮重新验收。


2026-10-25 NAV01：沿用 draw_smoke.mjs 增加实际五个导航标签可见及位于容器内的断言，覆盖1440/1280/1024px；既有绘制12组通过。真实Go预览在960×720、deviceScaleFactor=1.5时核验滑轨600px、五项均展示，截图 nav-width-live-150pct.png、记录 nav-width-live-verification.json；生产构建通过。未新增独立套件，无后端/接口行为变化；原生桌面未重验。证据为 custom/nav-width-build.log、nav-width-browser.log、nav-width-live.log。


NAV01 窄屏补充：前次仅验证至960px，未覆盖旧959px以下的100%增长规则。现去掉该规则、统一固定600px；复用 draw_smoke.mjs 增加实际宽度断言及900/800/640px检查，所有五项可见。构建/类型与绘制12组通过；真实Go预览800×720@1.5x确认600px，截图/记录见 custom/nav-fixed-live-800.png、nav-fixed-live-verification.json。复验日志 nav-fixed-build.log、nav-fixed-browser.log、nav-fixed-live.log。


## UX05 整体界面与设置页（2026-10-25）

沿用共享 `webui/run-local-smokes.mjs` 启动器，新纳入长期设置职责的 `settings_smoke.mjs`（没有并行新建测试服务器/样本套件）：设置导航、草稿替换取消、保存失败重试、Provider/本地转录表单、1440/1280/1024/900/800/640布局、键盘与减弱动态效果、TTS独立流程。既有编辑14组与绘制12组继续保留。使用安装的本地Chrome，禁止Edge，不下载浏览器，不调用外部模型/转录/配音。

```powershell
$env:VAC_DIST=(Resolve-Path './webui/dist').Path
& './scripts/TestWorkspace.ps1' -Action Run -Suite custom -Executable 'D:/Applications/NodeJs/node.exe' -Arguments @('webui/run-local-smokes.mjs','settings_smoke.mjs','review_shortcuts_smoke.mjs','draw_smoke.mjs')
```

最终设置7组、编辑14组、绘制12组行为复验通过；前端124项、样式/类型和最终构建通过。最终日志文案调整后设置7组再次通过，减弱动态效果下的鼠标分类往返也通过；分类选择须等待上次菜单实际收起再发下一次操作。真实Go服务6102使用独立示例AVP，捕获六个设置分类及资源/编辑/绘制/导出/TTS，页面错误0；当前未保存真实服务商配置或调用服务商。实际源码是timeline-audio-viz工作树的a23d138+未提交工作副本；应用版本0.2.4。原根测试入口仅用于独立输出/缓存，不能以其HEAD替代源码工作树。

证据：当前源码 `custom/ui-ux-browser.log`、`review-smokes/settings_smoke.mjs/results.json`；真实Go截图与记录在原根 `.run-data/tests/0.2.4/artifacts/custom/ui-ux/`、`ui-ux-live.log`。现状捕获中首次设置/绘制截图及后续含播放器加载圆弧的截图已拒收；最终绘制截图实际播放呈帧后暂停再捕获，无加载圆弧；测试曾发现并修复“设置深链被AVP旧工作区覆盖”，测试夹具limit参数及选择器自动化定位也已调整，不把夹具故障记作后端故障。界面截图不等于原生Tauri或人工观感验收；本轮不重跑Go/Python/渲染全链，沿用其历史证据并标明未重跑。


## REL031 版本发布检查（2026-10-26，自动验证通过）

以下为当时记录；0.3.1测试输出已随0.3.2按规则轮换，文中“当前”及文件位置不代表现在状态。

当前应用版本0.3.1，实际源码为timeline-audio-viz工作树的a23d138+工作副本。0.2.4证据按仓库规则轮换，原根和当前工作树的测试根只保留新版本及最简Markdown简档；正文中的0.2.4报告路径仅为历史证据定位，不代表文件仍保留。旧版本完整报告、缓存、数据库和渲染输出没有迁存，当前示例只保留工作文件和共享Sony ARW原片以重新生成预览。两测试根同口径占用在轮换时从1,337,966,250字节降至25,676,023字节，释放1,312,290,227字节；该数字不含后续0.3.1检查产生的新缓存与输出。原业务库、录像、用户原AVP、其他工作树保留。

本轮复用前端124项/样式/类型/生产构建、Go project/skill/MCP、真实两引擎绘制及Chrome设置/编辑/绘制回归，新增版本一致性检查仅放当前版本临时输出，不增加永久平行测试套件。版本检查区分Cargo自有包与第三方依赖，不全局替换版本号。前端等价执行package.json里的两组Node命令、style、tsc及Vite，避开此前已记录的npm启动器问题。Windows命令使用系统PowerShell7并加载profile。

检查状态与日志保存在当前源码 `.run-data/tests/0.3.1/artifacts/custom/release-*`；Go使用原根同版本短输出/缓存，记录的实际源码仍是当前工作树。恢复的独立预览为0.3.1 Go/前端，不以预览和MCP初始化替代原生安装器或人工质量验收。完整Go/Python/原生桌面、真实外部模型、安装更新不在本次升版中重跑。


0.3.1实际结果：版本/锁文件一致；Cargo元数据offline/locked通过；前端124（64+60）及样式/类型/构建通过；Go project/skill/MCP三包通过（MCP 68.443秒）及控制面构建通过；真实绘制7项通过（135.09秒，包含两引擎图像像素和正常/源码异常的Chrome退出）；Chrome设置7、编辑14、绘制12组通过；接口178 HTTP /17 IPC /150模型 /27 SSE，13文档/533链接检查无错误。

真实0.3.1 Go预览已恢复6102，示例AVP和资源均恢复；MCP initialize返回serverInfo.version=0.3.1。30帧640×360 Remotion预览SHA256仍为aee79887debb5f33a1cdb761f446d05c1eb6c3cb9363f50f469ed3ef3b637ec0，与轮换前一致。Chrome实际呈帧、关于窗口0.3.1和页面异常0通过。恢复首轮缺delivery目录、MCP未启用的预览配置问题已分别补齐，不作为源代码缺陷；最小失败日志与当前验证文件留在新版本目录。预览服务按用户查看用途保留，不将其记为残留渲染进程。原生Tauri、外部模型质量及人工观感未重验。


REL031代码已发布：`5d407409c16aa257e9aa539791ec64ba3074880b` → origin/agent/timeline-audio-viz，已用ls-remote核对。所有本轮源码检查对应此代码提交；后续发布状态文档提交不改变运行代码。当前工作树及原根summary中REL031项更新为passed，其余未在0.3.1重跑的历史项目继续保持pending，不将历史全量结果批量转绿。


## DRAW05 统一绘制实施与验收（2026-10-26）

本轮按[统一绘制设计](../docs/架构与功能详解.md#unified-draw-proposal)实施，实际源码为agent/timeline-audio-viz@8974cdc的未提交改动，应用0.3.1。REL031历史计数不作为本轮完成证据。阶段结果与失败轨迹如下，较早的“尚未”指该阶段当时状态。

实施优先扩展现有Node draw.test.mjs、Go project/service/MCP、webui draw_smoke.mjs及既有sequence/原生入口，复用固定媒体与0.3.1版本化临时区，不建立第二套“统一框架”专用永久服务器和报告体系。首个场景必须同时包含实际HF动效、React组件、透明前景和导入图片；以同一帧的组合像素、倒序/随机seek和黑白底alpha边缘证明双方协作，不能仅以统一按钮或先后生成两份MP4验收。

后续覆盖纯HF/纯Remotion旧输出对照、混合FrameAdapter、快照/素材与RAW recipe更新、字体就绪、帧完成等待、过期回包、局部缓存、撤销/草稿并发、缺少引擎与取消、AVP1/2读取/AVP3保存、MCP与工程隔离，以及绘制源在编辑序列中的节目/最终导出一致性。Chrome浏览器、原生Tauri、模型创作质量和人工体验分别留证；预览流畅度以固定样本实测，不从端口或视频编码成功推断。

DRAW05阶段1（2026-10-26，当前未提交工作副本）：既有 `draw.test.mjs` 增加原生HF/React/图片同帧测试，6次正逆序定位及透明像素验证通过（1/1，6.10秒）。首次缺失模块和运行时注入失败已定位并修复；不是整体框架验收。证据：当前版本custom/draw/unified/verification.json、frame-0/15/29/2.png与transparent.png。界面、AVP3、Agent、源入轨和原生桌面尚未验收。

DRAW05增量：Go project模型全包通过；既有service/draw_images_test新增持续预览草稿隔离/进程复用/错误会话/关闭测试通过，修复保存和列表的浅拷贝别名；源构图无MP4入轨、冻结/跟随依赖、AVP3重开通过。Node既有draw测试新增统一导出/透明WebM/纯引擎像素对照通过（2/2，12.85秒）；sequence测试新增FrameSource与原素材/原声混合通过（5.34秒），涉及来源12fps、绘制24fps、序列30fps。证据为当前版本custom/draw/unified-export及custom/sequence/drawing-source/verification.json。后续代码更新需按影响复验；UI、MCP和原生窗口仍待验证。

DRAW05当前增量：嵌套组/有理帧率/源范围的Go project全包通过；Node统一绘制5项通过（17.02秒，包含原生24fps精确seek、跨HTML/React椭圆遮罩和随机/倒序帧）。Go服务常驻预览/字体依赖/源快照/透明WebM与ProRes导出通过（16.51秒）。前端绘制10单元、TSC、Vite及Chrome夹具10项通过；Chrome覆盖1440/1200/900px、连续定位只有一个在途请求、草稿并发保留、失败提示与默认Agent管线。离线原生构建通过（12.13秒）。真实桌面首轮因ARW夹具路径错误、次轮因测试端CDP表达式转义错误停在导入后，属于验收脚本失败，原生完整链尚未通过；已修正既有入口继续复验。源工程/原片未改。

DRAW05回归补充：Go全包短回归中service277.36秒及其余17包通过，Skill因内嵌与文件说明副本不同失败；同步后project/Skill/绘制服务目标回归通过（service20.48秒，含整场景撤销、幂等重放、过期CAS和冻结资源保持）。前端完整127项通过，类型/样式/构建通过。Chrome实际指针新增断言先复现PNG原生dragstart→pointercancel，关闭图片原生拖动后当前10项通过；该修复不以旧桌面报告代验。Node按来源增量编译、共享遮罩、嵌套时钟5项通过（16.80秒）。测试端两次路径/表达式错误及原生拖动失败已分别定位；修复后桌面整轮验收进行中。

DRAW05真实大图修复：原生鼠标/整场景撤销已通过；6024×4024 Sony ARW显影为69,211,204字节PNG，旧路径在HTML+公共图层帧请求中重复内嵌，触发调试连接断开。改为受控本地HTTP资源并在浏览器内部为隔离HTML生成数据URI，帧协议不带图片正文；Node同帧/源码热替换/图片引用5项通过（16.18秒）。随后真实583,481字节检查帧触发旧256KiB产物限制；现有MCP套件增加确定性高细节PNG，先复现失败，再通过16MiB帧读取/原生image返回/无MP4源入轨/终渲回归（10.12秒）。文本产物限制保持256KiB。两次传输失败均保留在当前结果轨迹，原生整轮继续复验。

DRAW05最终原生验收通过：`verify-tauri-desktop.mjs`沿用同一入口，通过`VAC_VERIFY_DRAW=1`执行9项可见Windows检查，结果为passed、human_acceptance=pending。实际导入Sony ARW，鼠标移动/本地手势撤销重做、Go端Agent整场景撤销重做；MCP以图片+原生HTML/GSAP界面+React数据卡制作、检查0/15/29帧并返回原生图像；不先生成MP4即源入轨，run.finish完成共享终渲；原生保存框导出独立视频，AVP3保存/关闭/重开保留两份源码和播放头15。真实画布与帧已由根Agent目视检查，不代签用户观感。

证据：实际源码工作树当前版本`artifacts/custom/unified-native/results.json`、`unified-draw.png`、`native-unified-draw.png`、`绘制导出.mp4`及同目录测试AVP；后台隔离数据在原根同版本`artifacts/custom/unified-native/data`。完成运行`skr_058401d3d312a037`的共享序列MP4 SHA256=`eefb40fc83291989dc2c763d08116fc2dac6c169e8c34e7acfc6bd419297b169`。这是自动原生结果和当前Agent的MCP创作样例，不是内置服务商模型质量或用户人工验收。

最终回归：Node现有draw+sequence完整17项通过（57.50秒）；最终前端127单元通过、类型/样式/生产构建通过；最终Chrome夹具10项通过；Go vet通过。Go全包短回归17个非Skill包通过，Skill说明副本漂移经同步后目标复验通过；新增整场景事务project/Skill/service回归通过，高清PNG的MCP用例先红后绿（10.12秒）。不声称在最后所有增量后又跑过第二轮Go全包。接口文档包含184真实路由、157模型、28事件、17桌面命令；最终文档检查结果以管理入口记录为准。原用户“新项目.avp”SHA256仍为`981E72F51760EC5B0D28C722C32DC9FA722D2040F821C3D24B9CD2B544F38822`。

临时环境：旧输入法Hook生成的`workers/node/timeline-cli/%SystemDrive%`目录已核验非重解析点/无Git跟踪后，原样移入本版本`artifacts/custom/incidental-ime-cache`，344304字节保留、删除0字节。已为受控工具保留SYSTEMDRIVE/PROGRAMDATA；没有删除原片、测试源码、固定样本、业务数据或其他工作树。原生测试关闭后核验其独占进程退出；6102原测试示例使用已验收Go候选恢复，保留预览和正式安装器/业务运行分开。

最终透明像素补验：既有统一导出用例追加ProRes4444解码，对照WebM实际alpha：透明区0、半透明前景129（预期128±2），额外1项通过（10.62秒）；证据仍在同一custom/draw/unified-export/verification.json。最终保留6102示例的真实第15帧返回496127字节PNG/2个命中区域，验证后关闭该临时帧Worker，保留Go服务。受管文档检查通过（184接口/157模型/28事件/17桌面命令），不把直接调用缺少VAC_TEST_OUTPUT的入口错误记为文档语义失败。

## QA-FISH 用户监督的 StepFun 实际创作（2026-10-26）

本轮是实际模型与窗口操作测试，不替代 DRAW05 工程回归。源码仍为 agent/timeline-audio-viz@8974cdc 的当前工作副本；版本0.3.1。输出在原根 `.run-data/tests/0.3.1/artifacts/custom/stepfun-fat-fish/`。未提交/推送、未覆盖原用户工程。用户授权调用 StepFun 并允许寻找素材；根Agent确定橙金胖鱼/青蓝水下/5秒无声方案，全部鱼形和动作TSX由 `step-5-preview`、`reasoning_effort=high` 生成。

- 模型调用：第一次240.125秒超时，未返回正文；第二次HTTP200但16000 token耗尽、finish_reason=length、正文为空；第三次精简上下文并把max_tokens增至48000，247.578秒HTTP200/stop，返回9000字符决策。不能把前两次当作通过。没有记录模型内部推理正文或API Key。
- 素材：Michael Pointner / Pexels 31843981 水下照片（来源和许可见同目录素材来源.md），通过原生文件选择器导入。模型把图片元数据放到document.images，严格参数解码失败并使任务waiting_human。根Agent只映射图片绑定到统一文档隐藏image节点并更新CAS，鱼形、SVG和动画TSX未修改；原始及重开AVP内源码SHA256同为 `50f31358d0907ea3a9672bbc8362911354967471b484266e58ee642de9575579`。
- 实际失败：初版预览只有模型的渐变回退，用户指出照片未使用。修正绑定后，FFmpeg7.1将JPG探测为image2，prepareImages的format_whitelist缺少该格式。以原始参数独立复现退出-22。保持原JPG，显式jpeg_pipe解码到同图PNG，经原生选择器重新导入并绑定，实际背景随后生效。本次未改产品解码逻辑，JPG兼容缺陷仍待修。
- 实际检查：draw.check最终通过；MCP draw.preview和artifacts.get读取0、4、8、75、149帧，根Agent已目视查看。尾根旋转及鱼鳍/气泡变化可见，水下背景实际合成；不把源码自述当作画质证明。可见Tauri操作了原生导入/导出、播放、保存、关闭和重开。
- 产物：原生导出 `大肥鱼摇尾巴.mp4`，ffprobe独立核验H.264、1280×720、30/1fps、150帧、5.000秒、415098字节、无音轨；SHA256=`648b8591bc79ca698e01ec31824a1a74b1f6d97d4541e05767d3cef7cc95c14e`。实际编码第4帧亦已查看，照片存在。源快照入轨后run.finish完成共享合成，运行 `skr_1320eeacdaedf042` succeeded/23步，共享序列产物SHA256=`a984f6618d8bd0d05c460528edd5f4d2de3f4fe64f1173f2a069c0101cfd4ca8`。
- 保存与会话：`大肥鱼摇尾巴.avp`为AVP3，含可编辑构图、绑定和150帧序列；已关闭带临时凭据的自有桌面/Worker，模型桥接进程退出、REPL清空，再以明确移除凭据环境的进程重开AVP。重开恢复同一项目、一个序列/一个Remotion源和succeeded运行，未操作原6102预览或其工程。
- 尚未通过：用户人工观感/模型质量最终认可；外部生成进度未回传、陈旧等待领取提示/租约、模型错误字段的诊断与无绑定图片回退检查、JPG图片准备兼容。上述问题已列QA-FISH-FIX；成片导出成功不代表这些缺陷修复。

证据复用同一临时目录：result-001/002/003.json（仅最终正文和用量）、authorship.json、check-final.json、png-frame-{0,4,8,75,149}.json.png、finished.json、导出核验.json、实际MP4第4帧.png、照片已生效.png，以及AVP/MP4。桥接脚本均为本次可再生产物，不引入第二套永久测试入口。
QA-FISH收尾复核（2026-10-27）：当前测试桌面和后台已退出；没有将失效CDP连接当作当前可用窗口。对落盘AVP3重新解析，确认完成运行skr_1320eeacdaedf042为succeeded/23步、序列150帧，鱼与动画源码逐字等于StepFun第3次响应、绑定PNG存在且摘要匹配。重新ffprobe仍为5.000秒/1280×720/30fps/150帧/无音轨，MP4摘要未变。证据同目录收尾核验.json。用户随后确认“我觉得可以通过”，成片人工观感已通过；缺陷修复另行记录，不能以该认可替代修复回归。

## QA-FISH-FIX 测试发现问题修复（2026-10-27）

- 当前工作树 `timeline-audio-viz`、应用版本 0.3.1，未提交增量。没有再次请求真实 StepFun，未使用或展示前次密钥。模型协议、长等待和可纠错流程由本地模拟服务验证；原始成片及工程保留。
- 已通过：Node 现有 draw/sequence 套件 21 项（真实本地 Chrome/FFmpeg，包括图片原件与百分号路径、缺图禁止回退、双引擎/动态引用/位移像素、导出和浏览器生命周期）；前端同一组单元 128 项与样式/类型/生产构建；Chrome draw 最终12组、settings最终8组。
- Go service/project/api/preparation/skill/mcpserver六包整包通过（service 475.075秒、MCP最终66.571秒）。MCP首轮两项旧工具清单漏计新增心跳，已更新相同测试并完整复验，Worker/人工权限禁止断言保留。Python最终全套172项通过、跳过1项；之后新增错误流/脱敏边界，受影响模块11项通过。
- 环境记录：本机 `npm run test:unit` 在执行脚本前退出且未提供具体诊断；直接以安装的 Node 执行 package.json 声明的相同测试文件全部通过，不删除失败记录。Python 全套出现既有文件句柄 ResourceWarning，测试未失败。
- 原生入口沿用 `verify-tauri-desktop.mjs`，增加真实 MCP 心跳→可见绘制进度检查，图片改用包含 `%03d` 的原始 JPG；不把模拟模型测试记为真实模型质量验证。
- 可见Tauri最终10项通过：原生JPG导入、拖动/撤销重做、MCP真实心跳显示阶段与128字符、双引擎/图片同画布查帧、源入轨共享渲染、独立视频导出及AVP3保存重开。实际截图已目视复核。该次日志与截图在0.3.2版本轮换时按规则清理，结论保留于本说明；用户未对修复后控件手感重新验收。
- 原生首轮配置把数据放在源码资源目录内，被现有保护拒绝，已改为根测试区独立data；随后发现旧Tauri嵌入前端不能显示新增进度，使用已安装Rust1.98.1离线重建后通过。失败JSON曾保留，已随0.3.2版本轮换清理。调试构建关闭安装包资源打包，显式配置使用新Go二进制及匹配manifest，不代表安装器发布验收。
- 扩展检查：原有Go所有权/过期租约/角色/版本/项目切换与SSE范围测试；图片缺失/错误别名、草稿图片绑定/位移、删除不复活；流式EOF无完成标志、超大事件、截断、超时及错误不回显凭据；未保存服务商high/48000配置跨页保留；绘制任务刷新后由URL恢复真实进度；窄屏640–1440及键盘/减少动态效果。Node另以JPE/TIF扩展相同兼容用例通过；Go `TestProjectExpandedMediaAndMatroskaStreams`含JPE百分号路径及MKV实际多音轨映射再次通过（11.111秒），未发现需新增修复的格式问题。
- 结论：QA-FISH-FIX代码与本地分层验收完成。真实StepFun当时未复跑、安装器及修复后的用户人工手感未代签；当时应用为0.3.1、代码未提交。测试生成物随后按0.3.2版本规则轮换，用户素材与成片已保全。

## REL032 版本升级与发布（2026-10-27，已推送）

应用版本升级至0.3.2。版本/锁文件及Cargo元数据一致；实际Go MCP/project/skill三包通过，MCP初始化在worker与desktop两种连接下均与桌面版本一致；前端128项、样式、类型和构建通过；真实双引擎同帧/透明及图片测试1项、Chrome绘制12组/设置8组、文档契约184 HTTP/17 IPC/157模型/29 SSE通过。代码提交 `dff2f5a1d1fc0f7ebd99a0419fca8f43e091a769` 已推送到origin/agent/timeline-audio-viz并经ls-remote核对；发布状态说明作为后续文档提交。本次没有重跑真实StepFun、安装器或0.3.2可见桌面。

上一版用户确认的成片和素材已复制到根 `.run-data/accepted-projects/2026-10-27-fat-fish/`：原AVP字节备份SHA-256为`6502581a1f6a5b38979ad34db405b152ec4d339283a725722ce79f43adc54ad5`，另有重定位AVP副本；原图/PNG、成片和18项运行产物引用已复制并核对。成片SHA-256为`648b8591bc79ca698e01ec31824a1a74b1f6d97d4541e05767d3cef7cc95c14e`。固定Sony RAW另存根`.run-data/accepted-fixtures/`并复制到当前版本测试缓存，摘要一致。重定位副本已解析，项目、序列、脚本、画面和运行内容与原件一致且引用存在；尚未以0.3.2桌面重新打开验证。

根目录和当前工作树的0.3.1测试目录在核对标记、进程、重解析点和Git跟踪边界后按`Prepare`规则轮换。两测试根`Measure`同口径合计：轮换前10,566,776,555字节、轮换后3,697,773,546字节，减少6,869,003,009字节；保全副本位于统计范围外，因此这是测试存储占用变化，不等同磁盘净释放量。

## MV-SCRIPT01 原生媒体指纹回归（2026-10-30）

当前整合工作树、0.3.2未提交增量。复用 service/project_draw_test.go 增加旧SQL指纹与当前原生读模型不一致时的创建回归；旧调用方指纹仍被拒绝。TestWorkspace custom 运行 service/MCP draw/project 定向检查，先红（conflict）后绿（service 10.009秒、MCP 31.007秒）。输出位于根 .run-data/tests/agent-hook-go-fix/0.3.2/artifacts/custom，保留 native-fingerprint-red.log。Go新侧载二进制实际构建并已载入原生dev；真实MCP任务创建成功。后续绘制请求字段错误与人工恢复独立记录，不以回归通过代表成片质量或人工验收。

## MV-SCRIPT01 / AGENT-AUTO01 自主剪辑（2026-10-30）

真实业务生成：Excute me工程，用户明确批准Remotion替代Motion Canvas并指定60秒。任务skr_6533e12ed09f51bd通过实际MCP读取素材/歌词、保存源码、检查与预览、入轨、配乐合成、run.finish完成。最终MP4为60.000秒/1920×1080/30fps/1800帧/H.264+AAC 48kHz双声道，12,109,023字节，SHA256 d57817f1e6bcdfaa2ca53b758dadfa9006ff7cad949ce48fbcca064a7b2e7e39；基础音画passed，实际编码2/22/36/57秒画面已目视检查。工程保存，源码/分镜/成片位于根development/creative/world-execute-60s（业务产物不归入测试生成物）。无真实外部模型调用，人工观感和逐字听校未代签。

过程中统一文档缺引擎字段被误报授权失效、timeline操作字段op误用（正确为type）引起暂停，先按用户点击恢复，用户继而明确授权Computer use自行点击恢复；预览争用导致finish冲突，源码逐帧路径过慢，最终采用实际Remotion渲染后合成并完成。这些失败未写成成功。

自主执行维护在既有service/project_draw_test.go、skill_harness_test.go及MCP SDK/security/tasks套件，未新增独立runner。新增参数纠错/原生计划自动继续回归先红后绿；既有内置模型桩改为非绘制参数错误反馈，统一Remotion文档不会被误报权限，实际双请求共用预览输出。service 47.087秒、MCP剪辑定向61.840秒、Skill/project定向通过；新增MCP自助恢复真实SDK往返验证所属会话、状态及预算不重置。MCP整包初轮只在旧工具计数17断言失败（新增update后为18），修正断言后另记最终复验。前端134项、最终类型/样式通过；文档191 HTTP/17 IPC/165类型/30 SSE及15文档602链接通过，脚本文档受管验证通过。初次直接调用verify-script-workbench-docs因缺VAC_TEST_OUTPUT拒绝，随后统一入口通过；测试夹具初次空工程未初始化的失败记录保留，后续已复用固定clip_01.mp4正确初始化。

证据在根.run-data/tests/agent-hook-go-fix/0.3.2/artifacts/custom（autonomy-red.log、autonomy-service-green.log、mcp-tool-count-first.log及最后output.log）、agent-auto-webui和agent-auto-docs同版本目录。实际源码为integrate-remote-20261029@a336f4f加未提交增量；根runner的HEAD仅代表入口，不代表被测源码。原生自主执行新版载入及最终MCP整包结果随完成记录追加。

AGENT-AUTO01最终复验：MCP整包74.400秒通过（含新update接口的真实SDK恢复/确认、预算不重置、另一会话拒绝及原安全边界）；前端134项/最终tsc/样式通过。Go当前主程序构建退出0。原生debug窗口PID57092可见，后端PID59984，http://127.0.0.1:1935；真实HTTP MCP initialize→tools/list→DELETE确认18工具含skill_runs.update。Excute me重开并保存，序列revision8、一个源码构图，工程dirty=false。Codex保存的vac配置已更新该端口；当前聊天仍缓存旧46038连接，未宣称原连接已热重连。未再生成第二份一分钟成片、未发布安装器/提交/推送，人工观感待验。

## AGENT-AUTO02 残留人工门槛清理（2026-11-01）

继续integrate-remote-20261029的既有未提交增量，版本0.3.2。复用service/skill_harness_test.go与skill_agent_test.go、MCP SDK生命周期以及Skill builtin_draw_test.go，未新增独立runner或固定媒体。覆盖旧/原生任务自动确认、配音工具与设置API、所属任务步骤诊断、内置模型桩自动继续、绘制诊断工具声明；MCP SDK以Agent发起计划修改/配音设置/局部重剪，实际渲染1.0秒及0.8秒两份交付，无人工定稿调用。任务隔离与取消继续验证。

既有service定向43.532秒通过；最终计划/配音/实际run.steps调用1.113秒、Skill 0.062秒通过；MCP整包最终66.814秒通过。绘制诊断工具声明先红（缺run.steps）后绿。初轮MCP断言仍要求重剪后清空确认、随后将固定计划版本误当成时长，均保留失败记录并改为验证新行为和明确预期时长；未删除片段边界/成片实际时长检查。输出沿用根.run-data/tests/agent-hook-go-fix/0.3.2/artifacts/custom和agent-auto-docs同版本目录，红灯draw-diagnostics-red.log保留。

文档/契约通过：191 HTTP、17 IPC、165类型、30 SSE，16文档605链接。上一轮问题文档置于docs根导致“四份主文档”校验拒绝，已移至docs/development/当前问题清单与修复指导.md并更新链接，未放宽校验规则。本轮无前端产品代码变更，未重跑前端；无真实外部模型或配音质量验收，未替换正在运行的桌面后端、未打包/提交/发布。SDK模拟模型与真实本地渲染不冒充用户模型质量及原生体验验收。

### 2026-11-01 UX-SCRIPT02：移除验收UI、脚本保存与Agent读取

继续integrate-remote-20261029的既有未提交工作，版本0.3.2。复用project_workspace_test.go、project_test.go、skill_run_test.go及现有前端单元/浏览器入口，无新独立套件或媒体。新增覆盖空工程创建脚本、保存后重开、真实HTTP创建、SDK resources.read读取新正文及工程隔离；旧验收链接重定向、无人工定稿按钮、自动采用文稿导出、保存后交接和失败不复制旧任务纳入既有浏览器套件。未清理任何生成物或业务数据。

- Go定向：service/api/mcpserver三包通过，覆盖TestProjectNewScript、TestProjectHTTPRangeAndIsolation、TestProjectMCPResourcesFollowOpenProject和TestProjectScript；分别10.755/1.721/1.011秒。证据：原根.run-data/tests/agent-hook-go-fix/0.3.2/artifacts/custom/output.log。
- 前端全部135项、tsc --noEmit、style检查与生产构建通过。证据：原根.run-data/tests/agent-auto-webui/0.3.2/artifacts/custom/script02-unit.log及script02-build.log。
- 现有run-local-smokes.mjs + script_workbench_smoke.mjs共13项通过，completed=true、errors=[]，1440/800宽度及保存后界面截图已查看。证据：同custom/review-smokes/script_workbench_smoke.mjs/results.json与截图。初轮失败为折叠面板/旧人工定稿按钮的测试预期、导出桩错误递增版本及反馈status角色定位，更新现有夹具与定位后最终通过；不冒称产品故障已修复。
- 已验证真实HTTP/MCP协议与浏览器夹具流程；未重新启动更新后的原生桌面，未调用真实模型，人工体验与成片观感未代签。原生dev记录为stopped；本轮不更新已安装发行版本，不提交/推送。

接口沿用既有资源POST并扩展kind:"script"创建分支，不新增路由；机器清单与接口文档同步。历史验收API/记录保持兼容，当前UI无独立验收页面和操作。

### 2026-11-01 DOC-FEATURE01：活动文档整理与功能说明

仅整理说明与文档维护脚本，不改产品逻辑、运行时Skill或第三方原件。新增功能文档和文档索引，项目总览与README按当前0.3.2工作副本重整；架构/接口修正过期版本、工具数量、绘制接线及人工定稿描述；问题清单保留复现并同步已实现进展。旧根只增加当前入口，不同步旧工作树源码。

复用check-documentation.py和verify-script-workbench-docs.py，精确文档白名单由原四份调整为五份主文档加索引，脚本专项链接检查纳入两份新增说明；未另建测试套件。首轮静态检查拒绝新增文件，原因是旧白名单未更新；按用户明确新增功能说明的范围更新后通过。受管入口首次因受限环境无法读取CIM而未启动，使用允许的进程检查权限后正常执行。接口生成检查通过191 HTTP/17 IPC/165类型/30 SSE；脚本专项通过8文档604本地链接和191处理器位置。全说明最终18份、644本地链接检查通过，errors=[]，详见现有documentation/documentation-check.json。证据目录沿用原根.run-data/tests/agent-auto-webui/0.3.2/artifacts/custom/documentation与script-workbench，不创建日期报告。

未复跑产品单元、媒体、浏览器、原生桌面或真实模型；这些结论沿用上次记录且不扩展为本轮通过。无依赖安装、原始文档恢复副本清理、提交/推送或发行。

### 2026-11-01 DOC-FEATURE02：功能文档定位纠正

按用户澄清，将操作说明改为模块职责和Agent开发合同；覆盖12个功能模块的现有能力/复用入口/不变量/限制/联动，明确禁止重复入口、双重状态、逐步人工审批和无关扩展。项目/索引/架构/README及AGENTS同步其职责，未改产品代码、接口、测试套件或运行时Skill。复用受管check-documentation.py，18份说明647本地链接、191 HTTP/17 IPC/165类型/30 SSE均通过；证据仍写既有custom/documentation/documentation-check.json。文档变更仅做静态验证，未重跑媒体、桌面和模型；不扩展历史通过结论。

2026-11-02 UX-SCRIPT04：脚本移至持续挂载的可调宽右栏，复用现有useResizable，增加分隔条方向键操作。tsc与样式检查通过；现有脚本浏览器用例仍需适配中央工作区变为侧栏的布局预期，本轮未运行视觉回归或新版原生桌面，不宣称视觉验收完成。

### 2026-11-02 QA-SCRIPT04：当前候选整体流程复验（结束，整包仍有未通过项）

复用现有前端单元、run-local-smokes、draw.test.mjs与Go service/api/mcpserver套件。前端135项、类型/样式及生产构建通过；真实双引擎unified frame host检查通过。浏览器首次暴露两个实际问题：折叠侧栏被旧规则压成0px导致无法再展开，以及脚本入口未把右栏计入hasSider导致面板落到内容下方；分别补齐折叠条标记和容器条件，增加实际点击、草稿保留与水平位置断言。截图发现800px断点不匹配，统一到现有959px断点；工程宽度恢复同步720px范围。

旧测试的中央宽度断言和重复监视器定位已更新；dock分段面板原有折叠由用例正常展开。绘制浏览器首次因缺少前置PNG样本退出，按既有Node真实帧测试生成后继续；未复制新样本或新建runner。一次并发受管调用被目录锁拒绝，改串行。最终候选页面回归与Go整包仍在执行，不使用旧summary/output的通过替代在途结果。浏览器使用隔离API/本地模型桩，不调用真实配置模型或修改用户工程；新版原生桌面尚未验证。

QA-SCRIPT04最终：当前前端脚本15项、编辑14组、设置12项、绘制12项全部通过，脚本/设置completed=true，页面errors=[]；1440/800及空工程截图已目视，右栏位置符合当前要求。设置计时首轮受截图耗时跨越边界影响；暂停虚拟时钟尝试导致交互等待超时，最终改为截图之后主动刷新并等待响应，以该次完成为区间起点，三分钟与停用轮询断言通过，未改变产品轮询频率。生产构建/类型、样式、18文档647链接和191 HTTP/17 IPC/165类型/30 SSE合同通过。

Go完整三包：service在600.117秒整包上限退出，超时时正在运行TestAutoRunParksWhenTheVisibleSetChanges（显示运行0秒），不能证明该用例死锁，也不能记整包通过；API26.146秒、MCP92.423秒通过。原根.run-data/tests/agent-hook-go-fix/0.3.2/artifacts/custom/full-packages.log保留完整失败栈；随后同根output.log为service定向60.524秒通过，选择TestAutoRunParksWhenTheVisibleSetChanges、TestProjectNewScript、TestProjectScript、TestSkill及TestDrawBuiltin，覆盖当前脚本/Agent与绘制内部模型桩路径。定向通过不消除整包超时，后续需分段测量整包耗时/退出。

页面日志/JSON/截图位于原根.run-data/tests/agent-auto-webui/0.3.2/artifacts/custom/review-smokes；真实绘制样本由既有draw.test.mjs unified frame host在同custom/draw/unified生成，1项8054ms通过。所有浏览器均隔离API夹具；未启动新版原生桌面、未调用真实模型、未改用户工程、未提交/推送。

### 2026-11-02 QA-SERVICE-TIMEOUT：service整包超时根因调查（完成，优化未实施）

不修改产品逻辑。保留上一轮full-packages.log，对同一现有service包增加-json和CPU profile采样，诊断上限900秒而非宣称修复；生成物均在原根agent-hook-go-fix/0.3.2/artifacts/custom。超时栈当时位于PreparedPreflight→LocalProbe→runVersion→Windows CreateProcess；运行用例显示0秒，不能据此判断数据库死锁。该用例复用同一个已编译测试二进制独立重复5次全部通过，单次2.54–2.75秒，证据为agent-auto-webui同版本custom/auto-isolation.log。本机ffmpeg/ffprobe版本探测10个样本均exit0，首个0.25秒，其余约0.10秒，证据为同custom/probe-timing.json。

次要缺陷证据：旧超时栈有206个Bus.run等待协程；auto_review_test.go的newScoreEnv注册两个订阅却未Cleanup(bus.Close)，newService把service/store存入全局sync.Map却不Delete；部分ConfigureTTS的用例/辅助函数未注册CloseTTS。它们是可确认的测试生命周期遗漏，是否主导本轮超时尚待profile，不冒称主因。

最终证据：同一源码、相同受管环境，增加-json/-cpuprofile且诊断上限900秒的service全包复跑通过，耗时548.453秒；290个顶层用例通过、6个跳过（不冒充通过），无断言失败。顶层通过耗时之和546.36秒，说明本次时长由逐项执行累积；原600秒上限仅约51.55秒余量。TestAutoRunParksWhenTheVisibleSetChanges在整包内2.36秒通过，独立5次2.54–2.75秒通过。原始超时是包级预算耗尽时恰好进入该用例，不能指认为该用例卡死。上一轮还同时运行前端构建/浏览器，本次耗时变化可能受机器负载影响，但未做同负载A/B，不量化归因。

最慢10项合计248.11秒（约45%），其中5个AutoE2E用例合计166.57秒：MediumRevisedThenCompletes53.97、LowRerunsAndMends29.95、FailedPreviewHalts28.43、HighCompletesAndDelivers27.29、MediumWaitsThenExports26.93；另有ProjectRealMediaFramesAndDelivery21.40、UnifiedPreviewDraftIsolationAndOwnedLifetime18.53、ResourceLoudnessConcurrentConflictAndCancelReleasesLock16.14、SkillRealMediaFramesAndDelivery13.69、SkillLightweightBuiltinFromTranscriptToMP411.78。这些覆盖真实Worker/FFmpeg/渲染，不是纯业务单元。

CPU profile在Windows大量归入cgocall/syscall路径，累计栈主要涉及子进程输出管道读取、进程等待、路径查询和SQLite提交/FlushFileBuffers；LocalProbe/runVersion也有可见开销。累计样本含重叠调用栈和多线程，不能直接相加成墙钟耗时，不能由此猜测杀毒软件或磁盘故障。主结论为整包执行成本接近预算；清理遗漏是独立测试维护缺陷，不认定已证明造成本次超时。

证据保存在原根agent-hook-go-fix/0.3.2/artifacts/custom：full-packages.log为原600秒失败栈；service-profile-events.jsonl为本次逐事件记录，service-timing-summary.json为耗时摘要，service-cpu.pprof与service-profile.test.exe用于本地分析；当前output.log为本次完整JSON。后续修复方向：复用既有short与集成覆盖分层；纯业务检查减少重复进程探测但保留真实依赖覆盖；为newScoreEnv、newService注册表及相关TTS夹具补生命周期清理，再同条件重测。不要仅扩大上限后称性能修复，也不要调低SQLite持久化语义以换取测试速度。本轮仅调查和文档，不实施这些优化，不启动用户工程或真实模型。

### 2026-11-02 EDIT-CONTINUE01：生成后局部修改与原生联合验证（完成，MCP连续性未通过）

复用skill_harness_test.go与verify-tauri-desktop.mjs，未另建runner。首先复现两项失败：SaveSkillPlan可接受target范围外旁白改写；局部保存会清空未选片段声音配置。修复后Save与scripts.propose共用enforceTargetedEdit；未选片段完整沿用可信基线，选区内正文允许同段落数更新，其他段落变化被拒绝。现有真实媒体/AVP往返及声音保留三项37.102秒通过；HTTP全包23.473秒、MCP全包90.800秒通过。

在现有原生脚本增加VAC_VERIFY_AGENT_EDIT模式，调试桌面嵌入当前前端和同版Go/Worker资源；数据与生成物均在当前工作树既有agent-ux-browser-final-fix/0.3.2/artifacts/custom/native-agent-edit。首次用video-use创建visual_heavy任务因包只兼容lightweight而拒绝，测试改用vac-visual-summary，非人工审批阻断。原生/真实MCP已完成脚本保存读取、首次渲染检查、局部缩短第一段、保留第二段及人工片段、保存重开和1.7秒成片ffprobe核验。MCP重启端点2633→26932发生变化，保持配置的连接连续性未通过；重新发现端点后可初始化并读取原工程，不能冒称原客户端已自动重连。原生截图另暴露script路由未复用编辑器高度约束，已复用同一CSS并增加节目监视器与时间线工具防重叠断言，最终复验进行中。

默认测试根含历史非版本子目录导致受管入口拒绝启动，改用已有隔离测试根，无清理；声音测试初轮夹具未递增run_version触发唯一键冲突，修正夹具后才复现真实声音丢失。未调用真实服务商模型/云配音；本轮是可见Tauri+真实Go/MCP/Worker/FFmpeg的外部Agent协议流程，不伪称模型理解质量或人工观感已认可。不修改用户工程或安装发行版。

最终候选：调试Tauri/Go/Worker已重建，原生联合模式完成6项检查，5通过、1失败，result=partial。通过：原生脚本保存后MCP读取、首次真实MP4渲染/检查/完成、局部修改后未选段及人工片段完全保留、新成片1.7秒且AVP重开保存、重新发现地址后MCP重新初始化读取工程。失败：保持原安装地址的重启连续性；最终端点4069→46552，与初轮2633→26932一致复现。没有把重新发现连接记作原连接自动恢复。按用户本次先优化3再验证1/2/3的范围，未顺带修改运行端口/客户端身份机制。

script入口高度CSS已复用editing规则；当轮可见窗口截图已查看，节目监视器不与时间线工具重叠的几何断言通过，WebView异常为0。原生结果、OS截图、WebView截图、1.7秒MP4及隔离AVP在当前工作树.run-data/tests/agent-ux-browser-final-fix/0.3.2/artifacts/custom/native-agent-edit；当轮result=partial，后续同一入口复跑结果见MCP-STABLE01，不回写历史失败为通过。构建当轮使用既有agent-ux-build同版本custom/output.log，Go定向及API/MCP在原根agent-hook-go-fix同版本custom。18份文档647链接、接口合同和diff检查通过。真实模型质量、用户素材与安装器发行未验。

### 2026-11-02 MCP-STABLE01：固定MCP入口与重启恢复（实现及原生验证通过）

只扩展现有Rust runtime与可见桌面回归入口，不新增代理、HTTP接口或测试套件。数据目录的mcp-endpoint.json记录schema_version=1和首次成功启动端口；在覆盖desktop-session.json前迁移旧本机地址。无效固定记录拒绝启动，端口占用明确失败且不更换地址。Go既有会话404行为不变，回归客户端在收到旧会话404时重新初始化并重试一次；网络结果不确定的写请求不自动重放。

所有命令通过当前scripts/TestWorkspace.ps1的custom入口。StorageRoot沿用.run-data/tests/agent-ux-build：cargo +1.98.1 test --offline --locked --manifest-path desktop/Cargo.toml endpoint_tests先4项通过；完整Rust全套最终9项通过，0.39秒。覆盖初次动态分配、持久记录不受starting日志覆盖、旧地址迁移、无效记录和远程/带凭证地址拒绝迁移。四项日志为同版本artifacts/custom/endpoint-unit.log，最终全套为output.log。Build-TauriDesktop.ps1 -Debug构建通过（含当前前端/Go/Worker/Rust），未生成安装器或替换已安装版本。

StorageRoot沿用.run-data/tests/agent-ux-browser-final-fix，VAC_VERIFY_AGENT_EDIT=1；运行node scripts/verify-tauri-desktop.mjs 19339 launch <native-agent-edit绝对目录> <该目录desktop.json>。独立stable-data从空目录首次分配，真实可见Tauri/WebView2+Go/MCP/Worker/FFmpeg完成9项，全通过：脚本新建保存并MCP读取；首次成片/抽帧/完成；局部修改保留未选段及人工片段；新MP4为1.7秒并保存重开；后台重启保持URL；原客户端自动处理旧会话；占用固定端口拒绝换址；释放后恢复；完整桌面退出重开后原客户端继续读取。

本轮URL始终为http://127.0.0.1:36825/mcp，原客户端累计重新初始化3次。results.json模式为visible_tauri_live_mcp_script_edit_and_persistent_endpoint、result=passed；证据位于同版本artifacts/custom/native-agent-edit，父目录output.log列出9项结果。mcp-endpoint.json固定36825；最终desktop-session.json为stopped，拥有的测试窗口/后台已正常退出。OS截图agent-edit-desktop.png已检查，节目监视器与时间线不重叠，WebView无异常。

这覆盖前轮1/2/3的可重复协议/桌面流程，不代替真实模型理解、同等规模60秒创作质量或人工观感。客户端恢复由测试客户端验证，当前Codex聊天和其他第三方Agent实现未测；不自动继承旧MCP会话的任务身份/所有权。没有修改用户工程、安装版或远程仓库。

### 2026-11-02 REL033：0.3.3签名发行与本地安装版更新（完成，首轮观察超时后接续验证通过）

当前整合工作树版本递增至0.3.3，同步Tauri/Cargo/npm元数据及锁文件、MCP服务端与probe标识；不改依赖版本。复用verify-tauri-updater.mjs新增显式--installed-update模式，使用指定安装目录的实际exe和默认业务数据，通过生产HTTPS源下载安装、确认原生安装对话框、等待NSIS自动重启，并核对新版本、数据目录、资产、Worker、更新源最新状态和固定端口记录。不复制旧exe、不使用调试更新源、不直接运行安装包代替更新器。

已完成：前端完整135项（69+66）与样式通过；MCP整包129.023秒通过；Rust全套9项通过。接口191 HTTP/17 IPC/165类型/30 SSE、18份说明647链接及diff检查通过。签名正式构建与真实安装尚在执行，后续追加结果。原生回归沿用原入口，不新增固定样本或独立套件。

按规则通过Prepare轮换本轮复用的agent-ux-build与agent-ux-browser-final-fix测试根，旧0.3.2结论保留各根archives/0.3.2.md。相同文件统计范围前后分别6571565→392字节、99859928→435字节，共释放106430666字节；仅清理已标记测试生成物，其他测试根、源码、固定样本、签名密钥和业务数据未动。正式构建证据在agent-ux-build/0.3.3，MCP及后续原生回归在agent-ux-browser-final-fix/0.3.3，前端/Rust与实际更新在既有公开更新验证模式下的0.3.3/public-update/0.3.3/artifacts/custom；后者frontend-unit.log已从共享output.log保留，Rust也在更新测试前保留。

候选结果：Build-TauriDesktop.ps1 -Installer -Version 0.3.3已退出0，当前前端/Go/Worker/Rust正式包已生成；NSIS安装器74947424字节，SHA256 a19f1591a66a3d635de4df7681bc2da81c29ccea3ac5171b47567853d94665c3，更新签名428字节。安装器/签名附件名使用Video.Auto.Cut_0.3.3_x64-setup.exe及.sig，与latest.json URL一致。现有原生联合入口切换为build/desktop/app的0.3.3正式候选，9项全部通过，包括脚本保存、生成/二次修改、1.7秒实际成片、工程重开、后台/完整桌面重启同地址、旧会话恢复及端口争用恢复；测试窗口/后台已退出。此时尚未发布或执行本地安装更新。

发行结果：提交ebbbbc7c894de0be92d5de40a2ca6be2659cc56f与v0.3.3标签已推送并用ls-remote核对，GitHub Release 431188087公开为latest。生产latest.json=0.3.3，清单签名与本地.sig一致，安装器/.sig HEAD均200；公开附件74947424/428/788字节。未改其他工作树与远程分支。

实际安装：运行现有verify-tauri-updater.mjs --installed-update D:/Applications/VAC 0.3.3，原exe真实版本0.2.3、默认业务目录C:/Users/Kirig/AppData/Local/io.kiriukazurei.video-auto-cut/data、资产列表原为空。生产更新器完整下载74947424字节并验签，操作其原生安装确认。旧Go/Worker退出后NSIS解压时间超过观察器原120秒上限，首轮runner失败；UIAutomation确认NSIS仍响应并解压prismjs依赖，随后自动重启的新exe PID50964已经出现。不能将该失败删除或写成首轮全绿；它是测试等待窗口不足，未观测到安装失败。

将既有观察器上限调整为600秒并支持--resume读取同次进度；没有重放下载/安装，也没有手动启动桌面。原更新测试根有其自动重启窗口，受管入口拒绝再次Prepare（空闲保护），故复用空闲的agent-ux-browser-final-fix根运行观察器，VAC_TEST_ROOT仍指向原证据目录。最终results.json同时保留previous_attempt_error、observation_resumed=true及passed/installed/auto_restarted/exited=true。最终输出在agent-ux-browser-final-fix/0.3.3/artifacts/custom/output.log；该目录native-candidate.log保留候选9项，mcp-unit.log保留MCP整包。原公开更新根的updater-first-attempt.log和summary保留原失败，不能用另一runner通过覆盖其历史。

安装后验收：Tauri update_status与Windows文件/卸载注册均为0.3.3；真实原生窗口visible=true，截图installed-ready.png已目视检查“当前版本0.3.3/当前已是最新版本”，最近工程入口仍在。默认数据目录及空资产列表与安装前一致；三个Worker在线，更新检查up_to_date；mcp-endpoint.json迁移并固定原31525端口。独立对比已安装control-plane.exe与正式候选哈希一致，draw-runtime.mjs与当前源码哈希一致。测试正常关闭，desktop-session.json最终stopped，无遗留桌面进程。截图/最终JSON位于0.3.3/public-update/0.3.3/artifacts/custom/updater-installed。

本轮没有回退测试、真实服务商模型调用、用户原工程重剪或人工观感代签；空资产列表一致不代表非空历史库/所有旧版本迁移均已验。后续提交仅包含测试观察恢复与文档，不改变v0.3.3产品二进制，不移动发行标签。
