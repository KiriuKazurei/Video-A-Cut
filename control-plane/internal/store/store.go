// Package store owns the SQLite database. It contains no business rules;
// every validation lives in service.
package store

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/KiriuKazurei/Video-A-Cut/control-plane/internal/model"

	// Register the pure-Go SQLite driver. Driver-level behaviour (Windows
	// absolute paths, WAL, busy timeout) is configured through the DSN.
	_ "modernc.org/sqlite"
)

// migrationsFS holds the embedded .sql migration files shipped with the binary.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// defaultAuditLimit is the page size ListAudit falls back to when the
// caller passes a non-positive limit.
const defaultAuditLimit = 100

// Store is a thin wrapper over *sql.DB. It performs no business logic and
// holds no state beyond the connection pool.
type Store struct {
	db *sql.DB
	q  queryExecutor
	// tx is true on the transaction-scoped Store passed to Transaction. It
	// prevents a callback from accidentally starting a nested transaction on
	// the pool while its outer SQLite write lock is held.
	tx bool
}

type queryExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Open opens (creating if necessary) the SQLite database at path, applies all
// pending embedded migrations and returns a ready store.
func Open(path string) (*Store, error) {
	dsn, err := databaseDSN(path)
	if err != nil {
		return nil, fmt.Errorf("store: prepare sqlite path %s: %w", path, err)
	}
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

	return &Store{db: db, q: db}, nil
}

// databaseDSN encodes the filesystem path as a file URL before adding driver
// options. Raw concatenation treats # as a fragment, % as an escape and ? as
// the start of the query, so Ping can succeed while SQLite opens a different
// file from the one the caller named.
func databaseDSN(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	slash := filepath.ToSlash(abs)
	// A Windows drive path must be rooted in the URL path so url.URL does not
	// mistake the drive letter for a hostname.
	if len(slash) >= 2 && slash[1] == ':' {
		slash = "/" + slash
	}
	u := url.URL{Scheme: "file", Path: slash}
	u.RawQuery = "_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)"
	return u.String(), nil
}

