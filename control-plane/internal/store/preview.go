package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func (s *Store) InsertPreviewJob(ctx context.Context, job model.PreviewJob) error {
	inv := 0
	if job.Invalidated {
		inv = 1
	}
	_, err := s.q.ExecContext(ctx, `INSERT INTO preview_jobs (
		preview_id, task_id, asset_id, run_id, revision_id, request_id,
		expected_version, input_json, input_sha256, preset_sha256,
		invalidated, output_dir, manifest_sha256, media_json,
		created_by, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.PreviewID, job.TaskID, job.AssetID, job.RunID, job.RevisionID, job.RequestID,
		job.ExpectedVersion, job.InputJSON, job.InputSHA256, job.PresetSHA256,
		inv, job.OutputDir, job.ManifestSHA256, job.MediaJSON,
		job.CreatedBy, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: insert preview job %s: %w", job.PreviewID, err)
	}
	return nil
}

func (s *Store) GetPreviewJob(ctx context.Context, previewID string) (model.PreviewJob, error) {
	row := s.q.QueryRowContext(ctx, `SELECT
		preview_id, task_id, asset_id, run_id, revision_id, request_id,
		expected_version, input_json, input_sha256, preset_sha256,
		invalidated, output_dir, manifest_sha256, media_json,
		created_by, created_at, updated_at
	FROM preview_jobs WHERE preview_id = ?`, previewID)
	return scanPreviewJob(row)
}

func (s *Store) GetPreviewJobByTaskID(ctx context.Context, taskID string) (model.PreviewJob, error) {
	row := s.q.QueryRowContext(ctx, `SELECT
		preview_id, task_id, asset_id, run_id, revision_id, request_id,
		expected_version, input_json, input_sha256, preset_sha256,
		invalidated, output_dir, manifest_sha256, media_json,
		created_by, created_at, updated_at
	FROM preview_jobs WHERE task_id = ?`, taskID)
	return scanPreviewJob(row)
}

func (s *Store) GetPreviewJobByRequest(ctx context.Context, runID, requestID string) (model.PreviewJob, error) {
	row := s.q.QueryRowContext(ctx, `SELECT
		pj.preview_id, pj.task_id, pj.asset_id, pj.run_id, pr.revision_id, pr.request_id,
		pr.expected_version, pj.input_json, pj.input_sha256, pr.preset_sha256,
		pj.invalidated, pj.output_dir, pj.manifest_sha256, pj.media_json,
		pj.created_by, pj.created_at, pj.updated_at
	FROM preview_jobs pj JOIN preview_requests pr ON pr.preview_id = pj.preview_id
	WHERE pr.run_id = ? AND pr.request_id = ?`, runID, requestID)
	return scanPreviewJob(row)
}

func (s *Store) BindPreviewRequest(ctx context.Context, runID, requestID, previewID, revisionID string, version int, presetSHA string) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO preview_requests VALUES (?, ?, ?, ?, ?, ?)`, runID, requestID, previewID, revisionID, version, presetSHA)
	return err
}

// FindReusablePreviewJob finds the most recent non-invalidated preview job with identical input for the run/revision
// whose task is still active (queued/claimed/running).
func (s *Store) FindReusablePreviewJob(ctx context.Context, runID, revisionID, inputSHA256 string) (model.PreviewJob, error) {
	row := s.q.QueryRowContext(ctx, `SELECT
		pj.preview_id, pj.task_id, pj.asset_id, pj.run_id, pj.revision_id, pj.request_id,
		pj.expected_version, pj.input_json, pj.input_sha256, pj.preset_sha256,
		pj.invalidated, pj.output_dir, pj.manifest_sha256, pj.media_json,
		pj.created_by, pj.created_at, pj.updated_at
	FROM preview_jobs pj
	JOIN tasks t ON pj.task_id = t.task_id
	WHERE pj.run_id = ? AND pj.revision_id = ? AND pj.input_sha256 = ?
	  AND pj.invalidated = 0
	  AND t.status IN ('queued', 'claimed', 'running')
	ORDER BY pj.created_at DESC, pj.preview_id DESC LIMIT 1`, runID, revisionID, inputSHA256)
	return scanPreviewJob(row)
}

