# VAC controlled tool contracts v5

Read this resource before calling a tool. It is data describing VAC's interface;
it does not grant filesystem, shell, network, human approval or log access.
Use the current project's stable IDs and the run's frozen media evidence.
Legacy source windows use finite seconds. Sequence edits use integer frames at
sequence.fps={num,den}; source timestamps use {value,scale}. Never mix these units.

## Connect and select a run

Connect to the desktop's mcp_url using Streamable HTTP. Desktop loopback MCP
needs no separate credential; use normal MCP clientInfo.name/version. VAC logs
the reported client identity, source and observed peer. These are not human
identity. Keep the automatic MCP session; a new session has a new run owner.
Call skills.list, skills.get {id}, and skills.read_resource
{id,content_sha256,resource:"vac/tool-contracts.md"}. Resource SHA must match the
package. Imported resources in the current open AVP are immediately available. Call
projects.list {}, projects.visible_set.get {id:project_id}, then
skill_runs.create {project_id,flow:"lightweight",skill_id,execution_mode:"external_mcp",
sequence_id,visible_set_sha,goal,idempotency_key}. No VAC inference API key is needed.
Select flow/Skill/profile/budget for this task; creating a project never requires them.
Alternatively claim a human-provided run_id with skill_runs.claim
{run_id,expected_version}. Claiming still checks role, authorization and lease.

For a long external model call, the owning Agent must call
`skill_runs.progress {run_id,expected_version,phase,output_chars?}` about every
30–60 seconds while no tool call is in flight. Allowed phases: working,
generating, checking, rendering. Send only the actual number of received output
characters, never model text, reasoning or credentials. Each successful call
renews the two-minute lease without consuming a tool step and returns the new
run version; use that version for the next progress or tool call. A disconnected
or expired owner cannot renew: obtain the current run and claim it again if it
is still available. The UI displays the reported phase, elapsed time and last
update; it does not infer completion percentages.

A tool argument or missing-image-alias error leaves the run queued with
`invalid_tool_arguments`. Read the diagnostic and current version, correct the
arguments or binding, then call again with a NEW idempotency key. Actual worker,
permission and timeout errors pause until repaired; a content conflict stays queued for refreshed context.

## Project resources

`resources.list {project_id}` lists the current project resources: video, audio, image, subtitle, document and script. `resources.read {project_id,resource_id}` reads text/subtitle/script content or returns a native image. Video/audio inspection uses a Skill run. These tools reject closed-project IDs and never take filesystem paths. There is no per-resource visibility approval. Switching the project cancels old work; rediscover projects before continuing.

## Envelope and results

Use sequences.list {project_id} and sequences.get {project_id,sequence_id} to read
the current sequence/session/revision/content_version. references.list
{project_id,sequence_id} exposes typed resource and marker candidates; direct
references.resolve {project_id,references:[...]} resolves explicit stable IDs.
Choose a concrete candidate when names collide, retaining kind, target_id,
resource_id, sequence_id and clip_id. Source-marker occurrences stay on that
clip instance. Removed/trimmed anchors fail rather than choosing a namesake.
MCP does not expose human edit history. These read tools accept no disk path.

Call skill_runs.get {run_id} for {run,skill,media,tools}. Only listed tools from
the frozen package are executable. Each skill_runs.tool call has this envelope:

```json
{"run_id":"skr_example","request":{"name":"media.list","arguments":{},"expected_version":1,"idempotency_key":"unique-action-1"}}
```

Success structuredContent is {run,result}; use the returned run.version in the
next request. run and plan have independent versions. Stable idempotency keys
identify one exact name/arguments pair; replay the SAME key after a timeout,
never generate another key for uncertain work. A replay returns the stored
result and current run without executing the action twice. Every new action
uses a new key (1–128 characters). Arguments are an object, at most 128 KiB;
the HTTP request including wrapper is at most 256 KiB. No extra actor/role/path
argument grants permission. Tool errors have isError=true and textual details;
HTTP/RPC errors are transport/input errors, not successful tool results.

## Arguments and observable results

Agent tasks are autonomous editing workflows. Its task authorizes
plan creation and rendering; scripts.propose records the actual Agent as the plan
confirmer and continues queued, without a separate human finalization click.
Human acceptance remains a separate optional review of the rendered result.
For a paused task, call skill_runs.update {run_id,expected_version,action:"resume"}.
For an older pending plan, use action:"finalize". Both operate only on this
authenticated Agent's own run and preserve its remaining execution budget.
Argument errors and stale-context conflicts leave the run queued: read the
diagnostic, refresh context, correct the request and use a NEW idempotency key.
They never require a human recovery click. Do not repeat unchanged failed work.
Actual dependency failures can be resumed by the owning Agent after repair.

