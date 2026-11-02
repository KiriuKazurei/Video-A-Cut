package service

import (
	"context"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/skill"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
)

type SkillVoiceRequest struct {
	ExpectedVersion int          `json:"expected_version"`
	SegmentIndex    int          `json:"segment_index"`
	Enabled         bool         `json:"enabled"`
	Settings        tts.Settings `json:"settings"`
	Offset          float64      `json:"offset_seconds"`
	AcceptExternal  bool         `json:"accept_external"`
}

func (s *Service) SaveSkillVoice(ctx context.Context, actor, id string, req SkillVoiceRequest) (skill.Run, error) {
	if s.WorkspaceEnabled() {
		req.AcceptExternal = s.ProviderPolicy().ExternalTTS
	}
	r, err := s.GetSkillRun(ctx, actor, id)
	if err != nil {
		return r, err
	}
	if _, err = s.checkSkillRun(ctx, r); err != nil {
		return r, err
	}
	if r.Version != req.ExpectedVersion || r.Plan == nil || req.SegmentIndex < 0 || req.SegmentIndex >= len(r.Plan.Segments) || r.State == "running" || r.State == "cancelled" || r.State == "succeeded" {
		return r, model.ErrConflict
	}
	if err = s.applySkillVoice(&r, req); err != nil {
		return r, err
	}
	version := r.Version
	r.Version++
	r.UpdatedAt = s.autoNow()
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		if err := tx.SkillPut(ctx, "run", id, version, r); err != nil {
			return err
		}
		return auditTx(ctx, tx, actor, "skill.voice.save", id, "new plan revision")
	})
	if err == nil {
		s.publish("skill.run.changed", r)
	}
	return r, err
}

// Both the model tool and the editing API use the same voice policy.
func (s *Service) applySkillVoice(r *skill.Run, req SkillVoiceRequest) error {
	if r.Plan == nil || req.SegmentIndex < 0 || req.SegmentIndex >= len(r.Plan.Segments) {
		return model.ErrArgument
	}
	if s.WorkspaceEnabled() {
		req.AcceptExternal = s.ProviderPolicy().ExternalTTS
	}
	if req.Enabled {
		if err := req.Settings.Validate(); err != nil {
			return model.ErrArgument
		}
		if req.Settings.External() && !req.AcceptExternal {
			return model.ErrForbidden
		}
	}
	seg := &r.Plan.Segments[req.SegmentIndex]
	if !finiteScriptTime(req.Offset) || req.Offset < 0 || req.Offset >= seg.End-seg.Start {
		return model.ErrArgument
	}
	seg.TTSEnabled = req.Enabled
	seg.Voice = &req.Settings
	seg.VoiceOffset = req.Offset
	seg.VoiceConsent = req.AcceptExternal
	r.Plan.Version++
	r.Inspection = nil
	s.confirmTaskPlan(r)
	return nil
}
