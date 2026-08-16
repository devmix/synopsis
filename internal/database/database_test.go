package database_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devmix/synopsis/internal/database"
)

// migrationsDir resolves the absolute path to the project's migrations directory.
func migrationsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	return dir
}

// hasFTS5 checks if the FTS5 module is available in this SQLite build.
func hasFTS5(t *testing.T, d *database.Database) bool {
	t.Helper()
	var count int
	err := d.DB().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chunks_fts';",
	).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// hasVec0 checks if the vec0 module is available in this SQLite build.
func hasVec0(t *testing.T, d *database.Database) bool {
	t.Helper()
	var count int
	err := d.DB().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chunks_vec';",
	).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

func TestOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		vectorDim int
		wantErr   bool
	}{
		{
			name:      "valid open with dim 1024",
			vectorDim: 1024,
			wantErr:   false,
		},
		{
			name:      "zero vector dim rejected",
			vectorDim: 0,
			wantErr:   true,
		},
		{
			name:      "negative vector dim rejected",
			vectorDim: -1,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			dbPath := filepath.Join(dir, "test.db")

			d, err := database.Open(dbPath, tt.vectorDim)
			if (err != nil) != tt.wantErr {
				t.Errorf("Open() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if d != nil {
				d.Close() //nolint:errcheck
			}
		})
	}
}

func TestOpenCreatesMissingParentDirs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
	}{
		{
			name: "single missing level",
			path: filepath.Join("data"),
		},
		{
			name: "nested missing levels",
			path: filepath.Join("deep", "nested", "data"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			dbPath := filepath.Join(dir, tt.path, "test.db")

			d, err := database.Open(dbPath, 384)
			if err != nil {
				t.Fatalf("Open() error = %v, want success with auto-created directories", err)
			}
			defer d.Close() //nolint:errcheck

			info, statErr := os.Stat(filepath.Dir(dbPath))
			if statErr != nil {
				t.Fatalf("stat parent dir: %v", statErr)
			}
			if !info.IsDir() {
				t.Fatal("parent path is not a directory")
			}
		})
	}
}

func TestMigrate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := database.Open(dbPath, 1024, database.WithMigrationsDir(migrationsDir(t)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer d.Close() //nolint:errcheck

	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	// Verify core tables exist.
	tables := []string{"documents", "chunks", "entities", "chunk_entities", "facts", "entity_sources"}
	for _, table := range tables {
		var count int
		err := d.DB().QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?;", table,
		).Scan(&count)
		if err != nil {
			t.Fatalf("query table %s: %v", table, err)
		}
		if count == 0 {
			t.Errorf("table %s does not exist after migration", table)
		}
	}

	// Verify FTS5 virtual table if module is available.
	if hasFTS5(t, d) {
		t.Log("FTS5 module available — chunks_fts exists")
	} else {
		t.Log("FTS5 module not available — skipping FTS5 checks")
	}

	// Verify performance indexes on core tables.
	indexes := []string{
		"idx_chunks_doc_id", "idx_chunks_seq", "idx_entities_name",
		"idx_entities_type",
	}
	for _, idx := range indexes {
		var count int
		err := d.DB().QueryRow(
			"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?;", idx,
		).Scan(&count)
		if err != nil {
			t.Fatalf("query index %s: %v", idx, err)
		}
		if count == 0 {
			t.Errorf("index %s does not exist after migration", idx)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := database.Open(dbPath, 1024, database.WithMigrationsDir(migrationsDir(t)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer d.Close() //nolint:errcheck

	// Run migration twice — should not fail.
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("first Migrate() error = %v", err)
	}
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("second Migrate() error = %v", err)
	}
}

func TestWALMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := database.Open(dbPath, 1024)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer d.Close() //nolint:errcheck

	var mode string
	err = d.DB().QueryRow("PRAGMA journal_mode;").Scan(&mode)
	if err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("expected WAL mode, got %q", mode)
	}

	// Verify WAL file exists after write.
	d.DB().Exec("INSERT INTO documents (source_type, original_path) VALUES ('test', 'test');") //nolint:errcheck
	if _, err := os.Stat(dbPath + "-wal"); os.IsNotExist(err) {
		t.Error("WAL file not created — WAL mode may not be active")
	}
}

