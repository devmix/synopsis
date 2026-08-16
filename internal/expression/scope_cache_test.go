package expression

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

func TestScopeCache_RegisterLoader(t *testing.T) {
	t.Parallel()

	sc := NewScopeCache()
	var called int32
	sc.RegisterLoader("test", func(ctx context.Context) (interface{}, error) {
		atomic.AddInt32(&called, 1)
		return "data", nil
	})

	if atomic.LoadInt32(&called) != 0 {
		t.Error("builder should not be called during registration")
	}
}

func TestScopeCache_Get_LazyLoad(t *testing.T) {
	t.Parallel()

	sc := NewScopeCache()
	var buildCount int32
	sc.RegisterLoader("facts", func(ctx context.Context) (interface{}, error) {
		atomic.AddInt32(&buildCount, 1)
		return map[string]int{"count": 42}, nil
	})

	ctx := context.Background()

	// First call should build
	entry1, err := sc.Get(ctx, "facts")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if atomic.LoadInt32(&buildCount) != 1 {
		t.Errorf("builder called %d times, want 1", buildCount)
	}
	if entry1.Data.(map[string]int)["count"] != 42 {
		t.Errorf("data mismatch: got %v", entry1.Data)
	}

	// Second call should use cache
	entry2, err := sc.Get(ctx, "facts")
	if err != nil {
		t.Fatalf("Get cached: %v", err)
	}
	if atomic.LoadInt32(&buildCount) != 1 {
		t.Errorf("builder called %d times on second Get, want 1 (cached)", buildCount)
	}
	if entry1 != entry2 {
		t.Error("second Get should return same entry")
	}
}

func TestScopeCache_Get_UnusedScopeNeverLoads(t *testing.T) {
	t.Parallel()

	sc := NewScopeCache()
	var buildCount int32
	sc.RegisterLoader("graph", func(ctx context.Context) (interface{}, error) {
		atomic.AddInt32(&buildCount, 1)
		return "graph-data", nil
	})

	ctx := context.Background()

	// Never call Get for "graph" — builder should never run
	if atomic.LoadInt32(&buildCount) != 0 {
		t.Error("unused scope builder should not be called")
	}
	_ = ctx // suppress unused warning
}

func TestScopeCache_Get_NoLoaderRegistered(t *testing.T) {
	t.Parallel()

	sc := NewScopeCache()
	ctx := context.Background()

	_, err := sc.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for unregistered scope")
	}
	if got := err.Error(); got[:len(`scope "nonexistent"`)] != `scope "nonexistent"` {
		t.Errorf("error should include scope name, got: %s", got)
	}
}

func TestScopeCache_Get_BuilderError(t *testing.T) {
	t.Parallel()

	sc := NewScopeCache()
	sc.RegisterLoader("failing", func(ctx context.Context) (interface{}, error) {
		return nil, errors.New("db connection lost")
	})

	ctx := context.Background()

	_, err := sc.Get(ctx, "failing")
	if err == nil {
		t.Fatal("expected error from failing builder")
	}
	if got := err.Error(); got[:len(`scope "failing"`)] != `scope "failing"` {
		t.Errorf("error should include scope name, got: %s", got)
	}
}

func TestScopeCache_Get_MultipleScopes(t *testing.T) {
	t.Parallel()

	sc := NewScopeCache()
	var factsBuilt, graphBuilt int32

	sc.RegisterLoader("facts", func(ctx context.Context) (interface{}, error) {
		atomic.AddInt32(&factsBuilt, 1)
		return "facts-data", nil
	})
	sc.RegisterLoader("graph", func(ctx context.Context) (interface{}, error) {
		atomic.AddInt32(&graphBuilt, 1)
		return "graph-data", nil
	})

	ctx := context.Background()

	// Only load facts — graph should not be built
	_, err := sc.Get(ctx, "facts")
	if err != nil {
		t.Fatalf("Get facts: %v", err)
	}
	if atomic.LoadInt32(&factsBuilt) != 1 {
		t.Error("facts builder should have been called once")
	}
	if atomic.LoadInt32(&graphBuilt) != 0 {
		t.Error("graph builder should not be called when only facts is accessed")
	}

	// Now load graph — both should be built exactly once
	_, err = sc.Get(ctx, "graph")
	if err != nil {
		t.Fatalf("Get graph: %v", err)
	}
	if atomic.LoadInt32(&factsBuilt) != 1 || atomic.LoadInt32(&graphBuilt) != 1 {
		t.Errorf("both builders should be called exactly once, facts=%d graph=%d", factsBuilt, graphBuilt)
	}
}
