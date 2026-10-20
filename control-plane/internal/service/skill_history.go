package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"slices"
)

func (s *Service) SkillEditHistory(ctx context.Context, actor, id string) (map[string]any, error) {
	if err := requireHumanConfirm(actor, "read edit history"); err != nil {
		return nil, err
	}
	if _, err := s.GetSkillRun(ctx, actor, id); err != nil {
		return nil, err
	}
	nodes, err := s.st.EditCheckpoints(ctx, id)
	if err != nil {
		return nil, err
	}
	cursor, err := s.st.EditCursor(ctx, id)
	return map[string]any{"cursor": cursor, "checkpoints": nodes}, err
}
func checkpointValid(c store.EditCheckpoint) bool {
	h := sha256.Sum256(c.Body)
	return hex.EncodeToString(h[:]) == c.SHA
}
func (s *Service) MoveSkillHistory(ctx context.Context, actor, id, action string, version int) (skill.Run, error) {
	var r skill.Run
	if err := requireHumanConfirm(actor, "restore edit checkpoint"); err != nil {
		return r, err
	}
	err := s.st.Transaction(ctx, func(tx *store.Store) error {
		if e := tx.SkillGet(ctx, "run", id, &r); e != nil {
			return e
		}
		r.FramesSpent = max(r.FramesSpent, len(r.Frames))
		if r.Version != version || r.State == "running" || r.State == "cancelled" {
			return model.ErrConflict
		}
		// Context is checked on the same transaction through a service view.
		view := &Service{st: tx, deliveryRoot: s.deliveryRoot, clock: s.autoNow, workspace: s.workspace}
		check := r
		check.LeaseUntil = nil
		snap, e := view.checkSkillRun(ctx, check)
		if e != nil {
			return e
		}
		cursor, e := tx.EditCursor(ctx, id)
		if e != nil {
			return e
		}
		nodes, e := tx.EditCheckpoints(ctx, id)
		if e != nil {
			return e
		}
		lookup := map[string]store.EditCheckpoint{}
		for _, node := range nodes {
			lookup[node.ID] = node
		}
		current, ok := lookup[cursor.ID]
		if !ok || !checkpointValid(current) {
			return model.ErrConflict
		}
		target := ""
		switch action {
		case "undo":
			target = current.ParentID
			if target == "" {
				return model.ErrConflict
			}
			cursor.Redo = append(cursor.Redo, cursor.ID)
		case "redo":
			if len(cursor.Redo) == 0 {
				return model.ErrConflict
			}
			target = cursor.Redo[len(cursor.Redo)-1]
			cursor.Redo = cursor.Redo[:len(cursor.Redo)-1]
			if lookup[target].ParentID != cursor.ID {
				return model.ErrConflict
			}
		default:
			return model.ErrArgument
		}
		node, ok := lookup[target]
		if !ok || !checkpointValid(node) {
			return model.ErrConflict
		}
		var snapshot skill.Run
		if e = json.Unmarshal(node.Body, &snapshot); e != nil {
			return e
		}
		nextPlan, e := tx.NextSkillPlanVersion(ctx, id)
		if e != nil {
			return e
		}
		r.Plan = snapshot.Plan
		r.Summary = snapshot.Summary
		r.Frames = slices.Clone(snapshot.Frames)
		r.Artifacts = slices.Clone(snapshot.Artifacts)
		r.Inspection = nil
		r.EditRequest = nil
		if r.Plan != nil {
			r.Plan.Version = nextPlan
			r.Plan.FinalizedBy = ""
			r.Plan.FinalizedAt = nil
			for i := range r.Plan.Segments {
				r.Plan.Segments[i].VoiceConsent = false
			}
			if e = validateHarnessPlan(*r.Plan, snap); e != nil {
				return e
			}
			r.State = "waiting_human"
			r.Reason = "plan_confirmation"
		} else {
			r.State = "queued"
			r.Reason = "history_restored"
		}
		// No budget, provider consent, ownership or external lease is rolled back.
		r.Version++
		r.UpdatedAt = s.autoNow()
		cursor.ID = target
		if e = tx.RestoreEditRun(ctx, id, version, r, cursor); e != nil {
			return e
		}
		return s.journalTx(ctx, tx, actor, id, "history."+action, map[string]any{"from": current.ID, "to": target, "new_run_version": r.Version})
	})
	if err == nil {
		s.publish("skill.run.changed", r)
	}
	return r, err
}
func (s *Service) SkillCheckpointPreview(ctx context.Context, actor, id, checkpoint, artifact string) (string, skill.Artifact, error) {
	var zero skill.Artifact
	if err := requireHumanConfirm(actor, "preview historical edit"); err != nil {
		return "", zero, err
	}
	r, err := s.GetSkillRun(ctx, actor, id)
	if err != nil {
		return "", zero, err
	}
	check := r
	check.LeaseUntil = nil
	if _, err = s.checkSkillRun(ctx, check); err != nil {
		return "", zero, err
	}
	nodes, err := s.st.EditCheckpoints(ctx, id)
	if err != nil {
		return "", zero, err
	}
	for _, node := range nodes {
		if node.ID != checkpoint {
			continue
		}
		if !checkpointValid(node) {
			return "", zero, model.ErrConflict
		}
		var snapshot skill.Run
		if err = json.Unmarshal(node.Body, &snapshot); err != nil {
			return "", zero, err
		}
		for _, a := range snapshot.Artifacts {
			if a.ID == artifact && a.Kind == "mp4" && snapshot.Plan != nil && a.PlanVersion == snapshot.Plan.Version && isHumanActor(snapshot.Plan.FinalizedBy) {
				r.Plan = snapshot.Plan
				r.Artifacts = []skill.Artifact{a}
				return s.skillArtifactPath(ctx, r, artifact)
			}
		}
	}
	return "", zero, model.ErrNotFound
}
