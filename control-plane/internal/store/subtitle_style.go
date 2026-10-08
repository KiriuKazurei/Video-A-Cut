package store

import (
	"context"
	"database/sql"
	"errors"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func (s *Store) GetSubtitleStyle(ctx context.Context, run, rev string) (int, string, error) {
	var version int
	var raw string
	e := s.q.QueryRowContext(ctx, `SELECT draft_version,document_json FROM subtitle_style_drafts WHERE run_id=? AND revision_id=?`, run, rev).Scan(&version, &raw)
	if errors.Is(e, sql.ErrNoRows) {
		return 0, "", nil
	}
	return version, raw, e
}
func (s *Store) PutSubtitleStyle(ctx context.Context, run, rev string, expected int, raw string) error {
	if expected == 0 {
		r, e := s.q.ExecContext(ctx, `INSERT INTO subtitle_style_drafts VALUES(?,?,1,?) ON CONFLICT DO NOTHING`, run, rev, raw)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return model.ErrConflict
		}
		return nil
	}
	r, e := s.q.ExecContext(ctx, `UPDATE subtitle_style_drafts SET draft_version=draft_version+1,document_json=? WHERE run_id=? AND revision_id=? AND draft_version=?`, raw, run, rev, expected)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e == nil && n != 1 {
		return model.ErrConflict
	}
	return e
}
