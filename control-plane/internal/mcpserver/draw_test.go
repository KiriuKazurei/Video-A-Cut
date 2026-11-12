package mcpserver

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/scriptsyntax"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/shotcraft"
)

// TestDrawRT goes through the real HTTP MCP transport, browser renderer and
// shared sequence exporter. It needs only local tools, not a model/provider.
func TestDrawRT(t *testing.T) {
	for _, name := range []string{"node", "ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(name); err != nil {
			t.Skip(name + " not installed")
		}
	}
	f := newLoopFixture(t, 3*time.Minute, Agent{ID: "draw", Role: "editor"})
	ctx := t.Context()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	delivery := filepath.Join(t.TempDir(), "delivery")
	if err = os.MkdirAll(delivery, 0700); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.ConfigureDeliveryRoot(delivery); err != nil {
		t.Fatal(err)
	}
	if err = f.svc.ConfigureTTS("python", filepath.Join(root, "workers", "python")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.svc.CloseTTS)
	if err = f.svc.ConfigureProjectWorkspace(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(f.svc.CloseProjectWorkspace)
	if err = f.svc.ConfigureSkillHarness(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(t.TempDir(), "draw.avp"), Name: "MCP drawing"})
	if err != nil {
		t.Fatal(err)
	}
	pid, seq := view.Document.Project.ID, view.Document.Sequences[0].ID
	imagePath := filepath.Join(t.TempDir(), "imported%03d.jpg")
	picture := image.NewRGBA(image.Rect(0, 0, 100, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 100; x++ {
			picture.Set(x, y, color.RGBA{0, 255, 0, 255})
		}
	}
	imageFile, e := os.Create(imagePath)
	if e != nil {
		t.Fatal(e)
	}
	e = jpeg.Encode(imageFile, picture, &jpeg.Options{Quality: 95})
	closeErr := imageFile.Close()
	if e != nil || closeErr != nil {
		t.Fatalf("image fixture: %v %v", e, closeErr)
	}
	view, err = f.svc.ImportProjectResource(ctx, pid, imagePath)
	if err != nil {
		t.Fatal(err)
	}
	importedImage := view.Document.Resources[0]

	must := func(tool string, args any) map[string]any {
		t.Helper()
		out, bad, raw := f.call("draw", tool, args)
		if bad {
			t.Fatalf("%s failed: %.2400s", tool, raw)
		}
		return out
	}
	pack := must("skills.get", map[string]any{"id": "vac-draw-remotion"})
	must("skills.read_resource", map[string]any{"id": "vac-draw-remotion", "content_sha256": pack["content_sha256"], "resource": "vac/tool-contracts.md"})
	visible := must("projects.visible_set.get", map[string]any{"id": pid})
	args := map[string]any{"project_id": pid, "sequence_id": seq, "flow": "custom", "skill_id": "vac-draw-remotion", "strategy": "draw", "execution_mode": "external_mcp", "visible_set_sha": visible["sha256"], "goal": "Draw an orange moving box, inspect a frame and edit the resulting video.", "idempotency_key": "draw-create"}
	run := must("skill_runs.create", args)
	runID := run["id"]
	step := 0
	tool := func(name string, args any) map[string]any {
		t.Helper()
		step++
		t.Log("MCP tool", name)
		out := must("skill_runs.tool", map[string]any{"run_id": runID, "request": map[string]any{"name": name, "arguments": args, "expected_version": run["version"], "idempotency_key": name + time.Duration(step).String()}})
		run = out["run"].(map[string]any)
		result, ok := out["result"].(map[string]any)
		if !ok {
			t.Fatalf("unexpected %s result: %v", name, out["result"])
		}
		return result
	}
	source := "import React from 'react';import {AbsoluteFill,useCurrentFrame} from 'remotion';export default function Scene(){const frame=useCurrentFrame();return <AbsoluteFill style={{background:'#101820'}}><div style={{position:'absolute',left:frame*6,top:60,width:40,height:40,background:'#ff8800'}}/></AbsoluteFill>}"
	fresh := must("sequences.get", map[string]any{"project_id": pid, "sequence_id": seq})
	tool("draw.save", map[string]any{"session": view.ID, "expected_revision": fresh["project_revision"], "idempotency_key": "source", "composition": project.DrawComposition{ID: "box", Name: "Moving box", Engine: "remotion", Source: source, Width: 320, Height: 180, FPS: project.Rate{Num: 30, Den: 1}, DurationFrames: 30, Images: []project.DrawImageLayer{{ID: "hero", ResourceID: importedImage.ID, X: 240, Y: 120, Width: 40, Height: 40, DX: -100, DY: 0, FromFrame: 0, ToFrame: 29, Fit: "fill"}}}})
	comps, err := f.svc.DrawCompositions(pid)
	if err != nil || len(comps) != 1 {
		t.Fatalf("saved: %v %v", comps, err)
	}
	c := comps[0]
	if len(c.Images) != 1 || c.Images[0].ResourceSHA != importedImage.Source.SHA256 {
		t.Fatal("image reference not fingerprinted")
	}
	action := map[string]any{"session": view.ID, "composition_id": c.ID, "content_version": c.ContentVersion}
	tool("draw.check", action)
	rendered := tool("draw.render", action)
	if rendered["render"].(map[string]any)["frames"] != float64(30) {
		t.Fatalf("render: %v", rendered)
	}
	frameArgs := map[string]any{"session": view.ID, "composition_id": c.ID, "content_version": c.ContentVersion, "frame": 24}
	frame := tool("draw.get_frame", frameArgs)
	// Fetch through MCP and assert the transport carries a native image.
	step++
	image, bad, raw := f.call("draw", "skill_runs.tool", map[string]any{"run_id": runID, "request": map[string]any{"name": "artifacts.get", "arguments": map[string]any{"id": frame["artifact"].(map[string]any)["id"]}, "expected_version": run["version"], "idempotency_key": "image"}})
	if bad || !strings.Contains(raw, `"type":"image"`) || !strings.Contains(raw, `"mimeType":"image/png"`) {
		t.Fatalf("native image missing: %.300s", raw)
	}
	run = image["run"].(map[string]any)
	timeline, err := f.svc.Timeline(ctx, pid, seq)
	if err != nil {
		t.Fatal(err)
	}
	imported := tool("draw.import", map[string]any{"session": view.ID, "composition_id": c.ID, "content_version": c.ContentVersion, "sequence_id": seq, "expected_revision": timeline.Sequence.Revision, "idempotency_key": "import"})
	decodeView := func(v map[string]any) service.TimelineView {
		t.Helper()
		b, _ := json.Marshal(v)
		var out service.TimelineView
		if e := json.Unmarshal(b, &out); e != nil {
			t.Fatal(e)
		}
		return out
	}
	tv := decodeView(imported)
	var track project.Track
	for _, candidate := range tv.Sequence.Tracks {
		if len(candidate.Clips) > 0 {
			track = candidate
		}
	}
	if len(track.Clips) != 1 || track.Clips[0].At != 0 || track.Clips[0].Duration != 30 || track.Clips[0].Origin.RunID != runID {
		t.Fatalf("import provenance: %+v", track)
	}
	edit := func(operations []project.Operation) service.TimelineView {
		return decodeView(tool("timeline.edit", map[string]any{"sequence_id": seq, "command": project.Command{Session: view.ID, ExpectedRevision: tv.Sequence.Revision, IdempotencyKey: "edit-" + time.Duration(step).String(), Label: "Agent edit", Operations: operations}}))
	}
	tv = edit([]project.Operation{{Type: "clip.split", ClipIDs: []string{track.Clips[0].ID}, At: 15}})
	for _, candidate := range tv.Sequence.Tracks {
		if candidate.ID == track.ID {
			track = candidate
		}
	}
	if len(track.Clips) != 2 {
		t.Fatalf("split: %+v", track)
	}
	right := track.Clips[1]
	if right.At == 0 {
		right = track.Clips[0]
	}
	tv = edit([]project.Operation{{Type: "clip.move", ClipIDs: []string{right.ID}, At: 20}})
	finished := tool("run.finish", map[string]any{"sequence_revision": tv.Sequence.Revision, "content_version": tv.ContentVersion})
	if run["state"] != "succeeded" || finished["completed"] != true || finished["human_acceptance"] != "not_checked" {
		t.Fatalf("finish: %v %v", run, finished)
	}
	movie, _, err := f.svc.DrawRenderPath(pid, c.ID, c.ContentVersion)
	if err != nil {
		t.Fatal(err)
	}
	if data, e := os.ReadFile(movie); e != nil || len(data) == 0 {
		t.Fatalf("media: %v", e)
	}

	// Validate the shared export as media, including the five-frame gap created
	// by the actual Agent move. Retain the tiny result for independent inspection.
	exportPath, _, err := f.svc.SkillArtifactPath(ctx, "agent:draw", runID.(string), finished["artifact"].(map[string]any)["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	probe := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=nb_frames,width,height", "-of", "json", exportPath)
	rawProbe, e := probe.Output()
	if e != nil {
		t.Fatal(e)
	}
	var info struct {
		Streams []struct {
			Frames string `json:"nb_frames"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
		} `json:"streams"`
	}
	if e = json.Unmarshal(rawProbe, &info); e != nil || len(info.Streams) != 1 || info.Streams[0].Frames != "35" || info.Streams[0].Width != 320 || info.Streams[0].Height != 180 {
		t.Fatalf("export not 35 frames 320x180: %s %v", rawProbe, e)
	}
	average := func(at float64) float64 {
		t.Helper()
		cmd := exec.Command("ffmpeg", "-v", "error", "-ss", strconv.FormatFloat(at, 'f', 4, 64), "-i", exportPath, "-frames:v", "1", "-vf", "signalstats,metadata=print:file=-", "-f", "null", "-")
		b, e := cmd.Output()
		if e != nil {
			t.Fatal(e)
		}
		text := string(b)
		_, value, ok := strings.Cut(text, "lavfi.signalstats.YAVG=")
		if !ok {
			t.Fatalf("no pixel statistics: %s", b)
		}
		value = strings.Split(value, "\n")[0]
		result, e := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if e != nil {
			t.Fatal(e)
		}
		return result
	}
	content, gap := average(.1), average(17.0/30)
	if content < gap+5 {
		t.Fatalf("move gap not rendered: content=%v gap=%v", content, gap)
	}
	if out := os.Getenv("VAC_TEST_OUTPUT"); out != "" {
		b, e := os.ReadFile(exportPath)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(out, "draw-edited.mp4"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	// Persist/reopen without changing the external source, and reject old sessions.
	view, err = f.svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{})
	if err != nil {
		t.Fatal(err)
	}
	path := view.Path
	if _, err = f.svc.ProjectFileCommand(ctx, "close", service.ProjectCommand{}); err != nil {
		t.Fatal(err)
	}
	reopened, err := f.svc.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Document.DrawCompositions[0].Source != source || len(reopened.Document.Resources) != 2 {
		t.Fatal("AVP round-trip lost source/media")
	}
	if _, err = f.svc.SaveDrawComposition(ctx, pid, service.DrawSaveRequest{Session: view.ID, ExpectedRevision: reopened.Revision, IdempotencyKey: "old-session", Composition: c}); err != model.ErrConflict {
		t.Fatalf("old session accepted: %v", err)
	}
	if _, err = f.svc.DrawCompositions("another-project"); err != model.ErrNotFound {
		t.Fatalf("cross-project read: %v", err)
	}
	// Keep a compact reproducible evidence record in the managed output.
	if output := os.Getenv("VAC_TEST_OUTPUT"); output != "" {
		evidence := map[string]any{"source_engine": "remotion", "input_frames": 30, "split_frame": 15, "moved_right_to": 20, "run": run, "finish": finished, "native_mcp_image": true, "avp_round_trip": true, "project_image_resource": importedImage.ID, "image_resource_sha256": importedImage.Source.SHA256}
		b, _ := json.MarshalIndent(evidence, "", "  ")
		if e := os.WriteFile(filepath.Join(output, "draw-mcp-verification.json"), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
}

func TestMixedEditMCPPreviewAndSourceFinishWithoutStandaloneRender(t *testing.T) {
	f := newLoopFixture(t, 3*time.Minute, Agent{ID: "unified", Role: "editor"})
	ctx := t.Context()
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	delivery := filepath.Join(t.TempDir(), "delivery")
	if e = os.MkdirAll(delivery, 0700); e != nil {
		t.Fatal(e)
	}
	if e = f.svc.ConfigureDeliveryRoot(delivery); e != nil {
		t.Fatal(e)
	}
	if e = f.svc.ConfigureTTS("python", filepath.Join(root, "workers", "python")); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(f.svc.CloseTTS)
	if e = f.svc.ConfigureProjectWorkspace(ctx); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(f.svc.CloseProjectWorkspace)
	if e = f.svc.ConfigureSkillHarness(ctx); e != nil {
		t.Fatal(e)
	}
	user := shotcraft.UserTemplate{Version: 1, ID: "user-mcp-title", Summary: "Global MCP title", Category: "opening", DurationFrames: 60,
		Source: `export default function Title(){return <div>Global title</div>;}`}
	if _, e = f.svc.ImportShotTemplate(ctx, user); e != nil {
		t.Fatal(e)
	}
	view, e := f.svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(t.TempDir(), "unified.avp"), Name: "Unified MCP"})
	if e != nil {
		t.Fatal(e)
	}
	pid, sequence := view.Document.Project.ID, view.Document.Sequences[0].ID
	imagePath := filepath.Join(t.TempDir(), "shot-image.jpg")
	picture := image.NewRGBA(image.Rect(0, 0, 100, 60))
	for y := 0; y < 60; y++ {
		for x := 0; x < 100; x++ {
			picture.SetRGBA(x, y, color.RGBA{G: 255, A: 255})
		}
	}
	var jpegBytes bytes.Buffer
	if e = jpeg.Encode(&jpegBytes, picture, nil); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(imagePath, jpegBytes.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	view, e = f.svc.ImportProjectResource(ctx, pid, imagePath)
	if e != nil {
		t.Fatal(e)
	}
	importedImage := view.Document.Resources[0]
	view, e = f.svc.ImportProjectResource(ctx, pid, filepath.Join(root, "workers", "node", "timeline-cli", "fixtures", "media", "clip_01.mp4"))
	if e != nil {
		t.Fatal(e)
	}
	footage := view.Document.Resources[len(view.Document.Resources)-1]
	must := func(name string, args any) map[string]any {
		t.Helper()
		out, bad, raw := f.call("unified", name, args)
		if bad {
			t.Fatalf("%s: %.2400s", name, raw)
		}
		return out
	}
	pack := must("skills.get", map[string]any{"id": "vac-visual-summary"})
	must("skills.read_resource", map[string]any{"id": "vac-visual-summary", "content_sha256": pack["content_sha256"], "resource": "vac/tool-contracts.md"})
	visible := must("projects.visible_set.get", map[string]any{"id": pid})
	run := must("skill_runs.create", map[string]any{"project_id": pid, "sequence_id": sequence, "flow": "visual_heavy", "skill_id": "vac-visual-summary", "strategy": "edit", "execution_mode": "external_mcp", "visible_set_sha": visible["sha256"], "goal": "Combine footage and editable graphics in one sequence", "idempotency_key": "create"})
	runID := run["id"].(string)
	step := 0
	tool := func(name string, args any) map[string]any {
		t.Helper()
		step++
		out := must("skill_runs.tool", map[string]any{"run_id": runID, "request": map[string]any{"name": name, "arguments": args, "expected_version": run["version"], "idempotency_key": name + strconv.Itoa(step)}})
		run = out["run"].(map[string]any)
		return out["result"].(map[string]any)
	}
	capability := tool("draw.capabilities", map[string]any{})
	if capability["live_preview"] != true {
		t.Fatal("missing unified capability")
	}
	script := scriptsyntax.Parse("{#row-embed}展示嵌入页面")
	if len(script.Diagnostics) != 0 || len(script.Paragraphs) != 1 || script.Paragraphs[0].ShotCard != "row-embed" {
		t.Fatalf("shot card script did not resolve: %+v", script)
	}
	shotID := script.Paragraphs[0].ShotCard
	shot := tool("draw.shot", map[string]any{"action": "read", "id": shotID})
	if source, _ := shot["source"].(string); !strings.Contains(source, "export default") || shot["duration_frames"].(float64) <= 0 {
		t.Fatal("shot card did not provide executable source")
	}
	doc := project.DrawDocument{SchemaVersion: 1, Canvas: project.DrawCanvas{Width: 320, Height: 180, FPS: project.Rate{Num: 30, Den: 1}}, DurationFrames: 6, Preset: "auto", Sources: []project.DrawSource{{ID: "html", Engine: "hyperframes", Source: `<!doctype html><html><head><style>body{margin:0}#root{width:320px;height:180px}#box{width:80px;height:80px;background:red}</style></head><body><div id="root" data-composition-id="root" data-width="320" data-height="180" data-duration="0.2"><div id="box"></div></div><script>const tl=gsap.timeline({paused:true});tl.to("#box",{x:60,duration:.2,ease:"none"});window.__timelines.root=tl;</script></body></html>`}, {ID: "react", Engine: "remotion", Source: `import React from "react";export default function Card(){return <div style={{position:"absolute",left:160,top:40,width:80,height:60,background:"rgba(0,0,255,.5)"}}/>;}`}}, Nodes: []project.DrawNode{{ID: "web", Kind: "source", SourceID: "html", Duration: 6}, {ID: "card", Kind: "source", SourceID: "react", Duration: 6}}}
	current := must("sequences.get", map[string]any{"project_id": pid, "sequence_id": sequence})
	// A real detailed PNG must remain inspectable beyond the old 256 KiB
	// transcript-sized limit, without reducing the source or losing alpha.
	doc.Canvas.Width, doc.Canvas.Height = 640, 360
	doc.Sources[0].Canvas = &project.DrawCanvas{Width: 320, Height: 180, FPS: project.Rate{Num: 30, Den: 1}}
	doc.Sources[1].Source = `import React,{useLayoutEffect,useRef} from "react";export default function Pixels(){const ref=useRef();useLayoutEffect(()=>{const c=ref.current.getContext('2d'),im=c.createImageData(640,360);let seed=7;for(let i=0;i<im.data.length;i+=4){for(let j=0;j<3;j++){seed=(Math.imul(seed,1664525)+1013904223)>>>0;im.data[i+j]=seed>>>24;}im.data[i+3]=255;}c.putImageData(im,0,0);},[]);return <canvas ref={ref} width={640} height={360}/>;}`
	tool("draw.save", DrawSaveForTest(view.ID, uint64(current["project_revision"].(float64)), doc))
	list, e := f.svc.DrawCompositions(pid)
	if e != nil || len(list) != 1 {
		t.Fatalf("save %v %v", list, e)
	}
	c := list[0]
	frame := tool("draw.preview", map[string]any{"session": view.ID, "composition_id": c.ID, "content_version": c.ContentVersion, "frame": 3, "request_id": "same-frame"})
	if frame["artifact"].(map[string]any)["size_bytes"].(float64) <= 256<<10 {
		t.Fatal("fixture failed to cover a detailed frame")
	}
	step++
	image, bad, rawImage := f.call("unified", "skill_runs.tool", map[string]any{"run_id": runID, "request": map[string]any{"name": "artifacts.get", "arguments": map[string]any{"id": frame["artifact"].(map[string]any)["id"]}, "expected_version": run["version"], "idempotency_key": "native-image"}})
	if bad || !strings.Contains(rawImage, `"type":"image"`) {
		t.Fatalf("native MCP image missing: %.300s", rawImage)
	}
	run = image["run"].(map[string]any)
	if _, _, e = f.svc.DrawRenderPath(pid, c.ID, c.ContentVersion); e == nil {
		t.Fatal("preview unexpectedly required/published standalone movie")
	}
	current = must("sequences.get", map[string]any{"project_id": pid, "sequence_id": sequence})
	tool("draw.save", map[string]any{"session": view.ID, "expected_revision": current["project_revision"], "idempotency_key": "save-shot", "shot_card": shotID, "composition": project.DrawComposition{ID: "shot-row", Images: []project.DrawImageLayer{{ID: "detail-full", ResourceID: importedImage.ID, Width: 100, Height: 60, Mode: "source"}}}})
	list, e = f.svc.DrawCompositions(pid)
	if e != nil || len(list) != 2 || list[1].ID != "shot-row" {
		t.Fatalf("save by shot ID: %v %v", list, e)
	}
	preset := list[1]
	if !strings.Contains(preset.Source, "Vincentwei1021") || preset.DurationFrames != int64(shot["duration_frames"].(float64)) {
		t.Fatal("compiled source or native duration was not saved")
	}
	userScript := scriptsyntax.Parse("{#user-mcp-title/default}显示标题")
	if len(userScript.Diagnostics) != 0 || len(userScript.Paragraphs) != 1 || userScript.Paragraphs[0].ShotCard != "user-mcp-title/default" {
		t.Fatalf("user shot script did not resolve: %+v", userScript)
	}
	userShot := tool("draw.shot", map[string]any{"action": "read", "id": userScript.Paragraphs[0].ShotCard})
	if userShot["source"] != user.Source || userShot["license"] != nil {
		t.Fatalf("unexpected imported shot metadata: %v", userShot)
	}
	current = must("sequences.get", map[string]any{"project_id": pid, "sequence_id": sequence})
	tool("draw.save", map[string]any{"session": view.ID, "expected_revision": current["project_revision"], "idempotency_key": "save-user-shot", "shot_card": user.ID, "composition": project.DrawComposition{ID: "custom-title"}})
	if e = f.svc.DeleteShotTemplate(ctx, user.ID); e != nil {
		t.Fatal(e)
	}
	tv := tool("timeline.read", map[string]any{"sequence_id": sequence})
	tool("draw.import", map[string]any{"session": view.ID, "composition_id": c.ID, "content_version": c.ContentVersion, "sequence_id": sequence, "expected_revision": tv["sequence"].(map[string]any)["revision"], "idempotency_key": "attach", "source_mode": "snapshot"})
	tv = tool("timeline.read", map[string]any{"sequence_id": sequence})
	tool("draw.import", map[string]any{"session": view.ID, "composition_id": preset.ID, "content_version": preset.ContentVersion, "sequence_id": sequence, "expected_revision": tv["sequence"].(map[string]any)["revision"], "idempotency_key": "attach-shot", "source_mode": "snapshot", "at": 6})
	timeline, e := f.svc.Timeline(ctx, pid, sequence)
	if e != nil {
		t.Fatal(e)
	}
	var target project.Clip
	for _, track := range timeline.Sequence.Tracks {
		for _, clip := range track.Clips {
			if clip.Origin.ArtifactID == preset.ID && track.Kind == "code" {
				target = clip
			}
		}
	}
	if target.Code == nil {
		t.Fatal("Shotcraft source did not land on a code track")
	}
	preview := func(view service.TimelineView) string {
		t.Helper()
		frame, err := f.svc.CreateSequenceFrame(ctx, pid, sequence, service.SequenceFrameRequest{Session: view.Session, ExpectedRevision: view.Sequence.Revision, ContentVersion: view.ContentVersion, Frame: target.At + 12})
		if err != nil || frame.Image == "" {
			t.Fatalf("preview Shotcraft on the sequence: %v", err)
		}
		return frame.Image
	}
	before := preview(timeline)
	authored := "{#row-embed}展示嵌入页面\n{移动, x:90px, y:0px}\n{缩放, 比例:92%}"
	tool("timeline.edit", map[string]any{"sequence_id": sequence, "command": project.Command{Session: timeline.Session, ExpectedRevision: timeline.Sequence.Revision, IdempotencyKey: "edit-shot-script", Operations: []project.Operation{{Type: "code.script", ClipIDs: []string{target.ID}, CodeRevision: target.Code.Revision, Description: &authored, Unlinked: true}}}})
	edited, e := f.svc.Timeline(ctx, pid, sequence)
	if e != nil {
		t.Fatal(e)
	}
	if after := preview(edited); before == after {
		t.Fatal("code script did not change the actual preview pixels")
	}
	videoTrack := ""
	for _, track := range edited.Sequence.Tracks {
		if track.Kind == "video" {
			videoTrack = track.ID
			break
		}
	}
	if videoTrack == "" {
		t.Fatal("missing video track for the source footage")
	}
	out := project.Time{Value: 6, Scale: 30}
	tool("timeline.edit", map[string]any{"sequence_id": sequence, "command": project.Command{Session: edited.Session, ExpectedRevision: edited.Sequence.Revision, IdempotencyKey: "insert-footage", Operations: []project.Operation{{Type: "clip.insert", TrackID: videoTrack, ResourceID: footage.ID, SourceOut: &out, At: target.At + target.Duration}}}})
	edited, e = f.svc.Timeline(ctx, pid, sequence)
	if e != nil {
		t.Fatal(e)
	}
	finished := tool("run.finish", map[string]any{"sequence_revision": edited.Sequence.Revision, "content_version": edited.ContentVersion})
	if run["state"] != "succeeded" || finished["human_acceptance"] != "not_checked" {
		t.Fatalf("finish: %v %v", run, finished)
	}
	saved, e := f.svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{ExpectedSession: view.ID})
	if e != nil {
		t.Fatal(e)
	}
	reopened, e := f.svc.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: saved.Path, StopTasks: true})
	if e != nil {
		t.Fatal(e)
	}
	foundUserShot := false
	for _, composition := range reopened.Document.DrawCompositions {
		if composition.ID == "custom-title" && composition.Source == user.Source && composition.DurationFrames == 60 {
			foundUserShot = true
		}
	}
	if !foundUserShot {
		t.Fatal("reopened AVP did not retain the imported source after library removal")
	}
	foundShot := false
	for _, resource := range reopened.Document.Resources {
		if resource.Draw != nil && resource.Draw.Snapshot.Document != nil && resource.Draw.CompositionID == preset.ID && resource.Draw.Mode == "snapshot" {
			for _, source := range resource.Draw.Snapshot.Document.Sources {
				for _, binding := range resource.Draw.Snapshot.ImageBindings() {
					foundShot = foundShot || strings.Contains(source.Source, "Vincentwei1021") && binding.ID == "detail-full" && binding.ResourceID == importedImage.ID && binding.ResourceSHA != ""
				}
			}
		}
	}
	if !foundShot {
		t.Fatal("reopened AVP did not retain the attributed shot source snapshot and image binding")
	}
	foundScript, foundFootage := false, false
	for _, track := range reopened.Document.Sequences[0].Tracks {
		for _, clip := range track.Clips {
			foundFootage = foundFootage || track.Kind == "video" && clip.ResourceID == footage.ID && clip.At == target.At+target.Duration
			if clip.ID == target.ID && clip.ResourceID == target.ResourceID && clip.Code != nil && clip.Code.Script != nil && *clip.Code.Script == authored && len(clip.Effects.ParameterKeyframes) == 2 {
				foundScript = true
			}
		}
	}
	if !foundScript {
		t.Fatal("reopened AVP did not retain the authored shot and parameter script")
	}
	if !foundFootage {
		t.Fatal("reopened AVP did not retain the source footage next to the editable shot")
	}
	if output := os.Getenv("VAC_TEST_OUTPUT"); output != "" {
		raw, _ := json.MarshalIndent(map[string]any{"capabilities": capability, "preview": frame, "finish": finished, "run": run, "no_standalone_mp4": true}, "", "  ")
		if e = os.WriteFile(filepath.Join(output, "unified-draw-mcp.json"), raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
}

func DrawSaveForTest(session string, revision uint64, doc project.DrawDocument) service.DrawSaveRequest {
	return service.DrawSaveRequest{Session: session, ExpectedRevision: revision, IdempotencyKey: "save-unified", Composition: project.DrawComposition{ID: "unified", Name: "Unified", Document: &doc}}
}
