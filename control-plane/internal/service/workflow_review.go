package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// ReviewView is what the WebUI renders. Paths are package-relative, never absolute.
type ReviewView struct {
	Run      model.WorkflowRun   `json:"run"`
	Revision model.Revision      `json:"revision"`
	Scenes   []map[string]any    `json:"scenes"`
	Lines    []map[string]any    `json:"narration"`
	Files    []map[string]string `json:"evidence_files"`
}

func (s *Service) Review(ctx context.Context, runID string) (ReviewView, error) {
	run, err := s.st.GetWorkflow(ctx, runID)
	if err != nil {
		return ReviewView{}, err
	}
	rev, err := s.st.GetRevision(ctx, run.CurrentRevisionID)
	if err != nil {
		return ReviewView{}, err
	}
	edl, err := s.readRevision(rev)
	if err != nil {
		return ReviewView{}, err
	}
	reviews, _ := s.st.ListSceneReviews(ctx, run.RunID, rev.RevisionID)
	approved, _ := s.st.ListNarrationApprovalIDs(ctx, run.RunID, rev.RevisionID)
	byScene := map[string]model.SceneReview{}
	for _, revw := range reviews {
		byScene[revw.SceneID] = revw
	}
	files, err := s.evidenceFiles(rev.PackageRef, edl)
	if err != nil && len(asMapSlice(edl["scenes"])) > 0 {
		return ReviewView{}, err
	}
	for _, scene := range asMapSlice(edl["scenes"]) {
		id, _ := scene["scene_id"].(string)
		if row, ok := byScene[id]; ok {
			scene["decision"] = row.Decision
			scene["review_note"] = row.Note
		} else {
			scene["decision"] = ""
		}

	}
	for _, line := range asMapSlice(edl["narration"]) {
		id, _ := line["id"].(string)
		if hash, ok := approved[id]; ok {
			line["approved_hash"] = hash
		}
	}
	scenes, lines := asMapSlice(edl["scenes"]), asMapSlice(edl["narration"])
	if scenes == nil {
		scenes = []map[string]any{}
	}
	if lines == nil {
		lines = []map[string]any{}
	}
	if files == nil {
		files = []map[string]string{}
	}
	return ReviewView{Run: run, Revision: rev, Scenes: scenes, Lines: lines, Files: files}, nil
}

func (s *Service) ConfirmScene(ctx context.Context, actor, runID string, version int, revisionID, sceneID, decision, note string) (model.WorkflowRun, error) {
	if actor == "" || (decision != "confirmed" && decision != "rejected") {
		return model.WorkflowRun{}, fmt.Errorf("service: scene review: decision must be confirmed or rejected: %w", model.ErrArgument)
	}
	var saved model.WorkflowRun
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		run, rev, edl, err := s.lockRun(ctx, tx, runID, version, revisionID)
		if err != nil {
			return err
		}
		if err := s.ensureIdle(ctx, tx, run); err != nil {
			return err
		}
		var scene map[string]any
		for _, item := range asMapSlice(edl["scenes"]) {
			if item["scene_id"] == sceneID {
				scene = item
				break
			}
		}
		if scene == nil {
			return fmt.Errorf("service: scene %s is not in the revision: %w", sceneID, model.ErrNotFound)
		}
		evidence := ""
		if decision == "confirmed" {
			frames := stringList(scene["evidence_frames"])
			if len(frames) == 0 {
				return fmt.Errorf("service: scene %s has no evidence: %w", sceneID, model.ErrInvalidState)
			}
			if _, err := s.evidenceFiles(rev.PackageRef, edl); err != nil {
				return err
			}
			evidence = frames[0]
		}
		if err := tx.UpsertSceneReview(ctx, model.SceneReview{
			RunID: run.RunID, RevisionID: rev.RevisionID, SceneID: sceneID, Decision: decision,
			Note: note, EvidenceSHA256: evidence, Actor: actor, CreatedAt: time.Now().UTC(),
		}); err != nil {
			return err
		}
		if decision == "confirmed" {
			if err := s.openSortIfReady(ctx, tx, &run, edl); err != nil {
				return err
			}
		}
		run.Version++
		run.UpdatedAt = time.Now().UTC()
		if err := tx.UpdateWorkflow(ctx, run); err != nil {
			return err
		}
		saved = run
		return nil
	})
	if err != nil {
		return model.WorkflowRun{}, err
	}
	s.publish("review.changed", saved)
	s.audit(ctx, actor, "scene.review", runID, sceneID+" "+decision)
	return saved, nil
}

