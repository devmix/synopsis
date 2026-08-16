// Package search implements hybrid search combining lexical (FTS5/BM25) and semantic
// (sqlite-vec/cosine) results via Reciprocal Rank Fusion.
package search

import (
	"context"
	"database/sql"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/embedding"
	"github.com/devmix/synopsis/internal/graph"
)

// SearchResult is a single ranked hit returned by any search method.
type SearchResult struct {
	ChunkID       int                    `json:"chunk_id"`
	ChunkText     string                 `json:"chunk_text"`
	DocumentID    int                    `json:"document_id"`
	SequenceNum   int                    `json:"sequence_num"`
	StartOffset   *int                   `json:"start_offset,omitempty"`
	EndOffset     *int                   `json:"end_offset,omitempty"`
	DocumentPath  string                 `json:"document_path"`
	Score         float64                `json:"score"` // normalized; higher is better (inverted for BM25/cosine)
	Rank          int                    `json:"rank"`
	SourceType    string                 `json:"source_type"` // "lexical", "semantic" or "hybrid"
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	Entities      []dao.Entity           `json:"entities,omitempty"`
}

// Searcher provides lexical, semantic and hybrid search over the knowledge base.
type Searcher interface {
	// HybridSearch executes both lexical and semantic searches in parallel,
	// fuses results with RRF, enriches them, optionally filters by domain,
	// and returns top-K hits. Domain filtering happens before truncation so
	// up to topK results are returned within the specified domain.
	HybridSearch(ctx context.Context, query string, topK int, domain string) ([]SearchResult, error)

	// LexicalSearch performs only FTS5/BM25 keyword search.
	LexicalSearch(ctx context.Context, query string, topK int, domain string) ([]SearchResult, error)

	// SemanticSearch performs only vector similarity search via sqlite-vec.
	SemanticSearch(ctx context.Context, query string, topK int, domain string) ([]SearchResult, error)
}

// NewSearcher creates a fully configured hybrid searcher.
// The db parameter is the raw *sql.DB used for vector search queries;
// chunkDAO is used for FTS5 lexical search through its SearchFTS method.
func NewSearcher(
	db *sql.DB,
	chunkDAO *dao.ChunkDAO,
	docDAO *dao.DocumentDAO,
	chunkEntityDAO *dao.ChunkEntityDAO,
	provider embedding.Provider,
	cfg config.SearchConfig,
	graphCfg config.GraphConfig,
	g *graph.Graph,
) Searcher {
	hs := &hybridSearcher{
		cfg:      cfg,
		lexical:  newLexicalSearcher(chunkDAO),
		semantic: newSemanticSearcher(chunkDAO, provider),
		enricher: NewEnricher(
			docDAO,
			chunkEntityDAO,
		),
	}

	// Initialize graph expander if enabled.
	if graphCfg.EnableGraph && g != nil {
		hs.expander = NewGraphExpander(db, g, graphCfg)
	}

	// Initialize reranker with boost factors from the search config.
	hs.reranker = NewReranker(cfg)

	return hs
}
