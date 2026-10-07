package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func (s *Store) InsertDeliveryVariantJob(ctx context.Context, job model.DeliveryVariantJob) error {
	inv := 0
	if job.Invalidated {
		inv = 1
	}
	_, err := s.q.ExecContext(ctx, `INSERT INTO delivery_variants (
		variant_id, task_id, asset_id, run_id, revision_id, request_id,
		expected_version, input_json, input_sha256, spec_sha256,
		invalidated, output_dir, manifest_sha256, report_json,
		created_by, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.VariantID, job.TaskID, job.AssetID, job.RunID, job.RevisionID, job.RequestID,
		job.ExpectedVersion, job.InputJSON, job.InputSHA256, job.SpecSHA256,
		inv, job.OutputDir, job.ManifestSHA256, job.ReportJSON,
		job.CreatedBy, job.CreatedAt, job.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("store: insert delivery variant %s: %w", job.VariantID, err)
	}
	return nil
}

func (s *Store) GetDeliveryVariantJob(ctx context.Context, variantID string) (model.DeliveryVariantJob, error) {
	row := s.q.QueryRowContext(ctx, `SELECT
		variant_id, task_id, asset_id, run_id, revision_id, request_id,
		expected_version, input_json, input_sha256, spec_sha256,
		invalidated, output_dir, manifest_sha256, report_json,
		created_by, created_at, updated_at
	FROM delivery_variants WHERE variant_id = ?`, variantID)
	return scanDeliveryVariantJob(row)
}

func (s *Store) GetDeliveryVariantJobByTaskID(ctx context.Context, taskID string) (model.DeliveryVariantJob, error) {
	row := s.q.QueryRowContext(ctx, `SELECT
		variant_id, task_id, asset_id, run_id, revision_id, request_id,
		expected_version, input_json, input_sha256, spec_sha256,
		invalidated, output_dir, manifest_sha256, report_json,
		created_by, created_at, updated_at
	FROM delivery_variants WHERE task_id = ?`, taskID)
	return scanDeliveryVariantJob(row)
}

func (s *Store) GetDeliveryVariantJobByRequest(ctx context.Context, runID, requestID string) (model.DeliveryVariantJob, error) {
	row := s.q.QueryRowContext(ctx, `SELECT
		pj.variant_id, pj.task_id, pj.asset_id, pj.run_id, pr.revision_id, pr.request_id,
		pr.expected_version, pj.input_json, pj.input_sha256, pr.spec_sha256,
		pj.invalidated, pj.output_dir, pj.manifest_sha256, pj.report_json,
		pj.created_by, pj.created_at, pj.updated_at
	FROM delivery_variants pj JOIN delivery_variant_requests pr ON pr.variant_id = pj.variant_id
	WHERE pr.run_id = ? AND pr.request_id = ?`, runID, requestID)
	return scanDeliveryVariantJob(row)
}

func (s *Store) BindDeliveryVariantRequest(ctx context.Context, runID, requestID, variantID, revisionID string, version int, specSHA string) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO delivery_variant_requests VALUES (?, ?, ?, ?, ?, ?)`, runID, requestID, variantID, revisionID, version, specSHA)
	return err
}

// FindReusableDeliveryVariantJob finds the most recent non-invalidated delivery variant with identical input for the run/revision
// whose task is still active (queued/claimed/running).
func (s *Store) FindReusableDeliveryVariantJob(ctx context.Context, runID, revisionID, inputSHA256 string) (model.DeliveryVariantJob, error) {
	row := s.q.QueryRowContext(ctx, `SELECT
		pj.variant_id, pj.task_id, pj.asset_id, pj.run_id, pj.revision_id, pj.request_id,
		pj.expected_version, pj.input_json, pj.input_sha256, pj.spec_sha256,
		pj.invalidated, pj.output_dir, pj.manifest_sha256, pj.report_json,
		pj.created_by, pj.created_at, pj.updated_at
	FROM delivery_variants pj
	JOIN tasks t ON pj.task_id = t.task_id
	WHERE pj.run_id = ? AND pj.revision_id = ? AND pj.input_sha256 = ?
	  AND pj.invalidated = 0
	  AND t.status IN ('queued', 'claimed', 'running')
	ORDER BY pj.created_at DESC, pj.variant_id DESC LIMIT 1`, runID, revisionID, inputSHA256)
	return scanDeliveryVariantJob(row)
}

