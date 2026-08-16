// Package search (hybrid) orchestrates parallel lexical + semantic search with RRF fusion.
package search

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/graph"
	"github.com/devmix/synopsis/internal/utils"
)

// hybridSearcher implements the Searcher interface with parallel execution and RRF fusion.
type hybridSearcher struct {
	cfg config.SearchConfig

	lexical  *lexicalSearcher
	semantic *semanticSearcher
	enricher *Enricher
	expander *GraphExpander
	reranker *Reranker
}

// SetGraph swaps the knowledge graph used for entity expansion. It is a no-op
// when graph expansion is disabled (expander is nil).
func (hs *hybridSearcher) SetGraph(g *graph.Graph) {
	if hs.expander != nil {
		hs.expander.SetGraph(g)
	}
}

// finalize runs the common post-search pipeline: Enrich → Rerank → truncate → GraphExpand.
// Domain filtering is handled at DB level by sub-searches, so no application-level filter here.
// Expander errors are non-fatal (logged as warnings; results returned without graph context).
func (hs *hybridSearcher) finalize(ctx context.Context, results []SearchResult, topK int) ([]SearchResult, error) {
	if len(results) == 0 {
		return results, nil
	}

	// Enrich with metadata and entities.
	enriched, err := hs.enricher.Enrich(ctx, results)
	if err != nil {
		return nil, fmt.Errorf("enrich search results: %w", err)
	}

	// Rerank results with business rules, freshness, and authority BEFORE truncation.
	// This ensures the best-ranked results survive truncation.
	if hs.reranker != nil && len(enriched) > 0 {
		enriched = hs.reranker.Rerank(enriched)
	}

	// Truncate to topK after reranking. Domain filtering is done at DB level by sub-searches.
	if len(enriched) > topK {
		enriched = enriched[:topK]
	}

	// Graph context expansion (if enabled and entities found).
	if hs.expander != nil && len(enriched) > 0 {
		enriched, err = hs.expander.Expand(ctx, enriched)
		if err != nil {
			// Non-fatal: log but continue with enriched results.
			slog.WarnContext(ctx, "graph expansion failed", "error", err)
		}
	}

	return enriched, nil
}

// filterByDomain filters search results to only include those matching the given domain.
// Domains are stored in result.Metadata["domains"] as []interface{}, []string, or string.
// Comparisons are case-insensitive (domains are normalized via utils.Normalize).
func filterByDomain(results []SearchResult, domain string) []SearchResult {
	if domain == "" {
		return results
	}

	filtered := make([]SearchResult, 0, len(results))
	domainLower := utils.Normalize(domain)

	for _, r := range results {
		if r.Metadata == nil {
			continue
		}

		if domains, ok := r.Metadata["domains"]; ok {
			switch d := domains.(type) {
			case []string:
				for _, dm := range d {
					if utils.Normalize(dm) == domainLower {
						filtered = append(filtered, r)
						break
					}
				}
			case []interface{}:
				for _, dm := range d {
					if dmStr, ok := dm.(string); ok {
						if utils.Normalize(dmStr) == domainLower {
							filtered = append(filtered, r)
							break
						}
					}
				}
			case string:
				if utils.Normalize(d) == domainLower {
					filtered = append(filtered, r)
				}
			}
		}
	}

	return filtered
}

// invertScore converts a "lower-is-better" score (BM25, cosine distance) to
// "higher-is-better" using reciprocal. A score of 0 becomes math.MaxFloat64.
func invertScore(score float64) float64 {
	if score == 0 {
		return math.MaxFloat64
	}
	return 1.0 / score
}

