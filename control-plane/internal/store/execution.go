package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func nullTime(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UTC()
}

func scanNullTime(n sql.NullTime) *time.Time {
	if !n.Valid {
		return nil
	}
	v := n.Time.UTC()
	return &v
}

func (s *Store) InsertIngestExecution(ctx context.Context, e model.IngestExecution) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO ingest_executions(
execution_id,task_id,run_id,generation,agent_id,role,runtime_instance_id,input_sha256,policy_sha256,status,
lease_until,owned_dir,stop_reason,drained_at,result_request_id,submitted_at,created_at,updated_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ExecutionID, e.TaskID, e.RunID, e.Generation, e.AgentID, e.Role, e.RuntimeInstanceID, e.InputSHA256, e.PolicySHA256, e.Status,
		nullTime(e.LeaseUntil), e.OwnedDir, e.StopReason, nullTime(e.DrainedAt), e.ResultRequestID, nullTime(e.SubmittedAt), e.CreatedAt.UTC(), e.UpdatedAt.UTC())
	return uniqueConflict(err)
}

func scanExecution(row scanner) (model.IngestExecution, error) {
	var e model.IngestExecution
	var lease, drained, submitted sql.NullTime
	err := row.Scan(&e.ExecutionID, &e.TaskID, &e.RunID, &e.Generation, &e.AgentID, &e.Role, &e.RuntimeInstanceID,
		&e.InputSHA256, &e.PolicySHA256, &e.Status, &lease, &e.OwnedDir, &e.StopReason, &drained, &e.ResultRequestID,
		&submitted, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return e, notFound(err, "ingest execution")
	}
	e.LeaseUntil, e.DrainedAt, e.SubmittedAt = scanNullTime(lease), scanNullTime(drained), scanNullTime(submitted)
	return e, nil
}

const executionColumns = `execution_id,task_id,run_id,generation,agent_id,role,runtime_instance_id,input_sha256,policy_sha256,status,lease_until,owned_dir,stop_reason,drained_at,result_request_id,submitted_at,created_at,updated_at`

func (s *Store) GetIngestExecution(ctx context.Context, id string) (model.IngestExecution, error) {
	return scanExecution(s.q.QueryRowContext(ctx, `SELECT `+executionColumns+` FROM ingest_executions WHERE execution_id=?`, id))
}

func (s *Store) ListIngestExecutionsByTask(ctx context.Context, taskID string) ([]model.IngestExecution, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+executionColumns+` FROM ingest_executions WHERE task_id=? ORDER BY generation LIMIT 100`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.IngestExecution{}
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) ListLiveExecutionsByAgent(ctx context.Context, agentID string) ([]model.IngestExecution, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT `+executionColumns+` FROM ingest_executions WHERE agent_id=? AND status IN ('allocated','waiting_resource','running','suspended_connection') ORDER BY created_at LIMIT 50`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.IngestExecution{}
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) UpdateIngestExecution(ctx context.Context, e model.IngestExecution) error {
	_, err := s.q.ExecContext(ctx, `UPDATE ingest_executions SET status=?,lease_until=?,stop_reason=?,drained_at=?,result_request_id=?,submitted_at=?,updated_at=? WHERE execution_id=?`,
		e.Status, nullTime(e.LeaseUntil), e.StopReason, nullTime(e.DrainedAt), e.ResultRequestID, nullTime(e.SubmittedAt), time.Now().UTC(), e.ExecutionID)
	return err
}

func (s *Store) UpsertWorkerInstance(ctx context.Context, w model.WorkerInstance) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO ingest_worker_instances(agent_id,role,runtime_instance_id,status,observed_at) VALUES(?,?,?,?,?)
ON CONFLICT(agent_id, runtime_instance_id) DO UPDATE SET role=excluded.role, status=excluded.status, observed_at=excluded.observed_at`,
		w.AgentID, w.Role, w.RuntimeInstanceID, w.Status, w.ObservedAt.UTC())
	return err
}

