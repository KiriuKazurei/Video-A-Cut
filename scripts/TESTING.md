# 测试维护说明

更新：2026-10-20。本文是既有测试权威入口，主文档为[项目](../docs/项目文档.md)、[计划](../docs/计划文档.md)、[接口](../docs/接口文档.md)及[架构功能详解](../docs/架构与功能详解.md)。本轮合并说明不复制测试套件、不删除测试源码/样本、不执行产品数据清理。

## 1. 统一规则与源码身份

版本取 desktop/tauri.conf.json，当前 0.1.1。测试生成物只放 `.run-data/tests/<完整版本号>/`；固定媒体在 `workers/node/timeline-cli/fixtures/media/`。原录像、正式业务数据、签名密钥、正常开发依赖与 test-support/legacy 恢复副本不属于测试生成物。

`scripts/TestWorkspace.ps1` 统一设置 VAC_TEST_ROOT/VAC_TEST_OUTPUT、TEMP/TMP/TMPDIR、Go/npm 缓存、禁 Python 写缓存及离线 Go。支持 go/python/webui/pipeline/timeline/exporter/guard/workspace/custom。前端完整单元用 npm run test:unit，内置 webui suite 只覆盖原基础子集，不把二者计数混用。

当前源码工作树为 `E:/AI/Video Auto Cut/.run-data/worktrees/codex-skill-harness`，分支 agent/vu-skill。原根和其他旧工作树保留不同源码与未提交内容，不能用旧根 HEAD 或端口健康证明当前代码。custom 的 WorkingDirectory 必须明确；摘要中 runner 所在仓库的 HEAD 与实际被测源码不同时，单独记录源码提交/改动和证据。

不设固定容量上限，不按容量自动终止或清空 artifacts/cache；Measure 仅报告占用。版本变化后 Prepare 按规则提炼旧版本简档并轮换；同版本需要清空时显式 Clear，CacheOnly 保留 artifacts。每版本一份极简 Markdown，最多最近 3 版，未知修复版本/提交写未记录。

