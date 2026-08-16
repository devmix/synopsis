package search

import (
	"context"
	"fmt"
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
)

// TestHybridSearch_DomainFilterBeforeTruncation verifies that domain filtering
// happens before topK truncation: 30 chunks, 10 in domain X, topK=10 → with
// domain X returns 10 (not fewer).
func TestHybridSearch_DomainFilterBeforeTruncation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		domain    string
		topK      int
		wantCount int
	}{
		{
			name:      "domain hr returns up to topK results within domain",
			domain:    "hr",
			topK:      10,
			wantCount: 10, // 10 chunks in hr domain, all should be returned (not truncated before filter)
		},
		{
			name:      "domain engineering returns up to topK results within domain",
			domain:    "engineering",
			topK:      10,
			wantCount: 10, // 20 chunks in engineering, truncated to 10 after filter
		},
		{
			name:      "no domain returns all up to topK",
			domain:    "",
			topK:      10,
			wantCount: 10,
		},
		{
			name:      "domain with no matches returns empty",
			domain:    "product",
			topK:      10,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// Do NOT use t.Parallel() here — each subtest creates its own searcher
			// to avoid race conditions on shared mock state.

			hrDomain := "hr"
			engineeringDomain := "engineering"

			// Build 30 lexical results: chunks 1-20 are "engineering", chunks 21-30 are "hr".
			lexicalChunks := make([]dao.Chunk, 0, 30)
			for i := 1; i <= 30; i++ {
				lexicalChunks = append(lexicalChunks, dao.Chunk{ID: i, ChunkText: fmt.Sprintf("chunk %d", i), DocID: i})
			}

			mockChunkDAO := &mockChunkDAOLexical{
				searchFTSFunc: func(_ context.Context, _ string, _ int, domain string) ([]dao.Chunk, error) {
					filtered := make([]dao.Chunk, 0, len(lexicalChunks))
					for _, c := range lexicalChunks {
						chunkDomain := ""
						if c.ID >= 21 && c.ID <= 30 {
							chunkDomain = hrDomain
						} else {
							chunkDomain = engineeringDomain
						}
						if domain == "" || chunkDomain == domain {
							filtered = append(filtered, c)
						}
					}
					return filtered, nil
				},
			}

			mockProvider := &mockEmbeddingProvider{
				generateFunc: func(_ context.Context, _ []string) ([][]float32, error) {
					return nil, nil // no semantic results
				},
				vectorDimFunc: func() int { return 768 },
			}

			mockChunkDAOSemantic := &mockChunkDAOSemantic{
				searchVectorFunc: func(_ context.Context, _ []float32, _ int, _ string) ([]dao.Chunk, error) {
					return nil, nil // no semantic results
				},
			}

			docDAO := &mockDocDAOEnricher{
				getByIDsFunc: func(_ context.Context, ids []int) (map[int]*dao.Document, error) {
					docs := make(map[int]*dao.Document, len(ids))
					for _, id := range ids {
						var metaJSON *string
						if id >= 21 && id <= 30 {
							m := fmt.Sprintf(`{"domain":"%s"}`, hrDomain)
							metaJSON = &m
						} else {
							m := fmt.Sprintf(`{"domain":"%s"}`, engineeringDomain)
							metaJSON = &m
						}
						docs[id] = &dao.Document{ID: id, SourceType: "markdown", MetadataJSON: metaJSON}
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
					EnableSemantic: false,
					LexicalTopK:    30,
					SemanticTopK:   20,
					FinalTopK:      10,
					TimeoutMs:      5000,
					RRFK:           20,
				},
			}

			results, err := hs.HybridSearch(context.Background(), "test query", tt.topK, tt.domain)
			if err != nil {
				t.Fatalf("HybridSearch() error = %v", err)
			}

			if len(results) != tt.wantCount {
				t.Errorf("got %d results, want %d", len(results), tt.wantCount)
			}

			// Verify all returned results belong to the requested domain.
			if tt.domain != "" && len(results) > 0 {
				for _, r := range results {
					domains, ok := r.Metadata["domains"]
					if !ok {
						t.Errorf("chunk %d missing domains metadata", r.ChunkID)
						continue
					}
					domainSlice, ok := domains.([]string)
					if !ok {
						t.Errorf("chunk %d domains not []string: %T = %v", r.ChunkID, domains, domains)
						continue
					}
					found := false
					for _, d := range domainSlice {
						if d == tt.domain {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("chunk %d domains = %v, want to contain %q", r.ChunkID, domains, tt.domain)
					}
				}
			}
		})
	}
}