func (s *Store) ListDeliveryVariantJobs(ctx context.Context, runID string) ([]model.DeliveryVariantJob, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT
		variant_id, task_id, asset_id, run_id, revision_id, request_id,
		expected_version, input_json, input_sha256, spec_sha256,
		invalidated, output_dir, manifest_sha256, report_json,
		created_by, created_at, updated_at
	FROM delivery_variants WHERE run_id = ?
	ORDER BY created_at DESC, variant_id DESC`, runID)
	if err != nil {
		return nil, fmt.Errorf("store: list delivery variants for run %s: %w", runID, err)
	}
	defer rows.Close()

	var jobs []model.DeliveryVariantJob
	for rows.Next() {
		job, err := scanDeliveryVariantJob(rows)
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

func (s *Store) UpdateDeliveryVariantJobSuccess(ctx context.Context, variantID, outputDir, manifestSHA256, reportJSON string) error {
	now := time.Now().UTC()
	res, err := s.q.ExecContext(ctx, `UPDATE delivery_variants SET
		output_dir = ?, manifest_sha256 = ?, report_json = ?, updated_at = ?
	WHERE variant_id = ?`, outputDir, manifestSHA256, reportJSON, now, variantID)
	if err != nil {
		return fmt.Errorf("store: update delivery variant %s success: %w", variantID, err)
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

func (s *Store) InvalidateDeliveryVariantJob(ctx context.Context, variantID string) error {
	now := time.Now().UTC()
	_, err := s.q.ExecContext(ctx, `UPDATE delivery_variants SET invalidated = 1, updated_at = ? WHERE variant_id = ?`, now, variantID)
	if err != nil {
		return fmt.Errorf("store: invalidate delivery variant %s: %w", variantID, err)
	}
	return nil
}

func (s *Store) InvalidateDeliveryVariantJobsByRun(ctx context.Context, runID string) error {
	now := time.Now().UTC()
	_, err := s.q.ExecContext(ctx, `UPDATE delivery_variants SET invalidated = 1, updated_at = ? WHERE run_id = ? AND invalidated = 0`, now, runID)
	if err != nil {
		return fmt.Errorf("store: invalidate delivery variants for run %s: %w", runID, err)
	}
	return nil
}

func (s *Store) InvalidateDeliveryVariantJobsByRevision(ctx context.Context, runID, revisionID string) error {
	now := time.Now().UTC()
	_, err := s.q.ExecContext(ctx, `UPDATE delivery_variants SET invalidated = 1, updated_at = ? WHERE run_id = ? AND revision_id = ? AND invalidated = 0`, now, runID, revisionID)
	if err != nil {
		return fmt.Errorf("store: invalidate delivery variants for run %s revision %s: %w", runID, revisionID, err)
	}
	return nil
}

func (s *Store) InvalidateDeliveryVariantJobsForAssetExcept(ctx context.Context, assetID, keepRevisionID string) error {
	now := time.Now().UTC()
	_, err := s.q.ExecContext(ctx, `UPDATE delivery_variants SET invalidated = 1, updated_at = ? WHERE asset_id = ? AND revision_id != ? AND invalidated = 0`, now, assetID, keepRevisionID)
	if err != nil {
		return fmt.Errorf("store: invalidate delivery variants for asset %s except %s: %w", assetID, keepRevisionID, err)
	}
	return nil
}

func (s *Store) InsertDeliveryVariantArtifacts(ctx context.Context, artifacts []model.DeliveryVariantArtifactRecord) error {
	for _, a := range artifacts {
		_, err := s.q.ExecContext(ctx, `INSERT INTO delivery_variant_artifacts (
			variant_id, key, kind, path, mime, size, sha256
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(variant_id, key) DO UPDATE SET
			kind = excluded.kind, path = excluded.path, mime = excluded.mime,
			size = excluded.size, sha256 = excluded.sha256`,
			a.VariantID, a.Key, a.Kind, a.Path, a.MIME, a.Size, a.SHA256,
		)
		if err != nil {
			return fmt.Errorf("store: insert delivery variant artifact (%s, %s): %w", a.VariantID, a.Key, err)
		}
	}
	return nil
}

func (s *Store) ListDeliveryVariantArtifacts(ctx context.Context, variantID string) ([]model.DeliveryVariantArtifactRecord, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT
		variant_id, key, kind, path, mime, size, sha256
	FROM delivery_variant_artifacts WHERE variant_id = ? ORDER BY key`, variantID)
	if err != nil {
		return nil, fmt.Errorf("store: list delivery variant artifacts for %s: %w", variantID, err)
	}
	defer rows.Close()

	var list []model.DeliveryVariantArtifactRecord
	for rows.Next() {
		var a model.DeliveryVariantArtifactRecord
		if err := rows.Scan(&a.VariantID, &a.Key, &a.Kind, &a.Path, &a.MIME, &a.Size, &a.SHA256); err != nil {
			return nil, err
		}
		list = append(list, a)
	}
	return list, rows.Err()
}

