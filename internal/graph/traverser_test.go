package graph_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/graph"
)

// TestBFSWithoutFollowEntityLinks_SameDomainOnly verifies that BFS without the
// FollowEntityLinks flag does not cross domain boundaries (regression test).
func TestBFSWithoutFollowEntityLinks_SameDomainOnly(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	idAlice, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Domain: "hr"})
	idBob, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Bob", Domain: "policy"})

	// Create entity link so cross-domain connection exists.
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idAlice,
		TargetEntityID:  idBob,
		RelationType:    "same_as",
		Method:          "rule",
		Confidence:      0.95,
	})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	result, err := g.BFS(context.Background(), idAlice, graph.BFSOptions{MaxDepth: 5})
	if err != nil {
		t.Fatalf("BFS() error = %v", err)
	}

	// Without FollowEntityLinks, only Alice should be found (Bob is in different domain).
	if len(result.Nodes) != 1 {
		t.Errorf("expected 1 node without FollowEntityLinks, got %d", len(result.Nodes))
	}
	for _, n := range result.Nodes {
		if n.ID == idBob {
			t.Error("BFS crossed domain boundary without FollowEntityLinks flag")
		}
	}
}

// TestBFSWithFollowEntityLinks_CrossDomainViaLinks verifies that BFS with the
// FollowEntityLinks flag crosses domain boundaries via entity links.
func TestBFSWithFollowEntityLinks_CrossDomainViaLinks(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	idAlice, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Domain: "hr"})
	idBob, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Bob", Domain: "policy"})

	// Create entity link so cross-domain connection exists.
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idAlice,
		TargetEntityID:  idBob,
		RelationType:    "same_as",
		Method:          "rule",
		Confidence:      0.95,
	})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	result, err := g.BFS(context.Background(), idAlice, graph.BFSOptions{MaxDepth: 5, FollowEntityLinks: true})
	if err != nil {
		t.Fatalf("BFS() error = %v", err)
	}

	// With FollowEntityLinks and an entity link, Bob should be reachable.
	found := false
	for _, n := range result.Nodes {
		if n.ID == idBob {
			found = true
			break
		}
	}
	if !found {
		t.Error("BFS with FollowEntityLinks did not reach cross-domain entity via entity link")
	}
}

// TestBFSWithFollowEntityLinks_FactEdgeNoCrossDomain verifies that BFS with the
// FollowEntityLinks flag still blocks cross-domain traversal via fact edges.
func TestBFSWithFollowEntityLinks_FactEdgeNoCrossDomain(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	idAlice, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Domain: "hr"})
	idBob, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Bob", Domain: "policy"})

	// Create a fact edge (not an entity link) between domains.
	factDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "knows", ObjectEntityID: idBob}) //nolint:errcheck

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	result, err := g.BFS(context.Background(), idAlice, graph.BFSOptions{MaxDepth: 5, FollowEntityLinks: true})
	if err != nil {
		t.Fatalf("BFS() error = %v", err)
	}

	// Even with FollowEntityLinks, fact edges should not cross domains.
	for _, n := range result.Nodes {
		if n.ID == idBob {
			t.Error("BFS crossed domain boundary via fact edge despite no entity link")
		}
	}
}

// TestBFSWithFollowEntityLinks_CycleProtection verifies that cycle protection
// works correctly with cross-domain traversal.
func TestBFSWithFollowEntityLinks_CycleProtection(t *testing.T) {
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
	idC, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeC", Domain: "it"})

	// Create entity links forming a cycle: A -> B -> C -> A.
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idB, RelationType: "same_as", Method: "rule", Confidence: 0.95,
	})
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idB, TargetEntityID: idC, RelationType: "same_as", Method: "rule", Confidence: 0.95,
	})
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idC, TargetEntityID: idA, RelationType: "same_as", Method: "rule", Confidence: 0.95,
	})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	result, err := g.BFS(context.Background(), idA, graph.BFSOptions{MaxDepth: 10, FollowEntityLinks: true})
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

