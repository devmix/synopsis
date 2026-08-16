package dao

import (
	"context"
	"database/sql"
	"fmt"
)

// AppKV provides general-purpose key-value storage backed by SQLite.
type AppKV struct {
	db DBTX
}

// NewAppKV creates a new AppKV bound to the given database or transaction.
func NewAppKV(db DBTX) *AppKV {
	return &AppKV{db: db}
}

// Get retrieves a value by key. Returns (value, true) if found, ("", false) otherwise.
func (k *AppKV) Get(ctx context.Context, key string) (string, bool) {
	var value sql.NullString
	err := k.db.QueryRowContext(ctx, "SELECT value FROM app_kv WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", false
	}
	if err != nil {
		// Log error but don't fail — KV store is best-effort.
		return "", false
	}
	return value.String, true
}

// Set stores a key-value pair, updating the timestamp on each write.
func (k *AppKV) Set(ctx context.Context, key, value string) error {
	_, err := k.db.ExecContext(ctx,
		"INSERT INTO app_kv (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP) "+
			"ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP",
		key, value,
	)
	if err != nil {
		return fmt.Errorf("set kv %q: %w", key, err)
	}
	return nil
}
