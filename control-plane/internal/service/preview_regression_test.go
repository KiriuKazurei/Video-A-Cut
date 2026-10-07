package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type finalMediaProber struct{ media preview.Media }

func (p finalMediaProber) ProbeMedia(ctx context.Context, path string) (preview.Media, error) {
	if filepath.Base(path) == "final.mp4" {
		return p.media, nil
	}
	return preview.Media{Width: 1280, Height: 720, FPS: 30, FrameCount: 30, DurationSeconds: 1, VideoCodec: "h264"}, nil
}

func TestPreviewSubmissionChecksSpecificationsAndRegeneration(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*preview.Media, *preview.Report)
		valid  bool
	}{
		{"valid", func(*preview.Media, *preview.Report) {}, true},
		{"empty_checks", func(m *preview.Media, r *preview.Report) { r.Checks = nil }, false},
		{"wrong_codec", func(m *preview.Media, r *preview.Report) { m.VideoCodec = "mpeg4" }, false},
		{"no_audio", func(m *preview.Media, r *preview.Report) { m.AudioCodec = ""; m.SampleRate = 0; m.Channels = 0 }, false},
		{"wrong_frames", func(m *preview.Media, r *preview.Report) { m.FrameCount = 1 }, false},
		{"fractional_fps", func(m *preview.Media, r *preview.Report) { m.FPS = 29.97 }, false},
		{"wrong_canvas", func(m *preview.Media, r *preview.Report) { m.Width = 16 }, false},
		{"wrong_target", func(m *preview.Media, r *preview.Report) { r.TargetFrames = 31 }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			svc, root, _, run, rev := setupPreviewExportFixture(t)
			ctx := context.Background()
			registerValidCapability(t, svc, "agent-exp-1")
			req := preview.StartRequest{RevisionID: rev.RevisionID, ExpectedVersion: run.Version, PresetID: preview.PresetID, RequestID: "spec"}
			v, _, e := svc.StartPreview(ctx, "human:test", run.RunID, req)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = svc.ClaimForExecution(ctx, "agent-exp-1", "exporter", time.Now().Add(time.Minute), service.ClaimOptions{}); e != nil {
				t.Fatal(e)
			}
			job, e := getTestStore(svc).GetPreviewJob(ctx, v.PreviewID)
			if e != nil {
				t.Fatal(e)
			}
			dir := filepath.Join("previews", job.AssetID, job.RevisionID, job.PreviewID)
			writeCompletePreviewOutput(t, root, dir, job)
			reportPath := filepath.Join(root, dir, preview.ReportFilename)
			raw, e := os.ReadFile(reportPath)
			if e != nil {
				t.Fatal(e)
			}
			var report preview.Report
			if e = json.Unmarshal(raw, &report); e != nil {
				t.Fatal(e)
			}
			media := report.Media
			c.mutate(&media, &report)
			svc.SetMediaProber(finalMediaProber{media})
			raw, _ = json.Marshal(report)
			if e = os.WriteFile(reportPath, raw, 0600); e != nil {
				t.Fatal(e)
			}
			manPath := filepath.Join(root, dir, preview.ManifestFilename)
			raw, e = os.ReadFile(manPath)
			if e != nil {
				t.Fatal(e)
			}
			var man preview.Manifest
			if e = json.Unmarshal(raw, &man); e != nil {
				t.Fatal(e)
			}
			for i := range man.Artifacts {
				a := &man.Artifacts[i]
				b, e := os.ReadFile(filepath.Join(root, dir, a.Path))
				if e != nil {
					t.Fatal(e)
				}
				h := sha256.Sum256(b)
				a.SHA256 = hex.EncodeToString(h[:])
				a.Size = int64(len(b))
			}
			raw, _ = json.Marshal(man)
			if e = os.WriteFile(manPath, raw, 0600); e != nil {
				t.Fatal(e)
			}
			e = svc.SubmitPreview(ctx, "agent-exp-1", job.TaskID, filepath.ToSlash(dir))
			if c.valid {
				if e != nil {
					t.Fatal(e)
				}
				req.RequestID = "regenerate"
				next, created, e := svc.StartPreview(ctx, "human:test", run.RunID, req)
				if e != nil || !created || next.PreviewID == v.PreviewID {
					t.Fatalf("explicit regeneration reused succeeded job: %+v %v", next, e)
				}
			} else if e == nil {
				t.Fatal("noncompliant output accepted")
			}
		})
	}
}

func TestPreviewConcurrentRequestsBindTransportRetries(t *testing.T) {
	svc, _, _, run, rev := setupPreviewExportFixture(t)
	ctx := context.Background()
	registerValidCapability(t, svc, "agent-exp-1")
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, _, e := svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{RevisionID: rev.RevisionID, ExpectedVersion: run.Version, PresetID: preview.PresetID, RequestID: fmt.Sprintf("concurrent-%d", i)})
			if e != nil {
				errs <- e
			} else {
				ids <- v.PreviewID
			}
		}(i)
	}
	wg.Wait()
	close(ids)
	close(errs)
	for e := range errs {
		t.Fatal(e)
	}
	var id string
	for got := range ids {
		if id != "" && id != got {
			t.Fatalf("created more than one active task: %s %s", id, got)
		}
		id = got
	}
	if _, e := svc.CancelPreview(ctx, "human:test", id); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 8; i++ {
		v, created, e := svc.StartPreview(ctx, "human:test", run.RunID, preview.StartRequest{RevisionID: rev.RevisionID, ExpectedVersion: run.Version, PresetID: preview.PresetID, RequestID: fmt.Sprintf("concurrent-%d", i)})
		if e != nil || created || v.PreviewID != id {
			t.Fatalf("retry lost original binding: %+v %v", v, e)
		}
	}
}

