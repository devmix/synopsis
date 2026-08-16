// Package database provides SQLite connection management, migration execution,
// and schema initialization for the Synopsis RAG service.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3" // SQLite driver with extensions
)

const (
	// DefaultMigrationsDir is the default relative path to migration SQL files.
	DefaultMigrationsDir = "migrations"

	// MigrationTableName stores applied migration versions.
	MigrationTableName = "_schema_migrations"
)

// Database wraps a SQLite connection and tracks configuration.
type Database struct {
	db            *sql.DB
	path          string
	vectorDim     int
	migrationsDir string
}

// OpenOption configures the database during Open().
type OpenOption func(*Database)

// WithMigrationsDir sets the directory containing SQL migration files.
func WithMigrationsDir(dir string) OpenOption {
	return func(d *Database) {
		d.migrationsDir = dir
	}
}

// Open creates a new SQLite database at the given path with recommended PRAGMAs.
// vectorDim specifies the embedding dimension for the vec0 virtual table.
// Optional WithMigrationsDir can override the default migrations directory.
func Open(path string, vectorDim int, opts ...OpenOption) (*Database, error) {
	if vectorDim <= 0 {
		return nil, fmt.Errorf("vectorDim must be positive, got %d", vectorDim)
	}

	// Ensure parent directories exist (SQLite does not create them).
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create database directory %s: %w", dir, err)
		}
	}

	// Auto-register sqlite-vec extension (compiled into binary via CGO)
	// Must be called BEFORE opening the database connection
	sqlite_vec.Auto()

	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000&_locally_loadable_extensions=1"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	// Verify connection is alive.
	if err := db.Ping(); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	d := &Database{db: db, path: path, vectorDim: vectorDim, migrationsDir: DefaultMigrationsDir}
	for _, opt := range opts {
		opt(d)
	}

	if err := d.applyPRAGMAs(); err != nil {
		db.Close() //nolint:errcheck
		return nil, fmt.Errorf("apply pragmas: %w", err)
	}

	// Auto-register sqlite-vec extension (compiled into binary via CGO)
	sqlite_vec.Auto()

	return d, nil
}

// applyPRAGMAs configures SQLite for optimal performance.
func (d *Database) applyPRAGMAs() error {
	pragmas := map[string]string{
		"journal_mode": "WAL",
		"synchronous":  "NORMAL",
		"cache_size":   "-64000",                         // 64 MB negative = KB in WAL mode
		"mmap_size":    strconv.FormatInt(268435456, 10), // 256 MB
		"foreign_keys": "ON",
	}

	for name, value := range pragmas {
		if _, err := d.db.Exec(fmt.Sprintf("PRAGMA %s = %s;", name, value)); err != nil {
			return fmt.Errorf("set pragma %s: %w", name, err)
		}
	}
	return nil
}

// Migrate applies all pending SQL migrations from the configured migrations directory.
func (d *Database) Migrate(ctx context.Context) error {
	migrationsDir := d.migrationsDir
	if _, err := os.Stat(migrationsDir); os.IsNotExist(err) {
		return fmt.Errorf("migrations directory %s does not exist", migrationsDir)
	}

	applied, err := d.getAppliedMigrations()
	if err != nil {
		return fmt.Errorf("get applied migrations: %w", err)
	}

	files, err := d.listMigrationFiles(migrationsDir)
	if err != nil {
		return fmt.Errorf("list migration files: %w", err)
	}

	for _, f := range files {
		version := extractVersion(f)
		if applied[version] {
			continue // already applied
		}

		sqlPath := filepath.Join(migrationsDir, f)
		data, err := os.ReadFile(sqlPath)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", f, err)
		}

		if err := d.applyMigration(ctx, version, string(data)); err != nil {
			return fmt.Errorf("apply migration %s: %w", f, err)
		}
	}

	// Initialize vec0 table with configured dimension after core migrations.
	if err := d.initVectorTable(); err != nil {
		return fmt.Errorf("init vector table: %w", err)
	}

	return nil
}

// Close releases the database connection.
func (d *Database) Close() error {
	if d.db == nil {
		return nil
	}
	return d.db.Close()
}

// DB returns the underlying sql.DB for direct access.
func (d *Database) DB() *sql.DB {
	return d.db
}

// VectorDim returns the configured embedding dimension.
func (d *Database) VectorDim() int {
	return d.vectorDim
}

// Path returns the filesystem path to the database file.
func (d *Database) Path() string {
	return d.path
}

// ---------- internal helpers ----------

// getAppliedMigrations returns a set of already-applied migration versions.
func (d *Database) getAppliedMigrations() (map[int]bool, error) {
	applied := make(map[int]bool)

	// Create version tracking table if it doesn't exist.
	const createTable = `
		CREATE TABLE IF NOT EXISTS _schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`
	if _, err := d.db.Exec(createTable); err != nil {
		return nil, fmt.Errorf("create migrations table: %w", err)
	}

	rows, err := d.db.Query("SELECT version FROM _schema_migrations ORDER BY version")
	if err != nil {
		return nil, fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan migration version: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migration rows: %w", err)
	}

	return applied, nil
}

// listMigrationFiles returns sorted .sql filenames from the migrations directory.
func (d *Database) listMigrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	return files, nil
}

// extractVersion parses the migration version from a filename like "001_init.sql".
func extractVersion(filename string) int {
	parts := strings.SplitN(filepath.Base(filename), "_", 2)
	if len(parts) == 0 {
		return 0
	}
	v, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0
	}
	return v
}

// applyMigration executes SQL within a transaction and records the version.
func (d *Database) applyMigration(ctx context.Context, version int, sqlText string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}

	// go-sqlite3 supports executing multiple statements in a single Exec call.
	if _, execErr := tx.Exec(sqlText); execErr != nil {
		// If the error is about a missing module (fts5, vec0), skip gracefully.
		if !isModuleError(execErr) {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("execute migration v%d SQL: %w", version, execErr)
		}
		// Module not available — record version in the same transaction so we don't retry.
		const insertVersion = "INSERT OR IGNORE INTO _schema_migrations (version) VALUES (?);"
		if _, verr := tx.Exec(insertVersion, version); verr != nil {
			tx.Rollback() //nolint:errcheck
			return fmt.Errorf("record migration version %d: %w", version, verr)
		}
		if cerr := tx.Commit(); cerr != nil {
			return fmt.Errorf("commit migration v%d (module skip): %w", version, cerr)
		}
		return nil
	}

	// Record migration version.
	const insertVersion = "INSERT OR IGNORE INTO _schema_migrations (version) VALUES (?);"
	if _, err := tx.Exec(insertVersion, version); err != nil {
		tx.Rollback() //nolint:errcheck
		return fmt.Errorf("record migration version %d: %w", version, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration v%d: %w", version, err)
	}

	return nil
}

// isModuleError checks if the error indicates a missing SQLite module.
func isModuleError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no such module") || strings.Contains(msg, "incompatible library")
}
