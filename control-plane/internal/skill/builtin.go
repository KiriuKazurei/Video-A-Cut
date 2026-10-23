package skill

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed builtin/video-use/*
var upstream embed.FS

//go:embed builtin/vac-script-syntax.md
var syntaxContent string

//go:embed builtin/vac-tool-contracts.md
var ToolContracts string

// Builtins use the same snapshot contract as imported packages. Upstream documents
// are reference data; VAC tool permissions are defined by the adapter.
func Builtins() []Package {
	tools := []string{"media.list", "media.get_frames", "media.transcribe", "summaries.get", "summaries.submit", "scripts.propose", "renders.create", "renders.inspect", "renders.review", "artifacts.list", "artifacts.get", "run.finish", "timeline.read", "timeline.edit", "references.resolve"}
	modes := []string{"external_mcp", "builtin_api"}
	packages := []Package{
		{ID: "video-use", Name: "Video Use", Description: "VAC route-2 draw entry and lightweight edit adapter. Draw tools are not wired.", Version: "0.1.0-vac.5", Source: "builtin", Builtin: true, Enabled: true, Runtime: "harness", Flows: []string{"lightweight"}, Modes: modes, Tools: tools, Permissions: []string{"project_media", "provider", "ffmpeg"}, Inputs: []string{"visible_media", "goal"}, Outputs: []string{"plan", "mp4", "premiere_package"}, Content: VideoUseAdapter, Resources: map[string]string{}},
		{ID: "vac-visual-summary", Name: "画面大意与编排", Version: "1.2.0", Source: "builtin", Builtin: true, Enabled: true, Runtime: "harness", Flows: []string{"visual_heavy", "custom"}, Modes: modes, Tools: tools, Permissions: []string{"project_media", "provider", "ffmpeg"}, Inputs: []string{"visible_media", "goal", "script"}, Outputs: []string{"keyframes", "content_summary", "plan", "mp4", "premiere_package"}, Content: VisualAdapter, Resources: map[string]string{}},
		{ID: "vac-script-syntax", Name: "VAC 脚本语法", Version: "2.0.0", Source: "builtin", Builtin: true, Enabled: true, Runtime: "instructions_only", Flows: []string{"lightweight", "visual_heavy", "custom"}, Modes: modes, Tools: tools, Permissions: []string{"project_media", "ffmpeg"}, Inputs: []string{"script", "visible_media"}, Outputs: []string{"plan"}, Content: SyntaxAdapter, Resources: map[string]string{}},
	}
	_ = fs.WalkDir(upstream, "builtin/video-use", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		body, e := upstream.ReadFile(path)
		if e != nil {
			return e
		}
		packages[0].Resources["upstream/"+strings.TrimPrefix(path, "builtin/video-use/")] = string(body)
		return nil
	})
	packages[0].Upstream = "https://github.com/browser-use/video-use/tree/43cfc566833548093455c9d4a46bd69913b7e552"
	packages[2].Content = syntaxContent + "\n" + SyntaxAdapter
	for i := range packages {
		packages[i].Resources["vac/tool-contracts.md"] = ToolContracts
		packages[i].Content += "\nRead vac/tool-contracts.md using skills.read_resource with this package's content_sha256 before calling tools. It defines every argument/result and the flattened-plan inspection flow and the version-checked structured-sequence finish flow.\n"
	}
	// Draw stubs stay instruction-only. They are not given the harness tool footer.
	return append(packages, drawStubs()...)
}

func drawStubs() []Package {
	modes := []string{"external_mcp", "builtin_api"}
	stub := func(id, name, description, content string) Package {
		return Package{
			ID: id, Name: name, Description: description, Version: "0.1.0", Source: "builtin", Builtin: true, Enabled: true,
			Runtime: "instructions_only", Flows: []string{"custom"}, Modes: modes, Tools: []string{}, Permissions: []string{},
			Inputs: []string{"goal", "composition_spec"}, Outputs: []string{"composition_source"}, Content: content, Resources: map[string]string{},
		}
	}
	return []Package{
		stub("vac-draw-hyperframes", "HyperFrames 绘制", "Instruction-only HyperFrames composition stub. No shell and no render worker.", hyperframesDraw),
		stub("vac-draw-remotion", "Remotion 绘制", "Instruction-only Remotion composition stub. No shell and no render worker.", remotionDraw),
		stub("vac-draw-pipeline", "绘制管线", "Instruction-only shared draw pipeline. No shell and no render worker.", pipelineDraw),
	}
}

const VideoUseAdapter = `# Video Use — VAC adapter
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
`
const VisualAdapter = `# VAC visual summary
For run.edit_request, apply the human prompt only to its listed timeline/source
windows; preserve body, global subtitles, segment count and unselected segments.
Propose the edited plan and stop for human approval. Operation logs/checkpoints
and undo/redo are human-only and are never available through MCP.
External MCP Agents invoke the named tools through skill_runs.tool with run_id
and a versioned, idempotent request. Human approval remains in the VAC UI/API.
No ASR dependency. List visible media; extract frames with media.get_frames.
Default continuous high-change sampling keeps one in three candidates; include
start/end and low-change coverage. Budget overflow is a human decision, never
silent truncation. Use actual frames to write a structured content summary with
timeline [{media_id,start,end,description,confidence,frame_ids}], narrative,
beats and model identity. Submit through summaries.submit. Do not infer unseen
actions from a still image. Read summary plus the user's script to create a
plan via scripts.propose. Keep explicit script syntax and fixed source windows.
Stop for human finalization. Render the exact finalized plan through
renders.create, then inspect and finish. Never approve your own plan.
`
const hyperframesDraw = `# vac-draw-hyperframes

Instruction-only HyperFrames composition skill for VAC route 2 (绘制).
Phase 1 does not render. Lint, validate, render, sandbox install, and import are 方案接线中.
Do not invoke a shell or install dependencies. Do not expose a shell to the frontend.
HTML and CSS addressable animation is the composition model. A future worker may require Node.js 22 or newer; this package does not start that worker.
Keep composition source until a DRAW01 worker exists. An MP4 is not a reversible composition source.
Handoffs, typed by the draw page and not executed here:
- import_into_editing: after render, register the product as a project resource and place it on the editing timeline.
- composite_preview: an unrendered composition may preview together with edit tracks. No MP4 is required.
- merge_render: multiple draw slots and edit tracks render together at the end. Do not force a per-slot export first.
video-use may delegate here. This package does not replace that route-2 entry.
`
const remotionDraw = `# vac-draw-remotion

Instruction-only Remotion composition skill for VAC route 2 (绘制).
Phase 1 does not render. Scaffold, render, sandbox install, and import are 方案接线中.
Do not invoke a shell or install dependencies. Do not expose a shell to the frontend.
A React component tree is the composition model. This package does not start a renderer.
Keep composition source until a DRAW01 worker exists. An MP4 is not a reversible composition source.
Handoffs, typed by the draw page and not executed here:
- import_into_editing: after render, register the product as a project resource and place it on the editing timeline.
- composite_preview: an unrendered composition may preview together with edit tracks. No MP4 is required.
- merge_render: multiple draw slots and edit tracks render together at the end. Do not force a per-slot export first.
video-use may delegate here. This package does not replace that route-2 entry.
`
const pipelineDraw = `# vac-draw-pipeline

Instruction-only shared draw pipeline for VAC route 2 (绘制).
It describes how vac-draw-hyperframes and vac-draw-remotion meet the editing timeline.
Phase 1 does not call a worker. Lint, render, sandbox, and the three handoffs are 方案接线中.
Do not invoke a shell or install dependencies. Do not expose a shell to the frontend.
import_into_editing registers a finished render as a project resource and an editing clip.
composite_preview composites unrendered slots with edit tracks and does not require an MP4.
merge_render combines every selected draw slot with the edit tracks in one final render.
Choose the engine from the composition model. HyperFrames is the default page structure; Remotion is for a React component tree. Mixing a draw layer with an edit track does not create a third product route.
`
const SyntaxAdapter = `# VAC script syntax
Blank lines separate paragraphs. Ordinary text is the user's instruction, not
automatically narration. Explicit typed {@recognition-name} binds a resource or frame anchor; resolve IDs using references.resolve. A video binding chooses one current-project
source; {"text",字号:32px,字体:"Microsoft YaHei",} forces a subtitle.
{黑屏}, {白屏}, {交叉溶解} are supported entry effects; unknown or repeated
blocks must fail. No expressions or arbitrary disk paths. Segment narration is
independent text. Human finalization freezes the exact plan; subsequent changes
require a new finalization. Use scripts.propose and the shared renderer. For structured edits, timeline.read/edit operate only on the launch sequence with session/revision/idempotency checks; history and undo remain human-only. Single markers are valid frame anchors. After editing, run.finish uses sequence_revision and content_version from timeline.read; do not invent human acceptance.
`
