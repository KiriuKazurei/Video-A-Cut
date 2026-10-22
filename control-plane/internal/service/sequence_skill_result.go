package service

import (
	"context"
	"fmt"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/project"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
)

type SequenceResultRequest struct {
	Session          string `json:"session"`
	ExpectedRevision uint64 `json:"expected_revision"`
	IdempotencyKey   string `json:"idempotency_key"`
	RunID            string `json:"run_id"`
	ArtifactID       string `json:"artifact_id"`
	CheckpointID     string `json:"checkpoint_id,omitempty"`
}

func (s *Service) ImportSequenceSkillResult(ctx context.Context, projectID, sequenceID string, req SequenceResultRequest) (TimelineView, error) {
	if req.IdempotencyKey == "" {
		return TimelineView{}, model.ErrArgument
	}
	return s.importSequenceSkillResult(ctx, projectID, sequenceID, req, false, false)
}
func (s *Service) importSequenceSkillResult(ctx context.Context, projectID, sequenceID string, req SequenceResultRequest, automatic, alreadyLocked bool) (TimelineView, error) {
	run, err := s.GetSkillRun(ctx, "human:window", req.RunID)
	if err != nil {
		return TimelineView{}, err
	}
	if run.ProjectID != projectID {
		return TimelineView{}, model.ErrNotFound
	}
	var path string
	var artifact skill.Artifact
	if req.CheckpointID != "" {
		path, artifact, err = s.SkillCheckpointPreview(ctx, "human:window", run.ID, req.CheckpointID, req.ArtifactID)
	} else {
		if run.State != "succeeded" {
			return TimelineView{}, model.ErrInvalidState
		}
		path, artifact, err = s.skillArtifactPath(ctx, run, req.ArtifactID)
	}
	if err != nil {
		return TimelineView{}, err
	}
	if artifact.Kind != "mp4" {
		return TimelineView{}, model.ErrArgument
	}
	w := s.workspace
	if w == nil {
		return TimelineView{}, model.ErrInvalidState
	}
	if !alreadyLocked {
		if err = w.lockMutation(ctx); err != nil {
			return TimelineView{}, err
		}
		defer w.op.Unlock()
	}
	w.mu.Lock()
	if w.document == nil || w.document.Project.ID != projectID || (w.closing && !alreadyLocked) || (!automatic && req.Session != w.session) {
		w.mu.Unlock()
		return TimelineView{}, model.ErrConflict
	}
	draft := cloneProject(w.document)
	index := sequenceIndex(draft, sequenceID)
	if index < 0 {
		w.mu.Unlock()
		return TimelineView{}, model.ErrNotFound
	}
	groupID := "grp_" + skillHash([]any{run.ID, artifact.SHA, artifact.PlanVersion})[:24]
	// Receipts are outside the undo snapshot. Reopening a project must not
	// silently undo the user's removal of an automatically imported result.
	if automatic && draft.Sequences[index].ImportedResults[groupID] {
		view := timelineView(draft, draft.Sequences[index], w.session, w.revision)
		w.mu.Unlock()
		return view, nil
	}
	key := "skill-result:" + sequenceID + ":" + req.IdempotencyKey
	hash := skillHash(req)
	if !automatic {
		if receipt, ok := w.timelineReceipts[key]; ok {
			view := timelineView(draft, draft.Sequences[index], w.session, w.revision)
			w.mu.Unlock()
			if receipt.Hash != hash {
				return TimelineView{}, model.ErrConflict
			}
			return view, nil
		}
	}
	for _, g := range draft.Sequences[index].Groups {
		if g.ID == groupID {
			view := timelineView(draft, draft.Sequences[index], w.session, w.revision)
			w.mu.Unlock()
			return view, nil
		}
	}
	if !automatic && req.ExpectedRevision != draft.Sequences[index].Revision {
		w.mu.Unlock()
		return TimelineView{}, model.ErrConflict
	}
	w.mu.Unlock()
	if err = s.captureProjectScripts(ctx, draft); err != nil {
		return TimelineView{}, err
	}
	seq := &draft.Sequences[index]
	before := seq.Snapshot(draft)
	resource, err := inspectResource(ctx, path)
	if err != nil {
		return TimelineView{}, err
	}
	if resource.Kind != "video" || resource.Source.SHA256 != artifact.SHA || resource.Source.Size != artifact.Size {
		return TimelineView{}, model.ErrConflict
	}
	resource.ID = "res_" + skillHash([]any{run.ID, artifact.SHA, artifact.PlanVersion})[:24]
	resource.AssetID = resource.ID
	resource.Name = artifact.Name
	known := false
	for _, r := range draft.Resources {
		if r.ID == resource.ID {
			known = true
			resource = r
			break
		}
	}
	if !known {
		draft.Resources = append(draft.Resources, resource)
	}
	origin := project.Origin{Actor: "agent:" + run.ID, RunID: run.ID, ArtifactID: artifact.ID, SHA256: artifact.SHA, PlanVersion: artifact.PlanVersion}
	seq.Groups = append(seq.Groups, project.TrackGroup{ID: groupID, Name: fmt.Sprintf("Agent · %s · v%d", run.ID[len(run.ID)-6:], artifact.PlanVersion), Origin: origin})
	if err = seq.Apply(draft, project.Operation{Type: "track.add", Kind: "video", GroupID: groupID, Name: "Agent 视频"}, origin); err != nil {
		return TimelineView{}, err
	}
	trackID := seq.Tracks[len(seq.Tracks)-1].ID
	if err = seq.Apply(draft, project.Operation{Type: "clip.insert", TrackID: trackID, ResourceID: resource.ID, At: 0}, origin); err != nil {
		return TimelineView{}, err
	}
	seq.ScopeGroup = groupID
	if seq.ImportedResults == nil {
		seq.ImportedResults = map[string]bool{}
	}
	seq.ImportedResults[groupID] = true
	seq.Revision++
	seq.History = append(seq.History[:seq.HistoryCursor], project.EditRecord{ID: groupID, Label: "Skill 成片入轨", Actor: origin.Actor, Before: before, After: seq.Snapshot(draft)})
	seq.HistoryCursor = len(seq.History)
	if err = draft.Validate(); err != nil {
		return TimelineView{}, err
	}
	if !known {
		if err = s.materializeResource(ctx, resource, projectID); err != nil {
			return TimelineView{}, err
		}
	}
	w.mu.Lock()
	w.document = draft
	if !automatic {
		if w.timelineReceipts == nil {
			w.timelineReceipts = map[string]timelineReceipt{}
		}
		w.timelineReceipts[key] = timelineReceipt{Hash: hash}
	}
	w.revision++
	w.dirty = true
	view := timelineView(cloneProject(draft), draft.Sequences[index], w.session, w.revision)
	w.mu.Unlock()
	if err = s.refreshProjectMedia(ctx); err != nil {
		return view, err
	}
	s.publish("workspace.timeline.changed", map[string]any{"project_id": projectID, "sequence_id": sequenceID, "run_id": run.ID, "group_id": groupID})
	return view, nil
}
func (s *Service) importCompletedSequenceResult(ctx context.Context, run skill.Run) {
	s.importCompletedSequenceResultLocked(ctx, run, false)
}
func (s *Service) importCompletedSequenceResultLocked(ctx context.Context, run skill.Run, alreadyLocked bool) {
	if s.workspace == nil || run.State != "succeeded" || (run.StructuredEdit && run.Plan == nil) || run.SequenceID == "" || run.ProjectID != s.CurrentProjectID() {
		return
	}
	for i := len(run.Artifacts) - 1; i >= 0; i-- {
		a := run.Artifacts[i]
		if a.Kind == "mp4" && run.Plan != nil && a.PlanVersion == run.Plan.Version {
			_, err := s.importSequenceSkillResult(ctx, run.ProjectID, run.SequenceID, SequenceResultRequest{RunID: run.ID, ArtifactID: a.ID}, true, alreadyLocked)
			if err != nil {
				s.publish("workspace.timeline.import-failed", map[string]any{"project_id": run.ProjectID, "sequence_id": run.SequenceID, "run_id": run.ID, "message": err.Error()})
			}
			return
		}
	}
}
func (s *Service) reconcileSequenceResults(ctx context.Context, projectID, session string) {
	runs, err := s.ListSkillRuns(ctx, "human:workspace", projectID)
	if err != nil {
		return
	}
	for _, run := range runs {
		if s.ProjectSessionID() != session {
			return
		}
		s.importCompletedSequenceResultLocked(ctx, run, true)
	}
}
