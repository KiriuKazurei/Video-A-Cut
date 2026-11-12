# VAC 工具表

目录：公开MCP入口 → 受控工具封装 → 媒体/成片 → 时间线/绘制 → 版本与失败。

这是 VAC 0.5.3 的操作速查；精确 schema 和当前执行包合同在调用前实时读取。`?` 表示可选，不是字段名的一部分；表内对象是结构说明。工具前缀可能由宿主添加，按发现结果调用。

## 公开 MCP 入口

| 工具 | 参数 | 用途/返回 |
|---|---|---|
| `skills.list` | `{}` | 执行包列表，检查启用状态、flow、mode、工具权限 |
| `skills.get` | `{id}` | 当前包正文、版本、`content_sha256`、资源索引 |
| `skills.read_resource` | `{id,content_sha256,resource}` | 读取该摘要的包资源；工具合同资源名为 `vac/tool-contracts.md` |
| `projects.list` | `{}` | 单个当前打开且可访问的工程；含可供领取的任务信息 |
| `projects.visible_set.get` | `{id:project_id}` | 当前可用媒体与指纹；已导入工程素材不需再建逐项可见审批 |
| `resources.list` | `{project_id}` | 视频/音频/图片/字幕/文档/脚本目录，无磁盘路径 |
| `resources.read` | `{project_id,resource_id}` | 脚本工作台正文与修订、文档/字幕文字或原生图片；视频/音频改用运行内媒体工具 |
| `sequences.list` | `{project_id}` | 序列及工程 session |
| `sequences.get` | `{project_id,sequence_id}` | `sequence`（含 `revision`）、`session`、`project_revision`、`content_version` |
| `references.list` | `{project_id,sequence_id?}` | 素材与标记候选；保留具体类型和稳定ID |
| `references.resolve` | `{project_id,references:[...]}` | 解析显式引用到源时间/帧；失效不会按同名替换 |
| `skill_runs.create` | 见下例 | 建立当前工程/目标序列的任务，返回 Run |
| `skill_runs.claim` | `{run_id,expected_version}` | 领取外部任务，身份/角色/租约仍需匹配 |
| `skill_runs.get` | `{run_id}` | 本任务最新 Run、冻结 Skill/media、允许工具及编辑上下文 |
| `skill_runs.progress` | `{run_id,expected_version,phase,output_chars?}` | 长模型调用时30–60秒报告实际状态，续两分钟租约；返回新版本，不消耗工具步数 |
| `skill_runs.edit` | `{run_id,expected_version,action,arguments}` | `plan`、`voice`、`target`；只改本任务，详见流程参考 |
| `skill_runs.update` | `{run_id,expected_version,action}` | `finalize` 确认旧待定计划，`resume` 修复依赖后恢复；预算不重置 |
| `skill_runs.cancel` | `{run_id,expected_version}` | 取消任务及所属进程，终态 |
| `skill_runs.tool` | `{run_id,request:{name,arguments,expected_version,idempotency_key}}` | 统一执行下列包内受控工具 |

创建示例（所有ID、摘要和目标按当前返回值替换）：

```json
{"project_id":"PROJECT_ID","sequence_id":"SEQUENCE_ID","flow":"lightweight","skill_id":"video-use","execution_mode":"external_mcp","strategy":"edit","visible_set_sha":"CURRENT_MEDIA_SHA","goal":"保留原素材声音，剪出展示关键步骤的短片","idempotency_key":"create-edit-01"}
```

`visual_heavy` 通常使用当前 `vac-visual-summary`；绘制可用 `flow:custom`、`strategy:draw`、`skill_id:vac-draw-pipeline`。以实际包支持的组合为准。可选 `profile_id/profile_revision`、`budget`、`accept_transcription/accept_external` 仅按实际配置和授权填写，不为消除报错盲目设 true。

## 受控工具封装

以下名字填入 `request.name`，不是各自独立的顶层MCP工具。所有操作先确保列在 `skill_runs.get.tools` 中。

```json
{"run_id":"RUN_ID","request":{"name":"media.list","arguments":{},"expected_version":1,"idempotency_key":"read-media-01"}}
```

`1` 仅演示字段类型；调用时使用最新 `run.version`。成功读 `structuredContent.run/result`，以返回版本继续。`isError:true` 或RPC错误不算成功；有返回诊断/版本也要处理失败。

## 素材、计划、配音和成片

