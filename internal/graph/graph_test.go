package graph_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/graph"
)

func TestNewGraphFromDB(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	desc1 := "Senior engineer"
	idAlice, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc1})
	desc2 := "Engineering department"
	idEngineering, _ := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering", Description: &desc2})

	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "works_in", ObjectEntityID: idEngineering}) //nolint:errcheck

	g, stats, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}
	if g == nil {
		t.Fatal("expected non-nil graph")
	}
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.NodeCount != 2 {
		t.Errorf("expected 2 nodes, got %d", stats.NodeCount)
	}
	if stats.EdgeCount != 1 {
		t.Errorf("expected 1 edge, got %d", stats.EdgeCount)
	}
}

func TestGetEntityIDByName(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)

	desc := "An engineer"
	idEmployee, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	tests := []struct {
		name   string
		query  string
		wantID int
		wantOK bool
	}{
		{
			name:   "exact match",
			query:  "Alice",
			wantID: idEmployee,
			wantOK: true,
		},
		{
			name:   "case insensitive match",
			query:  "alice",
			wantID: idEmployee,
			wantOK: true,
		},
		{
			name:   "nonexistent entity",
			query:  "Nobody",
			wantID: 0,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotOK := g.GetEntityIDByName(tt.query, "")
			if gotOK != tt.wantOK {
				t.Errorf("GetEntityIDByName(%q) ok = %v, want %v", tt.query, gotOK, tt.wantOK)
			}
			if gotOK && gotID != tt.wantID {
				t.Errorf("GetEntityIDByName(%q) id = %d, want %d", tt.query, gotID, tt.wantID)
			}
		})
	}
}

func TestGetNode(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)

	desc := "An engineer"
	idEmployee, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	tests := []struct {
		name   string
		id     int
		wantOK bool
	}{
		{
			name:   "existing node",
			id:     idEmployee,
			wantOK: true,
		},
		{
			name:   "nonexistent node",
			id:     99999,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, gotOK := g.GetNode(tt.id)
			if gotOK != tt.wantOK {
				t.Errorf("GetNode(%d) ok = %v, want %v", tt.id, gotOK, tt.wantOK)
			}
			if gotOK && node.Name != "Alice" {
				t.Errorf("GetNode(%d).Name = %q, want %q", tt.id, node.Name, "Alice")
			}
		})
	}
}

func TestBFS(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	// Build a small graph:
	// Alice --works_in--> Engineering
	// Bob --works_in--> Engineering
	// Alice --owns--> NDA
	// Policy --requires--> Bob
	// Alice --reports_to--> Policy

	descAlice := "Senior engineer"
	idAlice, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &descAlice})
	descEngineering := "Engineering department"
	idEngineering, _ := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering", Description: &descEngineering})
	descNDA := "Confidentiality agreement"
	idNDA, _ := entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "NDA", Description: &descNDA})
	descBob := "Team lead"
	idBob, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Bob", Description: &descBob})
	descPolicy := "Security policy"
	idPolicy, _ := entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "Security Policy", Description: &descPolicy})

	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "works_in", ObjectEntityID: idEngineering}) //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idBob, Predicate: "works_in", ObjectEntityID: idEngineering})   //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "owns", ObjectEntityID: idNDA})             //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idPolicy, Predicate: "requires", ObjectEntityID: idBob})        //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "reports_to", ObjectEntityID: idPolicy})    //nolint:errcheck

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	tests := []struct {
		name          string
		startID       int
		opts          graph.BFSOptions
		wantNodeCount int // minimum expected nodes (center + discovered)
		wantEdgeCount int // minimum expected edges
	}{
		{
			name:          "BFS from Alice depth 1 outgoing",
			startID:       idAlice,
			opts:          graph.BFSOptions{MaxDepth: 1, Direction: graph.DirectionOutgoing},
			wantNodeCount: 4, // Alice + Engineering + NDA + Policy (3 outgoing edges)
			wantEdgeCount: 3,
		},
		{
			name:          "BFS from Alice depth 2 both",
			startID:       idAlice,
			opts:          graph.BFSOptions{MaxDepth: 2, Direction: graph.DirectionBoth},
			wantNodeCount: 5, // all nodes reachable within 2 hops
			wantEdgeCount: 4, // at least 4 edges discovered
		},
		{
			name:    "BFS nonexistent start node",
			startID: 99999,
			opts:    graph.BFSOptions{},
		},
		{
			name:          "BFS with relation type filter",
			startID:       idAlice,
			opts:          graph.BFSOptions{MaxDepth: 2, Direction: graph.DirectionOutgoing, RelationTypes: []string{"works_in"}},
			wantNodeCount: 2, // Alice + Engineering only (owns and reports_to filtered out)
			wantEdgeCount: 1,
		},
		{
			name:          "BFS with max nodes limit",
			startID:       idAlice,
			opts:          graph.BFSOptions{MaxDepth: 5, Direction: graph.DirectionBoth, MaxNodes: 3},
			wantNodeCount: 3, // limited to 3 nodes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := g.BFS(context.Background(), tt.startID, tt.opts)

			if tt.name == "BFS nonexistent start node" {
				if err == nil {
					t.Error("expected error for nonexistent start node")
				}
				return
			}

			if err != nil {
				t.Fatalf("BFS() error = %v", err)
			}

			if result.CenterEntity == nil {
				t.Fatal("CenterEntity is nil")
			}

			if len(result.Nodes) < tt.wantNodeCount {
				t.Errorf("got %d nodes, want at least %d", len(result.Nodes), tt.wantNodeCount)
			}

			if len(result.Edges) < tt.wantEdgeCount {
				t.Errorf("got %d edges, want at least %d", len(result.Edges), tt.wantEdgeCount)
			}
		})
	}
}

