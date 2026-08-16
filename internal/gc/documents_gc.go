// Package gc provides garbage collection operations for document data cleanup.
package gc

import (
	"context"
	"fmt"
	"strings"

	"github.com/devmix/synopsis/internal/database/dao"
)

// DocumentGC provides garbage collection operations for document data.
// It wraps the necessary DAOs to perform cleanup in a single interface.
type DocumentGC struct {
	db              dao.DBTX
	entitySourceDAO *dao.EntitySourceDAO
	factSourceDAO   *dao.FactSourceDAO
}

// NewDocumentGC creates a DocumentGC bound to the given database connection or transaction.
func NewDocumentGC(db dao.DBTX) *DocumentGC {
	return &DocumentGC{
		db:              db,
		entitySourceDAO: dao.NewEntitySourceDAO(db),
		factSourceDAO:   dao.NewFactSourceDAO(db),
	}
}

// FindAndDeleteOrphanedFacts finds and deletes facts that have no fact_sources,
// scoped to the given candidate fact IDs. Returns the number of facts deleted.
// Used during document refresh to clean up facts whose sources were just removed.
func (g *DocumentGC) FindAndDeleteOrphanedFacts(ctx context.Context, candidateFactIDs []int64) (int64, error) {
	if len(candidateFactIDs) == 0 {
		return 0, nil
	}

	factDAO := dao.NewFactDAO(g.db)

	orphanedFacts, err := factDAO.FindOrphanedFactIDs(ctx, false, candidateFactIDs...)
	if err != nil {
		return 0, fmt.Errorf("find orphaned facts: %w", err)
	}

	if len(orphanedFacts) == 0 {
		return 0, nil
	}

	deleted, err := factDAO.DeleteOrphanedFacts(ctx, orphanedFacts)
	if err != nil {
		return 0, fmt.Errorf("delete %d orphaned facts: %w", len(orphanedFacts), err)
	}

	return deleted, nil
}

// DeleteOrphanedEntityIDs batch-deletes entities that have no entity_sources links.
// Uses a single DELETE with subquery. Excludes EntityType entities.
// Returns the number of entities deleted.
func (g *DocumentGC) DeleteOrphanedEntityIDs(ctx context.Context) (int64, error) {
	entityDAO := dao.NewEntityDAO(g.db)
	return entityDAO.DeleteOrphanedEntityIDs(ctx)
}

// DeleteOrphanedFacts batch-deletes facts that have no fact_sources,
// excluding approved facts. Returns the number of facts deleted.
func (g *DocumentGC) DeleteOrphanedFacts(ctx context.Context) (int64, error) {
	factDAO := dao.NewFactDAO(g.db)

	orphanedFactIDs, err := factDAO.FindOrphanedFactIDs(ctx, true) // excludeApproved=true
	if err != nil {
		return 0, fmt.Errorf("find orphaned facts: %w", err)
	}

	if len(orphanedFactIDs) == 0 {
		return 0, nil
	}

	deleted, err := factDAO.DeleteOrphanedFacts(ctx, orphanedFactIDs)
	if err != nil {
		return 0, fmt.Errorf("delete %d orphaned facts: %w", len(orphanedFactIDs), err)
	}

	return deleted, nil
}

