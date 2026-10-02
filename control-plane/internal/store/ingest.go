package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func notFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("store: %s: %w", what, model.ErrNotFound)
	}
	return err
}

func uniqueConflict(err error) error {
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "unique") {
		return fmt.Errorf("store: %w", model.ErrConflict)
	}
	return err
}

const sourceColumns = `source_id,asset_id,root_id,relative_path,source_version,size_bytes,mtime_ns,snapshot_ref,sha256,probe_version,created_by,created_at,published_at`

func scanSource(row scanner) (model.RecordingSource, error) {
	var s model.RecordingSource
	var published sql.NullTime
	err := row.Scan(&s.SourceID, &s.AssetID, &s.RootID, &s.RelativePath, &s.SourceVersion, &s.SizeBytes, &s.MTimeNs,
		&s.SnapshotRef, &s.SHA256, &s.ProbeVersion, &s.CreatedBy, &s.CreatedAt, &published)
	if err != nil {
		return s, notFound(err, "recording source")
	}
	if published.Valid {
		t := published.Time.UTC()
		s.PublishedAt = &t
	}
	s.HasSnapshot = s.SnapshotRef != ""
	return s, nil
}

func (s *Store) InsertSource(ctx context.Context, src model.RecordingSource) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO recording_sources(`+sourceColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		src.SourceID, src.AssetID, src.RootID, src.RelativePath, src.SourceVersion, src.SizeBytes, src.MTimeNs,
		src.SnapshotRef, src.SHA256, src.ProbeVersion, src.CreatedBy, src.CreatedAt.UTC(), src.PublishedAt)
	return uniqueConflict(err)
}

func (s *Store) GetSource(ctx context.Context, id string) (model.RecordingSource, error) {
	return scanSource(s.q.QueryRowContext(ctx, `SELECT `+sourceColumns+` FROM recording_sources WHERE source_id=?`, id))
}

func (s *Store) ListSources(ctx context.Context, assetID string) ([]model.RecordingSource, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+sourceColumns+` FROM recording_sources WHERE asset_id=? ORDER BY created_at DESC, source_id LIMIT 50`, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.RecordingSource{}
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// PublishSource records the immutable snapshot once. A second publication
// is accepted only when it names the same snapshot and hash.
func (s *Store) PublishSource(ctx context.Context, id, snapshotRef, sha, probeVersion string) error {
	res, err := s.q.ExecContext(ctx, `UPDATE recording_sources SET snapshot_ref=?, sha256=?, probe_version=?, published_at=? WHERE source_id=? AND snapshot_ref=''`,
		snapshotRef, sha, probeVersion, time.Now().UTC(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	cur, err := s.GetSource(ctx, id)
	if err != nil {
		return err
	}
	if cur.SnapshotRef != snapshotRef || cur.SHA256 != sha {
		return fmt.Errorf("store: source snapshot is immutable: %w", model.ErrConflict)
	}
	return nil
}

const runColumns = `run_id,asset_id,source_id,state,stage,version,policy_json,policy_sha256,roots_sha256,analysis_revision,selection_revision,current_task_id,error_code,error_message,probe_json,probe_sha256,segments_ref,segments_sha256,files_json,package_ref,profile_id,profile_revision,profile_sha256,created_by,created_at,updated_at`

func scanRun(row scanner) (model.IngestRun, error) {
	var r model.IngestRun
	var files string
	err := row.Scan(&r.RunID, &r.AssetID, &r.SourceID, &r.State, &r.Stage, &r.Version, &r.PolicyJSON, &r.PolicySHA256, &r.RootsSHA256,
		&r.AnalysisRevision, &r.SelectionRevision, &r.CurrentTaskID, &r.ErrorCode, &r.ErrorMessage, &r.ProbeJSON, &r.ProbeSHA256,
		&r.SegmentsRef, &r.SegmentsSHA256, &files, &r.PackageRef, &r.ProfileID, &r.ProfileRevision, &r.ProfileSHA256,
		&r.CreatedBy, &r.CreatedAt, &r.UpdatedAt)
	if err != nil {
		return r, notFound(err, "ingest run")
	}
	r.Files, err = unmarshalStringMap(files)
	if r.Files == nil {
		r.Files = map[string]string{}
	}
	return r, err
}

func runArgs(r model.IngestRun) []any {
	return []any{r.AssetID, r.SourceID, r.State, r.Stage, r.Version, r.PolicyJSON, r.PolicySHA256, r.RootsSHA256,
		r.AnalysisRevision, r.SelectionRevision, r.CurrentTaskID, r.ErrorCode, r.ErrorMessage, r.ProbeJSON, r.ProbeSHA256,
		r.SegmentsRef, r.SegmentsSHA256, marshalStringMap(r.Files), r.PackageRef, r.ProfileID, r.ProfileRevision, r.ProfileSHA256,
		r.CreatedBy, r.CreatedAt.UTC(), time.Now().UTC()}
}

func (s *Store) InsertIngestRun(ctx context.Context, r model.IngestRun) error {
	args := append([]any{r.RunID}, runArgs(r)...)
	_, err := s.q.ExecContext(ctx, `INSERT INTO ingest_runs(`+runColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, args...)
	return uniqueConflict(err)
}

// UpdateIngestRun writes the row only when the stored version is prev, so
// two concurrent writers cannot both advance the same run.
func (s *Store) UpdateIngestRun(ctx context.Context, r model.IngestRun, prev int) error {
	args := append(runArgs(r), r.RunID, prev)
	res, err := s.q.ExecContext(ctx, `UPDATE ingest_runs SET asset_id=?,source_id=?,state=?,stage=?,version=?,policy_json=?,policy_sha256=?,roots_sha256=?,analysis_revision=?,selection_revision=?,current_task_id=?,error_code=?,error_message=?,probe_json=?,probe_sha256=?,segments_ref=?,segments_sha256=?,files_json=?,package_ref=?,profile_id=?,profile_revision=?,profile_sha256=?,created_by=?,created_at=?,updated_at=? WHERE run_id=? AND version=?`, args...)
	if err != nil {
		return uniqueConflict(err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return fmt.Errorf("store: ingest run version changed: %w", model.ErrConflict)
	}
	return nil
}

func (s *Store) GetIngestRun(ctx context.Context, id string) (model.IngestRun, error) {
	return scanRun(s.q.QueryRowContext(ctx, `SELECT `+runColumns+` FROM ingest_runs WHERE run_id=?`, id))
}

func (s *Store) ActiveIngestRun(ctx context.Context, assetID string) (model.IngestRun, error) {
	return scanRun(s.q.QueryRowContext(ctx, `SELECT `+runColumns+` FROM ingest_runs WHERE asset_id=? AND state IN ('queued','processing','awaiting_review') LIMIT 1`, assetID))
}

func (s *Store) ListIngestRuns(ctx context.Context, assetID string, limit, offset int) ([]model.IngestRun, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+runColumns+` FROM ingest_runs WHERE asset_id=? ORDER BY created_at DESC, run_id LIMIT ? OFFSET ?`, assetID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.IngestRun{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) InsertPlanRevision(ctx context.Context, p model.IngestPlanRevision) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO ingest_plan_revisions(run_id,revision,kind,schema_version,canonical_json,sha256,actor,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		p.RunID, p.Revision, p.Kind, p.SchemaVersion, p.CanonicalJSON, p.SHA256, p.Actor, p.CreatedAt.UTC())
	return uniqueConflict(err)
}

func (s *Store) GetPlanRevision(ctx context.Context, run string, revision int) (model.IngestPlanRevision, error) {
	var p model.IngestPlanRevision
	err := s.q.QueryRowContext(ctx, `SELECT run_id,revision,kind,schema_version,canonical_json,sha256,actor,created_at FROM ingest_plan_revisions WHERE run_id=? AND revision=?`, run, revision).
		Scan(&p.RunID, &p.Revision, &p.Kind, &p.SchemaVersion, &p.CanonicalJSON, &p.SHA256, &p.Actor, &p.CreatedAt)
	return p, notFound(err, "ingest plan revision")
}

func (s *Store) ListPlanRevisions(ctx context.Context, run string) ([]model.IngestPlanRevision, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT run_id,revision,kind,schema_version,canonical_json,sha256,actor,created_at FROM ingest_plan_revisions WHERE run_id=? ORDER BY revision LIMIT 200`, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.IngestPlanRevision{}
	for rows.Next() {
		var p model.IngestPlanRevision
		if err := rows.Scan(&p.RunID, &p.Revision, &p.Kind, &p.SchemaVersion, &p.CanonicalJSON, &p.SHA256, &p.Actor, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) NextPlanRevision(ctx context.Context, run string) (int, error) {
	var n int
	err := s.q.QueryRowContext(ctx, `SELECT COALESCE(MAX(revision),0)+1 FROM ingest_plan_revisions WHERE run_id=?`, run).Scan(&n)
	return n, err
}

const bindingColumns = `task_id,run_id,stage,plan_revision,input_sha256,roots_sha256,execution_id,execution_seq,attempt_of,invalidated,result_ref,result_sha256,handover_state,execution_state,created_at`

func scanBinding(row scanner) (model.IngestTaskBinding, error) {
	var b model.IngestTaskBinding
	var inv int
	err := row.Scan(&b.TaskID, &b.RunID, &b.Stage, &b.PlanRevision, &b.InputSHA256, &b.RootsSHA256, &b.ExecutionID, &b.ExecutionSeq,
		&b.AttemptOf, &inv, &b.ResultRef, &b.ResultSHA256, &b.HandoverState, &b.ExecutionState, &b.CreatedAt)
	b.Invalidated = inv != 0
	return b, notFound(err, "ingest task binding")
}

func (s *Store) InsertIngestBinding(ctx context.Context, b model.IngestTaskBinding) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO ingest_task_bindings(`+bindingColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.TaskID, b.RunID, b.Stage, b.PlanRevision, b.InputSHA256, b.RootsSHA256, b.ExecutionID, b.ExecutionSeq, b.AttemptOf,
		boolToInt(b.Invalidated), b.ResultRef, b.ResultSHA256, b.HandoverState, b.ExecutionState, b.CreatedAt.UTC())
	return uniqueConflict(err)
}

func (s *Store) GetIngestBinding(ctx context.Context, taskID string) (model.IngestTaskBinding, error) {
	return scanBinding(s.q.QueryRowContext(ctx, `SELECT `+bindingColumns+` FROM ingest_task_bindings WHERE task_id=?`, taskID))
}

func (s *Store) ListIngestBindings(ctx context.Context, run string) ([]model.IngestTaskBinding, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+bindingColumns+` FROM ingest_task_bindings WHERE run_id=? ORDER BY created_at, task_id LIMIT 200`, run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.IngestTaskBinding{}
	for rows.Next() {
		b, err := scanBinding(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) UpdateIngestBinding(ctx context.Context, b model.IngestTaskBinding) error {
	_, err := s.q.ExecContext(ctx, `UPDATE ingest_task_bindings SET execution_id=?,execution_seq=?,invalidated=?,result_ref=?,result_sha256=?,handover_state=?,execution_state=? WHERE task_id=?`,
		b.ExecutionID, b.ExecutionSeq, boolToInt(b.Invalidated), b.ResultRef, b.ResultSHA256, b.HandoverState, b.ExecutionState, b.TaskID)
	return err
}

func (s *Store) InsertCheckpoint(ctx context.Context, c model.IngestCheckpoint) error {
	if c.ManifestVersion == 0 {
		c.ManifestVersion = 1
	}
	if c.ItemStatus == "" {
		c.ItemStatus = "registered"
	}
	_, err := s.q.ExecContext(ctx, `INSERT INTO ingest_checkpoints(task_id,sequence,execution_id,input_sha256,policy_sha256,kind,item_index,ref,sha256,created_at,manifest_version,journal_version,source_execution_id,item_status,summary) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.TaskID, c.Sequence, c.ExecutionID, c.InputSHA256, c.PolicySHA256, c.Kind, c.ItemIndex, c.Ref, c.SHA256, c.CreatedAt.UTC(),
		c.ManifestVersion, c.JournalVersion, c.SourceExecutionID, c.ItemStatus, c.Summary)
	return uniqueConflict(err)
}

func (s *Store) ListCheckpoints(ctx context.Context, taskID string) ([]model.IngestCheckpoint, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT task_id,sequence,execution_id,input_sha256,policy_sha256,kind,item_index,ref,sha256,created_at,manifest_version,journal_version,source_execution_id,item_status,summary FROM ingest_checkpoints WHERE task_id=? ORDER BY sequence LIMIT 10000`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.IngestCheckpoint{}
	for rows.Next() {
		var c model.IngestCheckpoint
		if err := rows.Scan(&c.TaskID, &c.Sequence, &c.ExecutionID, &c.InputSHA256, &c.PolicySHA256, &c.Kind, &c.ItemIndex, &c.Ref, &c.SHA256, &c.CreatedAt,
			&c.ManifestVersion, &c.JournalVersion, &c.SourceExecutionID, &c.ItemStatus, &c.Summary); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) PutIngestCapability(ctx context.Context, agentID, roots, body string) error {
	now := time.Now().UTC()
	_, err := s.q.ExecContext(ctx, `INSERT INTO ingest_worker_capabilities(agent_id,roots_sha256,capability_json,observed_at,expires_at) VALUES(?,?,?,?,?)
 ON CONFLICT(agent_id) DO UPDATE SET roots_sha256=excluded.roots_sha256,capability_json=excluded.capability_json,observed_at=excluded.observed_at,expires_at=excluded.expires_at`,
		agentID, roots, body, now, now.Add(90*time.Second))
	return err
}

// IngestCapability returns the fresh capability report of one agent.
func (s *Store) IngestCapability(ctx context.Context, agentID string) (string, string, error) {
	var roots, body string
	err := s.q.QueryRowContext(ctx, `SELECT roots_sha256,capability_json FROM ingest_worker_capabilities WHERE agent_id=? AND expires_at>?`, agentID, time.Now().UTC()).Scan(&roots, &body)
	return roots, body, notFound(err, "ingest capability")
}

// FreshIngestCapabilities lists unexpired reports for the UI.
func (s *Store) FreshIngestCapabilities(ctx context.Context) ([]string, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT capability_json FROM ingest_worker_capabilities WHERE expires_at>? ORDER BY agent_id LIMIT 16`, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		out = append(out, body)
	}
	return out, rows.Err()
}