func TestBFS_CycleProtection(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	// Create a cycle: A -> B -> C -> A
	descA, descB, descC := "Node A", "Node B", "Node C"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "CycleA", Description: &descA})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "CycleB", Description: &descB})
	idC, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "CycleC", Description: &descC})

	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "knows", ObjectEntityID: idB}) //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idB, Predicate: "knows", ObjectEntityID: idC}) //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idC, Predicate: "knows", ObjectEntityID: idA}) //nolint:errcheck

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	result, err := g.BFS(context.Background(), idA, graph.BFSOptions{MaxDepth: 10, Direction: graph.DirectionOutgoing})
	if err != nil {
		t.Fatalf("BFS() error = %v", err)
	}

	// Should visit exactly 3 nodes (no duplicates due to cycle protection).
	if len(result.Nodes) != 3 {
		t.Errorf("expected 3 nodes with cycle protection, got %d", len(result.Nodes))
	}

	// Verify no duplicate node IDs.
	seen := make(map[int]bool)
	for _, n := range result.Nodes {
		if seen[n.ID] {
			t.Errorf("cycle protection failed: node %d visited twice", n.ID)
		}
		seen[n.ID] = true
	}
}

func TestFindEntityExact(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)

	desc := "An engineer"
	idEmployee, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	tests := []struct {
		name   string
		query  string
		wantOK bool
	}{
		{
			name:   "exact match case sensitive input",
			query:  "Alice",
			wantOK: true,
		},
		{
			name:   "case insensitive lookup",
			query:  "alice",
			wantOK: true,
		},
		{
			name:   "partial match should not find",
			query:  "Ali",
			wantOK: false,
		},
		{
			name:   "empty query",
			query:  "",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, gotOK := g.FindEntityExact(tt.query, "")
			if gotOK != tt.wantOK {
				t.Errorf("FindEntityExact(%q) ok = %v, want %v", tt.query, gotOK, tt.wantOK)
			}
			if gotOK && node.ID != idEmployee {
				t.Errorf("FindEntityExact(%q).Predicate = %d, want %d", tt.query, node.ID, idEmployee)
			}
		})
	}
}