Track compatibility and temporary render reservations:
- Video tracks accept ordinary movies, rendered drawings and editable drawing sources. Audio clips can move between compatible audio tracks across groups, including music, narration and linked movie audio; video and audio track types remain distinct. Comparison group membership is not a lock. Cross-group clip.move carries linked audio into matching destination audio tracks; insert mode ripples only the destination group.
- Only explicit track.update {track_id,locked:true/false} changes the lock. This is the same track.locked value used by the manual lock button. lock_owner records the triggering actor; it is not a second lock. An Agent can write into its own task reservation, but cannot acquire/release a manual or another task's lock. The human may explicitly unlock through the button.
- Before rendering into an existing video track: timeline.read, explicitly reserve it with timeline.edit/track.update locked:true, then draw.render; refresh the sequence revision and draw.import with target_track_id and at (integer sequence frames), then timeline.edit/track.update locked:false. Do not reserve an unrelated track or leave a reservation after insertion. Missing target_track_id retains the default new-group import; a target accepts rendered, snapshot and follow modes.
- A real tool execution failure, cancellation or completion releases only this task's remaining reservations through track.update. Stale/conflicting requests do not unlock ongoing work. Re-read the current lock/revision before retrying; never clear a user's lock to force an insert.

timeline.edit operations use the discriminant field "type", for example
{"type":"track.add","kind":"audio","name":"Music","group_id":"grp_current"}.
Read the returned generated track ID before a subsequent clip.insert.

| Name | arguments | result and gates |
|---|---|---|
| timeline.read | {sequence_id} (defaults to run.sequence_id) | Current sequence, session, revision, content_version; human history omitted. |
| references.resolve | {references:[{kind,target_id,resource_id?,sequence_id?,clip_id?,name,id,start,end,unresolved:false}]} | Typed frame/source anchors. A point is one legal frame, never a zero-length clip and never requires an out point. |
| timeline.edit | {sequence_id,command:{session,expected_revision,idempotency_key,label,operations:[...]}} | Atomic current-project edits on run.sequence_id only. Operations: track.add/update/remove, clip.insert/move/trim/split/update/link/unlink/delete/paste, marker.add/update/remove, gap.insert/close, sequence.rename, scope.set. history.undo/redo stay human-only. Lock/type/source failures roll back the whole command. Source identity is Agent and cannot be supplied as a human actor. |
| media.list | {} | Array of {id,asset_id,name,in,out,sha256}, no disk paths. |
| media.get_frames | {} or {media_id,every,interval_seconds,threshold,coverage_seconds} | {frames:[{id,media_id,pts,score,strategy,sha256}],strategy}. Optional ID selects one visible source; omit for all. every integer 1–30 (default 3), interval_seconds .25–30 (default 1), threshold .001–1 (default .12), coverage_seconds 1–120 (default 15). Frame budget overflow pauses, never silently truncates. |
| media.transcribe | {media_id} | {media_id,transcript,artifact}; transcript contains word-level timings and is cached by content/window/profile. Registers a transcript artifact. Requires a bound transcription Provider enabled in global provider settings and its configured key. Missing dependency: report it and stop; ask human to configure it or choose visual_heavy, do not change the flow yourself. |
| summaries.get | {} | {summary}, possibly null. |
| summaries.submit | {model,narrative,beats:[string],timeline:[{media_id,start,end,description,confidence,frame_ids:[string]}]} | {saved:true}; saves JSON/Markdown evidence summary. confidence is low/medium/high; every timeline item references existing extracted frames from that same media within its source interval. Do not invent evidence. |
| scripts.propose | {body,segments:[{media_id,start,end,narration}],subtitles:false} | Plan; runs continue queued with Agent confirmation. body has one blank-line-separated paragraph per segment (1–20 segments), duration of each is .1–120 seconds within visible media. Optional segment subtitles_enabled overrides global subtitles. Narration may be empty. External voice calls use global provider settings. Proposals are confirmed by the task Agent and continue queued. |


