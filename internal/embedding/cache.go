package embedding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/devmix/synopsis/internal/cache"
)

const defaultCacheMaxSize = 10_000
const embeddingsCacheTable = "embeddings_cache"

// EmbeddingCache stores previously computed embeddings to avoid redundant inference.
// With a cache store, it supports persistent caching: memory → DB → compute (write-through).
type EmbeddingCache struct {
	cache   map[string][]float32
	mu      sync.RWMutex
	maxSize int          // max entries; 0 means unlimited
	store   *cache.Store // nil = memory-only mode
}

// NewEmbeddingCache creates an empty cache instance with a default size limit.
func NewEmbeddingCache() *EmbeddingCache {
	return &EmbeddingCache{
		cache:   make(map[string][]float32),
		maxSize: defaultCacheMaxSize,
	}
}

// NewEmbeddingCacheWithStore creates a cache backed by both memory and persistent store.
func NewEmbeddingCacheWithStore(store *cache.Store) *EmbeddingCache {
	return &EmbeddingCache{
		cache:   make(map[string][]float32),
		maxSize: defaultCacheMaxSize,
		store:   store,
	}
}

// CacheKey computes a deterministic sha256 key from model name, dimension, and text.
func CacheKey(modelName string, dim int, text string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%s", modelName, dim, text)))
	return hex.EncodeToString(h[:])
}

// Get returns the cached embedding for text, or nil if not present.
// Lookup order: memory → DB (if store configured) → miss.
func (c *EmbeddingCache) Get(modelName string, dim int, text string) ([]float32, bool) {
	key := CacheKey(modelName, dim, text)

	c.mu.RLock()
	if v, ok := c.cache[key]; ok {
		c.mu.RUnlock()
		return v, true
	}
	c.mu.RUnlock()

	// Try DB store.
	if c.store != nil {
		if dbVal, hit := c.store.Get(context.Background(), embeddingsCacheTable, key); hit {
			var vec []float32
			if err := json.Unmarshal([]byte(dbVal), &vec); err == nil {
				c.mu.Lock()
				cp := make([]float32, len(vec))
				copy(cp, vec)
				c.cache[key] = cp
				c.mu.Unlock()
				return cp, true
			}
		}
	}

	return nil, false
}

// Set stores an embedding for text in the cache.
// Write-through: writes to both memory and DB (if store configured).
func (c *EmbeddingCache) Set(modelName string, dim int, text string, vec []float32) {
	key := CacheKey(modelName, dim, text)

	c.mu.Lock()
	if c.maxSize > 0 && len(c.cache) >= c.maxSize {
		c.cache = make(map[string][]float32)
	}
	cp := make([]float32, len(vec))
	copy(cp, vec)
	c.cache[key] = cp
	c.mu.Unlock()

	// Write-through to DB.
	if c.store != nil {
		jsonBytes, err := json.Marshal(vec)
		if err != nil {
			return // best-effort; memory cache already has the value
		}
		_ = c.store.Set(context.Background(), embeddingsCacheTable, key, string(jsonBytes))
	}
}
