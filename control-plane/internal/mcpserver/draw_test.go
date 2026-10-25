package mcpserver

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
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
	imagePath := filepath.Join(t.TempDir(), "imported.png")
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
	e = png.Encode(imageFile, picture)
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