| renders.create | {} | Artifact array including same-plan MP4 and Premiere ZIP. Only after this exact plan/version is confirmed and edit_request is cleared. |
| renders.inspect | {} | {inspection, engineering, basic_quality, human_acceptance}; inspection has plan_version/frame_ids/verdict/issues. basic_quality is bound to the MP4 sha256 and has status passed/failed/not_checked, audio_status/video_status and issues [{kind:black|silence,start,end}] in seconds. Missing/unknown audio expectations are skipped. Quality findings are advisory and do not block review/finish. Extracts rendered first/last/cut frames and validates media; inspection is REQUIRED before review/finish. |
| artifacts.list | {} | Current run artifact metadata [{id,name,kind,size_bytes,sha256,plan_version}]. |
| artifacts.get | {id:artifact_id} | Only registered frame/transcript artifacts up to 256 KiB. Frames return {artifact,mime_type} AND native MCP image content; inspect the image. Transcripts return {artifact,base64}, decode JSON text for word timestamps. IDs are not paths. Full films/packages are browsed/downloaded by the human in VAC. |
| renders.review | {plan_version,frame_ids,verdict:"passed",issues:[]} | Inspection. Read EVERY inspection frame via artifacts.get first. Use the exact ordered frame_ids from renders.inspect and current plan_version; verdict is passed/needs_changes, maximum 20 textual issues, passed requires no issues. This is machine review, not human acceptance. |
| run.finish | {} for a finalized flattened plan; {sequence_revision,content_version} for a structured edit without a flattened plan | {completed:true}; requires the current confirmed plan, no pending edit_request, same-plan valid MP4 and passed inspection/review. Structured sequence completion also returns basic_quality bound to its MP4. Quality warnings do not block completion. Never claim completion from an unrendered proposal. |


scripts.propose also accepts references:[...] using the AVS v2 occurrence binding
contract (start/end are UTF-16 offsets). A typed marker is an anchor, not a video
name. Keep the bound source frame inside its associated generated source window.
For structured track editing, first timeline.read; submit a CAS command through
timeline.edit using stable clip/track/resource IDs. Read the resulting revision
and content_version. With no flattened plan, run.finish accepts
{sequence_revision,content_version}, validates that exact shared composition,
publishes its MP4 and completes the run without duplicating its edited tracks.
This verifies rendering, not human perception or acceptance.

Full-film Skills retain the finalize/render/inspect/review workflow below. A
successful verified MP4 is imported once as a new resource and a parallel Agent
track group at frame zero in the launch sequence. The original edit remains.
Default comparison isolates the new group; original/all are explicit choices.
Flattened MP4s are single clips; never invent internal cuts from the movie.
Frame-content modification is disabled and returns not_implemented; do not
create generation jobs or contact a provider for it.

## Required sequence, local changes and recovery

1. Get context; read media and applicable transcript/frame evidence. For visual
   summary, submit evidence summary before proposing the plan.
2. scripts.propose; in the open desktop project continue with the confirmed plan. For an older pending plan, call skill_runs.update action=finalize.
3. renders.create → renders.inspect → artifacts.get for EACH inspection frame
   → renders.review with exact plan_version/frame_ids → run.finish.
4. When a human requests changes, skill_runs.get provides run.edit_request
   with prompt, base_plan and windows [{segment_index,media_id,timeline_start,
   timeline_end,source_start,source_end}]. Keep global subtitles, segment count, and unselected segments (including voice settings) unchanged. Script body paragraphs may change only at selected indices, with the same paragraph count. Both plan saves and scripts.propose enforce this scope. Modify only selected segments,
   scripts.propose again, confirm that new version and then repeat
   rendering, inspection, image reads, review and finish for the new plan.
5. Refresh context after conflict/timeout. If run is waiting_human because of
   dependency, tool failure, frame budget, clarification or context change,
   repair the cause, then use skill_runs.update action=resume. Do not retry unchanged billable
   steps, reset budgets, silently switch providers or recreate authorization.
   Revocation/invalid context denies execution. Cancellation is terminal;
   skill_runs.cancel {run_id,expected_version} stops this session's run.

Budgets default to 24 steps, 900 active seconds and 24 source frames (hard caps
64/3600/48); rendered inspection has its own 48-frame cap. Human decisions
open bounded execution windows, not unlimited work. Undo/redo does not refund
consumption or revive approvals. A detached session may need to claim its run
again after the old owner's lease ends; do not pretend another owner's run is
accessible. MCP records Agent confirmation and resume through skill_runs.update; it never impersonates human acceptance or grants visible-set approval,
voice consent, operation logs, history checkpoints, undo or redo.


## Local code drawing (executable)

