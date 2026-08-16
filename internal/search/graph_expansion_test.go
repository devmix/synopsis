package search

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/graph"
)

func TestGraphExpander_New(t *testing.T) {
	t.Parallel()

	cfg := config.GraphConfig{
		EnableGraph: true,
		MaxDepth:    3,
		MaxNodes:    100,
	}

	db := sql.OpenDB(nil)
	g := &graph.Graph{}

	expander := NewGraphExpander(db, g, cfg)

	if expander == nil {
		t.Fatal("NewGraphExpander returned nil")
	}
	if expander.maxDepth != 3 {
		t.Errorf("maxDepth = %d, want 3", expander.maxDepth)
	}
	if expander.maxNodes != 100 {
		t.Errorf("maxNodes = %d, want 100", expander.maxNodes)
	}
	if !expander.includeFacts {
		t.Error("includeFacts should be true by default")
	}
}

func TestGraphExpander_Expand_NoGraph(t *testing.T) {
	t.Parallel()

	cfg := config.GraphConfig{
		EnableGraph: false,
	}

	db := sql.OpenDB(nil)
	expander := NewGraphExpander(db, nil, cfg)

	results := []SearchResult{
		{ChunkID: 1, ChunkText: "test", DocumentID: 1},
	}

	expanded, err := expander.Expand(context.Background(), results)
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}
	if len(expanded) != 1 {
		t.Errorf("got %d results, want 1", len(expanded))
	}
}

func TestGraphExpander_Expand_EmptyResults(t *testing.T) {
	t.Parallel()

	cfg := config.GraphConfig{
		EnableGraph: true,
		MaxDepth:    5,
		MaxNodes:    1000,
	}

	db := sql.OpenDB(nil)
	g := &graph.Graph{}

	expander := NewGraphExpander(db, g, cfg)

	expanded, err := expander.Expand(context.Background(), nil)
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}
	if expanded != nil {
		t.Errorf("Expand with nil results returned %v, want nil", expanded)
	}
}

func TestGraphExpander_serializeEdges(t *testing.T) {
	t.Parallel()

	expander := &GraphExpander{}
	edges := []*graph.Edge{
		{SourceID: 1, TargetID: 2, RelationType: "relates_to"},
		{SourceID: 2, TargetID: 3, RelationType: "part_of"},
	}

	serialized := expander.serializeEdges(edges)

	if len(serialized) != 2 {
		t.Fatalf("got %d serialized edges, want 2", len(serialized))
	}
	if serialized[0]["relation_type"] != "relates_to" {
		t.Errorf("first edge relation_type = %v, want relates_to", serialized[0]["relation_type"])
	}
}

func TestGraphExpander_serializeFacts(t *testing.T) {
	t.Parallel()

	expander := &GraphExpander{}
	facts := []dao.Fact{
		{ID: 1, SubjectEntityID: 11, Predicate: "is", ObjectEntityID: 22},
		{ID: 2, SubjectEntityID: 33, Predicate: "has", ObjectEntityID: 44},
	}

	serialized := expander.serializeFacts(facts)

	if len(serialized) != 2 {
		t.Fatalf("got %d serialized facts, want 2", len(serialized))
	}
	if serialized[0]["predicate"] != "is" {
		t.Errorf("first fact predicate = %v, want is", serialized[0]["predicate"])
	}
	if serialized[0]["subject_entity_id"] != 11 {
		t.Errorf("first fact subject_entity_id = %v, want 11", serialized[0]["subject_entity_id"])
	}
	if serialized[0]["object_entity_id"] != 22 {
		t.Errorf("first fact object_entity_id = %v, want 22", serialized[0]["object_entity_id"])
	}

	// Facts without metadata must not have a "metadata" key.
	if _, ok := serialized[0]["metadata"]; ok {
		t.Error("fact without metadata should not have 'metadata' key")
	}
}

