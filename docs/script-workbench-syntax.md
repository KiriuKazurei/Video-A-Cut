# 脚本工作台与特殊语法 v1

## 与新 Skill 架构的关系（2026-10-19，设计）

[目标架构](Skill双流程架构设计.md)保留本语法与四态脚本。手写/Agent 生成是创作方式；新流程 A/B 是轻量/画面主导，二者不要混称。工作台未来可选择 Skill 和外部 MCP / 内置 API Agent；两种模式提交草案后均进入既有修订与定稿体系。

显式视频、字幕和效果语法优先于自由生成；冲突报错或澄清，不由 Skill 默默覆盖。video-use 自然语言目标可由 Agent 转成可审计编排，用户无需手写 EDL；流程 B 使用内容大意与用户脚本。新 Skill 入口尚未实现，下面仍是当前语法和源码边界。


更新：2026-10-18。脚本工作台与其他开发功能已合入 `agent/develop`（功能提交 d67e71d），原 `agent/script-workbench` 保留；尚未推送或人工全流程验收。此前最简验证只覆盖 d67e71d；本次 F2～F4 续作未测试、未构建、未运行，工程验证进度见 [流程优化进度](flow-optimize-progress.md)；后续 F3、F4 的要求仍见 [阶段合同](flow-optimize-stages.md)。

## 使用

进入「编辑 → 内容编辑」。先在资源页确认 Agent 可见集，再新建脚本。普通文字是生成要求，实际旁白在各段下方独立编辑；保存指令不会把“介绍抽卡机制”冒充成已生成的旁白。修改指令时，按原段内容匹配保留已有旁白、素材和声音；段数不变时保留同位置段。插入新段或改强制视频仍需人工检查绑定。

```text
{@抽卡演示.mp4}介绍抽卡机制。只介绍通用原理，不编造具体游戏概率。

{"这里展示抽卡结果", 字号:32px, 字体:"Microsoft YaHei",}{白屏}展示结果并说明随机性。

{交叉溶解}总结刚才的画面。
```

两条路径都需要最后人工定稿：

1. 手写：填指令、实际旁白、可见视频和源时间窗；保存草稿；人工定稿。
2. Agent：保存指令；选择模型预设并授权本次素材和修订；生成文稿；检查来源、逐段修改或否决/重生成；人工定稿。

草稿允许旁白和视频选择未填完整。未完成、尚有澄清问题或已否决的稿件不能定稿。只有特殊语法而无自然语言要求的段落可以没有旁白。模型不能定稿或恢复自动授权。

## 语法

空行分段；普通换行仍在同一段。块在段内任意位置都作用于整段，每段最多一个视频块、一个字幕块、一个效果块。

| 写法 | 行为 |
| --- | --- |
| `{@视频名称}` | 使用可见视频目录中的精确名称；不会访问任意路径或联网找视频。重名素材显示带稳定 ID 的名称，直接使用插入菜单。 |
| `{"字幕文字"}` | 强制显示原文，即使“自动字幕”关闭。 |
| `{"字幕文字", 字号:24px, 字体:"Microsoft YaHei",}` | 字号、字体分别可选，顺序不限，允许一个尾逗号。`opi:` 不属于语法。 |
| `{黑屏}` | 本段入口淡入黑色；有前段时，前段末尾淡出至黑色。 |
| `{白屏}` | 同上，使用白色。 |
| `{交叉溶解}` | 本段与前段重叠混合；第一段无前段时正常开始。 |

字幕使用 JSON 双引号字符串，可写 `\"`、`\\`、`\n`，字符串内部的逗号、冒号和花括号都是原文。普通文字中的花括号用 `\{`、`\}` 转义。块不是可执行 Java/C#，不能嵌套或写表达式。未知效果、未知/重复参数、未闭合块、重复块都显示错误并阻止保存/交付。

字号为 1～512px，可用小数；基于 1280×720 输出画布。省略字号默认 36px，省略字体优先使用本机中文字体，再使用其他可用字体。字体名必须来自“插入字体字幕”的本机字体族目录；不会下载字体，也不会默默替换明确指定但不存在的字体。

黑白淡变每侧最长 0.175 秒，短段不超过段长三分之一。溶解最长约 0.35 秒，同样受相邻段长三分之一限制，并对齐 30fps 整帧，重叠会缩短总时长。字幕在切入本段时切换。

脚本上限 64KiB、100 段；首版单次画面交付支持 1～20 段，每段 0.1～120 秒。视频窗口必须落在已确认可见 EDL 的窗口与真实媒体长度内。以上是当前实现边界，不是模型可以自行绕开的建议。

## 生成与查证

预设需为 configured，视觉和解说支持 OpenAI、Anthropic、Gemini 标准协议；未填写 api_format 沿用 OpenAI。自定义映射协议尚未接入本工作台生成入口。凭证从预设指定的本机环境变量读取。涉及外发需已有固定预设版本的外发授权；界面确认绑定资产、脚本修订、可见集、预设 ID 与版本。

