// Package database provides SQLite connection management, migration execution,
// and schema initialization for the Synopsis RAG service.
package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const vecTableName = "chunks_vec"

// DimensionMismatchError is returned when the configured embedding dimension
// does not match the dimension stored in the database's chunks_vec table.
type DimensionMismatchError struct {
	ConfigDim int // dimension from configuration
	DBDim     int // dimension found in database
}

func (e *DimensionMismatchError) Error() string {
	return fmt.Sprintf(
		"vector dimension mismatch: config=%d, database=%d — reindex required; delete the database or drop table %q and restart",
		e.ConfigDim, e.DBDim, vecTableName,
	)
}

// IsDimensionMismatchError checks whether err is a DimensionMismatchError.
func IsDimensionMismatchError(err error) bool {
	var dme *DimensionMismatchError
	return errors.As(err, &dme)
}

// initVectorTable creates or validates the vec0 virtual table with the configured dimension.
func (d *Database) initVectorTable() error {
	exists, err := d.tableExists(vecTableName)
	if err != nil {
		return fmt.Errorf("check vector table existence: %w", err)
	}

	if !exists {
		if err := d.createVectorTable(d.vectorDim); err != nil {
			// If vec0 module is not available, skip gracefully.
			if !isModuleError(err) {
				return fmt.Errorf("create vector table: %w", err)
			}
			return nil // vec0 not available — will be created when extension loads
		}
		return nil
	}

	// Table exists — check dimension compatibility.
	compatible, actualDim, err := CheckVectorDimCompatibility(d, d.vectorDim)
	if err != nil {
		return fmt.Errorf("check vector dim compatibility: %w", err)
	}

	if !compatible {
		return &DimensionMismatchError{ConfigDim: d.vectorDim, DBDim: actualDim}
	}

	return nil
}

// createVectorTable creates the vec0 virtual table with the specified dimension.
func (d *Database) createVectorTable(dim int) error {
	createSQL := fmt.Sprintf(
		"CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(chunk_id INTEGER PRIMARY KEY, vector FLOAT[%d]);",
		vecTableName, dim,
	)
	if _, err := d.db.Exec(createSQL); err != nil {
		return fmt.Errorf("create vec0 table with dim %d: %w", dim, err)
	}
	return nil
}

// InitVectorTable is the exported wrapper for createVectorTable.
func InitVectorTable(db *Database, dim int) error {
	if db == nil {
		return fmt.Errorf("database instance is nil")
	}
	if dim <= 0 {
		return fmt.Errorf("dimension must be positive, got %d", dim)
	}

	exists, err := db.tableExists(vecTableName)
	if err != nil {
		return fmt.Errorf("check vector table existence: %w", err)
	}

	if exists {
		compatible, _, checkErr := CheckVectorDimCompatibility(db, dim)
		if checkErr != nil {
			return fmt.Errorf("check dimension compatibility: %w", checkErr)
		}
		if !compatible {
			// Drop and recreate with new dimension.
			if _, err := db.db.Exec("DROP TABLE IF EXISTS " + vecTableName); err != nil {
				return fmt.Errorf("drop existing vector table: %w", err)
			}
		} else {
			return nil // already correct
		}
	}

	return db.createVectorTable(dim)
}

// CheckVectorDimCompatibility checks whether the vec0 table's dimension matches expectedDim.
// Returns (compatible, actualDimension, error). If the table doesn't exist, actualDimension is 0.
func CheckVectorDimCompatibility(db *Database, expectedDim int) (bool, int, error) {
	exists, err := db.tableExists(vecTableName)
	if err != nil {
		return false, 0, fmt.Errorf("check table existence: %w", err)
	}

	if !exists {
		return true, 0, nil // no table yet — compatible (will be created)
	}

	// Parse dimension from CREATE TABLE statement.
	rows, err := db.db.Query(
		"SELECT sql FROM sqlite_master WHERE type='table' AND name=?;", vecTableName,
	)
	if err != nil {
		return false, 0, fmt.Errorf("query table schema: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var createSQL string
	if rows.Next() {
		if err := rows.Scan(&createSQL); err != nil {
			return false, 0, fmt.Errorf("scan table schema: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return false, 0, fmt.Errorf("iterate schema rows: %w", err)
	}

	actualDim := extractVectorDimension(createSQL)
	return actualDim == expectedDim, actualDim, nil
}

// GetMigrationVersion returns the highest applied migration version.
func GetMigrationVersion(db *Database) (int, error) {
	// Ensure tracking table exists before querying.
	const createTable = `CREATE TABLE IF NOT EXISTS _schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.db.Exec(createTable); err != nil {
		return 0, fmt.Errorf("create migrations table: %w", err)
	}

	rows, err := db.db.Query(
		"SELECT MAX(version) FROM _schema_migrations;",
	)
	if err != nil {
		return 0, fmt.Errorf("query max migration version: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var v sqlNullInt
	if rows.Next() {
		if err := rows.Scan(&v); err != nil {
			return 0, fmt.Errorf("scan migration version: %w", err)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate version rows: %w", err)
	}

	if !v.Valid {
		return 0, nil // no migrations applied yet
	}
	return v.Int, nil
}

// tableExists checks whether a table exists in the database.
func (d *Database) tableExists(name string) (bool, error) {
	var count int
	err := d.db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?;", name,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check table %q existence: %w", name, err)
	}
	return count > 0, nil
}

// extractVectorDimension parses the FLOAT[N] dimension from a vec0 CREATE TABLE statement.
func extractVectorDimension(sql string) int {
	// Look for pattern like "FLOAT[1024]" or "float[768]".
	idx := strings.Index(strings.ToLower(sql), "float[")
	if idx < 0 {
		return 0
	}
	end := strings.Index(sql[idx:], "]")
	if end < 0 {
		return 0
	}
	dimStr := sql[idx+6 : idx+end] // skip "FLOAT["
	var dim int
	if _, err := fmt.Sscanf(dimStr, "%d", &dim); err != nil {
		return 0
	}
	return dim
}

// sqlNullInt is a helper for nullable integer scanning.
type sqlNullInt struct {
	Int   int
	Valid bool
}

func (n *sqlNullInt) Scan(v interface{}) error {
	if v == nil {
		n.Int, n.Valid = 0, false
		return nil
	}
	n.Valid = true
	switch val := v.(type) {
	case int64:
		n.Int = int(val)
	case []byte:
		var parsed int
		if _, err := fmt.Sscanf(string(val), "%d", &parsed); err != nil {
			return fmt.Errorf("parse integer from bytes: %w", err)
		}
		n.Int = parsed
	default:
		return fmt.Errorf("unsupported type for sqlNullInt: %T", v)
	}
	return nil
}

// DropVectorTable drops the chunks_vec table if it exists.
func (d *Database) DropVectorTable(ctx context.Context) error {
	if _, err := d.db.ExecContext(ctx, "DROP TABLE IF EXISTS "+vecTableName); err != nil {
		return fmt.Errorf("drop vector table: %w", err)
	}
	return nil
}

// VecTableName returns the name of the vec0 virtual table.
func VecTableName() string { return vecTableName }