任何清理先确认相关进程退出、目标属于明确测试根、目标/父链/子项无重解析点且无 Git 跟踪文件，PowerShell 使用 LiteralPath，并报告同口径前后占用。此条针对生成物；本轮用户单独授权的源文档删除按精确文档清单处理，不把它扩展到源码/固定样本。

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
$sourceRoot = "$managerRoot/.run-data/worktrees/codex-skill-harness"
& "$managerRoot/scripts/TestWorkspace.ps1" -Action Run -Suite custom -Executable go -WorkingDirectory "$sourceRoot/control-plane" -Arguments @('test','./...','-p=1','-vet=off','-count=1','-timeout=600s')
```

Go vet 单独执行，不跳过原失败用例。按变更选择现有包/命名用例；不要在相同结果通过后无理由重复全包。并发入口须避免共享测试根、端口、数据库和管理锁。正常失败保留退出码、摘要与日志。

## 3. 文档与接口契约

接口文档自动区域来自机器清单与真实 TypeScript AST。维护契约后显式生成，再运行现有检查：

```powershell
node scripts/update-interface-documentation.mjs --write
& $runner -Action Run -Suite custom -Executable node -WorkingDirectory $sourceRoot -Arguments @('scripts/update-interface-documentation.mjs','--check')
& $runner -Action Run -Suite custom -Executable python -WorkingDirectory $sourceRoot -Arguments @('-B','scripts/check-documentation.py','--root',$sourceRoot)
& $runner -Action Run -Suite custom -Executable python -WorkingDirectory $sourceRoot -Arguments @('-B','scripts/verify-script-workbench-docs.py')
```

检查精确路由集合、源 SHA、处理器位置、15 项桌面 IPC、HTTP 请求/响应索引、类型/SDK 自动区域及本地链接。文档迁移更新原检查对象，不退役 Go/前端/Worker 覆盖。运行时 Skill 文档若变更，同步嵌入/目录副本和版本，并执行相应 MCP/服务契约回归。

## 4. 媒体、MCP、浏览器及可见桌面入口

| 入口 | 目的与前置 |
|---|---|
| Go service/API/MCP 现有包 | 工程往返/隔离、CAS/幂等、Worker 边界、预算/历史、真实 FFmpeg 和本机模型夹具 |
| `workers/node/timeline-cli/test/` | 预览/ASS/脚本真实渲染、哈希和跨语言输入；复用固定 clip_01.mp4 |
| `workers/node/export-worker/test/` | owned task、取消/租约、输入/回执、摘要负向、恢复和发布 |
| `workers/python/tests/` | 模型协议/重定向、声音、脚本、导入隔离与恢复；默认不下载模型 |
| `scripts/verify-vu-runtime.ps1` | 现有真实 Go/Worker/MCP 连续流程；先按当前源码重建程序及匹配 runtime 清单 |
| `control-plane/internal/mcpserver/probe/main.go` | 官方 SDK 真实发现/调用；通过 managed custom 执行 go run -tags probe，内部 Worker 才显式给 token-env |
| `scripts/verify-skill-ui-completion.mjs --real-flow` | 基于运行探针产出的受管数据库副本/媒体和生产前端检查交互，夹具与真实后端范围分别记录 |
| `scripts/verify-tauri-desktop.mjs` | 可见 Tauri/WebView、原生文件框/菜单/剪贴板、媒体/脚本、关闭/保存、切换/重启；自己在管理器内部启动并退出 |
| `scripts/Test-TauriStateGuard.ps1` / `test-support/test-workspace.ps1` | 现有运行/清理/版本轮换守卫；通过对应 suite，不按本轮再复制独立测试 |

生产 Vite 测试构建的 outDir 显式指向 VAC_TEST_OUTPUT 管理目录。浏览器使用已有 Playwright/Edge，不重新下载浏览器。没有真实窗口证据时，不把 CDP 端口、进程 Responding 或页面托管记为可见桌面通过。

可见桌面既有调用形状如下；exe、生产前端、Go runtime 清单、desktop.json 和独立测试业务数据须先配套准备，不能把本机历史生成物当作 fresh checkout 自带资源：

```powershell
$env:VAC_DESKTOP_EXECUTABLE = "$managerRoot/.run-data/tests/0.1.1/cache/cargo/debug/video-auto-cut-desktop.exe"
& $runner -Action Run -Suite custom -Executable node -WorkingDirectory $sourceRoot -Arguments @('scripts/verify-tauri-desktop.mjs','9223','launch',"$sourceRoot/.run-data/tests/0.1.1/artifacts/custom/avp-desktop-ui","$sourceRoot/.run-data/tests/0.1.1/artifacts/custom/avp-desktop/desktop.json")
```

Windows Tauri production overlay 的 frontendDist 使用相对 desktop 目录的路径，避免 E: 被当 URL；测试数据根独立于源码资源根。测试应自己在管理器内部启动和退出，不预启窗口绕过在用进程守卫。正式打包仍使用 Build-TauriDesktop.ps1，不以测试 overlay 发行。

## 5. 最近适用结果

| 范围/候选 | 真实结果 | 证据与限制 |
|---|---|---|
| `7dda64d` 完整 Go | 16 个有测试的包通过，另 2 包无测试文件 | root 13.849s、API 21.956s、MCP 36.430s、service 448.146s、store 5.884s；原根短输出 custom/output.log，实际源码 WorkingDirectory 为当前 control-plane |
| `7dda64d` 前端/Python/Node | 前端 92/样式、Python 159、Timeline 54、Exporter 53 全通过 | 当前工作树 artifacts/custom/branch-*.log、branch-consolidation-verification.json；TS/Go vet/Rust fmt/文档契约也通过 |
| AVP 可见桌面 | 最新 22/22 通过 | custom/avp-desktop-ui/results.json 及截图；原生文件操作、六类预览、标记/备注、关联/重定位、待执行任务停止切换、单实例、关闭取消、保存退出队列及新进程参数重开 |
| AVP Go/Worker/媒体 | 往返、独立另存、全新库恢复、缺失/定位、AVS 关联、旧请求/Worker 排空、输出引用和依赖失效通过 | custom/avp-final-verification.json；真实 Go→Python→本机模型桩→Go→Node/FFmpeg，不是实际外部模型质量 |
| MCP 协议与媒体 | SDK 四版本 × Agent/Worker，原生图片、包资源哈希、三版 MP4/ZIP/完整解码/Range、定向改稿与历史回归通过 | 1.0/.8/.7 秒是确定性媒体夹具；Agent 无人工定稿/日志工具，窗口真实操作/旧服务级模拟人工分别记载 |
| 本轮文档整合，`6242cf7` + 本轮工作副本 | 文档/契约、样式、PowerShell 解析、保留边界通过；既有 MCP 全包通过 33.064s，Skill 包无独立测试文件 | documentation/documentation-check.json、retention-audit.json；script-workbench/doc-check.json；documentation-*.log。157 HTTP、15 IPC、119 类型、25 SSE；460 个本地链接、6 份工作台相关文档的 454 个链接。未重报产品全套/可见桌面/人工/发行验收 |

`custom/...` 相对当前工作树 `.run-data/tests/0.1.1/artifacts/`；共享 Cargo/Go 短输出位于原根对应版本。运行器本地结果可能已按规则轮换，报告存在不代表全部旧截图/数据库仍存在。

## 6. 已处理失败和未验边界

| 历史问题 | 处理事实 | 当前含义 |
|---|---|---|
| Go 180 秒超时和嵌套长路径失败 | 改为短版本化输出、明确源码 cwd、600 秒超时，完整包不跳过原失败用例后通过 | 不把长路径误写成产品逻辑已无所有 Windows 共享锁风险；B04 观察仍见计划 |
| 旧人工 Token/日志/预览断言 | AVP 移除窗口凭证，原测试更新到新身份/项目隔离契约，MCP/Worker 拒绝用例保留 | 旧“匿名日志应 403/先解锁”不是当前窗口流程；仍校验 Agent 工具和跨工程访问 |
| MCP 三个旧版本 tools/list 400、仅 base64 图片、契约分发不足 | SDK 协商、原生 ImageContent 和固定包资源已修复并实际往返 | 不再列作代码缺口；不等于真实视觉模型判断质量通过 |
| 原生文件框选错控件、页面数据未加载就输入、退出后 CDP 查询 | 既有探针等待实际可见/可编辑状态、限定窗口/控件和关闭传输后复验通过 | 早期失败提炼于 avp-desktop-first-failures.md；未放宽产品要求 |
| 2026-10-19 旧 500 MiB 规则中止组合测试 | 当时中止及拆分通过分别记录；随后已取消固定容量限制，两处 workspace 守卫各 36 项通过 | 不把旧中止记为全套通过，也不把旧容量门当现行政策；原临时 JSON/数据库/截图有部分已清除 |

当前外部 LLM/转录/云声音质量、人工观感、目标剪辑软件、声明规模长素材及最新安装包/系统关联/公开更新仍按计划另验。2026-10-02～03 的真实 OpenAI、原交付 Premiere 导入/播放/编辑、迁移重链接与视听效果已有用户确认；Anthropic/Gemini 真实调用按用户要求排除。2026-10-09 的 ASS/StepAudio 样本人工确认和本地签名升级同样保留，以上均为旧候选的有限证据，不扩大到当前 AVP 全面验收。
