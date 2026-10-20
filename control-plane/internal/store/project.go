package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Only editable project data participates in the active-project projection.
// Providers, profiles, Skills, templates, workers and append-only audit logs are
// global application state and are deliberately absent.
var projectTables = []string{
	"idempotency_keys",
	"assets", "tasks", "revisions", "workflow_runs", "workflow_stages",
	"scene_reviews", "narration_reviews", "workflow_narration_approvals", "acceptance_records",
	"recording_sources", "ingest_runs", "ingest_plan_revisions", "ingest_task_bindings", "ingest_checkpoints",
	"ingest_executions", "execution_controls", "execution_resources", "execution_requests",
	"preview_jobs", "preview_artifacts", "preview_requests", "preview_executions", "preview_execution_requests",
	"delivery_variants", "delivery_variant_artifacts", "delivery_variant_requests", "subtitle_style_drafts",
	"workflow_auto", "workflow_auto_rounds", "auto_reviews", "auto_actions", "auto_decisions",
	"visible_set_confirmations", "scripts", "script_workbenches", "script_revisions", "script_deliveries", "delivery_invalidations",
	"workflow_profile_bindings", "vac_projects", "vac_skill_runs", "vac_skill_steps", "vac_skill_snapshots",
	"vac_skill_requests", "vac_skill_revisions", "vac_edit_checkpoints", "vac_edit_cursors", "tts_preview_jobs",
}

func projectTableFilter(table string) string {
	if table == "idempotency_keys" {
		return ` WHERE scope NOT LIKE 'profile.save:%' AND scope NOT LIKE 'tts.template.save:%'`
	}
	return ""
}

type ProjectTable struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}
type ProjectSnapshot map[string]ProjectTable

func (s *Store) ProjectSnapshot(ctx context.Context) (ProjectSnapshot, error) {
	result := ProjectSnapshot{}
	err := s.Transaction(ctx, func(tx *Store) error {
		for _, table := range projectTables {
			rows, err := tx.q.QueryContext(ctx, `SELECT * FROM "`+table+`"`+projectTableFilter(table)+` ORDER BY rowid`)
			if err != nil {
				return fmt.Errorf("snapshot %s: %w", table, err)
			}
			columns, err := rows.Columns()
			if err != nil {
				rows.Close()
				return err
			}
			data := ProjectTable{Columns: columns, Rows: [][]any{}}
			for rows.Next() {
				values := make([]any, len(columns))
				pointers := make([]any, len(columns))
				for i := range values {
					pointers[i] = &values[i]
				}
				if err = rows.Scan(pointers...); err != nil {
					rows.Close()
					return err
				}
				for i, v := range values {
					if raw, ok := v.([]byte); ok {
						values[i] = string(raw)
					}
				}
				data.Rows = append(data.Rows, values)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			result[table] = data
		}
		return nil
	})
	return result, err
}

// RestoreProject atomically replaces the active projection. Unknown tables and
// column names are rejected before any deletion. Foreign keys are checked at
// commit, so a malformed document cannot replace a working project partially.
func (s *Store) RestoreProject(ctx context.Context, snapshot ProjectSnapshot) error {
	allowed := map[string]bool{}
	for _, name := range projectTables {
		allowed[name] = true
	}
	for name := range snapshot {
		if !allowed[name] {
			return fmt.Errorf("unexpected project table %q", name)
		}
	}
	return s.Transaction(ctx, func(tx *Store) error { return tx.restoreProject(ctx, snapshot) })
}

func (s *Store) ValidateProjectSnapshot(ctx context.Context, snapshot ProjectSnapshot) error {
	validated := errors.New("project validation rollback")
	err := s.Transaction(ctx, func(tx *Store) error {
		allowed := map[string]bool{}
		for _, name := range projectTables {
			allowed[name] = true
		}
		for name := range snapshot {
			if !allowed[name] {
				return fmt.Errorf("unexpected project table %q", name)
			}
		}
		if err := tx.restoreProject(ctx, snapshot); err != nil {
			return err
		}
		rows, err := tx.q.QueryContext(ctx, "PRAGMA foreign_key_check")
		if err != nil {
			return err
		}
		defer rows.Close()
		if rows.Next() {
			return fmt.Errorf("invalid project relationships")
		}
		if err = rows.Err(); err != nil {
			return err
		}
		return validated
	})
	if errors.Is(err, validated) {
		return nil
	}
	return err
}

func (tx *Store) restoreProject(ctx context.Context, snapshot ProjectSnapshot) error {
	for name, data := range snapshot {
		rows, err := tx.q.QueryContext(ctx, `SELECT * FROM "`+name+`" LIMIT 0`)
		if err != nil {
			return err
		}
		columns, err := rows.Columns()
		rows.Close()
		if err != nil {
			return err
		}
		if strings.Join(columns, "\x00") != strings.Join(data.Columns, "\x00") {
			return fmt.Errorf("incompatible columns for %s", name)
		}
		for _, row := range data.Rows {
			if len(row) != len(columns) {
				return fmt.Errorf("invalid %s row", name)
			}
			if name == "idempotency_keys" {
				for i, column := range columns {
					if column == "scope" {
						scope, ok := row[i].(string)
						if !ok || strings.HasPrefix(scope, "profile.save:") || strings.HasPrefix(scope, "tts.template.save:") {
							return fmt.Errorf("global configuration is not project data")
						}
					}
				}
			}
		}
	}
	if _, err := tx.q.ExecContext(ctx, "PRAGMA defer_foreign_keys=ON"); err != nil {
		return err
	}
	// Checkpoint immutability applies within an open project. Replacing the
	// entire projection is the sole exception, isolated in this transaction.
	if _, err := tx.q.ExecContext(ctx, "DROP TRIGGER vac_edit_checkpoints_no_delete"); err != nil {
		return err
	}
	for i := len(projectTables) - 1; i >= 0; i-- {
		if _, err := tx.q.ExecContext(ctx, `DELETE FROM "`+projectTables[i]+`"`+projectTableFilter(projectTables[i])); err != nil {
			return err
		}
	}
	for _, name := range projectTables {
		data, ok := snapshot[name]
		if !ok || len(data.Rows) == 0 {
			continue
		}
		quoted := make([]string, len(data.Columns))
		placeholders := make([]string, len(data.Columns))
		for i, col := range data.Columns {
			quoted[i] = `"` + strings.ReplaceAll(col, `"`, `""`) + `"`
			placeholders[i] = "?"
		}
		statement := `INSERT INTO "` + name + `" (` + strings.Join(quoted, ",") + `) VALUES (` + strings.Join(placeholders, ",") + `)`
		for _, row := range data.Rows {
			values := append([]any{}, row...)
			for i, value := range values {
				if number, ok := value.(json.Number); ok {
					if integer, err := number.Int64(); err == nil {
						values[i] = integer
					} else {
						real, err := number.Float64()
						if err != nil {
							return err
						}
						values[i] = real
					}
				}
			}
			if _, err := tx.q.ExecContext(ctx, statement, values...); err != nil {
				return fmt.Errorf("restore %s: %w", name, err)
			}
		}
	}
	if _, err := tx.q.ExecContext(ctx, `CREATE TRIGGER vac_edit_checkpoints_no_delete BEFORE DELETE ON vac_edit_checkpoints BEGIN SELECT RAISE(ABORT, 'Edit checkpoints are immutable'); END`); err != nil {
		return err
	}
	return nil
}
