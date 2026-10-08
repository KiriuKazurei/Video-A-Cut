package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func scanPreviewExecution(row scanner) (model.PreviewExecution, error) {
	var ex model.PreviewExecution
	err := row.Scan(&ex.ExecutionID, &ex.TaskID, &ex.AgentID, &ex.RuntimeInstanceID, &ex.Generation, &ex.InputSHA256)
	if errors.Is(err, sql.ErrNoRows) {
		return ex, model.ErrNotFound
	}
	return ex, err
}

func (s *Store) GetPreviewExecution(ctx context.Context, taskID string, generation int) (model.PreviewExecution, error) {
	return scanPreviewExecution(s.q.QueryRowContext(ctx, `SELECT execution_id, task_id, agent_id,
		runtime_instance_id, generation, input_sha256 FROM preview_executions WHERE task_id=? AND generation=?`, taskID, generation))
}

func (s *Store) PreviewExecutionByRequest(ctx context.Context, instance, request string) (model.PreviewExecution, error) {
	return scanPreviewExecution(s.q.QueryRowContext(ctx, `SELECT e.execution_id, e.task_id, e.agent_id,
		e.runtime_instance_id, e.generation, e.input_sha256 FROM preview_executions e
		JOIN preview_execution_requests r ON r.execution_id=e.execution_id
		WHERE r.runtime_instance_id=? AND r.request_id=?`, instance, request))
}

func (s *Store) InsertPreviewExecution(ctx context.Context, ex model.PreviewExecution) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO preview_executions
		(execution_id, task_id, agent_id, runtime_instance_id, generation, input_sha256) VALUES (?, ?, ?, ?, ?, ?)`,
		ex.ExecutionID, ex.TaskID, ex.AgentID, ex.RuntimeInstanceID, ex.Generation, ex.InputSHA256)
	return err
}

func (s *Store) BindPreviewExecutionRequest(ctx context.Context, instance, request, execution string) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO preview_execution_requests
		(runtime_instance_id, request_id, execution_id) VALUES (?, ?, ?)`, instance, request, execution)
	return err
}
