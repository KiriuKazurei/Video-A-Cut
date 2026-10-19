# Skill Harness 接口与前端交接

> 后续更新：原成果已本地提交为 `dc20174`；本轮增加应用工具隔离、人工专属追加日志、成片局部修改与撤销/重做，见[最新交接与验证](可控编辑与操作历史-2026-10-19.md)。旧阶段的数量、提交及未验说明仅代表当时范围。

更新：2026-10-19。对应分支 `agent/vu-skill`，工作区 `.run-data/worktrees/codex-skill-harness`。本文描述已写入代码的接口；测试、真实质量及剩余验收见[实施记录](Skill架构实施与验证-2026-10-19.md)。本轮提供调用和状态契约，页面布局由后续前端工作负责。

## 事实来源

- Go 类型：[skill/contracts.go](../control-plane/internal/skill/contracts.go)、[project 请求](../control-plane/internal/service/skill_projects.go)、[导入请求](../control-plane/internal/service/skill_import.go)、[声音请求](../control-plane/internal/service/skill_voice.go)。
- HTTP 注册：[api/skill.go](../control-plane/internal/api/skill.go)；MCP：[mcpserver/skill.go](../control-plane/internal/mcpserver/skill.go)。
- 前端：[skillContracts.ts](../webui/src/skillContracts.ts)、[skillApi.ts](../webui/src/skillApi.ts)。现有 `api.ts` 的 request/ApiError、runtimeUrl 和 AbortSignal 共用。
- 全部 HTTP 索引：[frontend-api-inventory.json](frontend-api-inventory.json)。基础 28 项加本轮 12 项，当前合计 143 个注册模式、144 个展开接口；11 个桌面 IPC 不变。

## 前端实现状态（2026-10-19）

以下 HTTP 表逐项标明接入状态；全量 144 项及前端待实现顺序以[前端接口清单与实现状态](前端接口清单与重构交接.md)为准。新架构 40 项均未接入页面：39 项已有 SDK，`GET /api/skills/{id}/content` 是无独立封装的详情别名，直接使用 `skillApi.skill` 的详情路由即可。

`skillContracts.ts` 类型、`skillApi.ts` 调用、SSE 失效领域及 `createSkillEdit` / `skillPreviewSource` 辅助函数已提供；尚无 TSX 页面调用这些新入口。人工解锁/锁定、项目/Skill 管理、双流程/双 Agent 引导、SkillRun 工作台/同版播放、段落声音回写、定向修改、日志及撤销/重做界面均待接入。已有旧 script/workflow 页面不能算作新 SkillRun 功能完成。

优先补齐人工会话入口；原 `/api/audit` 也已受保护，现有审计页需适配解锁/到期。`preview_url` 只作短时只读播放，历史预览需标记；恢复/修改后重新定稿。本文工程验证记录不代表新界面已验收。

## HTTP

所有 JSON 成功响应为 200；错误沿用 `{error:{code,message}}`。非法参数 400、权限 403、对象不存在 404、版本/状态冲突 409。媒体下载支持原生 Range；响应带 SHA256 和 private/no-store。失败后重新 GET run/steps，不能把请求超时当成未执行。

| 方法与路径 | 请求 / 返回 | 前端入口 | 前端状态 |
|---|---|---|---|
| GET /api/projects | Project[]，含只读 legacy 项目 | projects | 仅 SDK，页面未接入 |
| POST /api/projects | ProjectSave → Project | createProject | 仅 SDK，页面未接入 |
| GET /api/projects/{id} | Project | project | 仅 SDK，页面未接入 |
| PUT /api/projects/{id} | 完整 ProjectSave + expected_version → Project | saveProject | 仅 SDK，页面未接入 |
| POST /api/projects/{id}/media | root_id、relative_path、expected_version、idempotency_key → Project | importMedia | 仅 SDK，页面未接入 |
| GET /api/projects/{id}/visible-set | 项目 scope v2、内容 SHA、确认状态、媒体目录 | visibleSet | 仅 SDK，页面未接入 |
| POST /api/projects/{id}/visible-set/confirm | visible_set_sha、expected_version → VisibleSet | confirmVisible | 仅 SDK，页面未接入 |
| POST /api/projects/{id}/exports/ffmpeg | run_id、expected_version、idempotency_key → run/result | exportProject | 仅 SDK，页面未接入 |
| GET /api/skills | SkillPackage[] | skills | 仅 SDK，页面未接入 |
| POST /api/skills | 本地目录 path；要求 SKILL.md + skill.json | importSkill | 仅 SDK，页面未接入 |
| GET /api/skills/{id} | 元数据、正文、文本资源、内容摘要 | skill | 仅 SDK，页面未接入 |
| PATCH /api/skills/{id} | enabled | enableSkill | 仅 SDK，页面未接入 |
| GET /api/skills/{id}/content | 与详情相同，正文内含资源索引 | 无独立封装；skill 调用详情路径 | 未封装/未接入 |
| POST /api/skills/{id}/validate | 元数据/摘要、ffmpeg/ffprobe/Python/Node 探测 | validateSkill | 仅 SDK，页面未接入 |
| GET /api/system/ffmpeg | 版本、编码器、字幕、硬件能力 | ffmpeg | 仅 SDK，页面未接入 |
| GET /api/skill-runs?project_id=... | Run[] | runs | 仅 SDK，页面未接入 |
| POST /api/skill-runs | SkillRunCreate → Run | createRun | 仅 SDK，页面未接入 |
| GET /api/skill-runs/{id} | Run | run | 仅 SDK，页面未接入 |
| GET /api/skill-runs/{id}/context | 锁定 Skill、已确认媒体视图和工具名单 | context | 仅 SDK，页面未接入 |
| GET /api/skill-runs/{id}/steps | 模型/工具步骤、结果、失败原因 | steps | 仅 SDK，页面未接入 |
| GET /api/skill-runs/{id}/revisions | run_version + plan 的不可覆盖快照 | revisions | 仅 SDK，页面未接入 |
| GET /api/skill-runs/{id}/artifacts/{key} | 已登记、当前上下文有效且摘要一致的文件 | artifactURL | 仅 SDK，页面未接入 |
| PUT /api/skill-runs/{id}/voice | 精确段落声音配置；生成新计划修订，需再定稿 | saveVoice | 仅 SDK，页面未接入 |
| POST /api/skill-runs/{id}/tools | SkillToolRequest → run/result | tool | 仅 SDK，页面未接入 |
| POST /api/skill-runs/{id}/finalize | expected_version；仅人工 | action(finalize) | 仅 SDK，页面未接入 |
| POST /api/skill-runs/{id}/resume | expected_version；工具/预算/重启暂停后人工恢复 | action(resume) | 仅 SDK，页面未接入 |
| POST /api/skill-runs/{id}/cancel | expected_version；取消本次运行及进程树 | action(cancel) | 仅 SDK，页面未接入 |
| POST /api/skill-runs/{id}/execute | expected_version；内置 API Agent 运行到暂停/完成 | execute | 仅 SDK，页面未接入 |

