// Package search (enricher) adds document metadata and entities to search results.
package search

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/devmix/synopsis/internal/database/dao"
)

// Enricher adds document-level metadata and entity information to search results.
type Enricher struct {
	docDAO         docDAOEnricher
	chunkEntityDAO chunkEntityDAOEnricher
}

// docDAOEnricher abstracts DocumentDAO for enrichment.
type docDAOEnricher interface {
	GetByIDs(ctx context.Context, ids []int) (map[int]*dao.Document, error)
}

// chunkEntityDAOEnricher abstracts ChunkEntityDAO for enrichment.
type chunkEntityDAOEnricher interface {
	GetEntitiesByChunks(ctx context.Context, chunkIDs []int) (map[int][]dao.Entity, error)
}

// NewEnricher creates an Enricher bound to the given DAOs.
func NewEnricher(
	docDAO docDAOEnricher,
	chunkEntityDAO chunkEntityDAOEnricher,
) *Enricher {
	return &Enricher{
		docDAO:         docDAO,
		chunkEntityDAO: chunkEntityDAO,
	}
}

// Enrich adds document metadata and entities to each result in-place.
func (e *Enricher) Enrich(ctx context.Context, results []SearchResult) ([]SearchResult, error) {
	if len(results) == 0 {
		return results, nil
	}

	// Collect unique document IDs for batch fetching.
	docIDs := make([]int, 0, len(results))
	seen := make(map[int]bool, len(results))
	for _, r := range results {
		if !seen[r.DocumentID] {
			seen[r.DocumentID] = true
			docIDs = append(docIDs, r.DocumentID)
		}
	}

	// Fetch all documents in a single batch query.
	docs, err := e.docDAO.GetByIDs(ctx, docIDs)
	if err != nil {
		return nil, fmt.Errorf("enrich documents batch: %w", err)
	}

	// Collect unique chunk IDs for batch entity lookup.
	chunkIDs := make([]int, 0, len(results))
	for _, r := range results {
		chunkIDs = append(chunkIDs, r.ChunkID)
	}

	// Batch fetch all entities for all chunks in a single query.
	entityMap, err := e.chunkEntityDAO.GetEntitiesByChunks(ctx, chunkIDs)
	if err != nil {
		return nil, fmt.Errorf("enrich entities batch: %w", err)
	}

	// Enrich each result.
	for i := range results {
		r := &results[i]

		// Add document metadata.
		if r.Metadata == nil {
			r.Metadata = make(map[string]interface{})
		}

		if doc, ok := docs[r.DocumentID]; ok {
			r.DocumentPath = doc.OriginalPath
			r.SourceType = mergeSourceType(r.SourceType, doc.SourceType)
			r.Metadata["document_source_type"] = doc.SourceType

			// Normalize updated_at to RFC3339 so the reranker's freshness boost
			// can parse it (SQLite CURRENT_TIMESTAMP yields "2006-01-02 15:04:05").
			if updatedAt, ok := normalizeUpdatedAt(doc.UpdatedAt); ok {
				r.Metadata["updated_at"] = updatedAt
			}

			if doc.MetadataJSON != nil {
				r.Metadata["document_metadata_json"] = *doc.MetadataJSON

				// Extract and expose domains from metadata JSON as []string.
				var meta map[string]interface{}
				if err := json.Unmarshal([]byte(*doc.MetadataJSON), &meta); err == nil {
					if domains, ok := meta["domain"]; ok {
						r.Metadata["domains"] = normalizeDomains(domains)
					}
					extractRerankerFlags(meta, r)
				}
			}
		}

		// Attach entities from batch lookup.
		if ents, ok := entityMap[r.ChunkID]; ok {
			r.Entities = ents
		}
	}

	return results, nil
}

// mergeSourceType combines the search source type with document source type.
func mergeSourceType(searchSource, docSource string) string {
	if searchSource == "" {
		return docSource
	}
	if docSource == "" {
		return searchSource
	}
	return searchSource + "+" + docSource
}

// sqliteTimeLayout is the format produced by SQLite's CURRENT_TIMESTAMP.
const sqliteTimeLayout = "2006-01-02 15:04:05"

// normalizeUpdatedAt converts a document's updated_at value to RFC3339.
// It accepts RFC3339 (pass-through), the SQLite CURRENT_TIMESTAMP format
// ("2006-01-02 15:04:05", UTC) and the same format with fractional seconds.
// Returns (value, false) for empty or unparseable inputs so the caller can
// skip the key instead of failing.
func normalizeUpdatedAt(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t.Format(time.RFC3339), true
	}
	if t, err := time.Parse(sqliteTimeLayout, value); err == nil {
		return t.Format(time.RFC3339), true
	}
	if t, err := time.Parse(sqliteTimeLayout+".999999999", value); err == nil {
		return t.Format(time.RFC3339), true
	}
	return "", false
}

// extractRerankerFlags copies the reranker-relevant keys (is_deprecated,
// is_official, valid_to) from document metadata JSON into the result metadata.
// Missing or wrong-typed keys are skipped — the reranker ignores absent keys.
func extractRerankerFlags(meta map[string]interface{}, result *SearchResult) {
	if v, ok := meta["is_deprecated"].(bool); ok {
		result.Metadata["is_deprecated"] = v
	}
	if v, ok := meta["is_official"].(bool); ok {
		result.Metadata["is_official"] = v
	}
	if v, ok := meta["valid_to"].(string); ok && v != "" {
		result.Metadata["valid_to"] = v
	}
}

// normalizeDomains converts a domain value from JSON metadata to []string.
// Handles string (single domain), []interface{} (JSON array of strings),
// and []string (already normalized). Returns nil for unsupported types.
func normalizeDomains(v interface{}) []string {
	switch d := v.(type) {
	case string:
		if d == "" {
			return nil
		}
		return []string{d}
	case []interface{}:
		domains := make([]string, 0, len(d))
		for _, item := range d {
			if s, ok := item.(string); ok && s != "" {
				domains = append(domains, s)
			}
		}
		return domains
	case []string:
		filtered := make([]string, 0, len(d))
		for _, s := range d {
			if s != "" {
				filtered = append(filtered, s)
			}
		}
		return filtered
	default:
		return nil
	}
}
