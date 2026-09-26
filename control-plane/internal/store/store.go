// Package store owns the SQLite database. It contains no business rules;
// every validation lives in service.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	// Register the pure-Go SQLite driver. Driver-level behaviour (Windows
	// absolute paths, WAL, busy timeout) is configured through the DSN.
	_ "modernc.org/sqlite"
)

// migrationsFS holds the embedded .sql migration files shipped with the binary.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Store is a thin wrapper over *sql.DB. It performs no business logic and
// holds no state beyond the connection pool.
type Store struct {
	db *sql.DB
}

// Open opens (creating if necessary) the SQLite database at path, applies all
// pending embedded migrations and returns a ready store.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("store: open sqlite at %s: %w", path, err)
	}

	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(time.Hour)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: ping sqlite at %s: %w", path, err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: migrate sqlite at %s: %w", path, err)
	}

	return &Store{db: db}, nil
}

// DB exposes the underlying *sql.DB for callers that need raw access.
func (s *Store) DB() *sql.DB { return s.db }

// Close releases every connection in the pool. A repeat call returns an error
// rather than silently pretending the store is usable again.
func (s *Store) Close() error {
	if err := s.db.Close(); err != nil {
		if errors.Is(err, sql.ErrConnDone) {
			return nil
		}
		return fmt.Errorf("store: close: %w", err)
	}
	return nil
}

// migrationApplied reports whether version is already recorded in the ledger.
func migrationApplied(db *sql.DB, version int) (bool, error) {
	var recorded bool
	if err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = ?)`, version).Scan(&recorded); err != nil {
		return false, err
	}
	return recorded, nil
}

// migrate creates the schema_migrations ledger if needed and applies every
// embedded migration that is not recorded there yet. Each file runs inside its
// own transaction, so a half-applied file never becomes a recorded version.
func migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY
)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".sql" {
			continue
		}

		var version int
		if _, err := fmt.Sscanf(e.Name(), "%04d", &version); err != nil {
			return fmt.Errorf("parse migration version from %s: %w", e.Name(), err)
		}

		applied, err := migrationApplied(db, version)
		if err != nil {
			return fmt.Errorf("check migration %d: %w", version, err)
		}
		if applied {
			continue
		}

		sqlBytes, err := migrationsFS.ReadFile("migrations/" + e.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", e.Name(), err)
		}

		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %d: %w", version, err)
		}

		if _, err := tx.Exec(string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", e.Name(), err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", e.Name(), err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", e.Name(), err)
		}
	}

	return nil
}
