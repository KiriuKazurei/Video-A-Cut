package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// autoFixture creates one asset, one revision and one workflow run on it, the
// minimum the auto-run overlay tables reference.
func autoFixture(t *testing.T, s *store.Store, assetID, runID string) model.WorkflowRun {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateAsset(ctx, model.Asset{AssetID: assetID, Status: model.AssetStatusIngested, AgentVisible: true}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	rev := model.Revision{RevisionID: "rev-" + runID, AssetID: assetID, SchemaVersion: 1, PackageRef: "p/" + runID,
		EDLSHA256: "e", EvidenceManifestSHA256: "m", CreatedAt: now, CreatedBy: "human:test", Reason: "initial"}
	if err := s.InsertRevision(ctx, rev); err != nil {
		t.Fatal(err)
	}
	run := model.WorkflowRun{RunID: runID, AssetID: assetID, BaseRevisionID: rev.RevisionID, CurrentRevisionID: rev.RevisionID,
		Status: model.WorkflowAwaitingReview, Stage: "scene_review", Version: 1, ContentMode: "configured", CreatedAt: now, UpdatedAt: now}
	if err := s.InsertWorkflow(ctx, run); err != nil {
		t.Fatal(err)
	}
	return run
}

func autoRow(runID string, now time.Time) model.WorkflowAuto {
	return model.WorkflowAuto{
		RunID: runID, AutoVersion: 1, State: model.AutoStateActive, Brief: "只保留团战", BriefSHA256: "b",
		AuthorizedBy: "human:webui", AuthorizedAt: now, NoticeSHA256: "n",
		Scope:     model.AutoScope{MaxRounds: 1, MaxTotalRounds: 5, MaxActions: 10, Thresholds: map[string]float64{"scene_confidence_min": 0.6}},
		ProfileID: "p", ProfileRevision: 1, ProfileSHA256: "s", MaxRounds: 1, MaxTotalRounds: 5,
		CreatedAt: now, UpdatedAt: now, DeadlineAt: now.Add(time.Hour),
	}
}

func TestMigration0017IsIdempotentOnExistingDatabase(t *testing.T) {
	path := t.TempDir() + "/vac.db"
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	run := autoFixture(t, s, "a1", "r1")
	if err := s.InsertWorkflowAuto(context.Background(), autoRow(run.RunID, time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	again, err := store.Open(path)
	if err != nil {
		t.Fatalf("reopen applies migrations again: %v", err)
	}
	defer func() { _ = again.Close() }()
	if _, err := again.GetWorkflowAuto(context.Background(), "r1"); err != nil {
		t.Fatalf("overlay row lost on reopen: %v", err)
	}
	for _, table := range []string{"workflow_auto", "workflow_auto_rounds", "auto_reviews", "auto_actions", "auto_decisions"} {
		var n string
		if err := again.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&n); err != nil {
			t.Fatalf("table %s: %v", table, err)
		}
	}
	var idx string
	if err := again.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_workflow_auto_state'`).Scan(&idx); err != nil {
		t.Fatalf("state index: %v", err)
	}
	// The one-active-run index of workflow_runs is untouched.
	if err := again.DB().QueryRow(`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_workflow_one_active'`).Scan(&idx); err != nil {
		t.Fatalf("workflow_runs active index changed: %v", err)
	}
}

func TestWorkflowAutoRoundTripAndCompareAndSwap(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	run := autoFixture(t, s, "a1", "r1")
	now := time.Now().UTC().Truncate(time.Second)
	row := autoRow(run.RunID, now)
	if err := s.InsertWorkflowAuto(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWorkflowAuto(ctx, "r1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Brief != row.Brief || got.Scope.Thresholds["scene_confidence_min"] != 0.6 || got.Scope.MaxActions != 10 || got.State != model.AutoStateActive {
		t.Fatalf("round trip: %+v", got)
	}
	if _, err := s.GetWorkflowAuto(ctx, "missing"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("missing auto row: %v", err)
	}
	next := got
	next.State, next.AutoVersion = model.AutoStateRevoked, got.AutoVersion+1
	if err := s.UpdateWorkflowAuto(ctx, next); err != nil {
		t.Fatal(err)
	}
	stale := got
	stale.State, stale.AutoVersion = model.AutoStateHalted, got.AutoVersion+1
	if err := s.UpdateWorkflowAuto(ctx, stale); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale update must lose the compare-and-swap: %v", err)
	}
	final, _ := s.GetWorkflowAuto(ctx, "r1")
	if final.State != model.AutoStateRevoked || final.AutoVersion != 2 {
		t.Fatalf("stale write changed the row: %+v", final)
	}
}

func TestActiveAutoForAssetJoinsRunAsset(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	r1 := autoFixture(t, s, "a1", "r1")
	autoFixture(t, s, "a2", "r2")
	if _, live, err := s.ActiveAutoForAsset(ctx, "a1"); err != nil || live {
		t.Fatalf("no overlay yet: live=%v err=%v", live, err)
	}
	if err := s.InsertWorkflowAuto(ctx, autoRow(r1.RunID, now)); err != nil {
		t.Fatal(err)
	}
	if id, live, err := s.ActiveAutoForAsset(ctx, "a1"); err != nil || !live || id != "r1" {
		t.Fatalf("a1: %q %v %v", id, live, err)
	}
	if _, live, _ := s.ActiveAutoForAsset(ctx, "a2"); live {
		t.Fatal("another asset's run must not count")
	}
	for state, want := range map[string]bool{
		model.AutoStateAwaitingDecision: true, model.AutoStateCompleted: false, model.AutoStateHalted: false,
		model.AutoStateRevoked: false, model.AutoStateCancelled: false,
	} {
		row, _ := s.GetWorkflowAuto(ctx, "r1")
		row.State, row.AutoVersion = state, row.AutoVersion+1
		if err := s.UpdateWorkflowAuto(ctx, row); err != nil {
			t.Fatal(err)
		}
		if _, live, _ := s.ActiveAutoForAsset(ctx, "a1"); live != want {
			t.Fatalf("state %s: live=%v want %v", state, live, want)
		}
	}
}

func TestAutoActionLedgerIsIdempotentAndCountsByKind(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	now := time.Now().UTC()
	autoFixture(t, s, "a1", "r1")
	if err := s.InsertWorkflowAuto(ctx, autoRow("r1", now)); err != nil {
		t.Fatal(err)
	}
	if first, err := s.InsertAutoAction(ctx, "r1", "k1", model.AutoActionSceneConfirm, "rev", now); err != nil || !first {
		t.Fatalf("first insert: %v %v", first, err)
	}
	if again, err := s.InsertAutoAction(ctx, "r1", "k1", model.AutoActionSceneConfirm, "rev", now); err != nil || again {
		t.Fatalf("a repeated key must not add a row: %v %v", again, err)
	}
	if _, err := s.InsertAutoAction(ctx, "r1", "k2", "retry", "narrate", now); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.AutoActionExists(ctx, "r1", "k1"); !ok {
		t.Fatal("ledger row missing")
	}
	if n, err := s.CountAutoActions(ctx, "r1"); err != nil || n != 2 {
		t.Fatalf("count = %d, %v", n, err)
	}
}

func TestUpsertSceneReviewAutoNeverOverwritesAHuman(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	run := autoFixture(t, s, "a1", "r1")
	now := time.Now().UTC().Truncate(time.Second)
	row := func(scene, actor, decision string) model.SceneReview {
		return model.SceneReview{RunID: run.RunID, RevisionID: run.CurrentRevisionID, SceneID: scene, Decision: decision,
			EvidenceSHA256: "ev", Actor: actor, CreatedAt: now}
	}
	if err := s.UpsertSceneReview(ctx, row("human", "human:webui", "rejected")); err != nil {
		t.Fatal(err)
	}
	if applied, err := s.UpsertSceneReviewAuto(ctx, row("human", model.AutoActor, "confirmed")); err != nil || applied {
		t.Fatalf("auto overwrote a human decision: applied=%v err=%v", applied, err)
	}
	if applied, err := s.UpsertSceneReviewAuto(ctx, row("fresh", model.AutoActor, "confirmed")); err != nil || !applied {
		t.Fatalf("new row: %v %v", applied, err)
	}
	if applied, err := s.UpsertSceneReviewAuto(ctx, row("fresh", model.AutoActor, "confirmed")); err != nil || !applied {
		t.Fatalf("auto may refresh its own row: %v %v", applied, err)
	}
	reviews, err := s.ListSceneReviews(ctx, run.RunID, run.CurrentRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]model.SceneReview{}
	for _, r := range reviews {
		got[r.SceneID] = r
	}
	if got["human"].Actor != "human:webui" || got["human"].Decision != "rejected" || got["fresh"].Actor != model.AutoActor {
		t.Fatalf("rows = %+v", got)
	}
}

func TestAutoReviewInsertAndLookupByPreview(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	run := autoFixture(t, s, "a1", "r1")
	now := time.Now().UTC().Truncate(time.Second)
	review := func(id string, round int, preview string, at time.Time) model.AutoReview {
		return model.AutoReview{ReviewID: id, RunID: run.RunID, Round: round, RevisionID: run.CurrentRevisionID, PreviewID: preview,
			ScorerVersion: "v1", Score: 0.9, Band: "high", Reasons: []byte(`[]`), CreatedAt: at}
	}
	if _, err := s.GetAutoReviewForPreview(ctx, run.RunID, 0, "p1"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("before insert: %v", err)
	}
	if err := s.InsertAutoReview(ctx, review("arv_1", 0, "p1", now)); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertAutoReview(ctx, review("arv_1", 0, "p1", now)); err == nil {
		t.Fatal("a review id is unique")
	}
	if err := s.InsertAutoReview(ctx, review("arv_2", 1, "p1", now.Add(time.Second))); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAutoReviewForPreview(ctx, run.RunID, 0, "p1")
	if err != nil || got.ReviewID != "arv_1" || string(got.Reasons) != "[]" || string(got.Inputs) != "{}" || string(got.Thresholds) != "{}" || got.Band != "high" {
		t.Fatalf("got %+v %v", got, err)
	}
	if _, err := s.GetAutoReviewForPreview(ctx, run.RunID, 0, "other"); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("other preview: %v", err)
	}
	list, err := s.ListAutoReviews(ctx, run.RunID)
	if err != nil || len(list) != 2 || list[0].ReviewID != "arv_1" || list[1].ReviewID != "arv_2" {
		t.Fatalf("list %+v %v", list, err)
	}
}

