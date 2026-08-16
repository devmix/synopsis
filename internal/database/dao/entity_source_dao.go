package dao

import (
	"context"
	"fmt"
	"strings"
)

// EntitySource tracks the provenance of an entity to a document.
type EntitySource struct {
	ID         int `json:"id"`
	EntityID   int `json:"entity_id"`
	DocumentID int `json:"document_id"`
}

// EntitySourceDAO provides CRUD operations for the entity_sources table.
type EntitySourceDAO struct {
	db DBTX
}

// NewEntitySourceDAO creates a new EntitySourceDAO bound to the given database or transaction.
func NewEntitySourceDAO(db DBTX) *EntitySourceDAO {
	return &EntitySourceDAO{db: db}
}

// Create inserts a single entity source row and returns its generated Predicate.
func (d *EntitySourceDAO) Create(ctx context.Context, src EntitySource) (int64, error) {
	query := `INSERT INTO entity_sources (entity_id, document_id) VALUES (?, ?)`
	result, err := d.db.ExecContext(ctx, query, src.EntityID, src.DocumentID)
	if err != nil {
		return 0, fmt.Errorf("insert entity source: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return id, nil
}

// linkBatchSize limits the number of rows per multi-row INSERT statement.
const linkBatchSize = 500

// LinkBatch bulk-inserts entity→document provenance links using a single
// multi-row INSERT OR IGNORE statement, chunked into batches of ~500 for large lists.
func (d *EntitySourceDAO) LinkBatch(ctx context.Context, docID int, entityIDs []int) error {
	if len(entityIDs) == 0 {
		return nil
	}

	for start := 0; start < len(entityIDs); start += linkBatchSize {
		end := start + linkBatchSize
		if end > len(entityIDs) {
			end = len(entityIDs)
		}
		batch := entityIDs[start:end]

		var sb strings.Builder
		sb.WriteString("INSERT OR IGNORE INTO entity_sources (entity_id, document_id) VALUES ")
		args := make([]any, 0, len(batch)*2)
		for i, eid := range batch {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("(?,?)")
			args = append(args, eid, docID)
		}

		if _, err := d.db.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("link batch entities to document %d: %w", docID, err)
		}
	}
	return nil
}

// DeleteByDocumentID removes all entity source links for a given document.
// Returns the affected entity IDs before deletion (for scoped orphan cleanup).
func (d *EntitySourceDAO) DeleteByDocumentID(ctx context.Context, docID int) ([]int, error) {
	// Collect affected entity IDs first.
	rows, err := d.db.QueryContext(ctx, "SELECT DISTINCT entity_id FROM entity_sources WHERE document_id = ?", docID)
	if err != nil {
		return nil, fmt.Errorf("query entity sources for document %d: %w", docID, err)
	}
	var entityIDs []int
	for rows.Next() {
		var eid int
		if err := rows.Scan(&eid); err != nil {
			rows.Close() //nolint:errcheck
			return nil, fmt.Errorf("scan entity source row for document %d: %w", docID, err)
		}
		entityIDs = append(entityIDs, eid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity sources for document %d: %w", docID, err)
	}
	rows.Close() //nolint:errcheck

	// Now delete.
	if _, err := d.db.ExecContext(ctx, "DELETE FROM entity_sources WHERE document_id = ?", docID); err != nil {
		return nil, fmt.Errorf("delete entity sources for document %d: %w", docID, err)
	}
	return entityIDs, nil
}

// GetDocumentsByEntityID returns all document IDs that reference a given entity.
func (d *EntitySourceDAO) GetDocumentsByEntityID(ctx context.Context, entityID int) ([]int, error) {
	query := `SELECT document_id FROM entity_sources WHERE entity_id = ?`
	rows, err := d.db.QueryContext(ctx, query, entityID)
	if err != nil {
		return nil, fmt.Errorf("query documents for entity %d: %w", entityID, err)
	}
	defer rows.Close() //nolint:errcheck

	var docIDs []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan document id row: %w", err)
		}
		docIDs = append(docIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate document ids: %w", err)
	}

	return docIDs, nil
}

// FindOrphanedEntityIDs returns entity IDs that have zero entity_sources entries.
// Excludes entities of type "EntityType" which are shared infrastructure and should never be auto-deleted.
func (d *EntitySourceDAO) FindOrphanedEntityIDs(ctx context.Context) ([]int, error) {
	query := `SELECT e.id FROM entities e LEFT JOIN entity_sources es ON e.id = es.entity_id WHERE es.id IS NULL AND e.type != 'EntityType'`
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query orphaned entities: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan orphaned entity row: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orphaned entities: %w", err)
	}

	return ids, nil
}
