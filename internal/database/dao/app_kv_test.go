package dao

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAppKV(t *testing.T) {
	t.Parallel()

	db, cleanup, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	kv := NewAppKV(db)

	t.Run("SetAndGet", func(t *testing.T) {
		if err := kv.Set(ctx, "test_key", "test_value"); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		val, ok := kv.Get(ctx, "test_key")
		if !ok {
			t.Fatal("Get() returned false for existing key")
		}
		if val != "test_value" {
			t.Errorf("Get() = %q, want %q", val, "test_value")
		}
	})

	t.Run("GetMissingKey", func(t *testing.T) {
		val, ok := kv.Get(ctx, "nonexistent_key")
		if ok {
			t.Error("Get() returned true for missing key")
		}
		if val != "" {
			t.Errorf("Get() = %q, want empty string", val)
		}
	})

	t.Run("UpdateExistingKey", func(t *testing.T) {
		if err := kv.Set(ctx, "update_key", "initial"); err != nil {
			t.Fatalf("Set() initial error = %v", err)
		}

		if err := kv.Set(ctx, "update_key", "updated"); err != nil {
			t.Fatalf("Set() update error = %v", err)
		}

		val, ok := kv.Get(ctx, "update_key")
		if !ok {
			t.Fatal("Get() returned false for existing key")
		}
		if val != "updated" {
			t.Errorf("Get() = %q, want %q", val, "updated")
		}
	})

	t.Run("EmptyValue", func(t *testing.T) {
		if err := kv.Set(ctx, "empty_val_key", ""); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		val, ok := kv.Get(ctx, "empty_val_key")
		if !ok {
			t.Fatal("Get() returned false for key with empty value")
		}
		if val != "" {
			t.Errorf("Get() = %q, want empty string", val)
		}
	})

	t.Run("TimestampValue", func(t *testing.T) {
		ts := "2024-01-15T12:00:00Z"
		if err := kv.Set(ctx, "last_linking_run", ts); err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		val, ok := kv.Get(ctx, "last_linking_run")
		if !ok {
			t.Fatal("Get() returned false for timestamp key")
		}
		if val != ts {
			t.Errorf("Get() = %q, want %q", val, ts)
		}
	})
}
