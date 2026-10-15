package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/autoreview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/preview"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// Auto-run mode, slice B2: the self-assessment. ScoreAutoRun gathers what the
// pure scorer in internal/autoreview needs from rows and files that are
// already stored, runs it, and records the result. It does not change the
// run, move the auto state or start anything: what a band leads to (complete,
// wait for the user, rerun) belongs to the driver that arrives with B3.

const maxPreviewReportBytes = 1 << 20

// ScoreAutoRun scores the preview of an auto run's current export and stores
// the review. autoVersion is the caller's view of workflow_auto.auto_version;
// a stale one is an ErrConflict.
//
// The review row, its ledger row (kind "score", counted in actions_used) and
// the auto_version bump commit in one transaction, so a review exists if and
// only if its action was counted. Scoring the same preview twice in one round
// returns the stored review and writes nothing.
//
// A preview that is still queued, claimed or running is not scored
// (ErrInvalidState): the driver waits for it. A failed preview is scored LOW
// with a non-improvable reason. A cancelled one is refused. A preview that is
// not the current export of the run is an ErrConflict.
func (s *Service) ScoreAutoRun(ctx context.Context, runID, previewID string, autoVersion int) (model.AutoReview, error) {
	a, err := s.st.GetWorkflowAuto(ctx, runID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.AutoReview{}, fmt.Errorf("service: run %s is not an auto run: %w", runID, model.ErrForbidden)
		}
		return model.AutoReview{}, err
	}
	// An already stored review is returned before any other check: the
	// first scoring bumped auto_version, so a replay holds an older one.
	if prior, err := s.st.GetAutoReviewForPreview(ctx, runID, a.Round, previewID); err == nil {
		return prior, nil
	} else if !errors.Is(err, model.ErrNotFound) {
		return model.AutoReview{}, err
	}
	if a.AutoVersion != autoVersion {
		return model.AutoReview{}, fmt.Errorf("service: auto version conflict: %w", model.ErrConflict)
	}
	th, err := autoreview.ResolveThresholds(a.Scope.Thresholds)
	if err != nil {
		return model.AutoReview{}, fmt.Errorf("service: stored auto thresholds are invalid (%v): %w", err, model.ErrInvalidState)
	}

	// Files are read before the write lock is taken. Everything they depend on
	// is immutable (a revision, a finished preview); the transaction below
	// proves the preview is still the current one.
	col, err := s.collectAutoScoreInput(ctx, a, previewID)
	if err != nil {
		return model.AutoReview{}, err
	}
	res := autoreview.Score(col.input, th)
	// A review is stored with the numbers that explain it. If they cannot be
	// encoded nothing is written: an empty "{}" would be a review nobody can
	// explain.
	reasons, err := json.Marshal(res.Reasons)
	if err != nil {
		return model.AutoReview{}, fmt.Errorf("service: encode review reasons: %w", err)
	}
	inputs, err := json.Marshal(map[string]any{
		"components": res.Components, "facts": res.Facts, "brief_coverage": res.BriefCoverage,
		"improvable": res.Improvable, "preview_status": col.input.Preview.Status,
		"feedback_template_version": autoreview.FeedbackTemplateVersion,
	})
	if err != nil {
		return model.AutoReview{}, fmt.Errorf("service: encode review inputs: %w", err)
	}
	thresholds, err := json.Marshal(res.Thresholds)
	if err != nil {
		return model.AutoReview{}, fmt.Errorf("service: encode review thresholds: %w", err)
	}

	var saved model.AutoReview
	var savedAuto model.WorkflowAuto
	err = s.st.Transaction(ctx, func(tx *store.Store) error {
		cur, err := tx.GetWorkflowAuto(ctx, runID)
		if err != nil {
			return err
		}
		now := s.autoNow()
		key := fmt.Sprintf("auto:%s:%d:%s:%s", runID, cur.Round, model.AutoActionScore, previewID)
		if done, err := tx.AutoActionExists(ctx, runID, key); err != nil {
			return err
		} else if done {
			prior, err := tx.GetAutoReviewForPreview(ctx, runID, cur.Round, previewID)
			if err != nil {
				return err
			}
			saved = prior
			return nil
		}
		switch {
		case cur.State != model.AutoStateActive:
			return fmt.Errorf("service: auto run is %s: %w", cur.State, model.ErrForbidden)
		case now.After(cur.DeadlineAt):
			return fmt.Errorf("service: auto run is past its deadline: %w", model.ErrForbidden)
		case cur.ActionsUsed >= cur.Scope.MaxActions:
			return fmt.Errorf("service: auto action budget is spent: %w", model.ErrForbidden)
		case cur.AutoVersion != autoVersion || cur.Round != a.Round:
			return fmt.Errorf("service: auto version conflict: %w", model.ErrConflict)
		}
		run, err := tx.GetWorkflow(ctx, runID)
		if err != nil {
			return err
		}
		if run.Status == model.WorkflowCancelled {
			return fmt.Errorf("service: workflow is cancelled: %w", model.ErrInvalidState)
		}
		job, err := tx.GetPreviewJob(ctx, previewID)
		if err != nil {
			return err
		}
		latest, err := s.previewRevisionID(ctx, tx, runID)
		if err != nil {
			return err
		}
		view, err := s.projectPreviewViewAt(ctx, tx, job, latest)
		if err != nil {
			return err
		}
		if job.RunID != runID || !view.IsCurrent || view.RevisionID != col.revisionID ||
			view.InputSHA256 != col.inputSHA || view.Status != col.status {
			return fmt.Errorf("service: preview changed while it was being scored: %w", model.ErrConflict)
		}
		saved = model.AutoReview{
			ReviewID: newID("arv"), RunID: runID, Round: cur.Round, RevisionID: col.revisionID, PreviewID: previewID,
			ScorerVersion: res.ScorerVersion, Score: res.Score, Band: res.Band,
			Reasons: reasons, Inputs: inputs, Thresholds: thresholds, CreatedAt: now,
		}
		if err := tx.InsertAutoReview(ctx, saved); err != nil {
			return err
		}
		if _, err := tx.InsertAutoAction(ctx, runID, key, model.AutoActionScore, previewID, now); err != nil {
			return err
		}
		cur.ActionsUsed++
		cur.AutoVersion++
		cur.UpdatedAt = now
		if err := tx.UpdateWorkflowAuto(ctx, cur); err != nil {
			return err
		}
		if err := auditTx(ctx, tx, model.AutoActor, "auto.review", runID,
			fmt.Sprintf("preview=%s band=%s score=%.4f scorer=%s", previewID, saved.Band, saved.Score, saved.ScorerVersion)); err != nil {
			return err
		}
		savedAuto = cur
		return nil
	})
	if err != nil {
		return model.AutoReview{}, err
	}
	if savedAuto.RunID != "" {
		s.publish("auto.reviewed", map[string]any{
			"run_id": saved.RunID, "review_id": saved.ReviewID, "round": saved.Round, "preview_id": saved.PreviewID,
			"band": saved.Band, "score": saved.Score, "scorer_version": saved.ScorerVersion,
		})
		s.publishAuto(savedAuto)
	}
	return saved, nil
}