// TestHybridSearch_NoDomainBehaviorUnchanged verifies that HybridSearch without
// a domain filter produces the same results as before (backward compatibility).
func TestHybridSearch_NoDomainBehaviorUnchanged(t *testing.T) {
	t.Parallel()

	mockChunkDAO := &mockChunkDAOLexical{
		searchFTSFunc: func(_ context.Context, _ string, _ int, _ string) ([]dao.Chunk, error) {
			return []dao.Chunk{
				{ID: 1, ChunkText: "alpha", DocID: 1, Score: 0.9},
				{ID: 2, ChunkText: "beta", DocID: 1, Score: 0.8},
				{ID: 3, ChunkText: "gamma", DocID: 2, Score: 0.7},
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
				{ID: 1, ChunkText: "alpha", DocID: 1, Score: 0.95},
				{ID: 4, ChunkText: "delta", DocID: 2, Score: 0.6},
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

	// Should return all 4 unique chunks (3 lexical + 1 semantic-only).
	if len(results) != 4 {
		t.Fatalf("got %d results, want 4", len(results))
	}

	// Chunk 1 appears in both lists → highest RRF score → should be first.
	if results[0].ChunkID != 1 {
		t.Errorf("expected chunk 1 (shared) first, got chunk %d", results[0].ChunkID)
	}

	// Verify scores are in descending order.
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("scores not sorted: result[%d].score = %f > result[%d].score = %f",
				i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

// TestFilterByDomain verifies the filterByDomain helper function.
func TestFilterByDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		results   []SearchResult
		domain    string
		wantCount int
	}{
		{
			name: "empty domain returns all",
			results: []SearchResult{
				{ChunkID: 1, Metadata: map[string]interface{}{"domains": []interface{}{"hr"}}},
				{ChunkID: 2, Metadata: map[string]interface{}{"domains": []interface{}{"engineering"}}},
			},
			domain:    "",
			wantCount: 2,
		},
		{
			name: "domain matches interface slice",
			results: []SearchResult{
				{ChunkID: 1, Metadata: map[string]interface{}{"domains": []interface{}{"hr", "policy"}}},
				{ChunkID: 2, Metadata: map[string]interface{}{"domains": []interface{}{"engineering"}}},
			},
			domain:    "hr",
			wantCount: 1,
		},
		{
			name: "domain matches string type",
			results: []SearchResult{
				{ChunkID: 1, Metadata: map[string]interface{}{"domains": "engineering"}},
			},
			domain:    "engineering",
			wantCount: 1,
		},
		{
			name: "domain case insensitive",
			results: []SearchResult{
				{ChunkID: 1, Metadata: map[string]interface{}{"domains": []interface{}{"hr"}}},
			},
			domain:    "HR",
			wantCount: 1,
		},
		{
			name: "domain no match returns empty",
			results: []SearchResult{
				{ChunkID: 1, Metadata: map[string]interface{}{"domains": []interface{}{"hr"}}},
			},
			domain:    "product",
			wantCount: 0,
		},
		{
			name: "skips results without metadata",
			results: []SearchResult{
				{ChunkID: 1}, // no Metadata
				{ChunkID: 2, Metadata: map[string]interface{}{"domains": []interface{}{"hr"}}},
			},
			domain:    "hr",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := filterByDomain(tt.results, tt.domain)
			if len(got) != tt.wantCount {
				t.Errorf("filterByDomain() returned %d results, want %d", len(got), tt.wantCount)
			}
		})
	}
}

// TestLexicalSearch_DomainFilter verifies that LexicalSearch passes the domain
// parameter through to the underlying lexical searcher and DAO.
func TestLexicalSearch_DomainFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		domain    string
		wantCount int
	}{
		{
			name:      "domain hr returns only hr chunks",
			domain:    "hr",
			wantCount: 2,
		},
		{
			name:      "no domain returns all chunks",
			domain:    "",
			wantCount: 5,
		},
		{
			name:      "nonexistent domain returns empty",
			domain:    "product",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			var capturedDomain string
			mockChunkDAO := &mockChunkDAOLexical{
				searchFTSFunc: func(_ context.Context, _ string, _ int, domain string) ([]dao.Chunk, error) {
					capturedDomain = domain
					if domain == "hr" {
						return []dao.Chunk{
							{ID: 1, ChunkText: "hr policy", DocID: 1, Score: 0.5},
							{ID: 2, ChunkText: "hr benefits", DocID: 1, Score: 0.3},
						}, nil
					}
					if domain == "" {
						return []dao.Chunk{
							{ID: 1, ChunkText: "hr policy", DocID: 1, Score: 0.5},
							{ID: 2, ChunkText: "hr benefits", DocID: 1, Score: 0.3},
							{ID: 3, ChunkText: "eng spec", DocID: 2, Score: 0.4},
							{ID: 4, ChunkText: "eng design", DocID: 2, Score: 0.6},
							{ID: 5, ChunkText: "eng review", DocID: 2, Score: 0.7},
						}, nil
					}
					return nil, nil // nonexistent domain
				},
			}

			hrDomain := "hr"
			engineeringDomain := "engineering"
			docDAO := &mockDocDAOEnricher{
				getByIDsFunc: func(_ context.Context, ids []int) (map[int]*dao.Document, error) {
					docs := make(map[int]*dao.Document, len(ids))
					for _, id := range ids {
						var metaJSON *string
						if id == 1 || id == 2 {
							m := fmt.Sprintf(`{"domain":"%s"}`, hrDomain)
							metaJSON = &m
						} else {
							m := fmt.Sprintf(`{"domain":"%s"}`, engineeringDomain)
							metaJSON = &m
						}
						docs[id] = &dao.Document{ID: id, SourceType: "markdown", MetadataJSON: metaJSON}
					}
					return docs, nil
				},
			}

			chunkEntityDAO := &mockChunkEntityDAOEnricher{}

			hs := &hybridSearcher{
				lexical:  newLexicalSearcher(mockChunkDAO),
				enricher: NewEnricher(docDAO, chunkEntityDAO),
				reranker: NewReranker(config.SearchConfig{
					DeprecatedBoost: 1.0,
					OfficialBoost:   1.0,
					RecentBoost:     1.0,
				}),
				cfg: config.SearchConfig{
					LexicalTopK: 20,
					TimeoutMs:   5000,
				},
			}

			results, err := hs.LexicalSearch(context.Background(), "test", 10, tt.domain)
			if err != nil {
				t.Fatalf("LexicalSearch() error = %v", err)
			}

			if capturedDomain != tt.domain {
				t.Errorf("captured domain = %q, want %q", capturedDomain, tt.domain)
			}

			if len(results) != tt.wantCount {
				t.Errorf("LexicalSearch(domain=%q) returned %d results, want %d", tt.domain, len(results), tt.wantCount)
			}
		})
	}
}

