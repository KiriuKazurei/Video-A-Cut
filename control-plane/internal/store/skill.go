package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func skillTable(kind string) (string, error) {
	switch kind {
	case "project":
		return "vac_projects", nil
	case "skill":
		return "vac_skills", nil
	case "run":
		return "vac_skill_runs", nil
	}
	return "", model.ErrArgument
}
func (s *Store) SkillGet(ctx context.Context, kind, id string, out any) error {
	table, err := skillTable(kind)
	if err != nil {
		return err
	}
	var raw string
	err = s.q.QueryRowContext(ctx, "SELECT body FROM "+table+" WHERE id=?", id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return model.ErrNotFound
	}
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), out)
}
func (s *Store) SkillList(ctx context.Context, kind string) ([]json.RawMessage, error) {
	table, err := skillTable(kind)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.QueryContext(ctx, "SELECT body FROM "+table+" ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, json.RawMessage(raw))
	}
	return out, rows.Err()
}

// expected=0 inserts; positive versions use a compare-and-swap.
func (s *Store) SkillPut(ctx context.Context, kind, id string, expected int, value any) error {
	table, err := skillTable(kind)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if expected == 0 {
		_, err = s.q.ExecContext(ctx, "INSERT INTO "+table+" (id,version,body) VALUES (?,1,?)", id, string(raw))
		if err != nil {
			return err
		}
		return s.skillRecordRevision(ctx, kind, id, raw)
	}
	result, err := s.q.ExecContext(ctx, "UPDATE "+table+" SET version=version+1,body=? WHERE id=? AND version=?", string(raw), id, expected)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return model.ErrConflict
	}
	return s.skillRecordRevision(ctx, kind, id, raw)
}
func (s *Store) skillRecordRevision(ctx context.Context, kind, id string, raw []byte) error {
	if kind != "run" {
		return nil
	}
	var v struct {
		Plan    json.RawMessage `json:"plan"`
		Version int             `json:"version"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	if len(v.Plan) == 0 {
		return nil
	}
	_, err := s.q.ExecContext(ctx, "INSERT INTO vac_skill_revisions VALUES (?,?,?)", id, v.Version, string(v.Plan))
	return err
}
func (s *Store) SkillRevisions(ctx context.Context, id string) ([]json.RawMessage, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT run_version,body FROM vac_skill_revisions WHERE run_id=? ORDER BY run_version", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var version int
		var raw string
		if err = rows.Scan(&version, &raw); err != nil {
			return nil, err
		}
		item, e := json.Marshal(map[string]any{"run_version": version, "plan": json.RawMessage(raw)})
		if e != nil {
			return nil, e
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
func (s *Store) SkillRequest(ctx context.Context, scope, key string) (string, string, error) {
	var sha, id string
	err := s.q.QueryRowContext(ctx, "SELECT request_sha,result_id FROM vac_skill_requests WHERE scope=? AND request_key=?", scope, key).Scan(&sha, &id)
	if errors.Is(err, sql.ErrNoRows) {
		err = model.ErrNotFound
	}
	return sha, id, err
}
func (s *Store) SkillRemember(ctx context.Context, scope, key, sha, id string) error {
	_, err := s.q.ExecContext(ctx, "INSERT INTO vac_skill_requests VALUES (?,?,?,?)", scope, key, sha, id)
	return err
}
func (s *Store) SkillSetEnabled(ctx context.Context, id string, in any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	res, err := s.q.ExecContext(ctx, "UPDATE vac_skills SET version=version+1,body=? WHERE id=?", string(raw), id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if n != 1 {
		return model.ErrNotFound
	}
	return err
}
func (s *Store) SkillSnapshot(ctx context.Context, id string, out any) error {
	var raw string
	err := s.q.QueryRowContext(ctx, "SELECT body FROM vac_skill_snapshots WHERE run_id=?", id).Scan(&raw)
	if err != nil {
		return err
	}
	return json.Unmarshal([]byte(raw), out)
}
func (s *Store) PutSkillSnapshot(ctx context.Context, id string, in any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, "INSERT INTO vac_skill_snapshots VALUES (?,?)", id, string(raw))
	return err
}
func (s *Store) PutSkillStep(ctx context.Context, id, run string, in any) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, "INSERT INTO vac_skill_steps VALUES (?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body WHERE vac_skill_steps.run_id=excluded.run_id", id, run, string(raw))
	return err
}
func (s *Store) SkillSteps(ctx context.Context, run string) ([]json.RawMessage, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT body FROM vac_skill_steps WHERE run_id=? ORDER BY rowid", run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("skill step: %w", err)
		}
		out = append(out, json.RawMessage(raw))
	}
	return out, rows.Err()
}
