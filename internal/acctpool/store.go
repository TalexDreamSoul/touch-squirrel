// Package acctpool is the unified local account store (SQLite).
//
// One row = one account/credential abstract, regardless of registrar plugin
// (xAI, Tavily, future…). Secrets stay on disk (local-pool files / tavily keys
// file); this table holds index + filter fields for the panel pool UI.
package acctpool

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Well-known account types (filter keys).
const (
	TypeXAI    = "xai"
	TypeTavily = "tavily"
)

// Status values (subset, stringly typed for plugins).
const (
	StatusActive    = "active"
	StatusDisabled  = "disabled"
	StatusExhausted = "exhausted"
	StatusUnknown   = "unknown"
)

// Account is the panel-facing unified row.
type Account struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	Plugin     string            `json:"plugin"`
	Label      string            `json:"label"`
	Status     string            `json:"status"`
	Email      string            `json:"email,omitempty"`
	ExternalID string            `json:"external_id,omitempty"`
	SecretRef  string            `json:"secret_ref,omitempty"`
	Meta       map[string]string `json:"meta,omitempty"`
	Source     string            `json:"source,omitempty"`
	RunID      string            `json:"run_id,omitempty"`
	CreatedAt  string            `json:"created_at"`
	UpdatedAt  string            `json:"updated_at"`
	LastUsedAt string            `json:"last_used_at,omitempty"`
}

// ListFilter for paginated queries.
type ListFilter struct {
	Type   string // empty = all
	Plugin string
	Status string
	Q      string // matches label/email/external_id
	Page   int
	Limit  int
}

// ListResult is a page of accounts + totals.
type ListResult struct {
	Items      []Account      `json:"items"`
	Total      int            `json:"total"`
	Page       int            `json:"page"`
	PageSize   int            `json:"page_size"`
	TotalPages int            `json:"total_pages"`
	ByType     map[string]int `json:"by_type"`
}

// Store is a process-local SQLite account index.
type Store struct {
	path string
	db   *sql.DB
	mu   sync.Mutex
}

// Open opens (or creates) the SQLite DB at path and runs schema bootstrap.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, fmt.Errorf("acctpool: empty path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// modernc driver; busy timeout via DSN
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite write safety
	s := &Store{path: path, db: db}
	if err := s.migrateSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Path returns the DB file path.
func (s *Store) Path() string { return s.path }

// Close closes the DB.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *Store) migrateSchema() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS schema_meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS accounts (
  id          TEXT PRIMARY KEY,
  type        TEXT NOT NULL,
  plugin      TEXT NOT NULL,
  label       TEXT NOT NULL DEFAULT '',
  status      TEXT NOT NULL DEFAULT 'active',
  email       TEXT NOT NULL DEFAULT '',
  external_id TEXT NOT NULL DEFAULT '',
  secret_ref  TEXT NOT NULL DEFAULT '',
  meta_json   TEXT NOT NULL DEFAULT '{}',
  source      TEXT NOT NULL DEFAULT 'local',
  run_id      TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_accounts_type ON accounts(type);
CREATE INDEX IF NOT EXISTS idx_accounts_plugin ON accounts(plugin);
CREATE INDEX IF NOT EXISTS idx_accounts_status ON accounts(status);
CREATE INDEX IF NOT EXISTS idx_accounts_external ON accounts(type, external_id);
`)
	return err
}

func (s *Store) metaGet(key string) (string, bool) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM schema_meta WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		return "", false
	}
	return v, true
}

func (s *Store) metaSet(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO schema_meta(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value,
	)
	return err
}

// Upsert inserts or updates by id. If ID empty, generates one.
// Dedupes on (type, external_id) when external_id is non-empty.
func (s *Store) Upsert(a Account) (Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC().Format(time.RFC3339)
	a.Type = strings.TrimSpace(a.Type)
	a.Plugin = strings.TrimSpace(a.Plugin)
	if a.Type == "" || a.Plugin == "" {
		return a, fmt.Errorf("type and plugin required")
	}
	if a.Status == "" {
		a.Status = StatusActive
	}
	if a.Source == "" {
		a.Source = "local"
	}
	if a.Meta == nil {
		a.Meta = map[string]string{}
	}
	metaRaw, _ := json.Marshal(a.Meta)

	// resolve existing by external id
	if a.ID == "" && a.ExternalID != "" {
		var existing string
		err := s.db.QueryRow(
			`SELECT id FROM accounts WHERE type = ? AND external_id = ? LIMIT 1`,
			a.Type, a.ExternalID,
		).Scan(&existing)
		if err == nil {
			a.ID = existing
		}
	}
	if a.ID == "" {
		a.ID = newID()
		a.CreatedAt = now
	} else if a.CreatedAt == "" {
		var created string
		if err := s.db.QueryRow(`SELECT created_at FROM accounts WHERE id = ?`, a.ID).Scan(&created); err == nil {
			a.CreatedAt = created
		} else {
			a.CreatedAt = now
		}
	}
	a.UpdatedAt = now

	_, err := s.db.Exec(`
