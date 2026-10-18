package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// ListLibraryAssets returns every asset id, status and agent_visible flag,
// ordered by asset_id. The service decides which of those form the visible set.
func (s *Store) ListLibraryAssets(ctx context.Context) ([]model.VisibleAsset, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT asset_id, status, agent_visible FROM assets ORDER BY asset_id`)
	if err != nil {
		return nil, fmt.Errorf("store: list library assets: %w", err)
	}
	defer rows.Close()
	out := []model.VisibleAsset{}
	for rows.Next() {
		var a model.VisibleAsset
		var vis int
		if err := rows.Scan(&a.AssetID, &a.Status, &vis); err != nil {
			return nil, fmt.Errorf("store: scan library asset: %w", err)
		}
		a.AgentVisible = vis != 0
		a.Ingested = a.Status != ""
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list library assets: %w", err)
	}
	return out, nil
}

func (s *Store) InsertVisibleConfirmation(ctx context.Context, c model.VisibleConfirmation) error {
	body, err := json.Marshal(c.AssetIDs)
	if err != nil {
		return err
	}
	if c.AssetIDs == nil {
		body = []byte("[]")
	}
	_, err = s.q.ExecContext(ctx, `INSERT INTO visible_set_confirmations
  (set_sha, asset_ids_json, actor, script_finalize, created_at)
  VALUES (?, ?, ?, ?, ?)`,
		c.SetSHA, string(body), c.Actor, boolToInt(c.ScriptFinalize), c.CreatedAt.UTC())
	if err != nil {
		return fmt.Errorf("store: insert visible confirmation: %w", err)
	}
	return nil
}

func (s *Store) LatestVisibleConfirmation(ctx context.Context) (model.VisibleConfirmation, error) {
	var c model.VisibleConfirmation
	var raw string
	var fin int
	err := s.q.QueryRowContext(ctx, `SELECT id, set_sha, asset_ids_json, actor, script_finalize, created_at
  FROM visible_set_confirmations ORDER BY id DESC LIMIT 1`).Scan(&c.ID, &c.SetSHA, &raw, &c.Actor, &fin, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return model.VisibleConfirmation{}, model.ErrNotFound
	}
	if err != nil {
		return model.VisibleConfirmation{}, fmt.Errorf("store: latest visible confirmation: %w", err)
	}
	c.ScriptFinalize = fin != 0
	if err := json.Unmarshal([]byte(raw), &c.AssetIDs); err != nil {
		return model.VisibleConfirmation{}, fmt.Errorf("store: visible confirmation ids: %w", err)
	}
	if c.AssetIDs == nil {
		c.AssetIDs = []string{}
	}
	return c, nil
}

func (s *Store) InsertScript(ctx context.Context, sc model.Script) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO scripts
  (script_id, asset_id, status, visible_set_sha, title, body, finalized_by, finalized_at, stale_reason, created_at, updated_at)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sc.ScriptID, sc.AssetID, sc.Status, sc.VisibleSetSHA, sc.Title, sc.Body, sc.FinalizedBy,
		nullTime(sc.FinalizedAt), sc.StaleReason, sc.CreatedAt.UTC(), sc.UpdatedAt.UTC())
	if err != nil {
		return fmt.Errorf("store: insert script %s: %w", sc.ScriptID, err)
	}
	return nil
}

func scanScript(row interface{ Scan(...any) error }) (model.Script, error) {
	var sc model.Script
	var finalized sql.NullTime
	err := row.Scan(&sc.ScriptID, &sc.AssetID, &sc.Status, &sc.VisibleSetSHA, &sc.Title, &sc.Body,
		&sc.FinalizedBy, &finalized, &sc.StaleReason, &sc.CreatedAt, &sc.UpdatedAt)
	if err != nil {
		return model.Script{}, err
	}
	if finalized.Valid {
		t := finalized.Time.UTC()
		sc.FinalizedAt = &t
	}
	return sc, nil
}

const scriptColumns = `script_id, asset_id, status, visible_set_sha, title, body, finalized_by, finalized_at, stale_reason, created_at, updated_at`

func (s *Store) GetScript(ctx context.Context, id string) (model.Script, error) {
	sc, err := scanScript(s.q.QueryRowContext(ctx, `SELECT `+scriptColumns+` FROM scripts WHERE script_id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return model.Script{}, fmt.Errorf("store: get script %s: %w", id, model.ErrNotFound)
	}
	if err != nil {
		return model.Script{}, fmt.Errorf("store: get script %s: %w", id, err)
	}
	return sc, nil
}

