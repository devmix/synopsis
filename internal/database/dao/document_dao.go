package dao

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/devmix/synopsis/internal/utils"
)

// Document represents a source document in the knowledge base.
type Document struct {
	ID           int     `json:"id"`
	SourceType   string  `json:"source_type"`
	OriginalPath string  `json:"original_path"`
	MetadataJSON *string `json:"metadata_json,omitempty"`
	ContentHash  string  `json:"content_hash,omitempty"` // SHA-256 hash of document content for deduplication
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// DocumentDAO provides CRUD operations for the documents table.
type DocumentDAO struct {
	db DBTX
}

// NewDocumentDAO creates a new DocumentDAO bound to the given database or transaction.
func NewDocumentDAO(db DBTX) *DocumentDAO {
	return &DocumentDAO{db: db}
}

// Create inserts a new document and returns its generated Predicate.
func (d *DocumentDAO) Create(ctx context.Context, doc Document) (int, error) {
	query := `INSERT INTO documents (source_type, original_path, metadata_json, content_hash) VALUES (?, ?, ?, ?)`
	result, err := d.db.ExecContext(ctx, query, doc.SourceType, doc.OriginalPath, doc.MetadataJSON, doc.ContentHash)
	if err != nil {
		return 0, fmt.Errorf("insert document: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return int(id), nil
}

// GetByID retrieves a single document by its Predicate.
func (d *DocumentDAO) GetByID(ctx context.Context, id int) (*Document, error) {
	query := `SELECT id, source_type, original_path, metadata_json, content_hash, created_at, updated_at FROM documents WHERE id = ?`
	doc := &Document{}
	err := d.db.QueryRowContext(ctx, query, id).Scan(
		&doc.ID, &doc.SourceType, &doc.OriginalPath, &doc.MetadataJSON,
		&doc.ContentHash, &doc.CreatedAt, &doc.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // not found
	}
	if err != nil {
		return nil, fmt.Errorf("query document %d: %w", id, err)
	}
	return doc, nil
}

// GetByPath retrieves a single document by its original_path.
// Returns (nil, nil) if no document exists for the given path.
func (d *DocumentDAO) GetByPath(ctx context.Context, path string) (*Document, error) {
	query := `SELECT id, source_type, original_path, metadata_json, content_hash,  created_at, updated_at FROM documents WHERE original_path = ?`
	doc := &Document{}
	err := d.db.QueryRowContext(ctx, query, path).Scan(
		&doc.ID, &doc.SourceType, &doc.OriginalPath, &doc.MetadataJSON,
		&doc.ContentHash, &doc.CreatedAt, &doc.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // not found
	}
	if err != nil {
		return nil, fmt.Errorf("query document by path %q: %w", path, err)
	}
	return doc, nil
}

// GetByIDs retrieves multiple documents by their IDs in a single batch query.
// The result maps each document Predicate to its Document; IDs that do not exist in the
// database are simply absent from the map. An empty ids slice returns an empty
// map without error.
func (d *DocumentDAO) GetByIDs(ctx context.Context, ids []int) (map[int]*Document, error) {
	if len(ids) == 0 {
		return make(map[int]*Document), nil
	}

	// Build placeholder list: "?, ?, ?"
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT id, source_type, original_path, metadata_json, content_hash,  created_at, updated_at FROM documents WHERE id IN (%s)`,
		strings.Join(placeholders, ", "),
	)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get documents by ids batch: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	docs := make(map[int]*Document, len(ids))
	for rows.Next() {
		doc := &Document{}
		if err := rows.Scan(&doc.ID, &doc.SourceType, &doc.OriginalPath, &doc.MetadataJSON, &doc.ContentHash, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan document row: %w", err)
		}
		docs[doc.ID] = doc
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documents: %w", err)
	}

	return docs, nil
}

// UpdateHash updates the content_hash and updated_at timestamp for a document.
func (d *DocumentDAO) UpdateHash(ctx context.Context, id int, hash string) error {
	query := `UPDATE documents SET content_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	result, err := d.db.ExecContext(ctx, query, hash, id)
	if err != nil {
		return fmt.Errorf("update document hash %d: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check update rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("document %d not found", id)
	}
	return nil
}

// List returns all documents ordered by creation time descending.
func (d *DocumentDAO) List(ctx context.Context) ([]Document, error) {
	query := `SELECT id, source_type, original_path, metadata_json, content_hash,  created_at, updated_at FROM documents ORDER BY created_at DESC`
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list documents: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var docs []Document
	for rows.Next() {
		doc := Document{}
		if err := rows.Scan(&doc.ID, &doc.SourceType, &doc.OriginalPath, &doc.MetadataJSON, &doc.ContentHash, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan document row: %w", err)
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documents: %w", err)
	}

	return docs, nil
}

// ListByNERStatus returns all documents with the given NER status.
func (d *DocumentDAO) ListByNERStatus(ctx context.Context, status string) ([]Document, error) {
	query := `SELECT id, source_type, original_path, metadata_json, content_hash,  created_at, updated_at FROM documents WHERE ner_status = ? ORDER BY created_at DESC`
	rows, err := d.db.QueryContext(ctx, query, status)
	if err != nil {
		return nil, fmt.Errorf("list documents by ner_status %q: %w", status, err)
	}
	defer rows.Close() //nolint:errcheck

	var docs []Document
	for rows.Next() {
		doc := Document{}
		if err := rows.Scan(&doc.ID, &doc.SourceType, &doc.OriginalPath, &doc.MetadataJSON, &doc.ContentHash, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan document row: %w", err)
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documents: %w", err)
	}

	return docs, nil
}

// UpdateNERStatus updates the NER status of a document.
func (d *DocumentDAO) UpdateNERStatus(ctx context.Context, id int, status string) error {
	query := `UPDATE documents SET ner_status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	result, err := d.db.ExecContext(ctx, query, status, id)
	if err != nil {
		return fmt.Errorf("update document %d ner_status to %q: %w", id, status, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check update rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("document %d not found", id)
	}
	return nil
}

// Update modifies an existing document's path, metadata and content hash.
func (d *DocumentDAO) Update(ctx context.Context, doc Document) error {
	query := `UPDATE documents SET original_path = ?, metadata_json = ?, content_hash = ?,  updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	result, err := d.db.ExecContext(ctx, query, doc.OriginalPath, doc.MetadataJSON, doc.ContentHash, doc.ID)
	if err != nil {
		return fmt.Errorf("update document %d: %w", doc.ID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check update rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("document %d not found", doc.ID)
	}
	return nil
}

// Delete removes a document by Predicate. Chunks are cascaded.
func (d *DocumentDAO) Delete(ctx context.Context, id int) error {
	result, err := d.db.ExecContext(ctx, "DELETE FROM documents WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete document %d: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check delete rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("document %d not found", id)
	}
	return nil
}

// Count returns the total number of documents.
func (d *DocumentDAO) Count(ctx context.Context) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM documents").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count documents: %w", err)
	}
	return count, nil
}

// ListPaginated returns a page of documents ordered by id.
// Domain and sourceType filters are optional (empty string = no filter).
func (d *DocumentDAO) ListPaginated(ctx context.Context, offset, limit int, domain, sourceType string) ([]Document, int, error) {
	return d.listPaginatedWithFilters(ctx, offset, limit, domain, sourceType, "")
}

// ListPaginatedWithName returns a page of documents ordered by id with an optional name filter.
// The nameFilter is applied as LIKE on original_path (case-insensitive substring match).
func (d *DocumentDAO) ListPaginatedWithName(ctx context.Context, offset, limit int, domain, sourceType, nameFilter string) ([]Document, int, error) {
	return d.listPaginatedWithFilters(ctx, offset, limit, domain, sourceType, nameFilter)
}

// listPaginatedWithFilters is the internal implementation for paginated document listing.
func (d *DocumentDAO) listPaginatedWithFilters(ctx context.Context, offset, limit int, domain, sourceType, nameFilter string) ([]Document, int, error) {
	query := `SELECT id, source_type, original_path, metadata_json, content_hash, created_at, updated_at FROM documents`
	countQuery := `SELECT COUNT(*) FROM documents`
	args := []any{}
	countArgs := []any{}

	var whereClauses []string
	if domain != "" {
		whereClauses = append(whereClauses, "EXISTS (SELECT 1 FROM json_each(metadata_json, '$.domain') WHERE json_each.value = ? AND metadata_json IS NOT NULL AND json_valid(metadata_json))")
		args = append(args, domain)
		countArgs = append(countArgs, domain)
	}
	if sourceType != "" {
		whereClauses = append(whereClauses, "source_type = ?")
		args = append(args, sourceType)
		countArgs = append(countArgs, sourceType)
	}
	if nameFilter != "" {
		whereClauses = append(whereClauses, "lower(original_path) LIKE lower(?) ESCAPE '\\'")
		likePattern := "%" + utils.EscapeLike(nameFilter) + "%"
		args = append(args, likePattern)
		countArgs = append(countArgs, likePattern)
	}

	if len(whereClauses) > 0 {
		where := " WHERE " + strings.Join(whereClauses, " AND ")
		query += where
		countQuery += where
	}

	query += ` ORDER BY id LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list paginated documents: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var docs []Document
	for rows.Next() {
		doc := Document{}
		if err := rows.Scan(&doc.ID, &doc.SourceType, &doc.OriginalPath, &doc.MetadataJSON, &doc.ContentHash, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan document row: %w", err)
		}
		docs = append(docs, doc)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate documents: %w", err)
	}

	var totalCount int
	if err := d.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("count paginated documents: %w", err)
	}

	return docs, totalCount, nil
}

// DocumentsByType returns a map of source_type to document count.
func (d *DocumentDAO) DocumentsByType(ctx context.Context) (map[string]int, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT source_type, COUNT(*) FROM documents GROUP BY source_type`)
	if err != nil {
		return nil, fmt.Errorf("query documents by type: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	result := make(map[string]int)
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			return nil, fmt.Errorf("scan documents by type row: %w", err)
		}
		result[t] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate documents by type: %w", err)
	}
	return result, nil
}

// UniqueDomains returns distinct domain values extracted from metadata_json ($.domain).
func (d *DocumentDAO) UniqueDomains(ctx context.Context) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT DISTINCT json_each.value FROM documents, json_each(metadata_json, '$.domain') WHERE metadata_json IS NOT NULL AND json_valid(metadata_json) AND json_each.value IS NOT NULL AND json_each.value != ''`)
	if err != nil {
		return nil, fmt.Errorf("query unique domains: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var domains []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("scan domain row: %w", err)
		}
		domains = append(domains, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate domains: %w", err)
	}
	if domains == nil {
		domains = []string{}
	}
	return domains, nil
}