// The narration upsert for the auto-reviewer is conditional in SQL, not only in
// the service: a person's approval row is never replaced by it, whatever the
// hash, and the auto-reviewer's own row may be refreshed.
func TestInsertNarrationApprovalAutoNeverOverwritesAHuman(t *testing.T) {
	s := mustOpen(t)
	ctx := context.Background()
	run := autoFixture(t, s, "a2", "r2")
	now := time.Now().UTC().Truncate(time.Second)
	rows := func() map[string]store.NarrationApprovalRow {
		t.Helper()
		got, err := s.ListNarrationApprovalRows(ctx, run.RunID, run.CurrentRevisionID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if err := s.InsertNarrationApproval(ctx, run.RunID, run.CurrentRevisionID, "human", "h1", "human:webui", now); err != nil {
		t.Fatal(err)
	}
	for _, hash := range []string{"h1", "other"} { // the same text and different text
		if applied, err := s.InsertNarrationApprovalAuto(ctx, run.RunID, run.CurrentRevisionID, "human", hash, now); err != nil || applied {
			t.Fatalf("auto replaced a person's approval (hash %s): applied=%v err=%v", hash, applied, err)
		}
	}
	if got := rows()["human"]; got.Actor != "human:webui" || got.Hash != "h1" {
		t.Fatalf("the person's row changed: %+v", got)
	}
	if applied, err := s.InsertNarrationApprovalAuto(ctx, run.RunID, run.CurrentRevisionID, "fresh", "f1", now); err != nil || !applied {
		t.Fatalf("new row: applied=%v err=%v", applied, err)
	}
	if applied, err := s.InsertNarrationApprovalAuto(ctx, run.RunID, run.CurrentRevisionID, "fresh", "f2", now); err != nil || !applied {
		t.Fatalf("auto may refresh its own row: applied=%v err=%v", applied, err)
	}
	if got := rows()["fresh"]; got.Actor != model.AutoActor || got.Hash != "f2" {
		t.Fatalf("refreshed row = %+v", got)
	}
	// A person may still overwrite the auto-reviewer's row: the person wins.
	if err := s.InsertNarrationApproval(ctx, run.RunID, run.CurrentRevisionID, "fresh", "f3", "human:webui", now); err != nil {
		t.Fatal(err)
	}
	if applied, err := s.InsertNarrationApprovalAuto(ctx, run.RunID, run.CurrentRevisionID, "fresh", "f4", now); err != nil || applied {
		t.Fatalf("auto replaced a person's overwrite: applied=%v err=%v", applied, err)
	}
	if got := rows()["fresh"]; got.Actor != "human:webui" || got.Hash != "f3" {
		t.Fatalf("row = %+v", got)
	}
}