func (s *Store) InsertExecutionControl(ctx context.Context, c model.ExecutionControl) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO execution_controls(execution_id,control_version,command,reason,acked_at,ack_outcome,updated_at) VALUES(?,?,?,?,?,?,?)`,
		c.ExecutionID, c.ControlVersion, c.Command, c.Reason, nullTime(c.AckedAt), c.AckOutcome, c.UpdatedAt.UTC())
	return uniqueConflict(err)
}

func (s *Store) GetExecutionControl(ctx context.Context, executionID string) (model.ExecutionControl, error) {
	var c model.ExecutionControl
	var acked sql.NullTime
	err := s.q.QueryRowContext(ctx, `SELECT execution_id,control_version,command,reason,acked_at,ack_outcome,updated_at FROM execution_controls WHERE execution_id=?`, executionID).
		Scan(&c.ExecutionID, &c.ControlVersion, &c.Command, &c.Reason, &acked, &c.AckOutcome, &c.UpdatedAt)
	if err != nil {
		return c, notFound(err, "execution control")
	}
	c.AckedAt = scanNullTime(acked)
	return c, nil
}

// RequestStop records one idempotent stop command. Repeating it does not
// bump the control version again.
func (s *Store) RequestStop(ctx context.Context, executionID, reason string, now time.Time) error {
	var command string
	err := s.q.QueryRowContext(ctx, `SELECT command FROM execution_controls WHERE execution_id=?`, executionID).Scan(&command)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = s.q.ExecContext(ctx, `INSERT INTO execution_controls(execution_id,control_version,command,reason,ack_outcome,updated_at) VALUES(?,1,'stop',?,'',?)`,
			executionID, reason, now.UTC())
		return err
	}
	if err != nil {
		return err
	}
	if command == "stop" {
		_, err = s.q.ExecContext(ctx, `UPDATE execution_controls SET reason=?, updated_at=? WHERE execution_id=?`, reason, now.UTC(), executionID)
		return err
	}
	_, err = s.q.ExecContext(ctx, `UPDATE execution_controls SET command='stop', reason=?, control_version=control_version+1, updated_at=? WHERE execution_id=?`,
		reason, now.UTC(), executionID)
	return err
}

func (s *Store) AckExecutionControl(ctx context.Context, executionID, outcome string, now time.Time) error {
	_, err := s.q.ExecContext(ctx, `UPDATE execution_controls SET acked_at=?, ack_outcome=?, updated_at=? WHERE execution_id=?`,
		now.UTC(), outcome, now.UTC(), executionID)
	return err
}

func (s *Store) UpsertExecutionResource(ctx context.Context, r model.ExecutionResource) error {
	barrier := 0
	if r.Barrier {
		barrier = 1
	}
	_, err := s.q.ExecContext(ctx, `INSERT INTO execution_resources(resource_key,owner_execution_id,owner_generation,strategy,state,barrier,updated_at) VALUES(?,?,?,?,?,?,?)
ON CONFLICT(resource_key) DO UPDATE SET owner_execution_id=excluded.owner_execution_id, owner_generation=excluded.owner_generation, strategy=excluded.strategy, state=excluded.state, barrier=excluded.barrier, updated_at=excluded.updated_at`,
		r.ResourceKey, r.OwnerExecutionID, r.OwnerGeneration, r.Strategy, r.State, barrier, r.UpdatedAt.UTC())
	return err
}

func (s *Store) GetExecutionResource(ctx context.Context, key string) (model.ExecutionResource, error) {
	var r model.ExecutionResource
	var barrier int
	err := s.q.QueryRowContext(ctx, `SELECT resource_key,owner_execution_id,owner_generation,strategy,state,barrier,updated_at FROM execution_resources WHERE resource_key=?`, key).
		Scan(&r.ResourceKey, &r.OwnerExecutionID, &r.OwnerGeneration, &r.Strategy, &r.State, &barrier, &r.UpdatedAt)
	if err != nil {
		return r, notFound(err, "execution resource")
	}
	r.Barrier = barrier != 0
	return r, nil
}

func (s *Store) InsertExecutionRequest(ctx context.Context, r model.ExecutionRequest) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO execution_requests(runtime_instance_id,request_id,operation,agent_id,execution_id,content_sha256,result_status,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		r.RuntimeInstanceID, r.RequestID, r.Operation, r.AgentID, r.ExecutionID, r.ContentSHA256, r.ResultStatus, r.CreatedAt.UTC())
	return uniqueConflict(err)
}

func (s *Store) GetExecutionRequest(ctx context.Context, instance, requestID, operation string) (model.ExecutionRequest, error) {
	var r model.ExecutionRequest
	err := s.q.QueryRowContext(ctx, `SELECT runtime_instance_id,request_id,operation,agent_id,execution_id,content_sha256,result_status,created_at FROM execution_requests WHERE runtime_instance_id=? AND request_id=? AND operation=?`,
		instance, requestID, operation).Scan(&r.RuntimeInstanceID, &r.RequestID, &r.Operation, &r.AgentID, &r.ExecutionID, &r.ContentSHA256, &r.ResultStatus, &r.CreatedAt)
	if err != nil {
		return r, notFound(err, "execution request")
	}
	return r, nil
}

func (s *Store) UpdateExecutionRequestStatus(ctx context.Context, instance, requestID, operation, status, content string) error {
	_, err := s.q.ExecContext(ctx, `UPDATE execution_requests SET result_status=?, content_sha256=? WHERE runtime_instance_id=? AND request_id=? AND operation=?`,
		status, content, instance, requestID, operation)
	return err
}
