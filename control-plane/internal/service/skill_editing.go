package service

import (
	"context"
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/scriptsyntax"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"math"
	"slices"
	"strings"
	"time"
)

func editWindows(p *skill.Plan) []skill.EditWindow {
	out := []skill.EditWindow{}
	if p == nil {
		return out
	}
	cursor := 0.
	document := scriptsyntax.Parse(p.Body)
	for i, seg := range p.Segments {
		duration := math.Round((seg.End-seg.Start)*30) / 30
		if i > 0 && i < len(document.Paragraphs) && document.Paragraphs[i].Effect == "交叉溶解" {
			prior := p.Segments[i-1]
			priorDuration := math.Round((prior.End-prior.Start)*30) / 30
			cursor -= math.Floor(math.Min(.35, math.Min(priorDuration/3, duration/3))*30) / 30
		}
		out = append(out, skill.EditWindow{SegmentIndex: i, MediaID: seg.MediaID, TimelineStart: cursor, TimelineEnd: cursor + duration, SourceStart: seg.Start, SourceEnd: seg.End})
		cursor += duration
	}
	return out
}
func (s *Service) SkillWorkbench(ctx context.Context, actor, id string) (map[string]any, error) {
	if err := requireHumanConfirm(actor, "read editing workbench"); err != nil {
		return nil, err
	}
	r, err := s.GetSkillRun(ctx, actor, id)
	if err != nil {
		return nil, err
	}
	check := r
	check.LeaseUntil = nil
	if _, err = s.checkSkillRun(ctx, check); err != nil {
		return nil, err
	}
	cursor, err := s.st.EditCursor(ctx, id)
	if err != nil {
		return nil, err
	}
	previews := []map[string]any{}
	nodes, err := s.st.EditCheckpoints(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, a := range r.Artifacts {
		if a.Kind != "mp4" {
			continue
		}
		current := r.Plan != nil && a.PlanVersion == r.Plan.Version && r.Plan.FinalizedBy != ""
		valid := false
		if current {
			_, _, e := s.SkillArtifactPath(ctx, actor, id, a.ID)
			valid = e == nil
		}
		checkpoint := ""
		if !valid {
			for _, node := range nodes {
				if !checkpointValid(node) {
					return nil, model.ErrConflict
				}
				var saved skill.Run
				if json.Unmarshal(node.Body, &saved) != nil || saved.Plan == nil || saved.Plan.Version != a.PlanVersion || !isHumanActor(saved.Plan.FinalizedBy) {
					continue
				}
				for _, old := range saved.Artifacts {
					if old.ID == a.ID && old.SHA == a.SHA {
						checkpoint = node.ID
					}
				}
			}
		}
		if checkpoint != "" {
			_, _, e := s.SkillCheckpointPreview(ctx, actor, id, checkpoint, a.ID)
			valid = e == nil
		}
		previews = append(previews, map[string]any{"artifact": a, "is_current": current, "available": valid, "historical": !current, "checkpoint_id": checkpoint, "exportable": current && valid})
	}
	return map[string]any{"run": r, "segments": editWindows(r.Plan), "previews": previews, "history": cursor}, nil
}

type SkillEditRequest struct {
	ExpectedVersion int      `json:"expected_version"`
	PlanVersion     int      `json:"plan_version"`
	Prompt          string   `json:"prompt"`
	SegmentIndex    *int     `json:"segment_index,omitempty"`
	At              *float64 `json:"at_seconds,omitempty"`
	Start           *float64 `json:"timeline_start,omitempty"`
	End             *float64 `json:"timeline_end,omitempty"`
	ArtifactID      string   `json:"artifact_id"`
	ArtifactSHA     string   `json:"artifact_sha256"`
	IdempotencyKey  string   `json:"idempotency_key"`
}

func (s *Service) RequestSkillEdit(ctx context.Context, actor, id string, req SkillEditRequest) (skill.Run, error) {
	var zero skill.Run
	if err := requireHumanConfirm(actor, "request targeted edit"); err != nil {
		return zero, err
	}
	if strings.TrimSpace(req.Prompt) == "" || len(req.Prompt) > 8192 || req.IdempotencyKey == "" || len(req.IdempotencyKey) > 128 {
		return zero, model.ErrArgument
	}
	r, err := s.GetSkillRun(ctx, actor, id)
	if err != nil {
		return zero, err
	}
	scope := "skill-edit:" + id
	hash := skillHash(req)
	if old, _, e := s.st.SkillRequest(ctx, scope, req.IdempotencyKey); e == nil {
		if old != hash {
			return zero, model.ErrConflict
		}
		return r, nil
	}
	if r.Plan == nil || r.Plan.Version != req.PlanVersion || r.Version != req.ExpectedVersion || r.State == "running" || r.State == "cancelled" {
		return zero, model.ErrConflict
	}
	check := r
	check.LeaseUntil = nil
	if _, err = s.checkSkillRun(ctx, check); err != nil {
		return zero, err
	}
	if _, a, e := s.SkillArtifactPath(ctx, actor, id, req.ArtifactID); e != nil {
		return zero, e
	} else if a.Kind != "mp4" || a.PlanVersion != r.Plan.Version || a.SHA != req.ArtifactSHA {
		return zero, model.ErrConflict
	}
	windows := editWindows(r.Plan)
	selected := []skill.EditWindow{}
	modes := 0
	if req.SegmentIndex != nil {
		modes++
	}
	if req.At != nil {
		modes++
	}
	if req.Start != nil || req.End != nil {
		modes++
	}
	if modes != 1 {
		return zero, model.ErrArgument
	}
	if req.At != nil && !finiteScriptTime(*req.At) {
		return zero, model.ErrArgument
	}
	if req.Start != nil || req.End != nil {
		if req.Start == nil || req.End == nil || !finiteScriptTime(*req.Start) || !finiteScriptTime(*req.End) || *req.Start < 0 || *req.End <= *req.Start || *req.End > windows[len(windows)-1].TimelineEnd+1./30 {
			return zero, model.ErrArgument
		}
	}
	for _, w := range windows {
		match := req.SegmentIndex != nil && w.SegmentIndex == *req.SegmentIndex
		if req.At != nil {
			match = *req.At >= w.TimelineStart && *req.At < w.TimelineEnd
		}
		if req.Start != nil {
			match = *req.Start < w.TimelineEnd && *req.End > w.TimelineStart
		}
		if match {
			selected = append(selected, w)
		}
	}
	if len(selected) == 0 {
		return zero, model.ErrArgument
	}
	// Range selection is segment-granular. Do not silently expand an exact user
	// range; the workbench must select clip boundaries (or split the plan first).
	if req.Start != nil && (math.Abs(*req.Start-selected[0].TimelineStart) > 1e-6 || math.Abs(*req.End-selected[len(selected)-1].TimelineEnd) > 1e-6) {
		return zero, model.ErrArgument
	}
	base := *r.Plan
	base.Segments = slices.Clone(r.Plan.Segments)
	r.EditRequest = &skill.EditRequest{Prompt: req.Prompt, BasePlanVersion: base.Version, BasePlan: base, Windows: selected, RequestedStart: req.Start, RequestedEnd: req.End, SourceArtifactID: req.ArtifactID, SourceArtifactSHA: req.ArtifactSHA}
	r.State = "queued"
	r.Reason = "targeted_edit"
	r.BudgetStepBase, r.BudgetElapsedBase = r.Steps, r.ElapsedMilliseconds
	r.Inspection = nil
	if r.AgentRole != "" {
		until := s.autoNow().Add(2 * time.Minute)
		r.LeaseUntil = &until
	}
	version := r.Version
	r.Version++
	r.UpdatedAt = s.autoNow()
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		if e := tx.SkillPut(ctx, "run", id, version, r); e != nil {
			return e
		}
		if e := tx.SkillRemember(ctx, scope, req.IdempotencyKey, hash, id); e != nil {
			return e
		}
		return s.journalTx(ctx, tx, actor, id, "edit.request", r.EditRequest)
	})
	if err == nil {
		s.publish("skill.run.changed", r)
	}
	return r, err
}

