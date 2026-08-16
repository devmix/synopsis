package dao

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func newTxManagerFixture(t *testing.T) (*TxManager, func()) {
	t.Helper()

	db, cleanup, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	return NewTxManager(db), cleanup
}

func TestExecTxCommitsOnSuccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tm, cleanup := newTxManagerFixture(t)
	defer cleanup()

	err := tm.ExecTx(ctx, func(ctx context.Context, tx DBTX) error {
		docDAO := NewDocumentDAO(tx)
		_, err := docDAO.Create(ctx, Document{SourceType: "markdown", OriginalPath: "/test/tx.md"})
		return err
	})
	if err != nil {
		t.Fatalf("ExecTx() error = %v", err)
	}

	// The document must be visible after commit.
	docDAO := NewDocumentDAO(tm.db)
	got, err := docDAO.GetByPath(ctx, "/test/tx.md")
	if err != nil {
		t.Fatalf("GetByPath() error = %v", err)
	}
	if got == nil {
		t.Error("document not found after successful ExecTx")
	}
}

func TestExecTxRollsBackOnError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tm, cleanup := newTxManagerFixture(t)
	defer cleanup()

	sentinel := errors.New("boom")
	err := tm.ExecTx(ctx, func(ctx context.Context, tx DBTX) error {
		docDAO := NewDocumentDAO(tx)
		if _, err := docDAO.Create(ctx, Document{SourceType: "markdown", OriginalPath: "/test/tx.md"}); err != nil {
			t.Fatalf("create in tx: %v", err)
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("ExecTx() error = %v, want sentinel %v", err, sentinel)
	}

	// Nothing must persist after rollback.
	docDAO := NewDocumentDAO(tm.db)
	got, err := docDAO.GetByPath(ctx, "/test/tx.md")
	if err != nil {
		t.Fatalf("GetByPath() error = %v", err)
	}
	if got != nil {
		t.Errorf("document persisted despite rollback: %+v", got)
	}
}

func TestExecTxPropagatesError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tm, cleanup := newTxManagerFixture(t)
	defer cleanup()

	want := errors.New("inner failure")
	err := tm.ExecTx(ctx, func(ctx context.Context, tx DBTX) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Errorf("ExecTx() error = %v, want %v", err, want)
	}
}
