package dao

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
)

// Chunk represents a text fragment of a document.
type Chunk struct {
	ID          int    `json:"id"`
	DocID       int    `json:"doc_id"`
	ChunkText   string `json:"chunk_text"`
	SequenceNum int    `json:"sequence_num"`
	StartOffset *int   `json:"start_offset,omitempty"`
	EndOffset   *int   `json:"end_offset,omitempty"`
	CreatedAt   string `json:"created_at"`

	// transient

	Score float64 `json:"score,omitempty"` // BM25 score from FTS5 or cosine distance from vec0 (only populated by SearchFTS/SearchVector)
}

// ChunkDAO provides CRUD operations for the chunks table.
type ChunkDAO struct {
	db DBTX
}

// NewChunkDAO creates a new ChunkDAO bound to the given database or transaction.
func NewChunkDAO(db DBTX) *ChunkDAO {
	return &ChunkDAO{db: db}
}

// Create inserts a new chunk and returns its generated Predicate.
func (c *ChunkDAO) Create(ctx context.Context, chunk Chunk) (int, error) {
	query := `INSERT INTO chunks (doc_id, chunk_text, sequence_num, start_offset, end_offset) VALUES (?, ?, ?, ?, ?)`
	result, err := c.db.ExecContext(ctx, query, chunk.DocID, chunk.ChunkText, chunk.SequenceNum, chunk.StartOffset, chunk.EndOffset)
	if err != nil {
		return 0, fmt.Errorf("insert chunk: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return int(id), nil
}

// GetByID retrieves a single chunk by its Predicate.
func (c *ChunkDAO) GetByID(ctx context.Context, id int) (*Chunk, error) {
	query := `SELECT id, doc_id, chunk_text, sequence_num, start_offset, end_offset, created_at FROM chunks WHERE id = ?`
	chunk := &Chunk{}
	err := c.db.QueryRowContext(ctx, query, id).Scan(
		&chunk.ID, &chunk.DocID, &chunk.ChunkText, &chunk.SequenceNum,
		&chunk.StartOffset, &chunk.EndOffset, &chunk.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // not found
	}
	if err != nil {
		return nil, fmt.Errorf("query chunk %d: %w", id, err)
	}
	return chunk, nil
}

// ListByDocID returns all chunks for a document ordered by sequence number.
func (c *ChunkDAO) ListByDocID(ctx context.Context, docID int) ([]Chunk, error) {
	query := `SELECT id, doc_id, chunk_text, sequence_num, start_offset, end_offset, created_at FROM chunks WHERE doc_id = ? ORDER BY sequence_num`
	rows, err := c.db.QueryContext(ctx, query, docID)
	if err != nil {
		return nil, fmt.Errorf("list chunks for doc %d: %w", docID, err)
	}
	defer rows.Close() //nolint:errcheck

	var chunks []Chunk
	for rows.Next() {
		chunk := Chunk{}
		if err := rows.Scan(&chunk.ID, &chunk.DocID, &chunk.ChunkText, &chunk.SequenceNum, &chunk.StartOffset, &chunk.EndOffset, &chunk.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan chunk row: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chunks: %w", err)
	}

	return chunks, nil
}

// Update modifies an existing chunk's text. FTS5 triggers keep the index in sync.
func (c *ChunkDAO) Update(ctx context.Context, chunk Chunk) error {
	query := `UPDATE chunks SET chunk_text = ?, sequence_num = ?, start_offset = ?, end_offset = ? WHERE id = ?`
	result, err := c.db.ExecContext(ctx, query, chunk.ChunkText, chunk.SequenceNum, chunk.StartOffset, chunk.EndOffset, chunk.ID)
	if err != nil {
		return fmt.Errorf("update chunk %d: %w", chunk.ID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check update rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("chunk %d not found", chunk.ID)
	}
	return nil
}

// Delete removes a chunk by Predicate. FTS5 triggers clean up the index automatically.
func (c *ChunkDAO) Delete(ctx context.Context, id int) error {
	result, err := c.db.ExecContext(ctx, "DELETE FROM chunks WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete chunk %d: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check delete rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("chunk %d not found", id)
	}
	return nil
}

// CountByDocID returns the number of chunks for a given document.
func (c *ChunkDAO) CountByDocID(ctx context.Context, docID int) (int, error) {
	var count int
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chunks WHERE doc_id = ?", docID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count chunks for doc %d: %w", docID, err)
	}
	return count, nil
}

// Count returns the total number of chunks.
func (c *ChunkDAO) Count(ctx context.Context) (int, error) {
	var count int
	err := c.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM chunks").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count chunks: %w", err)
	}
	return count, nil
}

