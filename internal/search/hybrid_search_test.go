package search

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/graph"
)

func TestHybridSearch_TimeoutCancellation(t *testing.T) {
	t.Parallel()

	// A searcher with a very short timeout should return context.DeadlineExceeded.
	mockChunkDAO := &mockChunkDAOLexical{
		searchFTSFunc: func(ctx context.Context, _ string, _ int, _ string) ([]dao.Chunk, error) {
			// Simulate slow query by waiting for context cancellation.
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	mockProvider := &mockEmbeddingProvider{
		generateFunc: func(ctx context.Context, _ []string) ([][]float32, error) {
			// Simulate slow embedding generation.
			<-ctx.Done()
			return nil, ctx.Err()
		},
		vectorDimFunc: func() int { return 768 },
	}

	hs := &hybridSearcher{
		lexical:  newLexicalSearcher(mockChunkDAO),
		semantic: newSemanticSearcher(nil, mockProvider),
		enricher: NewEnricher(nil, nil),
		cfg: config.SearchConfig{
			EnableLexical:  true,
			EnableSemantic: true,
			LexicalTopK:    20,
			SemanticTopK:   20,
			FinalTopK:      10,
			TimeoutMs:      50, // very short timeout
			RRFK:           20,
		},
	}

	ctx := context.Background()
	_, err := hs.HybridSearch(ctx, "test query", 10, "")
	if err == nil {
		t.Fatal("expected timeout error but got none")
	}
	if !strings.Contains(err.Error(), "timed out") && !strings.Contains(err.Error(), "deadline exceeded") {
		t.Errorf("error = %q, want timeout-related error", err.Error())
	}
}

// TestLexicalSearch_RerankerApplied verifies that LexicalSearch applies the
// reranker pipeline via finalize: a deprecated document gets its score reduced.
func TestLexicalSearch_RerankerApplied(t *testing.T) {
	t.Parallel()

	deprecatedMeta := `{"is_deprecated": true}`
	normalMeta := `{}`

	mockChunkDAO := &mockChunkDAOLexical{
		searchFTSFunc: func(_ context.Context, _ string, _ int, _ string) ([]dao.Chunk, error) {
			return []dao.Chunk{
				{ID: 1, ChunkText: "deprecated content", DocID: 1, Score: 0.5},
				{ID: 2, ChunkText: "normal content", DocID: 2, Score: 0.3},
			}, nil
		},
	}

	docDAO := &mockDocDAOEnricher{
		getByIDsFunc: func(_ context.Context, ids []int) (map[int]*dao.Document, error) {
			return map[int]*dao.Document{
				1: {ID: 1, SourceType: "policy", MetadataJSON: &deprecatedMeta},
				2: {ID: 2, SourceType: "tutorial", MetadataJSON: &normalMeta},
			}, nil
		},
	}

	chunkEntityDAO := &mockChunkEntityDAOEnricher{}

	hs := &hybridSearcher{
		lexical:  newLexicalSearcher(mockChunkDAO),
		enricher: NewEnricher(docDAO, chunkEntityDAO),
		reranker: NewReranker(config.SearchConfig{
			DeprecatedBoost: 0.2,
			OfficialBoost:   1.0,
			RecentBoost:     1.0, // disable freshness boost for this test
			RecentDays:      90,
		}),
		cfg: config.SearchConfig{
			LexicalTopK: 20,
			TimeoutMs:   5000,
		},
	}

	results, err := hs.LexicalSearch(context.Background(), "test", 10, "")
	if err != nil {
		t.Fatalf("LexicalSearch() error = %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	// Deprecated doc: invert(0.5)=2.0 * 0.2 = 0.4; normal doc: invert(0.3)≈3.333 * 1.0 ≈ 3.333
	// Normal should rank first after reranking (higher inverted score).
	if results[0].ChunkID != 2 {
		t.Errorf("expected normal chunk (id=2) first, got id=%d (score=%f)", results[0].ChunkID, results[0].Score)
	}

	// Verify deprecated score was reduced.
	var deprecatedScore float64
	for _, r := range results {
		if r.ChunkID == 1 {
			deprecatedScore = r.Score
			break
		}
	}
	if absDiff(deprecatedScore, 0.4) > 0.0001 {
		t.Errorf("deprecated chunk score = %f, want ~0.4 (inverted 1/0.5=2.0 * deprecated_boost 0.2)", deprecatedScore)
	}
}

// TestSemanticSearch_ExpanderApplied verifies that SemanticSearch applies the
// graph expander pipeline via finalize: results with entities get related_entities metadata.
func TestSemanticSearch_ExpanderApplied(t *testing.T) {
	t.Parallel()

	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("setup test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	desc1, desc2 := "engineer", "engineering"
	idEmployee, err := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc1})
	if err != nil {
		t.Fatalf("create employee entity: %v", err)
	}
	idDept, err := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering", Description: &desc2})
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

	graphCfg := config.GraphConfig{EnableGraph: true, MaxDepth: 3, MaxNodes: 100}

	mockProvider := &mockEmbeddingProvider{
		generateFunc: func(_ context.Context, _ []string) ([][]float32, error) {
			return [][]float32{{0.1}}, nil
		},
		vectorDimFunc: func() int { return 1 },
	}

	mockChunkDAOSemantic := &mockChunkDAOSemantic{
		searchVectorFunc: func(_ context.Context, _ []float32, _ int, _ string) ([]dao.Chunk, error) {
			return []dao.Chunk{
				{ID: 1, ChunkText: "Alice works in Engineering", DocID: 1, Score: 0.9},
			}, nil
		},
	}

	docDAO := &mockDocDAOEnricher{
		getByIDsFunc: func(_ context.Context, ids []int) (map[int]*dao.Document, error) {
			return map[int]*dao.Document{
				1: {ID: 1, SourceType: "markdown"},
			}, nil
		},
	}

	chunkEntityDAO := &mockChunkEntityDAOEnricher{
		getEntitiesByChunksFunc: func(_ context.Context, chunkIDs []int) (map[int][]dao.Entity, error) {
			return map[int][]dao.Entity{
				1: {{ID: idEmployee, Name: "Alice", Type: "employee"}},
			}, nil
		},
	}

	hs := &hybridSearcher{
		semantic: newSemanticSearcher(mockChunkDAOSemantic, mockProvider),
		enricher: NewEnricher(docDAO, chunkEntityDAO),
		expander: NewGraphExpander(db, g, graphCfg),
		reranker: NewReranker(config.SearchConfig{
			DeprecatedBoost: 1.0,
			OfficialBoost:   1.0,
			RecentBoost:     1.0,
		}),
		cfg: config.SearchConfig{
			SemanticTopK: 20,
			TimeoutMs:    5000,
		},
	}

	results, err := hs.SemanticSearch(context.Background(), "Alice", 10, "")
	if err != nil {
		t.Fatalf("SemanticSearch() error = %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}

	// Verify related_entities metadata was added by the expander.
	relatedEntities, ok := results[0].Metadata["related_entities"]
	if !ok {
		t.Fatal("expected related_entities in metadata after finalize pipeline")
	}
	entityList, ok := relatedEntities.([]map[string]interface{})
	if !ok {
		t.Fatalf("related_entities is not []map[string]interface{}, got %T", relatedEntities)
	}
	if len(entityList) != 1 {
		t.Errorf("expected 1 entity in related_entities, got %d", len(entityList))
	}
}

// TestHybridSearch_BehaviorUnchanged verifies that HybridSearch still produces
// correct results after refactoring to use finalize: RRF fusion order is preserved.
func TestHybridSearch_BehaviorUnchanged(t *testing.T) {
	t.Parallel()

	mockChunkDAO := &mockChunkDAOLexical{
		searchFTSFunc: func(_ context.Context, _ string, _ int, _ string) ([]dao.Chunk, error) {
			return []dao.Chunk{
				{ID: 1, ChunkText: "shared text", DocID: 1, Score: 0.5},
				{ID: 2, ChunkText: "only lexical", DocID: 1, Score: 0.3},
			}, nil
		},
	}

	mockProvider := &mockEmbeddingProvider{
		generateFunc: func(_ context.Context, _ []string) ([][]float32, error) {
			return [][]float32{{0.1}}, nil
		},
		vectorDimFunc: func() int { return 1 },
	}

	mockChunkDAOSemantic := &mockChunkDAOSemantic{
		searchVectorFunc: func(_ context.Context, _ []float32, _ int, _ string) ([]dao.Chunk, error) {
			return []dao.Chunk{
				{ID: 1, ChunkText: "shared text", DocID: 1, Score: 0.9},
				{ID: 3, ChunkText: "only semantic", DocID: 2, Score: 0.8},
			}, nil
		},
	}

	docDAO := &mockDocDAOEnricher{
		getByIDsFunc: func(_ context.Context, ids []int) (map[int]*dao.Document, error) {
			docs := make(map[int]*dao.Document, len(ids))
			for _, id := range ids {
				docs[id] = &dao.Document{ID: id, SourceType: "markdown"}
			}
			return docs, nil
		},
	}

	chunkEntityDAO := &mockChunkEntityDAOEnricher{}

	hs := &hybridSearcher{
		lexical:  newLexicalSearcher(mockChunkDAO),
		semantic: newSemanticSearcher(mockChunkDAOSemantic, mockProvider),
		enricher: NewEnricher(docDAO, chunkEntityDAO),
		reranker: NewReranker(config.SearchConfig{
			DeprecatedBoost: 1.0,
			OfficialBoost:   1.0,
			RecentBoost:     1.0,
		}),
		cfg: config.SearchConfig{
			EnableLexical:  true,
			EnableSemantic: true,
			LexicalTopK:    20,
			SemanticTopK:   20,
			FinalTopK:      10,
			TimeoutMs:      5000,
			RRFK:           20,
		},
	}

	results, err := hs.HybridSearch(context.Background(), "test", 10, "")
	if err != nil {
		t.Fatalf("HybridSearch() error = %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	// Chunk 1 appears in both lists → highest RRF score → should be first.
	if results[0].ChunkID != 1 {
		t.Errorf("expected chunk 1 (shared) first, got chunk %d", results[0].ChunkID)
	}

	// Verify source types contain the expected search strategy.
	sourceMap := make(map[int]string)
	for _, r := range results {
		sourceMap[r.ChunkID] = r.SourceType
	}
	if !strings.Contains(sourceMap[1], "hybrid") {
		t.Errorf("chunk 1 source_type = %q, want to contain hybrid", sourceMap[1])
	}
	if !strings.Contains(sourceMap[2], "lexical") {
		t.Errorf("chunk 2 source_type = %q, want to contain lexical", sourceMap[2])
	}
	if !strings.Contains(sourceMap[3], "semantic") {
		t.Errorf("chunk 3 source_type = %q, want to contain semantic", sourceMap[3])
	}

	// Verify scores are in descending order.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("scores not sorted: result[%d].score = %f > result[%d].score = %f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}

	// Verify ranks are assigned correctly.
	for i, r := range results {
		if r.Rank != i+1 {
			t.Errorf("result[%d].Rank = %d, want %d", i, r.Rank, i+1)
		}
	}
}
