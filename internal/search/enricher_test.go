package search

import (
	"context"
	"testing"
	"time"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
)

// TestEnricherReranker_OfficialBoost verifies the full enricher→reranker chain:
// a document flagged is_official in metadata_json is really boosted above a
// non-official document after enrichment and reranking.
func TestEnricherReranker_OfficialBoost(t *testing.T) {
	t.Parallel()

	officialMeta := `{"is_official": true}`
	plainMeta := `{}`

	docDAO := &mockDocDAOEnricher{
		getByIDsFunc: func(_ context.Context, ids []int) (map[int]*dao.Document, error) {
			return map[int]*dao.Document{
				1: {ID: 1, SourceType: "policy", MetadataJSON: &officialMeta, UpdatedAt: "2026-08-01 12:00:00"},
				2: {ID: 2, SourceType: "tutorial", MetadataJSON: &plainMeta, UpdatedAt: "2026-08-01 12:00:00"},
			}, nil
		},
	}
	chunkEntityDAO := &mockChunkEntityDAOEnricher{}

	enricher := NewEnricher(docDAO, chunkEntityDAO)
	reranker := NewReranker(config.SearchConfig{
		DeprecatedBoost: 0.2,
		OfficialBoost:   1.5,
		RecentBoost:     1.0, // disable freshness boost for this test
		RecentDays:      90,
	})

	results := []SearchResult{
		{ChunkID: 1, DocumentID: 1, Score: 0.5}, // official
		{ChunkID: 2, DocumentID: 2, Score: 0.7}, // non-official
	}

	enriched, err := enricher.Enrich(context.Background(), results)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	// The official flag must be present in the enriched metadata.
	if got, ok := enriched[0].Metadata["is_official"].(bool); !ok || !got {
		t.Errorf("enriched[0].Metadata[\"is_official\"] = %v, want true", enriched[0].Metadata["is_official"])
	}

	reranked := reranker.Rerank(enriched)

	// Official: 0.5 * 1.5 = 0.75 > non-official 0.7 → official must rank first.
	if len(reranked) != 2 {
		t.Fatalf("Rerank() length = %d, want 2", len(reranked))
	}
	if reranked[0].DocumentID != 1 {
		t.Errorf("expected official document (id=1) first, got id=%d (score=%f)", reranked[0].DocumentID, reranked[0].Score)
	}
	if reranked[0].Rank != 1 {
		t.Errorf("expected rank 1 for official document, got %d", reranked[0].Rank)
	}
}

// TestEnricher_NormalizesUpdatedAt verifies that SQLite's CURRENT_TIMESTAMP
// format ("2006-01-02 15:04:05") is normalized to RFC3339 so the reranker's
// freshness boost can parse it and actually apply.
func TestEnricher_NormalizesUpdatedAt(t *testing.T) {
	t.Parallel()

	recentSQLite := time.Now().AddDate(0, 0, -1).Format("2006-01-02 15:04:05")
	oldSQLite := time.Now().AddDate(0, 0, -100).Format("2006-01-02 15:04:05")

	docDAO := &mockDocDAOEnricher{
		getByIDsFunc: func(_ context.Context, ids []int) (map[int]*dao.Document, error) {
			return map[int]*dao.Document{
				1: {ID: 1, SourceType: "policy", UpdatedAt: recentSQLite},
				2: {ID: 2, SourceType: "policy", UpdatedAt: oldSQLite},
			}, nil
		},
	}
	chunkEntityDAO := &mockChunkEntityDAOEnricher{}

	enricher := NewEnricher(docDAO, chunkEntityDAO)
	reranker := NewReranker(config.SearchConfig{
		DeprecatedBoost: 1.0,
		OfficialBoost:   1.0,
		RecentBoost:     1.2,
		RecentDays:      90,
	})

	results := []SearchResult{
		{ChunkID: 1, DocumentID: 1, Score: 1.0}, // recent
		{ChunkID: 2, DocumentID: 2, Score: 1.0}, // old
	}

	enriched, err := enricher.Enrich(context.Background(), results)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	// Both updated_at values must be RFC3339 after normalization.
	for i, r := range enriched {
		got, ok := r.Metadata["updated_at"].(string)
		if !ok {
			t.Fatalf("enriched[%d].Metadata[\"updated_at\"] missing or not a string: %v", i, r.Metadata["updated_at"])
		}
		if _, err := time.Parse(time.RFC3339, got); err != nil {
			t.Errorf("enriched[%d].updated_at = %q, not RFC3339: %v", i, got, err)
		}
	}

	reranked := reranker.Rerank(enriched)

	// Recent doc (1.0 * 1.2 = 1.2) must outrank the old one (1.0).
	if reranked[0].DocumentID != 1 {
		t.Errorf("expected recent document (id=1) first, got id=%d (score=%f)", reranked[0].DocumentID, reranked[0].Score)
	}
	if reranked[0].Score != 1.2 {
		t.Errorf("recent document score = %f, want 1.2 (freshness boost applied)", reranked[0].Score)
	}
}

