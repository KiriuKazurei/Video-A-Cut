# VAC controlled tool contracts v3

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

| Name | arguments | result and gates |
|---|---|---|
| timeline.read | {sequence_id} (defaults to run.sequence_id) | Current sequence, session, revision, content_version; human history omitted. |
| references.resolve | {references:[{kind,target_id,resource_id?,sequence_id?,clip_id?,name,id,start,end,unresolved:false}]} | Typed frame/source anchors. A point is one legal frame, never a zero-length clip and never requires an out point. |
| timeline.edit | {sequence_id,command:{session,expected_revision,idempotency_key,label,operations:[...]}} | Atomic current-project edits on run.sequence_id only. Operations: track.add/update/remove, clip.insert/move/trim/split/update/link/unlink/delete/paste, marker.add/update/remove, gap.insert, sequence.rename, scope.set. history.undo/redo stay human-only. Lock/type/source failures roll back the whole command. Source identity is Agent and cannot be supplied as a human actor. |
| media.list | {} | Array of {id,asset_id,name,in,out,sha256}, no disk paths. |
| media.get_frames | {} or {media_id,every,interval_seconds,threshold,coverage_seconds} | {frames:[{id,media_id,pts,score,strategy,sha256}],strategy}. Optional ID selects one visible source; omit for all. every integer 1–30 (default 3), interval_seconds .25–30 (default 1), threshold .001–1 (default .12), coverage_seconds 1–120 (default 15). Frame budget overflow pauses, never silently truncates. |
| media.transcribe | {media_id} | {media_id,transcript,artifact}; transcript contains word-level timings and is cached by content/window/profile. Registers a transcript artifact. Requires a bound transcription Provider enabled in global provider settings and its configured key. Missing dependency: report it and stop; ask human to configure it or choose visual_heavy, do not change the flow yourself. |
| summaries.get | {} | {summary}, possibly null. |
| summaries.submit | {model,narrative,beats:[string],timeline:[{media_id,start,end,description,confidence,frame_ids:[string]}]} | {saved:true}; saves JSON/Markdown evidence summary. confidence is low/medium/high; every timeline item references existing extracted frames from that same media within its source interval. Do not invent evidence. |
| scripts.propose | {body,segments:[{media_id,start,end,narration}],subtitles:false} | Plan; run becomes waiting_human/plan_confirmation. body has one blank-line-separated paragraph per segment (1–20 segments), duration of each is .1–120 seconds within visible media. Optional segment subtitles_enabled overrides global subtitles. Narration may be empty. External voice calls use global provider settings. Proposals never finalize themselves. |


| renders.create | {} | Artifact array including same-plan MP4 and Premiere ZIP. Only after a human finalizes this exact plan/version and clears edit_request. |
| renders.inspect | {} | {inspection, engineering, basic_quality, human_acceptance}; inspection has plan_version/frame_ids/verdict/issues. basic_quality is bound to the MP4 sha256 and has status passed/failed/not_checked, audio_status/video_status and issues [{kind:black|silence,start,end}] in seconds. Missing/unknown audio expectations are skipped. Quality findings are advisory and do not block review/finish. Extracts rendered first/last/cut frames and validates media; inspection is REQUIRED before review/finish. |
| artifacts.list | {} | Current run artifact metadata [{id,name,kind,size_bytes,sha256,plan_version}]. |
| artifacts.get | {id:artifact_id} | Only registered frame/transcript artifacts up to 256 KiB. Frames return {artifact,mime_type} AND native MCP image content; inspect the image. Transcripts return {artifact,base64}, decode JSON text for word timestamps. IDs are not paths. Full films/packages are browsed/downloaded by the human in VAC. |
| renders.review | {plan_version,frame_ids,verdict:"passed",issues:[]} | Inspection. Read EVERY inspection frame via artifacts.get first. Use the exact ordered frame_ids from renders.inspect and current plan_version; verdict is passed/needs_changes, maximum 20 textual issues, passed requires no issues. This is machine review, not human acceptance. |
| run.finish | {} for a finalized flattened plan; {sequence_revision,content_version} for a structured edit without a flattened plan | {completed:true}; requires current human-finalized plan, no pending edit_request, same-plan valid MP4 and passed inspection/review. Structured sequence completion also returns basic_quality bound to its MP4. Quality warnings do not block completion. Never claim completion from an unrendered proposal. |


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
2. scripts.propose; stop on waiting_human and tell the user what to approve.
   Only the human can edit/finalize the plan through VAC. Refresh run after it.
3. renders.create → renders.inspect → artifacts.get for EACH inspection frame
   → renders.review with exact plan_version/frame_ids → run.finish.
4. When a human requests changes, skill_runs.get provides run.edit_request
   with prompt, base_plan and windows [{segment_index,media_id,timeline_start,
   timeline_end,source_start,source_end}]. Keep body, global subtitles, segment
   count and unselected segments unchanged. Modify only selected segments,
   scripts.propose again and stop for a NEW human finalization. Then repeat
   rendering, inspection, image reads, review and finish for the new plan.
5. Refresh context after conflict/timeout. If run is waiting_human because of
   dependency, tool failure, frame budget, clarification or context change,
   stop and ask the human; resume is human-only. Do not retry fresh billable
   steps, reset budgets, silently switch providers or recreate authorization.
   Revocation/invalid context denies execution. Cancellation is terminal;
   skill_runs.cancel {run_id,expected_version} stops this session's run.

Budgets default to 24 steps, 900 active seconds and 24 source frames (hard caps
64/3600/48); rendered inspection has its own 48-frame cap. Human decisions
open bounded execution windows, not unlimited work. Undo/redo does not refund
consumption or revive approvals. A detached session may need to claim its run
again after the old owner's lease ends; do not pretend another owner's run is
accessible. MCP never offers human finalization, resume, visible-set approval,
voice consent, operation logs, history checkpoints, undo or redo.
