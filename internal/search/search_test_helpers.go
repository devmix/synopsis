package search

import (
	"context"

	"github.com/devmix/synopsis/internal/database/dao"
)

// --- Mock types for lexical chunk DAO ---

type mockChunkDAOLexical struct {
	searchFTSFunc func(ctx context.Context, query string, limit int, domain string) ([]dao.Chunk, error)
}

func (m *mockChunkDAOLexical) SearchFTS(ctx context.Context, query string, limit int, domain string) ([]dao.Chunk, error) {
	if m.searchFTSFunc != nil {
		return m.searchFTSFunc(ctx, query, limit, domain)
	}
	return nil, nil
}

// --- Mock types for semantic chunk DAO ---

type mockChunkDAOSemantic struct {
	searchVectorFunc func(ctx context.Context, vector []float32, topK int, domain string) ([]dao.Chunk, error)
}

func (m *mockChunkDAOSemantic) SearchVector(ctx context.Context, vector []float32, topK int, domain string) ([]dao.Chunk, error) {
	if m.searchVectorFunc != nil {
		return m.searchVectorFunc(ctx, vector, topK, domain)
	}
	return nil, nil
}

// --- Mock types for embedding provider ---

type mockEmbeddingProvider struct {
	generateFunc  func(ctx context.Context, texts []string) ([][]float32, error)
	vectorDimFunc func() int
}

func (m *mockEmbeddingProvider) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, texts)
	}
	return nil, nil
}

func (m *mockEmbeddingProvider) VectorDim() int {
	if m.vectorDimFunc != nil {
		return m.vectorDimFunc()
	}
	return 0
}

// --- Mock types for enricher tests ---

type mockDocDAOEnricher struct {
	getByIDsFunc  func(ctx context.Context, ids []int) (map[int]*dao.Document, error)
	getByIDsCalls int
	lastIDs       []int
}

func (m *mockDocDAOEnricher) GetByIDs(ctx context.Context, ids []int) (map[int]*dao.Document, error) {
	m.getByIDsCalls++
	m.lastIDs = append([]int(nil), ids...)
	if m.getByIDsFunc != nil {
		return m.getByIDsFunc(ctx, ids)
	}
	return make(map[int]*dao.Document), nil
}

type mockChunkEntityDAOEnricher struct {
	getEntitiesByChunksFunc func(ctx context.Context, chunkIDs []int) (map[int][]dao.Entity, error)
}

func (m *mockChunkEntityDAOEnricher) GetEntitiesByChunks(ctx context.Context, chunkIDs []int) (map[int][]dao.Entity, error) {
	if m.getEntitiesByChunksFunc != nil {
		return m.getEntitiesByChunksFunc(ctx, chunkIDs)
	}
	return make(map[int][]dao.Entity), nil
}