func (s *Store) ListScripts(ctx context.Context, assetID string) ([]model.Script, error) {
	q := `SELECT ` + scriptColumns + ` FROM scripts`
	args := []any{}
	if assetID != "" {
		q += ` WHERE asset_id = ?`
		args = append(args, assetID)
	}
	q += ` ORDER BY created_at, script_id`
	rows, err := s.q.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: list scripts: %w", err)
	}
	defer rows.Close()
	out := []model.Script{}
	for rows.Next() {
		sc, err := scanScript(rows)
		if err != nil {
			return nil, fmt.Errorf("store: scan script: %w", err)
		}
		out = append(out, sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list scripts: %w", err)
	}
	return out, nil
}

func (s *Store) UpdateScript(ctx context.Context, sc model.Script) error {
	res, err := s.q.ExecContext(ctx, `UPDATE scripts SET status = ?, visible_set_sha = ?, title = ?, body = ?,
  finalized_by = ?, finalized_at = ?, stale_reason = ?, updated_at = ? WHERE script_id = ?`,
		sc.Status, sc.VisibleSetSHA, sc.Title, sc.Body, sc.FinalizedBy, nullTime(sc.FinalizedAt),
		sc.StaleReason, sc.UpdatedAt.UTC(), sc.ScriptID)
	if err != nil {
		return fmt.Errorf("store: update script %s: %w", sc.ScriptID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("store: update script %s: %w", sc.ScriptID, model.ErrNotFound)
	}
	return nil
}

// ExpireOpenScripts marks draft scripts expired and finalized scripts as
// needing a new finalization. Rows are kept.
func (s *Store) ExpireOpenScripts(ctx context.Context, now time.Time, reason string) (int, error) {
	res, err := s.q.ExecContext(ctx, `UPDATE scripts SET
  status = CASE status WHEN ? THEN ? ELSE ? END,
  stale_reason = ?, updated_at = ?
  WHERE status IN (?, ?)`,
		model.ScriptFinalized, model.ScriptNeedsRefinalize, model.ScriptExpired,
		reason, now.UTC(), model.ScriptDraft, model.ScriptFinalized)
	if err != nil {
		return 0, fmt.Errorf("store: expire scripts: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func (s *Store) UpsertDeliveryInvalidation(ctx context.Context, assetID, reason string, now time.Time) error {
	_, err := s.q.ExecContext(ctx, `INSERT INTO delivery_invalidations (asset_id, reason, invalidated_at)
  VALUES (?, ?, ?)
  ON CONFLICT(asset_id) DO UPDATE SET reason = excluded.reason, invalidated_at = excluded.invalidated_at`,
		assetID, reason, now.UTC())
	if err != nil {
		return fmt.Errorf("store: invalidate delivery %s: %w", assetID, err)
	}
	return nil
}

func (s *Store) DeliveryInvalidation(ctx context.Context, assetID string) (string, bool, error) {
	var reason string
	err := s.q.QueryRowContext(ctx, `SELECT reason FROM delivery_invalidations WHERE asset_id = ?`, assetID).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("store: delivery invalidation %s: %w", assetID, err)
	}
	return reason, true, nil
}

func (s *Store) ClearDeliveryInvalidation(ctx context.Context, assetID string) error {
	_, err := s.q.ExecContext(ctx, `DELETE FROM delivery_invalidations WHERE asset_id = ?`, assetID)
	if err != nil {
		return fmt.Errorf("store: clear delivery invalidation %s: %w", assetID, err)
	}
	return nil
}

// DownstreamAssetIDs are assets that already have a script, an export, or a preview.
func (s *Store) DownstreamAssetIDs(ctx context.Context) ([]string, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT asset_id FROM assets WHERE status = ?
  UNION SELECT asset_id FROM scripts
  UNION SELECT asset_id FROM preview_jobs
  UNION SELECT asset_id FROM delivery_variants`, model.AssetStatusExported)
	if err != nil {
		return nil, fmt.Errorf("store: downstream assets: %w", err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
