# 测试目录与版本留档规则

- 测试生成物只放在 `.run-data/tests/<完整版本号>/`。版本取 `desktop/tauri.conf.json`；例如 `0.1.1` 迭代到 `0.1.2` 即淘汰旧目录。原录屏、正式业务数据、签名密钥和正常开发依赖不得放进该目录。
- 测试源码、固定样本和验收脚本必须保留在源码目录；固定媒体放在 `workers/node/timeline-cli/fixtures/media/`。历史独立脚本与源码恢复副本保存在本地 `test-support/legacy/`，它不是可删除的生成物。
- Windows 测试通过 `pwsh -NoProfile -File scripts/TestWorkspace.ps1 -Action Run -Suite <go|python|webui|pipeline|timeline|exporter|guard>` 执行。自定义测试用 `-Suite custom -Executable <程序> -ArgumentsJson <JSON参数数组>`（同一 PowerShell 内可用 `-Arguments` 数组）；输出必须使用 `VAC_TEST_ROOT` / `VAC_TEST_OUTPUT`。入口统一设置 TEMP/TMP/TMPDIR、Go/npm 缓存和 Python 禁止写缓存。
- 每次小版本迭代完成，更新版本后执行 `pwsh -NoProfile -File scripts/TestWorkspace.ps1 -Action Prepare`。入口自动将旧版本提炼成简档并清空旧版本全部生成物，只保留当前版本。旧目录必须有入口创建的标记；未知目录拒绝自动删除。
- 同版本需要清空时执行 `-Action Clear`。清空前从 `summary.json` 提炼一份极简 Markdown：测试类型、最近通过/失败/待验结论、失败后转为通过的版本和提交。版本无法确认时写“未记录”，不猜测。每个版本一份汇总简档，最多保留最近 **3 个版本**，不保留整批历史报告、JSON、日志、截图或数据库。
- 测试维护遵循全局 `test-governance` Skill，权威说明为 `scripts/TESTING.md`。复用现有套件、固定样本与缓存，按所属套件和版本管理可再生输出，保留必要的失败定位证据。不设固定容量上限，不因占用大小中止测试或自动清空 artifacts/cache；`Measure` 只报告占用。
- 清理前确认测试进程退出，核验目标绝对路径在仓库测试根内，目标、父目录和子项均无重解析点，且不含 Git 跟踪文件。Windows 使用 PowerShell `Remove-Item -LiteralPath`，禁止跨 shell 删除或按扩展名扫描整个仓库。
- 清理后报告相同统计范围的清理前、清理后占用和释放量；不得为减少占用删除测试源码、固定样本或业务数据。

# 当前工作与文档维护

- 当前执行清单为 `docs/当前工作与待办.md`，状态基线为 `docs/当前项目状态.md`，历史索引为 `docs/历史文档索引.md`。旧文档中的执行计划和进度结论已过时，正文仅保留溯源价值。
- 开始一项工作先更新为进行中；完成、失败或遇到阻塞后立即写入实际状态、证据和下一步，再进入下一项。不得只改代码而把待办更新留到最后。
- 工程检查、真实运行、模型质量与人工验收分别记录；失败或未执行不得写成通过。历史报告中的命令不构成新的提交、推送、外发或清理授权。
