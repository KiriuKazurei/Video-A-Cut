package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

const workflowAutoColumns = `run_id, auto_version, state, halt_reason, brief, brief_sha256, authorized_by, authorized_at,
  revoked_by, revoked_at, revoke_reason, consent_at_revoke, consent_granted_by_run, notice_sha256, scope_json,
  profile_id, profile_revision, profile_sha256, round, max_rounds, total_rounds, max_total_rounds, actions_used,
  tts_chars_used, delivery_hold, created_at, updated_at, deadline_at, paused_at, decision_deadline_at`

func (s *Store) InsertWorkflowAuto(ctx context.Context, a model.WorkflowAuto) error {
	scope, err := json.Marshal(a.Scope)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, `INSERT INTO workflow_auto (`+workflowAutoColumns+`)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.RunID, a.AutoVersion, a.State, a.HaltReason, a.Brief, a.BriefSHA256, a.AuthorizedBy, a.AuthorizedAt,
		a.RevokedBy, nullTime(a.RevokedAt), a.RevokeReason, a.ConsentAtRevoke, a.ConsentGrantedByRun, a.NoticeSHA256, string(scope),
		a.ProfileID, a.ProfileRevision, a.ProfileSHA256, a.Round, a.MaxRounds, a.TotalRounds, a.MaxTotalRounds, a.ActionsUsed,
		a.TTSCharsUsed, a.DeliveryHold, a.CreatedAt, a.UpdatedAt, a.DeadlineAt, nullTime(a.PausedAt), nullTime(a.DecisionDeadlineAt))
	if err != nil {
		return fmt.Errorf("store: insert workflow auto %s: %w", a.RunID, err)
	}
	return nil
}

func (s *Store) GetWorkflowAuto(ctx context.Context, runID string) (model.WorkflowAuto, error) {
	a, err := scanWorkflowAuto(s.q.QueryRowContext(ctx, `SELECT `+workflowAutoColumns+` FROM workflow_auto WHERE run_id = ?`, runID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.WorkflowAuto{}, fmt.Errorf("store: get workflow auto %s: %w", runID, model.ErrNotFound)
		}
		return model.WorkflowAuto{}, fmt.Errorf("store: get workflow auto %s: %w", runID, err)
	}
	return a, nil
}

func scanWorkflowAuto(row scanner) (model.WorkflowAuto, error) {
	var a model.WorkflowAuto
	var scope string
	var revokedAt, pausedAt, decisionAt sql.NullTime
	err := row.Scan(&a.RunID, &a.AutoVersion, &a.State, &a.HaltReason, &a.Brief, &a.BriefSHA256, &a.AuthorizedBy, &a.AuthorizedAt,
		&a.RevokedBy, &revokedAt, &a.RevokeReason, &a.ConsentAtRevoke, &a.ConsentGrantedByRun, &a.NoticeSHA256, &scope,
		&a.ProfileID, &a.ProfileRevision, &a.ProfileSHA256, &a.Round, &a.MaxRounds, &a.TotalRounds, &a.MaxTotalRounds, &a.ActionsUsed,
		&a.TTSCharsUsed, &a.DeliveryHold, &a.CreatedAt, &a.UpdatedAt, &a.DeadlineAt, &pausedAt, &decisionAt)
	if err != nil {
		return model.WorkflowAuto{}, err
	}
	if err := json.Unmarshal([]byte(scope), &a.Scope); err != nil {
		return model.WorkflowAuto{}, fmt.Errorf("store: workflow auto %s scope: %w", a.RunID, err)
	}
	a.RevokedAt, a.PausedAt, a.DecisionDeadlineAt = scanNullTime(revokedAt), scanNullTime(pausedAt), scanNullTime(decisionAt)
	return a, nil
}

// UpdateWorkflowAuto writes the mutable columns when auto_version still equals
// a.AutoVersion-1. The caller bumps AutoVersion first, the same convention as
// UpdateWorkflow, so a stale writer gets ErrConflict instead of overwriting.
func (s *Store) UpdateWorkflowAuto(ctx context.Context, a model.WorkflowAuto) error {
	res, err := s.q.ExecContext(ctx, `UPDATE workflow_auto SET
  auto_version = ?, state = ?, halt_reason = ?, revoked_by = ?, revoked_at = ?, revoke_reason = ?, consent_at_revoke = ?,
  round = ?, total_rounds = ?, actions_used = ?, tts_chars_used = ?, delivery_hold = ?, updated_at = ?,
  deadline_at = ?, paused_at = ?, decision_deadline_at = ?
  WHERE run_id = ? AND auto_version = ?`,
		a.AutoVersion, a.State, a.HaltReason, a.RevokedBy, nullTime(a.RevokedAt), a.RevokeReason, a.ConsentAtRevoke,
		a.Round, a.TotalRounds, a.ActionsUsed, a.TTSCharsUsed, a.DeliveryHold, a.UpdatedAt,
		a.DeadlineAt, nullTime(a.PausedAt), nullTime(a.DecisionDeadlineAt),
		a.RunID, a.AutoVersion-1)
	if err != nil {
		return fmt.Errorf("store: update workflow auto %s: %w", a.RunID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("store: update workflow auto %s: %w", a.RunID, model.ErrConflict)
	}
	return nil
}

// ActiveAutoForAsset reports an auto run on the asset whose state is still
// live (active or waiting for the user). workflow_auto has no asset_id, so the
// lookup joins workflow_runs.
func (s *Store) ActiveAutoForAsset(ctx context.Context, assetID string) (string, bool, error) {
	var runID string
	err := s.q.QueryRowContext(ctx, `SELECT a.run_id FROM workflow_auto a JOIN workflow_runs r ON r.run_id = a.run_id
  WHERE r.asset_id = ? AND a.state IN (?, ?) ORDER BY a.created_at DESC LIMIT 1`,
		assetID, model.AutoStateActive, model.AutoStateAwaitingDecision).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: active auto for asset %s: %w", assetID, err)
	}
	return runID, true, nil
}

func (s *Store) InsertAutoRound(ctx context.Context, r model.AutoRound) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO workflow_auto_rounds
  (run_id, round, base_revision_id, feedback, started_at, ended_at, outcome) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.RunID, r.Round, r.BaseRevisionID, r.Feedback, r.StartedAt, nullTime(r.EndedAt), r.Outcome)
	if err != nil {
		return fmt.Errorf("store: insert auto round %s/%d: %w", r.RunID, r.Round, err)
	}
	return nil
}

func (s *Store) ListAutoRounds(ctx context.Context, runID string) ([]model.AutoRound, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT run_id, round, base_revision_id, feedback, started_at, ended_at, outcome
  FROM workflow_auto_rounds WHERE run_id = ? ORDER BY round`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: list auto rounds: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []model.AutoRound{}
	for rows.Next() {
		var r model.AutoRound
		var ended sql.NullTime
		if err := rows.Scan(&r.RunID, &r.Round, &r.BaseRevisionID, &r.Feedback, &r.StartedAt, &ended, &r.Outcome); err != nil {
			return nil, err
		}
		r.EndedAt = scanNullTime(ended)
		out = append(out, r)
	}
	return out, rows.Err()
}