func (s *Service) openSortIfReady(ctx context.Context, tx *store.Store, run *model.WorkflowRun, edl map[string]any) error {
	scenes := asMapSlice(edl["scenes"])
	if len(scenes) == 0 || run.Stage != "scene_review" {
		return nil
	}
	reviews, err := tx.ListSceneReviews(ctx, run.RunID, run.CurrentRevisionID)
	if err != nil {
		return err
	}
	confirmed := map[string]bool{}
	for _, rev := range reviews {
		if rev.Decision == "confirmed" {
			confirmed[rev.SceneID] = true
		}
	}
	for _, scene := range scenes {
		id, _ := scene["scene_id"].(string)
		if id == "" || !confirmed[id] {
			return nil
		}
	}
	run.Status = model.WorkflowRunning
	run.Stage = model.TaskTypeSort
	run.BlockedReason = ""
	return s.queueStage(ctx, tx, *run, model.TaskTypeSort, run.CurrentRevisionID, "")
}

func (s *Service) ApproveWorkflowNarration(ctx context.Context, actor, runID string, version int, revisionID, narrationID string) (model.WorkflowRun, error) {
	if actor == "" || narrationID == "" {
		return model.WorkflowRun{}, fmt.Errorf("service: narration review: narration_id is required: %w", model.ErrArgument)
	}
	var saved model.WorkflowRun
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		run, rev, edl, err := s.lockRun(ctx, tx, runID, version, revisionID)
		if err != nil {
			return err
		}
		line := findNarration(edl, narrationID)
		if run.Stage != "draft_review" || run.Status != model.WorkflowAwaitingReview {
			return fmt.Errorf("workflow is not at draft review: %w", model.ErrInvalidState)
		}
		if err := validateWorkflowEdit(edl); err != nil {
			return err
		}
		if line == nil {
			return fmt.Errorf("service: narration %s is not in the revision: %w", narrationID, model.ErrNotFound)
		}
		start, ok1 := number(line["start"])
		end, ok2 := number(line["end"])
		text, _ := line["text"].(string)
		if !ok1 || !ok2 {
			return fmt.Errorf("service: narration window is invalid: %w", model.ErrInvalidState)
		}
		hash := NarrationDraftHash(text, start, end, narrationSource(line))
		if err := tx.InsertNarrationApproval(ctx, run.RunID, rev.RevisionID, narrationID, hash, actor, time.Now().UTC()); err != nil {
			return err
		}
		if err := s.openTTSIfReady(ctx, tx, &run, edl); err != nil {
			return err
		}
		run.Version++
		run.UpdatedAt = time.Now().UTC()
		if err := tx.UpdateWorkflow(ctx, run); err != nil {
			return err
		}
		saved = run
		return nil
	})
	if err != nil {
		return model.WorkflowRun{}, err
	}
	s.publish("review.changed", saved)
	s.audit(ctx, actor, "narration.approve", runID, narrationID)
	return saved, nil
}

func (s *Service) openTTSIfReady(ctx context.Context, tx *store.Store, run *model.WorkflowRun, edl map[string]any) error {
	lines := asMapSlice(edl["narration"])
	if len(lines) == 0 || run.Stage != "draft_review" {
		return nil
	}
	approved, err := tx.ListNarrationApprovalIDs(ctx, run.RunID, run.CurrentRevisionID)
	if err != nil {
		return err
	}
	for _, line := range lines {
		id, _ := line["id"].(string)
		start, ok1 := number(line["start"])
		end, ok2 := number(line["end"])
		text, _ := line["text"].(string)
		if !ok1 || !ok2 || approved[id] != NarrationDraftHash(text, start, end, narrationSource(line)) {
			return nil
		}
	}
	run.Status = model.WorkflowRunning
	run.Stage = model.TaskTypeTTS
	run.BlockedReason = ""
	return s.queueStage(ctx, tx, *run, model.TaskTypeTTS, run.CurrentRevisionID, "")
}

