package service_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func TestTimelineVideoProxy(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	source := filepath.Join(root, "portrait.mp4")
	mediaTool(t, "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=s=1080x1920:r=30:d=0.5", "-f", "lavfi", "-i", "sine=d=0.5", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-preset", "ultrafast", "-threads", "1", "-c:a", "aac", source)
	session, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "proxy.avp"), Name: "Proxy"})
	if err != nil {
		t.Fatal(err)
	}
	pid := session.Document.Project.ID
	session, err = svc.ImportProjectResource(ctx, pid, source)
	if err != nil {
		t.Fatal(err)
	}
	seq, resource := session.Document.Sequences[0], session.Document.Resources[0]
	view, err := svc.TimelineCommand(ctx, "human:window", pid, seq.ID, project.Command{
		Session: session.ID, ExpectedRevision: seq.Revision, IdempotencyKey: "proxy-clip",
		Operations: []project.Operation{{Type: "clip.insert", TrackID: seq.Tracks[0].ID, ResourceID: resource.ID}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := service.SequencePreviewRequest{Session: session.ID, ExpectedRevision: view.Sequence.Revision, ContentVersion: view.ContentVersion}
	plan, err := svc.SequencePlaybackPlan(ctx, pid, seq.ID, request)
	if err != nil || plan.Mode != "direct" || len(plan.Video) != 1 || plan.Video[0].ProxyVersion == "" || plan.Video[0].ProxyReady {
		t.Fatalf("cold plan should be playable before the proxy exists: %+v %v", plan, err)
	}
	version := plan.Video[0].ProxyVersion
	if _, err = svc.TimelineProxyPath(pid, resource.ID, version); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("missing proxy should not be served: %v", err)
	}
	if ready, err := svc.CreateTimelineProxy(ctx, pid, resource.ID, version); err != nil || !ready {
		t.Fatalf("proxy generation: ready=%t %v", ready, err)
	}
	proxy, err := svc.TimelineProxyPath(pid, resource.ID, version)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-show_entries", "stream=codec_type,width,height,nb_frames", "-of", "csv=p=0", proxy).CombinedOutput()
	if err != nil || strings.TrimSpace(string(probe)) != "video,540,960,15" {
		t.Fatalf("proxy should preserve frames while downscaling video only: %q", probe)
	}
	original, err := svc.ProjectResourcePath(pid, resource.ID, false)
	if err != nil || original != source {
		t.Fatalf("original media path changed: %q %v", original, err)
	}
	plan, err = svc.SequencePlaybackPlan(ctx, pid, seq.ID, request)
	if err != nil || len(plan.Video) != 1 || !plan.Video[0].ProxyReady || plan.Video[0].ProxyVersion != version {
		t.Fatalf("warm plan should use the generated proxy: %+v %v", plan, err)
	}
	if _, err = svc.TimelineProxyPath(pid, resource.ID, "obsolete"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("stale proxy URL was accepted: %v", err)
	}
}

func TestSequenceCurrentFrame(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	wd, _ := os.Getwd()
	if err := svc.ConfigureTTS("python", filepath.Join(wd, "..", "..", "..", "workers", "python")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.CloseTTS)
	path := filepath.Join(root, "red.mp4")
	mediaTool(t, "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=red:s=320x180:r=30:d=2", "-c:v", "libx264", "-threads", "1", path)
	ctx := t.Context()
	session, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "frame.avp"), Name: "Frame"})
	if err != nil {
		t.Fatal(err)
	}
	pid := session.Document.Project.ID
	session, err = svc.ImportProjectResource(ctx, pid, path)
	if err != nil {
		t.Fatal(err)
	}
	resource := session.Document.Resources[0]
	results := make(chan error, 8)
	for range cap(results) {
		go func() {
			path, err := svc.ResourceThumbnail(ctx, pid, resource.ID, 0)
			if err == nil {
				_, err = os.Stat(path)
			}
			results <- err
		}()
	}
	for range cap(results) {
		if err := <-results; err != nil {
			t.Fatalf("concurrent identical thumbnails should share one completed render: %v", err)
		}
	}
	seq := session.Document.Sequences[0]
	view, err := svc.TimelineCommand(ctx, "human:window", pid, seq.ID, project.Command{Session: session.ID, ExpectedRevision: seq.Revision, IdempotencyKey: "insert", Operations: []project.Operation{{Type: "clip.insert", TrackID: seq.Tracks[0].ID, ResourceID: session.Document.Resources[0].ID}}})
	if err != nil {
		t.Fatal(err)
	}
	req := service.SequenceFrameRequest{Session: session.ID, ExpectedRevision: view.Sequence.Revision, ContentVersion: view.ContentVersion, Frame: 45}
	plan, err := svc.SequencePlaybackPlan(ctx, pid, seq.ID, service.SequencePreviewRequest{Session: session.ID, ExpectedRevision: view.Sequence.Revision, ContentVersion: view.ContentVersion})
	if err != nil || plan.Mode != "direct" || len(plan.Video) != 1 || len(plan.Audio) != 0 || plan.Video[0].ResourceID != session.Document.Resources[0].ID {
		t.Fatalf("simple source playback must not render a movie: %+v %v", plan, err)
	}
	if _, err = svc.UpdateProjectWorkspaceState(ctx, session.ID, map[string]any{
		"sequence_states": map[string]any{seq.ID: map[string]any{"head": 45, "zoom": 60}},
	}); err != nil {
		t.Fatal(err)
	}
	frame, err := svc.CreateSequenceFrame(ctx, pid, seq.ID, req)
	if err != nil {
		t.Fatal(err)
	}
	if frame.WorkerPID == 0 {
		t.Fatal("current-frame worker must persist between seeks")
	}
	firstPID := frame.WorkerPID
	req.Frame = 46
	warm, err := svc.CreateSequenceFrame(ctx, pid, seq.ID, req)
	if err != nil || warm.WorkerPID != firstPID {
		t.Fatal("seek restarted frame worker", err)
	}
	if err = svc.CloseDrawPreview(pid, session.ID, "sequence:"+seq.ID); err != nil {
		t.Fatal(err)
	}
	warm, err = svc.CreateSequenceFrame(ctx, pid, seq.ID, req)
	if err != nil || warm.WorkerPID != firstPID {
		t.Fatal("drawing preview release crossed the sequence worker namespace", err)
	}
	req.Frame = 45
	raw, err := base64.StdEncoding.DecodeString(frame.Image[len("data:image/png;base64,"):])
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(raw))
	if err != nil || im.Bounds().Dx() != view.Sequence.Width || frame.Frame != 45 || frame.ContentVersion != view.ContentVersion {
		t.Fatal("invalid frame", err)
	}
	r, g, b, _ := im.At(view.Sequence.Width/2, view.Sequence.Height/2).RGBA()
	if r < 50000 || g > 5000 || b > 5000 {
		t.Fatal("wrong frame color", r, g, b)
	}
	if err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.Name() == "program.mp4" || (info.IsDir() && strings.HasPrefix(info.Name(), "frame-")) {
			t.Errorf("still request left a movie or temporary frame directory: %s", path)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	edl := filepath.Join(root, "delivery", "projects", pid, "references", resource.ID, "edl.json")
	edlBefore, err := os.Stat(edl)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{ExpectedSession: session.ID}); err != nil {
		t.Fatal(err)
	}
	restoreReq, initialImage := req, frame.Image
	req.Frame = -1
	if _, err = svc.CreateSequenceFrame(ctx, pid, seq.ID, req); !errors.Is(err, model.ErrArgument) {
		t.Fatal("negative frame accepted", err)
	}
	req.Frame = 45
	req.ContentVersion = "stale"
	if _, err = svc.CreateSequenceFrame(ctx, pid, seq.ID, req); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale frame accepted", err)
	}
	if _, err = svc.CreateSequencePreview(ctx, pid, seq.ID, service.SequencePreviewRequest{Session: session.ID, ExpectedRevision: view.Sequence.Revision, ContentVersion: view.ContentVersion}); err != nil {
		t.Fatal(err)
	}
	cachedPath, err := svc.SequencePreviewPath(ctx, pid, seq.ID, view.ContentVersion)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(cachedPath)
	if err != nil {
		t.Fatal(err)
	}
	req.ContentVersion, req.Frame = view.ContentVersion, 44
	if frame, err = svc.CreateSequenceFrame(ctx, pid, seq.ID, req); err != nil || frame.Frame != 44 || frame.Diagnostics["backend"] != "cached-movie" {
		t.Fatal("cached movie frame", err)
	}
	after, err := os.Stat(cachedPath)
	if err != nil || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("still rerendered the cached movie", err)
	}
	plan, err = svc.SequencePlaybackPlan(ctx, pid, seq.ID, service.SequencePreviewRequest{Session: session.ID, ExpectedRevision: view.Sequence.Revision, ContentVersion: view.ContentVersion})
	if err != nil || plan.Mode != "cached" {
		t.Fatalf("verified movie was not preferred: %+v %v", plan, err)
	}

	current, err := svc.ProjectWorkspace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	audioPath := filepath.Join(root, "code-tone.wav")
	mediaTool(t, "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000:duration=10", audioPath)
	current, err = svc.ImportProjectResource(ctx, pid, audioPath)
	if err != nil {
		t.Fatal(err)
	}
	var audioID string
	for _, resource := range current.Document.Resources {
		if resource.Source.OriginalPath == audioPath {
			audioID = resource.ID
		}
	}
	if audioID == "" {
		t.Fatal("audio fixture not imported")
	}
	doc := project.DrawDocument{SchemaVersion: 1, Goal: "蓝色画面及本条引用的声音", Canvas: project.DrawCanvas{Width: 320, Height: 180, FPS: project.Rate{Num: 24, Den: 1}}, DurationFrames: 240, Sources: []project.DrawSource{{ID: "react", Engine: "remotion", Source: `import React from 'react'; export default ()=> <div style={{position:'absolute',inset:0,background:'blue'}}/>;`}}, Nodes: []project.DrawNode{{ID: "scene", Kind: "source", SourceID: "react", Duration: 240}}, AudioBindings: []project.DrawAudioBinding{{ResourceID: audioID, DurationFrames: 240}}}
	current, err = svc.SaveDrawComposition(ctx, pid, service.DrawSaveRequest{Session: current.ID, ExpectedRevision: current.Revision, IdempotencyKey: "draw", Composition: project.DrawComposition{ID: "still", Name: "Still", Document: &doc}})
	if err != nil {
		t.Fatal(err)
	}
	c := current.Document.DrawCompositions[0]
	view, err = svc.TimelineCommand(ctx, "human:window", pid, seq.ID, project.Command{Session: current.ID, ExpectedRevision: view.Sequence.Revision, IdempotencyKey: "code-track", Operations: []project.Operation{{Type: "track.add", Kind: "code"}}})
	if err != nil {
		t.Fatal(err)
	}
	codeTrackID := view.Sequence.Tracks[len(view.Sequence.Tracks)-1].ID
	view, err = svc.ImportDrawComposition(ctx, "human:window", pid, c.ID, service.DrawImportRequest{Session: current.ID, ContentVersion: c.ContentVersion, ExpectedRevision: view.Sequence.Revision, IdempotencyKey: "draw-import", SequenceID: seq.ID, SourceMode: "snapshot", TargetTrackID: codeTrackID, At: 60})
	if err != nil {
		t.Fatal(err)
	}
	frame, err = svc.CreateSequenceFrame(ctx, pid, seq.ID, service.SequenceFrameRequest{Session: current.ID, ExpectedRevision: view.Sequence.Revision, ContentVersion: view.ContentVersion, Frame: 75})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = base64.StdEncoding.DecodeString(strings.TrimPrefix(frame.Image, "data:image/png;base64,"))
	if err != nil {
		t.Fatal(err)
	}
	im, err = png.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ = im.At(160, 90).RGBA()
	if b < 50000 || r > 5000 || g > 5000 {
		t.Fatal("drawing frame missing", r, g, b)
	}
	if frame.WorkerPID != firstPID {
		t.Fatal("adding a drawing restarted the sequence worker")
	}
	seek := service.SequenceFrameRequest{Session: current.ID, ExpectedRevision: view.Sequence.Revision, ContentVersion: view.ContentVersion, Frame: 76}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err = svc.CreateSequenceFrame(cancelled, pid, seq.ID, seek); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled seek", err)
	}
	warm, err = svc.CreateSequenceFrame(ctx, pid, seq.ID, seek)
	if err != nil || warm.WorkerPID != firstPID {
		t.Fatal("cancelled seek destroyed warm worker", err)
	}
	segmentReq := service.SequenceSegmentRequest{SequencePreviewRequest: service.SequencePreviewRequest{Session: current.ID, ExpectedRevision: view.Sequence.Revision, ContentVersion: view.ContentVersion}, StartFrame: 76}
	part, err := svc.CreateSequenceSegment(ctx, pid, seq.ID, segmentReq)
	if err != nil || part.Frames != 60 || part.StartFrame != 60 || part.Cached {
		t.Fatalf("bounded drawing segment: %+v %v", part, err)
	}
	segmentPath, err := svc.SequenceSegmentPath(ctx, pid, seq.ID, view.ContentVersion, part.Key)
	if err != nil {
		t.Fatal(err)
	}
	levels, err := exec.Command("ffmpeg", "-hide_banner", "-i", segmentPath, "-af", "volumedetect", "-vn", "-f", "null", "-").CombinedOutput()
	if err != nil || !strings.Contains(string(levels), "mean_volume:") || strings.Contains(string(levels), "mean_volume: -inf") {
		t.Fatalf("code segment lost declared audio: %s %v", levels, err)
	}
	if cached, err := svc.CreateSequenceSegment(ctx, pid, seq.ID, segmentReq); err != nil || !cached.Cached || cached.Key != part.Key {
		t.Fatalf("segment cache not reused: %+v %v", cached, err)
	}
	code := view.Sequence.Tracks[len(view.Sequence.Tracks)-1].Clips[0]
	description := "只更新描述，保留同一源码与参数"
	view, err = svc.TimelineCommand(ctx, "human:window", pid, seq.ID, project.Command{Session: current.ID, ExpectedRevision: view.Sequence.Revision, IdempotencyKey: "description-only", Operations: []project.Operation{{Type: "code.script", ClipIDs: []string{code.ID}, Description: &description, CodeRevision: code.Code.Revision, Unlinked: true}}})
	if err != nil {
		t.Fatal(err)
	}
	segmentReq.ExpectedRevision, segmentReq.ContentVersion = view.Sequence.Revision, view.ContentVersion
	if cached, err := svc.CreateSequenceSegment(ctx, pid, seq.ID, segmentReq); err != nil || !cached.Cached || cached.Key != part.Key {
		t.Fatalf("description invalidated unchanged segment: %+v %v", cached, err)
	}
	oldVersion := view.ContentVersion
	view, err = svc.TimelineCommand(ctx, "human:window", pid, seq.ID, project.Command{Session: current.ID, ExpectedRevision: view.Sequence.Revision, IdempotencyKey: "later-segment", Operations: []project.Operation{{Type: "clip.insert", TrackID: seq.Tracks[0].ID, ResourceID: session.Document.Resources[0].ID, At: 600}}})
	if err != nil {
		t.Fatal(err)
	}
	segmentReq.ExpectedRevision = view.Sequence.Revision
	segmentReq.ContentVersion = view.ContentVersion
	if cached, err := svc.CreateSequenceSegment(ctx, pid, seq.ID, segmentReq); err != nil || !cached.Cached || cached.Key != part.Key {
		t.Fatalf("unrelated later edit invalidated earlier segment: %+v %v", cached, err)
	}
	if _, err := svc.SequenceSegmentPath(ctx, pid, seq.ID, oldVersion, part.Key); !errors.Is(err, model.ErrConflict) {
		t.Fatal("stale segment URL accepted", err)
	}
	if _, err := svc.SequenceSegmentPath(ctx, pid, seq.ID, view.ContentVersion, part.Key); err != nil {
		t.Fatal("reused segment unavailable to current version", err)
	}
	if err = svc.CloseSequenceFrame(pid, current.ID, seq.ID); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "workspace", "sequence-previews"))
	if err != nil || len(entries) != 0 {
		t.Fatal("released worker retained output", err)
	}
	seek.ExpectedRevision = view.Sequence.Revision
	seek.ContentVersion = view.ContentVersion
	warm, err = svc.CreateSequenceFrame(ctx, pid, seq.ID, seek)
	if err != nil || warm.WorkerPID == firstPID {
		t.Fatal("released worker was reused", err)
	}
	if err = svc.CloseSequenceFrame(pid, current.ID, seq.ID); err != nil {
		t.Fatal(err)
	}
	session, err = svc.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: session.Path, ExpectedSession: session.ID, Decision: "discard", StopTasks: true})
	if err != nil {
		t.Fatal(err)
	}
	edlAfter, err := os.Stat(edl)
	if err != nil || !edlAfter.ModTime().Equal(edlBefore.ModTime()) {
		t.Fatal("unchanged resource projection was rewritten on reopen", err)
	}
	restoreReq.Session = session.ID
	restored, err := svc.CreateSequenceFrame(ctx, pid, seq.ID, restoreReq)
	if err != nil || restored.Image != initialImage || restored.WorkerPID != 0 || restored.Diagnostics["backend"] != "cached-still" {
		t.Fatalf("reopen should reuse the saved playhead frame without starting a worker: backend=%v pid=%d error=%v", restored.Diagnostics["backend"], restored.WorkerPID, err)
	}
}

