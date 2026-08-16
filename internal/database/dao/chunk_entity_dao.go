package dao

import (
	"context"
	"fmt"
	"strings"
)

// ChunkEntityDAO manages the many-to-many relationship between chunks and entities.
type ChunkEntityDAO struct {
	db DBTX
}

// NewChunkEntityDAO creates a new ChunkEntityDAO bound to the given database or transaction.
func NewChunkEntityDAO(db DBTX) *ChunkEntityDAO {
	return &ChunkEntityDAO{db: db}
}

// Link associates an entity with a chunk.
func (c *ChunkEntityDAO) Link(ctx context.Context, chunkID, entityID int) error {
	query := `INSERT OR IGNORE INTO chunk_entities (chunk_id, entity_id) VALUES (?, ?)`
	result, err := c.db.ExecContext(ctx, query, chunkID, entityID)
	if err != nil {
		return fmt.Errorf("link chunk %d to entity %d: %w", chunkID, entityID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check link rows affected: %w", err)
	}
	if rowsAffected == 0 {
		// Link already exists — not an error.
		return nil
	}
	return nil
}

// Unlink removes the association between a chunk and an entity.
func (c *ChunkEntityDAO) Unlink(ctx context.Context, chunkID, entityID int) error {
	result, err := c.db.ExecContext(
		ctx, "DELETE FROM chunk_entities WHERE chunk_id = ? AND entity_id = ?", chunkID, entityID,
	)
	if err != nil {
		return fmt.Errorf("unlink chunk %d from entity %d: %w", chunkID, entityID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check unlink rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("chunk %d and entity %d are not linked", chunkID, entityID)
	}
	return nil
}

// GetEntitiesByChunk returns all entity IDs associated with a chunk.
func (c *ChunkEntityDAO) GetEntitiesByChunk(ctx context.Context, chunkID int) ([]int, error) {
	query := `SELECT e.id FROM entities e INNER JOIN chunk_entities ce ON ce.entity_id = e.id WHERE ce.chunk_id = ? ORDER BY e.name`
	rows, err := c.db.QueryContext(ctx, query, chunkID)
	if err != nil {
		return nil, fmt.Errorf("get entities for chunk %d: %w", chunkID, err)
	}
	defer rows.Close() //nolint:errcheck

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan entity id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity ids: %w", err)
	}

	return ids, nil
}

// GetChunksByEntity returns all chunk IDs associated with an entity.
func (c *ChunkEntityDAO) GetChunksByEntity(ctx context.Context, entityID int) ([]int, error) {
	query := `SELECT ce.chunk_id FROM chunk_entities ce WHERE ce.entity_id = ? ORDER BY ce.chunk_id`
	rows, err := c.db.QueryContext(ctx, query, entityID)
	if err != nil {
		return nil, fmt.Errorf("get chunks for entity %d: %w", entityID, err)
	}
	defer rows.Close() //nolint:errcheck

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan chunk id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chunk ids: %w", err)
	}

	return ids, nil
}

// IsLinked checks whether a chunk and entity are associated.
func (c *ChunkEntityDAO) IsLinked(ctx context.Context, chunkID, entityID int) (bool, error) {
	var count int
	err := c.db.QueryRowContext(
		ctx, "SELECT COUNT(*) FROM chunk_entities WHERE chunk_id = ? AND entity_id = ?",
		chunkID, entityID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check link chunk %d entity %d: %w", chunkID, entityID, err)
	}
	return count > 0, nil
}