// TestSemanticSearch_DomainFilter verifies that SemanticSearch passes the domain
// parameter through to the underlying semantic searcher and DAO.
func TestSemanticSearch_DomainFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		domain    string
		wantCount int
	}{
		{
			name:      "domain hr returns only hr chunks",
			domain:    "hr",
			wantCount: 2,
		},
		{
			name:      "no domain returns all chunks",
			domain:    "",
			wantCount: 4,
		},
		{
			name:      "nonexistent domain returns empty",
			domain:    "product",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			var capturedDomain string
			mockProvider := &mockEmbeddingProvider{
				generateFunc: func(_ context.Context, _ []string) ([][]float32, error) {
					return [][]float32{{0.1, 0.2}}, nil
				},
				vectorDimFunc: func() int { return 2 },
			}

			mockChunkDAOSemantic := &mockChunkDAOSemantic{
				searchVectorFunc: func(_ context.Context, _ []float32, _ int, domain string) ([]dao.Chunk, error) {
					capturedDomain = domain
					if domain == "hr" {
						return []dao.Chunk{
							{ID: 1, ChunkText: "hr policy", DocID: 1, Score: 0.5},
							{ID: 2, ChunkText: "hr benefits", DocID: 1, Score: 0.3},
						}, nil
					}
					if domain == "" {
						return []dao.Chunk{
							{ID: 1, ChunkText: "hr policy", DocID: 1, Score: 0.5},
							{ID: 2, ChunkText: "hr benefits", DocID: 1, Score: 0.3},
							{ID: 3, ChunkText: "eng spec", DocID: 2, Score: 0.4},
							{ID: 4, ChunkText: "eng design", DocID: 2, Score: 0.6},
						}, nil
					}
					return nil, nil // nonexistent domain
				},
			}

			hrDomain := "hr"
			engineeringDomain := "engineering"
			docDAO := &mockDocDAOEnricher{
				getByIDsFunc: func(_ context.Context, ids []int) (map[int]*dao.Document, error) {
					docs := make(map[int]*dao.Document, len(ids))
					for _, id := range ids {
						var metaJSON *string
						if id == 1 || id == 2 {
							m := fmt.Sprintf(`{"domain":"%s"}`, hrDomain)
							metaJSON = &m
						} else {
							m := fmt.Sprintf(`{"domain":"%s"}`, engineeringDomain)
							metaJSON = &m
						}
						docs[id] = &dao.Document{ID: id, SourceType: "markdown", MetadataJSON: metaJSON}
					}
					return docs, nil
				},
			}

			chunkEntityDAO := &mockChunkEntityDAOEnricher{}

			hs := &hybridSearcher{
				semantic: newSemanticSearcher(mockChunkDAOSemantic, mockProvider),
				enricher: NewEnricher(docDAO, chunkEntityDAO),
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

			results, err := hs.SemanticSearch(context.Background(), "test", 10, tt.domain)
			if err != nil {
				t.Fatalf("SemanticSearch() error = %v", err)
			}

			if capturedDomain != tt.domain {
				t.Errorf("captured domain = %q, want %q", capturedDomain, tt.domain)
			}

			if len(results) != tt.wantCount {
				t.Errorf("SemanticSearch(domain=%q) returned %d results, want %d", tt.domain, len(results), tt.wantCount)
			}
		})
	}
}