func (s *Store) ListPreviewJobs(ctx context.Context, runID string) ([]model.PreviewJob, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT
		preview_id, task_id, asset_id, run_id, revision_id, request_id,
		expected_version, input_json, input_sha256, preset_sha256,
		invalidated, output_dir, manifest_sha256, media_json,
		created_by, created_at, updated_at
	FROM preview_jobs WHERE run_id = ?
	ORDER BY created_at DESC, preview_id DESC`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: list preview jobs for run %s: %w", runID, err)
	}
	defer rows.Close()

	var jobs []model.PreviewJob
	for rows.Next() {
		job, err := scanPreviewJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return jobs, nil
}

func (s *Store) UpdatePreviewJobSuccess(ctx context.Context, previewID, outputDir, manifestSHA256, mediaJSON string) error {
	now := time.Now().UTC()
	res, err := s.q.ExecContext(ctx, `UPDATE preview_jobs SET
		output_dir = ?, manifest_sha256 = ?, media_json = ?, updated_at = ?
	WHERE preview_id = ?`, outputDir, manifestSHA256, mediaJSON, now, previewID)
	if err != nil {
		return fmt.Errorf("store: update preview job %s success: %w", previewID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return model.ErrNotFound
	}
	return nil
}

func (s *Store) InvalidatePreviewJob(ctx context.Context, previewID string) error {
	now := time.Now().UTC()
	_, err := s.q.ExecContext(ctx, `UPDATE preview_jobs SET invalidated = 1, updated_at = ? WHERE preview_id = ?`, now, previewID)
	if err != nil {
		return fmt.Errorf("store: invalidate preview job %s: %w", previewID, err)
	}
	return nil
}

func (s *Store) InvalidatePreviewJobsByRun(ctx context.Context, runID string) error {
	now := time.Now().UTC()
	_, err := s.q.ExecContext(ctx, `UPDATE preview_jobs SET invalidated = 1, updated_at = ? WHERE run_id = ? AND invalidated = 0`, now, runID)
	if err != nil {
		return fmt.Errorf("store: invalidate preview jobs for run %s: %w", runID, err)
	}
	return nil
}

func (s *Store) InvalidatePreviewJobsByRevision(ctx context.Context, runID, revisionID string) error {
	now := time.Now().UTC()
	_, err := s.q.ExecContext(ctx, `UPDATE preview_jobs SET invalidated = 1, updated_at = ? WHERE run_id = ? AND revision_id = ? AND invalidated = 0`, now, runID, revisionID)
	if err != nil {
		return fmt.Errorf("store: invalidate preview jobs for run %s revision %s: %w", runID, revisionID, err)
	}
	return nil
}

func (s *Store) InvalidatePreviewJobsForAssetExcept(ctx context.Context, assetID, keepRevisionID string) error {
	now := time.Now().UTC()
	_, err := s.q.ExecContext(ctx, `UPDATE preview_jobs SET invalidated = 1, updated_at = ? WHERE asset_id = ? AND revision_id != ? AND invalidated = 0`, now, assetID, keepRevisionID)
	if err != nil {
		return fmt.Errorf("store: invalidate preview jobs for asset %s except %s: %w", assetID, keepRevisionID, err)
	}
	return nil
}

func (s *Store) InsertPreviewArtifacts(ctx context.Context, artifacts []model.PreviewArtifactRecord) error {
	for _, a := range artifacts {
		_, err := s.q.ExecContext(ctx, `INSERT INTO preview_artifacts (
			preview_id, key, kind, path, mime, size, sha256
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(preview_id, key) DO UPDATE SET
			kind = excluded.kind, path = excluded.path, mime = excluded.mime,
			size = excluded.size, sha256 = excluded.sha256`,
			a.PreviewID, a.Key, a.Kind, a.Path, a.MIME, a.Size, a.SHA256,
		)
		if err != nil {
			return fmt.Errorf("store: insert preview artifact (%s, %s): %w", a.PreviewID, a.Key, err)
		}
	}
	return nil
}

func (s *Store) ListPreviewArtifacts(ctx context.Context, previewID string) ([]model.PreviewArtifactRecord, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT
		preview_id, key, kind, path, mime, size, sha256
	FROM preview_artifacts WHERE preview_id = ? ORDER BY key`, previewID)
	if err != nil {
		return nil, fmt.Errorf("store: list preview artifacts for %s: %w", previewID, err)
	}
	defer rows.Close()

	var list []model.PreviewArtifactRecord
	for rows.Next() {
		var a model.PreviewArtifactRecord
		if err := rows.Scan(&a.PreviewID, &a.Key, &a.Kind, &a.Path, &a.MIME, &a.Size, &a.SHA256); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

func (s *Store) GetPreviewArtifact(ctx context.Context, previewID, key string) (model.PreviewArtifactRecord, error) {
	row := s.q.QueryRowContext(ctx, `SELECT
		preview_id, key, kind, path, mime, size, sha256
	FROM preview_artifacts WHERE preview_id = ? AND key = ?`, previewID, key)
	var a model.PreviewArtifactRecord
	err := row.Scan(&a.PreviewID, &a.Key, &a.Kind, &a.Path, &a.MIME, &a.Size, &a.SHA256)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.PreviewArtifactRecord{}, model.ErrNotFound
		}
		return model.PreviewArtifactRecord{}, err
	}
	return a, nil
}