// UnlinkChunk removes all entity associations for a chunk.
func (c *ChunkEntityDAO) UnlinkChunk(ctx context.Context, chunkID int) error {
	result, err := c.db.ExecContext(ctx, "DELETE FROM chunk_entities WHERE chunk_id = ?", chunkID)
	if err != nil {
		return fmt.Errorf("unlink all entities from chunk %d: %w", chunkID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check unlink rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("chunk %d has no entity links", chunkID)
	}
	return nil
}

// UnlinkEntity removes all chunk associations for an entity.
func (c *ChunkEntityDAO) UnlinkEntity(ctx context.Context, entityID int) error {
	result, err := c.db.ExecContext(ctx, "DELETE FROM chunk_entities WHERE entity_id = ?", entityID)
	if err != nil {
		return fmt.Errorf("unlink all chunks from entity %d: %w", entityID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check unlink rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("entity %d has no chunk links", entityID)
	}
	return nil
}

// GetChunkTextsByEntity returns up to limit chunk texts associated with an entity,
// ordered by sequence number. Returns a placeholder slice if no chunks are found.
func (c *ChunkEntityDAO) GetChunkTextsByEntity(ctx context.Context, entityID int, limit int) ([]string, error) {
	query := `SELECT c.chunk_text FROM chunks c
		INNER JOIN chunk_entities ce ON c.id = ce.chunk_id
		WHERE ce.entity_id = ?
		ORDER BY c.sequence_num ASC
		LIMIT ?`

	rows, err := c.db.QueryContext(ctx, query, entityID, limit)
	if err != nil {
		return nil, fmt.Errorf("get chunk texts for entity %d: %w", entityID, err)
	}
	defer rows.Close() //nolint:errcheck

	var chunks []string
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return nil, fmt.Errorf("scan chunk text for entity %d: %w", entityID, err)
		}
		chunks = append(chunks, text)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chunk texts for entity %d: %w", entityID, err)
	}

	if len(chunks) == 0 {
		chunks = []string{"<no context available>"}
	}

	return chunks, nil
}

// GetEntitiesByChunks returns all entities associated with the given chunk IDs in a single query.
// The result maps each chunk_id to its full Entity records (not just IDs).
func (c *ChunkEntityDAO) GetEntitiesByChunks(ctx context.Context, chunkIDs []int) (map[int][]Entity, error) {
	if len(chunkIDs) == 0 {
		return make(map[int][]Entity), nil
	}

	// Build placeholder list: "?, ?, ?"
	placeholders := make([]string, len(chunkIDs))
	args := make([]any, len(chunkIDs))
	for i, id := range chunkIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT ce.chunk_id, e.id, e.type, e.name, e.domain, e.description, e.metadata_json, e.created_at
		 FROM entities e
		 INNER JOIN chunk_entities ce ON ce.entity_id = e.id
		 WHERE ce.chunk_id IN (%s)
		 ORDER BY ce.chunk_id, e.name`,
		strings.Join(placeholders, ", "),
	)

	rows, err := c.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get entities for chunks batch: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	result := make(map[int][]Entity)
	for rows.Next() {
		var ent Entity
		var chunkID int
		if err := rows.Scan(&chunkID, &ent.ID, &ent.Type, &ent.Name, &ent.Domain, &ent.Description, &ent.MetadataJSON, &ent.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan batch entity row: %w", err)
		}
		result[chunkID] = append(result[chunkID], ent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate batch entities: %w", err)
	}

	return result, nil
}

// GetEntityIDsByDocID returns all distinct entity IDs associated with a document
// by joining chunk_entities with chunks filtered by doc_id. This allows retrieving
// entities for a document without loading the full chunk data.
func (c *ChunkEntityDAO) GetEntityIDsByDocID(ctx context.Context, docID int) ([]int, error) {
	query := `SELECT DISTINCT ce.entity_id FROM chunk_entities ce
		INNER JOIN chunks ch ON ch.id = ce.chunk_id
		WHERE ch.doc_id = ?
		ORDER BY ce.entity_id`

	rows, err := c.db.QueryContext(ctx, query, docID)
	if err != nil {
		return nil, fmt.Errorf("get entity IDs for document %d: %w", docID, err)
	}
	defer rows.Close() //nolint:errcheck

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan entity Predicate for document %d: %w", docID, err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity IDs for document %d: %w", docID, err)
	}

	return ids, nil
}
