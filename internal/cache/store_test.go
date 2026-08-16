package cache_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/devmix/synopsis/internal/cache"
)

// testStore creates a temporary file-based SQLite database and returns a Store.
func testStore(t *testing.T) (*cache.Store, func()) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_cache.db")

	store, err := cache.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	cleanup := func() {
		_ = store.Close()
		os.Remove(dbPath)          //nolint:errcheck
		os.Remove(dbPath + "-wal") //nolint:errcheck
		os.Remove(dbPath + "-shm") //nolint:errcheck
	}

	return store, cleanup
}

func TestStore_NewStoreCreatesMissingParentDirs(t *testing.T) {
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
			path: filepath.Join("deep", "nested", "cache"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			dbPath := filepath.Join(dir, tt.path, "test_cache.db")

			store, err := cache.NewStore(dbPath)
			if err != nil {
				t.Fatalf("NewStore() error = %v, want success with auto-created directories", err)
			}
			defer store.Close() //nolint:errcheck

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

func TestStore_GetSetRoundtrip(t *testing.T) {
	t.Parallel()

	store, cleanup := testStore(t)
	defer cleanup()

	tests := []struct {
		name  string
		table string
		key   string
		value string
	}{
		{
			name:  "llm_ner_cache table",
			table: "llm_ner_cache",
			key:   "key-1",
			value: `{"entities":[{"name":"API Gateway","type":"SERVICE"}]}`,
		},
		{
			name:  "embedding_cache table",
			table: "embedding_cache",
			key:   "doc-42",
			value: `[0.1,0.2,0.3]`,
		},
		{
			name:  "simple key-value",
			table: "kv",
			key:   "foo",
			value: "bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			if err := store.Set(ctx, tt.table, tt.key, tt.value); err != nil {
				t.Fatalf("SetNerResponse(%s,%s): %v", tt.table, tt.key, err)
			}

			got, hit := store.Get(ctx, tt.table, tt.key)
			if !hit {
				t.Fatal("expected cache hit after SetNerResponse")
			}
			if got != tt.value {
				t.Errorf("GetNerResponse() = %q, want %q", got, tt.value)
			}
		})
	}
}

func TestStore_AutoCreateTable(t *testing.T) {
	t.Parallel()

	store, cleanup := testStore(t)
	defer cleanup()

	ctx := context.Background()
	table := "auto_created_table"

	// First SetNerResponse should auto-create the table.
	if err := store.Set(ctx, table, "k", "v"); err != nil {
		t.Fatalf("SetNerResponse on new table: %v", err)
	}

	// GetNerResponse should work without explicit migration.
	got, hit := store.Get(ctx, table, "k")
	if !hit {
		t.Fatal("expected cache hit after auto-created SetNerResponse")
	}
	if got != "v" {
		t.Errorf("GetNerResponse() = %q, want %q", got, "v")
	}
}

func TestStore_Delete(t *testing.T) {
	t.Parallel()

	store, cleanup := testStore(t)
	defer cleanup()

	ctx := context.Background()
	table := "del_test"
	key := "to-delete"

	if err := store.Set(ctx, table, key, "value"); err != nil {
		t.Fatalf("SetNerResponse: %v", err)
	}

	if _, hit := store.Get(ctx, table, key); !hit {
		t.Fatal("expected cache hit before delete")
	}

	if err := store.Delete(ctx, table, key); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, hit := store.Get(ctx, table, key); hit {
		t.Error("expected cache miss after Delete")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	store, cleanup := testStore(t)
	defer cleanup()

	ctx := context.Background()
	table := "concurrent"
	const goroutines = 10
	const keysPerGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(gID int) {
			defer wg.Done()
			for i := 0; i < keysPerGoroutine; i++ {
				key := fmt.Sprintf("g%d-k%d", gID, i)
				value := fmt.Sprintf("val-%d-%d", gID, i)

				if err := store.Set(ctx, table, key, value); err != nil {
					t.Errorf("SetNerResponse(g=%d,i=%d): %v", gID, i, err)
					continue
				}

				got, hit := store.Get(ctx, table, key)
				if !hit {
					t.Errorf("GetNerResponse(g=%d,i=%d): expected hit", gID, i)
				} else if got != value {
					t.Errorf("GetNerResponse(g=%d,i=%d) = %q, want %q", gID, i, got, value)
				}
			}
		}(g)
	}

	wg.Wait()
}

func TestStore_NonExistentKey(t *testing.T) {
	t.Parallel()

	store, cleanup := testStore(t)
	defer cleanup()

	ctx := context.Background()

	got, hit := store.Get(ctx, "some_table", "nonexistent")
	if hit {
		t.Error("expected cache miss for nonexistent key")
	}
	if got != "" {
		t.Errorf("GetNerResponse() returned %q, want empty string", got)
	}
}

func TestStore_ContextCancellation(t *testing.T) {
	t.Parallel()

	store, cleanup := testStore(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	table := "ctx_cancel"

	// GetNerResponse on cancelled context should return miss.
	_, hit := store.Get(ctx, table, "any-key")
	if hit {
		t.Error("expected cache miss for cancelled context in GetNerResponse")
	}

	// SetNerResponse on cancelled context should error.
	err := store.Set(ctx, table, "key", "value")
	if err == nil {
		t.Error("expected error for cancelled context in SetNerResponse")
	}

	// Delete on cancelled context should error.
	err = store.Delete(ctx, table, "key")
	if err == nil {
		t.Error("expected error for cancelled context in Delete")
	}
}

func TestStore_Persistence(t *testing.T) {
	// Verify that data persists across Store instances (simulating process restart).
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist_cache.db")

	ctx := context.Background()

	// First store instance: write data.
	s1, err := cache.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore (first): %v", err)
	}

	table := "persist_test"
	key := "persist-key"
	value := `{"name":"Persisted","type":"CONCEPT"}`

	if err := s1.Set(ctx, table, key, value); err != nil {
		t.Fatalf("SetNerResponse: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close (first): %v", err)
	}

	// Second store instance: read data.
	s2, err := cache.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore (second): %v", err)
	}
	defer s2.Close() //nolint:errcheck

	got, hit := s2.Get(ctx, table, key)
	if !hit {
		t.Fatal("expected cache hit after simulated restart")
	}
	if got != value {
		t.Errorf("GetNerResponse() = %q, want %q", got, value)
	}
}

func TestStore_CloseNil(t *testing.T) {
	t.Parallel()

	var s *cache.Store
	if err := s.Close(); err != nil {
		t.Error("expected no error closing nil store")
	}
}
