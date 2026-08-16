package dao

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/devmix/synopsis/internal/utils"
)

type Fact struct {
	ID              int     `json:"id"`
	SubjectEntityID int     `json:"subject_entity_id,omitempty"`
	Predicate       string  `json:"predicate"`
	ObjectEntityID  int     `json:"object_entity_id,omitempty"`
	Domain          string  `json:"domain"`
	Metadata        *string `json:"metadata,omitempty"` // JSON
	Status          string  `json:"status"`
	ValidFrom       *string `json:"valid_from,omitempty"`
	ValidTo         *string `json:"valid_to,omitempty"`
	Weight          int     `json:"weight"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// FactDAO provides CRUD operations for the facts table.
type FactDAO struct {
	db DBTX
}

// NewFactDAO creates a new FactDAO.
func NewFactDAO(db DBTX) *FactDAO {
	return &FactDAO{db: db}
}

// Create inserts a new fact and returns its Predicate.
func (d *FactDAO) Create(ctx context.Context, fact Fact) (int, error) {
	if fact.Predicate == "" {
		return 0, fmt.Errorf("predicate is required")
	}

	query := `INSERT INTO facts (subject_entity_id, predicate, object_entity_id, domain, metadata, status, valid_from, valid_to) VALUES (?, ?, ?, ?, ?, 'approved', ?, ?)`
	result, err := d.db.ExecContext(ctx, query,
		fact.SubjectEntityID, fact.Predicate, fact.ObjectEntityID, fact.Domain,
		fact.Metadata, fact.ValidFrom, fact.ValidTo,
	)
	if err != nil {
		return 0, fmt.Errorf("insert fact: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("get last insert id: %w", err)
	}
	return int(id), nil
}

// CreateOrIgnore inserts a fact, returning the Predicate of the new or existing row
// (UNIQUE(subject_entity_id, object_entity_id, predicate)).
func (d *FactDAO) CreateOrIgnore(ctx context.Context, fact Fact) (int, error) {
	if fact.Predicate == "" {
		return 0, fmt.Errorf("predicate is required")
	}

	query := `INSERT INTO facts (subject_entity_id, predicate, object_entity_id, domain, metadata, status, valid_from, valid_to) VALUES (?, ?, ?, ?, ?, 'approved', ?, ?) ON CONFLICT(subject_entity_id, object_entity_id, predicate) DO UPDATE SET subject_entity_id = subject_entity_id RETURNING id`
	var id int64
	err := d.db.QueryRowContext(ctx, query,
		fact.SubjectEntityID, fact.Predicate, fact.ObjectEntityID, fact.Domain,
		fact.Metadata, fact.ValidFrom, fact.ValidTo,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert or ignore fact: %w", err)
	}
	return int(id), nil
}

// RecomputeWeights sets weight = COUNT(fact_sources) for the given fact IDs.
func (d *FactDAO) RecomputeWeights(ctx context.Context, factIDs []int64) error {
	if len(factIDs) == 0 {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("UPDATE facts SET weight = (SELECT COUNT(*) FROM fact_sources WHERE fact_sources.fact_id = facts.id) WHERE id IN (")
	args := make([]any, 0, len(factIDs))
	for i, id := range factIDs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("?")
		args = append(args, id)
	}
	sb.WriteString(")")

	if _, err := d.db.ExecContext(ctx, sb.String(), args...); err != nil {
		return fmt.Errorf("recompute fact weights: %w", err)
	}
	return nil
}

// GetByID retrieves a single fact by Predicate.
func (d *FactDAO) GetByID(ctx context.Context, id int) (*Fact, error) {
	query := `SELECT id, subject_entity_id, predicate, object_entity_id, domain, metadata, status, valid_from, valid_to, weight, created_at, updated_at FROM facts WHERE id = ?`
	fact := &Fact{}
	err := d.db.QueryRowContext(ctx, query, id).Scan(
		&fact.ID, &fact.SubjectEntityID, &fact.Predicate,
		&fact.ObjectEntityID, &fact.Domain, &fact.Metadata, &fact.Status,
		&fact.ValidFrom, &fact.ValidTo, &fact.Weight,
		&fact.CreatedAt, &fact.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil // not found
	}
	if err != nil {
		return nil, fmt.Errorf("query fact %d: %w", id, err)
	}
	return fact, nil
}

// scanRows scans rows into a slice of Facts.
func (d *FactDAO) scanRows(rows *sql.Rows) ([]Fact, error) {
	var facts []Fact
	for rows.Next() {
		var f Fact
		if err := rows.Scan(
			&f.ID, &f.SubjectEntityID,
			&f.Predicate, &f.ObjectEntityID, &f.Domain, &f.Metadata,
			&f.Status, &f.ValidFrom, &f.ValidTo, &f.Weight,
			&f.CreatedAt, &f.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan fact: %w", err)
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating facts: %w", err)
	}
	return facts, nil
}

func (d *FactDAO) ListByEntityID(ctx context.Context, entityID int) ([]Fact, error) {
	query := `SELECT
				id, subject_entity_id, predicate, object_entity_id, domain, metadata,
				status, valid_from, valid_to, weight, created_at, updated_at
			FROM facts
			WHERE (subject_entity_id = ? OR object_entity_id = ?) AND status = 'approved' ORDER BY created_at DESC`
	rows, err := d.db.QueryContext(ctx, query, entityID, entityID)
	if err != nil {
		return nil, fmt.Errorf("list facts for entity %d: %w", entityID, err)
	}
	defer rows.Close() //nolint:errcheck

	return d.scanRows(rows)
}

// ListByEntityIDs retrieves approved facts for multiple entity IDs in a single query.
// Returns a map from entity Predicate to the slice of Facts where that entity appears as either
// subject or object. Empty input returns an empty map (not nil).
//
// Note: SQLite supports up to 32766 parameters per query. Typical expansion sizes
// (top_k ≤ 100 × ~10 entities) are well below this limit.
func (d *FactDAO) ListByEntityIDs(ctx context.Context, entityIDs []int) (map[int][]Fact, error) {
	if len(entityIDs) == 0 {
		return make(map[int][]Fact), nil
	}

	// Build placeholder list: "?, ?, ?"
	placeholders := make([]string, len(entityIDs))
	args := make([]any, len(entityIDs))
	for i, id := range entityIDs {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT
			id, subject_entity_id, predicate, object_entity_id, domain, metadata,
			status, valid_from, valid_to, weight, created_at, updated_at
		FROM facts
		WHERE (subject_entity_id IN (%s) OR object_entity_id IN (%s)) AND status = 'approved'
		ORDER BY created_at DESC`,
		strings.Join(placeholders, ", "), strings.Join(placeholders, ", "),
	)

	rows, err := d.db.QueryContext(ctx, query, append(args, args...)...)
	if err != nil {
		return nil, fmt.Errorf("list facts for entities batch: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	result := make(map[int][]Fact)
	for rows.Next() {
		var f Fact
		if err := rows.Scan(
			&f.ID, &f.SubjectEntityID, &f.Predicate,
			&f.ObjectEntityID, &f.Domain, &f.Metadata, &f.Status,
			&f.ValidFrom, &f.ValidTo, &f.Weight,
			&f.CreatedAt, &f.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan batch fact row: %w", err)
		}
		result[f.SubjectEntityID] = append(result[f.SubjectEntityID], f)
		if f.ObjectEntityID != f.SubjectEntityID {
			result[f.ObjectEntityID] = append(result[f.ObjectEntityID], f)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate batch facts: %w", err)
	}

	return result, nil
}

// ListAll returns all approved facts in the database. Used to load the knowledge graph.
func (d *FactDAO) ListAll(ctx context.Context) ([]Fact, error) {
	query := `SELECT
				id, subject_entity_id, predicate, object_entity_id, domain, metadata,
				status, valid_from, valid_to, weight, created_at, updated_at
			FROM facts WHERE status = 'approved' ORDER BY id`
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list all facts: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	return d.scanRows(rows)
}

// FindOrphanedFactIDs returns fact IDs that have no remaining fact_sources entries.
// If excludeApproved is true, facts with status='approved' are excluded from results.
// If candidateIDs is non-nil, the search is scoped to only those fact IDs (useful for
// document-level cleanup where you already know which facts were affected).
func (d *FactDAO) FindOrphanedFactIDs(ctx context.Context, excludeApproved bool, candidateIDs ...int64) ([]int64, error) {
	query := `SELECT f.id FROM facts f LEFT JOIN fact_sources fs ON f.id = fs.fact_id WHERE fs.id IS NULL`
	if len(candidateIDs) > 0 {
		var sb strings.Builder
		sb.WriteString(" AND f.id IN (")
		for i := range candidateIDs {
			if i > 0 {
				sb.WriteString(",")
			}
			sb.WriteString("?")
		}
		sb.WriteString(")")
		query += sb.String()
	}
	if excludeApproved {
		query += ` AND f.status != 'approved'`
	}

	var args []any
	if len(candidateIDs) > 0 {
		args = make([]any, 0, len(candidateIDs))
		for _, id := range candidateIDs {
			args = append(args, id)
		}
	}

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query orphaned facts: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan orphaned fact row: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orphaned facts: %w", err)
	}

	return ids, nil
}

// DeleteOrphanedFacts bulk-deletes facts by their IDs using a single DELETE WHERE id IN (...) query.
// Returns the number of rows deleted.
func (d *FactDAO) DeleteOrphanedFacts(ctx context.Context, factIDs []int64) (int64, error) {
	if len(factIDs) == 0 {
		return 0, nil
	}

	var sb strings.Builder
	sb.WriteString("DELETE FROM facts WHERE id IN (")
	args := make([]any, 0, len(factIDs))
	for i, id := range factIDs {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("?")
		args = append(args, id)
	}
	sb.WriteString(")")

	result, err := d.db.ExecContext(ctx, sb.String(), args...)
	if err != nil {
		return 0, fmt.Errorf("delete orphaned facts: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("check delete rows affected: %w", err)
	}
	return rowsAffected, nil
}

// ValidateFactDomain checks that the subject and object entities of a fact
// belong to the same domain as the fact's declared domain. Returns an error
// if either entity is missing or has a different domain. Domains are compared
// after normalization (case-insensitive, trimmed).
func (d *FactDAO) ValidateFactDomain(ctx context.Context, subjectID, objectID int, domain string) error {
	query := `SELECT e.domain FROM entities e WHERE e.id = ?`

	var subjDomain, objDomain string
	if err := d.db.QueryRowContext(ctx, query, subjectID).Scan(&subjDomain); err != nil {
		return fmt.Errorf("validate fact domain: check subject entity %d: %w", subjectID, err)
	}
	if err := d.db.QueryRowContext(ctx, query, objectID).Scan(&objDomain); err != nil {
		return fmt.Errorf("validate fact domain: check object entity %d: %w", objectID, err)
	}

	normDomain := utils.Normalize(domain)
	if utils.Normalize(subjDomain) != normDomain || utils.Normalize(objDomain) != normDomain {
		return fmt.Errorf(
			"fact domain mismatch: fact=%q subject=%q object=%q",
			domain, subjDomain, objDomain,
		)
	}
	return nil
}

// GetByIDs retrieves multiple facts by their IDs in a single batch query.
// The result maps each fact Predicate to its Fact; IDs that do not exist are absent from the map.
// An empty ids slice returns an empty map without error.
func (d *FactDAO) GetByIDs(ctx context.Context, ids []int) (map[int]*Fact, error) {
	if len(ids) == 0 {
		return make(map[int]*Fact), nil
	}

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}

	query := fmt.Sprintf(
		`SELECT id, subject_entity_id, predicate, object_entity_id, domain, metadata, status, valid_from, valid_to, weight, created_at, updated_at FROM facts WHERE id IN (%s)`,
		strings.Join(placeholders, ", "),
	)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("get facts by ids batch: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	facts := make(map[int]*Fact, len(ids))
	for rows.Next() {
		fact := &Fact{}
		if err := rows.Scan(
			&fact.ID, &fact.SubjectEntityID, &fact.Predicate,
			&fact.ObjectEntityID, &fact.Domain, &fact.Metadata, &fact.Status,
			&fact.ValidFrom, &fact.ValidTo, &fact.Weight,
			&fact.CreatedAt, &fact.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan fact row: %w", err)
		}
		facts[fact.ID] = fact
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate facts: %w", err)
	}

	return facts, nil
}

// Count returns the total number of facts.
func (d *FactDAO) Count(ctx context.Context) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM facts").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count facts: %w", err)
	}
	return count, nil
}

