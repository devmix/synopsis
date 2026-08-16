package search_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/graph"
	"github.com/devmix/synopsis/internal/search"
)

// BenchmarkGraphExpansion_BFS benchmarks the BFS graph traversal with varying parameters.
func BenchmarkGraphExpansion_BFS(b *testing.B) {
	// Setup test database with a medium-sized graph
	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		b.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	// Create a medium-sized graph with 50 nodes and ~100 edges
	// This simulates a realistic knowledge graph scenario
	const (
		numEntities = 50
		avgDegree   = 4 // average outgoing edges per entity
	)

	// Create entities of different types
	entityTypes := []string{"employee", "department", "policy", "system", "service"}
	entityIDs := make([]int, 0, numEntities)

	for i := 0; i < numEntities; i++ {
		name := ""
		desc := ""
		switch i % 5 {
		case 0:
			name = "Alice"
			desc = "Senior employee"
		case 1:
			name = "Engineering"
			desc = "Engineering department"
		case 2:
			name = "NDA Policy"
			desc = "Confidentiality policy"
		case 3:
			name = "Microservice"
			desc = "Core system"
		case 4:
			name = "Admin Service"
			desc = "Management service"
		}

		id, _ := entDAO.Create(ctx, dao.Entity{
			Type:        entityTypes[i%len(entityTypes)],
			Name:        name + " " + string(rune('A'+i)),
			Description: &desc,
		})
		entityIDs = append(entityIDs, id)
	}

	// Create random relations to form a connected graph
	for i := 0; i < numEntities*avgDegree; i++ {
		src := entityIDs[i%numEntities]
		tgt := entityIDs[(i+1+(i%17))%numEntities]
		if src == tgt {
			continue
		}
		relTypes := []string{"works_in", "owns", "requires", "belongs_to", "knows"}
		relType := relTypes[i%len(relTypes)]
		relDAO.Create(ctx, dao.Fact{ //nolint:errcheck
			SubjectEntityID: src,
			Predicate:       relType,
			ObjectEntityID:  tgt,
		})
	}

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		b.Fatalf("NewGraphFromDB() error = %v", err)
	}

	// Benchmark different BFS configurations
	benchmarks := []struct {
		name      string
		startID   int
		maxDepth  int
		direction graph.Direction
		maxNodes  int
	}{
		{
			name:      "depth1_outgoing",
			startID:   entityIDs[0],
			maxDepth:  1,
			direction: graph.DirectionOutgoing,
			maxNodes:  1000,
		},
		{
			name:      "depth2_both",
			startID:   entityIDs[0],
			maxDepth:  2,
			direction: graph.DirectionBoth,
			maxNodes:  1000,
		},
		{
			name:      "depth3_incoming",
			startID:   entityIDs[0],
			maxDepth:  3,
			direction: graph.DirectionIncoming,
			maxNodes:  1000,
		},
		{
			name:      "depth5_both",
			startID:   entityIDs[0],
			maxDepth:  5,
			direction: graph.DirectionBoth,
			maxNodes:  1000,
		},
		{
			name:      "depth5_limited_nodes",
			startID:   entityIDs[0],
			maxDepth:  5,
			direction: graph.DirectionBoth,
			maxNodes:  50,
		},
	}

	for _, bb := range benchmarks {
		b.Run(bb.name, func(b *testing.B) {
			opts := graph.BFSOptions{
				MaxDepth:  bb.maxDepth,
				Direction: bb.direction,
				MaxNodes:  bb.maxNodes,
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				result, err := g.BFS(context.Background(), bb.startID, opts)
				if err != nil {
					b.Fatalf("BFS() error = %v", err)
				}
				_ = result
			}
		})
	}
}

// BenchmarkGraphExpansion_LargeGraph benchmarks BFS on a larger graph (500 nodes).
func BenchmarkGraphExpansion_LargeGraph(b *testing.B) {
	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		b.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	// Create a larger graph with 500 nodes
	const numEntities = 500

	entityTypes := []string{"employee", "department", "policy", "system", "service"}
	entityIDs := make([]int, 0, numEntities)

	for i := 0; i < numEntities; i++ {
		name := "Entity_" + string(rune('A'+i%26)) + string(rune('0'+i/26%10))
		desc := "Test entity for benchmarking"
		id, _ := entDAO.Create(ctx, dao.Entity{
			Type:        entityTypes[i%len(entityTypes)],
			Name:        name,
			Description: &desc,
		})
		entityIDs = append(entityIDs, id)
	}

	// Create ~1500 edges (avg degree 3)
	for i := 0; i < numEntities*3; i++ {
		src := entityIDs[i%numEntities]
		tgt := entityIDs[(i+1+(i%19))%numEntities]
		if src == tgt {
			continue
		}
		relDAO.Create(ctx, dao.Fact{ //nolint:errcheck
			SubjectEntityID: src,
			Predicate:       "connected",
			ObjectEntityID:  tgt,
		})
	}

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		b.Fatalf("NewGraphFromDB() error = %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		result, err := g.BFS(context.Background(), entityIDs[0], graph.BFSOptions{
			MaxDepth:  3,
			Direction: graph.DirectionBoth,
			MaxNodes:  1000,
		})
		if err != nil {
			b.Fatalf("BFS() error = %v", err)
		}
		_ = result
	}
}

// BenchmarkGraphExpander_Expand benchmarks the full graph expansion pipeline.
func BenchmarkGraphExpander_Expand(b *testing.B) {
	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		b.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	// Create a medium graph with facts
	const numEntities = 30

	entityTypes := []string{"employee", "department", "policy"}
	entityIDs := make([]int, 0, numEntities)

	for i := 0; i < numEntities; i++ {
		name := ""
		desc := ""
		switch i % 3 {
		case 0:
			name = "Alice_" + string(rune('A'+i))
			desc = "An employee"
		case 1:
			name = "Dept_" + string(rune('A'+i))
			desc = "A department"
		case 2:
			name = "Policy_" + string(rune('A'+i))
			desc = "A policy"
		}

		id, _ := entDAO.Create(ctx, dao.Entity{
			Type:        entityTypes[i%len(entityTypes)],
			Name:        name,
			Description: &desc,
		})
		entityIDs = append(entityIDs, id)
	}

	// Create relations
	for i := 0; i < numEntities*2; i++ {
		src := entityIDs[i%numEntities]
		tgt := entityIDs[(i+1+(i%13))%numEntities]
		if src == tgt {
			continue
		}
		relDAO.Create(ctx, dao.Fact{ //nolint:errcheck
			SubjectEntityID: src,
			Predicate:       "related_to",
			ObjectEntityID:  tgt,
		})
	}

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		b.Fatalf("NewGraphFromDB() error = %v", err)
	}

	// Create a simple graph config
	cfg := config.GraphConfig{
		MaxDepth:    3,
		MaxNodes:    100,
		EnableGraph: true,
	}

	expander := search.NewGraphExpander(db, g, cfg)

	// Create test search results
	results := []search.SearchResult{
		{
			DocumentID: 1,
			ChunkID:    1,
			ChunkText:  "Test content mentioning Alice_A",
			Score:      0.95,
			Entities:   []dao.Entity{{ID: entityIDs[0]}, {ID: entityIDs[1]}, {ID: entityIDs[2]}},
		},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		expanded, err := expander.Expand(ctx, results)
		if err != nil {
			b.Fatalf("Expand() error = %v", err)
		}
		_ = expanded
	}
}
