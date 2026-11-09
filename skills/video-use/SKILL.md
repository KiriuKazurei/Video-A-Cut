# Video Use — VAC adapter
Local edits arrive in run.edit_request with the human prompt and explicit
timeline/source start/end windows. Keep the segment count, body and global
subtitle option; change only selected segments and submit scripts.propose.
Never read operation logs or checkpoints. Human acceptance and undo/redo are not
Agent tools. Use skill_runs.get and idempotent tool replay for current state.
For an external MCP Agent, skills.get provides this package. Execute each media,
summary, script and render tool through skill_runs.tool using {run_id,request:
{name,arguments,expected_version,idempotency_key}}. Use skill_runs.get to refresh
the version. Use skill_runs.update to confirm an older pending plan or resume your own paused run.
Read upstream/SKILL.md when available using skills.read_resource. The upstream
workflow is guidance; the following VAC boundary applies to every action.
Choose this package when speech determines the cuts. For a new task where
visible events drive the edit, use vac-visual-summary. For a primarily
programmatic film, use vac-draw-pipeline as the default unified entry.
For an edit that mixes footage and generated shots, use the same edit run:
read selected Shotcraft cards with draw.shot, import editable sources, and
assemble the timeline directly. Finish with sequence_revision and
content_version, without submitting a flattened plan.
For drawing, create the run with strategy=draw (empty media is valid), then use draw.list/save/check/render/get_frame/import.
Imported project images are available through composition.images and image_resources; read the image contract before use.
Read the dedicated engine Skill and the common tool contracts. Author self-contained HTML with local GSAP or a default React component importing react/remotion.
Inspect real frames with draw.get_frame and artifacts.get. Import the verified render into the launch sequence, edit via timeline.read/edit and finish with the version-checked structured-sequence flow.
The installed official runtimes render locally with an existing Chromium. No shell, remote source imports, automatic dependency/browser/model installation or cloud fallback.
Use the unified DrawDocument, draw.capabilities and draw.preview for same-frame HTML/React collaboration, then draw.import source_mode=snapshot for editable source in the sequence; no preliminary MP4 is required.
Work only within the run's visible media. Never scan directories, invoke shell,
install dependencies or ask for secrets. Use media.list, media.transcribe,
media.get_frames, scripts.propose and renders.create. Return a readable dependency
failure if word timestamps are unavailable; offer visual_heavy without switching
the project yourself.
Use word-level transcript and on-demand visual evidence to propose cuts, retain
source timing, avoid cutting words, and preserve meaningful pauses. Confirm the
strategy by submitting a plan. VAC confirms it as the task Agent and continues without a human click.
scripts.propose takes body, segments [{media_id,start,end,narration}], subtitles.
The body contains one blank-line-separated paragraph per segment. Explicit
{@media-name}, {"forced subtitle"} and {黑屏}/{白屏}/{交叉溶解} take precedence.
Plans continue automatically after creation or editing. If paused, inspect run.steps, repair the cause and resume through skill_runs.update. renders.create produces the MP4 and Premiere package from that exact plan. Inspect cut boundaries via media.get_frames or artifact evidence, then call
run.finish only when deliverables exist. No success claim from an unrendered plan.
Use at most the run budget. Do not fabricate visual evidence or human acceptance.