// FullClearDocByID removes all per-document data for the given document Predicate.
// Order matters to respect FK constraints (foreign_keys=ON in database.go):
//  1. entity_sources → collect affected entity IDs
//  2. fact_sources → collect affected fact IDs
//  3. orphaned facts (scoped to affected fact IDs)
//  4. weight decrease: recompute weights for surviving facts whose sources were removed
//  5. chunks + vectors (chunk_entities cascade via FK)
//  6. scoped entity orphan cleanup: DeleteOrphanedByIDs(ctx, affectedEntityIDs) — deletes ONLY
//     candidates of this document that have no remaining entity_sources, excluding EntityType
//     and entities referenced by any fact (mandatory — facts→entities FK has no CASCADE).
func (g *DocumentGC) FullClearDocByID(ctx context.Context, docID int) error {
	// 1. Remove entity sources for this document; collect affected entity IDs.
	entityIDs, err := g.entitySourceDAO.DeleteByDocumentID(ctx, docID)
	if err != nil {
		return fmt.Errorf("delete entity sources for doc %d: %w", docID, err)
	}

	// 2. Remove fact sources and collect affected fact IDs.
	factIDs, err := g.factSourceDAO.DeleteByDocumentID(ctx, docID)
	if err != nil {
		return fmt.Errorf("delete fact sources for doc %d: %w", docID, err)
	}

	// 3. Scoped orphan cleanup for affected facts only.
	var factIDs64 []int64
	if len(factIDs) > 0 {
		factIDs64 = make([]int64, len(factIDs))
		for i, id := range factIDs {
			factIDs64[i] = int64(id)
		}
		if _, err := g.FindAndDeleteOrphanedFacts(ctx, factIDs64); err != nil {
			return fmt.Errorf("delete orphaned facts for doc %d: %w", docID, err)
		}
	}

	// 4. Weight decrease: surviving facts (sources in other documents) get their weight
	// recomputed downward; UPDATE over already-deleted fact IDs is a harmless no-op.
	if len(factIDs64) > 0 {
		factDAO := dao.NewFactDAO(g.db)
		if err := factDAO.RecomputeWeights(ctx, factIDs64); err != nil {
			return fmt.Errorf("recompute weights for doc %d: %w", docID, err)
		}
	}

	// 5. Remove chunks and their vectors (chunk_entities cascade via FK).
	chunkDAO := dao.NewChunkDAO(g.db)
	chunks, err := chunkDAO.ListByDocID(ctx, docID)
	if err != nil {
		return fmt.Errorf("list chunks for doc %d: %w", docID, err)
	}

	var chunkIDs []int
	for _, ch := range chunks {
		chunkIDs = append(chunkIDs, ch.ID)
	}

	if len(chunkIDs) > 0 {
		// Delete vectors; ignore "no such table" errors when sqlite-vec is unavailable.
		if err := chunkDAO.DeleteVectorsByChunkIDs(ctx, chunkIDs); err != nil && !isNoSuchTable(err) {
			return fmt.Errorf("delete vectors for doc %d: %w", docID, err)
		}
		if err := chunkDAO.DeleteByIDs(ctx, chunkIDs); err != nil {
			return fmt.Errorf("delete chunks for doc %d: %w", docID, err)
		}
	}

	// 6. Scoped entity orphan cleanup: delete only candidates from this document that have
	// no remaining entity_sources, excluding EntityType and entities referenced by facts.
	if len(entityIDs) > 0 {
		entityDAO := dao.NewEntityDAO(g.db)
		if _, err := entityDAO.DeleteOrphanedByIDs(ctx, entityIDs); err != nil {
			return fmt.Errorf("delete orphaned entities for doc %d: %w", docID, err)
		}
	}

	return nil
}

// DeleteOrphanedDocuments removes documents that have no chunks, no entity_sources,
// and no fact_sources. Returns the number of documents deleted.
func (g *DocumentGC) DeleteOrphanedDocuments(ctx context.Context) (int64, error) {
	query := `DELETE FROM documents WHERE id NOT IN (SELECT DISTINCT doc_id FROM chunks)
		AND id NOT IN (SELECT DISTINCT entity_id FROM entity_sources)
		AND id NOT IN (SELECT DISTINCT CAST(document_id AS INTEGER) FROM fact_sources WHERE document_id IS NOT NULL)`

	result, err := g.db.ExecContext(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("delete orphaned documents: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("check delete rows affected: %w", err)
	}
	return rowsAffected, nil
}

// isNoSuchTable checks if an error indicates a missing SQLite table.
func isNoSuchTable(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "no such table")
}
