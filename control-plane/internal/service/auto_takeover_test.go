package service_test

import (
	"errors"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/events"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
)

func watchAuto(env *autoEnv) *recordedEvents {
	bus := events.New()
	env.svc.SetBus(bus)
	rec := &recordedEvents{}
	bus.Subscribe("auto.changed", rec.add)
	return rec
}

// assertTakenOver checks the overlay is halted by the person, the decision
// and audit row exist, and auto.changed was announced.
func assertTakenOver(t *testing.T, env *autoEnv, rec *recordedEvents, runID, actor, via string) {
	t.Helper()
	v, err := env.svc.GetAuto(t.Context(), runID)
	if err != nil {
		t.Fatal(err)
	}
	if v.State != model.AutoStateHalted || v.HaltReason != model.AutoHaltHumanTakeover {
		t.Fatalf("%s: overlay = %s/%s, want halted/human_takeover", via, v.State, v.HaltReason)
	}
	n := 0
	for _, d := range v.Decisions {
		if d.Kind == model.AutoDecisionTakeover && d.Actor == actor {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%s: takeover decisions = %d", via, n)
	}
	if got := len(auditActions(t, env.svc)["auto.takeover"]); got != 1 {
		t.Fatalf("%s: takeover audit rows = %d", via, got)
	}
	if got := rec.named("auto.changed", 1); len(got) == 0 {
		t.Fatalf("%s: no auto.changed event", via)
	}
}

func TestTakeoverHumanSceneRejectHaltsAutoRun(t *testing.T) {
	env := newAutoEnv(t, false)
	rec := watchAuto(env)
	run, _ := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
	if _, err := env.svc.ConfirmScene(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "scn_1", "rejected", "bad"); err != nil {
		t.Fatal(err)
	}
	assertTakenOver(t, env, rec, run.RunID, "human:webui", "scene_reject")
}

func TestTakeoverHumanSceneConfirmIsNotATakeover(t *testing.T) {
	env := newAutoEnv(t, false)
	run, view := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
	if _, err := env.svc.ConfirmScene(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "scn_1", "confirmed", ""); err != nil {
		t.Fatal(err)
	}
	got, _ := env.svc.GetAuto(t.Context(), run.RunID)
	if got.State != model.AutoStateActive || got.AutoVersion != view.AutoVersion {
		t.Fatalf("a human confirmation took the run over: %+v", got)
	}
}

func TestTakeoverHumanCancelClosesAutoRun(t *testing.T) {
	env := newAutoEnv(t, false)
	rec := watchAuto(env)
	run, view := env.start(t, "")
	if _, err := env.svc.CancelWorkflow(t.Context(), "human:webui", run.RunID, "stop", run.Version); err != nil {
		t.Fatal(err)
	}
	got, _ := env.svc.GetAuto(t.Context(), run.RunID)
	if got.State != model.AutoStateCancelled || got.AutoVersion != view.AutoVersion+1 {
		t.Fatalf("overlay = %+v", got)
	}
	if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_decisions WHERE run_id=? AND kind='workflow_cancelled'`, run.RunID); n != 1 {
		t.Fatalf("cancel decisions = %d", n)
	}
	if len(rec.named("auto.changed", 1)) == 0 {
		t.Fatal("no auto.changed event")
	}
	// The asset is free for a new run: the cancelled overlay is not live.
	if _, _, err := env.svc.StartAutoRun(t.Context(), "human:webui", env.request(t, "again", "", false)); err != nil {
		t.Fatalf("a new auto run after cancel: %v", err)
	}
}

func TestTakeoverHumanRetryHaltsAutoRun(t *testing.T) {
	env := newAutoEnv(t, false)
	rec := watchAuto(env)
	run, _ := env.start(t, "")
	tk, err := env.svc.ClaimTask(t.Context(), "recognizer", "recognizer", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if err := env.svc.FailTask(t.Context(), "recognizer", tk.TaskID, "boom"); err != nil {
		t.Fatal(err)
	}
	failed, _, _ := env.svc.GetWorkflow(t.Context(), run.RunID)
	if failed.Status != model.WorkflowFailed {
		t.Fatalf("run = %+v", failed)
	}
	if _, err := env.svc.RetryWorkflow(t.Context(), "human:webui", run.RunID, "recognize", "retry-1", failed.Version); err != nil {
		t.Fatal(err)
	}
	assertTakenOver(t, env, rec, run.RunID, "human:webui", "retry")
}

func TestTakeoverHumanRevokeAndEditHaltAutoRun(t *testing.T) {
	for _, op := range []string{"revoke", "edit"} {
		t.Run(op, func(t *testing.T) {
			env := newAutoEnv(t, false)
			rec := watchAuto(env)
			run, _ := env.atDraftReview(t, "")
			var err error
			switch op {
			case "revoke":
				if _, err = env.svc.ApproveWorkflowNarration(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "nar_scn_1"); err != nil {
					t.Fatal(err)
				}
				// A human approval alone is not a takeover.
				if v, _ := env.svc.GetAuto(t.Context(), run.RunID); v.State != model.AutoStateActive {
					t.Fatalf("a human approval took the run over: %+v", v)
				}
				cur, _, _ := env.svc.GetWorkflow(t.Context(), run.RunID)
				_, err = env.svc.RevokeWorkflowNarration(t.Context(), "human:webui", run.RunID, "nar_scn_1", cur.Version)
			case "edit":
				_, err = env.svc.EditWorkflow(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "", "", nil, "nar_scn_1", "改过的解说", nil, nil, "")
			}
			if err != nil {
				t.Fatal(err)
			}
			assertTakenOver(t, env, rec, run.RunID, "human:webui", op)
		})
	}
}

func TestTakeoverReopenAnnouncesAutoChanged(t *testing.T) {
	env := newAutoEnv(t, false)
	rec := watchAuto(env)
	run, _ := env.start(t, "")
	st := storeOf(t, env.svc)
	a, _ := st.GetAsset(t.Context(), "clip")
	a.Status = model.AssetStatusExported
	a.Artifacts = map[string]string{"edl": "src/edl.json", "manifest": "src/delivery-manifest.json"}
	if err := st.UpdateAsset(t.Context(), a); err != nil {
		t.Fatal(err)
	}
	if _, err := env.svc.ReopenAsset(t.Context(), "human:webui", "clip"); err != nil {
		t.Fatal(err)
	}
	assertTakenOver(t, env, rec, run.RunID, "human:webui", "reopen")
}

func TestTakeoverHumanPreviewStartsOnlyWhenItCreatesAJob(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	// A rejected request (stale version) creates no job and takes nothing over.
	registerValidCapability(t, e.svc, "agent-exp-1")
	_, _, err := e.svc.StartPreview(t.Context(), "human:test", e.run.RunID, preview.StartRequest{
		RevisionID: e.rev.RevisionID, ExpectedVersion: e.run.Version + 9, PresetID: preview.PresetID, RequestID: "req-stale"})
	if err == nil {
		t.Fatal("stale preview start was accepted")
	}
	if a := e.auto(t); a.State != model.AutoStateActive || a.AutoVersion != 1 {
		t.Fatalf("a refused start took the run over: %+v", a)
	}
	e.svc.SetMediaProber(&testProber{media: preview.Media{Width: 1280, Height: 720, FPS: 30, FrameCount: 30, DurationSeconds: 1.0,
		VideoCodec: "h264", AudioCodec: "aac", SampleRate: 48000, Channels: 2}})
	if _, created, err := e.svc.StartPreview(t.Context(), "human:test", e.run.RunID, preview.StartRequest{
		RevisionID: e.rev.RevisionID, ExpectedVersion: e.run.Version, PresetID: preview.PresetID, RequestID: "req-human"}); err != nil || !created {
		t.Fatalf("human preview: created=%v err=%v", created, err)
	}
	a := e.auto(t)
	if a.State != model.AutoStateHalted || a.HaltReason != model.AutoHaltHumanTakeover || a.AutoVersion != 2 {
		t.Fatalf("overlay = %+v", a)
	}
	if got := e.events.named("auto.changed", 1); len(got) == 0 {
		t.Fatal("no auto.changed event")
	}
}

func TestAutoActorCannotStartPreviewThroughThePublicMethod(t *testing.T) {
	e := newScoreEnv(t, 0.9, "团战")
	registerValidCapability(t, e.svc, "agent-exp-1")
	_, _, err := e.svc.StartPreview(t.Context(), model.AutoActor, e.run.RunID, preview.StartRequest{
		RevisionID: e.rev.RevisionID, ExpectedVersion: e.run.Version, PresetID: preview.PresetID, RequestID: "req-auto"})
	if !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("auto actor preview: %v", err)
	}
}

// Non-auto runs: every hooked human path behaves as before and leaves no
// auto rows behind.
func TestTakeoverHooksLeaveNonAutoRunsUnchanged(t *testing.T) {
	svc, root, run := workflowSetup(t)
	ctx := t.Context()
	gate := workflowDraftGate(t, svc, root)
	if _, err := svc.ApproveWorkflowNarration(ctx, "human:webui", gate.RunID, gate.Version, gate.CurrentRevisionID, "nar_scn_1"); err != nil {
		t.Fatal(err)
	}
	cur, _, _ := svc.GetWorkflow(ctx, run.RunID)
	revoked, err := svc.RevokeWorkflowNarration(ctx, "human:webui", run.RunID, "nar_scn_1", cur.Version)
	if err != nil || revoked.Stage != "draft_review" || revoked.Status != model.WorkflowAwaitingReview {
		t.Fatalf("revoke: %+v %v", revoked, err)
	}
	edited, err := svc.EditWorkflow(ctx, "human:webui", run.RunID, revoked.Version, revoked.CurrentRevisionID, "", "", nil, "nar_scn_1", "修改后的解说", nil, nil, "")
	if err != nil || edited.Status != model.WorkflowAwaitingReview {
		t.Fatalf("edit: %+v %v", edited, err)
	}
	cancelled, err := svc.CancelWorkflow(ctx, "human:webui", run.RunID, "done", edited.Version)
	if err != nil || cancelled.Status != model.WorkflowCancelled {
		t.Fatalf("cancel: %+v %v", cancelled, err)
	}
	for _, table := range []string{"workflow_auto", "auto_decisions", "auto_actions"} {
		if n := count(t, svc, `SELECT COUNT(*) FROM `+table); n != 0 {
			t.Fatalf("%s has %d rows for a run that never was auto", table, n)
		}
	}
	if len(auditActions(t, svc)["auto.takeover"])+len(auditActions(t, svc)["auto.cancel"]) != 0 {
		t.Fatal("auto audit rows for a non-auto run")
	}
}

func TestTakeoverRejectedSceneOnNonAutoRunStillWorks(t *testing.T) {
	svc, root, _ := workflowSetup(t)
	run := workflowDeliver(t, svc, root, "recognizer", "recognized")
	got, err := svc.ConfirmScene(t.Context(), "human:webui", run.RunID, run.Version, run.CurrentRevisionID, "scn_1", "rejected", "bad")
	if err != nil || got.Version != run.Version+1 {
		t.Fatalf("reject: %+v %v", got, err)
	}
	if n := count(t, svc, `SELECT COUNT(*) FROM auto_decisions`); n != 0 {
		t.Fatalf("auto_decisions = %d", n)
	}
}