func mediaTool(t *testing.T, name string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("media fixture: %v %s", err, out)
	}
}
func TestProjectCameraRawDevelopAndSharedRender(t *testing.T) {
	python := os.Getenv("VAC_MEDIA_PYTHON")
	if python == "" {
		t.Skip("explicit local requirements-media.txt environment required")
	}
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	wd, _ := os.Getwd()
	sourceRoot := filepath.Clean(filepath.Join(wd, "..", "..", ".."))
	if err := svc.ConfigureTTS(python, filepath.Join(sourceRoot, "workers", "python")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "sensor.dng")
	mediaTool(t, python, filepath.Join(sourceRoot, "workers", "python", "tests", "test_raw_image.py"), "--fixture", path)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "raw.avp"), Name: "Raw"})
	if err != nil {
		t.Fatal(err)
	}
	pid := view.Document.Project.ID
	view, err = svc.ImportProjectResource(ctx, pid, path)
	if err != nil {
		t.Fatal(err)
	}
	r := view.Document.Resources[0]
	if r.RawDevelop == nil || r.Kind != "image" {
		t.Fatal("RAW import metadata missing")
	}
	// A fresh import loads a thumbnail and a large preview concurrently.
	failures := make(chan error, 3)
	for range 2 {
		go func() { _, err := svc.CreateResourcePreview(ctx, pid, r.ID); failures <- err }()
	}
	go func() { _, err := svc.ResourceThumbnail(ctx, pid, r.ID, 0); failures <- err }()
	for range 3 {
		if err := <-failures; err != nil {
			t.Fatal("concurrent RAW preview", err)
		}
	}
	initial, err := svc.CreateResourcePreview(ctx, pid, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(initial)
	if _, err := svc.ResourceThumbnail(ctx, pid, r.ID, 0); err != nil {
		t.Fatal(err)
	}
	seq := view.Document.Sequences[0]
	track, err := svc.TimelineCommand(ctx, "human:window", pid, seq.ID, project.Command{Session: view.ID, ExpectedRevision: seq.Revision, IdempotencyKey: "raw-insert", Operations: []project.Operation{{Type: "clip.insert", TrackID: seq.Tracks[0].ID, ResourceID: r.ID, At: 0}}})
	if err != nil {
		t.Fatal(err)
	}
	priorVersion := track.ContentVersion
	recipe := *r.RawDevelop
	recipe.Exposure = 1
	recipe.Saturation = 0
	view, err = svc.UpdateProjectResource(ctx, pid, r.ID, service.ResourcePatch{RawDevelop: &recipe})
	if err != nil {
		t.Fatal(err)
	}
	track, err = svc.Timeline(ctx, pid, seq.ID)
	if err != nil {
		t.Fatal(err)
	}
	if track.ContentVersion == priorVersion {
		t.Fatal("recipe change did not invalidate timeline")
	}
	if _, err := svc.ProjectFileCommand(ctx, "undo", service.ProjectCommand{ExpectedSession: view.ID}); err != nil {
		t.Fatal(err)
	}
	undo, err := svc.Timeline(ctx, pid, seq.ID)
	if err != nil || undo.ContentVersion != priorVersion {
		t.Fatal("RAW undo did not restore content", err)
	}
	if _, err := svc.ProjectFileCommand(ctx, "redo", service.ProjectCommand{ExpectedSession: view.ID}); err != nil {
		t.Fatal(err)
	}
	current, err := svc.CreateResourcePreview(ctx, pid, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	now, _ := os.ReadFile(current)
	if initial == current || string(first) == string(now) {
		t.Fatal("stale developed image reused")
	}
	f, err := os.Open(current)
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(f)
	f.Close()
	if err != nil || image.Bounds().Dx() != 64 {
		t.Fatal("invalid developed dimensions", err)
	}
	preview, err := svc.CreateSequencePreview(ctx, pid, seq.ID, service.SequencePreviewRequest{Session: view.ID, ExpectedRevision: track.Sequence.Revision, ContentVersion: track.ContentVersion})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Frames != 150 {
		t.Fatalf("RAW timeline %d", preview.Frames)
	}
	c := project.DrawComposition{ID: "raw-draw", Name: "Raw image", Engine: "remotion", Source: "export default ()=>null", Width: 320, Height: 180, FPS: project.Rate{Num: 30, Den: 1}, DurationFrames: 3, Images: []project.DrawImageLayer{{ID: "photo", ResourceID: r.ID, Width: 320, Height: 180, ToFrame: 2}}}
	view, err = svc.ProjectWorkspace(ctx)
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.SaveDrawComposition(ctx, pid, service.DrawSaveRequest{Session: view.ID, ExpectedRevision: view.Revision, IdempotencyKey: "raw-draw", Composition: c})
	if err != nil {
		t.Fatal(err)
	}
	c = view.Document.DrawCompositions[0]
	if c.Images[0].ResourceSHA == r.Source.SHA256 {
		t.Fatal("RAW recipe missing from drawing binding")
	}
	if _, err := svc.RenderDrawComposition(ctx, pid, c.ID, service.DrawActionRequest{Session: view.ID, ContentVersion: c.ContentVersion}); err != nil {
		t.Fatal(err)
	}
	currentTimeline, err := svc.Timeline(ctx, pid, seq.ID)
	if err != nil {
		t.Fatal(err)
	}
	attached, err := svc.ImportDrawComposition(ctx, "human:window", pid, c.ID, service.DrawImportRequest{Session: view.ID, ContentVersion: c.ContentVersion, SequenceID: seq.ID, ExpectedRevision: currentTimeline.Sequence.Revision, IdempotencyKey: "freeze-raw-source", SourceMode: "snapshot"})
	if err != nil {
		t.Fatal(err)
	}
	_ = attached
	resources, err := svc.ProjectResources(pid)
	if err != nil {
		t.Fatal(err)
	}
	var frozenResource project.Resource
	for _, candidate := range resources {
		if candidate.Draw != nil {
			frozenResource = candidate
		}
	}
	firstThumbnail, err := svc.ResourceThumbnail(ctx, pid, frozenResource.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	firstPixels, err := os.ReadFile(firstThumbnail)
	if err != nil {
		t.Fatal(err)
	}
	recipe.Exposure = 0
	view, err = svc.UpdateProjectResource(ctx, pid, r.ID, service.ResourcePatch{RawDevelop: &recipe})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.DrawRenderPath(pid, c.ID, c.ContentVersion); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale drawing allowed %v", err)
	}
	// Drop only this test's generated frames/derivative: the AVP must retain
	// enough source and RAW recipe state to regenerate the frozen picture.
	if err = os.Remove(firstThumbnail); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(current); err != nil {
		t.Fatal(err)
	}
	restoredThumbnail, err := svc.ResourceThumbnail(ctx, pid, frozenResource.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	restoredPixels, err := os.ReadFile(restoredThumbnail)
	if err != nil || string(restoredPixels) != string(firstPixels) {
		t.Fatal("RAW source snapshot changed after recipe update/cache loss", err)
	}
	view, err = svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{ExpectedSession: view.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = svc.ProjectFileCommand(ctx, "close", service.ProjectCommand{ExpectedSession: view.ID})
	if err != nil {
		t.Fatal(err)
	}
	view, err = svc.ProjectFileCommand(ctx, "open", service.ProjectCommand{Path: view.Path})
	if err != nil {
		t.Fatal(err)
	}
	if view.Document.Resources[0].RawDevelop.Saturation != 0 {
		t.Fatal("RAW recipe not portable")
	}
	// Relink between RAW and ordinary images must not leave an invalid recipe.
	view, err = svc.UpdateProjectResource(ctx, pid, r.ID, service.ResourcePatch{Path: current})
	if err != nil || view.Document.Resources[0].RawDevelop != nil {
		t.Fatal("RAW to PNG relink retained recipe", err)
	}
	if _, err = svc.ProjectFileCommand(ctx, "save", service.ProjectCommand{ExpectedSession: view.ID}); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("RAW source was modified")
	}
}

func TestProjectExpandedMediaAndMatroskaStreams(t *testing.T) {
	svc, root := avpWorkspaceFixture(t)
	ctx := t.Context()
	view, err := svc.ProjectFileCommand(ctx, "new", service.ProjectCommand{Path: filepath.Join(root, "formats.avp"), Name: "Formats"})
	if err != nil {
		t.Fatal(err)
	}
	pid := view.Document.Project.ID
	mkv := filepath.Join(root, "multi.mkv")
	mediaTool(t, "ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=blue:s=64x48:r=30:d=1", "-f", "lavfi", "-i", "sine=frequency=440:duration=1", "-f", "lavfi", "-i", "sine=frequency=880:duration=1", "-map", "0:v", "-map", "1:a", "-map", "2:a", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "pcm_s16le", mkv)
	view, err = svc.ImportProjectResource(ctx, pid, mkv)
	if err != nil {
		t.Fatal(err)
	}
	resource := view.Document.Resources[0]
	if len(project.Streams(resource, "audio")) != 2 {
		t.Fatal("Matroska lost audio streams")
	}
	seq := view.Document.Sequences[0]
	tracks, err := svc.TimelineCommand(ctx, "human:window", pid, seq.ID, project.Command{Session: view.ID, ExpectedRevision: seq.Revision, IdempotencyKey: "multi", Operations: []project.Operation{{Type: "clip.insert", TrackID: seq.Tracks[0].ID, ResourceID: resource.ID, At: 0}}})
	if err != nil {
		t.Fatal(err)
	}
	streams := map[int]bool{}
	links := map[string]bool{}
	for _, track := range tracks.Sequence.Tracks {
		for _, clip := range track.Clips {
			streams[clip.StreamIndex] = true
			links[clip.LinkID] = true
		}
	}
	if len(streams) != 3 || !streams[0] || !streams[1] || !streams[2] || len(links) != 1 || links[""] {
		t.Fatalf("source stream/link mapping %v %v", streams, links)
	}
	for _, ext := range []string{"mp4", "mov", "avi", "flv", "mp3", "wav", "jpg", "jpeg", "jpe", "jepg", "png", "gif", "bmp", "tiff", "webp"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(root, "fixture."+ext)
			if ext == "jpe" {
				path = filepath.Join(root, "fixture%03d.jpe")
			}
			args := []string{"-v", "error", "-y", "-i", mkv}
			if ext == "mp3" {
				args = append(args, "-map", "0:a:0", "-c:a", "libmp3lame")
			} else if ext == "mp4" || ext == "mov" || ext == "avi" {
				args = append(args, "-map", "0:v:0", "-map", "0:a:0", "-c:v", "libx264", "-c:a", "aac")
			} else if ext == "wav" {
				args = append(args, "-map", "0:a:1", "-c:a", "pcm_s16le")
			} else if ext == "flv" {
				args = append(args, "-map", "0:v:0", "-map", "0:a:0", "-c:v", "flv", "-c:a", "libmp3lame", "-ar", "44100")
			} else {
				args = append(args, "-map", "0:v:0", "-frames:v", "1")
				if ext == "jepg" {
					args = append(args, "-c:v", "mjpeg", "-f", "image2")
				}
				if ext == "jpe" {
					args = append(args, "-c:v", "mjpeg", "-f", "image2", "-update", "1")
				}
			}
			mediaTool(t, "ffmpeg", append(args, path)...)
			state, err := svc.ImportProjectResource(ctx, pid, path)
			if err != nil {
				t.Fatal(err)
			}
			r := state.Document.Resources[len(state.Document.Resources)-1]
			if _, err := svc.CreateResourcePreview(ctx, pid, r.ID); err != nil {
				t.Fatal(err)
			}
			if ext == "wav" && len(project.Streams(r, "audio")) != 1 {
				t.Fatal("WAV audio missing")
			}
			invalid := project.DefaultRawDevelop()
			if _, err := svc.UpdateProjectResource(ctx, pid, r.ID, service.ResourcePatch{RawDevelop: &invalid}); !errors.Is(err, model.ErrArgument) {
				t.Fatal("RAW recipe accepted on ordinary media", err)
			}
		})
	}
	raw, _ := json.Marshal(tracks)
	_ = os.WriteFile(filepath.Join(root, "stream-verification.json"), raw, 0600)
}
