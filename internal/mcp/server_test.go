package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/graph"
	"github.com/devmix/synopsis/internal/search"
	"github.com/devmix/synopsis/internal/logger"

	"github.com/mark3labs/mcp-go/mcp"
)

// mockSearcher implements search.Searcher for unit testing.
type mockSearcher struct{}

func (m *mockSearcher) HybridSearch(_ context.Context, _ string, _ int, _ string) ([]search.SearchResult, error) {
	return nil, nil
}

func (m *mockSearcher) LexicalSearch(_ context.Context, _ string, _ int, _ string) ([]search.SearchResult, error) {
	return nil, nil
}

func (m *mockSearcher) SemanticSearch(_ context.Context, _ string, _ int, _ string) ([]search.SearchResult, error) {
	return nil, nil
}

// migrationsDir resolves the absolute path to the project's migrations directory.
func migrationsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	return dir
}

// setupTestDB creates a temporary database with migrations applied.
func setupTestDB(t *testing.T) (*database.Database, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := database.Open(dbPath, 384, database.WithMigrationsDir(migrationsDir(t)))
	if err != nil {
		t.Fatalf("Open test db: %v", err)
	}

	cleanup := func() {
		d.Close() //nolint:errcheck
	}
	return d, cleanup
}

func TestNewServer_NilGraphAllowed(t *testing.T) {
	t.Parallel()

	db, cleanup := setupTestDB(t)
	defer cleanup()

	cfg := config.Config{
		Server: config.ServerConfig{
			Name:    "test-server",
			Version: "0.0.1",
			Host:    "127.0.0.1",
			Port:    0, // avoid port conflicts
		},
	}

	searcher := &mockSearcher{}

	// NewServer must accept nil graph without returning an error.
	srv, err := NewServer(cfg, db, searcher, nil)
	if err != nil {
		t.Fatalf("NewServer() returned unexpected error with nil graph: %v", err)
	}
	if srv == nil {
		t.Fatal("NewServer() returned nil server")
	}

	srv.Close() //nolint:errcheck
}

func TestNewServer_RequiredDependencies(t *testing.T) {
	t.Parallel()

	cfg := config.Config{
		Server: config.ServerConfig{
			Name:    "test-server",
			Version: "0.0.1",
		},
	}

	tests := []struct {
		name     string
		db       *database.Database
		searcher search.Searcher
		wantErr  bool
	}{
		{
			name:     "nil database returns error",
			db:       nil,
			searcher: &mockSearcher{},
			wantErr:  true,
		},
		{
			name:     "nil searcher returns error",
			db:       func() *database.Database { d, _ := setupTestDB(t); return d }(), //nolint:govet
			searcher: nil,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.db != nil {
				defer tt.db.Close() //nolint:errcheck
			}
			_, err := NewServer(cfg, tt.db, tt.searcher, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewServer() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestServer_SetGraphGetEntityRelations_Race verifies that concurrent SetGraph
// calls (as performed by the auto-update watcher) do not race with parallel
// get_entity_relations handler reads. Run with -race.
func TestServer_SetGraphGetEntityRelations_Race(t *testing.T) {
	t.Parallel()

	db, cleanup := setupTestDB(t)
	defer cleanup()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db.DB())
	relDAO := dao.NewFactDAO(db.DB())

	desc := "test entity"
	idAlice, err := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc})
	if err != nil {
		t.Fatalf("create employee entity: %v", err)
	}
	idEngineering, err := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering", Description: &desc})
	if err != nil {
		t.Fatalf("create department entity: %v", err)
	}
	if _, err := relDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "works_in", ObjectEntityID: idEngineering}); err != nil {
		t.Fatalf("create relation: %v", err)
	}

	g1, _, err := graph.NewGraphFromDB(ctx, db.DB())
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	// Second graph with an extra entity and edge.
	idNDA, err := entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "NDA", Description: &desc})
	if err != nil {
		t.Fatalf("create policy entity: %v", err)
	}
	if _, err := relDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "owns", ObjectEntityID: idNDA}); err != nil {
		t.Fatalf("create relation: %v", err)
	}

	g2, _, err := graph.NewGraphFromDB(ctx, db.DB())
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	cfg := config.Config{
		Server: config.ServerConfig{
			Name:    "test-server",
			Version: "0.0.1",
		},
	}

	srv, err := NewServer(cfg, db, &mockSearcher{}, g1)
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close() //nolint:errcheck

	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      ToolGetEntityRelations,
			Arguments: map[string]interface{}{"entity_id": fmt.Sprintf("%d", idAlice), "depth": 2},
		},
	}

	graphs := []*graph.Graph{g1, g2, nil}

	const (
		writers = 4
		readers = 4
		iters   = 200
	)

	var wg sync.WaitGroup

	// Writers: swap the graph pointer concurrently (watcher behaviour).
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				srv.SetGraph(graphs[j%len(graphs)])
			}
		}()
	}

	// Readers: invoke the get_entity_relations handler concurrently with swaps.
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				if _, err := srv.handleGetEntityRelations(ctx, req); err != nil {
					t.Errorf("handleGetEntityRelations() error = %v", err)
					return
				}
			}
		}()
	}

	wg.Wait()
}

// TestServer_MetricsRace verifies that concurrent middleware goroutines writing
// request metrics (as performed by HTTP request handlers) do not race with
// parallel GetMetrics/HealthCheck reads. Run with -race.
func TestServer_MetricsRace(t *testing.T) {
	t.Parallel()

	db, cleanup := setupTestDB(t)
	defer cleanup()

	log, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	cfg := config.Config{
		Server: config.ServerConfig{
			Name:    "test-server",
			Version: "0.0.1",
		},
	}

	srv, err := NewServer(cfg, db, &mockSearcher{}, nil, WithLogger(log))
	if err != nil {
		t.Fatalf("NewServer() error = %v", err)
	}
	defer srv.Close() //nolint:errcheck

	ctx := context.Background()
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      ToolGetEntityRelations,
			Arguments: map[string]interface{}{"entity_id": "1", "depth": 2},
		},
	}

	// Build the middleware chain around a trivial handler.
	handler := srv.loggingMiddleware()(func(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})

	const (
		writers = 4
		readers = 4
		iters   = 200
	)

	var wg sync.WaitGroup

	// Writers: invoke the middleware concurrently (simulates HTTP goroutines).
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				if _, err := handler(ctx, req); err != nil {
					t.Errorf("middleware handler error = %v", err)
					return
				}
			}
		}()
	}

	// Readers: read metrics and health concurrently with writes.
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				_ = srv.GetMetrics()
				_ = srv.HealthCheck()
			}
		}()
	}

	wg.Wait()
}