Use video-use with strategy="draw", or vac-draw-hyperframes / vac-draw-remotion /
vac-draw-pipeline (drawing is their default). All three drawing packages are
executable harness Skills and may be selected proactively for appropriate goals.
Empty media is valid for drawing; ordinary source editing still requires media.
No provider/key is needed for an external MCP Agent. Builtin model narration uses
the configured provider policy. Neither route receives arbitrary shell access.

Read skill_runs.get for draw_compositions and sequences.get for session and
project_revision. Read sequences.get immediately before draw.save: run/tool
journal changes can advance the project revision. Composition revision is a
second CAS: use the saved composition.revision, or 0 for a new stable ID.
One tool is one run step; every inspected draw frame consumes MaxFrames.
Existing wall-clock budgets cover bundling, rendering and export.

| Tool | arguments | result |
|---|---|---|
| draw.list | {} | Current project's saved composition array |
| draw.save | {session,expected_revision,idempotency_key,composition:{id,name,engine,source,width,height,fps:{num,den},duration_frames,revision}} | Project session with saved draw_compositions including new revision/content_version. expected_revision is project_revision. |
| draw.check | {session,composition_id,content_version} | {valid,checks,engine,runtime_version,content_version}; syntax, module and timebase errors fail the step. |
| draw.render | {session,composition_id,content_version} | {render,artifact}; actual local MP4, verified size/frame count/fps/duration/SHA, registered as a run artifact. |
| draw.get_frame | {session,composition_id,content_version,frame} | {frame,artifact}; integer zero-based frame in the current verified render, bounded by the run frame budget. Read artifacts.get {id:artifact.id} for a native MCP image. |
| draw.import | {session,composition_id,content_version,sequence_id,expected_revision,idempotency_key,target_track_id?,at?} | TimelineView; expected_revision is sequence.revision. Adds the resource to target_track_id at the specified sequence-frame at (default 0); without a target creates an independent Agent track group on run.sequence_id. Replay uses the SAME key. |

HyperFrames source is a complete inline HTML document. Give its root an id,
data-composition-id, data-width, data-height and data-duration matching the spec.
Local GSAP is injected; register paused deterministic frame-driven timelines in
window.__timelines keyed by root data-composition-id.
Remotion source is a TSX module with a default React component; import only react
and remotion. VAC creates the Composition/registerRoot entry from the saved spec.
Use useCurrentFrame/useVideoConfig/interpolate for deterministic motion.

Both render in owned external Chrome/headless profiles, outside the VAC WebView,
with module checks, document CSP sandbox and same-owned-origin request filtering.
Inline graphics, bound imported images and renderer-owned files are supported;
network sources, filesystem imports, arbitrary commands and installed packages
outside the documented modules are unavailable. Missing runtime/browser is an
explicit dependency failure; no silent browser/model downloads or cloud fallback.
Even dimensions: width 16..3840, height 16..2160; fps 1..120; 1..18000 frames,
at most 300 seconds; source at most 96 KiB; at most 16 compositions per project.

After draw.import use timeline.edit to split/move/trim/effect the rendered video.
Finish via run.finish {sequence_revision,content_version} read from the latest
TimelineView. This uses the shared sequence compositor, validates the export and
returns an MP4 artifact with human_acceptance="not_checked". Do not submit a
flattened plan or ask for plan finalization in this structured drawing route.
Unified documents render HTML and React source nodes together on one integer-frame clock. draw.preview inspects the actual shared RGBA canvas without requiring a movie. Source nodes preserve native HyperFrames semantics, isolated HTML, React frame context and optional independent source canvas/fps/duration. No engine choice is required from the user.

| Tool | arguments | result |
|---|---|---|
| draw.capabilities | {} | Current-project document schema, engines/versions, common nodes, alpha/color and explicitly unsupported capabilities. No installs/downloads. |
| draw.preview | {session,composition_id,content_version,frame,request_id?} | {frame,artifact}; actual current source pixels, consumes one frame budget, no MP4 prerequisite. |

New draw.save composition.document uses schema_version=1, canvas {width,height,fps:{num,den},alpha:"straight",color_space:"srgb"}, duration_frames, preset (auto/interface/data/media), sources [{id,engine,source,reason,canvas?,duration_frames?}], nodes [{id,kind,name?,source_id?,resource_id?,parent_id?,from,duration,source_offset?,speed?:{num,den},transform:{x,y,width?,height?,rotation?},opacity?,z?,hidden?,text?,color?,font_size?,parameters?,keyframes?}]. Kinds are source/image/text/group/mask/shape; graph cycles fail. keyframes are strictly increasing local frames with x/y/optional opacity; outer motion is independent of source animation. All ranges are half-open. Go generates composition engine/specification and plan, binds current-project image fingerprints, and returns the saved revision/content_version. AVP3 retains the entire editable document; AVP1/2 remain readable and AVS stays2. Legacy single-engine composition arguments remain valid.