func TestFindEntityPartial(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)

	desc1 := "Alice"
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc1})
	desc2 := "Engineering"
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering", Description: &desc2})
	desc3 := "NDA"
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "NDA", Description: &desc3})
	desc4 := "Security Policy"
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "Security Policy", Description: &desc4})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	tests := []struct {
		name        string
		pattern     string
		wantMinLen  int
		wantMaxLen  int
		wantContain string // one of the results should contain this name
	}{
		{
			name:        "partial match 'Eng'",
			pattern:     "Eng",
			wantMinLen:  1,
			wantMaxLen:  1,
			wantContain: "Engineering",
		},
		{
			name:        "partial match 'Security' finds Security Policy",
			pattern:     "Security",
			wantMinLen:  1,
			wantMaxLen:  2, // might also match if there's a Security entity
			wantContain: "Security Policy",
		},
		{
			name:        "partial prefix 'Ali' finds Alice",
			pattern:     "Ali",
			wantMinLen:  1,
			wantMaxLen:  1,
			wantContain: "Alice",
		},
		{
			name:       "no match returns empty",
			pattern:    "NonexistentEntityXYZ",
			wantMinLen: 0,
			wantMaxLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := g.FindEntityPartial(tt.pattern, "")
			if err != nil {
				t.Fatalf("FindEntityPartial(%q) error = %v", tt.pattern, err)
			}

			if len(results) < tt.wantMinLen || len(results) > tt.wantMaxLen {
				t.Errorf("got %d results, want [%d, %d]", len(results), tt.wantMinLen, tt.wantMaxLen)
			}

			if tt.wantContain != "" {
				found := false
				for _, n := range results {
					if n.Name == tt.wantContain {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected result containing %q not found", tt.wantContain)
				}
			}
		})
	}

	// Test empty pattern error.
	t.Run("empty pattern returns error", func(t *testing.T) {
		_, err := g.FindEntityPartial("", "")
		if err == nil {
			t.Error("expected error for empty pattern")
		}
	})
}

func TestFindEntitiesByType(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)

	desc1 := "NDA"
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "NDA Policy", Description: &desc1})
	desc2 := "Security"
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "Security Policy", Description: &desc2})
	desc3 := "Alice"
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc3})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	tests := []struct {
		name       string
		entityType string
		wantCount  int
	}{
		{
			name:       "find items",
			entityType: "policy",
			wantCount:  2,
		},
		{
			name:       "find characters",
			entityType: "employee",
			wantCount:  1,
		},
		{
			name:       "nonexistent type returns empty",
			entityType: "vehicle",
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := g.FindEntitiesByType(tt.entityType, "")
			if err != nil {
				t.Fatalf("FindEntitiesByType(%q) error = %v", tt.entityType, err)
			}
			if len(results) != tt.wantCount {
				t.Errorf("got %d results for type %q, want %d", len(results), tt.entityType, tt.wantCount)
			}
		})
	}

	t.Run("empty type returns error", func(t *testing.T) {
		_, err := g.FindEntitiesByType("", "")
		if err == nil {
			t.Error("expected error for empty entity type")
		}
	})
}

func TestGraphStats(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	desc1, desc2 := "A", "B"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeA", Description: &desc1})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "NodeB", Description: &desc2})

	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "located_in", ObjectEntityID: idB})   //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idB, Predicate: "connected_to", ObjectEntityID: idA}) //nolint:errcheck

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	stats := g.Stats()
	if stats.NodeCount != 2 {
		t.Errorf("NodeCount = %d, want 2", stats.NodeCount)
	}
	if stats.EdgeCount != 2 {
		t.Errorf("EdgeCount = %d, want 2", stats.EdgeCount)
	}
	if stats.AvgDegree != 2.0 {
		t.Errorf("AvgDegree = %f, want 2.0", stats.AvgDegree)
	}
}

func TestToDOT(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	desc1, desc2 := "Alice", "Engineering"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc1})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering", Description: &desc2})

	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "located_in", ObjectEntityID: idB}) //nolint:errcheck

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	dot, err := g.ToDOT(context.Background(), idA, graph.BFSOptions{MaxDepth: 2})
	if err != nil {
		t.Fatalf("ToDOT() error = %v", err)
	}

	if dot == "" {
		t.Error("expected non-empty DOT output")
	}

	// Check that DOT contains expected elements.
	tests := []struct {
		name    string
		content string
	}{
		{"digraph header", "digraph KnowledgeBase"},
		{"edge definition", "->"},
		{"relation label", "located_in"},
	}

	for _, tt := range tests {
		if !contains(dot, tt.content) {
			t.Errorf("DOT output missing %q (%s)", tt.content, tt.name)
		}
	}
}

