package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/tts"
)

func (s *Store) GetTTSTemplate(ctx context.Context, id string, revision int) (tts.Template, error) {
	var raw string
	var archived bool
	query := `SELECT r.document_json, t.archived FROM tts_templates t JOIN tts_template_revisions r ON r.template_id=t.template_id WHERE t.template_id=? AND r.revision=t.current_revision`
	args := []any{id}
	if revision > 0 {
		query = `SELECT r.document_json, t.archived FROM tts_templates t JOIN tts_template_revisions r ON r.template_id=t.template_id WHERE t.template_id=? AND r.revision=?`
		args = append(args, revision)
	}
	e := s.q.QueryRowContext(ctx, query, args...).Scan(&raw, &archived)
	if e == sql.ErrNoRows {
		return tts.Template{}, model.ErrNotFound
	}
	if e != nil {
		return tts.Template{}, e
	}
	var out tts.Template
	e = json.Unmarshal([]byte(raw), &out)
	out.Archived = archived
	return out, e
}
func (s *Store) ListTTSTemplates(ctx context.Context) ([]tts.Template, error) {
	rows, e := s.q.QueryContext(ctx, `SELECT r.document_json FROM tts_templates t JOIN tts_template_revisions r ON r.template_id=t.template_id AND r.revision=t.current_revision WHERE t.archived=0 ORDER BY t.template_id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []tts.Template{}
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var t tts.Template
		if e = json.Unmarshal([]byte(raw), &t); e != nil {
			return nil, e
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
func (s *Store) SaveTTSTemplate(ctx context.Context, t tts.Template, expected int) error {
	if expected == 0 {
		if _, e := s.q.ExecContext(ctx, `INSERT INTO tts_templates(template_id,current_revision) VALUES(?,?)`, t.TemplateID, t.Revision); e != nil {
			return e
		}
	} else {
		result, e := s.q.ExecContext(ctx, `UPDATE tts_templates SET current_revision=? WHERE template_id=? AND current_revision=? AND archived=0`, t.Revision, t.TemplateID, expected)
		if e != nil {
			return e
		}
		n, e := result.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return model.ErrConflict
		}
	}
	raw, e := json.Marshal(t)
	if e != nil {
		return e
	}
	_, e = s.q.ExecContext(ctx, `INSERT INTO tts_template_revisions(template_id,revision,document_json) VALUES(?,?,?)`, t.TemplateID, t.Revision, string(raw))
	return e
}
func (s *Store) ArchiveTTSTemplate(ctx context.Context, id string, expected int) error {
	r, e := s.q.ExecContext(ctx, `UPDATE tts_templates SET archived=1 WHERE template_id=? AND current_revision=?`, id, expected)
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
func (s *Store) PutTTSPreview(ctx context.Context, p tts.Preview) error {
	b, e := json.Marshal(p)
	if e != nil {
		return e
	}
	_, e = s.q.ExecContext(ctx, `INSERT INTO tts_preview_jobs(job_id,document_json) VALUES(?,?) ON CONFLICT(job_id) DO UPDATE SET document_json=excluded.document_json`, p.JobID, string(b))
	return e
}
func (s *Store) GetTTSPreview(ctx context.Context, id string) (tts.Preview, error) {
	var raw string
	e := s.q.QueryRowContext(ctx, `SELECT document_json FROM tts_preview_jobs WHERE job_id=?`, id).Scan(&raw)
	if e == sql.ErrNoRows {
		return tts.Preview{}, model.ErrNotFound
	}
	if e != nil {
		return tts.Preview{}, e
	}
	var out tts.Preview
	e = json.Unmarshal([]byte(raw), &out)
	return out, e
}
func (s *Store) ListTTSPreviews(ctx context.Context) ([]tts.Preview, error) {
	rows, e := s.q.QueryContext(ctx, `SELECT document_json FROM tts_preview_jobs ORDER BY rowid DESC`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []tts.Preview{}
	for rows.Next() {
		var raw string
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		var p tts.Preview
		if e = json.Unmarshal([]byte(raw), &p); e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
