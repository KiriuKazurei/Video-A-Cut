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
	r, err := s.GetSkillRun(ctx, actor, id)
	if err != nil {
		return r, err
	}
	if err = requireHumanConfirm(actor, "configure Skill voice"); err != nil {
		return r, err
	}
	if _, err = s.checkSkillRun(ctx, r); err != nil {
		return r, err
	}
	if r.Version != req.ExpectedVersion || r.Plan == nil || req.SegmentIndex < 0 || req.SegmentIndex >= len(r.Plan.Segments) || r.State == "running" || r.State == "cancelled" || r.State == "succeeded" {
		return r, model.ErrConflict
	}
	if req.Enabled {
		if err = req.Settings.Validate(); err != nil {
			return r, model.ErrArgument
		}
		if req.Settings.External() && !req.AcceptExternal {
			return r, model.ErrForbidden
		}
	}
	seg := &r.Plan.Segments[req.SegmentIndex]
	if !finiteScriptTime(req.Offset) || req.Offset < 0 || req.Offset >= seg.End-seg.Start {
		return r, model.ErrArgument
	}
	seg.TTSEnabled = req.Enabled
	seg.Voice = &req.Settings
	seg.VoiceOffset = req.Offset
	seg.VoiceConsent = req.AcceptExternal
	r.Plan.Version++
	r.Plan.FinalizedBy = ""
	r.Plan.FinalizedAt = nil
	r.Inspection = nil
	r.State = "waiting_human"
	r.Reason = "plan_confirmation"
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
