package dao

import (
	"context"
	"fmt"
	"strings"
)

// EntityLink represents a cross-domain link between two entities.
type EntityLink struct {
	SubjectEntityID int     `json:"subject_entity_id"`
	TargetEntityID  int     `json:"target_entity_id"`
	RelationType    string  `json:"relation_type"`
	Method          string  `json:"method"`
	Confidence      float64 `json:"confidence"`
	Evidence        *string `json:"evidence,omitempty"`
}

// EntityLinkDAO provides CRUD operations for the entity_links table.
type EntityLinkDAO struct {
	db DBTX
}

// NewEntityLinkDAO creates a new EntityLinkDAO bound to the given database or transaction.
func NewEntityLinkDAO(db DBTX) *EntityLinkDAO {
	return &EntityLinkDAO{db: db}
}

// Create inserts a single entity link. Uses INSERT OR IGNORE for idempotency on re-ingest.
// Returns (true, nil) when a new row is inserted, (false, nil) when the row already exists
// (duplicate primary key). Returns an error only for unexpected failures such as constraint
// violations beyond the duplicate case (e.g. CHECK subject_entity_id != target_entity_id).
func (d *EntityLinkDAO) Create(ctx context.Context, link EntityLink) (bool, error) {
	if link.SubjectEntityID == link.TargetEntityID {
		return false, fmt.Errorf("self-link not allowed: subject_entity_id (%d) equals target_entity_id", link.SubjectEntityID)
	}

	query := `INSERT OR IGNORE INTO entity_links (subject_entity_id, target_entity_id, relation_type, method, confidence, evidence) VALUES (?, ?, ?, ?, ?, ?)`
	result, err := d.db.ExecContext(ctx, query,
		link.SubjectEntityID, link.TargetEntityID, link.RelationType,
		link.Method, link.Confidence, link.Evidence,
	)
	if err != nil {
		return false, fmt.Errorf("insert entity link: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("check insert rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

// ListByEntity returns all links where the given entity appears as subject or target.
// The caller determines direction by comparing entityID with SubjectEntityID.
func (d *EntityLinkDAO) ListByEntity(ctx context.Context, entityID int) ([]EntityLink, error) {
	query := `SELECT subject_entity_id, target_entity_id, relation_type, method, confidence, evidence FROM entity_links WHERE subject_entity_id = ? OR target_entity_id = ? ORDER BY target_entity_id, subject_entity_id`
	rows, err := d.db.QueryContext(ctx, query, entityID, entityID)
	if err != nil {
		return nil, fmt.Errorf("list entity links for entity %d: %w", entityID, err)
	}
	defer rows.Close() //nolint:errcheck

	var links []EntityLink
	for rows.Next() {
		var link EntityLink
		if err := rows.Scan(&link.SubjectEntityID, &link.TargetEntityID, &link.RelationType, &link.Method, &link.Confidence, &link.Evidence); err != nil {
			return nil, fmt.Errorf("scan entity link row: %w", err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity links: %w", err)
	}

	return links, nil
}

// ListByMethod returns all links created by a specific method (rule, equals, llm).
func (d *EntityLinkDAO) ListByMethod(ctx context.Context, method string) ([]EntityLink, error) {
	query := `SELECT subject_entity_id, target_entity_id, relation_type, method, confidence, evidence FROM entity_links WHERE method = ?`
	rows, err := d.db.QueryContext(ctx, query, method)
	if err != nil {
		return nil, fmt.Errorf("list entity links by method %q: %w", method, err)
	}
	defer rows.Close() //nolint:errcheck

	var links []EntityLink
	for rows.Next() {
		var link EntityLink
		if err := rows.Scan(&link.SubjectEntityID, &link.TargetEntityID, &link.RelationType, &link.Method, &link.Confidence, &link.Evidence); err != nil {
			return nil, fmt.Errorf("scan entity link row: %w", err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity links by method: %w", err)
	}

	return links, nil
}

// ListAll returns all entity links ordered by subject and target.
func (d *EntityLinkDAO) ListAll(ctx context.Context) ([]EntityLink, error) {
	query := `SELECT subject_entity_id, target_entity_id, relation_type, method, confidence, evidence FROM entity_links ORDER BY subject_entity_id, target_entity_id`
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list all entity links: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var links []EntityLink
	for rows.Next() {
		var link EntityLink
		if err := rows.Scan(&link.SubjectEntityID, &link.TargetEntityID, &link.RelationType, &link.Method, &link.Confidence, &link.Evidence); err != nil {
			return nil, fmt.Errorf("scan entity link row: %w", err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate all entity links: %w", err)
	}

	return links, nil
}

// Delete removes both directions of a link between two entities (A→B and B→A).
// Returns an error if no matching rows are found.
func (d *EntityLinkDAO) Delete(ctx context.Context, subjectID, targetID int) error {
	query := `DELETE FROM entity_links WHERE (subject_entity_id = ? AND target_entity_id = ?) OR (subject_entity_id = ? AND target_entity_id = ?)`
	result, err := d.db.ExecContext(ctx, query, subjectID, targetID, targetID, subjectID)
	if err != nil {
		return fmt.Errorf("delete entity link (%d, %d): %w", subjectID, targetID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check delete rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("entity link (%d, %d) not found", subjectID, targetID)
	}
	return nil
}

// Count returns the total number of entity links.
func (d *EntityLinkDAO) Count(ctx context.Context) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entity_links").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count entity links: %w", err)
	}
	return count, nil
}

// GraphNodeCount returns the number of distinct entities referenced in entity_links.
func (d *EntityLinkDAO) GraphNodeCount(ctx context.Context) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT DISTINCT subject_entity_id FROM entity_links UNION SELECT DISTINCT target_entity_id FROM entity_links)`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count graph nodes: %w", err)
	}
	return count, nil
}

// DeleteByEntityIDs removes all links where either subject or target is in the given set of IDs.
// Returns the number of rows deleted. Used during incremental linking to clean up
// existing links for changed entities before rebuilding them.
func (d *EntityLinkDAO) DeleteByEntityIDs(ctx context.Context, entityIDs []int) (int64, error) {
	if len(entityIDs) == 0 {
		return 0, nil
	}

	inClause := buildInPlaceholders(len(entityIDs))

	var sb strings.Builder
	sb.WriteString("DELETE FROM entity_links WHERE subject_entity_id IN (")
	sb.WriteString(inClause + ") OR target_entity_id IN (")
	sb.WriteString(inClause + ")")

	args := make([]any, 0, len(entityIDs)*2)
	for _, id := range entityIDs {
		args = append(args, id)
	}
	for _, id := range entityIDs {
		args = append(args, id)
	}

	result, err := d.db.ExecContext(ctx, sb.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("delete entity links by IDs: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("check delete rows affected: %w", err)
	}
	return rowsAffected, nil
}

// buildInPlaceholders returns a comma-separated string of "?" placeholders for the given count.
func buildInPlaceholders(count int) string {
	parts := make([]string, count)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}
