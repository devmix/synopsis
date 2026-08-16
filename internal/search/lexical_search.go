// Package search (lexical) implements FTS5/BM25 keyword search over chunk text.
package search

import (
	"context"
	"fmt"

	"github.com/devmix/synopsis/internal/database/dao"
)

// LexicalSearchResult holds a raw FTS5 hit before enrichment.
type LexicalSearchResult struct {
	ChunkID       int     `json:"chunk_id"`
	ChunkText     string  `json:"chunk_text"`
	DocumentID    int     `json:"document_id"`
	SequenceNum   int     `json:"sequence_num"`
	StartOffset   *int    `json:"start_offset,omitempty"`
	EndOffset     *int    `json:"end_offset,omitempty"`
	Score         float64 `json:"score"` // BM25 score (lower is better in SQLite)
}

// lexicalSearcher performs FTS5 keyword search.
type lexicalSearcher struct {
	chunkDAO chunkDAOLexical
}

// chunkDAOLexical abstracts the ChunkDAO for lexical search so we can test without a real DB.
type chunkDAOLexical interface {
	SearchFTS(ctx context.Context, query string, limit int, domain string) ([]dao.Chunk, error)
}

func newLexicalSearcher(chunkDAO chunkDAOLexical) *lexicalSearcher {
	return &lexicalSearcher{chunkDAO: chunkDAO}
}

// Search executes an FTS5 query and returns ranked results.
// It handles FTS5 syntax (direct, phrase, prefix, combined) transparently.
// When domain is non-empty, only chunks from documents in that domain are returned.
func (ls *lexicalSearcher) Search(ctx context.Context, query string, topK int, domain string) ([]LexicalSearchResult, error) {
	if query == "" {
		return nil, nil
	}

	chunks, err := ls.chunkDAO.SearchFTS(ctx, query, topK, domain)
	if err != nil {
		return nil, fmt.Errorf("lexical FTS5 search: %w", err)
	}

	results := make([]LexicalSearchResult, 0, len(chunks))
	for _, c := range chunks {
		results = append(results, LexicalSearchResult{
			ChunkID:     c.ID,
			ChunkText:   c.ChunkText,
			DocumentID:  c.DocID,
			SequenceNum: c.SequenceNum,
			StartOffset: c.StartOffset,
			EndOffset:   c.EndOffset,
			Score:       c.Score, // BM25 score from FTS5 (lower is better)
		})
	}

	return results, nil
}