| 名称 | arguments | 结果/限制 |
|---|---|---|
| `media.list` | `{}` | 冻结素材 `{id,asset_id,name,in,out,sha256}`；源窗口单位秒 |
| `media.get_frames` | `{media_id?,every?,interval_seconds?,threshold?,coverage_seconds?}` | `{frames,strategy}`；每帧有ID、源pts、SHA。every 1–30，间隔.25–30秒，阈值.001–1，覆盖1–120秒；按预算定向抽取 |
| `media.transcribe` | `{media_id}` | 词级 transcript 与 artifact；需配置提供方/本地依赖，缺失不静默换云 |
| `summaries.get` | `{}` | 已存 summary 或 null |
| `summaries.submit` | `{model,narrative,beats:[string],timeline:[{media_id,start,end,description,confidence,frame_ids}]}` | 视觉证据摘要；confidence 为 low/medium/high，每项帧必须来自对应素材及窗口 |
| `scripts.propose` | `{body,segments:[{media_id,start,end,narration,subtitles_enabled?}],subtitles,references?}` | 按段提出计划，由任务Agent确认并继续；1–20段，每段.1–120源秒；正文空行分段与segments对应 |
| `scripts.voice` | `{segment_index,enabled,settings,offset_seconds?,accept_external?}` | 段配音设置；也可用公开 `skill_runs.edit action:voice`。settings依据当前声音合同/配置，不猜提供方和模板 |
| `renders.create` | `{}` | 该确定计划版本的MP4和Premiere包，不能含待处理 edit_request |
| `renders.inspect` | `{}` | 当前计划检查与首/尾/切点帧ID；质量状态 passed/failed/not_checked，黑屏/静音为提示 |
| `artifacts.list` | `{}` | 已登记产物ID/名称/类型/大小/SHA/plan_version |
| `artifacts.get` | `{id}` | 帧返回原生MCP图片，必须实际看图；文字转录返回base64 JSON。帧上限16MiB，转录256KiB；不能当任意文件下载工具 |
| `renders.review` | `{plan_version,frame_ids,verdict,issues}` | 先读取inspect的每张图；原顺序完整frame_ids。verdict passed/needs_changes，passed的issues必须为空；最多20条问题 |
| `run.steps` | `{}` | 本任务工具结果与失败诊断；不提供人工历史 |
| `run.finish` | 依路线为 `{}` 或 `{sequence_revision,content_version}` | 普通计划要求同版成片与检查/review；结构化路线导出并验证指定序列；局部代码任务以目标写入为准 |

## 时间线与绘制

| 名称 | arguments | 结果/限制 |
|---|---|---|
| `timeline.read` | `{sequence_id?}` | 目标序列当前 TimelineView，无人工历史 |
| `references.resolve` | `{references:[...]}` | 运行内引用解析；不同于公开入口，不带project_id |
| `timeline.edit` | `{sequence_id,command:{session,expected_revision,idempotency_key,label,operations:[...]}}` | 原子事务；`expected_revision` 是序列revision，操作判别字段 `type` |
| `draw.capabilities` | `{}` | 实际文档schema、引擎/版本、节点与未支持能力 |
| `draw.shot` | `{action:"list",query?,offset?}` 或 `{action:"read",id}` | 按 ID 或画面描述找卡，读取模板源码、原生帧长和图片别名。以运行当前声明的工具为准 |
| `draw.list` | `{}` | 当前工程保存的构图 |
| `draw.save` | `{session,expected_revision,idempotency_key,composition,shot_card?}` | 此处expected_revision是 **project_revision**；composition另带自身revision。带卡号原样实例化时传构图身份和图片绑定，由服务端填源码和规格 |
| `draw.check` | `{session,composition_id,content_version}` | 检查源码、模块与时基，错误不能略过 |
| `draw.preview` | `{session,composition_id,content_version,frame,request_id?}` | 源码真实RGBA帧与artifact，不要求先渲MP4；消耗帧预算 |
| `draw.render` | `{session,composition_id,content_version,format?}` | 验证后的render/artifact；format为mp4、webm-alpha、prores4444 |
| `draw.get_frame` | `{session,composition_id,content_version,frame}` | 普通构图读已渲帧；统一文档可读源码帧；零基整数帧 |
| `draw.import` | `{session,composition_id,content_version,sequence_id,expected_revision,idempotency_key,source_mode?,target_track_id?,at?}` | 此处expected_revision为 **sequence.revision**；rendered需先渲染，snapshot/follow可直接入代码轨；at为序列帧 |

操作包括 `track.add/update/remove`、`clip.insert/move/trim/split/update/link/unlink/delete/paste`、`marker.add/update/remove`、`gap.insert/close`、`sequence.rename`、`scope.set`，以及局部 `code.script/replace/job`。只有列出名称不等于知道字段，复杂操作读取实时合同；禁止把 `op` 当 `type`。

## 四种版本和错误恢复

| 值 | 来源与用途 |
|---|---|
| `run.version` | Run；所有运行写调用/受控工具的外层expected_version |
| `sequence.revision` | TimelineView；timeline.edit和draw.import |
| `project_revision` | sequences.get；draw.save（工具日志也会推进它，保存前重读） |
| `composition.revision/content_version` | 构图保存结果；更新构图自身CAS/检查/帧/渲染/导入 |

计划 `plan.version` 另用于review/局部请求；`content_sha256` 是执行包摘要，不是成片SHA或序列content_version。

网络超时：以同名、同参数、原幂等键重放确定是否已执行。参数错误：读取诊断、修正后使用新键。内容冲突：刷新全部相关版本并重新判断操作，不能仅替换版本号覆盖人工更改。依赖/Worker/超时失败：定位后修复可修复项，再resume；同一未修复失败不重复调用。取消/撤销授权不可恢复。默认预算24工具步/900活跃秒/24源帧，上限64/3600/48，以实际Run为准；成片检查帧另有48上限。长思考期间无工具在途才发progress，phase为working/generating/checking/rendering，不上传模型文本。