ProjectSave 包含 name、flow、expected_version、assets、visible、default_skills、disabled_skills、accept_external、accept_transcription，以及可选 profile_id/profile_revision。更新是完整替换，先读取最新对象再 PUT；切换配置清除旧确认及旧运行上下文。外部访问与转录是两项独立授权。导入只接受现有 ingest_roots 的 root_id/相对路径，复制后入资产表，不接受 Agent 提交任意绝对路径。导入后素材已入库，visible 仍需用户显式选择并确认。

## 最短调用流程

受保护页面先 `unlockHumanSession` 解锁，再开始以下流程；读取 Skill 说明不需要项目推理 Key。退出调用 `lockHumanSession` 并清空敏感页面状态。

1. createProject → importMedia 或关联现有 assets → saveProject 设置 visible。
2. visibleSet → 用户确认 → confirmVisible；确认会递增项目版本，随后重读 project。
3. 选择流程与 Skill，createRun。请求固定 project_id、skill_id、execution_mode、visible_set_sha、goal、idempotency_key；可选 profile_id/revision、accept_external、accept_transcription、budget。
4. `external_mcp` 等外部 Agent 领取或自行创建；`builtin_api` 调 execute。内置模式使用预设 narration Provider 推理，沿用 token_env 凭证机制；运行缺 Key 明确失败。
5. plan_confirmation 时展示计划，人工通过 `PUT /api/skill-runs/{id}/plan` / `savePlan` 修改草案，或通过 voice 接口调整声音；Agent 改稿使用 `scripts.propose`。随后人工 finalize 精确 run version。
6. 继续 execute 或让外部 Agent 调工具。renders.create 产出 MP4/Premiere 包；renders.inspect 生成切点检查帧；Agent 读取帧后用 renders.review 提交机器检查结论；run.finish 校验同版成片和检查通过。机器检查不能写成人工验收。

Run 状态为 queued/running/waiting_human/succeeded/failed/cancelled。常见暂停原因：plan_confirmation、frame_budget_exceeded、tool_failed、context_changed、authorization_revoked、runtime_restarted、agent_clarification。计划有独立 version、finalized_by/at；改稿/改声音会使旧交付失效。失败或取消不会发布本步骤部分产物；旧修订文件保留审计。

budget 默认 max_steps=24、max_seconds=900、max_frames=24；硬顶为 64 步、3600 秒、48 个源关键帧。模型调用也计步并审计，时长累计计费操作的活跃时间，等待人工不计。切点检查另限 48 帧。外部 Agent 自有推理成本不可由 VAC 精确计量。每次人工局部修改、定稿或明确恢复会创建新的有界执行窗口；累计调用/时长不倒退。普通重试和撤销/重做不重置消耗。

## MCP 与工具

MCP 沿用已配置 Agent token 和真实身份，不接受 actor 参数代替鉴权。可发现工具：