func TestGraphExpander_serializeFacts_WithMetadata(t *testing.T) {
	t.Parallel()

	expander := &GraphExpander{}

	metaStr := `{"threshold_amount":100,"condition":"active"}`
	facts := []dao.Fact{
		{ID: 1, SubjectEntityID: 11, Predicate: "is", ObjectEntityID: 22, Metadata: &metaStr},
		{ID: 2, SubjectEntityID: 33, Predicate: "has", ObjectEntityID: 44}, // no metadata
	}

	serialized := expander.serializeFacts(facts)

	if len(serialized) != 2 {
		t.Fatalf("got %d serialized facts, want 2", len(serialized))
	}

	// First fact has metadata — key must be present.
	metaVal, ok := serialized[0]["metadata"]
	if !ok {
		t.Fatal("expected 'metadata' key in first serialized fact")
	}
	if metaVal != metaStr {
		t.Errorf("metadata = %v, want %q", metaVal, metaStr)
	}

	// Second fact has no metadata — key must be absent.
	if _, ok := serialized[1]["metadata"]; ok {
		t.Error("fact without metadata should not have 'metadata' key")
	}
}

// TestGraphExpansionDisabled verifies that when graph is disabled, no expansion is applied.
func TestGraphExpansionDisabled(t *testing.T) {
	t.Parallel()

	cfg := config.GraphConfig{
		EnableGraph: false,
	}

	db := sql.OpenDB(nil)
	expander := NewGraphExpander(db, nil, cfg)

	results := []SearchResult{
		{ChunkID: 1, ChunkText: "policy", DocumentID: 1},
	}

	expanded, err := expander.Expand(context.Background(), results)
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}
	if len(expanded) != 1 {
		t.Errorf("got %d results, want 1", len(expanded))
	}
	// Should not have any graph-related metadata
	if expanded[0].Metadata != nil {
		if _, ok := expanded[0].Metadata["related_entities"]; ok {
			t.Error("related_entities should not be present when graph is disabled")
		}
	}
}

// TestGraphExpander_SetGraphExpand_Race verifies that concurrent SetGraph calls
// (as performed by the auto-update watcher) do not race with parallel Expand
// reads. Run with -race.
func TestGraphExpander_SetGraphExpand_Race(t *testing.T) {
	t.Parallel()

	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	desc := "test entity"
	idEmployee, err := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc})
	if err != nil {
		t.Fatalf("create employee entity: %v", err)
	}
	idDept, err := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering", Description: &desc})
	if err != nil {
		t.Fatalf("create department entity: %v", err)
	}
	if _, err := relDAO.Create(ctx, dao.Fact{SubjectEntityID: idEmployee, Predicate: "works_in", ObjectEntityID: idDept}); err != nil {
		t.Fatalf("create relation: %v", err)
	}

	g1, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	// Second graph with an extra entity and edge.
	idNDA, err := entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "NDA", Description: &desc})
	if err != nil {
		t.Fatalf("create policy entity: %v", err)
	}
	if _, err := relDAO.Create(ctx, dao.Fact{SubjectEntityID: idEmployee, Predicate: "owns", ObjectEntityID: idNDA}); err != nil {
		t.Fatalf("create relation: %v", err)
	}

	g2, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	cfg := config.GraphConfig{
		EnableGraph: true,
		MaxDepth:    3,
		MaxNodes:    100,
	}
	expander := NewGraphExpander(db, g1, cfg)

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
				expander.SetGraph(graphs[j%len(graphs)])
			}
		}()
	}

	// Readers: expand concurrently with the swaps. Each goroutine uses its own
	// results slice because Expand mutates result metadata in place.
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iters; j++ {
				results := []SearchResult{
					{ChunkID: 1, ChunkText: "Alice", DocumentID: 1, Entities: []dao.Entity{{ID: idEmployee}}},
				}
				if _, err := expander.Expand(ctx, results); err != nil {
					t.Errorf("Expand() error = %v", err)
					return
				}
			}
		}()
	}

	wg.Wait()
}