// SearchPaginated returns a page of facts with optional filters.
// predicateFilter: LIKE on predicate (case-insensitive substring).
// entityNameFilter: facts where subject OR object entity name matches (LIKE, case-insensitive).
// statusFilter: filter by status; empty string means no filter.
// domainFilter: filter by domain; empty string means no filter.
func (d *FactDAO) SearchPaginated(ctx context.Context, offset, limit int, predicateFilter, entityNameFilter, statusFilter, domainFilter string) ([]Fact, int, error) {
	baseQuery := `SELECT f.id, f.subject_entity_id, f.predicate, f.object_entity_id, f.domain, f.metadata, f.status, f.valid_from, f.valid_to, f.weight, f.created_at, f.updated_at FROM facts f`

	var whereClauses []string
	args := []any{}
	countArgs := []any{}

	if predicateFilter != "" {
		whereClauses = append(whereClauses, "lower(f.predicate) LIKE lower(?) ESCAPE '\\'")
		pattern := "%" + utils.EscapeLike(predicateFilter) + "%"
		args = append(args, pattern)
		countArgs = append(countArgs, pattern)
	}

	if entityNameFilter != "" {
		// Join with entities to filter by subject or object name.
		baseQuery += ` INNER JOIN entities e ON (e.id = f.subject_entity_id OR e.id = f.object_entity_id)`
		whereClauses = append(whereClauses, "lower(e.name) LIKE lower(?) ESCAPE '\\'")
		pattern := "%" + utils.EscapeLike(entityNameFilter) + "%"
		args = append(args, pattern)
		countArgs = append(countArgs, pattern)
	}

	if statusFilter != "" {
		whereClauses = append(whereClauses, "f.status = ?")
		args = append(args, statusFilter)
		countArgs = append(countArgs, statusFilter)
	}

	if domainFilter != "" {
		whereClauses = append(whereClauses, "f.domain = ?")
		args = append(args, domainFilter)
		countArgs = append(countArgs, domainFilter)
	}

	query := baseQuery
	countQuery := `SELECT COUNT(*) FROM facts f`
	if entityNameFilter != "" {
		countQuery = `SELECT COUNT(DISTINCT f.id) FROM facts f`
		countQuery += ` INNER JOIN entities e ON (e.id = f.subject_entity_id OR e.id = f.object_entity_id)`
	}

	if len(whereClauses) > 0 {
		where := " WHERE " + strings.Join(whereClauses, " AND ")
		query += where
		countQuery += where
	}

	query += ` ORDER BY f.id LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("search facts: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	var facts []Fact
	for rows.Next() {
		var f Fact
		if err := rows.Scan(
			&f.ID, &f.SubjectEntityID, &f.Predicate,
			&f.ObjectEntityID, &f.Domain, &f.Metadata, &f.Status,
			&f.ValidFrom, &f.ValidTo, &f.Weight,
			&f.CreatedAt, &f.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan fact row: %w", err)
		}
		facts = append(facts, f)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate facts: %w", err)
	}

	var totalCount int
	if err := d.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("count search facts: %w", err)
	}

	return facts, totalCount, nil
}
