package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

// Capture only editorial state. Ownership, budgets and permissions never rewind.
func editSnapshot(raw []byte) ([]byte, error) {
	var source map[string]json.RawMessage
	if err := json.Unmarshal(raw, &source); err != nil {
		return nil, err
	}
	out := map[string]json.RawMessage{}
	for _, key := range []string{"plan", "summary", "frames", "artifacts", "inspection", "edit_request"} {
		v := source[key]
		if len(v) == 0 {
			v = json.RawMessage("null")
		}
		out[key] = v
	}
	return json.Marshal(out)
}
func (s *Store) checkpointRun(ctx context.Context, id string, raw []byte) error {
	snapshot, err := editSnapshot(raw)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(snapshot)
	sha := hex.EncodeToString(hash[:])
	cursor, err := s.EditCursor(ctx, id)
	if err != nil {
		return err
	}
	if cursor.ID != "" {
		var current string
		err = s.q.QueryRowContext(ctx, "SELECT sha FROM vac_edit_checkpoints WHERE id=? AND run_id=?", cursor.ID, id).Scan(&current)
		if err != nil {
			return err
		}
		if current == sha {
			return nil
		}
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return err
	}
	checkpoint := EditCheckpoint{ID: "cp_" + hex.EncodeToString(token[:]), RunID: id, ParentID: cursor.ID, Body: snapshot, SHA: sha}
	if err = s.PutEditCheckpoint(ctx, checkpoint); err != nil {
		return err
	}
	return s.SetEditCursor(ctx, id, EditCursor{ID: checkpoint.ID, Redo: []string{}})
}

// Restore writes a new run version and revision while preserving the history graph.
func (s *Store) RestoreEditRun(ctx context.Context, id string, expected int, in any, cursor EditCursor) error {
	if !s.tx {
		return model.ErrInvalidState
	}
	if err := s.skillPut(ctx, "run", id, expected, in, false); err != nil {
		return err
	}
	return s.SetEditCursor(ctx, id, cursor)
}
func (s *Store) NextSkillPlanVersion(ctx context.Context, id string) (int, error) {
	var next int
	err := s.q.QueryRowContext(ctx, "SELECT COALESCE(MAX(CAST(json_extract(body,'$.version') AS INTEGER)),0)+1 FROM vac_skill_revisions WHERE run_id=?", id).Scan(&next)
	return next, err
}
func (s *Store) EnsureSkillCheckpoint(ctx context.Context, id string) error {
	return s.Transaction(ctx, func(tx *Store) error {
		cursor, e := tx.EditCursor(ctx, id)
		if e != nil {
			return e
		}
		if cursor.ID != "" {
			return nil
		}
		var raw json.RawMessage
		if e = tx.SkillGet(ctx, "run", id, &raw); e != nil {
			return e
		}
		return tx.checkpointRun(ctx, id, raw)
	})
}
