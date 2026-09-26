package store_test

import (
	"testing"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/store"
)

// mustMust opens a store on a fresh temp DB and fails the test on any error.
// t.TempDir() returns backslash-separated absolute paths on Windows; modernc
// SQLite handles those directly, so no path rewriting happens here.
func mustMust(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(t.TempDir() + "/vac.db")
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestOpenAppliesMigrations(t *testing.T) {
	s := mustMust(t)

	for _, table := range []string{"assets", "tasks", "agents", "audit_logs"} {
		var name string
		err := s.DB().QueryRow(`select name from sqlite_master where type='table' and name=?`, table).Scan(&name)
		if err != nil {
			t.Fatalf("table %s missing from sqlite_master: %v", table, err)
		}
		if name != table {
			t.Fatalf("table %s: got %q", table, name)
		}
	}
}

// TestOpenIsIdempotent reopens the same database file and verifies the
// migration ledger does not re-apply the already-recorded version.
func TestOpenIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	s1, err := store.Open(dir + "/vac.db")
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("close first: %v", err)
	}

	s2, err := store.Open(dir + "/vac.db")
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer func() { _ = s2.Close() }()

	var count int
	if err := s2.DB().QueryRow(`select count(*) from schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("schema_migrations rows = %d, want 1", count)
	}
}
