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
	tools := []string{"media.list", "media.get_frames", "media.transcribe", "summaries.get", "summaries.submit", "scripts.propose", "renders.create", "renders.inspect", "renders.review", "artifacts.list", "artifacts.get", "run.finish", "timeline.read", "timeline.edit", "references.resolve", "draw.list", "draw.save", "draw.check", "draw.render", "draw.get_frame", "draw.import", "draw.capabilities", "draw.preview"}
	modes := []string{"external_mcp", "builtin_api"}
	packages := []Package{
		{ID: "video-use", Name: "Video Use", Description: "Executable source-free drawing and lightweight video editing.", Version: "0.1.0-vac.7", Source: "builtin", Builtin: true, Enabled: true, Runtime: "harness", Flows: []string{"lightweight"}, Modes: modes, Tools: tools, Permissions: []string{"project_media", "provider", "ffmpeg"}, Inputs: []string{"visible_media", "goal"}, Outputs: []string{"plan", "mp4", "premiere_package"}, Content: VideoUseAdapter, Resources: map[string]string{}},
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
	packages = append(packages, drawSkills()...)
	for i := range packages {
		packages[i].Resources["vac/tool-contracts.md"] = ToolContracts
		packages[i].Content += "\nRead vac/tool-contracts.md using skills.read_resource with this package's content_sha256 before calling tools. It defines every argument/result and the flattened-plan inspection flow and the version-checked structured-sequence finish flow.\n"
	}
	return packages
}

