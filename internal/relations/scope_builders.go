package relations

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"

	"github.com/devmix/synopsis/internal/database/dao"
)

// FactIndex provides fast lookup of facts by entity Predicate and predicate.
type FactIndex struct {
	entityPred map[int]map[string][]dao.Fact
}

func buildFactIndex(ctx context.Context, db *sql.DB) (*FactIndex, error) {
	facts, err := dao.NewFactDAO(db).ListAll(ctx)
	if err != nil {
		return nil, fmt.Errorf("load facts: %w", err)
	}

	idx := &FactIndex{entityPred: make(map[int]map[string][]dao.Fact)}
	for _, f := range facts {
		for _, eid := range []int{f.SubjectEntityID, f.ObjectEntityID} {
			if eid == 0 {
				continue
			}
			if idx.entityPred[eid] == nil {
				idx.entityPred[eid] = make(map[string][]dao.Fact)
			}
			idx.entityPred[eid][f.Predicate] = append(idx.entityPred[eid][f.Predicate], f)
		}
	}

	return idx, nil
}

// Lookup returns facts for the given entity and predicate.
func (fi *FactIndex) Lookup(entityID int, predicate string) []dao.Fact {
	if fi == nil || fi.entityPred == nil {
		return nil
	}
	predMap := fi.entityPred[entityID]
	if predMap == nil {
		return nil
	}
	return predMap[predicate]
}

// ChunkIndex provides fast lookup of chunk texts by entity Predicate.
type ChunkIndex struct {
	entityChunks map[int][]string
}