Go 冻结当前可见目录并校验权限；Python 优先抽取强制视频，最多查看 min(预设帧预算, 24) 个片段，每个片段取一帧，受字节预算约束。视觉描述只代表这帧，不代表已完整看过整个视频。随后按项目 [Agent Skill](../skills/vac-script-syntax/SKILL.md) 写实际旁白、选择已查看视频和源窗口。

允许联网时，首版通过中文维基百科 API 查证最多前三段的文字要求，保留实际获得的 HTTPS 来源。检索只发送文字查询；媒体和模型凭证不发送给资料站。检索失败会保留为空来源，不会标成“已联网核实”。游戏或规则不明确时返回具体问题；把补充信息写入指令并保存后可重新生成。真实提供方的出稿质量、具体游戏知识覆盖仍待业务验收。

并发生成/导出只允许同一脚本执行一个；停止生成会取消当前请求。请求结束后再次校验修订、可见集及权限，迟到结果不能覆盖新稿。高亮只是编辑辅助，保存和交付均以 Go 解析及校验为准。

## 修订与自动流程

新增迁移 `0019_script_workbench.sql`，保留原 `scripts` 当前行，另存工作台元数据、不可覆盖的修订快照、同版交付引用。`workflow_runs` JSON 不变。首次保存额外保留修订 0；保存、生成、否决、人工定稿均记录操作者、时间、可见集与内容。

修改已定稿内容变为 `needs_refinalize(script_changed)`，旧交付不可继续下载。同一事务把相关活跃自动 run 停在 `script_changed` 决策卡。重新定稿后，人工通过既有 auto decision 接口发送：

```json
{
  "decision": "confirm_script",
  "feedback": "{\"script_id\":\"scr_x\",\"script_version\":4}",
  "expected_auto_version": 3,
  "idempotency_key": "本次唯一请求标识"
}
```

服务核对稿件所属资产、定稿状态、当前可见集及精确修订，再保存 scope 中的 `script_id` / `script_version`，恢复执行时扣除等待时长。未定稿、过期版本、`agent:` 和 `system:auto-review` 不能恢复。可见集变化仍优先走 `visible_set_changed` / `confirm_visible_set`。

旧自动 run 没有 visible_set_sha 时，不因普通可见集变更被卡死；使用工作台新稿后仍需人工确认。确认当前定稿时显式补绑定可见集与脚本范围。每次 confirm_script 的历史决定同时保存脚本 ID、修订、可见集指纹及人工 actor，后续改稿不会抹去此前授权范围。

此处接入的是自动流程的定稿确认门；既有自动 worker 串行流程尚未完整消费工作台段落稿，这一缺口继续保留。TTS 专页回写和直接脚本合成已接线；要交付工作台稿件，使用工作台或导出页内“生成成片与 Premiere 包”，不要把旧流程产物当作新稿成片。

旧 `POST /api/scripts/{id}/finalize` 保留 F1 兼容行为，但已保存过工作台修订的稿件必须使用带版本的定稿接口，防止绕过完整性检查。

## HTTP 与前端状态

| 方法/路径 | 用途 |
| --- | --- |
| `POST /api/script-workbench/parse` | `{body}` → 段落、指令、视频/字幕/效果及诊断。 |
| `GET /api/script-workbench/media` | 已确认可见视频目录；不返回磁盘绝对路径。 |
| `GET /api/script-workbench/fonts` | 本机安装的字体族。 |
| `GET /api/scripts/{id}/workbench` | 当前正文、版本、分段、来源、问题、开关及当前交付引用。 |
| `PUT /api/scripts/{id}/workbench` | 保存，需 expected_version / visible_set_sha / title / body / segments 与开关。 |
| `PUT /api/scripts/{id}/voice` | 专页回写；expected_version、visible_set_sha、segment_index、narration、settings、offset_seconds、enabled、accept_external；返回递增修订的工作台。 |
| `GET /api/scripts/{id}/revisions` | 最近 200 条修订；数据库保留全部。 |
| `POST /api/scripts/{id}/generate` | 需 expected_version / visible_set_sha / profile_id / profile_revision / allow_web_research。 |
| `POST /api/scripts/{id}/reject` | 否决；需 expected_version / visible_set_sha。 |
| `POST /api/scripts/{id}/workbench/finalize` | 人工定稿；同上。 |
| `POST /api/scripts/{id}/export` | 生成同版画面/Premiere 包；同上。 |
| `GET /api/scripts/{id}/delivery?version=N` | 当前有效修订的规格、时长、帧数、成片摘要与文件/大小/摘要清单。 |
| `GET /api/scripts/{id}/preview.mp4?version=N&sha256=H` | 当前有效修订成片；修订或同修订重渲染后的摘要不符则拒绝。 |
| `GET /api/scripts/{id}/delivery.zip?version=N&sha256=H` | 与播放器同修订、同成片摘要的 Premiere ZIP。 |

