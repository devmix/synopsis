package dao

import (
	"context"
	"database/sql"
	"fmt"
)

// TxManager owns the transaction lifecycle for database operations.
// DAOs never manage transactions: all multi-statement work must be wrapped
// in ExecTx, which guarantees rollback on error and commit on success.
type TxManager struct {
	db *sql.DB
}

// NewTxManager creates a TxManager over the given database handle.
// The handle must be a *sql.DB (connection pool), never a transaction.
func NewTxManager(db *sql.DB) *TxManager {
	return &TxManager{db: db}
}

// ExecTx runs fn inside a single transaction. The closure receives the
// transaction as DBTX so DAOs can be constructed over it directly; all
// statements execute on the same connection.
// If fn returns an error the transaction is rolled back and the error is
// returned (with the rollback error attached if rollback itself fails).
// On success the transaction is committed; a commit failure is returned.
func (tm *TxManager) ExecTx(ctx context.Context, fn func(ctx context.Context, tx DBTX) error) error {
	tx, err := tm.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	if err := fn(ctx, tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("transaction logic error: %v; rollback error: %w", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
