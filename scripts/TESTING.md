# 测试目录、版本与简档

更新：2026-10-18。agent/develop 已合入版本化入口和固定样本迁移。多工作区使用方式与本轮最简命令见 [当前运行与测试](../docs/当前运行与测试.md)；执行清单见 [当前工作与待办](../docs/当前工作与待办.md)。

通过 scripts/TestWorkspace.ps1 的 custom、WorkingDirectory 和 ArgumentsJson 选择实际源码。原根目录管理入口的内置 suite 指向旧源码，不能替 develop 背书。输出使用 VAC_TEST_ROOT/VAC_TEST_OUTPUT，版本来自 desktop/tauri.conf.json；TEMP/TMP/TMPDIR、Go/npm 缓存与 Python 禁写缓存统一设置。

小版本更新后 Prepare；同版本 Clear；只读 Measure；Clear -CacheOnly 保留 artifacts。清理前归每版本一份 Markdown，最多 3 版，未知修复版本/提交写未记录。500 MiB 包括规定范围的测试源码、样本、脚本和缓存；超限终止测试并归档清理，不删除源码、业务数据或密钥。清理必须验证进程退出、绝对路径、父子重解析点和 Git 跟踪边界。

本轮仅执行新增模块定向测试、类型/样式/构建、真实脚本往返与新二进制本机运行，未做基线、备份、浏览器冒烟或人工全流程。详细实际结果见 [整合记录](../docs/开发分支合并-2026-10-18.md)。