func drawSkills() []Package {
	tools := []string{"draw.list", "draw.save", "draw.check", "draw.render", "draw.get_frame", "draw.import", "draw.capabilities", "draw.preview", "timeline.read", "timeline.edit", "references.resolve", "artifacts.list", "artifacts.get", "run.finish"}
	makePackage := func(id, name, content string) Package {
		return Package{ID: id, Name: name, Description: "Executable local code-to-video and editing handoff", Version: "0.3.0", Source: "builtin", Builtin: true, Enabled: true, Runtime: "harness", Flows: []string{"lightweight", "visual_heavy", "custom"}, Modes: []string{"external_mcp", "builtin_api"}, Tools: tools, Permissions: []string{"project_media", "ffmpeg", "browser_render"}, Inputs: []string{"goal", "composition_spec", "project_images"}, Outputs: []string{"composition_source", "mp4", "timeline"}, Content: content, Resources: map[string]string{}}
	}
	return []Package{makePackage("vac-draw-hyperframes", "HyperFrames 绘制", hyperframesDraw), makePackage("vac-draw-remotion", "Remotion 绘制", remotionDraw), makePackage("vac-draw-pipeline", "绘制管线", pipelineDraw)}
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
vac-draw-pipeline as the default unified entry; use engine Skills only for a specific source.
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

Executable HyperFrames drawing Skill for VAC. Use strategy=draw; the current project may have no source media.
Read vac/tool-contracts.md before calling tools. Use only the declared draw/timeline/artifact tools; no shell, filesystem scans, dependency installation or remote assets.
Author one complete HTML document with a data-composition-id root and matching data-width, data-height and data-duration (seconds). Use inline CSS/SVG and paused GSAP timelines registered in window.__timelines[rootId]. GSAP is supplied locally; do not add a script src.
draw.save commits source and dimensions to the AVP with project session, project revision and an idempotency key.
Then draw.check, draw.render, draw.get_frame and artifacts.get inspect actual rendered frames. Runtime dependencies and an installed Chromium must exist; missing dependencies fail explicitly without downloads or cloud fallback.
draw.import adds the verified video to the run's launch sequence as a separate Agent track group. Use timeline.read/edit to trim, split, apply effects or combine with source media. Finish using run.finish {sequence_revision,content_version} after the final timeline edit.
For source-level editing use draw.import source_mode=snapshot (or explicit follow); no standalone MP4 prerequisite. The shared FrameSource participates in composite_preview and merge_render. Legacy rendered MP4 import remains available.
Human acceptance is not_checked, not automatic approval. Preserve source for subsequent code edits; MP4 is not reversible source.


Imported project images are first-class drawing assets. Read image_resources from
skill_runs.get (builtin model input includes the same catalog), or resources.list,
then add composition.images entries using stable image resource_id values.
Each instance has its own id,x,y,width,height,dx,dy,from_frame,to_frame,fit,
opacity and mode. Go supplies resource_sha256; never submit a filesystem path.
mode="layer" automatically places the image above the scene, with linear dx/dy
movement over the frame interval. mode="source" supplies the same image and
parameters for custom source placement, without an automatic duplicate overlay.
HyperFrames source uses <img src="vac-image:INSTANCE_ID"> and window.vacImages;
Remotion's default component receives {images}, with images[INSTANCE_ID].src.
Missing/removed/changed images fail explicitly; relink the resource and save the
composition to refresh its dependency fingerprint. Preserve all instance IDs.
`
const remotionDraw = `# vac-draw-remotion

Executable Remotion drawing Skill for VAC. Use strategy=draw; an empty current project is valid.
Read vac/tool-contracts.md. Author one TSX module exporting a default React component. Imports are limited to react and remotion; use AbsoluteFill, Sequence, useCurrentFrame, useVideoConfig, interpolate and spring for frame-addressed motion.
Use inline graphics and bound current-project image resources. Add external audio/video through the editing timeline. No remote imports, Node APIs, arbitrary paths, shell or installation commands.
draw.save stores source/specification with the current project session/revision and an idempotency key; draw.check checks syntax/imports/specification; draw.render uses the installed official renderer and Chromium without downloading a browser or using a cloud fallback.
Use draw.get_frame plus artifacts.get to inspect real pixels, then draw.import into the run's launch sequence. timeline.read/edit can change the resulting clips; run.finish takes the current sequence_revision and content_version.
Source-level composite_preview and merge_render use the shared FrameSource after draw.import source_mode=snapshot (or explicit follow), without an intermediate MP4. Omitted source_mode retains verified rendered import.
Retain editable source in the AVP. Do not invent human acceptance; machine completion reports not_checked.


Imported project images are first-class drawing assets. Read image_resources from
skill_runs.get (builtin model input includes the same catalog), or resources.list,
then add composition.images entries using stable image resource_id values.
Each instance has its own id,x,y,width,height,dx,dy,from_frame,to_frame,fit,
opacity and mode. Go supplies resource_sha256; never submit a filesystem path.
mode="layer" automatically places the image above the scene, with linear dx/dy
movement over the frame interval. mode="source" supplies the same image and
parameters for custom source placement, without an automatic duplicate overlay.
HyperFrames source uses <img src="vac-image:INSTANCE_ID"> and window.vacImages;
Remotion's default component receives {images}, with images[INSTANCE_ID].src.
Missing/removed/changed images fail explicitly; relink the resource and save the
composition to refresh its dependency fingerprint. Preserve all instance IDs.
`
const pipelineDraw = `# vac-draw-pipeline

Default unified drawing strategy (strategy=draw). Users describe a goal, not an engine. Read vac/tool-contracts.md and draw.capabilities first. Reuse the current DrawDocument and stable IDs. Preserve unrelated sources, layers, variables and manual edits; scope changes to the requested nodes. Source revision and project revision are separate CAS checks.

Choose per layer: HyperFrames for HTML/CSS layout, DOM/SVG, websites, UI motion and GSAP; Remotion for existing React components/templates or data expressions that are simpler as React; common image/text/group layers for basic graphics. Mix HTML and React in one document when it benefits the goal. Do not start both engines unnecessarily or switch a working implementation on every edit. Purpose presets auto/interface/data/media guide planning without forcing an engine. Record source.reason and a short document.explanation. Go validates and freezes the deterministic plan; there is no additional autonomous backend planner.

Use draw.save with composition.document schema_version=1: canvas {width,height,fps:{num,den},alpha:"straight",color_space:"srgb"}, duration_frames, preset, sources [{id,engine,source,reason,canvas?,duration_frames?}], nodes [{id,name,kind,source_id?,resource_id?,from,duration,transform:{x,y,width?,height?,rotation?},opacity?,z?,parameters?}]. Kinds source/image/text/group/mask/shape. Groups use parent_id, and must form a DAG. Node ranges are half-open [from,from+duration). source_offset is in source frames, speed is rational. Keep stable source and instance IDs. The outer transform belongs to the layer; internal source motion belongs to its source. Never duplicate the same motion in both scopes.

Imported images reference current-project image_resources IDs; Go supplies fingerprints and local decoding/RAW development. A common image node appears automatically; hidden image nodes may provide bindings for a source without adding another visible copy. Legacy image_bindings remain supported. Never supply filesystem paths. Missing/changed/removed resources are explicit errors.

For external runs, report actual model progress with skill_runs.progress every 30–60 seconds while awaiting output; use the returned run version and never send model text or credentials. Use draw.preview or draw.get_frame to inspect actual mixed pixels before any movie exists; each consumes the frame budget. artifacts.get returns a native MCP image. Inspect first/middle/end and relevant transition frames. Do not equate a render file with good visual quality. Preview and final render share the native HyperFrames runtime, React frame context, rational time map, RGBA composition and resources. Use paused deterministic GSAP timelines, useCurrentFrame/useVideoConfig and seeded deterministic expressions; no wall-clock timers, uncontrolled playback or network sources.

Use draw.import {source_mode:"snapshot",...} to add editable source directly to the launch editing sequence without rendering an MP4 first. snapshot freezes source; follow explicitly follows future saved changes. Omitted source_mode/rendered retains verified-MP4 import compatibility. A source import is one undoable independent track group at frame zero, leaving manual edits intact. Read timeline.read immediately before import for sequence revision. After timeline editing use run.finish {sequence_revision,content_version}; it exports the shared footage/audio/subtitle/drawing compositor and preserves frozen historical outputs. draw.render supports standalone mp4, webm-alpha and prores4444 (format optional, default mp4).

Fixed local engine dependencies and an installed Chrome/headless shell are required. Missing capability fails explicitly; never install packages, download browsers/models, invoke commands or silently use cloud fallbacks. Unsupported cross-document backdrop sampling is unavailable. Human acceptance remains not_checked until the user reviews the result.
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
