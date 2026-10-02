package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// NarrationApproval is one human review of a narration draft, keyed by the
// draft hash the service computed. The store does not decide whether the
// hash matches a draft; it only keeps the row the service asked to keep.
type NarrationApproval struct {
	AssetID   string
	DraftHash string
	Text      string
	Start     float64
	End       float64
	Source    string
	CreatedAt time.Time
}

// PutNarrationApproval inserts one review. A repeated hash for the same asset
// is not an error: created is false and the existing row is left untouched.
func (s *Store) PutNarrationApproval(ctx context.Context, row NarrationApproval) (bool, error) {
	if row.AssetID == "" || row.DraftHash == "" {
		return false, fmt.Errorf("store: narration approval: asset_id and draft_hash are required")
	}
	res, err := s.q.ExecContext(ctx, `INSERT INTO narration_reviews
  (asset_id, draft_hash, text, start_sec, end_sec, source, created_at)
  VALUES (?, ?, ?, ?, ?, ?, ?)`,
		row.AssetID, row.DraftHash, row.Text, row.Start, row.End, row.Source, time.Now().UTC())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return false, nil
		}
		return false, fmt.Errorf("store: put narration approval %s: %w", row.AssetID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: put narration approval %s: rows affected: %w", row.AssetID, err)
	}
	return n > 0, nil
}

// ListNarrationApprovals returns the draft hashes recorded for an asset,
// ordered so two reads of the same set compare equal.
func (s *Store) ListNarrationApprovals(ctx context.Context, assetID string) ([]string, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT draft_hash FROM narration_reviews
  WHERE asset_id = ? ORDER BY draft_hash`, assetID)
	if err != nil {
		return nil, fmt.Errorf("store: list narration approvals %s: %w", assetID, err)
	}
	defer func() { _ = rows.Close() }()
	out := []string{}
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, fmt.Errorf("store: scan narration approval: %w", err)
		}
		out = append(out, hash)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list narration approvals %s: %w", assetID, err)
	}
	return out, nil
}

// DeleteNarrationApproval removes one review. deleted is false when no row
// matched, which the service reports as not found.
func (s *Store) DeleteNarrationApproval(ctx context.Context, assetID, draftHash string) (bool, error) {
	res, err := s.q.ExecContext(ctx, `DELETE FROM narration_reviews WHERE asset_id = ? AND draft_hash = ?`,
		assetID, draftHash)
	if err != nil {
		return false, fmt.Errorf("store: delete narration approval %s: %w", assetID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: delete narration approval %s: rows affected: %w", assetID, err)
	}
	return n > 0, nil
}