// TestBFS_EntityLinkDeterminism_Directions verifies that BFS with FollowEntityLinks
// produces deterministic edge ordering for DirectionOutgoing and DirectionIncoming (m-9).
func TestBFS_EntityLinkDeterminism_Directions(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	// Create entities across domains.
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeA", Domain: "hr"})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeB", Domain: "policy"})
	idC, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeC", Domain: "it"})

	// Create entity links A→B and A→C (outgoing from A).
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idB, RelationType: "same_as", Method: "rule", Confidence: 1.0,
	})
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idC, RelationType: "related_to", Method: "equals", Confidence: 0.9,
	})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	const iterations = 50

	for _, dir := range []graph.Direction{graph.DirectionOutgoing, graph.DirectionIncoming} {
		t.Run(string(dir), func(t *testing.T) {
			var firstEdgePairs [][]int
			for i := 0; i < iterations; i++ {
				result, err := g.BFS(context.Background(), idA, graph.BFSOptions{MaxDepth: 2, Direction: dir, FollowEntityLinks: true})
				if err != nil {
					t.Fatalf("iteration %d: BFS() error = %v", i, err)
				}

				edgePairs := make([][]int, len(result.Edges))
				for j, e := range result.Edges {
					edgePairs[j] = []int{e.SourceID, e.TargetID}
				}

				if i == 0 {
					firstEdgePairs = edgePairs
					continue
				}

				if !edgePairsEq(firstEdgePairs, edgePairs) {
					t.Errorf("iteration %d: edge order differs from first run for direction %s.\nfirst: %v\ncurr : %v",
						i, dir, formatEdgePairs(firstEdgePairs), formatEdgePairs(edgePairs))
					break
				}
			}
		})
	}
}

// TestBFS_EntityLinkDeterminism verifies that BFS with FollowEntityLinks produces
// deterministic edge ordering across multiple runs (m-9 regression).
func TestBFS_EntityLinkDeterminism(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	// Create entities across domains.
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeA", Domain: "hr"})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeB", Domain: "policy"})
	idC, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeC", Domain: "it"})

	// Create entity links A→B and A→C.
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idB, RelationType: "same_as", Method: "rule", Confidence: 1.0,
	})
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idC, RelationType: "related_to", Method: "equals", Confidence: 0.9,
	})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	const iterations = 50
	var firstEdgePairs [][]int

	for i := 0; i < iterations; i++ {
		result, err := g.BFS(context.Background(), idA, graph.BFSOptions{MaxDepth: 2, Direction: graph.DirectionBoth, FollowEntityLinks: true})
		if err != nil {
			t.Fatalf("iteration %d: BFS() error = %v", i, err)
		}

		edgePairs := make([][]int, len(result.Edges))
		for j, e := range result.Edges {
			edgePairs[j] = []int{e.SourceID, e.TargetID}
		}

		if i == 0 {
			firstEdgePairs = edgePairs
			continue
		}

		if !edgePairsEq(firstEdgePairs, edgePairs) {
			t.Errorf("iteration %d: entity link edge order differs from first run.\nfirst: %v\ncurr : %v",
				i, formatEdgePairs(firstEdgePairs), formatEdgePairs(edgePairs))
			break
		}
	}
}

// TestBFS_DualEdgePreservation verifies that fact edges and entity link edges
// are assembled separately in DirectionBoth mode (m-12). If they shared a dedup
// map, an entity-link edge could overwrite a fact edge with the same source-target.
// This test uses two different neighbors to prove both types survive assembly.
func TestBFS_DualEdgePreservation(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeA", Domain: "hr"})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeB", Domain: "hr"})
	idC, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeC", Domain: "policy"})

	// Fact edge A→B (same domain).
	factDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "knows", ObjectEntityID: idB}) //nolint:errcheck

	// Entity link A→C (cross-domain, needs FollowEntityLinks).
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idC, RelationType: "same_as", Method: "rule", Confidence: 1.0,
	})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	result, err := g.BFS(context.Background(), idA, graph.BFSOptions{MaxDepth: 2, Direction: graph.DirectionBoth, FollowEntityLinks: true})
	if err != nil {
		t.Fatalf("BFS() error = %v", err)
	}

	// Both B (via fact edge) and C (via entity link) should be reachable.
	foundB := false
	foundC := false
	for _, n := range result.Nodes {
		if n.ID == idB {
			foundB = true
		}
		if n.ID == idC {
			foundC = true
		}
	}
	if !foundB {
		t.Error("node B not reached via fact edge")
	}
	if !foundC {
		t.Error("node C not reached via entity link edge")
	}

	// Verify both edge types appear in result.Edges.
	hasFactEdge := false
	hasEntityLinkEdge := false
	for _, e := range result.Edges {
		if e.Method == "" && (e.SourceID == idA || e.TargetID == idA) {
			hasFactEdge = true
		}
		if e.Method != "" {
			hasEntityLinkEdge = true
		}
	}
	if !hasFactEdge {
		t.Error("no fact edge in BFS result")
	}
	if !hasEntityLinkEdge {
		t.Error("no entity link edge in BFS result")
	}
}

