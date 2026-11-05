# 测试维护说明

更新：2026-11-06。本文是既有测试权威入口，主文档为[项目](../docs/项目文档.md)、[计划](../docs/计划文档.md)、[接口](../docs/接口文档.md)及[架构功能详解](../docs/架构与功能详解.md)。统一多轨编辑沿用现有工程、媒体、前端、Worker 和原生桌面套件，不删除测试源码/样本，不执行产品数据清理。

## 1. 统一规则与源码身份

版本取 desktop/tauri.conf.json，当前0.4.1。测试生成物只放 `.run-data/tests/<完整版本号>/`；固定媒体在 `workers/node/timeline-cli/fixtures/media/`。原录像、正式业务数据、签名密钥、正常开发依赖与 test-support/legacy 恢复副本不属于测试生成物。

`scripts/TestWorkspace.ps1` 统一设置 VAC_TEST_ROOT/VAC_TEST_OUTPUT、TEMP/TMP/TMPDIR、Go/npm 缓存、禁 Python 写缓存及离线 Go。支持 go/python/webui/pipeline/timeline/exporter/guard/workspace/custom。`webui` 现在调用完整 `npm run test:unit`，同时执行样式检查；旧基础子集退出受管默认入口。保留 `pipeline` 作为有明确范围的定向入口。

当前整合源码为 `E:/AI/Video Auto Cut/.run-data/worktrees/integrate-remote-20261029`，分支 `codex/integrate-remote-20261029`，当前基线`d373947`加语法v3、Tab与协同修复，应用 `0.4.1`。本地`checkpoint/0.4.0-before-script-syntax`保留原基线。原根和其他旧工作树保留不同源码，不能用旧根 HEAD 或端口健康证明当前代码。`custom` 的 WorkingDirectory 必须明确；新摘要从实际执行目录记录 head/working_directory，旧摘要若只记录runner身份仍按历史边界解释。

自定义检查使用稳定 `-TestName`，例如 `go-core`、`go-code-service`、`browser-editor`、`doc-contracts`。日志写入 `artifacts/custom/<TestName>/output.log`，summary按 `custom/<TestName>` 分开保存；不同检查互不覆盖，也不把无关检查的通过记成先前失败已修复。不按日期或每次重跑新建StorageRoot；同一检查重复使用同一名称。未命名旧custom结果保留溯源，不作为全套结论。

### 维护索引

| 能力 | 维护位置与入口 | 本轮处置 |
| --- | --- | --- |
| 工程、代码轨与整数帧编辑 | `project/timeline_test.go`、`project/draw_test.go`；Go既有包 | 保留迁移/锁/波纹/引用/独立源码；在原时间线文件补新条任务归属回归。 |
| 参数语法、工作台与颜色 | Go `project/timeline_test.go`、`scriptsyntax/parse_test.go`，前端 `script_workbench_test.mjs`、`gain_envelope_test.mjs` | 维护共享语法/时间/错误、布局保存与局部同步、缩放/透明度/定时字幕、切割回解析、补全与高亮；前端完整清单由package.json唯一维护。 |
| 局部Agent与真实服务 | `service/project_draw_test.go`、`project_workspace_test.go`、`project_media_test.go`、`draw_environment_test.go` | 保留冲突/部分成功/撤销/只读迁移；扩展既有分段用例检查实际声音、跨迁移轨转场与描述缓存。 |
| 三引擎、像素与混音 | `workers/node/timeline-cli/test/{draw,sequence,script-workbench}.test.mjs`；`-Suite timeline` | 保留引擎取帧/寻址/声音，扩展缩放/透明度与CPU/GPU/终渲一致、字幕结束清除、普通段落序列字幕和主动透明质量检查；不另建媒体套件。 |
| Agent模型上下文与Worker | `workers/python/tests/`；`-Suite python` | 保留现有套件，模型桩与真实质量分开；本轮路径辅助函数随命名StorageRoot联动。 |
| 编辑器/工作台交互 | `webui/run-local-smokes.mjs` + `review_shortcuts_smoke.mjs` / `script_workbench_smoke.mjs` | 当前编辑器22组、普通脚本15组；补参数无需Agent、错误草稿保护和命令补全。复用生产构建与本机Chrome，不是原生窗口/真实模型验收。 |
| 导出租约、恢复与资源归属 | `workers/node/export-worker/test/`；`-Suite exporter` | 保留职责独立的套件，不合并进timeline或服务桩。 |
| 输出隔离与安全轮换 | `test-support/test-workspace.ps1`、`scripts/test-paths.mjs`、Python `scripts/test_paths.py`；`-Suite workspace` | 原套件补命名结果隔离/非法名称拒绝；保留标记、重解析点、进程、跟踪文件及失败转通过保护。 |

退役：`webui` 的旧基础子集重复清单、指向不存在 `/workspace/vac-demo/run_browser_tests.py` 的 `test:browser-tts` 别名。TTS单元仍在完整webui套件，浏览器相关交互沿用现有Smoke/原生入口，未删除有效测试或样本。各npm受管别名与入口子PowerShell保留profile。未创建独立runner或固定媒体副本。

不设固定容量上限，不按容量自动终止或清空 artifacts/cache；Measure 仅报告占用。版本变化后 Prepare 按规则提炼旧版本简档并轮换；同版本需要清空时显式 Clear，CacheOnly 保留 artifacts。每版本一份极简 Markdown，最多最近 3 版，未知修复版本/提交写未记录。

任何清理先确认相关进程退出、目标属于明确测试根、目标/父链/子项无重解析点且无 Git 跟踪文件，PowerShell 使用 LiteralPath，并报告同口径前后占用。此条针对生成物；本轮用户单独授权的源文档删除按精确文档清单处理，不把它扩展到源码/固定样本。


## 2. 常用工程检查

Windows 使用系统 PowerShell 7、UTF-8 和已安装依赖，保持 profile 设置。以下从实际源码根执行：

```powershell
$sourceRoot = (Get-Location).Path
$runner = Join-Path $sourceRoot 'scripts/TestWorkspace.ps1'
$managed = @{Action='Run'; StorageRoot="$sourceRoot/.run-data/tests/code-track"}
& $runner @managed -Suite webui
& $runner @managed -Suite python
& $runner @managed -Suite timeline
& $runner @managed -Suite exporter
& $runner @managed -Suite workspace
& $runner @managed -Suite custom -TestName ui-types -Executable node -WorkingDirectory "$sourceRoot/webui" -Arguments @('node_modules/typescript/bin/tsc','--noEmit')
& $runner @managed -Suite custom -TestName go-core -Executable go -WorkingDirectory "$sourceRoot/control-plane" -Arguments @('test','./internal/project','./internal/scriptsyntax','./internal/skill','-count=1')
& $runner @managed -Suite custom -TestName go-code-service -Executable go -WorkingDirectory "$sourceRoot/control-plane" -Arguments @('test','./internal/service','-run','TestCode|TestSegmentInputs|TestSequenceVersion|TestDrawBuiltins|TestDrawingSourceAttachNeedsNoMovie|TestSequenceCurrentFrame|TestDrawReservedTrackImport|TestTimeline','-count=1','-timeout=240s')
& $runner @managed -Suite custom -TestName go-vet -Executable go -WorkingDirectory "$sourceRoot/control-plane" -Arguments @('vet','./...')
& $runner @managed -Suite custom -TestName rust-format -Executable cargo -WorkingDirectory "$sourceRoot/desktop" -Arguments @('fmt','--','--check')
```

完整Go用 `-Suite go`，或显式指定当前源码目录、包串行和超时：

```powershell
& $runner @managed -Suite custom -TestName go-all -Executable go -WorkingDirectory "$sourceRoot/control-plane" -Arguments @('test','./...','-p=1','-count=1','-timeout=600s')
```

嵌套工作树路径较长，Windows目录/临时路径失败须保留原日志；如改用原根短路径runner，明确实际WorkingDirectory，不能误测旧根。Go vet可单独执行，不跳过原失败用例。按变更选择现有包/命名用例；不要在相同结果通过后无理由重复全包。同StorageRoot内串行运行以复用缓存；并发前端可继续使用现有code-ui根，不增加新根。正常失败保留退出码、摘要与日志。

## 3. 文档与接口契约

接口文档自动区域来自机器清单与真实 TypeScript AST。维护契约后显式生成，再运行现有检查：

