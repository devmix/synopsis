package expression

import (
	"context"
	"fmt"
	"sync"
)

// ScopeCache provides lazy loading and caching of scope data.
// Data is built on first access by a registered builder and cached for the run duration.
type ScopeCache struct {
	loaders map[string]ScopeBuilder
	cache   map[string]*ScopeEntry
	mu      sync.RWMutex
}

// NewScopeCache creates an empty ScopeCache.
func NewScopeCache() *ScopeCache {
	return &ScopeCache{
		loaders: make(map[string]ScopeBuilder),
		cache:   make(map[string]*ScopeEntry),
	}
}

// RegisterLoader registers a builder function for the given scope name.
// The builder is called at most once per ScopeCache instance (lazy, cached).
func (sc *ScopeCache) RegisterLoader(name string, builder ScopeBuilder) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.loaders[name] = builder
}

// Get returns the scope entry for the given name.
// On first call, it invokes the registered builder and caches the result.
// Returns an error if no loader is registered or the builder fails.
func (sc *ScopeCache) Get(ctx context.Context, name string) (*ScopeEntry, error) {
	sc.mu.RLock()
	entry, cached := sc.cache[name]
	builder := sc.loaders[name]
	sc.mu.RUnlock()

	if cached && entry != nil && !entry.IsExpired() {
		return entry, nil
	}

	if builder == nil {
		return nil, fmt.Errorf("scope %q: no loader registered", name)
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Double-check after acquiring write lock
	if entry, ok := sc.cache[name]; ok && !entry.IsExpired() {
		return entry, nil
	}

	data, err := builder(ctx)
	if err != nil {
		return nil, fmt.Errorf("scope %q: build failed: %w", name, err)
	}

	entry = &ScopeEntry{Data: data}
	sc.cache[name] = entry
	return entry, nil
}