// TestGraphExpander_ExcludesDraftFacts verifies that graph expansion does not
// include facts with draft or rejected status in related_entities metadata.
func TestGraphExpander_ExcludesDraftFacts(t *testing.T) {
	t.Parallel()

	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	desc := "test entity"
	idEmployee, err := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc})
	if err != nil {
		t.Fatalf("create employee entity: %v", err)
	}
	idDept, err := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering", Description: &desc})
	if err != nil {
		t.Fatalf("create department entity: %v", err)
	}

	// Create an approved fact.
	if _, err := factDAO.Create(ctx, dao.Fact{SubjectEntityID: idEmployee, Predicate: "works_in", ObjectEntityID: idDept}); err != nil {
		t.Fatalf("create approved fact: %v", err)
	}

	// Create a draft fact — must NOT appear in expansion.
	draftFactID, err := factDAO.Create(ctx, dao.Fact{SubjectEntityID: idEmployee, Predicate: "owns", ObjectEntityID: idDept})
	if err != nil {
		t.Fatalf("create draft fact: %v", err)
	}
	if _, err := db.Exec("UPDATE facts SET status = 'draft' WHERE id = ?", draftFactID); err != nil {
		t.Fatalf("set draft status: %v", err)
	}

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	cfg := config.GraphConfig{
		EnableGraph: true,
		MaxDepth:    3,
		MaxNodes:    100,
	}
	expander := NewGraphExpander(db, g, cfg)

	results := []SearchResult{
		{ChunkID: 1, ChunkText: "Alice", DocumentID: 1, Entities: []dao.Entity{{ID: idEmployee}}},
	}

	expanded, err := expander.Expand(ctx, results)
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}

	if len(expanded) != 1 {
		t.Fatalf("got %d expanded results, want 1", len(expanded))
	}

	// Check related_entities metadata.
	relatedEntities, ok := expanded[0].Metadata["related_entities"]
	if !ok {
		t.Fatal("expected related_entities in metadata")
	}
	entityList, ok := relatedEntities.([]map[string]interface{})
	if !ok {
		t.Fatalf("related_entities is not []map[string]interface{}, got %T", relatedEntities)
	}

	if len(entityList) != 1 {
		t.Fatalf("expected 1 entity in related_entities, got %d", len(entityList))
	}

	entityData := entityList[0]
	factList, ok := entityData["facts"].([]map[string]interface{})
	if !ok {
		t.Fatalf("entity facts is not []map[string]interface{}, got %T", entityData["facts"])
	}

	if len(factList) != 1 {
		t.Errorf("expected 1 fact (only approved), got %d", len(factList))
	}
	for _, f := range factList {
		if pred, ok := f["predicate"].(string); ok && pred == "owns" {
			t.Error("expansion includes draft fact with predicate 'owns'")
		}
	}
}

// TestGraphExpander_MaxNodesLimit verifies that expansion respects the MaxNodes limit.
// Creates a star graph (1 center, 200 neighbors) and checks that MaxNodes: 10 yields ≤ 10 edges.
func TestGraphExpander_MaxNodesLimit(t *testing.T) {
	t.Parallel()

	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	const (
		numNeighbors = 200
		maxNodes     = 10
	)

	// Create center entity.
	descCenter := "center"
	idCenter, err := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Center", Description: &descCenter})
	if err != nil {
		t.Fatalf("create center entity: %v", err)
	}

	// Create 200 neighbor entities and edges from center to each.
	entityIDs := make([]int, numNeighbors)
	for i := 0; i < numNeighbors; i++ {
		name := fmt.Sprintf("Neighbor_%d", i)
		desc := "neighbor"
		id, err := entDAO.Create(ctx, dao.Entity{Type: "department", Name: name, Description: &desc})
		if err != nil {
			t.Fatalf("create neighbor %d: %v", i, err)
		}
		entityIDs[i] = id

		if _, err := relDAO.Create(ctx, dao.Fact{SubjectEntityID: idCenter, Predicate: "connected_to", ObjectEntityID: id}); err != nil {
			t.Fatalf("create edge to neighbor %d: %v", i, err)
		}
	}

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	cfg := config.GraphConfig{
		EnableGraph: true,
		MaxDepth:    5,
		MaxNodes:    maxNodes,
	}
	expander := NewGraphExpander(db, g, cfg)

	results := []SearchResult{
		{ChunkID: 1, ChunkText: "center", DocumentID: 1, Entities: []dao.Entity{{ID: idCenter}}},
	}

	expanded, err := expander.Expand(ctx, results)
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}

	if len(expanded) != 1 {
		t.Fatalf("got %d expanded results, want 1", len(expanded))
	}

	relatedEntities, ok := expanded[0].Metadata["related_entities"]
	if !ok {
		t.Fatal("expected related_entities in metadata")
	}
	entityList, ok := relatedEntities.([]map[string]interface{})
	if !ok {
		t.Fatalf("related_entities is not []map[string]interface{}, got %T", relatedEntities)
	}

	if len(entityList) != 1 {
		t.Fatalf("expected 1 entity in related_entities, got %d", len(entityList))
	}

	entityData := entityList[0]
	edgeList, ok := entityData["edges"].([]map[string]interface{})
	if !ok {
		t.Fatalf("entity edges is not []map[string]interface{}, got %T", entityData["edges"])
	}

	if len(edgeList) > maxNodes {
		t.Errorf("MaxNodes limit violated: got %d edges, want at most %d", len(edgeList), maxNodes)
	}
}