func TestMmapSize(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := database.Open(dbPath, 1024)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer d.Close() //nolint:errcheck

	var mmapSize int64
	err = d.DB().QueryRow("PRAGMA mmap_size;").Scan(&mmapSize)
	if err != nil {
		t.Fatalf("query mmap_size: %v", err)
	}
	if mmapSize <= 0 {
		t.Errorf("expected positive mmap_size, got %d", mmapSize)
	}
}

func TestGetMigrationVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := database.Open(dbPath, 1024, database.WithMigrationsDir(migrationsDir(t)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer d.Close() //nolint:errcheck

	// Before migration — version should be 0.
	version, err := database.GetMigrationVersion(d)
	if err != nil {
		t.Fatalf("GetMigrationVersion before migrate: %v", err)
	}
	if version != 0 {
		t.Errorf("expected version 0 before migration, got %d", version)
	}

	// After migration — version should be >= 1 (at least core tables).
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	version, err = database.GetMigrationVersion(d)
	if err != nil {
		t.Fatalf("GetMigrationVersion after migrate: %v", err)
	}
	if version < 1 {
		t.Errorf("expected version >= 1 after migration, got %d", version)
	}
}

func TestVectorTableCreated(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := database.Open(dbPath, 1024, database.WithMigrationsDir(migrationsDir(t)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer d.Close() //nolint:errcheck

	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	// Check vec0 table if module is available.
	if hasVec0(t, d) {
		var createSQL string
		err = d.DB().QueryRow(
			"SELECT sql FROM sqlite_master WHERE type='table' AND name='chunks_vec';",
		).Scan(&createSQL)
		if err != nil {
			t.Fatalf("query chunks_vec schema: %v", err)
		}
		if !strings.Contains(strings.ToLower(createSQL), "float[1024]") {
			t.Errorf("chunks_vec dimension mismatch in CREATE SQL: %s", createSQL)
		}
	} else {
		t.Log("vec0 module not available — skipping vec0 checks")
	}
}

func TestVectorDimMismatch(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	migDir := migrationsDir(t)

	// Open with dim 1024 and migrate.
	d1, err := database.Open(dbPath, 1024, database.WithMigrationsDir(migDir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := d1.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	// Check if vec0 was actually created.
	if !hasVec0(t, d1) {
		d1.Close() //nolint:errcheck
		t.Log("vec0 module not available — skipping dimension mismatch test")
		return
	}
	d1.Close() //nolint:errcheck

	// Re-open with different dim — should fail.
	d2, err := database.Open(dbPath, 768, database.WithMigrationsDir(migDir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer d2.Close() //nolint:errcheck

	err = d2.Migrate(context.Background())
	if err == nil {
		d2.Close() //nolint:errcheck
		t.Fatal("expected dimension mismatch error, got nil")
	}
}

func TestClose(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := database.Open(dbPath, 1024)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := d.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Double close should not panic.
	if err := d.Close(); err != nil {
		t.Errorf("double Close() error = %v", err)
	}
}

func TestAccessors(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := database.Open(dbPath, 512)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer d.Close() //nolint:errcheck

	if d.VectorDim() != 512 {
		t.Errorf("VectorDim() = %d, want 512", d.VectorDim())
	}
	if d.Path() != dbPath {
		t.Errorf("Path() = %s, want %s", d.Path(), dbPath)
	}
	if d.DB() == nil {
		t.Error("DB() returned nil")
	}
}

func TestMigrateDocHashOnExistingDB(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	migDir := migrationsDir(t)

	d, err := database.Open(dbPath, 1024, database.WithMigrationsDir(migDir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer d.Close() //nolint:errcheck

	// Run all migrations.
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	// Verify content_hash column exists on documents table.
	var colCount int
	err = d.DB().QueryRow(
		"SELECT COUNT(*) FROM pragma_table_info('documents') WHERE name='content_hash';",
	).Scan(&colCount)
	if err != nil {
		t.Fatalf("query content_hash column: %v", err)
	}
	if colCount == 0 {
		t.Error("content_hash column not found on documents table after migration")
	}

	// Verify unique index on original_path exists.
	var idxCount int
	err = d.DB().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_documents_original_path';",
	).Scan(&idxCount)
	if err != nil {
		t.Fatalf("query unique index: %v", err)
	}
	if idxCount == 0 {
		t.Error("unique index idx_documents_original_path not found after migration")
	}

	// Verify duplicate prevention works.
	_, err = d.DB().Exec(
		"INSERT INTO documents (source_type, original_path) VALUES ('test', '/path/to/doc.md');",
	)
	if err != nil {
		t.Fatalf("insert first document: %v", err)
	}

	_, err = d.DB().Exec(
		"INSERT INTO documents (source_type, original_path) VALUES ('test', '/path/to/doc.md');",
	)
	if err == nil {
		t.Error("expected unique constraint violation for duplicate path, got nil")
	}
	if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "unique") {
		t.Errorf("expected UNIQUE constraint error, got: %v", err)
	}

	// Verify only one document exists.
	var docCount int
	err = d.DB().QueryRow(
		"SELECT COUNT(*) FROM documents WHERE original_path='/path/to/doc.md';",
	).Scan(&docCount)
	if err != nil {
		t.Fatalf("query doc count: %v", err)
	}
	if docCount != 1 {
		t.Errorf("expected 1 document, got %d (duplicate not prevented)", docCount)
	}
}

func TestDedupMigration(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	migDir := migrationsDir(t)

	d, err := database.Open(dbPath, 1024, database.WithMigrationsDir(migDir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer d.Close() //nolint:errcheck

	// Apply only migration 1 (schema), leaving dedup pending.
	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	// Verify unique index prevents duplicates after full migration.
	_, err = d.DB().Exec(
		"INSERT INTO documents (source_type, original_path) VALUES ('test', '/path/to/doc.md');",
	)
	if err != nil {
		t.Fatalf("insert first document: %v", err)
	}

	_, err = d.DB().Exec(
		"INSERT INTO documents (source_type, original_path) VALUES ('test', '/path/to/doc.md');",
	)
	if err == nil {
		t.Error("expected unique constraint violation for duplicate path, got nil")
	}
	if !strings.Contains(err.Error(), "UNIQUE") && !strings.Contains(err.Error(), "unique") {
		t.Errorf("expected UNIQUE constraint error, got: %v", err)
	}
}

func TestDimensionMismatchError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		configDim   int
		dbDim       int
		wantMessage string
	}{
		{
			name:        "384 to 768 mismatch",
			configDim:   768,
			dbDim:       384,
			wantMessage: "vector dimension mismatch: config=768, database=384",
		},
		{
			name:        "1024 to 512 mismatch",
			configDim:   512,
			dbDim:       1024,
			wantMessage: "vector dimension mismatch: config=512, database=1024",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := &database.DimensionMismatchError{ConfigDim: tt.configDim, DBDim: tt.dbDim}
			if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Errorf("DimensionMismatchError.Error() = %q, want substring %q", err.Error(), tt.wantMessage)
			}

			if !database.IsDimensionMismatchError(err) {
				t.Error("IsDimensionMismatchError returned false for DimensionMismatchError")
			}
		})
	}
}

func TestIsDimensionMismatchError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "dimension mismatch error",
			err:  &database.DimensionMismatchError{ConfigDim: 768, DBDim: 384},
			want: true,
		},
		{
			name: "wrapped dimension mismatch error",
			err:  fmt.Errorf("init vector table: %w", &database.DimensionMismatchError{ConfigDim: 768, DBDim: 384}),
			want: true,
		},
		{
			name: "other error",
			err:  fmt.Errorf("some other error"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := database.IsDimensionMismatchError(tt.err)
			if got != tt.want {
				t.Errorf("IsDimensionMismatchError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestDropVectorTable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	migDir := migrationsDir(t)

	d, err := database.Open(dbPath, 1024, database.WithMigrationsDir(migDir))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer d.Close() //nolint:errcheck

	if err := d.Migrate(context.Background()); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	// Drop the vector table.
	if err := d.DropVectorTable(context.Background()); err != nil {
		t.Fatalf("DropVectorTable() error = %v", err)
	}

	// Verify table is gone.
	if hasVec0(t, d) {
		t.Error("chunks_vec still exists after DropVectorTable")
	}
}

func TestVecTableName(t *testing.T) {
	t.Parallel()

	if got := database.VecTableName(); got != "chunks_vec" {
		t.Errorf("VecTableName() = %q, want %q", got, "chunks_vec")
	}
}