// SearchFTS performs a full-text search using the FTS5 index.
// When domain is non-empty, results are filtered by document domain via JOIN
// with documents table and json_each before LIMIT is applied.
func (c *ChunkDAO) SearchFTS(ctx context.Context, query string, limit int, domain string) ([]Chunk, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	var sqlQuery string
	var args []any

	if domain != "" {
		sqlQuery = `SELECT c.id, c.doc_id, c.chunk_text, c.sequence_num, c.start_offset, c.end_offset, c.created_at, bm25(chunks_fts) AS score FROM chunks c INNER JOIN chunks_fts ON chunks_fts.rowid = c.id INNER JOIN documents d ON d.id = c.doc_id WHERE chunks_fts MATCH ? AND EXISTS (SELECT 1 FROM json_each(d.metadata_json, '$.domain') WHERE json_each.value = ? AND d.metadata_json IS NOT NULL AND json_valid(d.metadata_json)) LIMIT ?`
		args = []any{query, domain, limit}
	} else {
		sqlQuery = `SELECT c.id, c.doc_id, c.chunk_text, c.sequence_num, c.start_offset, c.end_offset, c.created_at, bm25(chunks_fts) AS score FROM chunks c INNER JOIN chunks_fts ON chunks_fts.rowid = c.id WHERE chunks_fts MATCH ? LIMIT ?`
		args = []any{query, limit}
	}

	rows, err := c.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("fts search %q: %w", query, err)
	}
	defer rows.Close() //nolint:errcheck

	var chunks []Chunk
	for rows.Next() {
		chunk := Chunk{}
		if err := rows.Scan(&chunk.ID, &chunk.DocID, &chunk.ChunkText, &chunk.SequenceNum, &chunk.StartOffset, &chunk.EndOffset, &chunk.CreatedAt, &chunk.Score); err != nil {
			return nil, fmt.Errorf("scan fts result row: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fts results: %w", err)
	}

	return chunks, nil
}