func (s *Service) RevokeWorkflowNarration(ctx context.Context, actor, runID, narrationID string, version int) (model.WorkflowRun, error) {
	if actor == "" || narrationID == "" {
		return model.WorkflowRun{}, model.ErrArgument
	}
	var saved model.WorkflowRun
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		run, err := s.locked(ctx, tx, runID, version)
		if err != nil {
			return err
		}
		rev, err := tx.GetRevision(ctx, run.CurrentRevisionID)
		if err != nil {
			return err
		}
		edl, err := s.readRevision(rev)
		if err != nil {
			return err
		}
		if findNarration(edl, narrationID) == nil {
			return model.ErrNotFound
		}
		asset, err := tx.GetAsset(ctx, run.AssetID)
		if err != nil {
			return err
		}
		if asset.Status == model.AssetStatusExported {
			asset.Status = model.AssetStatusIngested
			if err := tx.UpdateAsset(ctx, asset); err != nil {
				return err
			}
		}
		if err := tx.DeleteRunNarrationApproval(ctx, run.RunID, narrationID); err != nil {
			return err
		}
		if err := s.invalidateFrom(ctx, tx, &run, model.TaskTypeTTS); err != nil {
			return err
		}
		run.Status = model.WorkflowAwaitingReview
		run.Stage = "draft_review"
		run.BlockedReason = "narration approval was revoked"
		run.Version++
		run.UpdatedAt = time.Now().UTC()
		if err := tx.UpdateWorkflow(ctx, run); err != nil {
			return err
		}
		saved = run
		return nil
	})
	if err != nil {
		return model.WorkflowRun{}, err
	}
	s.publish("review.changed", saved)
	s.audit(ctx, actor, "narration.revoke", runID, narrationID)
	return saved, nil
}

func (s *Service) CancelWorkflow(ctx context.Context, actor, runID, reason string, version int) (model.WorkflowRun, error) {
	if actor == "" || reason == "" {
		return model.WorkflowRun{}, fmt.Errorf("service: cancel workflow: actor and reason are required: %w", model.ErrArgument)
	}
	var saved model.WorkflowRun
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		run, err := s.locked(ctx, tx, runID, version)
		if err != nil {
			return err
		}
		if err := s.invalidateFrom(ctx, tx, &run, model.TaskTypeRecognize); err != nil {
			return err
		}
		run.Status = model.WorkflowCancelled
		run.BlockedReason = reason
		run.Version++
		run.UpdatedAt = time.Now().UTC()
		if err := tx.UpdateWorkflow(ctx, run); err != nil {
			return err
		}
		saved = run
		return nil
	})
	if err != nil {
		return model.WorkflowRun{}, err
	}
	s.publish("workflow.changed", saved)
	s.audit(ctx, actor, "workflow.cancel", runID, reason)
	return saved, nil
}