// A targeted Agent proposal may change only selected segments. Never accept an
// unrelated rewrite hidden inside a local edit, or restore Agent-supplied consent.
func enforceTargetedEdit(r *skill.Run, p *skill.Plan) error {
	if r.EditRequest == nil {
		return nil
	}
	edit := r.EditRequest
	base := edit.BasePlan
	if r.Plan == nil || r.Plan.Version != edit.BasePlanVersion || p.Body != base.Body || p.Subtitles != base.Subtitles || len(p.Segments) != len(base.Segments) {
		return model.ErrConflict
	}
	selected := map[int]bool{}
	for _, w := range edit.Windows {
		selected[w.SegmentIndex] = true
	}
	for i := range p.Segments {
		if selected[i] {
			continue
		}
		a, b := p.Segments[i], base.Segments[i]
		if a.MediaID != b.MediaID || a.Start != b.Start || a.End != b.End || a.Narration != b.Narration || skillHash(a.SubtitlesEnabled) != skillHash(b.SubtitlesEnabled) {
			return model.ErrForbidden
		}
		p.Segments[i] = b
	}
	return nil
}

type SkillPlanSave struct {
	ExpectedVersion int        `json:"expected_version"`
	Plan            skill.Plan `json:"plan"`
}

func (s *Service) SaveSkillPlan(ctx context.Context, actor, id string, req SkillPlanSave) (skill.Run, error) {
	r, err := s.GetSkillRun(ctx, actor, id)
	if err != nil {
		return r, err
	}
	if err = requireHumanConfirm(actor, "save editing plan"); err != nil {
		return r, err
	}
	check := r
	check.LeaseUntil = nil
	snap, err := s.checkSkillRun(ctx, check)
	if err != nil {
		return r, err
	}
	if r.Version != req.ExpectedVersion || r.State == "running" || r.State == "cancelled" {
		return r, model.ErrConflict
	}
	if err = validateHarnessPlan(req.Plan, snap); err != nil {
		return r, err
	}
	req.Plan.Version, err = s.st.NextSkillPlanVersion(ctx, id)
	if err != nil {
		return r, err
	}
	req.Plan.FinalizedBy = ""
	req.Plan.FinalizedAt = nil
	// Voice settings and their consent must go through the dedicated human endpoint.
	for i := range req.Plan.Segments {
		req.Plan.Segments[i].TTSEnabled = false
		req.Plan.Segments[i].Voice = nil
		req.Plan.Segments[i].VoiceConsent = false
		req.Plan.Segments[i].VoiceOffset = 0
	}
	r.Plan = &req.Plan
	r.EditRequest = nil
	r.Inspection = nil
	r.State = "waiting_human"
	r.Reason = "plan_confirmation"
	version := r.Version
	r.Version++
	r.UpdatedAt = s.autoNow()
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		if e := tx.SkillPut(ctx, "run", id, version, r); e != nil {
			return e
		}
		return s.journalTx(ctx, tx, actor, id, "plan.save", map[string]any{"plan_version": r.Plan.Version})
	})
	if err == nil {
		s.publish("skill.run.changed", r)
	}
	return r, err
}
