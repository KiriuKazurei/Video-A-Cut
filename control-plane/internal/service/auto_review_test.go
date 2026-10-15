package service_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/autoreview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// scoreEnv is an auto run whose export is finished and whose preview exists.
// The overlay row is inserted directly: B2 reads what B1 wrote and does not
// depend on how the run got there.
type scoreEnv struct {
	svc    *service.Service
	root   string
	run    model.WorkflowRun
	rev    model.Revision
	st     *store.Store
	events *recordedEvents
}

type recordedEvents struct {
	mu  sync.Mutex
	got []events.Envelope
}

func (r *recordedEvents) add(e events.Envelope) {
	r.mu.Lock()
	r.got = append(r.got, e)
	r.mu.Unlock()
}

// named waits briefly for the bus (it delivers asynchronously) and returns the
// events with that name seen so far.
func (r *recordedEvents) named(name string, want int) []events.Envelope {
	deadline := time.Now().Add(2 * time.Second)
	for {
		r.mu.Lock()
		var out []events.Envelope
		for _, e := range r.got {
			if e.Name == name {
				out = append(out, e)
			}
		}
		r.mu.Unlock()
		if len(out) >= want || time.Now().After(deadline) {
			return out
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// goodEDL is a one-second timeline with one confident scene, one narration line
// of three characters (3.75 per second), and audio and subtitle inside the window.
func goodEDL(digest string, confidence any) map[string]any {
	return map[string]any{
		"timeline":   map[string]any{"fps": 30, "sample_rate": 48000},
		"video":      []any{map[string]any{"src": "media/clip.mp4", "in": 0, "out": 1, "timeline_in": 0}},
		"game_audio": []any{map[string]any{"src": "media/clip.mp4", "in": 0, "out": 1, "timeline_in": 0}},
		"scenes": []any{map[string]any{"scene_id": "scn_1", "index": 0, "label": "团战", "method": "vision",
			"confidence": confidence, "evidence_frames": []string{digest}}},
		"narration": []any{map[string]any{"id": "nar_scn_1", "text": "三个字", "start": 0.2, "end": 1.0, "source_scene_id": "scn_1", "source_scene_label": "团战"}},
		"voice":     []any{map[string]any{"id": "vo_scn_1", "src": "media/vo_scn_1.wav", "start": 0.2, "end": 0.9}},
		"subtitle":  []any{map[string]any{"id": "sub_scn_1", "start": 0.2, "end": 0.9}},
		"music":     []any{},
	}
}

func newScoreEnv(t *testing.T, confidence any, brief string) *scoreEnv {
	t.Helper()
	svc := newService(t)
	root := t.TempDir()
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	pkg := "exports/clip_pkg"
	writeWorkflowFixture(t, root, pkg) // samples/frame.jpg + evidence manifest
	pkgDir := filepath.Join(root, pkg)
	frame, _ := os.ReadFile(filepath.Join(pkgDir, "samples", "frame.jpg"))
	edlBody, _ := json.Marshal(goodEDL(sha(frame), confidence))
	manifest := `{"schema_version": 1, "artifacts": [
		{"kind": "edl", "path": "edl.json"}, {"kind": "source_video", "path": "media/clip.mp4"},
		{"kind": "game_audio", "path": "audio_a1.wav"}, {"kind": "voice", "path": "audio_a2.wav"}, {"kind": "music", "path": "audio_a3.wav"},
		{"kind": "sample", "path": "samples/frame.jpg"}, {"kind": "evidence_manifest", "path": "samples/evidence-manifest.json"}]}`
	if err := os.MkdirAll(filepath.Join(pkgDir, "media"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"edl.json": string(edlBody), "delivery-manifest.json": manifest, "media/clip.mp4": "fake-mp4",
		"media/vo_scn_1.wav": "fake-voice", "audio_a1.wav": "a1", "audio_a2.wav": "a2", "audio_a3.wav": "a3",
	} {
		if err := os.WriteFile(filepath.Join(pkgDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	evidence, _ := os.ReadFile(filepath.Join(pkgDir, "samples", "evidence-manifest.json"))

	asset := model.Asset{AssetID: "asset-score", Status: model.AssetStatusExported, AgentVisible: true, AllowedAgents: []string{"exporter"},
		Artifacts: map[string]string{"edl": pkg + "/edl.json", "manifest": pkg + "/delivery-manifest.json"}}
	if err := svc.CreateAsset(ctx, asset); err != nil {
		t.Fatal(err)
	}
	rev := model.Revision{RevisionID: "rev-score-1", AssetID: asset.AssetID, SchemaVersion: 1, PackageRef: pkg,
		EDLSHA256: sha(edlBody), EvidenceManifestSHA256: sha(evidence), CreatedAt: time.Now().UTC(), CreatedBy: "agent:exporter", Reason: "export"}
	st := getTestStore(svc)
	if err := st.InsertRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	run := model.WorkflowRun{RunID: "run-score", AssetID: asset.AssetID, BaseRevisionID: rev.RevisionID, CurrentRevisionID: rev.RevisionID,
		Status: model.WorkflowReadyAcceptance, Stage: "acceptance", Version: 3, ContentMode: "configured",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := st.InsertWorkflow(ctx, run); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertStage(ctx, model.WorkflowStage{RunID: run.RunID, TaskID: "task-export-stage", Stage: model.TaskTypeExport,
		InputRevisionID: "rev-input-0", OutputRevisionID: rev.RevisionID}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateTask(ctx, model.Task{TaskID: "task-export-stage", AssetID: asset.AssetID, Type: model.TaskTypeExport,
		AgentRole: "exporter", Status: model.TaskStatusSucceeded, UpdatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := st.InsertWorkflowAuto(ctx, model.WorkflowAuto{RunID: run.RunID, AutoVersion: 1, State: model.AutoStateActive,
		Brief: brief, BriefSHA256: sha([]byte(brief)), AuthorizedBy: "human:webui", AuthorizedAt: now,
		NoticeSHA256: "n", ProfileID: "p", ProfileRevision: 1, ProfileSHA256: "s",
		Scope:     model.AutoScope{MaxActions: 10, AutoPreviewExport: true, Thresholds: map[string]float64{}},
		MaxRounds: 1, MaxTotalRounds: 5, CreatedAt: now, UpdatedAt: now, DeadlineAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	bus := events.New()
	svc.SetBus(bus)
	rec := &recordedEvents{}
	bus.Subscribe("auto.reviewed", rec.add)
	bus.Subscribe("auto.changed", rec.add)
	return &scoreEnv{svc: svc, root: root, run: run, rev: rev, st: st, events: rec}
}

// preview starts a preview and, when finish is set, completes it with a
// matching one-second report.
func (e *scoreEnv) preview(t *testing.T, finish bool) string {
	t.Helper()
	ctx := t.Context()
	e.prepPreview(t)
	// The auto-reviewer's own start: a person starting it would take the run over.
	view, _, err := e.svc.AutoStartPreview(ctx, e.run.RunID, e.run.Version, e.auto(t).AutoVersion, e.rev.RevisionID)
	if err != nil {
		t.Fatal(err)
	}
	// Starting the preview is plumbing for these tests: leave the overlay as the
	// B2 tests were written against, with no action and no version spent.
	mustExec(t, e.st, `UPDATE workflow_auto SET auto_version = 1, actions_used = 0 WHERE run_id = ?`, e.run.RunID)
	mustExec(t, e.st, `DELETE FROM auto_actions WHERE run_id = ? AND kind = 'preview'`, e.run.RunID)
	if !finish {
		return view.PreviewID
	}
	e.finishPreview(t, view.PreviewID)
	return view.PreviewID
}

// prepPreview registers a capable export worker and a matching media prober.
func (e *scoreEnv) prepPreview(t *testing.T) {
	t.Helper()
	registerValidCapability(t, e.svc, "agent-exp-1")
	e.svc.SetMediaProber(&testProber{media: preview.Media{Width: 1280, Height: 720, FPS: 30, FrameCount: 30, DurationSeconds: 1.0,
		VideoCodec: "h264", AudioCodec: "aac", SampleRate: 48000, Channels: 2,
		AudioInterval: &preview.AudioInterval{StartPTS: 0, DurationTS: 48000, TimeBase: "1/48000", StartSeconds: 0, DurationSeconds: 1}}})
}

// finishPreview plays the export worker: claims the preview and delivers a
// matching one-second report.
func (e *scoreEnv) finishPreview(t *testing.T, previewID string) {
	t.Helper()
	ctx := t.Context()
	registerValidCapability(t, e.svc, "agent-exp-1")
	if _, err := e.svc.ClaimForExecution(ctx, "agent-exp-1", "exporter", time.Now().Add(time.Minute), previewClaimOptions("agent-exp-1")); err != nil {
		t.Fatal(err)
	}
	job, err := e.st.GetPreviewJob(ctx, previewID)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join("previews", job.AssetID, job.RevisionID, job.PreviewID)
	writeCompletePreviewOutput(t, e.root, dir, job)
	if err := e.svc.SubmitPreviewScoped(ctx, previewTestScope(t, e.svc, "agent-exp-1", job.TaskID), filepath.ToSlash(dir)); err != nil {
		t.Fatalf("SubmitPreview: %v", err)
	}
}

// setThresholds rewrites the stored scope the way a narrowing would leave it.
func (e *scoreEnv) setThresholds(t *testing.T, th map[string]float64) {
	t.Helper()
	a := e.auto(t)
	a.Scope.Thresholds = th
	body, _ := json.Marshal(a.Scope)
	mustExec(t, e.st, `UPDATE workflow_auto SET scope_json = ? WHERE run_id = ?`, string(body), e.run.RunID)
}

func (e *scoreEnv) auto(t *testing.T) model.WorkflowAuto {
	t.Helper()
	a, err := e.st.GetWorkflowAuto(t.Context(), e.run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestScoreAutoRunStoresHighReviewAndEmitsEvent(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	pid := e.preview(t, true)
	before := e.auto(t)
	review, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, before.AutoVersion)
	if err != nil {
		t.Fatal(err)
	}
	if review.Band != autoreview.BandHigh || review.Score != 0.965 || review.ScorerVersion != autoreview.ScorerVersion ||
		review.RunID != e.run.RunID || review.RevisionID != e.rev.RevisionID || review.PreviewID != pid || review.Round != 0 || review.ReviewID == "" {
		t.Fatalf("review = %+v", review)
	}
	if string(review.Reasons) != "[]" {
		t.Fatalf("reasons = %s", review.Reasons)
	}
	var th autoreview.Thresholds
	if err := json.Unmarshal(review.Thresholds, &th); err != nil || th != autoreview.DefaultThresholds() {
		t.Fatalf("thresholds_json = %s (%v)", review.Thresholds, err)
	}
	var inputs struct {
		Components []autoreview.Component `json:"components"`
		Facts      autoreview.Facts       `json:"facts"`
	}
	if err := json.Unmarshal(review.Inputs, &inputs); err != nil || len(inputs.Components) != 4 || inputs.Facts.Lines != 1 || inputs.Facts.LinesWithAudio != 1 {
		t.Fatalf("inputs_json = %s (%v)", review.Inputs, err)
	}
	after := e.auto(t)
	if after.ActionsUsed != before.ActionsUsed+1 || after.AutoVersion != before.AutoVersion+1 || after.State != model.AutoStateActive {
		t.Fatalf("overlay: %+v -> %+v", before, after)
	}
	if n, _ := e.st.CountAutoActions(t.Context(), e.run.RunID); n != after.ActionsUsed {
		t.Fatalf("ledger rows %d != actions_used %d", n, after.ActionsUsed)
	}
	if got, _ := e.st.GetWorkflow(t.Context(), e.run.RunID); got.Version != e.run.Version || got.Status != model.WorkflowReadyAcceptance {
		t.Fatalf("scoring changed the run: %+v", got)
	}
	reviewed := e.events.named("auto.reviewed", 1)
	if len(reviewed) != 1 {
		t.Fatalf("auto.reviewed events: %d", len(reviewed))
	}
	payload, _ := reviewed[0].Payload.(map[string]any)
	if payload["review_id"] != review.ReviewID || payload["band"] != "high" || payload["run_id"] != e.run.RunID {
		t.Fatalf("payload = %+v", payload)
	}
	if audit := auditActions(t, e.svc)["auto.review"]; len(audit) != 1 || audit[0].Actor != model.AutoActor {
		t.Fatalf("audit = %+v", audit)
	}
	list, err := e.svc.ListAutoReviews(t.Context(), e.run.RunID)
	if err != nil || len(list) != 1 || list[0].ReviewID != review.ReviewID {
		t.Fatalf("list = %+v %v", list, err)
	}
	view, err := e.svc.GetAuto(t.Context(), e.run.RunID)
	if err != nil || view.LatestReview == nil || view.LatestReview.ReviewID != review.ReviewID {
		t.Fatalf("view = %+v %v", view, err)
	}
}

func TestScoreAutoRunIsIdempotentPerPreviewAndRound(t *testing.T) {
	e := newScoreEnv(t, 0.9, "")
	pid := e.preview(t, true)
	first, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, 1)
	if err != nil {
		t.Fatal(err)
	}
	again, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, 1) // stale version, replay
	if err != nil || again.ReviewID != first.ReviewID {
		t.Fatalf("replay = %+v %v", again, err)
	}
	if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_reviews WHERE run_id=?`, e.run.RunID); n != 1 {
		t.Fatalf("reviews = %d", n)
	}
	if a := e.auto(t); a.ActionsUsed != 1 || a.AutoVersion != 2 {
		t.Fatalf("a replay counted again: %+v", a)
	}
	e.events.named("auto.reviewed", 1)
	time.Sleep(50 * time.Millisecond)
	if got := len(e.events.named("auto.reviewed", 2)); got != 1 {
		t.Fatalf("a replay emitted another event: %d events", got)
	}
}

func TestScoreAutoRunLowConfidenceBlocksHigh(t *testing.T) {
	e := newScoreEnv(t, 0.5, "钓鱼")
	pid := e.preview(t, true)
	review, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, 1)
	if err != nil {
		t.Fatal(err)
	}
	// .35*.5+.65 = .825 -> MEDIUM, with the confidence warn and the brief downgrade.
	if review.Band != autoreview.BandMedium || review.Score != 0.825 {
		t.Fatalf("review = %+v", review)
	}
	var reasons []autoreview.Reason
	if err := json.Unmarshal(review.Reasons, &reasons); err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, r := range reasons {
		have[r.Code+":"+r.Item] = true
	}
	for _, want := range []string{"low_confidence:", "item_below_floor:recognition", "brief_low_coverage:"} {
		if !have[want] {
			t.Fatalf("missing %s in %v", want, reasons)
		}
	}
}

func TestScoreAutoRunBuiltinConfidenceFailsSafe(t *testing.T) {
	e := newScoreEnv(t, nil, "")
	pid := e.preview(t, true)
	review, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if review.Band == autoreview.BandHigh || review.Score != 0.65 || review.Band != autoreview.BandMedium {
		t.Fatalf("a scene with no confidence must not reach HIGH: %+v", review)
	}
}

func TestScoreAutoRunRefusals(t *testing.T) {
	ctx := t.Context()
	t.Run("not an auto run", func(t *testing.T) {
		e := newScoreEnv(t, 0.9, "")
		pid := e.preview(t, true)
		if _, err := e.st.DB().Exec(`DELETE FROM workflow_auto`); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.ScoreAutoRun(ctx, e.run.RunID, pid, 1); !errors.Is(err, model.ErrForbidden) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("stale auto version", func(t *testing.T) {
		e := newScoreEnv(t, 0.9, "")
		pid := e.preview(t, true)
		if _, err := e.svc.ScoreAutoRun(ctx, e.run.RunID, pid, 7); !errors.Is(err, model.ErrConflict) {
			t.Fatalf("%v", err)
		}
		if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_reviews`); n != 0 {
			t.Fatalf("reviews = %d", n)
		}
	})
	t.Run("preview not finished", func(t *testing.T) {
		e := newScoreEnv(t, 0.9, "")
		pid := e.preview(t, false)
		if _, err := e.svc.ScoreAutoRun(ctx, e.run.RunID, pid, 1); !errors.Is(err, model.ErrInvalidState) {
			t.Fatalf("%v", err)
		}
		if a := e.auto(t); a.ActionsUsed != 0 || a.AutoVersion != 1 {
			t.Fatalf("a refused score changed the overlay: %+v", a)
		}
	})
	t.Run("unknown preview", func(t *testing.T) {
		e := newScoreEnv(t, 0.9, "")
		if _, err := e.svc.ScoreAutoRun(ctx, e.run.RunID, "prv_nope", 1); !errors.Is(err, model.ErrNotFound) {
			t.Fatalf("%v", err)
		}
	})
	t.Run("invalidated preview is not current", func(t *testing.T) {
		e := newScoreEnv(t, 0.9, "")
		pid := e.preview(t, true)
		if err := e.st.InvalidatePreviewJob(ctx, pid); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.ScoreAutoRun(ctx, e.run.RunID, pid, 1); !errors.Is(err, model.ErrConflict) {
			t.Fatalf("%v", err)
		}
	})
	for name, mutate := range map[string]func(*store.Store, string){
		"revoked": func(st *store.Store, id string) {
			mustExec(t, st, `UPDATE workflow_auto SET state='revoked' WHERE run_id=?`, id)
		},
		"budget spent": func(st *store.Store, id string) {
			mustExec(t, st, `UPDATE workflow_auto SET actions_used=10 WHERE run_id=?`, id)
		},
		"past deadline": func(st *store.Store, id string) {
			mustExec(t, st, `UPDATE workflow_auto SET deadline_at='2000-01-01 00:00:00' WHERE run_id=?`, id)
		},
		"waiting on user": func(st *store.Store, id string) {
			mustExec(t, st, `UPDATE workflow_auto SET state='awaiting_user_decision' WHERE run_id=?`, id)
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newScoreEnv(t, 0.9, "")
			pid := e.preview(t, true)
			mutate(e.st, e.run.RunID)
			if _, err := e.svc.ScoreAutoRun(ctx, e.run.RunID, pid, 1); !errors.Is(err, model.ErrForbidden) {
				t.Fatalf("%v", err)
			}
			if n := count(t, e.svc, `SELECT COUNT(*) FROM auto_reviews`); n != 0 {
				t.Fatalf("reviews = %d", n)
			}
		})
	}
	t.Run("cancelled run", func(t *testing.T) {
		e := newScoreEnv(t, 0.9, "")
		pid := e.preview(t, true)
		mustExec(t, e.st, `UPDATE workflow_runs SET status='cancelled' WHERE run_id=?`, e.run.RunID)
		if _, err := e.svc.ScoreAutoRun(ctx, e.run.RunID, pid, 1); err == nil {
			t.Fatal("a cancelled run was scored")
		}
	})
	t.Run("invalid stored thresholds", func(t *testing.T) {
		e := newScoreEnv(t, 0.9, "")
		pid := e.preview(t, true)
		e.setThresholds(t, map[string]float64{"high_score_min": 0.5, "medium_score_min": 0.9})
		if _, err := e.svc.ScoreAutoRun(ctx, e.run.RunID, pid, 1); !errors.Is(err, model.ErrInvalidState) {
			t.Fatalf("%v", err)
		}
	})
}

func TestScoreAutoRunUsesScopeThresholds(t *testing.T) {
	e := newScoreEnv(t, 0.9, "")
	pid := e.preview(t, true)
	e.setThresholds(t, map[string]float64{"high_score_min": 0.99, "medium_score_min": 0.98})
	review, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, 1)
	if err != nil {
		t.Fatal(err)
	}
	// .965 is under both custom thresholds.
	if review.Band != autoreview.BandLow {
		t.Fatalf("band = %s", review.Band)
	}
	var th autoreview.Thresholds
	_ = json.Unmarshal(review.Thresholds, &th)
	if th.HighScore != 0.99 || th.MediumScore != 0.98 {
		t.Fatalf("thresholds_json = %s", review.Thresholds)
	}
}

func TestScoreAutoRunFailedPreviewIsLowAndNotImprovable(t *testing.T) {
	e := newScoreEnv(t, 0.9, "")
	pid := e.preview(t, false)
	job, _ := e.st.GetPreviewJob(t.Context(), pid)
	mustExec(t, e.st, `UPDATE tasks SET status='failed' WHERE task_id=?`, job.TaskID)
	review, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, 1)
	if err != nil {
		t.Fatal(err)
	}
	var inputs struct {
		Improvable bool   `json:"improvable"`
		Status     string `json:"preview_status"`
	}
	_ = json.Unmarshal(review.Inputs, &inputs)
	if review.Band != autoreview.BandLow || inputs.Improvable || inputs.Status != "failed" {
		t.Fatalf("review = %+v inputs = %s", review, review.Inputs)
	}
}

func TestScoreAutoRunMissingVoiceFileIsLow(t *testing.T) {
	e := newScoreEnv(t, 0.9, "")
	pid := e.preview(t, true)
	if err := os.Remove(filepath.Join(e.root, "exports/clip_pkg/media/vo_scn_1.wav")); err != nil {
		t.Fatal(err)
	}
	review, err := e.svc.ScoreAutoRun(t.Context(), e.run.RunID, pid, 1)
	if err != nil {
		t.Fatal(err)
	}
	if review.Band != autoreview.BandLow {
		t.Fatalf("a narration line without its audio file must be LOW: %+v", review)
	}
}

func mustExec(t *testing.T, st *store.Store, query string, args ...any) {
	t.Helper()
	if _, err := st.DB().ExecContext(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}