func (s *Service) RetryWorkflow(ctx context.Context, actor, runID, failedStage, idemKey string, version int) (model.WorkflowRun, error) {
	if actor == "" || idemKey == "" {
		return model.WorkflowRun{}, fmt.Errorf("service: retry workflow: actor and idempotency_key are required: %w", model.ErrArgument)
	}
	scope := "workflow.retry:" + runID
	requestHash := fmt.Sprintf("%s|%d", failedStage, version)
	if cached, err := s.idempotent(ctx, scope, idemKey, requestHash); err == nil {
		var run model.WorkflowRun
		if json.Unmarshal(cached, &run) == nil {
			return run, nil
		}
	} else if !errors.Is(err, model.ErrNotFound) {
		return model.WorkflowRun{}, err
	}
	var saved model.WorkflowRun
	var attemptOf string
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		if got, body, e := tx.GetIdempotency(ctx, scope, idemKey); e == nil {
			if got != requestHash {
				return model.ErrConflict
			}
			return json.Unmarshal([]byte(body), &saved)
		} else if !errors.Is(e, model.ErrNotFound) {
			return e
		}
		run, err := s.locked(ctx, tx, runID, version)
		if err != nil {
			return err
		}
		if run.Status != model.WorkflowFailed || run.Stage != failedStage {
			return fmt.Errorf("service: retry workflow: run is not failed at %s: %w", failedStage, model.ErrInvalidState)
		}
		stages, err := tx.ListStages(ctx, run.RunID)
		if err != nil {
			return err
		}
		input := run.CurrentRevisionID
		for _, stg := range stages {
			if stg.Stage == failedStage && !stg.Invalidated {
				attemptOf = stg.TaskID
				input = stg.InputRevisionID
			}
		}
		run.Status = model.WorkflowRunning
		run.ErrorCode = ""
		run.BlockedReason = ""
		if err := s.queueStage(ctx, tx, run, failedStage, input, attemptOf); err != nil {
			return err
		}
		run.Version++
		run.UpdatedAt = time.Now().UTC()
		if err := tx.UpdateWorkflow(ctx, run); err != nil {
			return err
		}
		saved = run
		body, _ := json.Marshal(saved)
		return tx.PutIdempotency(ctx, scope, idemKey, requestHash, string(body))
	})
	if err != nil {
		return model.WorkflowRun{}, err
	}
	s.publish("workflow.changed", saved)
	s.audit(ctx, actor, "workflow.retry", runID, failedStage)
	return saved, nil
}

func (s *Service) RecordAcceptance(ctx context.Context, actor, runID, checkItem, result, note string, version int) (model.AcceptanceRecord, error) {
	if result == model.AcceptanceFailed && strings.TrimSpace(note) == "" {
		return model.AcceptanceRecord{}, model.ErrArgument
	}
	if actor == "" || (result != model.AcceptancePassed && result != model.AcceptanceFailed) {
		return model.AcceptanceRecord{}, fmt.Errorf("service: acceptance result must be passed or failed: %w", model.ErrArgument)
	}
	allowed := map[string]bool{
		"content_fidelity": true, "evidence_trace": true, "browser": true, "premiere": true,
		"cuts_fps": true, "subtitle_audio_duck": true,
	}
	if !allowed[checkItem] {
		return model.AcceptanceRecord{}, fmt.Errorf("service: unknown acceptance item: %w", model.ErrArgument)
	}
	var rec model.AcceptanceRecord
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		run, err := s.locked(ctx, tx, runID, version)
		if err != nil {
			return err
		}
		if run.Status != model.WorkflowReadyAcceptance {
			return fmt.Errorf("service: acceptance is not open: %w", model.ErrInvalidState)
		}
		rev, err := tx.GetRevision(ctx, run.CurrentRevisionID)
		if err != nil {
			return err
		}
		manifest := rev.PackageRef + "/delivery-manifest.json"
		if _, err := s.readRevision(rev); err != nil {
			return err
		}
		sum, err := s.hashUnderRoot(manifest)
		if err != nil {
			return err
		}
		rec = model.AcceptanceRecord{
			RunID: run.RunID, ExportRevisionID: rev.RevisionID, ManifestSHA256: sum,
			CheckItem: checkItem, Result: result, Actor: actor, Note: note, CreatedAt: time.Now().UTC(),
		}
		if err := tx.InsertAcceptance(ctx, rec); err != nil {
			return err
		}
		run.Version++
		run.UpdatedAt = time.Now().UTC()
		return tx.UpdateWorkflow(ctx, run)
	})
	if err != nil {
		return model.AcceptanceRecord{}, err
	}
	s.publish("acceptance.changed", rec)
	s.audit(ctx, actor, "acceptance.record", runID, checkItem+"="+result)
	return rec, nil
}

func (s *Service) ListAcceptance(ctx context.Context, runID string) ([]model.AcceptanceRecord, error) {
	return s.st.ListAcceptance(ctx, runID)
}

