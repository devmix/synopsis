// Package search (semantic) implements vector similarity search via sqlite-vec.
package search

import (
	"context"
	"fmt"

	"github.com/devmix/synopsis/internal/database/dao"
)

// SemanticSearchResult holds a raw vector hit before enrichment.
type SemanticSearchResult struct {
	ChunkID       int     `json:"chunk_id"`
	ChunkText     string  `json:"chunk_text"`
	DocumentID    int     `json:"document_id"`
	SequenceNum   int     `json:"sequence_num"`
	StartOffset   *int    `json:"start_offset,omitempty"`
	EndOffset     *int    `json:"end_offset,omitempty"`
	Score         float64 `json:"score"` // cosine distance (lower is better)
}

// semanticSearcher performs vector similarity search using sqlite-vec.
type semanticSearcher struct {
	chunkDAO  chunkDAOSemantic
	provider  embeddingProvider
	vectorDim int
}

// chunkDAOSemantic abstracts the vector search over chunks for testing.
type chunkDAOSemantic interface {
	SearchVector(ctx context.Context, vector []float32, topK int, domain string) ([]dao.Chunk, error)
}

// embeddingProvider abstracts the embedding provider for testing.
type embeddingProvider interface {
	GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error)
	VectorDim() int
}

func newSemanticSearcher(chunkDAO chunkDAOSemantic, provider embeddingProvider) *semanticSearcher {
	return &semanticSearcher{
		chunkDAO:  chunkDAO,
		provider:  provider,
		vectorDim: provider.VectorDim(),
	}
}

// Search generates an embedding for the query and finds nearest chunks via sqlite-vec.
// When domain is non-empty, only chunks from documents in that domain are returned.
func (ss *semanticSearcher) Search(ctx context.Context, query string, topK int, domain string) ([]SemanticSearchResult, error) {
	if query == "" {
		return nil, nil
	}

	embeddings, err := ss.provider.GenerateEmbeddings(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("generate embedding for query: %w", err)
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return nil, fmt.Errorf("empty embedding generated")
	}

	chunks, err := ss.chunkDAO.SearchVector(ctx, embeddings[0], topK, domain)
	if err != nil {
		return nil, fmt.Errorf("semantic vector search: %w", err)
	}

	results := make([]SemanticSearchResult, 0, len(chunks))
	for _, c := range chunks {
		results = append(results, SemanticSearchResult{
			ChunkID:     c.ID,
			ChunkText:   c.ChunkText,
			DocumentID:  c.DocID,
			SequenceNum: c.SequenceNum,
			StartOffset: c.StartOffset,
			EndOffset:   c.EndOffset,
			Score:       c.Score,
		})
	}

	return results, nil
}