func TestAuditPreviewRejectsReplacedAssetEDL(t *testing.T) {
	svc, _, asset, run, rev := setupPreviewExportFixture(t)
	ctx := context.Background()
	registerValidCapability(t, svc, "agent-exp-1")
	asset.Artifacts["edl"] = "exports/new-package/edl.json"
	if err := getTestStore(svc).UpdateAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	v, created, err := svc.StartPreview(ctx, "human:audit", run.RunID, preview.StartRequest{RevisionID: rev.RevisionID, ExpectedVersion: run.Version, PresetID: preview.PresetID, RequestID: "audit-stale"})
	if err == nil {
		t.Fatalf("stale asset package accepted: created=%v is_current=%v preview=%s", created, v.IsCurrent, v.PreviewID)
	}
}

func TestAuditPreviewRequestIDCannotRebindRevision(t *testing.T) {
	svc, _, _, run, rev := setupPreviewExportFixture(t)
	ctx := context.Background()
	st := getTestStore(svc)
	registerValidCapability(t, svc, "agent-exp-1")
	req := preview.StartRequest{RevisionID: rev.RevisionID, ExpectedVersion: run.Version, PresetID: preview.PresetID, RequestID: "audit-request"}
	first, _, err := svc.StartPreview(ctx, "human:audit", run.RunID, req)
	if err != nil {
		t.Fatal(err)
	}
	nextRev := rev
	nextRev.RevisionID = "rev-export-2"
	if err = st.InsertRevision(ctx, nextRev); err != nil {
		t.Fatal(err)
	}
	if err = st.SetStageOutput(ctx, "task-export-stage", nextRev.RevisionID); err != nil {
		t.Fatal(err)
	}
	run.CurrentRevisionID = nextRev.RevisionID
	run.Version++
	if err = st.UpdateWorkflow(ctx, run); err != nil {
		t.Fatal(err)
	}
	req.RevisionID = nextRev.RevisionID
	req.ExpectedVersion = run.Version
	second, _, err := svc.StartPreview(ctx, "human:audit", run.RunID, req)
	if err == nil {
		t.Fatalf("different revision silently reused: first=%s requested=%s returned=%s preview=%s", first.RevisionID, req.RevisionID, second.RevisionID, second.PreviewID)
	}
}

func TestAuditPreviewRejectsNonCompliantRealMedia(t *testing.T) {
	svc, root, _, run, rev := setupPreviewExportFixture(t)
	ctx := context.Background()
	registerValidCapability(t, svc, "agent-exp-1")
	v, _, err := svc.StartPreview(ctx, "human:audit", run.RunID, preview.StartRequest{RevisionID: rev.RevisionID, ExpectedVersion: run.Version, PresetID: preview.PresetID, RequestID: "audit-media"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := svc.ClaimForExecution(ctx, "agent-exp-1", "exporter", time.Now().Add(time.Minute), service.ClaimOptions{})
	if err != nil || !claim.Claimed || claim.Task.TaskID != v.TaskID {
		t.Fatalf("claim: %+v %v", claim, err)
	}
	job, err := getTestStore(svc).GetPreviewJob(ctx, v.PreviewID)
	if err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join("previews", job.AssetID, job.RevisionID, job.PreviewID)
	writeCompletePreviewOutput(t, root, rel, job)
	dir := filepath.Join(root, rel)
	cmd := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=red:s=16x16:r=5:d=0.2", "-c:v", "mpeg4", "-an", filepath.Join(dir, "final.mp4"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
	reportPath := filepath.Join(dir, preview.ReportFilename)
	raw, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report preview.Report
	if err = json.Unmarshal(raw, &report); err != nil {
		t.Fatal(err)
	}
	report.Checks = []preview.Check{}
	raw, err = json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(reportPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, preview.ManifestFilename)
	raw, err = os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest preview.Manifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		t.Fatal(err)
	}
	for i := range manifest.Artifacts {
		a := &manifest.Artifacts[i]
		b, e := os.ReadFile(filepath.Join(dir, a.Path))
		if e != nil {
			t.Fatal(e)
		}
		h := sha256.Sum256(b)
		a.SHA256 = hex.EncodeToString(h[:])
		a.Size = int64(len(b))
	}
	raw, err = json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(manifestPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	// Uses the production ffprobe prober, not the existing unit-test stub.
	if err = svc.SubmitPreview(ctx, "agent-exp-1", job.TaskID, filepath.ToSlash(rel)); err == nil {
		result, e := svc.GetPreview(ctx, job.PreviewID)
		if e != nil {
			t.Fatal(e)
		}
		t.Fatalf("noncompliant real media accepted: status=%s media=%+v report_checks=%d", result.Status, result.Media, len(report.Checks))
	}
}
