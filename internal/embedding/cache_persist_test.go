package embedding_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/cache"
	"github.com/devmix/synopsis/internal/embedding"
)

// testEmbeddingStore creates a temporary file-based SQLite database and returns a Store.
func testEmbeddingStore(t *testing.T) (*cache.Store, func()) {
	t.Helper()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_embed_cache.db")

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

func TestEmbeddingCache_PersistentRoundTrip(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist_embed.db")

	const model = "bge-small"
	const dim = 384
	text := "hello world"
	vec := []float32{0.1, 0.2, 0.3}

	// First store instance: write data.
	s1, err := cache.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore (first): %v", err)
	}

	c1 := embedding.NewEmbeddingCacheWithStore(s1)
	c1.Set(model, dim, text, vec)

	if err := s1.Close(); err != nil {
		t.Fatalf("Close (first): %v", err)
	}

	// Second store instance: read data.
	s2, err := cache.NewStore(dbPath)
	if err != nil {
		t.Fatalf("NewStore (second): %v", err)
	}
	defer s2.Close() //nolint:errcheck

	c2 := embedding.NewEmbeddingCacheWithStore(s2)

	got, ok := c2.Get(model, dim, text)
	if !ok {
		t.Fatal("expected cache hit after simulated restart")
	}
	if len(got) != len(vec) {
		t.Fatalf("len = %d, want %d", len(got), len(vec))
	}
	for i := range got {
		if got[i] != vec[i] {
			t.Errorf("[%d] = %f, want %f", i, got[i], vec[i])
		}
	}

	// Verify it's also in memory cache after DB hit.
	got2, ok := c2.Get(model, dim, text)
	if !ok {
		t.Fatal("expected cache hit from memory after first DB load")
	}
	for i := range got2 {
		if got2[i] != vec[i] {
			t.Errorf("[%d] = %f, want %f", i, got2[i], vec[i])
		}
	}
}

func TestEmbeddingCache_ModelDimMiss(t *testing.T) {
	t.Parallel()

	store, cleanup := testEmbeddingStore(t)
	defer cleanup()

	c := embedding.NewEmbeddingCacheWithStore(store)

	const model = "bge-small"
	const dim = 384
	text := "hello world"
	vec1 := []float32{0.1, 0.2, 0.3}
	vec2 := []float32{0.7, 0.8, 0.9}

	// Store with model A, dim 384.
	c.Set(model, dim, text, vec1)

	// Different model name should miss.
	got, ok := c.Get("other-model", dim, text)
	if ok {
		t.Error("expected cache miss for different model name")
	}
	if got != nil {
		t.Errorf("expected nil vector for miss, got %v", got)
	}

	// Different dimension should miss.
	got, ok = c.Get(model, 1024, text)
	if ok {
		t.Error("expected cache miss for different dimension")
	}
	if got != nil {
		t.Errorf("expected nil vector for miss, got %v", got)
	}

	// Original key should still work.
	got, ok = c.Get(model, dim, text)
	if !ok {
		t.Fatal("expected cache hit for original key")
	}
	for i := range got {
		if got[i] != vec1[i] {
			t.Errorf("[%d] = %f, want %f", i, got[i], vec1[i])
		}
	}

	// Store with different model and verify isolation.
	c.Set("other-model", dim, text, vec2)
	gotA, _ := c.Get(model, dim, text)
	gotB, _ := c.Get("other-model", dim, text)
	for i := range gotA {
		if gotA[i] != vec1[i] {
			t.Errorf("model A [%d] = %f, want %f (leaked from model B)", i, gotA[i], vec1[i])
		}
	}
	for i := range gotB {
		if gotB[i] != vec2[i] {
			t.Errorf("model B [%d] = %f, want %f", i, gotB[i], vec2[i])
		}
	}
}

func TestEmbeddingCache_NilStore(t *testing.T) {
	t.Parallel()

	c := embedding.NewEmbeddingCache()

	const model = "test"
	const dim = 768
	text := "hello"
	vec := []float32{0.1, 0.2}

	// Should not panic with nil store.
	c.Set(model, dim, text, vec)
	got, ok := c.Get(model, dim, text)
	if !ok {
		t.Fatal("expected cache hit")
	}
	for i := range got {
		if got[i] != vec[i] {
			t.Errorf("[%d] = %f, want %f", i, got[i], vec[i])
		}
	}

	// Miss should work.
	_, ok = c.Get(model, dim, "missing")
	if ok {
		t.Error("expected cache miss for missing key")
	}
}

func TestEmbeddingCache_DBWriteThrough(t *testing.T) {
	t.Parallel()

	store, cleanup := testEmbeddingStore(t)
	defer cleanup()

	c := embedding.NewEmbeddingCacheWithStore(store)

	const model = "test"
	const dim = 128
	text := "write-through-test"
	vec := []float32{0.5, 0.6, 0.7}

	// Set should write to both memory and DB.
	c.Set(model, dim, text, vec)

	// Verify in memory.
	got, ok := c.Get(model, dim, text)
	if !ok {
		t.Fatal("expected cache hit from memory")
	}
	for i := range got {
		if got[i] != vec[i] {
			t.Errorf("[%d] = %f, want %f", i, got[i], vec[i])
		}
	}

	// Verify in DB directly.
	ctx := context.Background()
	dbVal, hit := store.Get(ctx, "embeddings_cache", embedding.CacheKey(model, dim, text))
	if !hit {
		t.Fatal("expected DB cache hit after write-through")
	}
	expectedJSON := `[0.5,0.6,0.7]`
	if dbVal != expectedJSON {
		t.Errorf("DB value = %q, want %q", dbVal, expectedJSON)
	}
}
