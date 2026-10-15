package service_test

import "testing"

// A ledger row exists only if its action committed. After a crash between the
// commit and the caller learning of it, the caller repeats the call with the
// versions it started with; by then the run has advanced, the auto version has
// moved and the auto run may even be revoked. The repeat must still report
// success and must not do or count anything again.
func TestAutoConfirmSceneReplayAfterCommitIsSuccess(t *testing.T) {
	env := newAutoEnv(t, false)
	ctx := t.Context()
	run, view := env.atSceneReview(t, "", [2]float64{0.9, 0.8})
	first, err := env.svc.AutoConfirmScene(ctx, run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if first.Stage == "scene_review" {
		t.Fatalf("run did not advance: %+v", first)
	}
	check := func(name string) {
		t.Helper()
		got, err := env.svc.AutoConfirmScene(ctx, run.RunID, run.Version, view.AutoVersion, run.CurrentRevisionID)
		if err != nil {
			t.Fatalf("%s: replay of a committed confirmation failed: %v", name, err)
		}
		if got.RunID != run.RunID || got.Version != first.Version {
			t.Fatalf("%s: replay returned %+v, want the run as the first call left it", name, got)
		}
		if n := count(t, env.svc, `SELECT COUNT(*) FROM auto_actions WHERE run_id=? AND kind='scene_confirm'`, run.RunID); n != 1 {
			t.Fatalf("%s: ledger rows = %d", name, n)
		}
		if a, _ := env.svc.GetAuto(ctx, run.RunID); a.ActionsUsed != 1 {
			t.Fatalf("%s: actions_used = %d, a replay must not count", name, a.ActionsUsed)
		}
	}
	check("state advanced")
	now, _ := env.svc.GetAuto(ctx, run.RunID)
	if now.AutoVersion != view.AutoVersion+1 {
		t.Fatalf("auto_version = %d", now.AutoVersion)
	}
	if _, err := env.svc.RevokeAuto(ctx, "human:webui", run.RunID, "stop", now.AutoVersion, false); err != nil {
		t.Fatal(err)
	}
	check("revoked")
}