draw.import additionally accepts source_mode="snapshot" (frozen editable source) or "follow" (explicitly follows the saved source). Neither needs a rendered file. Omitted/"rendered" requires the verified MP4 as before. The run launch sequence restriction, CAS, idempotency and one undo transaction apply to all modes. Existing footage, sound and subtitles use the shared sequence compositor; source frames use the same lossless drawing FrameSource before one final encode. Historical exports stay frozen. draw.get_frame also uses live source pixels for unified documents.

Windows uses only installed Chrome/headless shell. The builtin loop refreshes project revision before draw.save, while composition revision still requires CAS. No runtime downloads or cloud fallback.


## Current-project image instances

composition.images is optional. In unified documents these bindings are normalized into canonical image nodes; source mode becomes a hidden node. Source image_bindings are normalized the same way. Do not use document.images. Static and dynamic missing image aliases fail explicitly instead of displaying a silent fallback. An entry is:
{"id":"hero","resource_id":"res_imported_image","x":40,"y":20,
 "width":320,"height":180,"dx":200,"dy":40,"from_frame":0,"to_frame":89,
 "fit":"contain","opacity":1,"mode":"layer"}.

Go binds the imported image SHA and rejects IDs outside the open project.
resource_sha256 in saved data is server-owned. Multiple instances may share
one resource_id; instance IDs are unique inside the composition. Up to 64
instances and 32 image originals are supported. x/y/width/height/dx/dy are
canvas pixels, from_frame/to_frame are integers. Position before the interval
is x/y; after it is x+dx/y+dy; inside it is linear interpolation. Equal frame
endpoints mean static placement. fit is contain/cover/fill, opacity is 0..1.

mode=layer automatically renders the image above the scene. mode=source passes
it to source code and does not add an automatic overlay. HyperFrames uses
<img src="vac-image:hero">, and window.vacImages.hero supplies the parameters.
Remotion receives {images} in its default component and uses
<Img src={images.hero.src} style={{position:"absolute",left:images.hero.x,
top:images.hero.y,width:images.hero.width,height:images.hero.height}}/>.
Import Img/useCurrentFrame from remotion for custom frame-driven animation.
Neither reference accepts a disk path, remote URL or another project's ID.

Image resources are resolved and hash-checked by Go, converted locally to PNG,
and served only from the renderer's owned origin. PNG/JPEG/WebP/BMP/TIFF and
the first static frame of GIF/APNG are supported; originals are never changed.
Prepared images are bounded to 8192 pixels on each side while retaining aspect
and alpha. The rendering dependency includes each image SHA and every instance
parameter; missing/removed/changed sources fail rather than reusing stale pixels.
Relink and save to refresh the binding. AVP keeps the original resource index
and per-instance parameters; removing an image retains referenced placeholders.

For draw.render, optional format is mp4 (default), webm-alpha or prores4444. The returned artifact uses the matching extension. PNG/RGBA source frames remain lossless before the single final encode. crop uses canvas pixels; shape/mask supports rect or ellipse. source.capabilities may declare dom/gsap for HTML, react/svg for React, and rgba/transform/image/text/mask for either; unsupported requirements fail with source ID.

artifacts.get reads verified frame PNG/JPEG artifacts up to 16 MiB; transcript reads remain limited to 256 KiB. This supports full RGBA inspection without silently downscaling detailed frames. Project, run, hash and tool budgets still apply.

## Autonomous task editing and diagnostics

Use skill_runs.edit {run_id,expected_version,action,arguments} for your own task.
Actions: plan with {plan:{body,segments,subtitles}}, voice with
{segment_index,enabled,settings,offset_seconds?,accept_external?}, target with
the targeted-edit request {plan_version,prompt,artifact_id,artifact_sha256,
idempotency_key,segment_index? or at_seconds? or timeline_start?/timeline_end?}. Returns {run}. Creation and edits confirm the
new plan as the task Agent without a human click, including legacy task flows.
Voice follows the configured provider policy; no new credential is required.
Within skill_runs.tool, scripts.voice accepts the same voice arguments;
run.steps {} reads this task's recorded tool results for failure diagnosis.
These tools are available to the builtin model loop too. Read the returned
version before continuing. Repair/resume and export do not constitute human
acceptance of the movie.