func (s *Store) GetDeliveryVariantArtifact(ctx context.Context, variantID, key string) (model.DeliveryVariantArtifactRecord, error) {
	row := s.q.QueryRowContext(ctx, `SELECT
		variant_id, key, kind, path, mime, size, sha256
	FROM delivery_variant_artifacts WHERE variant_id = ? AND key = ?`, variantID, key)
	var a model.DeliveryVariantArtifactRecord
	err := row.Scan(&a.VariantID, &a.Key, &a.Kind, &a.Path, &a.MIME, &a.Size, &a.SHA256)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.DeliveryVariantArtifactRecord{}, model.ErrNotFound
		}
		return model.DeliveryVariantArtifactRecord{}, err
	}
	return a, nil
}

func (s *Store) PutDeliveryVariantCapability(ctx context.Context, agentID string, protocolVersion int, ready bool, capJSON string, expiresAt time.Time) error {
	now := time.Now().UTC()
	readyInt := 0
	if ready {
		readyInt = 1
	}
	_, err := s.q.ExecContext(ctx, `INSERT INTO delivery_variant_worker_capabilities (
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
		return fmt.Errorf("store: put delivery variant capability for %s: %w", agentID, err)
	}
	return nil
}

func (s *Store) GetActiveDeliveryVariantCapability(ctx context.Context, agentID string) (model.DeliveryVariantWorkerCapability, error) {
	now := time.Now().UTC()
	row := s.q.QueryRowContext(ctx, `SELECT
		agent_id, protocol_version, ready, capability_json, observed_at, expires_at
	FROM delivery_variant_worker_capabilities WHERE agent_id = ? AND expires_at > ?`, agentID, now)
	return scanDeliveryVariantCapability(row)
}

func (s *Store) ListActiveDeliveryVariantCapabilities(ctx context.Context) ([]model.DeliveryVariantWorkerCapability, error) {
	now := time.Now().UTC()
	rows, err := s.q.QueryContext(ctx, `SELECT
		agent_id, protocol_version, ready, capability_json, observed_at, expires_at
	FROM delivery_variant_worker_capabilities WHERE expires_at > ? ORDER BY agent_id`, now)
	if err != nil {
		return nil, fmt.Errorf("store: list active preview capabilities: %w", err)
	}
	defer rows.Close()

	var list []model.DeliveryVariantWorkerCapability
	for rows.Next() {
		cap, err := scanDeliveryVariantCapability(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, cap)
	}
	return list, rows.Err()
}

func scanDeliveryVariantJob(r scanner) (model.DeliveryVariantJob, error) {
	var j model.DeliveryVariantJob
	var inv int
	err := r.Scan(
		&j.VariantID, &j.TaskID, &j.AssetID, &j.RunID, &j.RevisionID, &j.RequestID,
		&j.ExpectedVersion, &j.InputJSON, &j.InputSHA256, &j.SpecSHA256,
		&inv, &j.OutputDir, &j.ManifestSHA256, &j.ReportJSON,
		&j.CreatedBy, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.DeliveryVariantJob{}, model.ErrNotFound
		}
		return model.DeliveryVariantJob{}, err
	}
	j.Invalidated = (inv == 1)
	return j, nil
}

func scanDeliveryVariantCapability(r scanner) (model.DeliveryVariantWorkerCapability, error) {
	var c model.DeliveryVariantWorkerCapability
	var readyInt int
	err := r.Scan(
		&c.AgentID, &c.ProtocolVersion, &readyInt, &c.CapabilityJSON, &c.ObservedAt, &c.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.DeliveryVariantWorkerCapability{}, model.ErrNotFound
		}
		return model.DeliveryVariantWorkerCapability{}, err
	}
	c.Ready = (readyInt == 1)
	return c, nil
}
