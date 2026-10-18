package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func (s *Store) GetScriptWorkbench(ctx context.Context, id string) (int, string, error) {
	var version int
	var body string
	err := s.q.QueryRowContext(ctx, `SELECT version, document_json FROM script_workbenches WHERE script_id=?`, id).Scan(&version, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", nil
	}
	return version, body, err
}

func (s *Store) PutScriptWorkbench(ctx context.Context, id string, expected int, body string) error {
	res, err := s.q.ExecContext(ctx, `INSERT INTO script_workbenches(script_id,version,document_json) VALUES(?,?,?)
ON CONFLICT(script_id) DO UPDATE SET version=excluded.version,document_json=excluded.document_json WHERE script_workbenches.version=?`, id, expected+1, body, expected)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err == nil && n != 1 {
		return model.ErrConflict
	}
	return err
}

type ScriptRevision struct {
	Version   int       `json:"version"`
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Snapshot  string    `json:"snapshot"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *Store) InsertScriptRevision(ctx context.Context, id string, r ScriptRevision) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO script_revisions(script_id,version,action,actor,snapshot_json,created_at) VALUES(?,?,?,?,?,?)`, id, r.Version, r.Action, r.Actor, r.Snapshot, r.CreatedAt.UTC())
	return err
}

func (s *Store) ListScriptRevisions(ctx context.Context, id string) ([]ScriptRevision, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT version,action,actor,snapshot_json,created_at FROM script_revisions WHERE script_id=? ORDER BY version DESC LIMIT 200`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ScriptRevision{}
	for rows.Next() {
		var r ScriptRevision
		if err := rows.Scan(&r.Version, &r.Action, &r.Actor, &r.Snapshot, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type ScriptDelivery struct {
	Version       int       `json:"version"`
	VisibleSetSHA string    `json:"visible_set_sha"`
	PackageRef    string    `json:"-"`
	CreatedAt     time.Time `json:"created_at"`
}

func (s *Store) PutScriptDelivery(ctx context.Context, id string, d ScriptDelivery) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO script_deliveries(script_id,version,visible_set_sha,package_ref,created_at) VALUES(?,?,?,?,?)
ON CONFLICT(script_id,version) DO UPDATE SET visible_set_sha=excluded.visible_set_sha,package_ref=excluded.package_ref,created_at=excluded.created_at`, id, d.Version, d.VisibleSetSHA, d.PackageRef, d.CreatedAt.UTC())
	return err
}
func (s *Store) GetScriptDelivery(ctx context.Context, id string, version int) (ScriptDelivery, error) {
	var d ScriptDelivery
	err := s.q.QueryRowContext(ctx, `SELECT version,visible_set_sha,package_ref,created_at FROM script_deliveries WHERE script_id=? AND version=?`, id, version).Scan(&d.Version, &d.VisibleSetSHA, &d.PackageRef, &d.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return d, model.ErrNotFound
	}
	return d, err
}