func (s *Service) lockRun(ctx context.Context, tx *store.Store, runID string, version int, revisionID string) (model.WorkflowRun, model.Revision, map[string]any, error) {
	run, err := s.locked(ctx, tx, runID, version)
	if err != nil {
		return model.WorkflowRun{}, model.Revision{}, nil, err
	}
	if run.CurrentRevisionID != revisionID {
		return model.WorkflowRun{}, model.Revision{}, nil, fmt.Errorf("service: revision is not current: %w", model.ErrConflict)
	}
	rev, err := tx.GetRevision(ctx, revisionID)
	if err != nil {
		return model.WorkflowRun{}, model.Revision{}, nil, err
	}
	edl, err := s.readRevision(rev)
	if err != nil {
		return model.WorkflowRun{}, model.Revision{}, nil, err
	}

	return run, rev, edl, nil
}

func (s *Service) locked(ctx context.Context, tx *store.Store, runID string, version int) (model.WorkflowRun, error) {
	run, err := tx.GetWorkflow(ctx, runID)
	if err != nil {
		return model.WorkflowRun{}, err
	}
	if run.Version != version {
		return model.WorkflowRun{}, fmt.Errorf("service: workflow version conflict: %w", model.ErrConflict)
	}
	if run.Status == model.WorkflowCancelled {
		return model.WorkflowRun{}, fmt.Errorf("service: workflow is cancelled: %w", model.ErrInvalidState)
	}
	return run, nil
}

func (s *Service) ensureIdle(ctx context.Context, tx *store.Store, run model.WorkflowRun) error {
	stages, err := tx.ListStages(ctx, run.RunID)
	if err != nil {
		return err
	}
	for _, stg := range stages {
		if stg.Invalidated {
			continue
		}
		tk, err := tx.GetTask(ctx, stg.TaskID)
		if err != nil {
			return err
		}
		if tk.Status == model.TaskStatusClaimed || tk.Status == model.TaskStatusRunning {
			return fmt.Errorf("service: workflow has an active worker; cancel it before editing: %w", model.ErrConflict)
		}
	}
	return nil
}

func (s *Service) invalidateFrom(ctx context.Context, tx *store.Store, run *model.WorkflowRun, fromStage string) error {
	order := []string{model.TaskTypeRecognize, model.TaskTypeSort, model.TaskTypeNarrate, model.TaskTypeTTS, model.TaskTypeSubtitle, model.TaskTypeMix, model.TaskTypeExport}
	drop := map[string]bool{}
	seen := fromStage == model.TaskTypeRecognize
	for _, stage := range order {
		if stage == fromStage {
			seen = true
		}
		if seen {
			drop[stage] = true
		}
	}
	stages, err := tx.ListStages(ctx, run.RunID)
	if err != nil {
		return err
	}
	var ids []string
	for _, stg := range stages {
		if !drop[stg.Stage] || stg.Invalidated {
			continue
		}
		tk, err := tx.GetTask(ctx, stg.TaskID)
		if err != nil {
			return err
		}
		if tk.Status == model.TaskStatusQueued || tk.Status == model.TaskStatusClaimed || tk.Status == model.TaskStatusRunning {
			tk.Status = model.TaskStatusCancelled
			tk.LeaseUntil = nil
			tk.Message = "workflow invalidated this stage"
			if err := tx.UpdateTask(ctx, tk); err != nil {
				return err
			}
		}
		ids = append(ids, stg.TaskID)
	}
	return tx.InvalidateQueuedStages(ctx, run.RunID, ids)
}

func findNarration(edl map[string]any, id string) map[string]any {
	for _, line := range asMapSlice(edl["narration"]) {
		if line["id"] == id {
			return line
		}
	}
	return nil
}

