package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"
)

type JournalEntry struct {
	Sequence    int64           `json:"sequence"`
	RunID       string          `json:"run_id"`
	Body        json.RawMessage `json:"body"`
	PreviousSHA string          `json:"previous_sha256"`
	SHA         string          `json:"sha256"`
}

func journalSHA(previous, run string, body []byte) string {
	raw, _ := json.Marshal([]string{previous, run, string(body)})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}

// Call inside a Store transaction so concurrent writers extend a single chain.
func (s *Store) AppendAgentJournal(ctx context.Context, run string, body any) error {
	if !s.tx {
		return errors.New("journal append requires transaction")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	previous := ""
	err = s.q.QueryRowContext(ctx, "SELECT sha FROM vac_agent_journal ORDER BY sequence DESC LIMIT 1").Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	_, err = s.q.ExecContext(ctx, "INSERT INTO vac_agent_journal(run_id,body,previous_sha,sha) VALUES (?,?,?,?)", run, string(raw), previous, journalSHA(previous, run, raw))
	return err
}
func (s *Store) AgentJournal(ctx context.Context, run string, after int64, limit int) ([]JournalEntry, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.q.QueryContext(ctx, "SELECT sequence,run_id,body,previous_sha,sha FROM vac_agent_journal WHERE sequence>? AND (?='' OR run_id=?) ORDER BY sequence LIMIT ?", after, run, run, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []JournalEntry{}
	for rows.Next() {
		var e JournalEntry
		var body string
		if err = rows.Scan(&e.Sequence, &e.RunID, &body, &e.PreviousSHA, &e.SHA); err != nil {
			return nil, err
		}
		e.Body = json.RawMessage(body)
		out = append(out, e)
	}
	return out, rows.Err()
}
func (s *Store) VerifyAgentJournal(ctx context.Context) (int64, error) {
	var after int64
	previous := ""
	for {
		rows, err := s.AgentJournal(ctx, "", after, 500)
		if err != nil {
			return after, err
		}
		if len(rows) == 0 {
			return after, nil
		}
		for _, e := range rows {
			if e.PreviousSHA != previous || journalSHA(previous, e.RunID, e.Body) != e.SHA {
				return after, model.ErrConflict
			}
			after = e.Sequence
			previous = e.SHA
		}
	}
}

type EditCheckpoint struct {
	ID       string          `json:"id"`
	RunID    string          `json:"run_id"`
	ParentID string          `json:"parent_id"`
	Body     json.RawMessage `json:"snapshot"`
	SHA      string          `json:"sha256"`
}
type EditCursor struct {
	ID   string   `json:"checkpoint_id"`
	Redo []string `json:"redo"`
}

func (s *Store) EditCursor(ctx context.Context, run string) (EditCursor, error) {
	var c EditCursor
	var raw string
	err := s.q.QueryRowContext(ctx, "SELECT checkpoint_id,redo FROM vac_edit_cursors WHERE run_id=?", run).Scan(&c.ID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	err = json.Unmarshal([]byte(raw), &c.Redo)
	return c, err
}
func (s *Store) SetEditCursor(ctx context.Context, run string, c EditCursor) error {
	raw, err := json.Marshal(c.Redo)
	if err != nil {
		return err
	}
	_, err = s.q.ExecContext(ctx, "INSERT INTO vac_edit_cursors VALUES(?,?,?) ON CONFLICT(run_id) DO UPDATE SET checkpoint_id=excluded.checkpoint_id,redo=excluded.redo", run, c.ID, string(raw))
	return err
}
func (s *Store) PutEditCheckpoint(ctx context.Context, c EditCheckpoint) error {
	_, err := s.q.ExecContext(ctx, "INSERT INTO vac_edit_checkpoints VALUES(?,?,?,?,?)", c.ID, c.RunID, c.ParentID, string(c.Body), c.SHA)
	return err
}
func (s *Store) EditCheckpoints(ctx context.Context, run string) ([]EditCheckpoint, error) {
	rows, err := s.q.QueryContext(ctx, "SELECT id,run_id,parent_id,body,sha FROM vac_edit_checkpoints WHERE run_id=? ORDER BY rowid", run)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EditCheckpoint{}
	for rows.Next() {
		var c EditCheckpoint
		var raw string
		if err = rows.Scan(&c.ID, &c.RunID, &c.ParentID, &raw, &c.SHA); err != nil {
			return nil, err
		}
		c.Body = json.RawMessage(raw)
		out = append(out, c)
	}
	return out, rows.Err()
}