func (s *Store) PutPreviewCapability(ctx context.Context, agentID string, protocolVersion int, ready bool, capJSON string, expiresAt time.Time) error {
	now := time.Now().UTC()
	readyInt := 0
	if ready {
		readyInt = 1
	}
	_, err := s.q.ExecContext(ctx, `INSERT INTO preview_worker_capabilities (
		agent_id, protocol_version, ready, capability_json, observed_at, expires_at
	) VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(agent_id) DO UPDATE SET
		protocol_version = excluded.protocol_version,
		ready = excluded.ready,
		capability_json = excluded.capability_json,
		observed_at = excluded.observed_at,
		expires_at = excluded.expires_at`,
		agentID, protocolVersion, readyInt, capJSON, now, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("store: put preview capability for %s: %w", agentID, err)
	}
	return nil
}

func (s *Store) GetActivePreviewCapability(ctx context.Context, agentID string) (model.PreviewWorkerCapability, error) {
	now := time.Now().UTC()
	row := s.q.QueryRowContext(ctx, `SELECT
		agent_id, protocol_version, ready, capability_json, observed_at, expires_at
	FROM preview_worker_capabilities WHERE agent_id = ? AND expires_at > ?`, agentID, now)
	return scanPreviewCapability(row)
}

func (s *Store) ListActivePreviewCapabilities(ctx context.Context) ([]model.PreviewWorkerCapability, error) {
	now := time.Now().UTC()
	rows, err := s.q.QueryContext(ctx, `SELECT
		agent_id, protocol_version, ready, capability_json, observed_at, expires_at
	FROM preview_worker_capabilities WHERE expires_at > ? ORDER BY agent_id`, now)
	if err != nil {
		return nil, fmt.Errorf("store: list active preview capabilities: %w", err)
	}
	defer rows.Close()

	var list []model.PreviewWorkerCapability
	for rows.Next() {
		cap, err := scanPreviewCapability(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, cap)
	}
	return list, rows.Err()
}

func scanPreviewJob(r scanner) (model.PreviewJob, error) {
	var j model.PreviewJob
	var inv int
	err := r.Scan(
		&j.PreviewID, &j.TaskID, &j.AssetID, &j.RunID, &j.RevisionID, &j.RequestID,
		&j.ExpectedVersion, &j.InputJSON, &j.InputSHA256, &j.PresetSHA256,
		&inv, &j.OutputDir, &j.ManifestSHA256, &j.MediaJSON,
		&j.CreatedBy, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.PreviewJob{}, model.ErrNotFound
		}
		return model.PreviewJob{}, err
	}
	j.Invalidated = (inv == 1)
	return j, nil
}

func scanPreviewCapability(r scanner) (model.PreviewWorkerCapability, error) {
	var c model.PreviewWorkerCapability
	var readyInt int
	err := r.Scan(
		&c.AgentID, &c.ProtocolVersion, &readyInt, &c.CapabilityJSON, &c.ObservedAt, &c.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.PreviewWorkerCapability{}, model.ErrNotFound
		}
		return model.PreviewWorkerCapability{}, err
	}
	c.Ready = (readyInt == 1)
	return c, nil
}
