package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func (s *Store) InsertRevision(ctx context.Context, rev model.Revision) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO revisions
  (revision_id, asset_id, parent_revision_id, schema_version, package_ref, edl_sha256,
   evidence_manifest_sha256, created_at, created_by, reason)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rev.RevisionID, rev.AssetID, nullIfEmpty(rev.ParentRevisionID), rev.SchemaVersion, rev.PackageRef,
		rev.EDLSHA256, rev.EvidenceManifestSHA256, rev.CreatedAt, rev.CreatedBy, rev.Reason)
	if err != nil {
		return fmt.Errorf("store: insert revision %s: %w", rev.RevisionID, err)
	}
	return nil
}

func (s *Store) GetRevision(ctx context.Context, id string) (model.Revision, error) {
	row := s.q.QueryRowContext(ctx, `SELECT revision_id, asset_id, parent_revision_id, schema_version,
  package_ref, edl_sha256, evidence_manifest_sha256, created_at, created_by, reason
  FROM revisions WHERE revision_id = ?`, id)
	rev, err := scanRevision(row)
	if err != nil {
		return model.Revision{}, fmt.Errorf("store: get revision %s: %w", id, err)
	}
	return rev, nil
}

func scanRevision(row scanner) (model.Revision, error) {
	var rev model.Revision
	var parent sql.NullString
	err := row.Scan(&rev.RevisionID, &rev.AssetID, &parent, &rev.SchemaVersion, &rev.PackageRef,
		&rev.EDLSHA256, &rev.EvidenceManifestSHA256, &rev.CreatedAt, &rev.CreatedBy, &rev.Reason)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.Revision{}, model.ErrNotFound
		}
		return model.Revision{}, err
	}
	if parent.Valid {
		rev.ParentRevisionID = parent.String
	}
	return rev, nil
}

func (s *Store) InsertWorkflow(ctx context.Context, run model.WorkflowRun) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO workflow_runs
  (run_id, asset_id, base_revision_id, current_revision_id, status, stage, version, content_mode,
   created_at, updated_at, error_code, blocked_reason)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.RunID, run.AssetID, run.BaseRevisionID, run.CurrentRevisionID, run.Status, run.Stage, run.Version,
		run.ContentMode, run.CreatedAt, run.UpdatedAt, run.ErrorCode, run.BlockedReason)
	if err != nil {
		return fmt.Errorf("store: insert workflow %s: %w", run.RunID, err)
	}
	return nil
}

