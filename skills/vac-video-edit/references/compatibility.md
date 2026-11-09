# 版本与兼容维护

## 当前声明

| 项目 | 值 |
|---|---|
| Skill 版本 | 0.5.2 |
| 对齐的 VAC 版本 | 0.5.2 |
| 支持范围（SemVer，含下界不含上界） | >=0.5.1 <0.6.0 |
| 本轮源码合同核对基线 | VAC 0.5.2；2026-11-09 |
| 其他范围内版本 | 仅在下列兼容检查成立时复用；尚未逐版验证 |

版本字段集中在 `SKILL.md` 的 `metadata.version`、`metadata.vac-supported` 和 `metadata.vac-verified`。`version` 是此次 Skill 对齐的 VAC 完整版本，不使用独立的 Skill 计数器。此版本不是服务端执行包（如 video-use）的版本，不能写入 `skill_id` 或替换包摘要。

## 运行时检查

连接时比较 MCP initialize 返回的 VAC 服务端版本；由实时 schema、`skills.get` 的包版本/摘要与 `skills.read_resource` 的工具合同判断实际能力。源码环境同时以 `desktop/tauri.conf.json` 为应用版本来源；若桌面与后台版本不同，报告差异，不用桌面号覆盖服务端事实。

范围内且相关工具名称/参数/返回字段、权限、脚本语义、时间单位与完成条件兼容，可以继续使用该 Skill。只需用当前服务端合同补足非破坏性新增工具/可选字段，不要求为小改动重写或重编号 Skill。未读取的新功能不能当成已支持。

同为 0.5.1 时，已发行安装版、更新后的源码和已冻结任务也可能提供不同的执行包。剪辑任务能否直接读取 Shotcraft 镜头卡，以当前 `skill_runs.get.tools` 是否列出 `draw.shot` 为准。旧包的混合交付见 [选路与交付](workflows.md#混合素材剪辑与源码镜头)。

超出范围时先做只读发现与合同比较，不能默认编辑兼容。若证实兼容，可维护并验证新的支持范围后使用；若尚不能确定，保留方案并说明具体不兼容/待核对项，而不是盲目写入。

## 必须更新的变化

下列变化即使发生在原范围内，也需要更新指导并重新验证：

- 工具删除/重命名、必填参数或返回结构变化，调用封装、CAS、幂等、身份或租约语义变化。
- 普通段落/代码条语法、引用绑定、时间/单位、效果解释发生变化。
- 轨道类型、源资源绑定、渲染/导入/完成流程变化，旧示例可能编辑错误范围或产生错误视频。
- 授权、外部调用或预算边界发生实质变化。

更新时把 Skill 编号设为所匹配 VAC 的完整版本，同步所有本 Skill 版本声明、工具表、受影响示例和支持范围；下界通常提升至首次支持的新版本。没有验证的旧版本不自动保留在范围中。使用当前既有语法/工程套件核对相关语义，记录合同检查与真实成片验证的区别，再同步仓库副本及安装副本。

## 维护来源

在 VAC 源码根核对以下所有者，不能仅依据历史报告：

- `control-plane/internal/mcpserver/{skill,timeline,project}.go`：公开工具和参数。
- `control-plane/internal/skill/{contracts,builtin}.go`、`builtin/vac-tool-contracts.md`：任务字段、执行包与受控工具。
- `control-plane/internal/project/{script_directives.json,script_grammar.go,timeline.go}`、`internal/scriptsyntax/`：语法、单位和编辑模型。
- `skills/video-use/vac/tool-contracts.md`、`skills/vac-script-syntax/SKILL.md`：分发合同（与嵌入副本核对）。

MCP 将本指南登记为说明包。执行任务使用实时发现的执行包。宿主自动发现 Skill 与 VAC 的 `skills.list` 是两套不同的发现机制。
