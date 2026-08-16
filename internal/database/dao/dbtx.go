package dao

import (
	"context"
	"database/sql"
)

// DBTX is a minimal interface satisfied by both *sql.DB and *sql.Tx.
// DAOs can be constructed over either a connection or an active transaction
// so that all SQL statements live in the DAO layer.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