func buildChunkIndex(ctx context.Context, db *sql.DB) (*ChunkIndex, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT ce.entity_id, c.chunk_text
		FROM chunk_entities ce
		INNER JOIN chunks c ON c.id = ce.chunk_id
		ORDER BY ce.entity_id, c.sequence_num`)
	if err != nil {
		return nil, fmt.Errorf("query chunk texts: %w", err)
	}
	defer rows.Close() //nolint:errcheck

	idx := &ChunkIndex{entityChunks: make(map[int][]string)}
	for rows.Next() {
		var entityID int
		var text string
		if err := rows.Scan(&entityID, &text); err != nil {
			return nil, fmt.Errorf("scan chunk row: %w", err)
		}
		idx.entityChunks[entityID] = append(idx.entityChunks[entityID], text)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chunk rows: %w", err)
	}

	return idx, nil
}

// Texts returns all chunk texts for the given entity Predicate.
func (ci *ChunkIndex) Texts(entityID int) []string {
	if ci == nil || ci.entityChunks == nil {
		return nil
	}
	return ci.entityChunks[entityID]
}

// Contains checks if any chunk text for the entity contains the given substring.
func (ci *ChunkIndex) Contains(entityID int, text string) bool {
	if ci == nil || ci.entityChunks == nil {
		return false
	}
	for _, t := range ci.entityChunks[entityID] {
		if strings.Contains(t, text) {
			return true
		}
	}
	return false
}

// AdjacencyEntry represents a neighbor in the graph index.
type AdjacencyEntry struct {
	EntityID     int
	RelationType string
}

// GraphIndex provides incremental BFS layer construction for path finding.
// It is safe for concurrent read access.
type GraphIndex struct {
	mu      sync.RWMutex
	layers  map[int]map[int][]AdjacencyEntry // hop → entity_id → neighbors
	maxHops int
}

func buildGraphIndex(ctx context.Context, db *sql.DB, maxHops int) (*GraphIndex, error) {
	if maxHops < 0 {
		return nil, fmt.Errorf("maxHops must be non-negative, got %d", maxHops)
	}

	gi := &GraphIndex{
		layers:  make(map[int]map[int][]AdjacencyEntry),
		maxHops: maxHops,
	}

	// Hop 0: all entities are their own neighbors (identity layer).
	ents, err := dao.NewEntityDAO(db).List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list entities for graph index: %w", err)
	}

	gi.layers[0] = make(map[int][]AdjacencyEntry, len(ents))
	for _, e := range ents {
		gi.layers[0][e.ID] = []AdjacencyEntry{{EntityID: e.ID, RelationType: "self"}}
	}

	if maxHops > 0 {
		if err := gi.Extend(ctx, db, maxHops); err != nil {
			return nil, fmt.Errorf("extend graph index: %w", err)
		}
	}

	return gi, nil
}

// Extend adds BFS layers up to targetHops. Already-built hops are skipped.
func (gi *GraphIndex) Extend(ctx context.Context, db *sql.DB, targetHops int) error {
	gi.mu.Lock()
	defer gi.mu.Unlock()

	currentHop := 0
	for h := range gi.layers {
		if h > currentHop && h <= targetHops {
			currentHop = h
		}
	}

	linkDAO := dao.NewEntityLinkDAO(db)

	for hop := currentHop + 1; hop <= targetHops; hop++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		prevLayer := gi.layers[hop-1]
		if prevLayer == nil {
			break
		}

		nextLayer := make(map[int][]AdjacencyEntry)

		for entityID := range prevLayer {
			links, err := linkDAO.ListByEntity(ctx, entityID)
			if err != nil {
				return fmt.Errorf("list links for entity %d at hop %d: %w", entityID, hop, err)
			}

			for _, link := range links {
				var neighborID int
				if link.SubjectEntityID == entityID {
					neighborID = link.TargetEntityID
				} else {
					neighborID = link.SubjectEntityID
				}

				// Skip if already seen at a lower hop.
				if gi.layers[hop-1][neighborID] != nil {
					continue
				}

				nextLayer[neighborID] = append(nextLayer[neighborID], AdjacencyEntry{
					EntityID:     neighborID,
					RelationType: link.RelationType,
				})
			}
		}

		gi.layers[hop] = nextLayer
	}

	return nil
}

// Neighbors returns entity IDs reachable from the given entity within exactly hops distance.
func (gi *GraphIndex) Neighbors(entityID int, hops int) []int {
	if gi == nil || hops < 0 {
		return nil
	}

	gi.mu.RLock()
	defer gi.mu.RUnlock()

	layer := gi.layers[hops]
	if layer == nil {
		return nil
	}

	entries := layer[entityID]
	if len(entries) == 0 {
		return nil
	}

	ids := make([]int, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.EntityID)
	}
	return ids
}

// PathExists checks if there is a path between two entities within maxHops.
// If relationType is non-empty, only links matching that type are considered.
func (gi *GraphIndex) PathExists(aID, bID, maxHops int, relationType string) bool {
	if gi == nil || maxHops < 0 {
		return false
	}

	gi.mu.RLock()
	defer gi.mu.RUnlock()

	if aID == bID && gi.layers[0][aID] != nil {
		return true
	}

	for hop := 1; hop <= maxHops; hop++ {
		layer := gi.layers[hop]
		if layer == nil {
			break
		}

		aNeighbors := layer[aID]
		bNeighbors := layer[bID]

		for _, aEntry := range aNeighbors {
			if relationType != "" && aEntry.RelationType != relationType {
				continue
			}
			for _, bEntry := range bNeighbors {
				if relationType != "" && bEntry.RelationType != relationType {
					continue
				}
				if aEntry.EntityID == bEntry.EntityID || aEntry.EntityID == bID || bEntry.EntityID == aID {
					return true
				}
			}
		}

		// Also check if b is directly reachable from a at this hop.
		for _, entry := range layer[aID] {
			if relationType != "" && entry.RelationType != relationType {
				continue
			}
			if entry.EntityID == bID {
				return true
			}
		}
		for _, entry := range layer[bID] {
			if relationType != "" && entry.RelationType != relationType {
				continue
			}
			if entry.EntityID == aID {
				return true
			}
		}
	}

	return false
}