## 代码轨与局部代码条任务

新增时间线轨道类型 `code`，只接收 `draw_source` 源码画面；渲染出的 MP4 仍进入 `video`。HTML/Canvas 使用 HyperFrames，React 使用 Remotion，Motion Canvas 使用 `motion_canvas`（3.17.2），源码导出 `makeScene2D(function* (view) {...})`，只导入 `@motion-canvas/core`、`@motion-canvas/2d`。不使用 Vite 编辑器或远程依赖，不下载浏览器。

不同画面代码由 Agent 明确交付成独立完整段；参数动画留在条内。`TimelineClip.code` 保存实例级 `description`、`generated_description`、`revision`、描述状态及最近 `run_id`，读取时间线时补充 `content_version`。内容指纹包含片段/源码身份与参数，排除时间线起点及任务状态，允许前序波纹后继续应用，但拒绝人工修改后的旧结果。复制/切割后的实例独立编辑，源码替换新建快照资源，保留旧资源供撤销。

现有 `timeline.edit` 的 command.operations 扩展：

- `code.script {clip_ids:[id],description,code_revision,mode?}`：保存描述并解析参数；`mode:describe` 只补齐空描述，保留原画面和参数。参数使用下述语法，正文保存为普通创作描述，空行不新建代码条。
- `code.replace {clip_ids:[id],code_version,code_segments:[{composition:完整DrawComposition,description}]}`：一个目标可替换成1–32个独立段；源码静态检查和实际首帧检查通过后，一次提交替换及当前轨组波纹。沿用锁定/链接规则，缩短范围仍有未选择内容时明确拒绝。每个目标一个事务和撤销项，失败不回滚其他成功目标。
- `code.job {clip_ids:[id],code_run_id}`：绑定已创建任务的状态，不更改源码或参数。

`skill_runs.create` 增加 `code_targets:{clip_id:content_version}` 与 `code_action:describe|regenerate`；使用现有 `strategy:draw`、`vac-draw-pipeline`、内部API/外部MCP运行。服务端限制这些任务只能提交捕获目标的相应代码操作，不允许 `draw.save/import` 或普通时间线写入。任务上下文和内部模型输入提供 `code_sources:{clip_id:DrawComposition}`，包含实例的快照或当前跟随源。失败的替换操作保存在 `run.code_candidates`，可在冲突解除后通过工作台应用保留结果。至少一次目标写入成功后，可用 `run.finish {}`结束局部任务，不强制渲染整片。

只有代码没有描述时，入轨或打开旧工程即安排现有Agent运行；有已配置内部模型时自动执行，否则排队供外部Agent领取。用户新输入的描述、修订冲突、取消或已删除目标不会被迟到结果覆盖。外部Agent通过 `projects.list` 查看排队任务，`skill_runs.claim`领取后使用现有工具协议；复制任务不表示已派发。

代码条声音声明在 `DrawDocument.audio_bindings`：`{resource_id,resource_sha256?,at_frame,duration_frames,source_in,stream_index,gain_db}`。帧采用源码FPS，source_in单位秒；只引用当前工程已导入的真实音频流，Go解析路径，Node统一混音。音量线控制此条声明的声音；独立旁白和背景音乐不自动绑定。隐藏只影响代码画面，静音只影响代码声音。

### 代码条参数语法

沿用 `{名称, 参数:值}`，首版支持：

```text
{音量, 时间:00:01:10, 增益:-6dB}
{移动, 时间:00:01:10, x:100px, y:0px, 持续:2s}
{旋转, 时间:00:01:10, 角度:30, 持续:2s}
```

工作台显示/输入序列时间（时:分:秒或时:分:秒:帧），按序列有理FPS和整数帧转换为条内时间存储。移动代码条后只更新显示，动画时刻仍相对代码条。像素位置相对画布中心，角度顺时针；省略持续表示该时刻切换，持续使用秒并线性插值。音量包络回显可带 `起始增益`，以无损表示已有增益线的起止值。时间越界、重复时刻、重叠动画、未知/缺失参数和括号不完整均报错。

参数编辑与既有增益线/Inspector共用效果模型，自动保存后直接预览，无需Agent；普通描述修改只进入待生成草稿。多选按时间起点、同起点按轨道顺序排列，显示名称/编号与对应颜色；生成只提交描述发生变化的条。预览和正式导出共用参数解释器，切割/修剪保持原动画速度，正式导出不降采样。
