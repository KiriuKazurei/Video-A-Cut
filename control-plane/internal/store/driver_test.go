package store_test

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDriverInMemory(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var version string
	if err := db.QueryRow("select sqlite_version()").Scan(&version); err != nil {
		t.Fatalf("query: %v", err)
	}
	if version == "" {
		t.Fatal("empty sqlite version")
	}
}
