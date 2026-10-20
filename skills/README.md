# VAC Skill 包与权限约定

> 2026-10-19：agent/vu-skill 已实现原生 Skill 注册/快照和运行工具，video-use 的固定上游与 VAC 入口位于 video-use/，Go 嵌入同版资源。实际接口见 [Skill 交接](../docs/Skill-Harness接口与前端交接.md)。


更新：2026-10-19。当前 `agent/vu-skill` 已实现三种原生包及本地文本包导入。实现契约见[接口交接](../docs/Skill-Harness接口与前端交接.md)，[双流程设计](../docs/Skill双流程架构设计.md)保留设计来源。

## 目录

```text
skills/
  README.md
  vac-script-syntax/       # develop 已有项目语法说明包
  video-use/              # 已纳入：VAC 入口说明、元数据及适配资源
    SKILL.md
    skill.json
    upstream/             # 固定 commit 原件、来源、摘要与 LICENSE
```

原生 `video-use`、`vac-visual-summary` 和 `vac-script-syntax` 由 [Go builtin](../control-plane/internal/skill/builtin.go) 注册。上游选定文件固定在 commit 43cfc566833548093455c9d4a46bd69913b7e552，资源嵌入 Go，安装目录不需要对外部 Agent 可见。上游辅助脚本仅作为参考分发，不执行安装脚本。

## Agent 如何使用

外部 Agent 经 MCP 的 skills.list/get 获得锁定版本的正文、适配约束及资源索引，按需读取包内说明，再调用 VAC 项目工具。内置 Agent 通过配置的 API Key/Provider 加载相同上下文并执行工具调用循环。两种模式都由 Agent 理解 Skill；Harness 管运行与工具权限。MCP 分发不依赖外部 Agent 能打开 VAC 安装目录。

上述调用入口已实现；MCP 不提供人工计划定稿或恢复工具。当前窗口打开 `.avp` 后，导入资源直接供该工程 Agent 使用，不再逐项确认可见集。服务调用权限集中于全局服务商配置，运行继续绑定工程、内容快照与预算；外部模型费用不可由 VAC 完整观测。实际工程契约见[AVP 说明](../docs/AVP工程文件与资源工作区.md)。

## 元数据和权限

skill.json 使用 [Package 类型](../control-plane/internal/skill/contracts.go)：id、名称、描述、版本、runtime、inputs/outputs、compatible_flows、execution_modes、tools、permissions 等；source 与内容摘要由 Go 填写。source 为 builtin/local_path，runtime 为 instructions_only/harness；不实现 git/registry 安装。SKILL.md 不承载密钥；包摘要及版本在运行时冻结。

- 本地导入只校验说明和资源，不能执行安装脚本或自动下载；包内资源读取拒绝越界和重解析点。
- 全局禁用不得由项目覆盖重新启用；builtin 不可卸载。项目按流程选择默认 Skill。
- Agent 读取当前工程的素材视图、脚本和摘要，不获取任意磁盘读权限；Worker 由 Go 解析受控引用。
- 网络、转录、视觉、TTS 均使用服务商设置中的能力与调用许可；凭据不写入工程文件。
- 输出限项目 run、keyframes、summaries、renders、delivery 等受控目录；模型输出中的路径不直接执行。
- 渲染传意图参数和受控时间线引用，由 VAC 统一媒体服务执行；不开放任意 shell/raw filter 作为首版插件能力。
- 定稿、恢复和验收继续按对应工具的角色边界执行；可见窗口操作直接使用人工身份，不需要人工会话凭证。

## video-use 适配

采用上游能力说明，保留 MIT LICENSE、来源与固定 commit；VAC 适配说明单独维护，不改写上游原件冒充原版。替换目录扫描、凭证读取、渲染和产物发布的调用边界，将 EDL 映射到 VAC 契约。上游特定转录服务作为可选 Provider，替代服务需满足所需时间戳能力。初版兼容流程 A；流程 B 使用抽帧与内容大意。动态动画执行等未支持能力须明确展示，不声称已完整支持上游所有功能。