INSERT INTO accounts(
  id, type, plugin, label, status, email, external_id, secret_ref, meta_json,
  source, run_id, created_at, updated_at, last_used_at
) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET
  type=excluded.type,
  plugin=excluded.plugin,
  label=excluded.label,
  status=excluded.status,
  email=excluded.email,
  external_id=excluded.external_id,
  secret_ref=excluded.secret_ref,
  meta_json=excluded.meta_json,
  source=excluded.source,
  run_id=excluded.run_id,
  updated_at=excluded.updated_at,
  last_used_at=CASE WHEN excluded.last_used_at != '' THEN excluded.last_used_at ELSE accounts.last_used_at END
`, a.ID, a.Type, a.Plugin, a.Label, a.Status, a.Email, a.ExternalID, a.SecretRef, string(metaRaw),
		a.Source, a.RunID, a.CreatedAt, a.UpdatedAt, a.LastUsedAt)
	if err != nil {
		return a, err
	}
	return a, nil
}

// List returns a filtered page.
func (s *Store) List(f ListFilter) (ListResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if f.Page <= 0 {
		f.Page = 1
	}
	if f.Limit <= 0 {
		f.Limit = 20
	}
	if f.Limit > 200 {
		f.Limit = 200
	}

	where := []string{"1=1"}
	args := []any{}
	if t := strings.TrimSpace(f.Type); t != "" && t != "all" {
		where = append(where, "type = ?")
		args = append(args, t)
	}
	if p := strings.TrimSpace(f.Plugin); p != "" {
		where = append(where, "plugin = ?")
		args = append(args, p)
	}
	if st := strings.TrimSpace(f.Status); st != "" {
		where = append(where, "status = ?")
		args = append(args, st)
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		where = append(where, "(label LIKE ? OR email LIKE ? OR external_id LIKE ? OR id LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like, like)
	}
	wsql := strings.Join(where, " AND ")

	var total int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM accounts WHERE `+wsql, args...).Scan(&total); err != nil {
		return ListResult{}, err
	}

	offset := (f.Page - 1) * f.Limit
	rows, err := s.db.Query(
		`SELECT id, type, plugin, label, status, email, external_id, secret_ref, meta_json,
		        source, run_id, created_at, updated_at, last_used_at
		 FROM accounts WHERE `+wsql+` ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		append(append([]any{}, args...), f.Limit, offset)...,
	)
	if err != nil {
		return ListResult{}, err
	}
	defer rows.Close()

	items := make([]Account, 0, f.Limit)
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return ListResult{}, err
		}
		items = append(items, a)
	}

	byType := map[string]int{}
	tr, err := s.db.Query(`SELECT type, COUNT(1) FROM accounts GROUP BY type`)
	if err == nil {
		defer tr.Close()
		for tr.Next() {
			var t string
			var n int
			if tr.Scan(&t, &n) == nil {
				byType[t] = n
			}
		}
	}

	pages := total / f.Limit
	if total%f.Limit != 0 {
		pages++
	}
	if pages == 0 {
		pages = 1
	}
	return ListResult{
		Items:      items,
		Total:      total,
		Page:       f.Page,
		PageSize:   f.Limit,
		TotalPages: pages,
		ByType:     byType,
	}, nil
}

// Types returns distinct type values.
func (s *Store) Types() ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.db.Query(`SELECT DISTINCT type FROM accounts ORDER BY type`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if rows.Scan(&t) == nil && t != "" {
			out = append(out, t)
		}
	}
	return out, nil
}

// Count returns total rows.
func (s *Store) Count() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(`SELECT COUNT(1) FROM accounts`).Scan(&n)
	return n, err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanAccount(row scannable) (Account, error) {
	var a Account
	var metaRaw string
	err := row.Scan(
		&a.ID, &a.Type, &a.Plugin, &a.Label, &a.Status, &a.Email, &a.ExternalID, &a.SecretRef, &metaRaw,
		&a.Source, &a.RunID, &a.CreatedAt, &a.UpdatedAt, &a.LastUsedAt,
	)
	if err != nil {
		return a, err
	}
	a.Meta = map[string]string{}
	if strings.TrimSpace(metaRaw) != "" {
		_ = json.Unmarshal([]byte(metaRaw), &a.Meta)
	}
	return a, nil
}

func newID() string {
	return fmt.Sprintf("acc_%d", time.Now().UnixNano())
}

// DefaultDBPath under squirrel home root.
func DefaultDBPath(homeRoot string) string {
	return filepath.Join(homeRoot, "accounts.db")
}