// HybridSearch executes both lexical and semantic searches in parallel,
// fuses results with RRF over the full pool (max(LexicalTopK, SemanticTopK)),
// enriches them, truncates to topK, reranks, and expands entities.
// Domain filtering is handled at DB level by sub-searches.
func (hs *hybridSearcher) HybridSearch(ctx context.Context, query string, topK int, domain string) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	if topK <= 0 {
		topK = hs.cfg.FinalTopK
	}

	timeout := time.Duration(hs.cfg.TimeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var lexicalResults []LexicalSearchResult
	var semanticResults []SemanticSearchResult
	var lexErr, semErr error

	type result struct {
		results []LexicalSearchResult
		err     error
	}
	type sresult struct {
		results []SemanticSearchResult
		err     error
	}

	lexCh := make(chan result, 1)
	semCh := make(chan sresult, 1)

	// Run lexical search in a goroutine.
	go func() {
		var res []LexicalSearchResult
		var err error
		if hs.cfg.EnableLexical {
			res, err = hs.lexical.Search(ctx, query, hs.cfg.LexicalTopK, domain)
		}
		lexCh <- result{results: res, err: err}
	}()

	// Run semantic search in a goroutine.
	go func() {
		var res []SemanticSearchResult
		var err error
		if hs.cfg.EnableSemantic {
			res, err = hs.semantic.Search(ctx, query, hs.cfg.SemanticTopK, domain)
		}
		semCh <- sresult{results: res, err: err}
	}()

	// Collect results with timeout.
	lexDone := false
	semDone := false
	for !lexDone || !semDone {
		select {
		case r := <-lexCh:
			lexicalResults = r.results
			lexErr = r.err
			lexDone = true
		case sr := <-semCh:
			semanticResults = sr.results
			semErr = sr.err
			semDone = true
		case <-ctx.Done():
			return nil, fmt.Errorf("hybrid search timed out: %w", ctx.Err())
		}
	}

	// If both failed, return an error.
	if lexErr != nil && semErr != nil {
		return nil, fmt.Errorf("both lexical (%v) and semantic (%v) searches failed", lexErr, semErr)
	}

	// If one failed but the other succeeded, continue with available results.
	if lexErr != nil {
		lexicalResults = nil
	}
	if semErr != nil {
		semanticResults = nil
	}

	// Fuse results with RRF over full pool (max of lexical/semantic topK).
	fusionPool := hs.cfg.LexicalTopK
	if hs.cfg.SemanticTopK > fusionPool {
		fusionPool = hs.cfg.SemanticTopK
	}
	fused := ReciprocalRankFusion(lexicalResults, semanticResults, hs.cfg.RRFK, fusionPool)

	return hs.finalize(ctx, fused, topK)
}

// LexicalSearch performs only FTS5/BM25 keyword search.
func (hs *hybridSearcher) LexicalSearch(ctx context.Context, query string, topK int, domain string) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	if topK <= 0 {
		topK = hs.cfg.LexicalTopK
	}

	timeout := time.Duration(hs.cfg.TimeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	results, err := hs.lexical.Search(ctx, query, topK, domain)
	if err != nil {
		return nil, fmt.Errorf("lexical search: %w", err)
	}

		searchResults := make([]SearchResult, 0, len(results))
	for i, r := range results {
		searchResults = append(searchResults, SearchResult{
			ChunkID:    r.ChunkID,
			ChunkText:  r.ChunkText,
			DocumentID: r.DocumentID,
			Score:      invertScore(r.Score),
			Rank:       i + 1,
			SourceType: "lexical",
			Metadata:   make(map[string]interface{}),
		})
	}

	return hs.finalize(ctx, searchResults, topK)
}

// SemanticSearch performs only vector similarity search via sqlite-vec.
func (hs *hybridSearcher) SemanticSearch(ctx context.Context, query string, topK int, domain string) ([]SearchResult, error) {
	if query == "" {
		return nil, nil
	}

	if topK <= 0 {
		topK = hs.cfg.SemanticTopK
	}

	timeout := time.Duration(hs.cfg.TimeoutMs) * time.Millisecond
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	results, err := hs.semantic.Search(ctx, query, topK, domain)
	if err != nil {
		return nil, fmt.Errorf("semantic search: %w", err)
	}

		searchResults := make([]SearchResult, 0, len(results))
	for i, r := range results {
		searchResults = append(searchResults, SearchResult{
			ChunkID:    r.ChunkID,
			ChunkText:  r.ChunkText,
			DocumentID: r.DocumentID,
			Score:      invertScore(r.Score),
			Rank:       i + 1,
			SourceType: "semantic",
			Metadata:   make(map[string]interface{}),
		})
	}

	return hs.finalize(ctx, searchResults, topK)
}