// TestEnricher_InvalidUpdatedAtSkipped verifies that unparseable updated_at
// values are skipped (key absent) instead of failing enrichment.
func TestEnricher_InvalidUpdatedAtSkipped(t *testing.T) {
	t.Parallel()

	docDAO := &mockDocDAOEnricher{
		getByIDsFunc: func(_ context.Context, ids []int) (map[int]*dao.Document, error) {
			return map[int]*dao.Document{
				1: {ID: 1, SourceType: "policy", UpdatedAt: "not-a-date"},
				2: {ID: 2, SourceType: "policy", UpdatedAt: ""},
			}, nil
		},
	}
	chunkEntityDAO := &mockChunkEntityDAOEnricher{}

	enricher := NewEnricher(docDAO, chunkEntityDAO)

	results := []SearchResult{
		{ChunkID: 1, DocumentID: 1, Score: 1.0},
		{ChunkID: 2, DocumentID: 2, Score: 1.0},
	}

	enriched, err := enricher.Enrich(context.Background(), results)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	for i, r := range enriched {
		if _, ok := r.Metadata["updated_at"]; ok {
			t.Errorf("enriched[%d].Metadata[\"updated_at\"] should be skipped, got %v", i, r.Metadata["updated_at"])
		}
	}
}

// TestEnricher_MetadataFlags verifies extraction of reranker-relevant keys
// (is_deprecated, is_official, valid_to) from document metadata_json.
func TestEnricher_MetadataFlags(t *testing.T) {
	t.Parallel()

	meta := `{"is_official": true, "is_deprecated": false, "valid_to": "2026-12-31T00:00:00Z", "domain": "combat"}`
	badTypes := `{"is_official": "yes", "is_deprecated": 1, "valid_to": ""}`
	empty := `{}`

	docDAO := &mockDocDAOEnricher{
		getByIDsFunc: func(_ context.Context, ids []int) (map[int]*dao.Document, error) {
			return map[int]*dao.Document{
				1: {ID: 1, SourceType: "policy", MetadataJSON: &meta},
				2: {ID: 2, SourceType: "policy", MetadataJSON: &badTypes},
				3: {ID: 3, SourceType: "policy", MetadataJSON: &empty},
			}, nil
		},
	}
	chunkEntityDAO := &mockChunkEntityDAOEnricher{}

	enricher := NewEnricher(docDAO, chunkEntityDAO)

	results := []SearchResult{
		{ChunkID: 1, DocumentID: 1, Score: 1.0},
		{ChunkID: 2, DocumentID: 2, Score: 1.0},
		{ChunkID: 3, DocumentID: 3, Score: 1.0},
	}

	enriched, err := enricher.Enrich(context.Background(), results)
	if err != nil {
		t.Fatalf("Enrich() error = %v", err)
	}

	// Doc 1: all flags extracted.
	if got, ok := enriched[0].Metadata["is_official"].(bool); !ok || !got {
		t.Errorf("doc 1 is_official = %v, want true", enriched[0].Metadata["is_official"])
	}
	if got, ok := enriched[0].Metadata["is_deprecated"].(bool); !ok || got {
		t.Errorf("doc 1 is_deprecated = %v, want false", enriched[0].Metadata["is_deprecated"])
	}
	if got, ok := enriched[0].Metadata["valid_to"].(string); !ok || got != "2026-12-31T00:00:00Z" {
		t.Errorf("doc 1 valid_to = %v, want 2026-12-31T00:00:00Z", enriched[0].Metadata["valid_to"])
	}
	if got, ok := enriched[0].Metadata["domains"].([]string); !ok || len(got) != 1 || got[0] != "combat" {
		t.Errorf("doc 1 domains = %v, want [combat]", enriched[0].Metadata["domains"])
	}

	// Doc 2: wrong-typed values must be skipped.
	for _, key := range []string{"is_official", "is_deprecated", "valid_to"} {
		if _, ok := enriched[1].Metadata[key]; ok {
			t.Errorf("doc 2 Metadata[%q] should be skipped, got %v", key, enriched[1].Metadata[key])
		}
	}

	// Doc 3: missing keys must not be set.
	for _, key := range []string{"is_official", "is_deprecated", "valid_to"} {
		if _, ok := enriched[2].Metadata[key]; ok {
			t.Errorf("doc 3 Metadata[%q] should be absent, got %v", key, enriched[2].Metadata[key])
		}
	}
}