func TestBFSOptions_ApplyDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		input     graph.BFSOptions
		wantDepth int
		wantDir   graph.Direction
		wantMax   int
	}{
		{
			name:      "zero values get defaults",
			input:     graph.BFSOptions{},
			wantDepth: 5,
			wantDir:   graph.DirectionBoth,
			wantMax:   1000,
		},
		{
			name:      "max depth capped at 10",
			input:     graph.BFSOptions{MaxDepth: 20},
			wantDepth: 10,
			wantDir:   graph.DirectionBoth,
			wantMax:   1000,
		},
		{
			name:      "custom values preserved",
			input:     graph.BFSOptions{MaxDepth: 3, Direction: graph.DirectionOutgoing, MaxNodes: 500},
			wantDepth: 3,
			wantDir:   graph.DirectionOutgoing,
			wantMax:   500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.input
			opts.ApplyDefaults()

			if opts.MaxDepth != tt.wantDepth {
				t.Errorf("MaxDepth = %d, want %d", opts.MaxDepth, tt.wantDepth)
			}
			if opts.Direction != tt.wantDir {
				t.Errorf("Direction = %q, want %q", opts.Direction, tt.wantDir)
			}
			if opts.MaxNodes != tt.wantMax {
				t.Errorf("MaxNodes = %d, want %d", opts.MaxNodes, tt.wantMax)
			}
		})
	}
}

// TestBFS_DoesNotCrossDomains verifies that BFS traversal never crosses domain
// boundaries: entities in other domains are not reachable even when a
// cross-domain edge exists in the graph.
func TestBFS_DoesNotCrossDomains(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	// Alice and Bob belong to "hr"; Carol belongs to "policy".
	// A cross-domain edge Bob -> Carol exists in the graph (created directly,
	// bypassing ingestion-time validation).
	idAlice, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Domain: "hr"})
	idBob, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Bob", Domain: "hr"})
	idCarol, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Carol", Domain: "policy"})

	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "knows", ObjectEntityID: idBob, Domain: "hr"}) //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idBob, Predicate: "knows", ObjectEntityID: idCarol, Domain: "hr"}) //nolint:errcheck

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	result, err := g.BFS(context.Background(), idAlice, graph.BFSOptions{MaxDepth: 5, Direction: graph.DirectionBoth})
	if err != nil {
		t.Fatalf("BFS() error = %v", err)
	}

	// Only Alice and Bob (both "hr") are reachable; Carol ("policy") must not be.
	if len(result.Nodes) != 2 {
		t.Errorf("BFS crossed domain boundary: got %d nodes, want 2", len(result.Nodes))
	}
	for _, n := range result.Nodes {
		if n.ID == idCarol {
			t.Error("BFS crossed domain boundary: reached entity in another domain")
		}
		if n.Domain != "hr" {
			t.Errorf("BFS returned node %q in domain %q, want %q", n.Name, n.Domain, "hr")
		}
	}
}

// TestGetEntityIDsByName verifies that exact-name lookup across all domains
// returns all matches in deterministic (sorted) order.
func TestGetEntityIDsByName(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)

	// Same name in two domains plus a distinct name.
	idArch1, _ := entDAO.Create(ctx, dao.Entity{Type: "ROLE", Name: "Архитектор", Domain: "construction"})
	idArch2, _ := entDAO.Create(ctx, dao.Entity{Type: "ROLE", Name: "Архитектор", Domain: "it"})
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Иван", Domain: "it"})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	ids := g.GetEntityIDsByName("Архитектор")
	if len(ids) != 2 {
		t.Fatalf("GetEntityIDsByName() returned %d IDs, want 2", len(ids))
	}
	want := []int{idArch1, idArch2}
	if idArch1 > idArch2 {
		want = []int{idArch2, idArch1}
	}
	for i := range ids {
		if ids[i] != want[i] {
			t.Errorf("GetEntityIDsByName()[%d] = %d, want %d (deterministic order)", i, ids[i], want[i])
		}
	}

	// Domain-qualified exact lookup returns the right entity.
	id, ok := g.GetEntityIDByName("Архитектор", "it")
	if !ok || id != idArch2 {
		t.Errorf("GetEntityIDByName(domain=it) = %d, %v; want %d, true", id, ok, idArch2)
	}
	id, ok = g.GetEntityIDByName("Архитектор", "IT")
	if !ok || id != idArch2 {
		t.Errorf("GetEntityIDByName(domain=IT) = %d, %v; want %d, true (normalized domain)", id, ok, idArch2)
	}
}