func (s *Store) UpdateWorkflow(ctx context.Context, run model.WorkflowRun) error {
	res, err := s.q.ExecContext(ctx, `UPDATE workflow_runs SET
  current_revision_id = ?, status = ?, stage = ?, version = ?, updated_at = ?, error_code = ?, blocked_reason = ?
  WHERE run_id = ? AND version = ?`,
		run.CurrentRevisionID, run.Status, run.Stage, run.Version, run.UpdatedAt, run.ErrorCode, run.BlockedReason,
		run.RunID, run.Version-1)
	if err != nil {
		return fmt.Errorf("store: update workflow %s: %w", run.RunID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("store: update workflow %s: %w", run.RunID, model.ErrConflict)
	}
	return nil
}

func (s *Store) GetWorkflow(ctx context.Context, id string) (model.WorkflowRun, error) {
	row := s.q.QueryRowContext(ctx, `SELECT run_id, asset_id, base_revision_id, current_revision_id, status, stage,
  version, content_mode, created_at, updated_at, error_code, blocked_reason
  FROM workflow_runs WHERE run_id = ?`, id)
	return scanWorkflow(row, id)
}

func (s *Store) ActiveWorkflow(ctx context.Context, assetID string) (model.WorkflowRun, error) {
	row := s.q.QueryRowContext(ctx, `SELECT run_id, asset_id, base_revision_id, current_revision_id, status, stage,
  version, content_mode, created_at, updated_at, error_code, blocked_reason
  FROM workflow_runs WHERE asset_id = ? AND status IN ('created','running','awaiting_review','failed')`, assetID)
	return scanWorkflow(row, assetID)
}

func (s *Store) ListWorkflows(ctx context.Context, assetID string, limit int) ([]model.WorkflowRun, error) {
	return s.ListWorkflowsPage(ctx, assetID, limit, 0)
}
func (s *Store) ListWorkflowsPage(ctx context.Context, assetID string, limit, offset int) ([]model.WorkflowRun, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.q.QueryContext(ctx, `SELECT run_id, asset_id, base_revision_id, current_revision_id, status, stage,
  version, content_mode, created_at, updated_at, error_code, blocked_reason
  FROM workflow_runs WHERE asset_id = ? ORDER BY created_at DESC,run_id DESC LIMIT ? OFFSET ?`, assetID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []model.WorkflowRun{}
	for rows.Next() {
		run, err := scanWorkflow(rows, assetID)
		if err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func scanWorkflow(row scanner, id string) (model.WorkflowRun, error) {
	var run model.WorkflowRun
	err := row.Scan(&run.RunID, &run.AssetID, &run.BaseRevisionID, &run.CurrentRevisionID, &run.Status, &run.Stage,
		&run.Version, &run.ContentMode, &run.CreatedAt, &run.UpdatedAt, &run.ErrorCode, &run.BlockedReason)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.WorkflowRun{}, fmt.Errorf("store: workflow %s: %w", id, model.ErrNotFound)
		}
		return model.WorkflowRun{}, err
	}
	return run, nil
}

func (s *Store) InsertStage(ctx context.Context, stg model.WorkflowStage) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO workflow_stages
  (run_id, task_id, stage, input_revision_id, output_revision_id, attempt_of, invalidated)
  VALUES (?, ?, ?, ?, ?, ?, ?)`,
		stg.RunID, stg.TaskID, stg.Stage, stg.InputRevisionID, stg.OutputRevisionID, stg.AttemptOf, boolToInt(stg.Invalidated))
	if err != nil {
		return fmt.Errorf("store: insert stage %s: %w", stg.TaskID, err)
	}
	return nil
}

func (s *Store) StageByTask(ctx context.Context, taskID string) (model.WorkflowStage, error) {
	row := s.q.QueryRowContext(ctx, `SELECT run_id, task_id, stage, input_revision_id, output_revision_id, attempt_of, invalidated
  FROM workflow_stages WHERE task_id = ?`, taskID)
	var stg model.WorkflowStage
	var invalidated int
	err := row.Scan(&stg.RunID, &stg.TaskID, &stg.Stage, &stg.InputRevisionID, &stg.OutputRevisionID, &stg.AttemptOf, &invalidated)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.WorkflowStage{}, model.ErrNotFound
		}
		return model.WorkflowStage{}, err
	}
	stg.Invalidated = invalidated != 0
	return stg, nil
}

func (s *Store) ListStages(ctx context.Context, runID string) ([]model.WorkflowStage, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT run_id, task_id, stage, input_revision_id, output_revision_id, attempt_of, invalidated
  FROM workflow_stages WHERE run_id = ? ORDER BY task_id`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []model.WorkflowStage{}
	for rows.Next() {
		var stg model.WorkflowStage
		var invalidated int
		if err := rows.Scan(&stg.RunID, &stg.TaskID, &stg.Stage, &stg.InputRevisionID, &stg.OutputRevisionID, &stg.AttemptOf, &invalidated); err != nil {
			return nil, err
		}
		stg.Invalidated = invalidated != 0
		out = append(out, stg)
	}
	return out, rows.Err()
}

func (s *Store) SetStageOutput(ctx context.Context, taskID, outputRevision string) error {
	_, err := s.q.ExecContext(ctx, `UPDATE workflow_stages SET output_revision_id = ? WHERE task_id = ?`, outputRevision, taskID)
	return err
}

func (s *Store) InvalidateQueuedStages(ctx context.Context, runID string, taskIDs []string) error {
	for _, id := range taskIDs {
		if _, err := s.q.ExecContext(ctx, `UPDATE workflow_stages SET invalidated = 1 WHERE run_id = ? AND task_id = ?`, runID, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) UpsertSceneReview(ctx context.Context, rev model.SceneReview) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO scene_reviews
  (run_id, revision_id, scene_id, decision, note, evidence_sha256, actor, created_at)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?)
  ON CONFLICT(run_id, revision_id, scene_id) DO UPDATE SET
    decision = excluded.decision, note = excluded.note, evidence_sha256 = excluded.evidence_sha256,
    actor = excluded.actor, created_at = excluded.created_at`,
		rev.RunID, rev.RevisionID, rev.SceneID, rev.Decision, rev.Note, rev.EvidenceSHA256, rev.Actor, rev.CreatedAt)
	return err
}

func (s *Store) ListSceneReviews(ctx context.Context, runID, revisionID string) ([]model.SceneReview, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT run_id, revision_id, scene_id, decision, note, evidence_sha256, actor, created_at
  FROM scene_reviews WHERE run_id = ? AND revision_id = ?`, runID, revisionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []model.SceneReview{}
	for rows.Next() {
		var rev model.SceneReview
		if err := rows.Scan(&rev.RunID, &rev.RevisionID, &rev.SceneID, &rev.Decision, &rev.Note, &rev.EvidenceSHA256, &rev.Actor, &rev.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rev)
	}
	return out, rows.Err()
}

func (s *Store) InsertNarrationApproval(ctx context.Context, runID, revisionID, narrationID, hash, actor string, at time.Time) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO workflow_narration_approvals
  (run_id, revision_id, narration_id, draft_hash, actor, created_at) VALUES (?, ?, ?, ?, ?, ?)
  ON CONFLICT(run_id, revision_id, narration_id) DO UPDATE SET draft_hash = excluded.draft_hash, actor = excluded.actor, created_at = excluded.created_at`,
		runID, revisionID, narrationID, hash, actor, at)
	return err
}

func (s *Store) DeleteWorkflowNarrationApproval(ctx context.Context, runID, revisionID, narrationID string) (bool, error) {
	res, err := s.q.ExecContext(ctx, `DELETE FROM workflow_narration_approvals
  WHERE run_id = ? AND revision_id = ? AND narration_id = ?`, runID, revisionID, narrationID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (s *Store) ListNarrationApprovalIDs(ctx context.Context, runID, revisionID string) (map[string]string, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT narration_id, draft_hash FROM workflow_narration_approvals
  WHERE run_id = ? AND revision_id = ?`, runID, revisionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]string{}
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			return nil, err
		}
		out[id] = hash
	}
	return out, rows.Err()
}