// TestGraphExpander_ContextCancellation verifies that a pre-cancelled context
// does not cause a fatal error — partial results are returned.
func TestGraphExpander_ContextCancellation(t *testing.T) {
	t.Parallel()

	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	desc1, desc2 := "engineer", "engineering"
	idEmployee, err := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc1})
	if err != nil {
		t.Fatalf("create employee entity: %v", err)
	}
	idDept, err := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering", Description: &desc2})
	if err != nil {
		t.Fatalf("create department entity: %v", err)
	}
	if _, err := relDAO.Create(ctx, dao.Fact{SubjectEntityID: idEmployee, Predicate: "works_in", ObjectEntityID: idDept}); err != nil {
		t.Fatalf("create relation: %v", err)
	}

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	cfg := config.GraphConfig{
		EnableGraph: true,
		MaxDepth:    3,
		MaxNodes:    100,
	}
	expander := NewGraphExpander(db, g, cfg)

	results := []SearchResult{
		{ChunkID: 1, ChunkText: "Alice", DocumentID: 1, Entities: []dao.Entity{{ID: idEmployee}}},
	}

	// Use a pre-cancelled context.
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	expanded, err := expander.Expand(cancelCtx, results)
	if err != nil {
		t.Fatalf("Expand() returned fatal error with cancelled ctx: %v", err)
	}

	// Should return original results without panic/fatal error.
	if len(expanded) != 1 {
		t.Errorf("got %d expanded results, want 1 (original result preserved)", len(expanded))
	}
}

// TestGraphExpander_MultipleEntities verifies that a chunk with multiple entities
// gets expansion data for each entity in related_entities.
func TestGraphExpander_MultipleEntities(t *testing.T) {
	t.Parallel()

	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	desc := "test entity"
	idEmployee, err := entDAO.Create(ctx, dao.Entity{Type: "employee",
		Name: "Alice", Description: &desc})
	if err != nil {
		t.Fatalf("create employee entity: %v", err)
	}
	idDept, err := entDAO.Create(ctx, dao.Entity{Type: "department",
		Name: "Engineering", Description: &desc})
	if err != nil {
		t.Fatalf("create department entity: %v", err)
	}
	idNDA, err := entDAO.Create(ctx, dao.Entity{Type: "policy",
		Name: "NDA", Description: &desc})
	if err != nil {
		t.Fatalf("create policy entity: %v", err)
	}

	// Create facts connecting entities.
	if _, err := factDAO.Create(ctx, dao.Fact{SubjectEntityID: idEmployee, Predicate: "works_in", ObjectEntityID: idDept}); err != nil {
		t.Fatalf("create relation Alice->Engineering: %v", err)
	}
	if _, err := factDAO.Create(ctx, dao.Fact{SubjectEntityID: idEmployee, Predicate: "owns", ObjectEntityID: idNDA}); err != nil {
		t.Fatalf("create relation Alice->NDA: %v", err)
	}

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	cfg := config.GraphConfig{
		EnableGraph: true,
		MaxDepth:    3,
		MaxNodes:    100,
	}
	expander := NewGraphExpander(db, g, cfg)

	// Result with three entities.
	results := []SearchResult{
		{
			ChunkID:    1,
			ChunkText:  "Alice in Engineering with NDA",
			DocumentID: 1,
			Entities:   []dao.Entity{{ID: idEmployee}, {ID: idDept}, {ID: idNDA}},
		},
	}

	expanded, err := expander.Expand(ctx, results)
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}

	if len(expanded) != 1 {
		t.Fatalf("got %d expanded results, want 1", len(expanded))
	}

	relatedEntities, ok := expanded[0].Metadata["related_entities"]
	if !ok {
		t.Fatal("expected related_entities in metadata")
	}
	entityList, ok := relatedEntities.([]map[string]interface{})
	if !ok {
		t.Fatalf("related_entities is not []map[string]interface{}, got %T", relatedEntities)
	}

	// All three entities should be expanded.
	if len(entityList) != 3 {
		t.Errorf("expected 3 entities in related_entities, got %d", len(entityList))
	}

	// Build a map of entity_id -> entry for easier checking.
	entityMap := make(map[int]map[string]interface{})
	for _, e := range entityList {
		eid, ok := e["entity_id"].(int)
		if !ok {
			continue
		}
		entityMap[eid] = e
	}

	// Verify each entity is present.
	for id, name := range map[int]string{idEmployee: "Alice", idDept: "Engineering", idNDA: "NDA"} {
		entry, ok := entityMap[id]
		if !ok {
			t.Errorf("entity %d (%s) not found in related_entities", id, name)
			continue
		}
		if entry["name"] != name {
			t.Errorf("entity %d: name = %v, want %s", id, entry["name"], name)
		}
	}

	// Alice should have edges (connected to Engineering and NDA).
	employeeEntry := entityMap[idEmployee]
	if employeeEdges, ok := employeeEntry["edges"].([]map[string]interface{}); !ok || len(employeeEdges) == 0 {
		t.Error("expected Alice to have non-empty edges in related_entities")
	}
}

