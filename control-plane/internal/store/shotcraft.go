package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

func (s *Store) ListShotTemplates(ctx context.Context) ([]string, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT payload FROM shotcraft_user_templates ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var payload string
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		result = append(result, payload)
	}
	return result, rows.Err()
}

func (s *Store) ShotTemplate(ctx context.Context, id string) (string, error) {
	var payload string
	err := s.q.QueryRowContext(ctx, "SELECT payload FROM shotcraft_user_templates WHERE id = ?", id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return "", model.ErrNotFound
	}
	return payload, err
}

func (s *Store) InsertShotTemplate(ctx context.Context, id, payload string) error {
	res, err := s.q.ExecContext(ctx, "INSERT INTO shotcraft_user_templates (id, payload) VALUES (?, ?) ON CONFLICT(id) DO NOTHING", id, payload)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err == nil && count == 0 {
		return model.ErrConflict
	}
	return err
}

func (s *Store) DeleteShotTemplate(ctx context.Context, id string) error {
	res, err := s.q.ExecContext(ctx, "DELETE FROM shotcraft_user_templates WHERE id = ?", id)
	if err != nil {
		return err
	}
	count, err := res.RowsAffected()
	if err == nil && count == 0 {
		return model.ErrNotFound
	}
	return err
}

func (s *Store) ShotTemplateDirectory(ctx context.Context) (string, error) {
	var directory string
	err := s.q.QueryRowContext(ctx, "SELECT directory FROM shotcraft_library_settings WHERE singleton = 1").Scan(&directory)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return directory, err
}

func (s *Store) SetShotTemplateDirectory(ctx context.Context, directory string) error {
	_, err := s.q.ExecContext(ctx, "INSERT INTO shotcraft_library_settings (singleton, directory) VALUES (1, ?) ON CONFLICT(singleton) DO UPDATE SET directory = excluded.directory", directory)
	return err
}
