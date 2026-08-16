// Package search (reranker) applies business rules, freshness boost, and authority boost
// to hybrid search results after RRF fusion.
package search

import (
	"sort"
	"time"

	"github.com/devmix/synopsis/internal/config"
)

// Reranker applies business rules, freshness boost, and authority boost
// to hybrid search results after RRF fusion.
type Reranker struct {
	// Configurable boost factors.
	DeprecatedBoost float64 // default 0.2
	OfficialBoost   float64 // default 1.5
	RecentBoost     float64 // default 1.2 (for documents updated in last 90 days)
	AuthorityBoost  map[string]float64 // document type → boost factor

	// Thresholds.
	RecentDays int // documents updated within this many days get freshness boost (default 90)
}

// NewReranker creates a reranker with default boost factors, optionally
// overridden by the given search config. Zero-valued config fields keep their
// defaults. The variadic parameter allows callers without a config to keep
// using the defaults.
func NewReranker(cfg ...config.SearchConfig) *Reranker {
	r := &Reranker{
		DeprecatedBoost: 0.2,
		OfficialBoost:   1.5,
		RecentBoost:     1.2,
		AuthorityBoost:  make(map[string]float64),
		RecentDays:      90,
	}

	if len(cfg) > 0 {
		c := cfg[0]
		if c.DeprecatedBoost > 0 {
			r.DeprecatedBoost = c.DeprecatedBoost
		}
		if c.OfficialBoost > 0 {
			r.OfficialBoost = c.OfficialBoost
		}
		if c.RecentBoost > 0 {
			r.RecentBoost = c.RecentBoost
		}
		if c.RecentDays > 0 {
			r.RecentDays = c.RecentDays
		}
		if c.AuthorityBoost != nil {
			r.AuthorityBoost = c.AuthorityBoost
		}
	}

	return r
}

// Rerank applies all boosting rules to search results, preserving RRF order with adjustments.
// Returns reordered results.
func (r *Reranker) Rerank(results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}

	// Apply all boosts in sequence.
	results = r.ApplyBusinessRules(results)
	results = r.ApplyFreshnessBoost(results)
	results = r.ApplyAuthorityBoost(results)

	// Re-sort by score descending.
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Re-assign ranks after re-sorting.
	for i := range results {
		results[i].Rank = i + 1
	}

	return results
}

// ApplyBusinessRules removes or downgrades results based on document status.
// Rules:
//   - deprecated documents: score *= 0.2
//   - official policies: score *= 1.5
//   - expired documents (valid_to < today): score *= 0.1
func (r *Reranker) ApplyBusinessRules(results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}

	for i := range results {
		result := &results[i]
		factor := r.boostFactor(*result)
		result.Score *= factor
	}

	return results
}

// ApplyFreshnessBoost boosts scores for recently updated documents.
// Documents updated within RecentDays get score *= RecentBoost.
func (r *Reranker) ApplyFreshnessBoost(results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}

	recentThreshold := time.Now().AddDate(0, 0, -r.RecentDays)

	for i := range results {
		result := &results[i]
		if result.Metadata == nil {
			continue
		}

		// Check for updated_at timestamp in metadata.
		if updatedAt, ok := result.Metadata["updated_at"].(string); ok {
			if parsedTime, err := time.Parse(time.RFC3339, updatedAt); err == nil {
				if parsedTime.After(recentThreshold) {
					result.Score *= r.RecentBoost
				}
			}
		}
	}

	return results
}

// ApplyAuthorityBoost boosts scores based on document type authority.
// AuthorityBoost map defines per-type boost; default is 1.0.
func (r *Reranker) ApplyAuthorityBoost(results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}

	for i := range results {
		result := &results[i]
		if result.Metadata == nil {
			continue
		}

		// Check for document_source_type in metadata.
		if sourceType, ok := result.Metadata["document_source_type"].(string); ok {
			if boost, exists := r.AuthorityBoost[sourceType]; exists {
				result.Score *= boost
			}
		}
	}

	return results
}

// boostFactor extracts relevant metadata from a search result for boosting.
// Returns the appropriate multiplier based on document status.
func (r *Reranker) boostFactor(result SearchResult) float64 {
	factor := 1.0

	if result.Metadata == nil {
		return factor
	}

	// Check for deprecated flag.
	if isDeprecated, ok := result.Metadata["is_deprecated"].(bool); ok && isDeprecated {
		factor *= r.DeprecatedBoost
	}

	// Check for official flag.
	if isOfficial, ok := result.Metadata["is_official"].(bool); ok && isOfficial {
		factor *= r.OfficialBoost
	}

	// Check for expiration (valid_to date).
	if validTo, ok := result.Metadata["valid_to"].(string); ok && validTo != "" {
		if parsedTime, err := time.Parse(time.RFC3339, validTo); err == nil {
			if parsedTime.Before(time.Now()) {
				// Expired document: severe penalty.
				factor *= 0.1
			}
		}
	}

	return factor
}
