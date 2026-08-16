package embedding_test

import (
	"sync"
	"testing"

	"github.com/devmix/synopsis/internal/embedding"
)

func TestEmbeddingCache(t *testing.T) {
	t.Parallel()

	c := embedding.NewEmbeddingCache()

	vec1 := []float32{0.1, 0.2, 0.3}
	vec2 := []float32{0.4, 0.5, 0.6}

	const model = "test-model"
	const dim = 768

	// Set and get.
	c.Set(model, dim, "hello", vec1)
	got, ok := c.Get(model, dim, "hello")
	if !ok {
		t.Fatal("Get() returned false for cached key")
	}
	if len(got) != len(vec1) {
		t.Fatalf("len = %d, want %d", len(got), len(vec1))
	}
	for i := range got {
		if got[i] != vec1[i] {
			t.Errorf("[%d] = %f, want %f", i, got[i], vec1[i])
		}
	}

	// Miss.
	_, ok = c.Get(model, dim, "missing")
	if ok {
		t.Error("Get() returned true for missing key")
	}

	// Overwrite.
	c.Set(model, dim, "hello", vec2)
	got, _ = c.Get(model, dim, "hello")
	for i := range got {
		if got[i] != vec2[i] {
			t.Errorf("[%d] = %f, want %f (after overwrite)", i, got[i], vec2[i])
		}
	}

	// Mutation safety: modifying original should not affect cache.
	vec1[0] = 999
	got, _ = c.Get(model, dim, "hello")
	if got[0] == 999 {
		t.Error("cache was mutated by external slice modification")
	}

	// Different model name should be a miss.
	c.Set("other-model", dim, "hello", vec1)
	got, _ = c.Get(model, dim, "hello")
	for i := range got {
		if got[i] != vec2[i] {
			t.Errorf("[%d] = %f, want %f (different model should not affect original)", i, got[i], vec2[i])
		}
	}

	// Different dim should be a miss.
	c.Set(model, 1024, "hello", vec1)
	got, _ = c.Get(model, dim, "hello")
	for i := range got {
		if got[i] != vec2[i] {
			t.Errorf("[%d] = %f, want %f (different dim should not affect original)", i, got[i], vec2[i])
		}
	}
}

func TestEmbeddingCacheConcurrent(t *testing.T) {
	t.Parallel()

	c := embedding.NewEmbeddingCache()

	const model = "test-model"
	const dim = 768

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			key := string(rune('a' + i%26))
			c.Set(model, dim, key, []float32{float32(i)})
		}(i)
		go func(i int) {
			defer wg.Done()
			key := string(rune('a' + i%26))
			c.Get(model, dim, key) //nolint:errcheck
		}(i)
	}
	wg.Wait()
}
