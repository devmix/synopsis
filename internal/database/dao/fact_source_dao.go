package dao

import (
	"context"
	"fmt"
)

// FactSource links a fact to its source document/chunk and quote.
type FactSource struct {
	ID          int     `json:"id"`
	FactID      int     `json:"fact_id"`
	DocumentID  int     `json:"document_id"`
	Quote       *string `json:"quote,omitempty"`
	ExtractedAt string  `json:"extracted_at"`
}

// FactSourceDAO provides CRUD operations for the fact_sources table.
type FactSourceDAO struct {
	db DBTX
}

// NewFactSourceDAO creates a new FactSourceDAO.
func NewFactSourceDAO(db DBTX) *FactSourceDAO {
	return &FactSourceDAO{db: db}
}

// Create inserts a new fact source and returns its Predicate.
func (d *FactSourceDAO) Create(ctx context.Context, src FactSource) (int, error) {
	if src.FactID == 0 {
		return 0, fmt.Errorf("fact_id is required")
	}

	query := `INSERT INTO fact_sources (fact_id, document_id,  quote, extracted_at) VALUES (?,  ?, ?, ?)`
	result, err := d.db.ExecContext(ctx, query,
		src.FactID, src.DocumentID, src.Quote, src.ExtractedAt,
	)
	if err != nil {
		return 0, fmt.Errorf("insert fact source: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return int(id), nil
}

// GetByFactID returns all sources for a given fact.
func (d *FactSourceDAO) GetByFactID(ctx context.Context, factID int) ([]FactSource, error) {
	query := `SELECT id, fact_id, document_id, quote, extracted_at FROM fact_sources WHERE fact_id = ? ORDER BY id`
	rows, err := d.db.QueryContext(ctx, query, factID)
	if err != nil {
		return nil, fmt.Errorf("get sources for fact %d: %w", factID, err)
	}
	defer rows.Close() //nolint:errcheck

	var sources []FactSource
	for rows.Next() {
		src := FactSource{}
		if err := rows.Scan(&src.ID, &src.FactID, &src.DocumentID, &src.Quote, &src.ExtractedAt); err != nil {
			return nil, fmt.Errorf("scan fact source row: %w", err)
		}
		sources = append(sources, src)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fact sources: %w", err)
	}

	return sources, nil
}

// Delete removes a fact source by Predicate.
func (d *FactSourceDAO) Delete(ctx context.Context, id int) error {
	result, err := d.db.ExecContext(ctx, "DELETE FROM fact_sources WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete fact source %d: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check delete rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("fact source %d not found", id)
	}
	return nil
}

// DeleteByFactID removes all sources for a given fact.
func (d *FactSourceDAO) DeleteByFactID(ctx context.Context, factID int) error {
	result, err := d.db.ExecContext(ctx, "DELETE FROM fact_sources WHERE fact_id = ?", factID)
	if err != nil {
		return fmt.Errorf("delete sources for fact %d: %w", factID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check delete rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("no sources found for fact %d", factID)
	}
	return nil
}

// DeleteByDocumentID removes all fact_sources for a given document.
// Returns the affected fact IDs before deletion (for scoped orphan cleanup).
func (d *FactSourceDAO) DeleteByDocumentID(ctx context.Context, docID int) ([]int, error) {
	// Collect affected fact IDs first.
	rows, err := d.db.QueryContext(ctx, "SELECT DISTINCT fact_id FROM fact_sources WHERE document_id = ?", docID)
	if err != nil {
		return nil, fmt.Errorf("query fact sources for document %d: %w", docID, err)
	}
	var factIDs []int
	for rows.Next() {
		var fid int
		if err := rows.Scan(&fid); err != nil {
			rows.Close() //nolint:errcheck
			return nil, fmt.Errorf("scan fact source row for document %d: %w", docID, err)
		}
		factIDs = append(factIDs, fid)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fact sources for document %d: %w", docID, err)
	}
	rows.Close() //nolint:errcheck

	// Now delete.
	if _, err := d.db.ExecContext(ctx, "DELETE FROM fact_sources WHERE document_id = ?", docID); err != nil {
		return nil, fmt.Errorf("delete fact sources for document %d: %w", docID, err)
	}
	return factIDs, nil
}