// TestGraphExpander_Deduplication verifies that when the same entity appears in
// multiple results, it is only expanded once (BFS runs once).
func TestGraphExpander_Deduplication(t *testing.T) {
	t.Parallel()

	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	desc := "test entity"
	idEmployee, err := entDAO.Create(ctx, dao.Entity{Type: "employee",
		Name: "Alice", Description: &desc})
	if err != nil {
		t.Fatalf("create employee entity: %v", err)
	}
	idDept, err := entDAO.Create(ctx, dao.Entity{Type: "department",
		Name: "Engineering", Description: &desc})
	if err != nil {
		t.Fatalf("create department entity: %v", err)
	}

	if _, err := factDAO.Create(ctx, dao.Fact{SubjectEntityID: idEmployee, Predicate: "works_in", ObjectEntityID: idDept}); err != nil {
		t.Fatalf("create relation: %v", err)
	}

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	cfg := config.GraphConfig{
		EnableGraph: true,
		MaxDepth:    3,
		MaxNodes:    100,
	}
	expander := NewGraphExpander(db, g, cfg)

	// Two results both referencing the same entity.
	results := []SearchResult{
		{ChunkID: 1, ChunkText: "Alice in Engineering", DocumentID: 1, Entities: []dao.Entity{{ID: idEmployee}}},
		{ChunkID: 2, ChunkText: "Alice in Engineering again", DocumentID: 1, Entities: []dao.Entity{{ID: idEmployee}}},
	}

	expanded, err := expander.Expand(ctx, results)
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}

	if len(expanded) != 2 {
		t.Fatalf("got %d expanded results, want 2", len(expanded))
	}

	// Both results should have related_entities with the Alice entity.
	for i, r := range expanded {
		relatedEntities, ok := r.Metadata["related_entities"]
		if !ok {
			t.Errorf("result %d: expected related_entities in metadata", i)
			continue
		}
		entityList, ok := relatedEntities.([]map[string]interface{})
		if !ok {
			t.Fatalf("result %d: related_entities is not []map[string]interface{}, got %T", i, relatedEntities)
		}
		if len(entityList) != 1 {
			t.Errorf("result %d: expected 1 entity in related_entities, got %d", i, len(entityList))
		}
	}
}

