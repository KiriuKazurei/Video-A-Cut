package service_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/service"
)

func scriptFixture(t *testing.T) (*service.Service, model.Script, []service.ScriptMedia) {
	t.Helper()
	svc := newService(t)
	root := t.TempDir()
	for _, id := range []string{"clip", "hidden"} {
		writeWorkflowFixture(t, root, id)
		if err := os.WriteFile(filepath.Join(root, id, "a.mp4"), []byte("media path fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := svc.CreateAsset(t.Context(), model.Asset{AssetID: id, Status: model.AssetStatusIngested, AgentVisible: id == "clip", AllowedAgents: []string{"recognizer", "narrator", "exporter"}, Artifacts: map[string]string{"edl": id + "/edl.json"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.ConfigureDeliveryRoot(root); err != nil {
		t.Fatal(err)
	}
	confirmVisible(t, svc)
	sc, err := svc.EnterScript(t.Context(), "human:test", "clip")
	if err != nil {
		t.Fatal(err)
	}
	media, err := svc.ScriptMediaCatalogue(t.Context())
	if err != nil || len(media) != 1 {
		t.Fatalf("catalogue: %+v %v", media, err)
	}
	return svc, sc, media
}

func TestScriptWorkbenchManualRevisionsAndStaleWrites(t *testing.T) {
	svc, sc, media := scriptFixture(t)
	ctx := t.Context()
	public, _ := json.Marshal(media)
	if strings.Contains(string(public), "path") || strings.Contains(string(public), "hidden") {
		t.Fatalf("private media leaked: %s", public)
	}
	req := service.SaveScriptRequest{VisibleSetSHA: sc.VisibleSetSHA, Title: "抽卡介绍", Body: "{@a.mp4}介绍抽卡机制", Segments: []service.ScriptSegment{{Index: 0, MediaID: media[0].ID, Start: 0, End: 1}}}
	w, err := svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, req)
	if err != nil || w.Version != 1 {
		t.Fatalf("partial manual save: %+v %v", w, err)
	}
	if _, err = svc.FinalizeScriptWorkbench(ctx, "human:test", sc.ScriptID, 1, sc.VisibleSetSHA); err == nil {
		t.Fatal("unfinished narration was finalized")
	}
	if _, err = svc.FinalizeScript(ctx, "human:test", sc.ScriptID); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("legacy finalize bypass: %v", err)
	}
	if _, err = svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, req); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale save: %v", err)
	}
	req.ExpectedVersion = 1
	req.Segments[0].Narration = "抽卡通常消耗游戏资源，随机获得角色或道具。"
	w, err = svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, req)
	if err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{"agent:writer", "system:auto-review"} {
		if _, err = svc.FinalizeScriptWorkbench(ctx, actor, sc.ScriptID, w.Version, sc.VisibleSetSHA); !errors.Is(err, model.ErrForbidden) {
			t.Fatalf("agent finalize: %v", err)
		}
	}
	w, err = svc.FinalizeScriptWorkbench(ctx, "human:test", sc.ScriptID, w.Version, sc.VisibleSetSHA)
	if err != nil || w.Script.Status != model.ScriptFinalized || w.Script.FinalizedBy != "human:test" {
		t.Fatalf("finalize: %+v %v", w, err)
	}
	revs, err := svc.ScriptRevisions(ctx, sc.ScriptID)
	if err != nil || len(revs) != 4 || revs[0].Action != "finalize" || !strings.Contains(revs[1].Snapshot, "抽卡通常") {
		t.Fatalf("snapshots: %+v %v", revs, err)
	}
	req.ExpectedVersion = w.Version
	req.Body = "{@a.mp4}{\"新字幕\", 字号:24px,}介绍抽卡机制"
	w, err = svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, req)
	if err != nil || w.Script.Status != model.ScriptNeedsRefinalize || w.Script.FinalizedAt != nil {
		t.Fatalf("edit finalized: %+v %v", w, err)
	}
	if _, err = svc.ScriptDeliveryPath(ctx, sc.ScriptID, ""); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("old delivery readable: %v", err)
	}
}

