package service_test

import (
	"encoding/json"
	"errors"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

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
	recipe.Exposure = 0
	view, err = svc.UpdateProjectResource(ctx, pid, r.ID, service.ResourcePatch{RawDevelop: &recipe})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.DrawRenderPath(pid, c.ID, c.ContentVersion); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale drawing allowed %v", err)
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
	for _, ext := range []string{"mp4", "mov", "avi", "flv", "mp3", "wav", "jpg", "jpeg", "jepg", "png", "gif", "bmp", "tiff", "webp"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(root, "fixture."+ext)
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