// TestGraphExpander_Expand_FactBatchErrorNonFatal verifies that when batch fact
// loading fails (DB error), Expand returns original results without panic. The
// error is logged as non-fatal; edges are not lost because the error propagates
// from expandEntities to Expand which handles it gracefully.
func TestGraphExpander_Expand_FactBatchErrorNonFatal(t *testing.T) {
	t.Parallel()

	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	desc1, desc2 := "employee", "department"
	idEmployee, err := entDAO.Create(ctx, dao.Entity{Type: "employee",
		Name: "Alice", Description: &desc1})
	if err != nil {
		t.Fatalf("create employee entity: %v", err)
	}
	idDept, err := entDAO.Create(ctx, dao.Entity{Type: "department",
		Name: "Engineering", Description: &desc2})
	if err != nil {
		t.Fatalf("create department entity: %v", err)
	}
	if _, err := factDAO.Create(ctx, dao.Fact{SubjectEntityID: idEmployee, Predicate: "works_in", ObjectEntityID: idDept}); err != nil {
		t.Fatalf("create relation: %v", err)
	}

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	cfg := config.GraphConfig{
		EnableGraph: true,
		MaxDepth:    3,
		MaxNodes:    100,
	}

	// Close the DB so that ListByEntityIDs will fail. The graph is already loaded
	// in memory so BFS still works; only fact loading fails.
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	expander := NewGraphExpander(db, g, cfg)

	results := []SearchResult{
		{ChunkID: 1, ChunkText: "Alice works in Engineering", DocumentID: 1, Entities: []dao.Entity{{ID: idEmployee}}},
	}

	expanded, err := expander.Expand(ctx, results)
	if err != nil {
		t.Fatalf("Expand() returned fatal error: %v", err)
	}

	// Should return original results (non-fatal path).
	if len(expanded) != 1 {
		t.Errorf("got %d expanded results, want 1 (original result preserved)", len(expanded))
	}

	// Original chunk text should be intact.
	if expanded[0].ChunkText != "Alice works in Engineering" {
		t.Errorf("chunk text = %q, want %q", expanded[0].ChunkText, "Alice works in Engineering")
	}
}

// TestGraphExpander_DanglingEdge verifies that when entity A has an edge to B
// but B does not exist as a node in the graph, the dangling edge is excluded
// from related_edges (consistent with get_entity_relations behavior).
func TestGraphExpander_DanglingEdge(t *testing.T) {
	t.Parallel()

	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	desc := "entity A"
	idA, err := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "A", Description: &desc})
	if err != nil {
		t.Fatalf("create entity A: %v", err)
	}

	// Create a fact (edge) from A to a non-existent entity B.
	const idB = 9999 // no entity with this Predicate exists in the database
	if _, err := factDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "relates_to", ObjectEntityID: idB}); err != nil {
		t.Fatalf("create dangling edge A->B: %v", err)
	}

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	cfg := config.GraphConfig{
		EnableGraph: true,
		MaxDepth:    3,
		MaxNodes:    100,
	}
	expander := NewGraphExpander(db, g, cfg)

	results := []SearchResult{
		{ChunkID: 1, ChunkText: "entity A", DocumentID: 1, Entities: []dao.Entity{{ID: idA}}},
	}

	expanded, err := expander.Expand(ctx, results)
	if err != nil {
		t.Fatalf("Expand() error = %v", err)
	}

	if len(expanded) != 1 {
		t.Fatalf("got %d expanded results, want 1", len(expanded))
	}

	// Check related_entities metadata.
	relatedEntities, ok := expanded[0].Metadata["related_entities"]
	if !ok {
		t.Fatal("expected related_entities in metadata")
	}
	entityList, ok := relatedEntities.([]map[string]interface{})
	if !ok {
		t.Fatalf("related_entities is not []map[string]interface{}, got %T", relatedEntities)
	}

	if len(entityList) != 1 {
		t.Fatalf("expected 1 entity in related_entities, got %d", len(entityList))
	}

	entityData := entityList[0]

	// If edges exist, none should reference the dangling target B.
	if edgeListRaw, ok := entityData["edges"]; ok {
		edgeList, ok := edgeListRaw.([]map[string]interface{})
		if !ok {
			t.Fatalf("entity edges is not []map[string]interface{}, got %T", edgeListRaw)
		}
		for _, e := range edgeList {
			targetID, ok := e["target_id"].(int)
			if !ok {
				continue
			}
			if targetID == idB {
				t.Errorf("dangling edge to non-existent entity B (id=%d) found in related_edges", idB)
			}
		}
	}
}