// InsertAutoReview stores one self-assessment. Reviews are append-only.
func (s *Store) InsertAutoReview(ctx context.Context, r model.AutoReview) error {
	reasons, inputs, thresholds := string(r.Reasons), string(r.Inputs), string(r.Thresholds)
	if reasons == "" {
		reasons = "[]"
	}
	if inputs == "" {
		inputs = "{}"
	}
	if thresholds == "" {
		thresholds = "{}"
	}
	_, err := s.q.ExecContext(ctx, `INSERT INTO auto_reviews
  (review_id, run_id, round, revision_id, preview_id, scorer_version, score, band, reasons_json, inputs_json, thresholds_json, created_at)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ReviewID, r.RunID, r.Round, r.RevisionID, r.PreviewID, r.ScorerVersion, r.Score, r.Band, reasons, inputs, thresholds, r.CreatedAt)
	if err != nil {
		return fmt.Errorf("store: insert auto review %s: %w", r.ReviewID, err)
	}
	return nil
}

// GetAutoReviewForPreview returns the first review a preview received in a
// round, or ErrNotFound.
func (s *Store) GetAutoReviewForPreview(ctx context.Context, runID string, round int, previewID string) (model.AutoReview, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT review_id, run_id, round, revision_id, preview_id, scorer_version, score, band,
  reasons_json, inputs_json, thresholds_json, created_at FROM auto_reviews
  WHERE run_id = ? AND round = ? AND preview_id = ? ORDER BY created_at, review_id LIMIT 1`, runID, round, previewID)
	if err != nil {
		return model.AutoReview{}, fmt.Errorf("store: get auto review: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return model.AutoReview{}, err
		}
		return model.AutoReview{}, fmt.Errorf("store: auto review of preview %s: %w", previewID, model.ErrNotFound)
	}
	var r model.AutoReview
	var reasons, inputs, thresholds string
	if err := rows.Scan(&r.ReviewID, &r.RunID, &r.Round, &r.RevisionID, &r.PreviewID, &r.ScorerVersion, &r.Score, &r.Band,
		&reasons, &inputs, &thresholds, &r.CreatedAt); err != nil {
		return model.AutoReview{}, err
	}
	r.Reasons, r.Inputs, r.Thresholds = json.RawMessage(reasons), json.RawMessage(inputs), json.RawMessage(thresholds)
	return r, nil
}