// Transaction runs fn on one SQLite transaction and commits only when fn
// returns nil. The DSN's _txlock=immediate obtains SQLite's write reservation
// before service reads state, serializing read/modify/write workflows across
// all Store and Service instances that open the same database.
func (s *Store) Transaction(ctx context.Context, fn func(*Store) error) error {
	if s.tx {
		return errors.New("store: nested transaction")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	view := &Store{db: s.db, q: tx, tx: true}
	if err := fn(view); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return errors.Join(err, fmt.Errorf("store: rollback transaction: %w", rollbackErr))
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: commit transaction: %w", err)
	}
	return nil
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

// scanner is the subset of *sql.Row / *sql.Rows the asset scan path needs,
// so both call sites can share scanAsset.
type scanner interface {
	Scan(dest ...any) error
}

// marshalStringMap encodes a string map as a JSON object. A nil or empty map
// encodes as "{}" so the NOT NULL artifacts column always holds valid JSON.
func marshalStringMap(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// marshalStringSlice encodes a string slice as a JSON array. A nil or empty
// slice encodes as "[]" so the NOT NULL allowed_agents column always holds
// valid JSON.
func marshalStringSlice(s []string) string {
	if len(s) == 0 {
		return "[]"
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// scanAsset decodes one assets row into a model.Asset. sql.ErrNoRows is
// translated to model.ErrNotFound so callers can use errors.Is uniformly.
func scanAsset(row scanner) (model.Asset, error) {
	var (
		a          model.Asset
		agentsJSON string
		artsJSON   string
		agentVis   int
		humanAppr  int
		locked     int
	)

	scanErr := row.Scan(&a.AssetID, &a.Status, &agentVis, &humanAppr, &locked,
		&agentsJSON, &artsJSON, &a.CreatedAt, &a.UpdatedAt)
	if scanErr != nil {
		if errors.Is(scanErr, sql.ErrNoRows) {
			return model.Asset{}, fmt.Errorf("store: asset: %w", model.ErrNotFound)
		}
		return model.Asset{}, fmt.Errorf("store: scan asset: %w", scanErr)
	}

	agents, err := unmarshalStringSlice(agentsJSON)
	if err != nil {
		return model.Asset{}, fmt.Errorf("store: decode asset %s allowed_agents: %w", a.AssetID, err)
	}
	arts, err := unmarshalStringMap(artsJSON)
	if err != nil {
		return model.Asset{}, fmt.Errorf("store: decode asset %s artifacts: %w", a.AssetID, err)
	}

	a.AgentVisible = agentVis != 0
	a.HumanApproved = humanAppr != 0
	a.Locked = locked != 0
	a.AllowedAgents = agents
	a.Artifacts = arts
	return a, nil
}

// unmarshalStringSlice decodes a JSON array of strings. An empty document
// yields a nil slice, which behaves as empty for range/len/index purposes.
func unmarshalStringSlice(s string) ([]string, error) {
	if s == "" {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// unmarshalStringMap decodes a JSON object of string values.
func unmarshalStringMap(s string) (map[string]string, error) {
	if s == "" {
		return nil, nil
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateAsset inserts a new asset. CreatedAt and UpdatedAt are set to the
// current UTC time; callers must not pre-populate them. An empty AssetID is
// rejected with model.ErrArgument and a duplicate id with model.ErrConflict.
func (s *Store) CreateAsset(ctx context.Context, a model.Asset) error {
	if a.AssetID == "" {
		return fmt.Errorf("store: create asset: asset_id is required: %w", model.ErrArgument)
	}

	now := time.Now().UTC()
	_, err := s.q.ExecContext(ctx, `INSERT INTO assets
  (asset_id, status, agent_visible, human_approved, locked, allowed_agents, artifacts, created_at, updated_at)
  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.AssetID, a.Status, boolToInt(a.AgentVisible), boolToInt(a.HumanApproved), boolToInt(a.Locked),
		marshalStringSlice(a.AllowedAgents), marshalStringMap(a.Artifacts), now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			return fmt.Errorf("store: create asset %s: %w", a.AssetID, model.ErrConflict)
		}
		return fmt.Errorf("store: create asset %s: %w", a.AssetID, err)
	}
	return nil
}

// GetAsset returns the asset with the given id, or an error wrapping
// model.ErrNotFound.
func (s *Store) GetAsset(ctx context.Context, id string) (model.Asset, error) {
	row := s.q.QueryRowContext(ctx, `SELECT asset_id, status, agent_visible, human_approved, locked, allowed_agents, artifacts, created_at, updated_at
  FROM assets WHERE asset_id = ?`, id)

	a, err := scanAsset(row)
	if err != nil {
		return model.Asset{}, fmt.Errorf("store: get asset %s: %w", id, err)
	}
	return a, nil
}

// ListAssets returns every asset ordered by asset_id.
func (s *Store) ListAssets(ctx context.Context) ([]model.Asset, error) {
	rows, err := s.q.QueryContext(ctx, `SELECT asset_id, status, agent_visible, human_approved, locked, allowed_agents, artifacts, created_at, updated_at
  FROM assets ORDER BY asset_id`)
	if err != nil {
		return nil, fmt.Errorf("store: list assets: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []model.Asset{}
	for rows.Next() {
		a, err := scanAsset(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list assets: %w", err)
	}
	return out, nil
}

// UpdateAsset overwrites every mutable column of an existing asset and
// refuses silently-missing rows by wrapping model.ErrNotFound.
func (s *Store) UpdateAsset(ctx context.Context, a model.Asset) error {
	if a.AssetID == "" {
		return fmt.Errorf("store: update asset: asset_id is required: %w", model.ErrArgument)
	}

	res, err := s.q.ExecContext(ctx, `UPDATE assets SET
  status = ?, agent_visible = ?, human_approved = ?, locked = ?,
  allowed_agents = ?, artifacts = ?, updated_at = ?
  WHERE asset_id = ?`,
		a.Status, boolToInt(a.AgentVisible), boolToInt(a.HumanApproved), boolToInt(a.Locked),
		marshalStringSlice(a.AllowedAgents), marshalStringMap(a.Artifacts), time.Now().UTC(), a.AssetID)
	if err != nil {
		return fmt.Errorf("store: update asset %s: %w", a.AssetID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update asset %s: rows affected: %w", a.AssetID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: update asset %s: %w", a.AssetID, model.ErrNotFound)
	}
	return nil
}

// taskColumns is the projection the task read paths share. It is spelled out
// in full rather than with `select *` so a schema change cannot silently
// shift the scanTask column order. The column named `type` is a keyword-ish
// identifier but modernc accepts it unquoted here; it is only named in the
// projection, never in ORDER BY or GROUP BY, so no quoting is needed.
const taskColumns = `select task_id,asset_id,type,agent_role,agent_id,status,progress,message,lease_expires_at,claimed_at,updated_at,artifacts,depends_on,attempts from tasks`

// CreateTask inserts a task. updated_at is stamped by the store; all other
// columns come from the caller, so a task can be created in any status the
// caller needs to seed. An empty TaskID or AssetID is rejected with
// model.ErrArgument.
func (s *Store) CreateTask(ctx context.Context, tk model.Task) error {
	if tk.TaskID == "" {
		return fmt.Errorf("store: create task: task_id is required: %w", model.ErrArgument)
	}
	if tk.AssetID == "" {
		return fmt.Errorf("store: create task %s: asset_id is required: %w", tk.TaskID, model.ErrArgument)
	}

	_, err := s.q.ExecContext(ctx, `INSERT INTO tasks
	  (task_id, asset_id, type, agent_role, agent_id, status, progress, message, lease_expires_at, claimed_at, updated_at, artifacts, enqueued_at, depends_on, attempts)
	  VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tk.TaskID, tk.AssetID, tk.Type, tk.AgentRole, nullIfEmpty(tk.AgentID),
		tk.Status, tk.Progress, nullIfEmpty(tk.Message), tk.LeaseUntil, tk.ClaimedAt,
		time.Now().UTC(), marshalStringMap(tk.Artifacts), time.Now().UTC(),
		marshalStringSlice(tk.DependsOn), tk.Attempts)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint") {
			return fmt.Errorf("store: create task %s: %w", tk.TaskID, model.ErrConflict)
		}
		return fmt.Errorf("store: create task %s: %w", tk.TaskID, err)
	}
	return nil
}

// GetTask returns the task with the given id, or an error wrapping
// model.ErrNotFound.
func (s *Store) GetTask(ctx context.Context, id string) (model.Task, error) {
	row := s.q.QueryRowContext(ctx, taskColumns+` WHERE task_id = ?`, id)

	tk, err := scanTask(row)
	if err != nil {
		return model.Task{}, fmt.Errorf("store: get task %s: %w", id, err)
	}
	return tk, nil
}

// ClaimCandidates returns queued tasks for a role, oldest task_id first.
// It performs no leasing: that is service's job.
func (s *Store) ClaimCandidates(ctx context.Context, role string) ([]model.Task, error) {
	rows, err := s.q.QueryContext(ctx,
		taskColumns+` WHERE agent_role = ? AND status = ? ORDER BY CASE WHEN enqueued_at IS NULL THEN 0 ELSE 1 END, enqueued_at, rowid`,
		role, model.TaskStatusQueued)
	if err != nil {
		return nil, fmt.Errorf("store: claim candidates for role %s: %w", role, err)
	}
	defer func() { _ = rows.Close() }()

	out, err := scanTasks(rows)
	if err != nil {
		return nil, fmt.Errorf("store: claim candidates for role %s: %w", role, err)
	}
	return out, nil
}

// ListActiveTasks returns every task that has not reached a terminal status,
// oldest update first. It is the input of the lease-recovery sweep: the queue
// only needs to look at tasks that could still be holding a lease.
//
// The terminal set is spelled out as an inclusion list rather than a NOT IN
// exclusion so a future status added to model has to be placed explicitly on
// one side of the line. Ordering by updated_at puts the most stale — and
// therefore the first worth recovering — row first.
func (s *Store) ListActiveTasks(ctx context.Context) ([]model.Task, error) {
	rows, err := s.q.QueryContext(ctx,
		taskColumns+` WHERE status IN (?, ?, ?) ORDER BY updated_at`,
		model.TaskStatusQueued, model.TaskStatusClaimed, model.TaskStatusRunning)
	if err != nil {
		return nil, fmt.Errorf("store: list active tasks: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out, err := scanTasks(rows)
	if err != nil {
		return nil, fmt.Errorf("store: list active tasks: %w", err)
	}
	return out, nil
}

// UpdateTask overwrites the mutable columns of an existing task. A missing
// row wraps model.ErrNotFound. An empty TaskID is rejected with
// model.ErrArgument.
func (s *Store) UpdateTask(ctx context.Context, tk model.Task) error {
	if tk.TaskID == "" {
		return fmt.Errorf("store: update task: task_id is required: %w", model.ErrArgument)
	}

	res, err := s.q.ExecContext(ctx, `UPDATE tasks SET
  status = ?, progress = ?, message = ?, agent_id = ?, agent_role = ?,
  lease_expires_at = ?, claimed_at = ?, artifacts = ?, updated_at = ?, attempts = ?
  WHERE task_id = ?`,
		tk.Status, tk.Progress, nullIfEmpty(tk.Message), nullIfEmpty(tk.AgentID), tk.AgentRole,
		tk.LeaseUntil, tk.ClaimedAt, marshalStringMap(tk.Artifacts), time.Now().UTC(), tk.Attempts, tk.TaskID)
	if err != nil {
		return fmt.Errorf("store: update task %s: %w", tk.TaskID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: update task %s: rows affected: %w", tk.TaskID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: update task %s: %w", tk.TaskID, model.ErrNotFound)
	}
	return nil
}

// scanTask decodes one tasks row into a model.Task. The nullable agent_id,
// message, lease_expires_at and claimed_at columns are read through sql
// null wrappers so an absent value leaves the corresponding field at its
// zero value ("" / nil pointer) instead of failing the scan. Timestamps are
// normalised to UTC because SQLite stores DATETIME as text.
func scanTask(row scanner) (model.Task, error) {
	var (
		tk        model.Task
		agentID   sql.NullString
		message   sql.NullString
		lease     sql.NullTime
		claimedAt sql.NullTime
		artsJSON  string
		depsJSON  string
	)

	err := row.Scan(&tk.TaskID, &tk.AssetID, &tk.Type, &tk.AgentRole, &agentID, &tk.Status,
		&tk.Progress, &message, &lease, &claimedAt, &tk.UpdatedAt, &artsJSON, &depsJSON, &tk.Attempts)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Task{}, fmt.Errorf("store: task: %w", model.ErrNotFound)
		}
		return model.Task{}, fmt.Errorf("store: scan task: %w", err)
	}

	arts, err := unmarshalStringMap(artsJSON)
	if err != nil {
		return model.Task{}, fmt.Errorf("store: decode task %s artifacts: %w", tk.TaskID, err)
	}

	deps, err := unmarshalStringSlice(depsJSON)
	if err != nil {
		return model.Task{}, fmt.Errorf("store: decode task %s depends_on: %w", tk.TaskID, err)
	}
	if len(deps) > 0 {
		tk.DependsOn = deps
	}

	tk.AgentID = agentID.String
	tk.Message = message.String
	tk.Artifacts = arts
	if lease.Valid {
		v := lease.Time.UTC()
		tk.LeaseUntil = &v
	}
	if claimedAt.Valid {
		v := claimedAt.Time.UTC()
		tk.ClaimedAt = &v
	}
	return tk, nil
}

// scanTasks decodes every remaining row of a task result set.
func scanTasks(rows *sql.Rows) ([]model.Task, error) {
	out := []model.Task{}
	for rows.Next() {
		tk, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, tk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: scan tasks: %w", err)
	}
	return out, nil
}

// boolToInt converts a bool into the SQLite integer the schema stores.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// UpsertAgent inserts an agent or refreshes it, recording last_seen as now.
// The conflict target is the primary key, so a known agent_id updates in
// place instead of creating a duplicate row. An empty AgentID is rejected
// with model.ErrArgument.
func (s *Store) UpsertAgent(ctx context.Context, a model.Agent) error {
	if a.AgentID == "" {
		return fmt.Errorf("store: upsert agent: agent_id is required: %w", model.ErrArgument)
	}

	_, err := s.q.ExecContext(ctx, `INSERT INTO agents (agent_id, role, last_seen, current_task_id, health)
  VALUES (?, ?, ?, ?, ?)
  ON CONFLICT(agent_id) DO UPDATE SET
    role = excluded.role,
    last_seen = excluded.last_seen,
    current_task_id = excluded.current_task_id,
    health = excluded.health`,
		a.AgentID, a.Role, time.Now().UTC(), nullIfEmpty(a.CurrentTaskID), a.Health)
	if err != nil {
		return fmt.Errorf("store: upsert agent %s: %w", a.AgentID, err)
	}
	return nil
}

// GetAgent returns the agent with the given id, or an error wrapping
// model.ErrNotFound.
func (s *Store) GetAgent(ctx context.Context, id string) (model.Agent, error) {
	var (
		a      model.Agent
		last   sql.NullTime
		taskID sql.NullString
	)

	err := s.q.QueryRowContext(ctx, `SELECT agent_id, role, last_seen, current_task_id, health
  FROM agents WHERE agent_id = ?`, id).
		Scan(&a.AgentID, &a.Role, &last, &taskID, &a.Health)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.Agent{}, fmt.Errorf("store: agent %s: %w", id, model.ErrNotFound)
		}
		return model.Agent{}, fmt.Errorf("store: scan agent %s: %w", id, err)
	}

	if last.Valid {
		a.LastSeen = last.Time
	}
	if taskID.Valid {
		a.CurrentTaskID = taskID.String
	}
	return a, nil
}

// WriteAudit appends one audit entry. Empty actor/action/target wrap
// model.ErrArgument. CreatedAt is set to the current UTC time.
func (s *Store) WriteAudit(ctx context.Context, l model.AuditLog) error {
	if l.Actor == "" {
		return fmt.Errorf("store: write audit: actor is required: %w", model.ErrArgument)
	}
	if l.Action == "" {
		return fmt.Errorf("store: write audit: action is required: %w", model.ErrArgument)
	}
	if l.Target == "" {
		return fmt.Errorf("store: write audit: target is required: %w", model.ErrArgument)
	}

	res, err := s.q.ExecContext(ctx, `INSERT INTO audit_logs (actor, action, target, detail, created_at)
  VALUES (?, ?, ?, ?, ?)`,
		l.Actor, l.Action, l.Target, nullIfEmpty(l.Detail), time.Now().UTC())
	if err != nil {
		return fmt.Errorf("store: write audit %s/%s: %w", l.Actor, l.Action, err)
	}
	if _, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("store: write audit %s/%s: rows affected: %w", l.Actor, l.Action, err)
	}
	return nil
}

// ListAudit returns the newest `limit` audit entries, newest first. A
// non-positive limit falls back to 100.
func (s *Store) ListAudit(ctx context.Context, limit int) ([]model.AuditLog, error) {
	if limit <= 0 {
		limit = defaultAuditLimit
	}

	rows, err := s.q.QueryContext(ctx, `SELECT id, actor, action, target, detail, created_at
  FROM audit_logs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("store: list audit: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := []model.AuditLog{}
	for rows.Next() {
		var (
			l      model.AuditLog
			detail sql.NullString
		)
		if err := rows.Scan(&l.ID, &l.Actor, &l.Action, &l.Target, &detail, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("store: scan audit: %w", err)
		}
		if detail.Valid {
			l.Detail = detail.String
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: list audit: %w", err)
	}
	return out, nil
}

// nullIfEmpty maps an empty string to SQL NULL so nullable text columns
// stay absent instead of holding an empty placeholder.
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
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
