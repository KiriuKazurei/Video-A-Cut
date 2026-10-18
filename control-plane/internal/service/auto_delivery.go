package service

import (
	"context"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// AutoDelivery is what an auto run has produced for the person, in the terms of
// the design's definition of "delivered" (§6.0): the delivery package of the
// current export AND the 720p preview of that same export. The preview is a
// 720p preview spec, never the original-quality final cut; Spec says so for the
// UI to print.
type AutoDelivery struct {
	// Delivered is true only when both parts exist and the preview is the
	// current export's.
	Delivered bool `json:"delivered"`
	// PackageReady is true while delivery.zip can be downloaded: the run stands
	// at acceptance with a current export revision.
	PackageReady   bool   `json:"package_ready"`
	PreviewID      string `json:"preview_id,omitempty"`
	PreviewStatus  string `json:"preview_status,omitempty"`
	PreviewCurrent bool   `json:"preview_is_current"`
	PreviewPreset  string `json:"preview_preset"`
	Spec           string `json:"spec"`
}

// AutoDecisionPrompt is the open question of a run that waits for the person
// ("待你确认" in the UI). It is present only while the run is waiting.
type AutoDecisionPrompt struct {
	// Reason is why the run waits: medium, exhausted or asset_busy.
	Reason string `json:"reason"`
	// CanExport is false when there is no delivered export to release; the
	// person can still revise or discard.
	CanExport bool `json:"can_export"`
	// CanRevise is false when the round, action or TTS budget is spent.
	CanRevise  bool       `json:"can_revise"`
	DeadlineAt *time.Time `json:"deadline_at,omitempty"`
}

// autoDeliveryOf looks at the run's current export and its newest 720p preview.
func (s *Service) autoDeliveryOf(ctx context.Context, runID string) (AutoDelivery, error) {
	return s.autoDeliveryAt(ctx, s.st, runID)
}

// autoDeliveryAt reads through st, so a transaction can ask inside itself.
func (s *Service) autoDeliveryAt(ctx context.Context, st *store.Store, runID string) (AutoDelivery, error) {
	d := AutoDelivery{PreviewPreset: preview.PresetID, Spec: "720p"}
	run, err := st.GetWorkflow(ctx, runID)
	if err != nil {
		return d, err
	}
	exportRev, err := s.previewRevisionID(ctx, st, runID)
	if err != nil {
		return d, err
	}
	if _, blocked, err := st.DeliveryInvalidation(ctx, run.AssetID); err != nil {
		return d, err
	} else if blocked {
		return d, nil
	}
	d.PackageReady = exportRev != "" && run.Status == model.WorkflowReadyAcceptance
	if exportRev == "" {
		return d, nil
	}
	jobs, err := st.ListPreviewJobs(ctx, runID)
	if err != nil {
		return d, err
	}
	for _, job := range jobs { // newest first
		if job.Invalidated || job.RevisionID != exportRev {
			continue
		}
		view, err := s.projectPreviewViewAt(ctx, st, job, exportRev)
		if err != nil {
			return d, err
		}
		if view.PresetID != preview.PresetID || view.SubtitleVariantID != "" {
			continue
		}
		d.PreviewID, d.PreviewStatus, d.PreviewCurrent = view.PreviewID, view.Status, view.IsCurrent
		break
	}
	d.Delivered = d.PackageReady && d.PreviewCurrent && d.PreviewStatus == model.TaskStatusSucceeded
	return d, nil
}

// fillAutoView adds the parts of the read model that need the preview and the
// run: what was delivered, and what the person is being asked.
func (s *Service) fillAutoView(ctx context.Context, v *AutoView) error {
	d, err := s.autoDeliveryOf(ctx, v.RunID)
	if err != nil {
		return err
	}
	v.Delivery = d
	v.DeliveryHoldEnforced = false
	if v.State == model.AutoStateAwaitingDecision {
		v.Decision = &AutoDecisionPrompt{
			Reason:     v.HaltReason,
			CanExport:  d.Delivered,
			CanRevise:  autoCanRevise(v.WorkflowAuto),
			DeadlineAt: v.DecisionDeadlineAt,
		}
	}
	return nil
}

func autoCanRevise(a model.WorkflowAuto) bool {
	return a.TotalRounds < a.MaxTotalRounds && a.ActionsUsed < a.Scope.MaxActions && a.TTSCharsUsed < a.Scope.MaxTTSChars
}