func TestScriptWorkbenchVisibilitySyntaxAndForcedSelection(t *testing.T) {
	svc, sc, media := scriptFixture(t)
	ctx := t.Context()
	for _, body := range []string{"{未知效果}", "{@hidden.mp4}介绍抽卡", "{@a.mp4}介绍{黑屏}{白屏}"} {
		if _, err := svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, service.SaveScriptRequest{VisibleSetSHA: sc.VisibleSetSHA, Body: body}); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
	req := service.SaveScriptRequest{VisibleSetSHA: sc.VisibleSetSHA, Body: "介绍抽卡", Segments: []service.ScriptSegment{{Index: 0, Narration: "说明", MediaID: "forged", Start: 0, End: 1}}}
	if _, err := svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, req); !errors.Is(err, model.ErrArgument) {
		t.Fatalf("forged media: %v", err)
	}
	req.Segments[0].MediaID = media[0].ID
	req.Segments[0].End = 2
	if _, err := svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, req); !errors.Is(err, model.ErrArgument) {
		t.Fatalf("out of window: %v", err)
	}
	req.Segments = nil
	if _, err := svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, req); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BatchSetAgentVisible(ctx, "human:test", []string{"hidden"}, true); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ScriptMediaCatalogue(ctx); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("unconfirmed catalogue: %v", err)
	}
	confirmVisible(t, svc)
	if _, err := svc.GenerateScriptWorkbench(ctx, "human:test", sc.ScriptID, service.GenerateScriptRequest{ExpectedVersion: 1, VisibleSetSHA: sc.VisibleSetSHA, ProfileID: "nonexistent", ProfileRevision: 1}); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("old context reached model/profile: %v", err)
	}
	catalogue, err := svc.ScriptMediaCatalogue(ctx)
	if err != nil || len(catalogue) != 2 || catalogue[0].Name == catalogue[1].Name {
		t.Fatalf("duplicate filename aliases: %+v %v", catalogue, err)
	}
	req.Segments = nil
	req.VisibleSetSHA = sc.VisibleSetSHA
	if _, err = svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, req); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("old visible sha: %v", err)
	}
}

func TestScriptChangeParksAutoAndNeedsExactHumanRevision(t *testing.T) {
	t.Run("current", func(t *testing.T) { scriptChangeRoundTrip(t, false) })
	t.Run("legacy", func(t *testing.T) { scriptChangeRoundTrip(t, true) })
}

func scriptChangeRoundTrip(t *testing.T, legacy bool) {
	env := newAutoEnv(t, false)
	ctx := t.Context()
	if err := os.WriteFile(filepath.Join(env.root, "src", "a.mp4"), []byte("media path fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	run, _ := env.start(t, "")
	if legacy {
		mustExec(t, storeOf(t, env.svc), `UPDATE workflow_auto SET scope_json=json_remove(scope_json, '$.visible_set_sha', '$.visible_asset_ids', '$.script_finalize') WHERE run_id=?`, run.RunID)
	}
	before, _ := json.Marshal(run)
	sc, err := env.svc.EnterScript(ctx, "human:test", "clip")
	if err != nil {
		t.Fatal(err)
	}
	media, err := env.svc.ScriptMediaCatalogue(ctx)
	if err != nil || len(media) != 1 {
		t.Fatalf("media %+v %v", media, err)
	}
	req := service.SaveScriptRequest{VisibleSetSHA: sc.VisibleSetSHA, Body: "介绍关卡", Segments: []service.ScriptSegment{{Index: 0, Narration: "进入这个关卡。", MediaID: media[0].ID, Start: 0, End: 1}}}
	w, err := env.svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, req)
	if err != nil {
		t.Fatal(err)
	}
	a, err := env.svc.GetAuto(ctx, run.RunID)
	if err != nil || a.State != model.AutoStateAwaitingDecision || a.HaltReason != "script_changed" || !a.DeliveryHold || a.Decision.CanRevise {
		t.Fatalf("park: %+v %v", a, err)
	}
	proof := func(v int) string {
		raw, _ := json.Marshal(map[string]any{"script_id": sc.ScriptID, "script_version": v})
		return string(raw)
	}
	if _, _, err = env.svc.DecideAuto(ctx, "human:test", run.RunID, "revise", "重新剪辑", a.AutoVersion, "bypass"); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("revise bypass: %v", err)
	}
	if _, _, err = env.svc.DecideAuto(ctx, "human:test", run.RunID, "confirm_script", proof(w.Version), a.AutoVersion, "early"); !errors.Is(err, model.ErrInvalidState) {
		t.Fatalf("unfinalized resume: %v", err)
	}
	w, err = env.svc.FinalizeScriptWorkbench(ctx, "human:test", sc.ScriptID, w.Version, sc.VisibleSetSHA)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = env.svc.DecideAuto(ctx, "agent:writer", run.RunID, "confirm_script", proof(w.Version), a.AutoVersion, "agent"); !errors.Is(err, model.ErrForbidden) {
		t.Fatalf("agent resume: %v", err)
	}
	if _, _, err = env.svc.DecideAuto(ctx, "human:test", run.RunID, "confirm_script", proof(w.Version-1), a.AutoVersion, "stale"); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale revision resume: %v", err)
	}
	_, a, err = env.svc.DecideAuto(ctx, "human:test", run.RunID, "confirm_script", proof(w.Version), a.AutoVersion, "resume")
	if err != nil || a.State != model.AutoStateActive || a.Scope.ScriptID != sc.ScriptID || a.Scope.ScriptVersion != w.Version || a.Scope.VisibleSetSHA != sc.VisibleSetSHA || !a.Scope.ScriptFinalize {
		t.Fatalf("resume: %+v %v", a, err)
	}
	confirmed := false
	for _, d := range a.Decisions {
		if d.Kind != "script_confirmed" {
			continue
		}
		var receipt struct {
			ScriptID string `json:"script_id"`
			Version  int    `json:"script_version"`
			SHA      string `json:"visible_set_sha"`
		}
		if err = json.Unmarshal(d.Basis, &receipt); err != nil || d.Target != sc.ScriptID || receipt.ScriptID != sc.ScriptID || receipt.Version != w.Version || receipt.SHA != sc.VisibleSetSHA || d.Actor != "human:test" {
			t.Fatalf("confirmation audit lost its scope: %+v %v", receipt, err)
		}
		confirmed = true
	}
	if !confirmed {
		t.Fatal("script confirmation decision missing")
	}
	same, _, err := env.svc.GetWorkflow(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(same)
	if string(before) != string(after) {
		t.Fatalf("script edited workflow JSON: %s => %s", before, after)
	}
	task, err := env.svc.ClaimTask(ctx, "recognizer", "recognizer", time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	input, err := env.svc.GetTaskInput(ctx, "recognizer", "recognizer", task.TaskID)
	if err != nil || input.Script == nil || input.Script.Version != w.Version || input.Script.Workbench.Segments[0].Narration != "进入这个关卡。" || input.Script.Sources[media[0].ID] == "" {
		t.Fatalf("legacy worker lost fixed script: %+v %v", input.Script, err)
	}
	req.ExpectedVersion = w.Version
	if _, err = env.svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, req); err != nil {
		t.Fatal(err)
	}
	a, err = env.svc.GetAuto(ctx, run.RunID)
	if err != nil || a.State != model.AutoStateAwaitingDecision {
		t.Fatalf("second edit did not park: %+v %v", a, err)
	}
	if _, err = env.svc.GetTaskInput(ctx, "recognizer", "recognizer", task.TaskID); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale script still delivered to worker: %v", err)
	}
}