// SearchVector performs a k-nearest-neighbor search over the vec0 table.
// The returned chunks carry the cosine distance in Score (lower is better).
// When domain is non-empty, results are filtered by document domain via JOIN
// with documents table and json_each before LIMIT is applied.
func (c *ChunkDAO) SearchVector(ctx context.Context, vector []float32, topK int, domain string) ([]Chunk, error) {
	if len(vector) == 0 {
		return nil, fmt.Errorf("empty query vector")
	}
	if topK <= 0 {
		topK = 20
	}

	var sqlQuery string
	var args []any

	if domain != "" {
		sqlQuery = `
			SELECT c.id, c.doc_id, c.chunk_text, c.sequence_num, c.start_offset, c.end_offset, c.created_at, v.distance
			FROM chunks_vec v
			JOIN chunks c ON c.id = v.chunk_id
			JOIN documents d ON d.id = c.doc_id
			WHERE v.vector MATCH ? AND k = ? AND EXISTS (SELECT 1 FROM json_each(d.metadata_json, '$.domain') WHERE json_each.value = ? AND d.metadata_json IS NOT NULL AND json_valid(d.metadata_json))`
		args = []any{FormatVector(vector), topK, domain}
	} else {
		sqlQuery = `
			SELECT c.id, c.doc_id, c.chunk_text, c.sequence_num, c.start_offset, c.end_offset, c.created_at, v.distance
			FROM chunks_vec v
			JOIN chunks c ON c.id = v.chunk_id
			WHERE v.vector MATCH ? AND k = ?`
		args = []any{FormatVector(vector), topK}
	}

	rows, err := c.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("semantic vector search: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var chunks []Chunk
	for rows.Next() {
		chunk := Chunk{}
		if err := rows.Scan(&chunk.ID, &chunk.DocID, &chunk.ChunkText, &chunk.SequenceNum, &chunk.StartOffset, &chunk.EndOffset, &chunk.CreatedAt, &chunk.Score); err != nil {
			return nil, fmt.Errorf("scan semantic result row: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate semantic results: %w", err)
	}

	return chunks, nil
}

// UpsertVector inserts or replaces the embedding vector for a chunk.
func (c *ChunkDAO) UpsertVector(ctx context.Context, chunkID int, vector []float32) error {
	if len(vector) == 0 {
		return fmt.Errorf("empty vector for chunk %d", chunkID)
	}

	query := `INSERT OR REPLACE INTO chunks_vec (chunk_id, vector) VALUES (?, ?)`
	if _, err := c.db.ExecContext(ctx, query, chunkID, FormatVector(vector)); err != nil {
		return fmt.Errorf("insert vector for chunk %d: %w", chunkID, err)
	}
	return nil
}

// DeleteVectorsByChunkIDs removes embedding vectors for specific chunk IDs.
func (c *ChunkDAO) DeleteVectorsByChunkIDs(ctx context.Context, chunkIDs []int) error {
	if len(chunkIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(chunkIDs))
	args := make([]any, len(chunkIDs))
	for i, id := range chunkIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf("DELETE FROM chunks_vec WHERE chunk_id IN (%s)", strings.Join(placeholders, ","))
	if _, err := c.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete vectors for %d chunks: %w", len(chunkIDs), err)
	}
	return nil
}

// DeleteByIDs removes specific chunks by their IDs. FTS5 triggers clean up the index automatically.
func (c *ChunkDAO) DeleteByIDs(ctx context.Context, chunkIDs []int) error {
	if len(chunkIDs) == 0 {
		return nil
	}
	placeholders := make([]string, len(chunkIDs))
	args := make([]any, len(chunkIDs))
	for i, id := range chunkIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	query := fmt.Sprintf("DELETE FROM chunks WHERE id IN (%s)", strings.Join(placeholders, ","))
	if _, err := c.db.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("delete %d chunks: %w", len(chunkIDs), err)
	}
	return nil
}

// DeleteOrphanedVectors removes vectors from chunks_vec that reference non-existent chunks.
// Returns the number of rows deleted. Ignores "no such table" errors when sqlite-vec is unavailable.
func (c *ChunkDAO) DeleteOrphanedVectors(ctx context.Context) (int64, error) {
	result, err := c.db.ExecContext(ctx, "DELETE FROM chunks_vec WHERE chunk_id NOT IN (SELECT id FROM chunks)")
	if err != nil {
		if strings.Contains(err.Error(), "no such table") {
			return 0, nil // sqlite-vec unavailable; harmless
		}
		return 0, fmt.Errorf("delete orphaned vectors: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("check delete rows affected: %w", err)
	}
	return rowsAffected, nil
}

// ListAll returns all chunks ordered by id. Used for re-embedding when the
// embedding model dimension changes.
func (c *ChunkDAO) ListAll(ctx context.Context) ([]Chunk, error) {
	query := `SELECT id, doc_id, chunk_text, sequence_num, start_offset, end_offset, created_at FROM chunks ORDER BY id`
	rows, err := c.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list all chunks: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var chunks []Chunk
	for rows.Next() {
		chunk := Chunk{}
		if err := rows.Scan(&chunk.ID, &chunk.DocID, &chunk.ChunkText, &chunk.SequenceNum, &chunk.StartOffset, &chunk.EndOffset, &chunk.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan chunk row: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chunks: %w", err)
	}

	return chunks, nil
}

// FormatVector renders a float32 slice as a JSON array string for sqlite-vec.
func FormatVector(vector []float32) string {
	parts := make([]string, len(vector))
	for i, v := range vector {
		parts[i] = strconv.FormatFloat(float64(v), 'g', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
