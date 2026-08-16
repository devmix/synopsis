package search

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/graph"
)

// GraphExpander auto-expands retrieved entities by their graph relations.
type GraphExpander struct {
	factDAO      *dao.FactDAO
	graph        atomic.Pointer[graph.Graph]
	maxDepth     int
	maxNodes     int
	includeFacts bool
}

// NewGraphExpander creates a new graph expander.
func NewGraphExpander(db *sql.DB, g *graph.Graph, cfg config.GraphConfig) *GraphExpander {
	e := &GraphExpander{
		factDAO:      dao.NewFactDAO(db),
		maxDepth:     cfg.MaxDepth,
		maxNodes:     cfg.MaxNodes,
		includeFacts: true, // always include facts for enriched context
	}
	e.graph.Store(g)
	return e
}

// SetGraph swaps the underlying knowledge graph. Used by the auto-updater to
// refresh expansion data after re-indexing.
func (e *GraphExpander) SetGraph(g *graph.Graph) {
	e.graph.Store(g)
}

// Expand takes search results and expands entity nodes by their relations.
// Returns enriched search results with additional context from graph traversal.
func (e *GraphExpander) Expand(ctx context.Context, results []SearchResult) ([]SearchResult, error) {
	if e.graph.Load() == nil {
		return results, nil
	}

	expandedMap, err := e.expandEntities(ctx, results)
	if err != nil {
		// Non-fatal: log but continue with original results.
		slog.WarnContext(ctx, "graph expansion failed", "error", err)
		return results, nil
	}

	// Enrich each result with structured related_entities metadata.
	for i := range results {
		if len(results[i].Entities) == 0 {
			continue
		}

		var related []map[string]interface{}
		for _, ent := range results[i].Entities {
			exp, ok := expandedMap[ent.ID]
			if !ok {
				continue
			}

			entityData := map[string]interface{}{
				"entity_id": exp.Entity.ID,
				"name":      exp.Entity.Name,
				"domain":    exp.Entity.Domain,
			}

			if edges := e.serializeEdges(exp.Edges); len(edges) > 0 {
				entityData["edges"] = edges
			}

			if e.includeFacts && len(exp.Facts) > 0 {
				entityData["facts"] = e.serializeFacts(exp.Facts)
			}

			related = append(related, entityData)
		}

		if len(related) > 0 {
			if results[i].Metadata == nil {
				results[i].Metadata = make(map[string]interface{})
			}
			results[i].Metadata["related_entities"] = related
		}
	}

	return results, nil
}

// expandEntities finds all entities mentioned in search results and expands them.
// Returns a map from entity Predicate to ExpandedEntity (deduplicated by visited set).
func (e *GraphExpander) expandEntities(ctx context.Context, results []SearchResult) (map[int]*ExpandedEntity, error) {
	g := e.graph.Load()
	if g == nil {
		return nil, nil
	}

	expandedMap := make(map[int]*ExpandedEntity)
	visited := make(map[int]bool)
	var entityIDs []int

	for _, result := range results {
		for _, ent := range result.Entities {
			entityID := ent.ID
			if entityID == 0 || visited[entityID] {
				continue
			}
			visited[entityID] = true

			if _, ok := g.GetNode(entityID); !ok {
				continue
			}

			entityIDs = append(entityIDs, entityID)
		}
	}

	if len(entityIDs) == 0 {
		return expandedMap, nil
	}

	// Batch-load approved facts for all entities in a single query.
	factMap := make(map[int][]dao.Fact)
	if e.includeFacts {
		var err error
		factMap, err = e.factDAO.ListByEntityIDs(ctx, entityIDs)
		if err != nil {
			return nil, fmt.Errorf("batch load facts: %w", err)
		}
	}

	for _, entityID := range entityIDs {
		node, ok := g.GetNode(entityID)
		if !ok {
			continue
		}

		// Perform BFS to find related entities up to maxDepth.
		bfsResult, bfsErr := g.BFS(ctx, entityID, graph.BFSOptions{
			MaxDepth:  e.maxDepth,
			Direction: graph.DirectionBoth,
			MaxNodes:  e.maxNodes,
		})
		if bfsErr != nil {
			// Non-fatal: log cancellation; partial results in bfsResult are still used below.
			slog.WarnContext(ctx, "graph expansion cancelled", "entity_id", entityID, "error", bfsErr)
		}

		var edges []*graph.Edge
		if bfsResult != nil {
			edges = bfsResult.Edges
		}

		exp := &ExpandedEntity{
			Entity: node,
			Edges:  edges,
			Facts:  factMap[entityID],
		}
		expandedMap[entityID] = exp
	}

	return expandedMap, nil
}

// serializeEdges converts graph edges to a serializable format.
func (e *GraphExpander) serializeEdges(edges []*graph.Edge) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(edges))
	for _, edge := range edges {
		m := map[string]interface{}{
			"source_id":     edge.SourceID,
			"target_id":     edge.TargetID,
			"relation_type": edge.RelationType,
		}
		if edge.Method != "" {
			m["method"] = edge.Method
		}
		if edge.Confidence > 0 {
			m["confidence"] = edge.Confidence
		}
		if edge.Evidence != nil {
			m["evidence"] = *edge.Evidence
		}
		result = append(result, m)
	}
	return result
}

// serializeFacts converts facts to a serializable format.
func (e *GraphExpander) serializeFacts(facts []dao.Fact) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(facts))
	for _, f := range facts {
		factMap := map[string]interface{}{
			"id":                f.ID,
			"predicate":         f.Predicate,
			"subject_entity_id": f.SubjectEntityID,
			"object_entity_id":  f.ObjectEntityID,
		}
		if f.Metadata != nil {
			factMap["metadata"] = *f.Metadata
		}
		result = append(result, factMap)
	}
	return result
}

// ExpandedEntity represents an entity found in search results with its graph context.
type ExpandedEntity struct {
	Entity *graph.EntityNode
	Edges  []*graph.Edge
	Facts  []dao.Fact
}
