// Package dao provides Data Access Objects for database CRUD operations.
package dao

import (
	"database/sql"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3" // SQLite driver
)

// TestDB creates a temporary SQLite database with core schema migrations applied.
// It returns the *sql.DB and a cleanup function that closes and removes it.
// The migrationsDir parameter should point to the project's migrations directory.
func TestDB(migrationsDir string) (*sql.DB, func(), error) {
	dir, err := os.MkdirTemp("", "dao-test-*")
	if err != nil {
		return nil, nil, err
	}

	dbPath := filepath.Join(dir, "test.db")
	dsn := dbPath + "?_journal_mode=WAL"

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		os.RemoveAll(dir) //nolint:errcheck
		return nil, nil, err
	}

	files, err := os.ReadDir(migrationsDir)
	if err != nil {
		db.Close()  //nolint:errcheck
		os.RemoveAll(dir) //nolint:errcheck
		return nil, nil, err
	}

	for _, f := range files {
		if f.IsDir() || filepath.Ext(f.Name()) != ".sql" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(migrationsDir, f.Name()))
		if err != nil {
			db.Close()  //nolint:errcheck
			os.RemoveAll(dir) //nolint:errcheck
			return nil, nil, err
		}

		// Apply migration; skip FTS5/vec0 if modules unavailable.
		if _, err := db.Exec(string(data)); err != nil {
			// If the error is about missing module (fts5 or vec0), skip gracefully.
			if !isModuleError(err) {
				db.Close()  //nolint:errcheck
				os.RemoveAll(dir) //nolint:errcheck
				return nil, nil, err
			}
		}
	}

	cleanup := func() {
		db.Close()  //nolint:errcheck
		os.Remove(dbPath)     //nolint:errcheck
		os.Remove(dbPath + "-wal")  //nolint:errcheck
		os.Remove(dbPath + "-shm")  //nolint:errcheck
		os.RemoveAll(dir) //nolint:errcheck
	}

	return db, cleanup, nil
}

// isModuleError checks if the error indicates a missing SQLite module.
func isModuleError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return containsAny(msg, "no such module", "incompatible library")
}

// containsAny returns true if s contains any of the substrings.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if len(sub) > 0 && len(s) >= len(sub) {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
		}
	}
	return false
}