func TestNewGraphFromDB_ExcludesDraftFacts(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	desc1 := "Senior engineer"
	idAlice, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc1})
	desc2 := "Engineering department"
	idEngineering, _ := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering", Description: &desc2})

	// Create an approved fact — should appear in graph.
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "works_in", ObjectEntityID: idEngineering}) //nolint:errcheck

	// Create a draft fact — must NOT appear in graph.
	draftFactID, err := relDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "owns", ObjectEntityID: idEngineering})
	if err != nil {
		t.Fatalf("create draft fact: %v", err)
	}
	if _, err := db.Exec("UPDATE facts SET status = 'draft' WHERE id = ?", draftFactID); err != nil {
		t.Fatalf("set draft status: %v", err)
	}

	g, stats, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	if stats.NodeCount != 2 {
		t.Errorf("expected 2 nodes, got %d", stats.NodeCount)
	}
	if stats.EdgeCount != 1 {
		t.Errorf("expected 1 edge (draft fact excluded), got %d", stats.EdgeCount)
	}

	// Verify the draft fact's predicate does not appear in any edge.
	for _, edges := range g.GetOutgoingEdges(idAlice) {
		if edges.RelationType == "owns" {
			t.Error("graph contains edge from draft fact (predicate 'owns')")
		}
	}
}

func TestBFS_Determinism(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	// Build a graph with 6 nodes and multiple edges to exercise both directions.
	// A --knows--> B, C
	// B --works_at--> D
	// C --works_at--> D
	// E --reports_to--> D
	// F --manages--> A

	descA := "Node A"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "EntityA", Description: &descA})
	descB := "Node B"
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "EntityB", Description: &descB})
	descC := "Node C"
	idC, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "EntityC", Description: &descC})
	descD := "Node D"
	idD, _ := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "EntityD", Description: &descD})
	descE := "Node E"
	idE, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "EntityE", Description: &descE})
	descF := "Node F"
	idF, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "EntityF", Description: &descF})

	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "knows", ObjectEntityID: idB})      //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "knows", ObjectEntityID: idC})      //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idB, Predicate: "works_at", ObjectEntityID: idD})   //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idC, Predicate: "works_at", ObjectEntityID: idD})   //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idE, Predicate: "reports_to", ObjectEntityID: idD}) //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idF, Predicate: "manages", ObjectEntityID: idA})    //nolint:errcheck

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	tests := []struct {
		name    string
		startID int
		opts    graph.BFSOptions
	}{
		{
			name:    "DirectionBoth from A depth 3",
			startID: idA,
			opts:    graph.BFSOptions{MaxDepth: 3, Direction: graph.DirectionBoth},
		},
		{
			name:    "DirectionOutgoing from D depth 2",
			startID: idD,
			opts:    graph.BFSOptions{MaxDepth: 2, Direction: graph.DirectionOutgoing},
		},
		{
			name:    "DirectionIncoming from D depth 3",
			startID: idD,
			opts:    graph.BFSOptions{MaxDepth: 3, Direction: graph.DirectionIncoming},
		},
	}

	const iterations = 100

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var firstNodes []int
			var firstEdges [][]int // [sourceID, targetID] per edge

			for i := 0; i < iterations; i++ {
				result, err := g.BFS(context.Background(), tt.startID, tt.opts)
				if err != nil {
					t.Fatalf("iteration %d: BFS() error = %v", i, err)
				}

				nodeIDs := make([]int, len(result.Nodes))
				for j, n := range result.Nodes {
					nodeIDs[j] = n.ID
				}

				edgePairs := make([][]int, len(result.Edges))
				for j, e := range result.Edges {
					edgePairs[j] = []int{e.SourceID, e.TargetID}
				}

				if i == 0 {
					firstNodes = nodeIDs
					firstEdges = edgePairs
					continue
				}

				if !sliceEq(firstNodes, nodeIDs) {
					t.Errorf("iteration %d: node order differs from first run.\nfirst: %v\ncurr : %v",
						i, firstNodes, nodeIDs)
					break
				}

				if !edgePairsEq(firstEdges, edgePairs) {
					t.Errorf("iteration %d: edge order differs from first run.\nfirst: %v\ncurr : %v",
						i, formatEdgePairs(firstEdges), formatEdgePairs(edgePairs))
					break
				}
			}
		})
	}
}