| MCP 名称 | 用途 |
|---|---|
| skills.list / skills.get | 读取包、版本、正文、资源索引 |
| skills.read_resource | 按 id、content_sha256、resource 读取包内文本 |
| projects.list / projects.visible_set.get | 读取人工授权外部访问的项目与可见集 |
| skill_runs.create / claim | 创建或领取外部模式 run，绑定已认证 Agent 与角色 |
| skill_runs.get / cancel | 读取本人 run 的当前上下文或取消；步骤日志只向人工开放 |
| skill_runs.tool | 调用下面的受控工具；参数是 run_id + request |

项目访问许可不能绕过资产 AgentVisible、Locked 和角色白名单；run 归属检查阻止其他 Agent 读取/调用。MCP 不提供可见集确认、定稿、外发批准或人工验收动作。Skill 详情可独立读取，不需要 VAC 推理模型 Key；素材外发仍须项目及对应 Provider 授权。

```json
{
  "run_id": "skr_example",
  "request": {
    "name": "media.get_frames",
    "arguments": {"media_id": "source-id", "every": 3, "interval_seconds": 1, "threshold": 0.12, "coverage_seconds": 15},
    "expected_version": 1,
    "idempotency_key": "frames-once"
  }
}
```

| 工具名 | arguments / 结果 |
|---|---|
| media.list | {} → 冻结媒体列表，不含磁盘路径 |
| media.get_frames | media_id 可选、every/interval_seconds/threshold/coverage_seconds 可选；源帧及策略，超限停止并在步骤结果保留候选数 |
| media.transcribe | media_id；固定预设中的可选 ElevenLabs 转录，词级时间戳；按内容/窗口/预设缓存 |
| summaries.get / submit | submit 为 model、narrative、beats、timeline；每行 media_id/start/end/description/confidence/frame_ids；须引用真实提取帧 |
| scripts.propose | body、segments、subtitles；segments 含 media_id/start/end/narration；显式语法优先，模型不能创建声音外发许可 |
| renders.create | {}；仅精确定稿计划，复用 Node/FFmpeg；MP4 + Premiere ZIP |
| renders.inspect | {}；成片切点、首尾取帧与媒体参数检查；返回 inspection.frame_ids |
| renders.review | plan_version、frame_ids、verdict(passed/needs_changes)、issues；绑定当前检查集合 |
| artifacts.list / get | get 接受 id，返回受控小帧/转录的 base64；大成片通过 HTTP 文件 URL |
| run.finish | {}；同版正式成片 + 机器检查通过才完成，人工质量仍待验 |

Provider 的新增可选字段：

```json
{
  "transcription": {
    "api_format": "elevenlabs",
    "endpoint": "https://api.elevenlabs.io/v1",
    "model": "scribe_v1",
    "token_env": "ELEVENLABS_API_KEY"
  }
}
```

该字段纳入不可变预设及摘要、外发许可，不另开模型凭证通道。外部 MCP 模式可使用仅 builtin 视觉/解说加可选 transcription 的预设，无需配置 VAC 推理 Key；内置模式要求 configured narration。项目可固定 profile_id/revision 作为 MCP 工具预设，Agent 不能改投其它预设。默认无转录授权；流程 B 没有 ASR 前置依赖。

## 事件、持久化与交互边界

新事件 project.changed / skill.changed / skill.run.changed 已加入既有 SSE 与刷新函数；32 个全局刷新域包含 vu-projects/project/visible/skills/runs/run/context。查询键至少绑定 project 与 run；请求中携带 expected_version，结果读取后以返回的新 version 继续操作。切换项目取消旧 AbortSignal、停止旧播放器；页面隐藏/恢复不要保留旧授权勾选。

SQLite 保存状态、不可变输入和计划历史；project.json 与 summaries 是镜像。媒体与交付文件通过控制面 ID 下载，不拼磁盘路径。导出 ZIP 和 MP4 属于相同计划修订，旧包摘要/上下文不一致即拒绝下载。偏好与默认 Skill 保存在 Go；本地缓存不保存凭证或人工批准。

TTS 专页可以调用 saveVoice：expected_version、segment_index、enabled、settings(沿用 TtsSettings)、offset_seconds、accept_external。每次声音回写生成新计划修订并撤销旧定稿；实际合成、EQ/偏移、混音复用既有 TTS/Node 能力。现有工作台原 voice/delivery 接口继续保留。

## 兼容链与剩余验收

旧 workflow JSON 不变；TaskInput 新增可选 script，只有绑定工作台修订的自动 run 才出现。Go 再核对精确定稿；Python narrate 使用原文和选段，不重新写稿；TTS/字幕逐段开关与声音来自该快照；Node export 使用共同脚本渲染器；过期输入在领取/提交时阻断。无 script 的旧任务保持原路径。

本轮未提供新布局、新手引导页面或自动授权页面的视觉接线。真实云端转录/多模态/音色质量、游戏素材阈值、Premiere 实机导入、安装包与用户验收不由接口测试替代。原 CR03 前端焦点缺口、B04 Windows rename 调查继续独立记录。

外部 Agent 租约默认 2 分钟，工具执行期间延长至本次预算；过期排队运行可重新 claim，身份/角色重绑定后再次校验。人工确认不受闲置租约限制；人工恢复会刷新租约。旧绑定脚本任务要求使用支持 script 回执的新版 Worker，不能用缺失回执降级绕过检查。