func (s *Store) ListAutoReviews(ctx context.Context, runID string) ([]model.AutoReview, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT review_id, run_id, round, revision_id, preview_id, scorer_version, score, band,
  reasons_json, inputs_json, thresholds_json, created_at FROM auto_reviews WHERE run_id = ? ORDER BY created_at, review_id`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: list auto reviews: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []model.AutoReview{}
	for rows.Next() {
		var r model.AutoReview
		var reasons, inputs, thresholds string
		if err := rows.Scan(&r.ReviewID, &r.RunID, &r.Round, &r.RevisionID, &r.PreviewID, &r.ScorerVersion, &r.Score, &r.Band,
			&reasons, &inputs, &thresholds, &r.CreatedAt); err != nil {
			return nil, err
		}
		r.Reasons, r.Inputs, r.Thresholds = json.RawMessage(reasons), json.RawMessage(inputs), json.RawMessage(thresholds)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) InsertAutoDecision(ctx context.Context, d model.AutoDecision) error {
	basis := string(d.Basis)
	if basis == "" {
		basis = "{}"
	}
	_, err := s.q.ExecContext(ctx, `INSERT INTO auto_decisions (run_id, round, kind, target, actor, basis_json, created_at)
  VALUES (?, ?, ?, ?, ?, ?, ?)`, d.RunID, d.Round, d.Kind, d.Target, d.Actor, basis, d.CreatedAt)
	if err != nil {
		return fmt.Errorf("store: insert auto decision %s/%s: %w", d.RunID, d.Kind, err)
	}
	return nil
}

func (s *Store) ListAutoDecisions(ctx context.Context, runID string) ([]model.AutoDecision, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id, run_id, round, kind, target, actor, basis_json, created_at
  FROM auto_decisions WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: list auto decisions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := []model.AutoDecision{}
	for rows.Next() {
		var d model.AutoDecision
		var basis string
		if err := rows.Scan(&d.ID, &d.RunID, &d.Round, &d.Kind, &d.Target, &d.Actor, &basis, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.Basis = json.RawMessage(basis)
		out = append(out, d)
	}
	return out, rows.Err()
}

// InsertAutoAction records one ledger row and reports whether it was new. A
// false result means the action key was already recorded, i.e. the action
// committed before and must not be repeated.
func (s *Store) InsertAutoAction(ctx context.Context, runID, key, kind, target string, at time.Time) (bool, error) {
	res, err := s.q.ExecContext(ctx, `INSERT OR IGNORE INTO auto_actions (run_id, action_key, kind, target, created_at)
  VALUES (?, ?, ?, ?, ?)`, runID, key, kind, target, at)
	if err != nil {
		return false, fmt.Errorf("store: insert auto action %s: %w", key, err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) AutoActionExists(ctx context.Context, runID, key string) (bool, error) {
	var n int
	err := s.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM auto_actions WHERE run_id = ? AND action_key = ?`, runID, key).Scan(&n)
	return n > 0, err
}

func (s *Store) CountAutoActions(ctx context.Context, runID string) (int, error) {
	var n int
	err := s.q.QueryRowContext(ctx, `SELECT COUNT(*) FROM auto_actions WHERE run_id = ?`, runID).Scan(&n)
	return n, err
}

// UpsertSceneReviewAuto is the guarded counterpart of UpsertSceneReview: on a
// conflict it overwrites only a row an auto-reviewer wrote itself. A row a
// human decided is left alone and applied=false is returned, so the caller
// treats the scene as already satisfied instead of overwriting the human.
func (s *Store) UpsertSceneReviewAuto(ctx context.Context, rev model.SceneReview) (bool, error) {
	res, err := s.q.ExecContext(ctx, `INSERT INTO scene_reviews
  (run_id, revision_id, scene_id, decision, note, evidence_sha256, actor, created_at)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?)
  ON CONFLICT(run_id, revision_id, scene_id) DO UPDATE SET
    decision = excluded.decision, note = excluded.note, evidence_sha256 = excluded.evidence_sha256,
    actor = excluded.actor, created_at = excluded.created_at
  WHERE scene_reviews.actor = ?`,
		rev.RunID, rev.RevisionID, rev.SceneID, rev.Decision, rev.Note, rev.EvidenceSHA256, rev.Actor, rev.CreatedAt, model.AutoActor)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
