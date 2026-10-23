# Video Use — VAC adapter
Local edits arrive in run.edit_request with the human prompt and explicit
timeline/source start/end windows. Keep the segment count, body and global
subtitle option; change only selected segments and submit scripts.propose.
Never read operation logs or checkpoints. Human approval and undo/redo are not
Agent tools. Use skill_runs.get and idempotent tool replay for current state.
For an external MCP Agent, skills.get provides this package. Execute each media,
summary, script and render tool through skill_runs.tool using {run_id,request:
{name,arguments,expected_version,idempotency_key}}. Use skill_runs.get to refresh
the version. There is no MCP human approval tool.
Read upstream/SKILL.md when available using skills.read_resource. The upstream
workflow is guidance; the following VAC boundary applies to every action.
Route 2, 绘制, is a first-class strategy of this package, not an overlay accessory.
When the goal asks for programmatic picture, a title, UI motion, kinetic type,
or a film without source footage, follow this draw strategy and read
vac-draw-hyperframes, vac-draw-remotion, or vac-draw-pipeline.
Adapter version 0.1.0-vac.5 records that promotion. Draw tools, the sandbox,
and the render worker are not wired; the product phrase is 方案接线中.
Do not invoke a shell or install dependencies, and do not claim a draw render
succeeded. The draw page encodes three later handoffs: import into Editing after
render, composite-preview with edit tracks before an MP4 exists, and merge-render
of several draw slots together with edit tracks at the end.
Work only within the run's visible media. Never scan directories, invoke shell,
install dependencies or ask for secrets. Use media.list, media.transcribe,
media.get_frames, scripts.propose and renders.create. Return a readable dependency
failure if word timestamps are unavailable; offer visual_heavy without switching
the project yourself.
Use word-level transcript and on-demand visual evidence to propose cuts, retain
source timing, avoid cutting words, and preserve meaningful pauses. Confirm the
strategy by submitting a draft plan. Never approve or finalize it yourself.
scripts.propose takes body, segments [{media_id,start,end,narration}], subtitles.
The body contains one blank-line-separated paragraph per segment. Explicit
{@media-name}, {"forced subtitle"} and {黑屏}/{白屏}/{交叉溶解} take precedence.
When the run is waiting_human, stop and explain the decision. After a person
finalizes, renders.create produces the MP4 and Premiere package from that exact
plan. Inspect cut boundaries via media.get_frames or artifact evidence, then call
run.finish only when deliverables exist. No success claim from an unrendered plan.
Use at most the run budget. Do not fabricate visual evidence or human acceptance.

Read vac/tool-contracts.md using skills.read_resource with this package's content_sha256 before calling tools. It defines every argument/result and the flattened-plan inspection flow and the version-checked structured-sequence finish flow.
