package store_test

import (
	"context"
	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
	"path/filepath"
	"testing"
)

func TestAgentJournalAndCheckpointsAreImmutable(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	err = s.Transaction(ctx, func(tx *store.Store) error {
		return tx.AppendAgentJournal(ctx, "run", map[string]any{"action": "test"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec("UPDATE vac_agent_journal SET body='{}'"); err == nil {
		t.Fatal("journal updated")
	}
	if _, err = s.DB().Exec("DELETE FROM vac_agent_journal"); err == nil {
		t.Fatal("journal deleted")
	}
	if n, err := s.VerifyAgentJournal(ctx); err != nil || n != 1 {
		t.Fatalf("chain %d %v", n, err)
	}
	if err = s.PutEditCheckpoint(ctx, store.EditCheckpoint{ID: "cp", RunID: "run", Body: []byte(`{}`), SHA: "fixture"}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec("UPDATE vac_edit_checkpoints SET body='[]'"); err == nil {
		t.Fatal("checkpoint updated")
	}
	if _, err = s.DB().Exec("DELETE FROM vac_edit_checkpoints"); err == nil {
		t.Fatal("checkpoint deleted")
	}
}
