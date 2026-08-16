// Package cache provides caching abstractions for the ingestion pipeline.
package cache

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// Store provides a generic key-value cache backed by SQLite.
// Tables are auto-created on first access, allowing multiple independent
// caches to share the same database file without schema migrations.
type Store struct {
	db   *sql.DB
	mu   sync.Mutex // protects table auto-creation
	seen map[string]struct{}
}

// NewStore opens a SQLite database at dbPath with WAL mode and returns a Store.
func NewStore(dbPath string) (*Store, error) {
	// Ensure parent directories exist (SQLite does not create them).
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create cache directory %s: %w", dir, err)
		}
	}

	dsn := dbPath + "?_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open cache database %s: %w", dbPath, err)
	}

	// Apply PRAGMAs for performance and concurrency.
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close() //nolint:errcheck
			return nil, fmt.Errorf("apply pragma %s: %w", pragma, err)
		}
	}

	s := &Store{
		db:   db,
		seen: make(map[string]struct{}),
	}

	if err := s.db.Ping(); err != nil {
		s.db.Close() //nolint:errcheck
		return nil, fmt.Errorf("ping cache database %s: %w", dbPath, err)
	}

	return s, nil
}

// Close closes the underlying SQLite database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// isValidSQLIdentifier checks that name contains only letters, digits and underscores,
// and does not start with a digit. This prevents SQL injection via table names.
func isValidSQLIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		letter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		digit := r >= '0' && r <= '9'
		if !letter && !digit && r != '_' {
			return false
		}
		if i == 0 && digit {
			return false
		}
	}
	return true
}

// ensureTable creates table if it does not exist yet.
func (s *Store) ensureTable(table string) error {
	if !isValidSQLIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[table]; ok {
		return nil
	}
	q := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (cache_key TEXT PRIMARY KEY, result TEXT NOT NULL)", table)
	if _, err := s.db.Exec(q); err != nil {
		return fmt.Errorf("create cache table %s: %w", table, err)
	}
	s.seen[table] = struct{}{}
	return nil
}

// Get retrieves a cached value for the given table and key.
// Returns the value and true on hit, or empty string and false on miss.
func (s *Store) Get(ctx context.Context, table, key string) (string, bool) {
	if !isValidSQLIdentifier(table) {
		return "", false
	}
	if ctx.Err() != nil {
		return "", false
	}
	if err := s.ensureTable(table); err != nil {
		return "", false
	}

	var result string
	q := fmt.Sprintf("SELECT result FROM %s WHERE cache_key = ?", table)
	err := s.db.QueryRowContext(ctx, q, key).Scan(&result)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		return "", false // treat any error as a miss
	}

	return result, true
}

// Set stores a value in the cache for the given table and key.
func (s *Store) Set(ctx context.Context, table, key, value string) error {
	if !isValidSQLIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("cache set: context cancelled: %w", ctx.Err())
	}
	if err := s.ensureTable(table); err != nil {
		return err
	}

	q := fmt.Sprintf("INSERT OR REPLACE INTO %s (cache_key, result) VALUES (?, ?)", table)
	_, err := s.db.ExecContext(ctx, q, key, value)
	if err != nil {
		return fmt.Errorf("cache set: insert into %s: %w", table, err)
	}

	return nil
}

// Delete removes a cached entry for the given table and key.
func (s *Store) Delete(ctx context.Context, table, key string) error {
	if !isValidSQLIdentifier(table) {
		return fmt.Errorf("invalid table name: %q", table)
	}
	if ctx.Err() != nil {
		return fmt.Errorf("cache delete: context cancelled: %w", ctx.Err())
	}
	if err := s.ensureTable(table); err != nil {
		return err
	}

	q := fmt.Sprintf("DELETE FROM %s WHERE cache_key = ?", table)
	_, err := s.db.ExecContext(ctx, q, key)
	if err != nil {
		return fmt.Errorf("cache delete: delete from %s: %w", table, err)
	}

	return nil
}