// TestHybridSearch_DomainPassedToSubsearches verifies that HybridSearch passes
// the domain parameter to both lexical and semantic sub-searches.
func TestHybridSearch_DomainPassedToSubsearches(t *testing.T) {
	t.Parallel()

	var capturedLexicalDomain, capturedSemanticDomain string

	mockChunkDAO := &mockChunkDAOLexical{
		searchFTSFunc: func(_ context.Context, _ string, _ int, domain string) ([]dao.Chunk, error) {
			capturedLexicalDomain = domain
			return []dao.Chunk{{ID: 1, ChunkText: "test", DocID: 1, Score: 0.5}}, nil
		},
	}

	mockProvider := &mockEmbeddingProvider{
		generateFunc: func(_ context.Context, _ []string) ([][]float32, error) {
			return [][]float32{{0.1}}, nil
		},
		vectorDimFunc: func() int { return 1 },
	}

	mockChunkDAOSemantic := &mockChunkDAOSemantic{
		searchVectorFunc: func(_ context.Context, _ []float32, _ int, domain string) ([]dao.Chunk, error) {
			capturedSemanticDomain = domain
			return []dao.Chunk{{ID: 1, ChunkText: "test", DocID: 1, Score: 0.9}}, nil
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

	testDomain := "engineering"
	_, err := hs.HybridSearch(context.Background(), "test", 10, testDomain)
	if err != nil {
		t.Fatalf("HybridSearch() error = %v", err)
	}

	if capturedLexicalDomain != testDomain {
		t.Errorf("lexical subsearch domain = %q, want %q", capturedLexicalDomain, testDomain)
	}
	if capturedSemanticDomain != testDomain {
		t.Errorf("semantic subsearch domain = %q, want %q", capturedSemanticDomain, testDomain)
	}
}