```powershell
node scripts/update-interface-documentation.mjs --refresh-inventory --write
& $runner @managed -Suite custom -TestName doc-contracts -Executable node -WorkingDirectory $sourceRoot -Arguments @('scripts/update-interface-documentation.mjs','--check')
& $runner @managed -Suite custom -TestName doc-links -Executable python -WorkingDirectory $sourceRoot -Arguments @('-B','scripts/check-documentation.py','--root',$sourceRoot)
& $runner @managed -Suite custom -TestName doc-workbench -Executable python -WorkingDirectory $sourceRoot -Arguments @('-B','scripts/verify-script-workbench-docs.py')
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
| `scripts/verify-tauri-updater.mjs --public-download <已准备的旧版release目录> <当前版本>` | 通过统一 custom 入口运行；复制旧版桌面到独立测试目录，用正式 HTTPS 更新源检查/下载/验签并截取可安装状态，不执行安装；已有用户桌面实例时拒绝启动，停止并退出自身测试进程 |
| `scripts/Test-TauriStateGuard.ps1` / `test-support/test-workspace.ps1` | 现有运行/清理/版本轮换守卫；通过对应 suite，不按本轮再复制独立测试 |

生产 Vite 测试构建的 outDir 显式指向 VAC_TEST_OUTPUT 管理目录。浏览器使用已有 Playwright/本地Chrome，禁止Edge，不重新下载浏览器。没有真实窗口证据时，不把 CDP 端口、进程 Responding 或页面托管记为可见桌面通过。

可见桌面既有调用形状如下；exe、生产前端、Go runtime 清单、desktop.json 和独立测试业务数据须先配套准备，不能把本机历史生成物当作 fresh checkout 自带资源：

```powershell
$env:VAC_DESKTOP_EXECUTABLE = '<已准备并核对版本的候选exe绝对路径>'
$nativeRoot = '<当前版本目录下已准备的原生验收目录绝对路径>'
& $runner @managed -Suite custom -TestName desktop-native -Executable node -WorkingDirectory $sourceRoot -Arguments @('scripts/verify-tauri-desktop.mjs','9223','launch',"$nativeRoot/ui","$nativeRoot/desktop.json")
```

Windows Tauri production overlay 的 frontendDist 使用相对 desktop 目录的路径，避免 E: 被当 URL；测试数据根独立于源码资源根。测试应自己在管理器内部启动和退出，不预启窗口绕过在用进程守卫。正式打包仍使用 Build-TauriDesktop.ps1，不以测试 overlay 发行。

统一编辑器的可见桌面检查已归入同一 `verify-tauri-desktop.mjs`：通过所属进程核验的 Win32 鼠标/键盘执行拖入、取消、移动、右键和快捷键，CDP 只读取坐标和状态；同时验证中文重名补全、绑定改名、实际 MCP 结构化编辑、节目/原生导出同哈希、紧凑布局和多序列保存重开。六类资源及工程隔离的原断言继续执行，旧双入口界面断言随产品更新。`VAC_DESKTOP_KEEP_FAILURE=1` 仅用于失败后保留窗口诊断，失败仍以非零状态退出。

本轮维护前端 `script_workbench_test.mjs` 的稳定绑定/UTF-16/吸附/链接断言，更新 `test_suite.mjs` 的 SSE 查询领域；Python 在既有 `test_skill_runtime.py` 内检查序列、引用和工具契约传给模型。AI 画面修改接口默认 501，未调用服务商；确定性媒体检查不代表外部模型质量或人工观感通过。

`webui/run-local-smokes.mjs` 服务生产构建，`review_shortcuts_smoke.mjs` 当前22组覆盖统一编辑器、播放接管、多选代码条、参数工作台及局部生成。写请求只记录到本机夹具，不当成真实Go编辑或模型验证。已不挂载的quickstart卡片检查退出默认入口，原源码保留溯源；不把旧CR03计数当当前结果。生产构建写入VAC_TEST_OUTPUT/ui-dist，浏览器运行前令VAC_DIST指向它，再从webui目录通过命名custom执行 `node run-local-smokes.mjs review_shortcuts_smoke.mjs`。

本地 ASR 的默认单元/协议测试不下载模型；真实识别复用 service 现有 `TestProjectLocalTranscriptionRealRuntime`。仅在显式设置 VAC_ASR_TEST_MODEL（完整本地模型绝对目录）与 VAC_ASR_TEST_PYTHON（已安装 faster-whisper 的 Python）时执行，可选 VAC_ASR_TEST_AUDIO。未给音频时用 Windows 本机 SAPI 在 t.TempDir 生成语音；模型、依赖及原媒体不修改。用同一环境执行上面的 Go 全包，覆盖环境诊断、实际词时标、幂等重放、取消和跨工程拒绝；英文合成语音不能代替中文/长录像质量验收。桌面脚本同样读取这两个环境变量以检查原生选择、依赖诊断与预设持久化。

## 0.4.1版本与源码推送（2026-11-06 VERSION041）

按用户授权，将本地语法v3、Tab补全及系统协同修复一并纳入0.4.1源码。桌面tauri/Cargo/npm、前端npm及两类MCP服务端/探针版本统一为0.4.1，Cargo.lock仅改应用包条目，不修改同号依赖。

现有code-track/code-track-go/code-ui及原根agent-ux-go通过各自统一Prepare轮换至0.4.1，旧0.4.0生成物已归档提炼并清除；下方0.4.0明细路径是历史位置，不再承诺可直接打开。测试源码、固定样本、正常依赖、调试候选与业务数据保留。Prepare未输出完整的清理前占用，本轮不估算释放量；仅按标记/重解析点/进程/跟踪文件守卫进行入口轮换。

本轮版本字段及应用锁条目一致。前端146项（74+72）、样式/类型通过；日志code-ui/0.4.1/artifacts/custom/version-check/lock-parser-attempt.log保留该次单元结果及随后PowerShell锁文件空键解析错误。改为AsHashTable并启用终止错误后，版本和Cargo/npm锁文件、196HTTP/17IPC/171模型/30SSE及18文档666链接最终通过，当前version-check/output.log为准确结果。MCP两类服务端×四协议8个子场景通过，0.854秒，code-track/0.4.1/artifacts/custom/go-version/output.log。

媒体与原生此前验收保持实际范围，未将它们改写为0.4.1新版重跑通过。公开安装版仍0.3.4，开发调试候选仍为此前0.4.0，本轮不创建Release或重建安装器。当前检查完成，提交与远程核对随后记录。

实现提交8089f7e700ce60391cb64a4f17e358f7b0842fd2（57文件）已正常快进推送origin/codex/integrate-remote-20261029，git ls-remote与本地实现HEAD完全一致；暂存差异空白检查通过。无测试生成物、业务AVP、安装包或密钥入库。本记录作为随后文档提交；源码推送不代表安装版或Release更新。

## 系统修复（2026-11-05 SYSTEM-FIX，本轮修复与对应验收完成）

最终结论：发现的问题已完成对应修复及定向跨模块复验。305项service（四组）、105项完整媒体、146项前端（74+72）、编辑器23组与普通脚本15组、40项治理通过。原生编辑/标记/脚本绑定/MCP/成片哈希/紧凑画面/AVP3重开8项通过后，保存恢复阶段沿用同一入口独立复验2项通过；这不是原默认全套从零一轮全绿，旧原生结果仍按原失败保留。其他17个Go包沿用本轮此前未受实现变更影响的通过证据。

最终原生重开证据：code-track-go/0.4.0/artifacts/custom/desktop-reopen/{output.log,ui/results.json}，result=passed。媒体恢复头119、readyState=4、currentTime=3.966666秒（约119/30秒）；随后显式保存工作区、刷新，选中资源/播放头保持且无虚假dirty。该阶段使用最新调试候选，新WebView媒体会话；后端受管数据缓存可复用。失败前新数据目录的buffered=[2,3.98802]却time=0/ready=1已被实际修复；不以模拟通过替代本项结果。所有测试拥有的窗口已退出。

最终Debug候选通过Build-TauriDesktop -Debug，runtime清单/Go和源码依赖一致；前端包为index-DPE2ZyB6.js。产品修复限于源/节目播放器归属和分段恢复时序；其余为既有测试、原生输入与统一执行入口维护。版本0.4.0未变，未发行、未更新安装版、未提交/推送。真实模型质量、其他工程组合及人工观感未代签。以下保留逐步定位过程，不以其中的“待验”替代本段最终状态。

先定位再修复，保留下面SYSTEM-INTEGRATION原红灯，不将验证设施问题直接归为产品缺陷。

- 原生取消后第二次移动使用了旧坐标：实际纵坐标1029→1133，差104屏幕像素。原入口改为等待布局连续稳定、滚动归零并重读目标；取消/再次移动及全部联动音轨更新已真实通过。窗口悬停、首帧和前台获取补有界同步，仍检查所属窗口/输入范围，不向其他窗口发输入。新增VAC_VERIFY_TIMELINE=1复用原入口定位，不删除默认全链路。
- service回归证实测试辅助表保留Service/Store，事件Bus未关闭；newService清理时关闭订阅、删辅助表、关闭数据库。fixture-release-red.log保留红灯，修复后心跳/真实自动交付及释放回归39.402秒通过。service-before-cleanup.jsonl显示253个顶层用例已累计567.95秒，600秒包级总时限并非单个用例死锁。既有go入口自动发现全部service测试，每80项一组、保留600秒和全部子场景；305项四组分别257.256/95.870/115.866/85.561秒通过。当前复验使用同一入口脚本文本，其他17包本轮此前已通过，未重复未受影响包。原根agent-ux-go/0.4.0/artifacts/custom/output.log为分组结果；原完整超时日志保留。
- timeline入口设--test-concurrency=1，避免不同测试文件同时占浏览器/FFmpeg。未修改GPU的120秒时限、像素阈值或跳过用例；完整105/105、237.560秒通过，GPU26.641秒。code-ui/0.4.0/artifacts/timeline/output.log为新结果，system-integration-timeout.log保留旧超时。工作区治理40项通过；两条scan警告为故意错误夹具。
- 继续协同排查发现产品缺陷：源监视器额外挂载ProgramPlayback，与节目监视器共用e.program/e.playback和序列时钟。原浏览器套件新增“打开/关闭源监视器仍只保留一个节目播放器”断言，先红（源内1实例），去掉源内错误实例后转绿。源素材仍有自己的播放器。前端144项/样式/类型/生产构建和编辑器23组/普通脚本15组通过，证据code-track-go/0.4.0/artifacts/custom/browser-editor/，duplicate-player-red.log保留红灯。更新调试候选和真实原生复验中；不把浏览器夹具当原生结果。

上述路径除明确原根外相对当前源码.run-data/tests。当前仍d373947加工作区修改、0.4.0；保留业务工程/旧失败/测试样本，未提交、推送或更新安装版。

后续原生闭环收口：更新默认折叠的源监视器入口、已退役的面板选择器和播放器控制方式，保留画面完整/原生键盘操作/导出哈希等断言。当前候选已真实通过8项（含源内无节目播放器、预览导出哈希相同、AVP3重开）；最后保存选择/页面刷新用受管AVP独立复验，按AVP实际selected_resource与显式工作区导航验证，不硬编码最初素材。

冷数据重开又复现分段播放竞态：播放头119/30秒，实际buffered=[2,3.98802]，video.currentTime=0、readyState=1，静帧正确而视频一直未就绪。SourceBuffer updateend可先于loadedmetadata；原程序此时检查video.buffered为空，末段已追加完，之后不再寻址。现有desktop_test增加真实事件顺序替身，先红（时间仍0），等待metadata后再检查/寻址转绿。完整前端现在145项（73+72）、样式/类型通过；frontend-check/metadata-restore-red.log保留红灯。当前正编译该修复的调试候选，真实MSE重开尚待复验，未以模型替身通过替代原生结果。

原生复验进一步表明metadata已到时video.buffered仍可能落后于SourceBuffer，单等metadata仍不充分。保留该次失败与CDP中断，现有回归扩为metadata早/晚两种顺序；早到场景先红，改用已确认追加的SourceBuffer范围寻址后两种均绿。最新前端146项（74+72）、样式和类型检查通过，code-ui/0.4.0/artifacts/custom/frontend-check/output.log；ranges-lag-red.log保留红灯。Go/Node媒体未受此UI时序改动影响，不重复已通过的305/105项；实际桌面重开仍在复验。

## 系统协同检查（2026-11-05 SYSTEM-INTEGRATION，检查完成但系统验收未通过）

源码为当前整合工作树d373947加本地未提交代码轨语法与Tab修改。按链路验收：工程/资源导入与隔离→脚本稳定引用→MCP/Agent编辑→时间线事务/声音→真实Worker预览和正式导出→AVP保存重开；同时覆盖取消、旧修订、缓存失效与进程释放。路由夹具和模型桩单独标记，不以单模块结果替代真实协作。

| 边界 | 既有验收入口与本轮状态 |
| --- | --- |
| HTTP/MCP→服务→工程/运行/Worker | 原根短路径TestWorkspace custom，实际cwd为当前control-plane，`go test -p 1 ./... -count=1 -timeout=600s`退出1。17包通过，含HTTP/API52.510秒、MCP285.148秒；service601.738秒总超时。完整日志已保留在原根agent-ux-go/0.4.0/artifacts/custom/full-system-go.log；摘要runner HEAD不代表当前源码。 |
| Python内容/Agent协议与发布恢复 | 当前code-ui/0.4.0/artifacts/python/output.log：174项、124.057秒，退出0，3项按环境跳过；模型桩不等于真实服务商质量。 |
| Node三引擎→共享合成/字幕/音频→交付 | code-ui完整timeline共105项：104完成、1项取消（WebGL一致性120秒超时），整套退出1、304.586秒。原日志timeline/system-integration-timeout.log保留；结束冷构建后原用例专项35.996秒通过，custom/node-syntax/output.log，不据此抹掉并行全套红灯。exporter53/53、4.288秒通过。 |
| 可见桌面→真实Go/MCP→预览/导出→保存重开 | 当前Debug候选构建退出0，Rust4分27秒；清单核对Go二进制、运行时sequence源码哈希与当前源一致。原生desktop-native链路5项通过，之后取消拖动再移动仍at=0、revision=2，等待at=30超时，退出1。后续标记/导出/多序列重开未执行；不是端口就绪即通过。 |
| 独立可见桌面→MCP局部编辑→媒体→持久化/后台恢复 | 同一当前候选、另一个独立desktop-agent/data，原入口VAC_VERIFY_AGENT_EDIT=1，9项通过：脚本保存/MCP读取、真实MP4、局部编辑保留其他片段、1.7秒成片/AVP重开、端点与旧会话恢复、端口占用拒绝漂移、释放恢复及桌面退出重启重连。截图已实际检查。没有调用真实模型，不表示其生成质量通过。 |

原生证据在code-track-go/0.4.0/artifacts/custom/{integration-build,desktop-native,desktop-agent}/；两次原生ui/results.json、失败failure.png和Agent截图均保留，窗口已由入口关闭。上述路径除明确原根外均相对当前源码.run-data/tests。没有另建runner、复制固定样本、删除旧失败或改业务工程；调试候选为正常build/desktop/app-debug开发产物，不更新安装版。

开放项：①原生verify-tauri-desktop.mjs:85的取消后再次移动未提交；failure.png底部仍提示“已取消拖动”，需区分产品状态残留与Win32输入自动化问题，暂不宣称根因。②service整包600秒预算耗尽，当时TestSkillBlackWarningDoesNotBlockCompletion仅运行2秒，不能认定该用例死锁；独立复验10.244秒通过（原根custom/output.log），整包仍红。日志另有最近项目写入Access denied警告，生命周期/目录访问需继续定位。③GPU并行负载下超时而专项通过，尚未证明争用根因或修复稳定性。

本轮结论为部分核心跨模块链路已证实、系统验收未通过。后续优先复现原生手势和服务生命周期，再复验并行GPU链路。真实外部模型质量、全部人工操作/听感及安装升级仍待验。本轮只执行检查、构建调试候选和维护记录，未修改产品实现、提交或推送。

## Tab补全（2026-11-05 SCRIPT-TAB，0.4.0本地增量）

沿用review_shortcuts_smoke与script_workbench_smoke，在原语法/工作台用例中检查代码条Tab补全命令和字段、方向键选中的候选、接受后焦点保留、Shift+Tab反向导航、无候选Tab导航及输入法组合输入保护；普通脚本同时验证原Tab行为和反向导航。不新增套件、样本或输出根。

当前源码仍为d373947加工作树修改。前端144项（72+72）与样式通过，日志保留于code-track-go/0.4.0/artifacts/custom/browser-editor/tab-build-routing-failed.log的单元阶段；该次后续构建因本机npm参数转发误将旧输出当源码根，浏览器仍红，不能作为产品通过证据。随后直接调用现有tsc/Vite CLI，从实际webui源码构建3186模块，11.78秒退出0；同一最新构建的编辑器22组及普通脚本15组通过，当前output.log和review-smokes两份套件日志为最终证据。最初缺少Tab的回归红灯在tab-completion-red.log，失败保留、最终已修。

以上路径相对当前源码.run-data/tests/。工作台键盘提示、功能与接口说明同步更新；没有更改后端/参数语义，不重复Go或媒体全套，原生桌面安装包未验。本轮输出仍归同一0.4.0受管目录，没有清理旧失败或业务文件。

## 本轮语法v3（2026-11-05 SCRIPT-V3，0.4.0本地增量）

执行身份为`d373947+working-tree`，基线已打本地标签，未推送新改动或制作安装包。沿用既有套件、固定样本和三个活动测试根，不创建独立runner、不清理历史失败。旧计数不替代本轮结果。

| 检查 | 本轮实际结果及证据 |
| --- | --- |
| Go工程/语法/Skill完整包 | 最终退出0，分别2.130/2.032/2.517秒；`code-track/0.4.0/artifacts/custom/go-syntax/output.log`。覆盖作者文本、补描述回显且不改参数、删指令恢复默认、时间/范围、转义、切割后缩放/透明度/字幕及重新解析一致。Go vet及嵌入Skill包另在custom/skill-contracts通过。 |
| 相关Go服务与真实Worker | 最终53.458秒退出0，含AVP保存重开、目标隔离/部分失败/波纹/撤销、真实脚本渲染和并发保存。日志在**原根**`.run-data/tests/agent-ux-go/0.4.0/artifacts/custom/output.log`；实际WorkingDirectory为当前整合工作树的control-plane。原根旧runner摘要只记录runner HEAD，不代表执行源码身份。 |
| 前端完整单元/生产构建 | 最终144项（72+72）、样式、TypeScript和Vite生产构建退出0；`code-track-go/0.4.0/artifacts/custom/frontend-check/output.log`。AntD的use-client构建警告保留，非错误。 |
| 浏览器工作台 | 编辑器22组及普通脚本15组通过；`code-track-go/0.4.0/artifacts/custom/browser-editor/review-smokes/`两份原套件日志。包含保存原生参数不调用Agent、空行保存、无效101%草稿不提交、命令补全；同目录上级`code-track-ui.png`已目视检查。写API为本机夹具，真实持久化由Go服务测试覆盖。 |
| 完整Node媒体与补查 | 完整105/105、119.108秒；`code-ui/0.4.0/artifacts/timeline/output.log`。三引擎真实像素/声音、动态缩放与旋转、透明度、CPU/GPU静帧与电影、字幕结束清除和普通段落序列字幕通过。此后补透明度质量排除的局部效果范围，最终相关8/8、5.159秒，custom/node-syntax/output.log；未因单个质量检查变化再跑无关引擎全套。 |
| 文档合同 | 196 HTTP/17 IPC/171模型/30 SSE、18文档666链接、工作台8文档626链接全部通过；`code-track/0.4.0/artifacts/custom/{doc-contracts,doc-links,doc-workbench}/output.log`。两组Agent说明与嵌入副本SHA256相同。 |

上表除明确原根一项外，路径相对当前源码`.run-data/tests/`。首轮Go长路径下libass打不开临时ASS；仅改变受管输出为已有短路径根，相同真实Worker用例及最终相关服务通过，未据此改产品、关闭整个Windows文件系统问题或声称完整service通过。短路径重跑仍使用当前源码，GOCACHE复用当前code-track缓存，TEMP/GOTMPDIR由原统一入口管理。

修复前证据保留：Go的`go-syntax/{initial-red.log,numeric-review-red.log,description-review-red.log}`；Node的`custom/node-syntax/{initial-red.log,pixel-mismatch.log,opacity-review-red.log,opacity-scope-red.log}`和`timeline/first-syntax-failure.log`。分别反映新增语法、作者文字/补描述回显丢失、前后端数值不一致、动态缩放尺寸、过期字幕、主动透明误报及局部范围；最终按所属包/套件及质量专项验证修复，不把无关绿色结果写成修复。没有删除有效旧覆盖、固定媒体或业务文件。

最后一次前端构建使用默认dist，随后核验5个生成文件、无跟踪项/重解析点并移入上述frontend-check/ui-dist；同目录build-output-relocation.json保留文件哈希和源/目标回执。当前生成物均归受管0.4.0目录；后续复跑构建使用`node node_modules/typescript/bin/tsc --noEmit`及`node node_modules/vite/bin/vite.js build --outDir "$env:VAC_TEST_OUTPUT/ui-dist"`，避免本机PowerShell/npm转发吞掉--outDir并将目录误当源码根。

仍未执行：全仓Go/Python/exporter/Rust重跑、真实外部模型质量、0.4.0原生Tauri/安装器及人工听感。历史全量失败保持开放。本轮仅实现线性整画面参数和字幕；缓动、元素级目标、新旁白语法与表达式执行没有实现。

## 0.4.0版本与源码推送（2026-11-04 VERSION040）

按用户指令同步桌面tauri/Cargo/npm及前端npm清单和锁文件；MCP两种服务端及SDK探针也同步0.4.0，原0.3.3/0.3.4版本不一致已通过本轮协议复验关闭。此次推送包含此前代码轨和复审修复，不创建Release、不更新安装版。

三个活动测试根code-track、code-track-go、code-ui通过统一Prepare轮换到0.4.0，0.3.4结果提炼到各自archives/0.3.4.md；下方0.3.4日志/截图路径仅为原始证据位置，版本轮换后已移除，不再声称可直接打开。完整旧Go失败、定向修复与未验边界保留在本说明和简档。0.4.0只报告实际重跑的检查，不把此前全套计数改写为新版全套通过。

0.4.0当前检查：版本字段与锁文件一致；`TestSDKProtocolNegotiationAndCalls`通过（worker/desktop各4种协议，共8个子场景，0.695秒），日志为code-track/0.4.0/artifacts/custom/go-version/output.log。前端完整142项/样式、TypeScript、196 HTTP/17 IPC/170模型/30 SSE及18文档665链接通过，日志为code-ui/0.4.0/artifacts/custom/version-check/output.log；初次文档命令传入相对root导致路径误定位，修正绝对root后复验通过，relative-root-failed.log保留调用错误。未重跑完整Go/Node/Python、真实模型或0.4.0原生安装包。

轮换前后统计范围相同（三个活动测试根），1229789502→3599字节，释放1229785903字节；只删除入口核验过的旧受管生成物，源码/样本/业务工程/开发依赖保留。新0.4.0检查随后重新产生缓存，不把轮换后3599字节当最终占用。

实现提交ff636cac3576042976ae994ee04039c61649cb4e（83文件）已正常快进推送origin/codex/integrate-remote-20261029，远程HEAD与本地实现提交逐字一致；提交前差异空白检查通过。无测试输出、业务AVP、安装包或签名文件入库。本记录为随后的文档提交；0.4.0没有创建Release或更新用户安装版，旧Windows全量失败及未验范围保持开放。

## 前次审查与测试整理（2026-11-04 CODE-REVIEW，0.3.4）

源码为上述4496ace加未提交改动，版本0.3.4。本轮先审查代码轨、再维护原套件；没有新增独立测试体系。

确认并修复：分段预览漏掉代码声音资源、迁移轨间的转场前条源尾部未入分段、描述/任务元数据误入分段缓存、新实例继承旧实例任务、颜色随其他条增删漂移、移动草稿误改转义及字幕内文字。截图另发现多选描边盖住身份色，增加条内色标，与脚本卡片一致。

| 验证 | 当前结果与可复跑证据 |
| --- | --- |
| Go模型/语法/Skill | 三个完整包通过，code-track/0.3.4/artifacts/custom/go-core/output.log。 |
| Go相关服务 | 最终35.109秒通过，含实际声音分段/同缓存复用、迁移转场依赖、局部生成/冲突/撤销与只读迁移；code-track当前版本artifacts/custom/go-code-service/output.log。转场查找按组建索引，避免长时间线平方扫描。 |
| 前端完整单元 | 新的统一webui入口142/142（72+70）及样式通过；code-ui当前版本artifacts/webui/output.log。 |
| 工作区治理 | 40项生命周期检查通过；code-track当前版本artifacts/workspace/output.log。包含新命名隔离、非法名称和Node/Python命名StorageRoot兼容。两条Storage scan警告为故意注入的错误保护夹具，不是运行失败。 |
| Python全量 | 174项结束、3项按环境跳过，退出0；code-ui当前版本artifacts/python/output.log。此前WinError 5用例本轮通过，不宣称已经找到并修复该间歇文件系统问题。 |
| Node完整媒体 | 104/104通过，107.458秒；code-track当前版本artifacts/timeline/output.log。 |
| 最终界面 | 最终色标样式/类型/生产构建/Chrome20组通过；code-ui当前版本artifacts/custom/browser-editor/output.log和review-smokes/review_shortcuts_smoke.mjs.log。截图code-track-ui.png已复核；新增可见色标与文本卡片同色断言。 |
| 文档合同 | 196 HTTP/17 IPC/170模型/30 SSE、18文档665链接及工作台8文档625本地链接通过；code-ui当前版本artifacts/custom/{doc-contracts,doc-links,doc-workbench}/output.log。差异空白检查通过。 |

以上路径相对当前源码的`.run-data/tests/`。本轮通过命名日志整理当前证据，不删除固定媒体、测试源码、业务工程或旧全量失败。先前匿名custom摘要混有不同命令，仅作历史记录；当前结论按上表命名记录判断。红灯保留在code-track的custom/review-identity-red.log、custom/review-segment-red.log、workspace/review-names-red.log、workspace/review-paths-red.log，以及code-ui的custom/review-ui-red.log、custom/review-transition-red.log。code-track-go的custom/browser-color是修复前色标红灯，不作为修复后全套结论。

Go完整套件本轮未重跑；原MCP版本不一致和Windows目录失败尚未关闭，见下方CODE-TRACK原验收。未执行真实外部模型质量、最新原生Tauri/安装包或人工听感验收；未提交、推送、发行或更新安装版。

### 前次实现验收（保留范围，不替代本轮）

### 2026-11-04 CODE-TRACK

实际源码为当前整合工作树 `codex/integrate-remote-20261029`，基准 `4496ace` + 未提交改动，应用仍0.3.4。仅使用现有 project/scriptsyntax/skill/service、前端脚本/增益线、review_shortcuts_smoke、Node draw/sequence、Python skill_runtime 套件及共同夹具；没有新增独立runner或测试套件。原“源码混入视频轨”的断言改为源码进入代码轨；保留普通视频、MP4和旧格式覆盖，新增代码条的原文件不改写、独立生成、参数/声音/撤销覆盖。

| 层次 | 本轮结果与证据 |
| --- | --- |
| 前端工程 | `npm run test:unit` 140/140；tsc、样式、生产构建通过。最终命令含单元→类型→构建→浏览器，记录在 `.run-data/tests/code-ui/0.3.4/artifacts/custom/output.log`。 |
| 浏览器交互 | 既有 `run-local-smokes.mjs review_shortcuts_smoke.mjs` 20组通过；包含代码条缩略图与增益线、顺序与颜色、自动保存、只生成改动条及选中轨可见。该root下 `review-smokes/review_shortcuts_smoke.mjs.log` 和单张 `code-track-ui.png` 为当前证据；本机API夹具，不是实际模型运行。 |
| Node真实媒体 | 完整timeline 104/104，通过三引擎实际浏览器帧/反向寻址及共同合成；记录在 `.run-data/tests/code-track/0.3.4/artifacts/timeline/output.log`。最后声音质量联动后，既有sequence文件20/20复验通过；包括三引擎单帧/电影像素和代码音量，未降低正式导出采样。 |
| Go相关 | project、scriptsyntax、skill整包通过；service按 `TestCode\|TestTimeline\|TestDrawBuiltins\|TestDrawingSourceAttachNeedsNoMovie\|TestSequenceCurrentFrame\|TestDrawReservedTrackImport` 定向通过。真实服务调用Node首帧检查；覆盖一条变多条/波纹、任务状态不推进编辑修订、人工新描述不被覆盖、失败源码保留、一次撤销、旧AVP打开不写盘。最近整包输出在code-track当前版本custom/output.log，服务详细命令同下方复跑入口。 |
| Python | 完整174项曾有1项 `test_stop_command_and_heartbeat_use_flat_fields` 的 WinError 5、3项跳过；失败记录保留在code-track当前版本python/output.log。受影响Agent上下文12项及该失败用例随后13/13定向通过；不把一次定向通过改写为完整套件重新通过。 |
| 文档与合同 | `node scripts/update-interface-documentation.mjs` 与 `python -B scripts/check-documentation.py --root .` 通过：196 HTTP、17 IPC、170模型、30 SSE，18份文档665链接；差异空白检查通过。 |

Go全量失败保留在 `.run-data/tests/code-track-go/0.3.4/artifacts/go/output.log`：`TestSDKProtocolNegotiationAndCalls` 的原MCP版本0.3.3/桌面0.3.4不一致；`TestTickConfirmsScenesApprovesNarrationAndIsIdempotent`、`TestRelaxedThresholdsAreRecordedInTheAuthorization` 的Windows目录rename拒绝访问；`TestVariantRejectsTamperedASSDespiteUpdatedManifestAndPassedReport` 的临时目录mkdtemp失败。本次联动失败（源码轨类型、后台任务修订竞争、Skill文档漂移）已修正并定向通过。首轮API幂等键500在随后API整包回归通过；首轮因编译期间增加源码而失败的service构建不算本轮最终验证。不得据此声称Go全量已通过。

复跑仍走 `scripts/TestWorkspace.ps1 -Action Run`。Node用 `-Suite timeline`；Go相关包和前端完整单元用 `-Suite custom -Executable go|npm.cmd -Arguments ... -WorkingDirectory <实际源码目录>`，浏览器使用已有run-local-smokes并将VAC_DIST指向版本目录内构建。测试根通过 `-StorageRoot` 隔离在上述三个根：统一入口有目录锁，code-track与code-track-go还存在前缀占用识别，故前端使用code-ui；不绕过入口或清理未知目录。缓存、截图、临时媒体只在各自0.3.4目录，后续按既有版本轮换规则处理。

真实外部模型质量、原生Tauri和安装包、人工视觉/听感仍未执行。未推送/发行、未更新安装版，未用业务AVP作为测试夹具。Motion Canvas依赖精确锁定3.17.2，复用已有浏览器与FFmpeg；桌面既有src/node_modules递归资源规则覆盖新运行时，未以静态资源检查替代实际安装包验证。

## 5. 历史验证与失败溯源

历史条目只解释当时源码和失败修复轨迹；其中部分输出已按版本规则清理，不能视为当前可用证据或可直接执行命令。当前维护入口与结果以上文为准。

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

### 2026-11-02 QA-EDIT-INSTALLED01：生成后继续修改（实际安装版9项通过）

用户指定验证“生成后继续修改”。源码/测试入口为整合工作树0a40537（仅新增本轮文档记录），实际执行D:/Applications/VAC/video-auto-cut-desktop.exe 0.3.3及安装目录的control-plane.exe/runtime，不使用开发版替代；现有verify-tauri-desktop.mjs的VAC_VERIFY_AGENT_EDIT=1模式、19339端口，仍经TestWorkspace custom与agent-ux-browser-final-fix根运行。复用native-agent-edit输出，将desktop.json指向安装版资源与隔离installed-data。用户默认业务目录及工程未改，没有重新构建、安装、调用真实服务商模型或发布。

结果9/9、result=passed：原生右栏新建保存脚本→真实MCP读取→首次生成2秒MP4并检查/完成→target选择第一段→第一段1秒缩至0.7秒并更新其正文→重新渲染/检查/完成得到1.7秒新MP4。第二段与修改前深比较一致，人工视频/音频片段深比较一致，新旧产物哈希不同；ffprobe时长与AVP保存重开通过。独立读取本轮磁盘agent-edit-47284.avp确认AVP3、原人工30帧片段与新51帧产物仍在。

后台和完整桌面重启均保持http://127.0.0.1:52216/mcp，同一回归客户端自动重新初始化；固定端口被占用时明确报错、不换址，释放后恢复。真实原生窗口与新截图已查看，节目监视器不遮挡时间线的边界断言通过，WebView无异常。最终installed-data/desktop-session.json为stopped，测试程序已正常退出。

最新证据替换现有native-agent-edit同一入口的results.json、edited-program.mp4、agent-edit-desktop.png、native-agent-edit.png与desktop.json，数据为installed-data；该目录先前data为正式候选历史测试，与本轮安装版分开。父级custom/output.log为本轮9项输出；既有native-candidate.log与MCP/更新验证独立记录仍保留。不把合成测试素材与确定性MCP调用写成真实模型理解、用户原片重剪或人工观感通过。

### 2026-11-03 MV-CONTINUE01：生产试用实际创作续接

实际0.3.3安装版/当前Excute me工程，通过原生MCP读取歌词、序列及Skill合同；用户选择Remotion+TypeScript+SVG替代Motion Canvas。歌曲实际为OGG，源120–180秒制作60秒新片；41歌词提示点分镜基于工程中英歌词与旧LRC，未声称新增逐词转录。源码draw.save/check、实际preview/render与图帧检查后导入新轨组；timeline.edit将旧成片放0–60、新画面放60–120，原音频0–60接120–180，原有轨道深比较完全一致。

任务skr_eaf2747efb3735f6结构化run.finish=succeeded，序列revision13；最终MP4 SHA256 a5598d165073fb1152d078389476623b0543692af726e0a823b27cca40ce55b9，27564291字节；基础video/audio均passed、issues为空。独立ffprobe确认1920×1080/30fps/3600帧/H.264+AAC 48kHz双声道/120.000秒。已查看新片1/7/30/58秒和成片编码59.9/60.1/90/119秒。AVP保存后dirty=false，源码规范化CRLF/LF后与交付TSX一致，1800帧（首轮逐字符断言仅因换行格式不同失败，核查确认不是源码内容差异）。工程未关闭重开，原磁盘版本已备份。

这是业务制作，不是固定样本回归；产物在原根.run-data/development/creative/world-execute-120-180：120秒MP4、分镜说明、IllegalLove.tsx、cues.json、制作结果/成片核验及编码抽帧，源码亦保存在AVP。严格120–180秒使首尾歌词被区间边界截取；完整连续听校、逐词精度和审美由用户确认。没有产品改码、打包或发布。

### 2026-11-03 TRACK-MIX01：轨道兼容与统一锁定（实现/工程与真实媒体通过）

源码为当前整合工作树0a40537加本轮未提交修改。保留此前试用/创作文档改动；真实用户工程只读定位，未执行试验性拖动、保存或重开。截图对应序列revision17，各轨locked=false；顶部已渲MP4。复用project/timeline_test.go、service/project_draw_test.go与统一TestWorkspace custom，没有新增runner/固定媒体。

红灯：跨轨组普通视频和绘制源移动均报旧“目标轨道类型不匹配或已锁定”；联动音轨同样失败；Agent持锁插入失败。随后指定轨道导入、取消/失败清理、旧版本请求误解锁、跨组插入错误波纹源组分别复现后修复，必要日志已保留。新模型Apply维持相同持久locked值，owner仅记录触发者；正常编辑/Agent指定轨道导入走同一模型，跨组联动音轨映射到目标轨组，真实类型/手动锁仍阻止操作。锁与owner不改变画面指纹。

通过：模型整包（含绘制源/普通MP4、联动多音轨、目标波纹、手动/其他Agent锁及持锁插入/显式解锁）；服务定向61.389秒，包含TestDrawReservedTrackImport三种模式snapshot/follow/rendered的实际工程、真实30帧MP4渲染后持锁导入、幂等、解锁与指纹，以及取消/执行失败只释放本任务锁、CAS旧请求不误解锁；同时运行原真实媒体成片及绘制事务/CAS路径。API整包29.398秒、MCP整包125.487秒、Skill整包2.257秒通过；未重跑service全包，不借定向通过关闭整包性能问题。前端完整135项与样式通过，Build-TauriDesktop.ps1 -Debug退出0，包含类型/Vite/当前Go/Worker/Rust（Rust dev构建2m11s）；新调试版位于build/desktop/app-debug/video-auto-cut-desktop.exe。没有安装/替换正在试用的D盘版本。

实际渲染首轮测试临时目录达270字符，Node mkdtemp ENOENT；缩短测试名称后实际MP4渲染/插入通过，未更改产品路径或扩大超时来掩盖断言失败。该失败保存在agent-ux-browser-final-fix/0.3.3/artifacts/custom/rendered-import-longpath-failed.log。该目录另有track-service-green.log与本轮output.log；红灯日志track-reserved-import-red.log/reservation-cleanup-red.log/reservation-stale-red.log。模型与API/MCP在agent-ux-build同版本custom/track-model-green.log、track-api-mcp-green.log及output.log，首轮track-mix-red.log和插入波纹红灯保留。前端单元在0.3.3/public-update/0.3.3/artifacts/custom/track-ui-unit.log，当前output.log为构建。文档18份647链接、191 HTTP/17 IPC/165类型/30 SSE和diff检查通过。

原生窗口与锁按钮本轮未实际运行新构建验收，只有新构建/共用bool及事件链实现证据；不沿用旧安装截图冒充已验证新UI。尚未提交、推送或发布，本轮未改用户AVP或重启其桌面。

### 2026-11-03 TRACK-MIX02：视频换视频轨、音频换音频轨

用户澄清后收口为：保留video/audio类别，仅放开各自兼容轨道之间（含跨Agent轨组）的切换，不做音画同轨或自动抽音。产品移动/锁定实现沿用TRACK-MIX01的通用逻辑；在现有project/timeline_test.go补TestTimelineAudioMixingAndSharedReservation：音乐、配音、由视频生成的联动音频均验证同一锁状态下阻止人工插入、允许本任务插入、显式解锁与跨组换轨；拖动联动音频时4路声音和1路画面保持类型、源ID、落点与联动关系进入目标组。最终模型整包1.787秒通过，生成物沿用agent-ux-build/0.3.3/artifacts/custom/output.log。

范围讨论阶段曾探索视频文件直接投音轨抽音（有临时模型红灯/服务导出记录），用户澄清后已撤回对应产品代码和临时服务测试，保留严格类型边界；这些早期日志不属于当前需求的失败或通过证据。当前音频用例通过实际从视频轨插入生成联动音轨，再移动音频实例，不冒称直接把视频文件投到音轨已支持。仅更新功能/Skill合同与既有模型覆盖；未改用户工程、更新安装版或发布，新原生界面交互仍待验。

### 2026-11-03 PREVIEW-FIRST01：当前帧优先、随后整段预览

源码为整合工作树0a40537加未提交修改，应用仍为0.3.3。复用TestWorkspace custom及既有sequence.test.mjs、draw.test.mjs、project_media_test.go、API project_test.go和review_shortcuts_smoke.mjs；无新runner/独立套件/固定媒体。原根、业务AVP、安装版均未改动。功能合同见功能文档第8节，接口为POST sequences/{sequence}/preview/frame，仍检查会话/序列修订/内容版本/资源状态。

旧监视器等待整段MP4、绘制整场PNG及基础检查后才播放。新增单帧路径只寻当前源帧，复用sequenceGraph效果/字幕；100ms播放头防抖，取消旧请求，按当前版本及帧显示，整段请求只在首帧完成或失败后启动，不随拖动重启，视频定位就绪后接管。已有同版且SHA有效的MP4直接取帧；临时输出短路径且请求后清理，合成器版本写入内容指纹，旧合成缓存不冒充新规则。

红灯已保留：Node缺少当前帧入口、Go缺少接口、浏览器未请求保存的第45帧；新增移动画面同帧比较发现局部效果PTS小数舍入，修为FPS滤镜后的整数帧定位；浏览器视频接管夹具漏匹配query version后按真实URL修正。Go末轮真实Remotion补验命中随机端口10080，Chromium报ERR_UNSAFE_PORT；改绘制服务共用高位49152–65535本机端口并重试占用，不放开浏览器网络策略，网络/弹窗阻断回归仍通过。HTTP首轮TestAutoIdempotencyKeyLengthCountsBytesNotRunes返回500，独立重跑与整包重跑均通过，原因尚未查明，不把偶发故障写成已修复。

通过：序列真实媒体14项，覆盖音轨索引/速度、实际叠加、局部效果/转场/字幕、绘制RGBA及异FPS、基础黑屏/静音检查；单帧与成片对应像素比较包含源入点、1.5倍速与分段末帧。前端135项与样式通过，TypeScript/Vite构建通过；Chrome17组检查含保存播放头先显示静帧、整段请求顺序、晚到旧帧丢弃、拖动不重启整段、实际fixture视频就绪后接管，无页面异常。HTTP整包重跑37.174秒通过；Go真实MP4/Remotion当前帧、缓存MP4复用、旧版本/越界拒绝与临时目录清理最终11.016秒通过。端口修复后序列14项最终37.404秒通过；HyperFrames/Remotion原生绘制两项实际30帧/像素/缓存回归50.522秒通过，网络/弹窗阻断1项通过。内容指纹变更后时间线撤销/标记/绘制导入定向16.141秒通过。未重跑完整service/MCP套件，不用定向结果关闭历史整包性能问题。

证据沿用当前工作树.run-data/tests中的三个已登记根：agent-ux-build/0.3.3/artifacts为Node媒体与端口回归，custom/sequence/drawing-source/verification.json保存透明像素及单帧/整段耗时；agent-ux-browser-final-fix/0.3.3/artifacts保存preview-service-*.log、preview-api-failed.log、preview-api-focused.log、preview-api-full-green.log、preview-web-unit.log、preview-unsafe-port-failed.log及最终custom/output.log；0.3.3/public-update/0.3.3/artifacts保存preview-browser-red.log、preview-handoff-fixture-failed.log、preview-browser-17-green.log、preview-web-build.log及浏览器review-smokes日志。构建已有use-client/npm提示保留，不作为失败。文档检查192 HTTP/17 IPC/166类型/30 SSE、18份650链接及diff检查通过。

性能数值仅针对320×180、0.8秒输出/24帧绘制测试源：最终一轮单帧3.015秒、整段5.430秒，含独立编译/浏览器冷启动；不同重跑存在波动，不推算业务一分钟工程，不宣称实时。当前未打包/替换用户安装版或验证其窗口、未运行用户工程性能测试、未调用真实模型或发布；人工手感仍待用户体验。

### 2026-11-03 PREVIEW-ENGINE01：常驻、局部缓存与第一步GPU合成

当前整合工作树0a40537加未提交改动，应用仍0.3.3。用户确认先规划再执行第一步，并将GPU提前。复用Node sequence/draw、Go project_media/draw_images/API project、前端unit与review_shortcuts_smoke、原生verify-tauri-desktop；无新独立runner、固定媒体或模型服务调用。新预览模块属于产品代码，测试仍归原套件。

实现范围：每活动序列常驻Worker/共享浏览器，最多4个绘制宿主；64MiB PNG字节LRU、源键局部失效、前后2秒/每侧最多60帧单路预取及新请求抢占。sequenceGraph输出统一有效层/效果计划，GPU执行裁切/缩放/位置/层叠/黑白淡出/溶解；字幕复用ASS，普通视频仍FFmpeg取帧，无GPU时CPU后端。响应记录真实renderer/硬件标记和缓存/编译计数，回退不得保留硬件使用标记。取消单次寻帧保留编译环境，离开/切换/关闭释放所属进程及私有缓存；绘制与节目进程类型键隔离。正式导出沿用完整路径及质量检查。

| 覆盖与入口 | 本轮结果 |
| --- | --- |
| timeline-cli `npm test -- --test-concurrency=1`，通过TestWorkspace custom执行 | 全部99项通过，289.292秒；双引擎实际渲染、RGBA/时钟/图片、媒体/字幕/导出、网络/进程取消及新GPU/缓存均覆盖。后续仅诊断标记收口，最终WebGL/CPU/导出对比定向27.530秒通过 |
| 前端 `npm run test:unit`、类型/Vite/样式 | 135项通过；加载提示最终CSS经样式、18组浏览器与原生可见验证，不用原135项替代后续视觉检查 |
| service `TestSequenceCurrentFrame`、`TestPreviewLifetime`、`TestUnifiedPreviewNormalizes*`、`TestNativeAgentSharesDesktopPreview`、`TestDrawProgressCannotCrossProjectSwitch` | 最终39.392秒通过；真实MP4/Remotion当前帧、同PID、取消保温、释放/重建/目录回收、特殊ID不跨类型释放、原绘制草稿/图片/工程隔离及交付覆盖 |
| `go test ./internal/api -count=1 -timeout=240s` | 最终整包29.796秒通过，含真实frame PNG、越界/旧版本、释放端点及会话拒绝 |
| `review_shortcuts_smoke.mjs` | 18组通过；实际后端标签、保存播放头、迟到帧、背景请求不重启、遮挡命中、离开释放、实际fixture视频接管及原快捷键 |
| `Build-TauriDesktop.ps1 -Debug`与`VAC_VERIFY_PREVIEW=1`既有原生入口 | 最终构建通过；Node诊断资源另同步并以SHA核对。隔离Tauri/真实Go/GPU 4项通过，PNG橙色像素[255,157,69,255]、图片顶层命中、加载条高度、时间线无遮挡均断言，原生截图已目视检查，测试窗口正常退出 |

原生复跑方法：TestWorkspace custom设置VAC_DESKTOP_EXECUTABLE为build/desktop/app-debug/video-auto-cut-desktop.exe、VAC_VERIFY_PREVIEW=1；执行`node scripts/verify-tauri-desktop.mjs 19349 launch <版本化native-preview目录> <该目录desktop.json>`，配置使用当前调试control-plane/runtime及独立data目录。入口拒绝接管其他VAC实例，不动用户工程。四项分别验证一分钟1080p源的常驻硬件GPU、位置编辑复用源像素、实际窗口画面及布局、离开释放。

性能证据：最终原生RTX3070 Laptop/ANGLE Direct3D11，1920×1080/30fps/1800帧构图的第900/901帧寻址，冷启动HTTP整次3686ms、预取命中后173ms、位置修改195ms；Node合成耗时与HTTP整次分列在results.json。重复帧及后续帧含缓存/预取命中，不能写成无缓存解码速度、30fps实时播放或完整一分钟导出耗时。另一Node一分钟测试源验证源码只编译一次、位置编辑不重编译、改源码仅增量编译、改为原生HyperFrames仍复用宿主、附近预取/过期队列取消及有界字节缓存；像素对照覆盖GPU/CPU/实际MP4、黑白fade alpha和24/30/12fps透明叠加。

保留红灯：缺常驻入口及Go WorkerPID、GPU标签未接线；RGBA黑fade同时降低alpha导致GPU过亮，按实际PNG修复shader。原生首轮自动4项虽通过，独立截图发现旧全屏加载遮罩盖住已加载图片；`results-before-visual-review.json`、`native-preview-obscured.png`与`visual-review-failed.json`保留失败，新增遮挡/像素断言后角落状态条通过。旧生命周期测试名过长导致mkdtemp ENOENT，仅缩短为TestPreviewLifetime后重验，测试职责未删除。最后复现特殊构图ID释放错误命名空间，按draw/sequence类型隔离后通过。自动审批服务额度曾阻止最后两项命令启动，用户继续后重新审批执行通过，不记作测试已运行或安全判定拒绝。

证据：agent-ux-build/0.3.3/artifacts内preview-engine-node-all-green.log、preview-engine-api-final-green.log、custom/output.log及custom/sequence/persistent-verification.json、gpu-verification.json；agent-ux-browser-final-fix/0.3.3/artifacts内preview-engine-service-green.log、lifetime-longpath-failed.log、lifetime-green.log、namespace-red.log及custom/output.log（带preview-engine前缀）；0.3.3/public-update/0.3.3/artifacts内preview-engine-browser-final-green.log、overlay-red.log、namespace-build-green.log及custom/native-preview/results.json、preview.png、native-preview.png。生成物仍由当前版本测试根管理，未清理其他数据。

未执行完整service/MCP整包、真实模型质量或人工体验验收；未替换安装版、更新远程、签名发行或修改业务AVP。连续音画播放、硬件解码、视频纹理直传与代理质量调度留第二步；本次不声称PR/达芬奇级全流程实时性能。功能/架构/接口合同同步，文档193 HTTP/17 IPC/167类型/30 SSE与本地链接及diff核验通过。

### 2026-11-03 REL034：0.3.4正式发行与安装版更新（完成）

版本字段七处同步0.3.4；复用现有构建、签名、原生预览和生产更新器验证入口，不新增独立套件。0.3.4前端完整135项及样式检查通过（另基础webui子集65项），输出在`.run-data/tests/agent-ux-browser-final-fix/0.3.4/artifacts/{custom,webui}/output.log`。正式签名包、候选原生画面、远程发行和实际安装升级结果见本节后续记录。

按Prepare轮换三个已注册测试根，均先检查进程/标记/重解析点/跟踪文件，固定样本、业务数据及签名目录保留。相同根范围字节统计：agent-ux-build 281845773→589，释放281845184；agent-ux-browser-final-fix 847293112→606，释放847292506；0.3.3/public-update 153097259→435，释放153096824。旧结果提炼为各根archives/0.3.3.md，新测试统一写入入口产生的0.3.4目录，未清理未知根目录。

Rust既有9项在0.3.4全部通过，输出agent-ux-build/0.3.4/artifacts/custom/output.log。首次错误指定--lib导致无library target而未运行用例；去掉该参数后重跑退出0，保留入口错误为已纠正。

0.3.4正式Build-TauriDesktop.ps1 -Installer退出0，包含前端/Go/Rust release及NSIS签名。安装器74990924字节，签名428字节，公钥与生产更新器配置一致。正式app候选的VAC_VERIFY_PREVIEW=1原生4项通过；实际RTX3070/D3D11 WebGL2，1080p一分钟测试源冷帧3863ms、热缓存172ms、重复192ms、时间线位置编辑235ms，仅编译一次。画面顶层命中、橙色RGBA、加载提示高度、时间线无遮挡与离开释放均通过，native-preview.png已目视检查。证据在agent-ux-build/0.3.4/artifacts/custom/native-preview；测试窗口正常退出，未操作业务工程。以上不代表30fps连续音画或歌词项目的性能结论。

源码提交c19099c26eac8b28389687cb5955babfd648cec3及轻量标签v0.3.4已原子推送到origin/codex/integrate-remote-20261029。安装器SHA256为523b094cd869657a787314599601d9d01f4cc011b9944135e0e52382a9ba97af。GitHub Release三附件上传中，尚未将发布可用性或安装升级记通过。

正式Release432617795已公开，安装器与签名远程SHA256与本地一致。首轮公开检查清单所指带空格地址404、GitHub实际改名为点的安装器200；已据此修正New-TauriUpdateManifest.ps1的空格规范化，并只替换latest.json，原安装器/签名不变。API附件哈希核对通过，公开下载仍有旧CDN缓存，缓存刷新/生产更新器实际安装尚待。另PowerShell Invoke-WebRequest对octet-stream返回Byte[]，检查正文时需显式UTF8解码，不能直接ConvertFrom-Json误判字段。

最终实际安装版验证通过：TestWorkspace custom、StorageRoot=.run-data/tests/0.3.3/public-update，执行`node scripts/verify-tauri-updater.mjs --installed-update D:/Applications/VAC 0.3.4`，使用原生产HTTPS地址，无测试端点覆盖。0.3.3检测0.3.4、完整下载74990924字节、密码学验签、所属原生安装确认、旧后台退出、NSIS安装、自动重启均成功；结果passed/installed/auto_restarted/exited均true。重启后窗口visible=true、版本0.3.4、三Worker在线；数据目录不变且安装前后资产列表一致（本次均空，业务工程未打开）；mcp-endpoint.json仍为31525，再查phase=up_to_date。证据`.run-data/tests/0.3.3/public-update/0.3.4/artifacts/custom/updater-installed/{results.json,progress.json,downloaded.png,installed-ready.png}`及父目录output.log；installed-ready.png已实际检查，最近工程Excute me仍可见。测试窗口正常退出后重新打开普通安装版供用户使用。

公开清单命名修复已由实际生产更新器完整下载与安装证实，API最新附件哈希与本地修正清单一致。独立安装资源检查：控制面、runtime manifest、新GPU/缓存三模块与候选SHA256完全一致；桌面exe初次完整哈希不同，逐字节核实只有offset0xb63382..0xb63384的Tauri包类型UNK→NSS三字节差异，内存归一化后与候选完全一致，无其他字节变化。没有手工覆盖安装文件。发行标签c19099c保留；生成器脚本修复和最终记录作为后续源码提交推送，不需要重建相同应用二进制。

本轮完成工程门禁、真实候选原生GPU及真实安装更新，未调用模型、未重新编辑业务工程、未验回退及连续实时音画，不将其写成通过。

### 2026-11-03 PLAY034：0.3.4真实工程首次播放等待排查（排查完成，性能问题未修）

复用verify-tauri-desktop.mjs原生入口，增加VAC_VERIFY_PLAYBACK_PROJECT可选分支。只允许无既有桌面实例时启动测试拥有的窗口、要求独立空workspace，只读打开指定业务AVP；实际点击播放并核对时间前进，关闭时discard测试UI编辑，并核对业务文件SHA256不变。不新增独立runner，不改安装程序或产品播放逻辑。

使用D:/Applications/VAC原0.3.4（资源和控制面均来自安装目录），TestWorkspace custom、StorageRoot=.run-data/tests/agent-ux-build。两轮输出为0.3.4/artifacts/custom/native-playback-baseline和native-playback-cached，后者复用前者的独立data目录，WebView profile/报告另存。启动参数为19349 launch <输出目录> <desktop.json>，环境VAC_DESKTOP_EXECUTABLE指向安装exe、VAC_VERIFY_PLAYBACK_PROJECT指向用户Excute me.avp。

冷缓存：当前帧HTTP8046ms；随后整段POST已发起，FFmpeg处理120秒1920x1080/30fps序列，POST130189ms；约145830ms可播放，video.readyState=4、播放按钮启用，静帧被视频接管。点击后时间9.633333→11.077393秒且paused=false。跨进程热缓存：约9967ms可播放，帧HTTP1980ms、整段POST1566ms，没有再次耗时合成；点击后9.633333→11.091559秒且paused=false。两轮各2项断言通过、results.result=passed，业务AVP哈希均不变、测试拥有的窗口均退出；playing.png已实际检查。没有改写用户默认数据目录的缓存，结果不意味着该目录已完成缓存准备。

根因证据：0.3.4的sequenceContentVersion新增compositor=sequence-integer-pts-v2，淘汰旧合成缓存，避免复用旧时间基输出；用户缓存新版本目录e1d9f...只有未完成rendering.mp4。单帧完成后前端确实发出完整预览请求；播放按钮依赖整段preview元数据返回，GPU后端仅加速当前帧，不加速该CPU全段渲染。未复现完成后无法接管或缓存永远不加载。首次可播放约2分26秒仍属已确认P1性能/体验问题，本轮只定位并补足真实播放覆盖，未声称修复；后续应评估播放头附近分段可播放缓存/渐进就绪，验收应测首个可播放片段与连续音画，而非仅静帧。

### 2026-11-03～04 PLAY-OPT01：混合播放与局部分段优化（开发候选定向验收完成，完整service仍红）

最终结果（未提交工作区、未发布0.3.4调试候选）：nfinal原生5组通过，3381ms首次可播（200ms轮询粒度），唯一preview请求为playback计划992ms，没有整段渲染。0→62.093061秒正常墙钟速率、58.27/60秒切点、独立音轨/VU及75秒寻址通过，最大采样偏差23.44ms，AVP哈希不变；对照同口径旧安装版145830ms，减少约97.68%等待。该数字为本机同一业务工程单次最终测量，不推广为所有工程保证。nsproxy8long原生5组通过：30.033333→46.037637秒用16083ms，跨8个约2秒窗口持续播放，实际GPU像素、复用和离开释放通过；冷GPU帧3897ms、热/重复159ms是独立API帧指标，不冒充复杂端到端首次可播5秒。2秒简单首播目标尚未达到。

两次早期原生候选启动未就绪（nf3 setup underlying handle、nf4空白WebView），根因未确认，不据最终通过宣称已修启动问题；固定调试端口与其他进程出站端口冲突后改由空闲端口启动，未终止用户进程。最终Debug构建通过；五个修改的Node运行时文件与候选哈希逐一一致，Go二进制与runtime清单匹配。最终候选及所有截图/JSON在agent-ux-browser-final-fix/0.3.4/artifacts/custom/{nfinal,nsproxy8long}，测试完成窗口已退出。未重写安装版或业务AVP，未调用外部模型、提交、推送或发布。人类手感及其他工程仍待试用；扩展service全包失败保持下述记录。

最终代理档位复验：nsproxy短跨段5项通过，30.033333→33.026686秒用3072ms，GPU冷帧4015ms、宿主/源像素复用、可见像素和退出释放均通过。但拉长nsproxylong后30.033333→46.050063秒用20352ms，未过≥90%墙钟门禁，因此不接受仅短测。当前代理改为最高960宽/8fps源码采样，缓存renderer升为bounded-preview-v3-540p-source8-main31；24fps源码步长3回归先红后绿，最终Node相关4项25.041秒通过。最新Go预览/内容指纹/字体/工作区定向16.655秒通过。完整Debug构建通过，候选再同步最新Go及sequence.mjs并核对SHA一致；安装版及业务AVP保持不变。nsproxy8long及最新业务候选的结果随后记录。

2026-11-04后续证据：nf7最终直播放5组通过，约6085ms首次可播，playback计划30ms且没有完整preview POST；从0连续播放至62.069841秒且墙钟速率≥95%，58.27/60秒切点、独立音轨/VU及75秒寻址通过，最大采样音画偏差12.505ms，业务AVP哈希保持。nfast约4098ms、nf2约6043ms和nf6约7127ms只说明就绪耗时，后续检查失败，不能代替nf7连续播放证据。先前过严的音画同步会频繁暂停，随后发现React延迟位置回流又触发seek；当前显式用户seek和媒体上报分开，禁止UI反馈驱动反复寻址。隐藏编辑器不重建播放器，离开时释放媒体与VU上下文。

nsfinal因夹具切资源页后未使用返回的新工程revision导致409，夹具已修。nsfinal2/3的GPU冷/热帧、效果修改复用和可见像素通过，但分段推进约3.02秒用了8.334秒，不能称流畅。当前加入预览源码最高12fps时间采样（成片仍用序列FPS和原音频），与最高1280宽共同作为代理画质；精确静帧和正式导出不降采样。新增既有FrameSource回归先红（缺frame_step）后绿，确认24fps源码步长2、起点对齐、22帧窗口和30fps输出、透明叠加与原音频正确。Node相关4项27.563秒及去掉多余双RAF后相关3项24.127秒通过，复用原sequence/draw用例。最新完整前端137项/样式、类型与18文档/662链接/196HTTP/17IPC/170类型/30SSE通过。

扩展Go service/API整包：API32.113秒通过；service失败，记录service-api-full-failure.log（同agent-ux-build/0.3.4/artifacts/custom）。包含auto_hardening快照rename Access denied、delivery_variant暂存mkdtemp ENOENT、loudness5400秒PCM样本No space left on device，最终TestSkillProjectRequiresFreshConfirmation/Windows EvalSymlinks处600秒超时。未据此修改无关模块，未将历史失败写成新播放逻辑回归或通过；当时E盘仅约829MB可用。针对本次改动的预览/内容指纹/工作区单独复验，完整套件仍需在足够磁盘及稳定Windows目录环境复核。本轮未删除业务或测试文件腾空间。

复用sequence/draw Node、service project_media、API整包、前端desktop unit、review_shortcuts_smoke与verify-tauri-desktop原生入口，不新增独立套件或固定媒体。保留业务AVP/安装版；调试候选在build/desktop/app-debug，正式发行仍0.3.4。功能合同第8节随新路径更新。

阶段结果：分段测试先红（旧实现60帧而非请求18帧），随后发现转场中段未延续透明度，修复后与全段对应像素/音频一致。Node sequence/draw现有33项328.31秒通过；后续常驻分段复用2项17.625秒通过。前端新增直播放半开切点/源偏移和MSE窗口推进/后退检查，最新完整137项/样式/类型通过。首次调试构建通过；第二轮MSE修复调试构建通过。

第一轮真实业务工程直播放：native-playback-fast原生4项通过，约12216ms可播，唯一preview请求为playback计划（2258ms），没有整段渲染请求；原生点击实际播放、58.27/60秒换源及75秒寻址通过，最大采样偏差0.01018秒，AVP SHA不变。原生输出在agent-ux-browser-final-fix/0.3.4/artifacts/custom/native-playback-fast。此后发现每次读取资源/时间线会克隆全部AVP历史，已缩小只读快照，等待新候选实测，不把12秒记成达到2秒目标。

原生分段首轮自动通过但证据不足：只播放到1.932979秒，未跨首段边界且初始画面为0而非30秒；保留native-playback-segments。加强定位/跨段断言后native-playback-segments-warm失败，截图暴露MSE窗口起点等于旧终点导致异常（appendWindowStart=2而旧end=2），并发现外部播放头改变未调用播放器seek；已修窗口更新顺序和播放头同步，补前端单元。分段源码准备改用同一常驻宿主，避免每段冷编译。native-playback-segments-fixed正在验证，不能用旧首轮结果宣称连续播放通过。

Go service定向55.133秒通过，覆盖当前帧/取消释放/共享原生Agent/绘制生命周期及快照隔离；包含真实绘制两秒分段、缓存命中、后方编辑保留前段缓存、旧版本URL拒绝。扩充用例后首次仍用旧seek版本导致Conflict，更新测试请求后通过，产品校验保持。上述运行使用原有TestWorkspace custom，所有生成物在注册测试根的0.3.4目录；未清理失败证据。HTTP整包、最终原生、文档合同同步仍待本轮结果。

### 2026-11-04 用户授权的旧 run-data 清理

已移除50个确定的可再生产物目录：原根与当前整合工作树的旧测试版本输出，以及整合工作树build/cache/go、build/desktop/{debug,release}/{deps,build,incremental,.fingerprint,examples}。测试根含历史自定义StorageRoot，统一入口会拒绝未知目录，故按既有标记/summary核验后集中提炼和定点清理；没有重新运行测试。原根archives保留0.2.2、0.3.1、0.3.2，整合工作树archives保留0.3.2、0.3.3、0.3.4，每版本一份；各轮failed/pending/passed及已记录fixed_in均保留，未知修复版本/提交写未记录，不将后续无关通过当成修复。

清理前确认相关进程退出，逐目标核验绝对路径、父链/子项无重解析点及所属Git无跟踪文件，使用PowerShell Remove-Item -LiteralPath。清理前后原根与整合工作树Git状态完全一致。保留全部工作树及未提交修改、当前code-track测试输出、development/accepted-projects/accepted-fixtures、正常开发依赖、固定样本和恢复副本、debug/release可执行程序及runtime、app/app-debug、release/bundle和updates正式包、签名及发布元数据。未知用途的小型历史目录未清理。

同口径原根.run-data文件逻辑大小21509796820→8620418254字节，减少12889378566字节（12.00 GiB）；E盘可用空间35639836672→46505267200字节，实际增加10865430528字节（10.12 GiB）。文件逻辑大小跳过重解析点目标，硬链接/文件系统分配导致其与磁盘释放值不同。本次旧测试明细路径已失效，结论以版本简档与本文历史边界为准；code-track当前证据仍可用。下次构建会重新生成已清除缓存，本次未新增产品验证或质量验收结论。
