package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

type ProfileRevisionRow struct {
	ProfileID     string
	Revision      int
	SchemaVersion int
	CanonicalJSON string
	SHA256        string
	Actor         string
	CreatedAt     time.Time
	Archived      bool
}

func (s *Store) GetProfileRevision(ctx context.Context, profileID string, revision int) (ProfileRevisionRow, error) {
	query := `SELECT r.profile_id, r.revision, r.schema_version, r.canonical_json, r.sha256, r.actor, r.created_at, p.archived
  FROM processing_profile_revisions r JOIN processing_profiles p ON p.profile_id = r.profile_id
  WHERE r.profile_id = ?`
	args := []any{profileID}
	if revision > 0 {
		query += ` AND r.revision = ?`
		args = append(args, revision)
	} else {
		query += ` AND r.revision = p.current_revision`
	}
	row := s.q.QueryRowContext(ctx, query, args...)
	var out ProfileRevisionRow
	var archived int
	err := row.Scan(&out.ProfileID, &out.Revision, &out.SchemaVersion, &out.CanonicalJSON, &out.SHA256, &out.Actor, &out.CreatedAt, &archived)
	if err == sql.ErrNoRows {
		return ProfileRevisionRow{}, fmt.Errorf("store: profile %s: %w", profileID, model.ErrNotFound)
	}
	if err != nil {
		return ProfileRevisionRow{}, err
	}
	out.Archived = archived != 0
	return out, nil
}

func (s *Store) ListCurrentProfiles(ctx context.Context, limit, offset int) ([]ProfileRevisionRow, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := s.q.QueryContext(ctx, `SELECT r.profile_id, r.revision, r.schema_version, r.canonical_json, r.sha256, r.actor, r.created_at, p.archived
  FROM processing_profiles p JOIN processing_profile_revisions r
    ON r.profile_id = p.profile_id AND r.revision = p.current_revision
  ORDER BY p.profile_id LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []ProfileRevisionRow{}
	for rows.Next() {
		var row ProfileRevisionRow
		var archived int
		if err := rows.Scan(&row.ProfileID, &row.Revision, &row.SchemaVersion, &row.CanonicalJSON, &row.SHA256, &row.Actor, &row.CreatedAt, &archived); err != nil {
			return nil, err
		}
		row.Archived = archived != 0
		out = append(out, row)
	}
	return out, rows.Err()
}

// InsertProfileRevision appends one immutable revision and moves the current pointer.
// expectedCurrent is 0 when the profile does not exist yet.
func (s *Store) InsertProfileRevision(ctx context.Context, row ProfileRevisionRow, expectedCurrent int) error {
	now := row.CreatedAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if expectedCurrent == 0 {
		if _, err := s.q.ExecContext(ctx, `INSERT INTO processing_profiles
  (profile_id, current_revision, archived, created_at, updated_at) VALUES (?, ?, 0, ?, ?)`,
			row.ProfileID, row.Revision, now, now); err != nil {
			return fmt.Errorf("store: create profile %s: %w", row.ProfileID, err)
		}
	}
	if _, err := s.q.ExecContext(ctx, `INSERT INTO processing_profile_revisions
  (profile_id, revision, schema_version, canonical_json, sha256, actor, created_at)
  VALUES (?, ?, ?, ?, ?, ?, ?)`,
		row.ProfileID, row.Revision, row.SchemaVersion, row.CanonicalJSON, row.SHA256, row.Actor, now); err != nil {
		return fmt.Errorf("store: insert profile revision %s: %w", row.ProfileID, err)
	}
	if expectedCurrent == 0 {
		return nil
	}
	res, err := s.q.ExecContext(ctx, `UPDATE processing_profiles
  SET current_revision = ?, updated_at = ? WHERE profile_id = ? AND current_revision = ?`,
		row.Revision, now, row.ProfileID, expectedCurrent)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("store: profile %s revision conflict: %w", row.ProfileID, model.ErrConflict)
	}
	return nil
}