// TestBFS_DualEdgeSamePair verifies the exact m-12 scenario: the SAME pair (A,B)
// has both a fact edge AND an entity-link edge. Since BFS visits each node once,
// only one edge per neighbor appears in traversal results — but the key guarantee
// is that the fact-edge provenance is preserved (not overwritten by entity-link).
// Additionally, when edges go to DIFFERENT neighbors, both types must appear with
// distinct Method values and correct provenance.
func TestBFS_DualEdgeSamePair(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeA", Domain: "hr"})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeB", Domain: "hr"})
	idC, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "NodeC", Domain: "policy"})

	// Fact edge A→B (same domain).
	factDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "knows", ObjectEntityID: idB}) //nolint:errcheck

	// Entity link A→B (SAME pair — both edges must coexist in assembly; fact edge wins traversal).
	evidence := "rule: hr/NodeA -> hr/NodeB"
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idB, RelationType: "same_as", Method: "rule", Confidence: 1.0, Evidence: &evidence,
	})

	// Entity link A→C (different neighbor — entity-link edge must appear independently).
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idC, RelationType: "same_as", Method: "rule", Confidence: 1.0,
	})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	result, err := g.BFS(context.Background(), idA, graph.BFSOptions{MaxDepth: 2, Direction: graph.DirectionBoth, FollowEntityLinks: true})
	if err != nil {
		t.Fatalf("BFS() error = %v", err)
	}

	// Verify B is reached via fact edge (Method == ""), not entity-link.
	// This proves the fact edge was NOT overwritten by the entity-link during assembly.
	factEdgeToB := false
	for _, e := range result.Edges {
		if e.SourceID == idA && e.TargetID == idB {
			if e.Method == "" {
				factEdgeToB = true
			} else {
				t.Errorf("edge A→B has Method=%q (fact edge overwritten by entity-link?)", e.Method)
			}
		}
	}
	if !factEdgeToB {
		t.Error("fact edge A→B missing from BFS result")
	}

	// Verify C is reached via entity link with correct provenance.
	entityLinkToC := false
	for _, e := range result.Edges {
		if e.SourceID == idA && e.TargetID == idC {
			if e.Method == "rule" && e.Confidence == 1.0 {
				entityLinkToC = true
			} else {
				t.Errorf("edge A→C has Method=%q Confidence=%f, want rule/1.0", e.Method, e.Confidence)
			}
		}
	}
	if !entityLinkToC {
		t.Error("entity link edge A→C missing from BFS result")
	}

	// Verify both nodes are present.
	foundB := false
	foundC := false
	for _, n := range result.Nodes {
		if n.ID == idB {
			foundB = true
		}
		if n.ID == idC {
			foundC = true
		}
	}
	if !foundB {
		t.Error("node B not reached")
	}
	if !foundC {
		t.Error("node C not reached via entity link")
	}

	// Also verify with DirectionOutgoing — same guarantees.
	resultOut, err := g.BFS(context.Background(), idA, graph.BFSOptions{MaxDepth: 2, Direction: graph.DirectionOutgoing, FollowEntityLinks: true})
	if err != nil {
		t.Fatalf("BFS outgoing error = %v", err)
	}
	factEdgeToB = false
	entityLinkToC = false
	for _, e := range resultOut.Edges {
		if e.SourceID == idA && e.TargetID == idB && e.Method == "" {
			factEdgeToB = true
		}
		if e.SourceID == idA && e.TargetID == idC && e.Method == "rule" {
			entityLinkToC = true
		}
	}
	if !factEdgeToB {
		t.Error("DirectionOutgoing: fact edge A→B missing")
	}
	if !entityLinkToC {
		t.Error("DirectionOutgoing: entity link edge A→C missing")
	}
}

// TestBFS_EntityLinkProvenance verifies that entity-link edges carry Method/Confidence/Evidence (m-10).
func TestBFS_EntityLinkProvenance(t *testing.T) {
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

	evidence := "rule: hr/NodeA -> policy/NodeB"
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idB, RelationType: "same_as", Method: "rule", Confidence: 1.0, Evidence: &evidence,
	})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	result, err := g.BFS(context.Background(), idA, graph.BFSOptions{MaxDepth: 2, Direction: graph.DirectionBoth, FollowEntityLinks: true})
	if err != nil {
		t.Fatalf("BFS() error = %v", err)
	}

	found := false
	for _, e := range result.Edges {
		if e.SourceID == idA && e.TargetID == idB {
			found = true
			if e.Method != "rule" {
				t.Errorf("entity link edge Method = %q, want %q", e.Method, "rule")
			}
			if e.Confidence != 1.0 {
				t.Errorf("entity link edge Confidence = %f, want %f", e.Confidence, 1.0)
			}
			if e.Evidence == nil || *e.Evidence != evidence {
				t.Errorf("entity link edge Evidence = %v, want %q", e.Evidence, evidence)
			}
		}
	}
	if !found {
		t.Error("expected entity link edge from A to B in BFS result")
	}
}