func stringList(v any) []string {
	switch items := v.(type) {
	case []any:
		out := []string{}
		for _, item := range items {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return items
	default:
		return nil
	}
}

// BindLegacyNarrationApproval attaches a phase-4 review body to the active
// run when the text, window and source match one saved draft. A hash supplied
// by the client is never trusted.
func (s *Service) BindLegacyNarrationApproval(ctx context.Context, actor, assetID string, draft NarrationDraft) (string, bool, error) {
	run, err := s.st.ActiveWorkflow(ctx, assetID)
	if errors.Is(err, model.ErrNotFound) {
		history, e := s.st.ListWorkflows(ctx, assetID, 1)
		if e != nil {
			return "", true, e
		}
		if len(history) != 0 {
			return "", true, fmt.Errorf("use versioned workflow review for this asset: %w", model.ErrConflict)
		}
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	rev, err := s.st.GetRevision(ctx, run.CurrentRevisionID)
	if err != nil {
		return "", false, err
	}
	edl, err := s.readRevision(rev)
	if err != nil {
		return "", false, err
	}
	var matched string
	for _, line := range asMapSlice(edl["narration"]) {
		start, ok1 := number(line["start"])
		end, ok2 := number(line["end"])
		text, _ := line["text"].(string)
		if ok1 && ok2 && text == draft.Text && start == draft.Start && end == draft.End && narrationSource(line) == draft.Source {
			matched, _ = line["id"].(string)
			break
		}
	}
	if matched == "" {
		return "", true, fmt.Errorf("service: draft does not match the current revision: %w", model.ErrConflict)
	}
	hash := NarrationDraftHash(draft.Text, draft.Start, draft.End, draft.Source)
	_, err = s.ApproveWorkflowNarration(ctx, actor, run.RunID, run.Version, rev.RevisionID, matched)
	return hash, true, err
}

// EditWorkflow copies the current package, applies a limited scene or draft
// change, and stores a new revision. The caller must send the current version.
func (s *Service) EditWorkflow(ctx context.Context, actor, runID string, version int, baseRevision string, sceneID, label string, rank *int, narrationID, text string, start, end *float64, sourceSceneID string) (model.WorkflowRun, error) {
	if actor == "" {
		return model.WorkflowRun{}, fmt.Errorf("service: edit workflow: actor is required: %w", model.ErrArgument)
	}
	var saved model.WorkflowRun
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		run, rev, edl, err := s.lockRun(ctx, tx, runID, version, baseRevision)
		if err != nil {
			return err
		}
		if err := s.ensureIdle(ctx, tx, run); err != nil {
			return err
		}
		sceneEdit := sceneID != ""
		if sceneEdit {
			if err := applySceneEdit(edl, sceneID, label, rank); err != nil {
				return err
			}
			delete(edl, "narration")
			edl["voice"] = []any{}
			edl["subtitle"] = []any{}
		} else if narrationID != "" {
			if err := applyNarrationEdit(edl, narrationID, text, start, end, sourceSceneID); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("service: edit workflow: nothing to change: %w", model.ErrArgument)
		}
		if err := validateWorkflowEdit(edl); err != nil {
			return err
		}
		if _, err := s.evidenceFiles(rev.PackageRef, edl); err != nil {
			return err
		}
		asset, err := tx.GetAsset(ctx, run.AssetID)
		if err != nil {
			return err
		}
		if asset.Status == model.AssetStatusExported {
			asset.Status = model.AssetStatusIngested
			if err := tx.UpdateAsset(ctx, asset); err != nil {
				return err
			}
		}
		nextRef := "revisions/" + run.AssetID + "/" + newID("rev")
		if err := s.copyPackage(rev.PackageRef, nextRef, edl); err != nil {
			return err
		}
		sum, err := s.hashUnderRoot(nextRef + "/edl.json")
		if err != nil {
			return err
		}
		ev, err := evidenceHash(s.deliveryRoot, nextRef)
		if err != nil {
			return err
		}
		next := model.Revision{
			RevisionID: newID("rev"), AssetID: run.AssetID, ParentRevisionID: rev.RevisionID,
			SchemaVersion: 1, PackageRef: nextRef, EDLSHA256: sum, EvidenceManifestSHA256: ev,
			CreatedAt: time.Now().UTC(), CreatedBy: actor, Reason: "edit",
		}
		if err := tx.InsertRevision(ctx, next); err != nil {
			return err
		}
		from := model.TaskTypeTTS
		if sceneEdit {
			from = model.TaskTypeSort
			run.Stage = "scene_review"
			run.BlockedReason = "scene edit invalidated confirmation and downstream drafts"
		} else {
			run.Stage = "draft_review"
			run.BlockedReason = "narration edit invalidated its approval"
		}
		if err := s.invalidateFrom(ctx, tx, &run, from); err != nil {
			return err
		}
		run.CurrentRevisionID = next.RevisionID
		run.Status = model.WorkflowAwaitingReview
		run.Version++
		run.UpdatedAt = time.Now().UTC()
		if err := tx.UpdateWorkflow(ctx, run); err != nil {
			return err
		}
		saved = run
		return nil
	})
	if err != nil {
		return model.WorkflowRun{}, err
	}
	s.publish("workflow.changed", saved)
	s.audit(ctx, actor, "revision.create", saved.CurrentRevisionID, "edit")
	return saved, nil
}