func TestAutoStartedAfterAnExistingDraftNeedsScriptConfirmation(t *testing.T) {
	env := newAutoEnv(t, false)
	ctx := t.Context()
	confirmVisible(t, env.svc)
	sc, err := env.svc.EnterScript(ctx, "human:test", "clip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = env.svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, service.SaveScriptRequest{VisibleSetSHA: sc.VisibleSetSHA, Body: "介绍关卡"}); err != nil {
		t.Fatal(err)
	}
	run, _ := env.start(t, "")
	if err = env.svc.AutoTick(ctx); err != nil {
		t.Fatal(err)
	}
	a, err := env.svc.GetAuto(ctx, run.RunID)
	if err != nil || a.State != model.AutoStateAwaitingDecision || a.HaltReason != "script_changed" || a.ActionsUsed != 0 {
		t.Fatalf("draft bypassed: state=%s reason=%s actions=%d err=%v", a.State, a.HaltReason, a.ActionsUsed, err)
	}
}

func TestScriptWorkbenchReadsOneRevisionDuringConcurrentSaves(t *testing.T) {
	svc, sc, _ := scriptFixture(t)
	done := make(chan error, 1)
	go func() {
		for i := 1; i <= 30; i++ {
			_, err := svc.SaveScriptWorkbench(t.Context(), "human:test", sc.ScriptID, service.SaveScriptRequest{ExpectedVersion: i - 1, VisibleSetSHA: sc.VisibleSetSHA, Title: fmt.Sprint(i), Body: "介绍通用原理"})
			if err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for {
		w, err := svc.GetScriptWorkbench(t.Context(), sc.ScriptID)
		if err != nil {
			t.Fatal(err)
		}
		if w.Version > 0 && w.Script.Title != fmt.Sprint(w.Version) {
			t.Fatalf("mixed script/content versions: title=%s version=%d", w.Script.Title, w.Version)
		}
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			return
		default:
		}
	}
}

func TestScriptMediaIDsDoNotRebindToAnotherPackageWithTheSameFilename(t *testing.T) {
	svc, sc, media := scriptFixture(t)
	ctx := t.Context()
	root := filepath.Dir(filepath.Dir(media[0].Path))
	writeWorkflowFixture(t, root, "replacement")
	if err := os.WriteFile(filepath.Join(root, "replacement", "a.mp4"), []byte("replacement video"), 0600); err != nil {
		t.Fatal(err)
	}
	a, err := storeOf(t, svc).GetAsset(ctx, "clip")
	if err != nil {
		t.Fatal(err)
	}
	a.Artifacts["edl"] = "replacement/edl.json"
	if err = storeOf(t, svc).UpdateAsset(ctx, a); err != nil {
		t.Fatal(err)
	}
	next, err := svc.ScriptMediaCatalogue(ctx)
	if err != nil || len(next) != 1 || next[0].Name != media[0].Name || next[0].ID == media[0].ID {
		t.Fatalf("source was silently rebound: %+v %v", next, err)
	}
	_, err = svc.SaveScriptWorkbench(ctx, "human:test", sc.ScriptID, service.SaveScriptRequest{VisibleSetSHA: sc.VisibleSetSHA, Body: "{@a.mp4}说明", Segments: []service.ScriptSegment{{Index: 0, Narration: "说明", MediaID: media[0].ID, Start: 0, End: 1}}})
	if !errors.Is(err, model.ErrArgument) {
		t.Fatalf("accepted obsolete media id: %v", err)
	}
}