// TestLoadEntityLinks verifies that entity links are loaded from DB and accessible.
func TestLoadEntityLinks(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	// Create entities in different domains.
	idAlice, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Domain: "hr"})
	idBob, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Bob", Domain: "policy"})

	// Create a cross-domain entity link.
	evidence := "Same person in HR and policy systems"
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idAlice,
		TargetEntityID:  idBob,
		RelationType:    "same_as",
		Method:          "rule",
		Confidence:      0.95,
		Evidence:        &evidence,
	})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	tests := []struct {
		name      string
		nodeID    int
		wantCount int
	}{
		{
			name:      "source node has one entity link edge",
			nodeID:    idAlice,
			wantCount: 1,
		},
		{
			name:      "target node has no outgoing entity links",
			nodeID:    idBob,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edges := g.GetEntityLinkEdges(tt.nodeID)
			if len(edges) != tt.wantCount {
				t.Errorf("GetEntityLinkEdges(%d) returned %d edges, want %d", tt.nodeID, len(edges), tt.wantCount)
			}
			if tt.wantCount > 0 {
				if edges[0].SourceID != idAlice || edges[0].TargetID != idBob {
					t.Errorf("GetEntityLinkEdges(%d)[0] = {%d->%d}, want {%d->%d}",
						tt.nodeID, edges[0].SourceID, edges[0].TargetID, idAlice, idBob)
				}
			}
		})
	}
}

// TestIsEntityLinkEdge verifies entity link edge lookup in both directions.
func TestIsEntityLinkEdge(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "EntityA", Domain: "hr"})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "EntityB", Domain: "policy"})
	idC, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "EntityC", Domain: "it"})

	// Create link A -> B.
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA,
		TargetEntityID:  idB,
		RelationType:    "same_as",
		Method:          "rule",
		Confidence:      0.95,
	})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	tests := []struct {
		name     string
		sourceID int
		targetID int
		want     bool
	}{
		{
			name:     "same direction as stored link",
			sourceID: idA,
			targetID: idB,
			want:     true,
		},
		{
			name:     "reverse direction should be false",
			sourceID: idB,
			targetID: idA,
			want:     false,
		},
		{
			name:     "non-existent pair",
			sourceID: idA,
			targetID: idC,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := g.IsEntityLinkEdge(tt.sourceID, tt.targetID)
			if got != tt.want {
				t.Errorf("IsEntityLinkEdge(%d, %d) = %v, want %v", tt.sourceID, tt.targetID, got, tt.want)
			}
		})
	}
}

func sliceEq(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func edgePairsEq(a, b [][]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i][0] != b[i][0] || a[i][1] != b[i][1] {
			return false
		}
	}
	return true
}