func (s *Store) InsertAcceptance(ctx context.Context, rec model.AcceptanceRecord) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO acceptance_records
  (run_id, export_revision_id, manifest_sha256, check_item, result, actor, note, created_at)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.RunID, rec.ExportRevisionID, rec.ManifestSHA256, rec.CheckItem, rec.Result, rec.Actor, rec.Note, rec.CreatedAt)
	return err
}

func (s *Store) ListAcceptance(ctx context.Context, runID string) ([]model.AcceptanceRecord, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT id, run_id, export_revision_id, manifest_sha256, check_item, result, actor, note, created_at
  FROM acceptance_records WHERE run_id = ? ORDER BY id`, runID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []model.AcceptanceRecord{}
	for rows.Next() {
		var rec model.AcceptanceRecord
		if err := rows.Scan(&rec.ID, &rec.RunID, &rec.ExportRevisionID, &rec.ManifestSHA256, &rec.CheckItem, &rec.Result, &rec.Actor, &rec.Note, &rec.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func (s *Store) GetIdempotency(ctx context.Context, scope, key string) (string, string, error) {
	var hash, body string
	err := s.q.QueryRowContext(ctx, `SELECT request_hash, response_json FROM idempotency_keys WHERE scope = ? AND idem_key = ?`, scope, key).Scan(&hash, &body)
	if err == sql.ErrNoRows {
		return "", "", model.ErrNotFound
	}
	return hash, body, err
}

func (s *Store) PutIdempotency(ctx context.Context, scope, key, hash, body string) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO idempotency_keys (scope, idem_key, request_hash, response_json, created_at)
  VALUES (?, ?, ?, ?, ?)`, scope, key, hash, body, time.Now().UTC())
	return err
}

func (s *Store) DeleteRunNarrationApproval(ctx context.Context, runID, narrationID string) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM workflow_narration_approvals WHERE run_id=? AND narration_id=?`, runID, narrationID)
	return err
}

func (s *Store) RevisionPackages(ctx context.Context) (map[string]bool, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT package_ref FROM revisions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out[p] = true
	}
	return out, rows.Err()
}