// TestEnricher_BatchGetByIDs_SingleQuery is the FIX-037 spy test: enriching
// 20 results must issue exactly one document query (GetByIDs), not 20 GetByID
// calls. Duplicate document IDs must be deduplicated into a single query.
func TestEnricher_BatchGetByIDs_SingleQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		results   []SearchResult
		wantCalls int
		wantIDs   int
	}{
		{
			name: "20 results across 20 distinct documents",
			results: func() []SearchResult {
				results := make([]SearchResult, 20)
				for i := range results {
					results[i] = SearchResult{ChunkID: i + 1, DocumentID: i + 1, Score: 1.0}
				}
				return results
			}(),
			wantCalls: 1,
			wantIDs:   20,
		},
		{
			name: "20 results across 5 distinct documents (duplicates deduplicated)",
			results: func() []SearchResult {
				results := make([]SearchResult, 20)
				for i := range results {
					results[i] = SearchResult{ChunkID: i + 1, DocumentID: i%5 + 1, Score: 1.0}
				}
				return results
			}(),
			wantCalls: 1,
			wantIDs:   5,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			docDAO := &mockDocDAOEnricher{
				getByIDsFunc: func(_ context.Context, ids []int) (map[int]*dao.Document, error) {
					docs := make(map[int]*dao.Document, len(ids))
					for _, id := range ids {
						docs[id] = &dao.Document{ID: id, SourceType: "policy", UpdatedAt: "2026-08-01 12:00:00"}
					}
					return docs, nil
				},
			}
			chunkEntityDAO := &mockChunkEntityDAOEnricher{}

			enricher := NewEnricher(docDAO, chunkEntityDAO)

			enriched, err := enricher.Enrich(context.Background(), tt.results)
			if err != nil {
				t.Fatalf("Enrich() error = %v", err)
			}
			if len(enriched) != len(tt.results) {
				t.Fatalf("Enrich() length = %d, want %d", len(enriched), len(tt.results))
			}

			if docDAO.getByIDsCalls != tt.wantCalls {
				t.Errorf("GetByIDs calls = %d, want %d", docDAO.getByIDsCalls, tt.wantCalls)
			}
			if len(docDAO.lastIDs) != tt.wantIDs {
				t.Errorf("GetByIDs received %d ids, want %d (deduplicated)", len(docDAO.lastIDs), tt.wantIDs)
			}
		})
	}
}