func formatEdgePairs(pairs [][]int) string {
	parts := make([]string, len(pairs))
	for i, p := range pairs {
		parts[i] = fmt.Sprintf("%d-%d", p[0], p[1])
	}
	return fmt.Sprintf("[%s]", fmt.Sprint(parts))
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestBFS_MaxNodesSemantics verifies that MaxNodes limits only the node count,
// and edges are never more than nodes (unified semantics).
func TestBFS_MaxNodesSemantics(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	// Build a graph with 100 nodes connected in a chain plus cross-edges.
	const numNodes = 100
	entityIDs := make([]int, 0, numNodes)

	for i := 0; i < numNodes; i++ {
		name := fmt.Sprintf("Node_%d", i)
		desc := "Test node"
		id, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: name, Description: &desc})
		entityIDs = append(entityIDs, id)
	}

	// Chain edges: Node_i -> Node_{i+1}
	for i := 0; i < numNodes-1; i++ {
		relDAO.Create(ctx, dao.Fact{SubjectEntityID: entityIDs[i], Predicate: "next", ObjectEntityID: entityIDs[i+1]}) //nolint:errcheck
	}

	// Cross edges for wider branching at each level.
	for i := 0; i < numNodes-2; i++ {
		relDAO.Create(ctx, dao.Fact{SubjectEntityID: entityIDs[i], Predicate: "cross", ObjectEntityID: entityIDs[i+2]}) //nolint:errcheck
	}

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	tests := []struct {
		name      string
		maxNodes  int
		direction graph.Direction
		wantMaxN  int // maximum allowed nodes in result
	}{
		{
			name:      "MaxNodes_10_outgoing",
			maxNodes:  10,
			direction: graph.DirectionOutgoing,
			wantMaxN:  10,
		},
		{
			name:      "MaxNodes_10_both",
			maxNodes:  10,
			direction: graph.DirectionBoth,
			wantMaxN:  10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := g.BFS(context.Background(), entityIDs[0], graph.BFSOptions{
				MaxDepth:  0, // ApplyDefaults sets to 5
				Direction: tt.direction,
				MaxNodes:  tt.maxNodes,
			})
			if err != nil {
				t.Fatalf("BFS() error = %v", err)
			}

			if len(result.Nodes) > tt.wantMaxN {
				t.Errorf("got %d nodes, want at most %d (MaxNodes=%d)", len(result.Nodes), tt.wantMaxN, tt.maxNodes)
			}

			if len(result.Edges) > len(result.Nodes) {
				t.Errorf("edges (%d) exceed nodes (%d); invariant edges ≤ nodes violated", len(result.Edges), len(result.Nodes))
			}
		})
	}
}

// TestBFS_ContextCancellation verifies that a cancelled context stops BFS traversal.
func TestBFS_ContextCancellation(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	desc1, desc2 := "A", "B"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeA", Description: &desc1})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "NodeB", Description: &desc2})

	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "located_in", ObjectEntityID: idB}) //nolint:errcheck

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	// Pre-cancelled context.
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := g.BFS(cancelCtx, idA, graph.BFSOptions{MaxDepth: 5, Direction: graph.DirectionBoth})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}

	// Should return partial results (at least the center node) with an error.
	if result == nil {
		t.Fatal("expected non-nil partial result even on cancellation")
	}
	if result.CenterEntity == nil {
		t.Error("expected center entity in partial result")
	}
}

// TestGraphStats_AvgDegreeIncludesEntityLinks verifies that avgDegree accounts for
// cross-domain entity link edges (m-11).
func TestGraphStats_AvgDegreeIncludesEntityLinks(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeA", Domain: "hr"})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeB", Domain: "policy"})

	// Create a bidirectional entity link A↔B.
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idB, RelationType: "same_as", Method: "rule", Confidence: 1.0,
	})
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idB, TargetEntityID: idA, RelationType: "same_as", Method: "rule", Confidence: 1.0,
	})

	g, stats, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	// 2 nodes, 2 outgoing entity links + 2 incoming entity links = 4 total edge references.
	// avgDegree = 4 / 2 = 2.0
	if stats.AvgDegree != 2.0 {
		t.Errorf("AvgDegree = %f, want 2.0 (entity link edges must be counted)", stats.AvgDegree)
	}

	// Also verify via Stats() method.
	s := g.Stats()
	if s.AvgDegree != 2.0 {
		t.Errorf("Stats().AvgDegree = %f, want 2.0", s.AvgDegree)
	}
}