写操作 HTTP actor 固定 `human:webui`。乐观锁冲突返回 409，不覆盖其他会话已保存的新稿。SSE `script.changed`、`script.rendered` 仅通知失效，数据重新从 REST 读取。查询域包括 scripts / script-workbench / script-revisions / script-media / script-delivery / visible-set；重新连接与刷新快照会一并刷新。未保存稿只在组件内，不写 localStorage。

## 交付与后续验收

本机需要已配置的 Python worker、Node、FFmpeg/ffprobe 与 Node 字体依赖；运行环境使用现有 Go ConfigureTTS 的 Python 目录定位项目 Skill 和工作台 worker。此前开发源码运行已覆盖首版；本次已把 skills/vac-script-syntax 加入 Tauri 资源映射与桌面目录复制，资源配置尚未构建或发行验证。

全局 TTS/自动字幕为默认值，每段可分别覆盖。两者都关时仍按源窗口出片；只开字幕时使用实际旁白文字，不依赖配音。显式字幕块始终生效，自动字幕和效果预渲染到视频。

开启本段 TTS 后，保存草稿，再从段落跳到现有 TTS 专页；URL 绑定脚本 ID、修订和零起始段号，专页显示包含转场重叠的成片时间位置。选择音色、语气、三频均衡（120Hz/1kHz/4kHz，-12～12dB）及段内配音起点，回写生成新修订（voice 审计），旧定稿变为 needs_refinalize。回到脚本检查并人工重新定稿后，才能生成新成片。已有远程新修订不会被旧页面覆盖；未回写配音修改单独提示。

每段配音文本为 1～1000 字符；不自动截断、拉伸或切换引擎。StepFun 外部请求需要本段明确授权，修改文本或声音设置清除旧授权；模型生成也不能赋予 TTS 外发授权。未指定设置时进入 TTS 页优先发现本机 SAPI 声音，不自动改用云端。均衡与音调在下次合成生效；SAPI 不支持模型语气指令。原声在有旁白的段降低到 25%，按段内起点混入旁白；配音超出源段窗口则明确失败，需调整文本、语速或窗口。

声音快照是工作台 JSON 的可选 voice 字段，含 settings/offset_seconds/accept_external；段上 tts_enabled/subtitles_enabled 为空时继承全局默认。旧修订没有这些字段时沿用旧行为；不新增数据库迁移或改变 workflow_runs JSON。旧自动流程的 deliveryBlocked 保持原失效状态，不因直接脚本导出而恢复旧产物下载；脚本预览/ZIP 单独要求当前人工定稿修订、可见集和当前交付引用均有效。完整统一旧自动链仍待续作。

包内含 `final.mp4`、`edit.xml`、`timeline.otio`、`media/`、成片混音轨 `audio_Game.wav`、启用配音段的 `voice-*.wav`、`script.json`、`narration.txt`、`knowledge-sources.json`、`script-delivery.json` 和 `IMPORT.txt`。script.json 保留原始指令、语法树、实际旁白、媒体 ID 与相对源引用；schema_version=2 的清单保留脚本/修订/可见集、规格、成片与逐文件大小/哈希，Go 在发布交付引用前核验文件及预览/Premiere 视频副本摘要。站内播放器与下载链接携带修订和成片摘要。混音轨已烘焙，不承诺分开的原声/旁白可编辑轨道。Premiere 导入 edit.xml；移动交付目录时按 IMPORT.txt 重链接 media。它是预渲染画面，不承诺原生可调整的 Premiere 转场。

以下为续作前首版的历史工程证据，不能证明新配音/清单链路通过：真实 FFmpeg 工程验证已获得 720p 成片、中文抽帧、黑白亮度和溶解混合的证据。工作台 Go 8 项（含真实跨语言往返）、人工定稿门变异、Python 11、Node 21、前端 84、浏览器 7 组、类型/构建/样式、Skill 官方校验均通过；浏览器模型使用本机夹具。最新 Go service 全套仍记录 3 项 Windows rename 偶发拒绝访问，随后三个用例各 3 次复跑均通过；根因未确认，挂 B04，不能写成全套零失败。详细计数、证据与限制见 [流程优化进度](flow-optimize-progress.md)。

Premiere 实机导入、真实提供方知识/画面质量、模型阈值、独立安装包均单独待验，不能从本轮测试推定通过。本轮 TTS/交付接线没有运行证据；既有自动 worker 完整消费工作台稿件仍未实现。详细续作状态见 [清单](F2-F4续作清单-2026-10-18.md)。

扩展新语法时同时更新 Go 解析器、编辑器高亮、Skill、生成契约、渲染器和对应验收；不要让未支持的块静默失效。