type autoScoreInput struct {
	input      autoreview.Input
	revisionID string
	inputSHA   string
	status     string
}

// collectAutoScoreInput reads the revision's EDL, the preview and its render
// report. It reads stored data only: no worker is called and no text from the
// model is interpreted.
func (s *Service) collectAutoScoreInput(ctx context.Context, a model.WorkflowAuto, previewID string) (autoScoreInput, error) {
	job, err := s.st.GetPreviewJob(ctx, previewID)
	if err != nil {
		return autoScoreInput{}, err
	}
	if job.RunID != a.RunID {
		return autoScoreInput{}, fmt.Errorf("service: preview %s does not belong to run %s: %w", previewID, a.RunID, model.ErrNotFound)
	}
	latest, err := s.getLatestExportRevisionID(ctx, a.RunID)
	if err != nil {
		return autoScoreInput{}, err
	}
	view, err := s.projectPreviewView(ctx, job, latest)
	if err != nil {
		return autoScoreInput{}, err
	}
	if !view.IsCurrent {
		return autoScoreInput{}, fmt.Errorf("service: preview %s is not the current export's preview: %w", previewID, model.ErrConflict)
	}
	out := autoScoreInput{revisionID: view.RevisionID, inputSHA: view.InputSHA256, status: view.Status}
	switch view.Status {
	case model.TaskStatusSucceeded, model.TaskStatusFailed:
	case model.TaskStatusCancelled:
		return autoScoreInput{}, fmt.Errorf("service: preview %s was cancelled: %w", previewID, model.ErrInvalidState)
	default:
		return autoScoreInput{}, fmt.Errorf("service: preview %s is %s, not finished: %w", previewID, view.Status, model.ErrInvalidState)
	}

	rev, err := s.st.GetRevision(ctx, view.RevisionID)
	if err != nil {
		return autoScoreInput{}, err
	}
	edl, err := s.readRevision(rev)
	if err != nil {
		return autoScoreInput{}, err
	}
	pkg := filepath.FromSlash(rev.PackageRef)
	facts := autoreview.FromEDL(edl, func(rel string) bool {
		_, _, err := s.deliveryPath(filepath.Join(pkg, filepath.FromSlash(rel)), false)
		return err == nil
	})
	out.input = autoreview.Input{
		Scenes: facts.Scenes, Lines: facts.Lines, EDLDurationSeconds: facts.EDLDurationSeconds,
		Brief: a.Brief, Preview: &autoreview.Preview{Status: view.Status},
	}
	if view.Status != model.TaskStatusSucceeded {
		return out, nil
	}
	if view.Media != nil {
		out.input.Preview.Media = &autoreview.Media{DurationSeconds: view.Media.DurationSeconds}
	}
	if rep, ok := s.readPreviewReport(ctx, previewID); ok {
		for _, c := range rep.Checks {
			out.input.Preview.Checks = append(out.input.Preview.Checks, autoreview.Check{Code: c.Code, Status: c.Status})
		}
		if out.input.Preview.Media == nil {
			out.input.Preview.Media = &autoreview.Media{DurationSeconds: rep.Media.DurationSeconds}
		}
	}
	return out, nil
}

// readPreviewReport loads the render report of a finished preview. An
// unreadable report is reported as absent, which the scorer treats as missing
// input and so never as a pass.
func (s *Service) readPreviewReport(ctx context.Context, previewID string) (preview.Report, bool) {
	f, err := s.ResolvePreviewFile(ctx, previewID, "report")
	if err != nil {
		return preview.Report{}, false
	}
	fh, err := os.Open(f.AbsolutePath)
	if err != nil {
		return preview.Report{}, false
	}
	defer func() { _ = fh.Close() }()
	raw, err := io.ReadAll(io.LimitReader(fh, maxPreviewReportBytes+1))
	if err != nil || len(raw) > maxPreviewReportBytes {
		return preview.Report{}, false
	}
	var rep preview.Report
	if json.Unmarshal(raw, &rep) != nil {
		return preview.Report{}, false
	}
	return rep, true
}
