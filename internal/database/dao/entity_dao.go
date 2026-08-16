package dao

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/devmix/synopsis/internal/utils"
)

// Entity represents a knowledge graph entity.
type Entity struct {
	ID           int     `json:"id"`
	Type         string  `json:"type"`
	Name         string  `json:"name"`
	Domain       string  `json:"domain"`
	Confidence   float64 `json:"confidence,omitempty"`
	Description  *string `json:"description,omitempty"`
	MetadataJSON *string `json:"metadata_json,omitempty"`
	CreatedAt    string  `json:"created_at"`
}

// EntityDAO provides CRUD operations for the entities table.
type EntityDAO struct {
	db DBTX
}

// NewEntityDAO creates a new EntityDAO bound to the given database or transaction.
func NewEntityDAO(db DBTX) *EntityDAO {
	return &EntityDAO{db: db}
}

// Create inserts a new entity and returns its generated Predicate.
func (e *EntityDAO) Create(ctx context.Context, ent Entity) (int, error) {
	query := `INSERT INTO entities (type, name, domain, confidence, description, metadata_json) VALUES (?, ?, ?, ?, ?, ?)`
	result, err := e.db.ExecContext(ctx, query, ent.Type, ent.Name, ent.Domain, ent.Confidence, ent.Description, ent.MetadataJSON)
	if err != nil {
		return 0, fmt.Errorf("insert entity %q: %w", ent.Name, err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return int(id), nil
}

// GetByID retrieves a single entity by its Predicate.
func (e *EntityDAO) GetByID(ctx context.Context, id int) (*Entity, error) {
	query := `SELECT id, type, name, domain, confidence, description, metadata_json, created_at FROM entities WHERE id = ?`
	ent := &Entity{}
	err := e.db.QueryRowContext(ctx, query, id).Scan(
		&ent.ID, &ent.Type, &ent.Name, &ent.Domain, &ent.Confidence, &ent.Description, &ent.MetadataJSON, &ent.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // not found
	}
	if err != nil {
		return nil, fmt.Errorf("query entity %d: %w", id, err)
	}
	return ent, nil
}

// GetByName retrieves a single entity by its name and domain.
func (e *EntityDAO) GetByName(ctx context.Context, name string, domain string) (*Entity, error) {
	query := `SELECT id, type, name, domain, confidence, description, metadata_json, created_at FROM entities WHERE name = ? AND domain = ?`
	ent := &Entity{}
	err := e.db.QueryRowContext(ctx, query, name, domain).Scan(
		&ent.ID, &ent.Type, &ent.Name, &ent.Domain, &ent.Confidence, &ent.Description, &ent.MetadataJSON, &ent.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // not found
	}
	if err != nil {
		return nil, fmt.Errorf("query entity %q in domain %q: %w", name, domain, err)
	}
	return ent, nil
}

// GetByNameFold retrieves a single entity by its name and domain, case-insensitively.
// Uses SQL lower() for comparison so that "Alice" matches "alice".
func (e *EntityDAO) GetByNameFold(ctx context.Context, name string, domain string) (*Entity, error) {
	query := `SELECT id, type, name, domain, confidence, description, metadata_json, created_at FROM entities WHERE lower(name) = lower(?) AND lower(domain) = lower(?)`
	ent := &Entity{}
	err := e.db.QueryRowContext(ctx, query, name, domain).Scan(
		&ent.ID, &ent.Type, &ent.Name, &ent.Domain, &ent.Confidence, &ent.Description, &ent.MetadataJSON, &ent.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // not found
	}
	if err != nil {
		return nil, fmt.Errorf("query entity %q in domain %q (case-insensitive): %w", name, domain, err)
	}
	return ent, nil
}

// ListByNameFold returns all entities matching the given name case-insensitively.
// Results are ordered by id for deterministic output. Uses SQL lower() for comparison.
func (e *EntityDAO) ListByNameFold(ctx context.Context, name string) ([]Entity, error) {
	query := `SELECT id, type, name, domain, confidence, description, metadata_json, created_at FROM entities WHERE lower(name) = lower(?) ORDER BY id`
	rows, err := e.db.QueryContext(ctx, query, name)
	if err != nil {
		return nil, fmt.Errorf("list entities by name %q (case-insensitive): %w", name, err)
	}
	defer rows.Close() //nolint:errcheck

	var ents []Entity
	for rows.Next() {
		ent := Entity{}
		if err := rows.Scan(&ent.ID, &ent.Type, &ent.Name, &ent.Domain, &ent.Confidence, &ent.Description, &ent.MetadataJSON, &ent.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan entity row: %w", err)
		}
		ents = append(ents, ent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entities by name: %w", err)
	}

	return ents, nil
}

// List returns all entities ordered by name.
func (e *EntityDAO) List(ctx context.Context) ([]Entity, error) {
	query := `SELECT id, type, name, domain, confidence, description, metadata_json, created_at FROM entities ORDER BY name`
	rows, err := e.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var ents []Entity
	for rows.Next() {
		ent := Entity{}
		if err := rows.Scan(&ent.ID, &ent.Type, &ent.Name, &ent.Domain, &ent.Confidence, &ent.Description, &ent.MetadataJSON, &ent.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan entity row: %w", err)
		}
		ents = append(ents, ent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entities: %w", err)
	}

	return ents, nil
}

// ListByType returns all entities of a given type, optionally filtered by domain.
// If domain is empty, it matches all domains.
func (e *EntityDAO) ListByType(ctx context.Context, entityType string, domain string) ([]Entity, error) {
	query := `SELECT id, type, name, domain, confidence, description, metadata_json, created_at FROM entities WHERE type = ?`
	args := []any{entityType}
	if domain != "" {
		query += ` AND domain = ?`
		args = append(args, domain)
	}
	query += ` ORDER BY name`
	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list entities of type %q: %w", entityType, err)
	}
	defer rows.Close() //nolint:errcheck

	var ents []Entity
	for rows.Next() {
		ent := Entity{}
		if err := rows.Scan(&ent.ID, &ent.Type, &ent.Name, &ent.Domain, &ent.Confidence, &ent.Description, &ent.MetadataJSON, &ent.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan entity row: %w", err)
		}
		ents = append(ents, ent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entities: %w", err)
	}

	return ents, nil
}

// Update modifies an existing entity's fields. Domain cannot be changed via this method.
func (e *EntityDAO) Update(ctx context.Context, ent Entity) error {
	query := `UPDATE entities SET type = ?, description = ?, metadata_json = ? WHERE id = ?`
	result, err := e.db.ExecContext(ctx, query, ent.Type, ent.Description, ent.MetadataJSON, ent.ID)
	if err != nil {
		return fmt.Errorf("update entity %d: %w", ent.ID, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check update rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("entity %d not found", ent.ID)
	}
	return nil
}

// UpdateName renames an existing entity within its domain. Used by entity resolution when a
// longer, more complete canonical name is discovered for the same entity.
func (e *EntityDAO) UpdateName(ctx context.Context, id int, name string) error {
	result, err := e.db.ExecContext(ctx, "UPDATE entities SET name = ? WHERE id = ?", name, id)
	if err != nil {
		return fmt.Errorf("update entity %d name to %q: %w", id, name, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check update name rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("entity %d not found", id)
	}
	return nil
}

// Delete removes an entity by Predicate. Facts and chunk_entities are cascaded.
func (e *EntityDAO) Delete(ctx context.Context, id int) error {
	result, err := e.db.ExecContext(ctx, "DELETE FROM entities WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete entity %d: %w", id, err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check delete rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("entity %d not found", id)
	}
	return nil
}

// Count returns the total number of entities.
func (e *EntityDAO) Count(ctx context.Context) (int, error) {
	var count int
	err := e.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entities").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count entities: %w", err)
	}
	return count, nil
}

// ListPaginated returns a page of entities ordered by id.
// Domain and entityType filters are optional (empty string = no filter).
func (e *EntityDAO) ListPaginated(ctx context.Context, offset, limit int, domain, entityType string) ([]Entity, int, error) {
	return e.listPaginatedWithFilters(ctx, offset, limit, domain, entityType, "")
}

// ListPaginatedWithName returns a page of entities ordered by id with an optional name filter.
// The nameFilter is applied as LIKE on name (case-insensitive substring match).
func (e *EntityDAO) ListPaginatedWithName(ctx context.Context, offset, limit int, domain, entityType, nameFilter string) ([]Entity, int, error) {
	return e.listPaginatedWithFilters(ctx, offset, limit, domain, entityType, nameFilter)
}

// listPaginatedWithFilters is the internal implementation for paginated entity listing.
func (e *EntityDAO) listPaginatedWithFilters(ctx context.Context, offset, limit int, domain, entityType, nameFilter string) ([]Entity, int, error) {
	query := `SELECT id, type, name, domain, confidence, description, metadata_json, created_at FROM entities`
	countQuery := `SELECT COUNT(*) FROM entities`
	args := []any{}
	countArgs := []any{}

	var whereClauses []string
	if entityType != "" {
		whereClauses = append(whereClauses, "type = ?")
		args = append(args, entityType)
		countArgs = append(countArgs, entityType)
	}
	if domain != "" {
		whereClauses = append(whereClauses, "domain = ?")
		args = append(args, domain)
		countArgs = append(countArgs, domain)
	}
	if nameFilter != "" {
		whereClauses = append(whereClauses, "lower(name) LIKE lower(?) ESCAPE '\\'")
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

	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list paginated entities: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var ents []Entity
	for rows.Next() {
		ent := Entity{}
		if err := rows.Scan(&ent.ID, &ent.Type, &ent.Name, &ent.Domain, &ent.Confidence, &ent.Description, &ent.MetadataJSON, &ent.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan entity row: %w", err)
		}
		ents = append(ents, ent)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate entities: %w", err)
	}

	var totalCount int
	if err := e.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("count paginated entities: %w", err)
	}

	return ents, totalCount, nil
}

func (e *EntityDAO) GetOrCreate(ctx context.Context, name string, entityType string, domain string, confidence float64, metadataJSON *string, description *string) (int, error) {
	existing, err := e.GetByName(ctx, name, domain)
	if err != nil {
		return 0, fmt.Errorf("lookup entity %q: %w", name, err)
	}
	if existing != nil {
		return existing.ID, nil
	}

	id, err := e.Create(ctx, Entity{
		Type:         entityType,
		Name:         name,
		Domain:       domain,
		Confidence:   confidence,
		Description:  description,
		MetadataJSON: metadataJSON,
	})
	if err != nil {
		return 0, fmt.Errorf("create entity %q: %w", name, err)
	}
	return id, nil
}

// DeleteOrphanedEntityIDs removes entities that have no entity_sources links (excludes EntityType).
// Excludes entities referenced by any fact (mandatory — otherwise the orphan_cleanup job fails
// with FK errors since facts→entities FK has no CASCADE).
// Uses a single DELETE with subquery for batch deletion. Transaction-aware.
// Entity links are cascade-deleted automatically via ON DELETE CASCADE on entity_links.
func (e *EntityDAO) DeleteOrphanedEntityIDs(ctx context.Context) (int64, error) {
	query := `DELETE FROM entities WHERE id IN (
		SELECT e.id FROM entities e LEFT JOIN entity_sources es ON e.id = es.entity_id
		WHERE es.id IS NULL AND e.type != 'EntityType'
			AND e.id NOT IN (SELECT subject_entity_id FROM facts UNION SELECT object_entity_id FROM facts)
	)`
	result, err := e.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("delete orphaned entities: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("check delete rows affected: %w", err)
	}
	return rowsAffected, nil
}

// DeleteOrphanedByIDs deletes only the given candidate entity IDs that have no
// remaining entity_sources, excluding EntityType and entities referenced by
// facts (subject or object). Entity links are cascade-deleted automatically via
// ON DELETE CASCADE on entity_links. Returns the number of rows deleted.
func (e *EntityDAO) DeleteOrphanedByIDs(ctx context.Context, candidateIDs []int) (int64, error) {
	if len(candidateIDs) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString("DELETE FROM entities WHERE id IN (")
	args := make([]any, 0, len(candidateIDs))
	for i, id := range candidateIDs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("?")
		args = append(args, id)
	}
	sb.WriteString(") AND type != 'EntityType' ")
	sb.WriteString("AND id NOT IN (SELECT entity_id FROM entity_sources) ")
	sb.WriteString("AND id NOT IN (SELECT subject_entity_id FROM facts UNION SELECT object_entity_id FROM facts)")

	result, err := e.db.ExecContext(ctx, sb.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("delete orphaned entities by IDs: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("check delete rows affected: %w", err)
	}
	return rowsAffected, nil
}

// GetByIDs retrieves multiple entities by their IDs in a single batch query.
// The result maps each entity Predicate to its Entity; IDs that do not exist are absent from the map.
// An empty ids slice returns an empty map without error.
func (e *EntityDAO) GetByIDs(ctx context.Context, ids []int) (map[int]*Entity, error) {
	if len(ids) == 0 {
		return make(map[int]*Entity), nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT id, type, name, domain, confidence, description, metadata_json, created_at FROM entities WHERE id IN (%s)`,
		strings.Join(placeholders, ", "),
	)

	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get entities by ids batch: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	entities := make(map[int]*Entity, len(ids))
	for rows.Next() {
		ent := &Entity{}
		if err := rows.Scan(&ent.ID, &ent.Type, &ent.Name, &ent.Domain, &ent.Confidence, &ent.Description, &ent.MetadataJSON, &ent.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan entity row: %w", err)
		}
		entities[ent.ID] = ent
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entities: %w", err)
	}

	return entities, nil
}

// TypesByCount returns a map of entity type to count.
func (e *EntityDAO) TypesByCount(ctx context.Context) (map[string]int, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT type, COUNT(*) FROM entities GROUP BY type`)
	if err != nil {
		return nil, fmt.Errorf("query entities by type: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	result := make(map[string]int)
	for rows.Next() {
		var t string
		var c int
		if err := rows.Scan(&t, &c); err != nil {
			return nil, fmt.Errorf("scan entities by type row: %w", err)
		}
		result[t] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entities by type: %w", err)
	}
	return result, nil
}

// DomainsByCount returns a map of domain to entity count.
func (e *EntityDAO) DomainsByCount(ctx context.Context) (map[string]int, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT domain, COUNT(*) FROM entities GROUP BY domain`)
	if err != nil {
		return nil, fmt.Errorf("query entities by domain: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	result := make(map[string]int)
	for rows.Next() {
		var d string
		var c int
		if err := rows.Scan(&d, &c); err != nil {
			return nil, fmt.Errorf("scan entities by domain row: %w", err)
		}
		result[d] = c
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entities by domain: %w", err)
	}
	return result, nil
}

// UniqueTypes returns distinct entity types ordered alphabetically.
func (e *EntityDAO) UniqueTypes(ctx context.Context) ([]string, error) {
	rows, err := e.db.QueryContext(ctx, `SELECT DISTINCT type FROM entities ORDER BY type`)
	if err != nil {
		return nil, fmt.Errorf("query unique entity types: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var types []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan entity type row: %w", err)
		}
		types = append(types, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity types: %w", err)
	}
	if types == nil {
		types = []string{}
	}
	return types, nil
}

// ListCreatedSince returns all entities with created_at strictly after the given timestamp.
// The since parameter is an ISO-8601 datetime string (e.g., "2024-01-15 12:00:00").
func (e *EntityDAO) ListCreatedSince(ctx context.Context, since string) ([]Entity, error) {
	query := `SELECT id, type, name, domain, confidence, description, metadata_json, created_at FROM entities WHERE created_at > ? ORDER BY created_at`
	rows, err := e.db.QueryContext(ctx, query, since)
	if err != nil {
		return nil, fmt.Errorf("list entities created since %s: %w", since, err)
	}
	defer rows.Close() //nolint:errcheck

	var ents []Entity
	for rows.Next() {
		ent := Entity{}
		if err := rows.Scan(&ent.ID, &ent.Type, &ent.Name, &ent.Domain, &ent.Confidence, &ent.Description, &ent.MetadataJSON, &ent.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan entity row: %w", err)
		}
		ents = append(ents, ent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entities created since: %w", err)
	}

	return ents, nil
}