func applySceneEdit(edl map[string]any, sceneID, label string, rank *int) error {
	var target map[string]any
	for _, scene := range asMapSlice(edl["scenes"]) {
		if scene["scene_id"] == sceneID {
			target = scene
			break
		}
	}
	if target == nil {
		return fmt.Errorf("service: scene %s: %w", sceneID, model.ErrNotFound)
	}
	if label != "" {
		if len([]rune(label)) > 100 {
			return fmt.Errorf("service: label is too long: %w", model.ErrArgument)
		}
		target["label"] = label
	}
	if rank != nil {
		items := asMapSlice(edl["scenes"])
		if *rank < 0 || *rank >= len(items) {
			return fmt.Errorf("service: sequence_rank must be >= 0: %w", model.ErrArgument)
		}
		others := []map[string]any{}
		for _, scene := range items {
			if scene["scene_id"] != sceneID {
				others = append(others, scene)
			}
		}
		order := append(others[:*rank], append([]map[string]any{target}, others[*rank:]...)...)
		for i, scene := range order {
			scene["sequence_rank"] = i
		}
	}
	return nil
}

func applyNarrationEdit(edl map[string]any, narrationID, text string, start, end *float64, sourceSceneID string) error {
	line := findNarration(edl, narrationID)
	if line == nil {
		return fmt.Errorf("service: narration %s: %w", narrationID, model.ErrNotFound)
	}
	if text != "" {
		if err := validateNarrationDraft(NarrationDraft{Text: text, Start: 0, End: 1}); err != nil {
			return err
		}
		line["text"] = text
	}
	if start != nil {
		line["start"] = *start
	}
	if end != nil {
		line["end"] = *end
	}
	if sourceSceneID != "" {
		found := false
		for _, scene := range asMapSlice(edl["scenes"]) {
			if scene["scene_id"] == sourceSceneID {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("service: source scene is not in this revision: %w", model.ErrArgument)
		}
		line["source_scene_id"] = sourceSceneID
		for _, scene := range asMapSlice(edl["scenes"]) {
			if scene["scene_id"] == sourceSceneID {
				line["source_scene_label"] = scene["label"]
			}
		}
	}
	s, ok1 := number(line["start"])
	e, ok2 := number(line["end"])
	if !ok1 || !ok2 || !(s < e) {
		return fmt.Errorf("service: narration window is invalid: %w", model.ErrArgument)
	}
	return nil
}

func (s *Service) copyPackage(from, to string, edl map[string]any) error {
	src, _, err := s.deliveryPath(filepath.FromSlash(from), true)
	if err != nil {
		return err
	}
	dst := joinRoot(s.deliveryRoot, to)
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0755); err != nil {
		return err
	}
	if _, _, err := s.deliveryPath(filepath.FromSlash(filepathSlashDir(to)), true); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".snapshot-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	if err := copyTree(src, staging); err != nil {
		return err
	}
	body, err := json.MarshalIndent(edl, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, "edl.json"), append(body, '\n'), 0644); err != nil {
		return err
	}
	return os.Rename(staging, dst)
}
