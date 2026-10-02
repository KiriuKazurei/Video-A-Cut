package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"time"
)

type ProfileBinding struct {
	RunID     string `json:"run_id"`
	ProfileID string `json:"profile_id"`
	Revision  int    `json:"revision"`
	SHA256    string `json:"profile_sha256"`
	JSON      string `json:"-"`
}

func (s *Store) HasCapability(ctx context.Context, id, sha string) (bool, error) {
	var count int
	e := s.q.QueryRowContext(ctx, `SELECT count(*) FROM worker_capabilities WHERE agent_id=? AND profile_sha256=? AND expires_at>?`, id, sha, time.Now().UTC()).Scan(&count)
	return count > 0, e
}

func (s *Store) BindProfile(ctx context.Context, b ProfileBinding) error {
	_, e := s.q.ExecContext(ctx, `INSERT INTO workflow_profile_bindings VALUES(?,?,?,?,?)`, b.RunID, b.ProfileID, b.Revision, b.SHA256, b.JSON)
	return e
}
func (s *Store) ProfileBinding(ctx context.Context, run string) (ProfileBinding, error) {
	var b ProfileBinding
	e := s.q.QueryRowContext(ctx, `SELECT run_id,profile_id,revision,profile_sha256,canonical_json FROM workflow_profile_bindings WHERE run_id=?`, run).Scan(&b.RunID, &b.ProfileID, &b.Revision, &b.SHA256, &b.JSON)
	if errors.Is(e, sql.ErrNoRows) {
		return b, model.ErrNotFound
	}
	return b, e
}
func (s *Store) SetProfileConsent(ctx context.Context, id string, revision int, sha, actor string, granted bool) error {
	_, e := s.q.ExecContext(ctx, `INSERT INTO profile_external_consents VALUES(?,?,?,?,?,?) ON CONFLICT(profile_id,revision) DO UPDATE SET profile_sha256=excluded.profile_sha256,actor=excluded.actor,granted=excluded.granted,updated_at=excluded.updated_at`, id, revision, sha, actor, granted, time.Now().UTC())
	return e
}
func (s *Store) ProfileConsent(ctx context.Context, id string, revision int, sha string) (bool, error) {
	var grant bool
	e := s.q.QueryRowContext(ctx, `SELECT granted FROM profile_external_consents WHERE profile_id=? AND revision=? AND profile_sha256=?`, id, revision, sha).Scan(&grant)
	if errors.Is(e, sql.ErrNoRows) {
		return false, nil
	}
	return grant, e
}
func (s *Store) PutCapability(ctx context.Context, id, role, sha, body string) error {
	now := time.Now().UTC()
	_, e := s.q.ExecContext(ctx, `INSERT INTO worker_capabilities VALUES(?,?,?,?,?,?) ON CONFLICT(agent_id,profile_sha256) DO UPDATE SET role=excluded.role,capability_json=excluded.capability_json,observed_at=excluded.observed_at,expires_at=excluded.expires_at`, id, role, sha, body, now, now.Add(90*time.Second))
	return e
}
func (s *Store) Capabilities(ctx context.Context, sha string) (map[string][]string, error) {
	rows, e := s.q.QueryContext(ctx, `SELECT role,capability_json FROM worker_capabilities WHERE profile_sha256=? AND expires_at>?`, sha, time.Now().UTC())
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var role, body string
		if e := rows.Scan(&role, &body); e != nil {
			return nil, e
		}
		out[role] = append(out[role], body)
	}
	return out, rows.Err()
}
