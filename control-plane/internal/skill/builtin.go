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
	tools := []string{"media.list", "media.get_frames", "media.transcribe", "summaries.get", "summaries.submit", "scripts.propose", "renders.create", "renders.inspect", "renders.review", "artifacts.list", "artifacts.get", "run.finish"}
	modes := []string{"external_mcp", "builtin_api"}
	packages := []Package{
		{ID: "video-use", Name: "Video Use", Version: "0.1.0-vac.3", Source: "builtin", Builtin: true, Enabled: true, Runtime: "harness", Flows: []string{"lightweight"}, Modes: modes, Tools: tools, Permissions: []string{"project_media", "provider", "ffmpeg"}, Inputs: []string{"visible_media", "goal"}, Outputs: []string{"plan", "mp4", "premiere_package"}, Content: VideoUseAdapter, Resources: map[string]string{}},
		{ID: "vac-visual-summary", Name: "画面大意与编排", Version: "1.1.1", Source: "builtin", Builtin: true, Enabled: true, Runtime: "harness", Flows: []string{"visual_heavy", "custom"}, Modes: modes, Tools: tools, Permissions: []string{"project_media", "provider", "ffmpeg"}, Inputs: []string{"visible_media", "goal", "script"}, Outputs: []string{"keyframes", "content_summary", "plan", "mp4", "premiere_package"}, Content: VisualAdapter, Resources: map[string]string{}},
		{ID: "vac-script-syntax", Name: "VAC 脚本语法", Version: "1.0.1", Source: "builtin", Builtin: true, Enabled: true, Runtime: "instructions_only", Flows: []string{"lightweight", "visual_heavy", "custom"}, Modes: modes, Tools: tools, Permissions: []string{"project_media", "ffmpeg"}, Inputs: []string{"script", "visible_media"}, Outputs: []string{"plan"}, Content: SyntaxAdapter, Resources: map[string]string{}},
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
		packages[i].Content += "\nRead vac/tool-contracts.md using skills.read_resource with this package's content_sha256 before calling tools. It defines every argument/result and the mandatory renders.inspect -> artifacts.get images -> renders.review -> run.finish sequence.\n"
	}
	return packages
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
const SyntaxAdapter = `# VAC script syntax
Blank lines separate paragraphs. Ordinary text is the user's instruction, not
automatically narration. Explicit {@visible-media-name} chooses one authorized
source; {"text",字号:32px,字体:"Microsoft YaHei",} forces a subtitle.
{黑屏}, {白屏}, {交叉溶解} are supported entry effects; unknown or repeated
blocks must fail. No expressions or arbitrary disk paths. Segment narration is
independent text. Human finalization freezes the exact plan; subsequent changes
require a new finalization. Use scripts.propose and the shared renderer.
`
