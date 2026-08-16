// Package entities implements entity resolution: deduplication of
// NER-extracted entities by name similarity and persistence with
// document provenance.
package entities

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/ingestion/ner"
	"github.com/devmix/synopsis/internal/utils"
)

// Resolver deduplicates entities by name similarity against both the current
// batch and previously persisted entities, then records document provenance.
// The resolver is stateless with respect to the database: persistence uses
// the DBTX passed to AddEntities, so DAOs can be constructed over either a
// connection pool or a transaction. The in-memory blocking index is the only
// long-lived state and is protected by an internal mutex.
// Keys are domain-qualified: "domain:type:bigram" for blocks,
// "domain:name" for names map.
type Resolver struct {
	mu                  sync.RWMutex
	names               map[string]int   // "domain:normalized_name" -> entity Predicate
	byID                map[int]string   // entity Predicate -> canonical name
	blocks              map[string][]int // "domain:type:bigram" -> entity IDs
	domains             map[int]string   // entity Predicate -> domain
	hydrated            bool
	similarityThreshold float64
}

// NewResolver creates an entity resolver with the given similarity similarityThreshold
// and an empty blocking index.
func NewResolver(similarityThreshold float64) *Resolver {
	return &Resolver{
		similarityThreshold: similarityThreshold,
		names:               make(map[string]int),
		byID:                make(map[int]string),
		blocks:              make(map[string][]int),
		domains:             make(map[int]string),
	}
}

// Lookup resolves each entity against the hydrated index WITHOUT creating
// anything. Returns IDs aligned with the input slice; 0 means "not found".
func (r *Resolver) Lookup(ctx context.Context, dbtx dao.DBTX, entities []ner.Entity) ([]int, error) {
	if len(entities) == 0 {
		return nil, nil
	}

	entityDAO := dao.NewEntityDAO(dbtx)

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hydrate(ctx, entityDAO); err != nil {
		return nil, err
	}

	ids := make([]int, len(entities))
	for i, ent := range entities {
		id, _, score := r.findBestCandidate(ent)
		if score >= r.similarityThreshold {
			ids[i] = id
		} else {
			ids[i] = 0
		}
	}
	return ids, nil
}

// LookupOrCreate resolves each entity against the hydrated index. Existing
// entities return their Predicate; missing entities are created as synthetic entities
// with deduplication semantics identical to AddEntities. Created entities are
// linked to docID via entity_sources and indexed in-memory immediately.
// Returns IDs aligned with the input slice (no 0 entries for created ones).
func (r *Resolver) LookupOrCreate(ctx context.Context, dbtx dao.DBTX, docID int, entities []ner.Entity) ([]int, error) {
	ids, _, err := r.LookupOrCreateWithStats(ctx, dbtx, docID, entities)
	return ids, err
}

// LookupOrCreateWithStats resolves each entity against the hydrated index and
// returns both the resolved IDs and the count of newly created (synthetic)
// entities. Existing entities return their Predicate; missing entities are created as
// synthetic entities with deduplication semantics identical to AddEntities.
// Created entities are linked to docID via entity_sources and indexed in-memory
// immediately. Returns IDs aligned with the input slice and the number of
// newly created entities (not total resolved).
func (r *Resolver) LookupOrCreateWithStats(ctx context.Context, dbtx dao.DBTX, docID int, entities []ner.Entity) ([]int, int, error) {
	if len(entities) == 0 {
		return nil, 0, nil
	}
	if docID <= 0 {
		return nil, 0, fmt.Errorf("invalid document Predicate %d", docID)
	}

	entityDAO := dao.NewEntityDAO(dbtx)
	entitySourceDAO := dao.NewEntitySourceDAO(dbtx)

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hydrate(ctx, entityDAO); err != nil {
		return nil, 0, err
	}

	ids := make([]int, len(entities))
	newIDs := make([]int, 0, len(entities))

	for i, ent := range entities {
		id, _, score := r.findBestCandidate(ent)
		if score >= r.similarityThreshold {
			ids[i] = id
		} else {
			// Create synthetic entity using the same resolveOne logic as AddEntities.
			resolvedID, _, err := r.resolveOne(ctx, entityDAO, ent)
			if err != nil {
				return nil, 0, fmt.Errorf("create synthetic entity %q: %w", ent.Name, err)
			}
			ids[i] = resolvedID
			newIDs = append(newIDs, resolvedID)
		}
	}

	// Link newly created entities to the document for provenance.
	if len(newIDs) > 0 {
		if err := entitySourceDAO.LinkBatch(ctx, docID, newIDs); err != nil {
			return nil, 0, fmt.Errorf("link synthetic entities to document %d: %w", docID, err)
		}
	}

	return ids, len(newIDs), nil
}

// AddEntities normalizes, deduplicates, and persists the given NER entities.
// All database access goes through dbtx (a *sql.DB or *sql.Tx), so callers
// can run resolution inside their own transaction.
// Each resolved canonical entity is linked to docID for provenance.
// Returns the resolved entities with their database IDs.
func (r *Resolver) AddEntities(ctx context.Context, dbtx dao.DBTX, docID int, entities []ner.Entity) ([]dao.Entity, error) {
	if len(entities) == 0 {
		return nil, nil
	}
	if docID <= 0 {
		return nil, fmt.Errorf("invalid document Predicate %d", docID)
	}

	entityDAO := dao.NewEntityDAO(dbtx)
	entitySourceDAO := dao.NewEntitySourceDAO(dbtx)

	r.mu.Lock()
	defer r.mu.Unlock()

	if err := r.hydrate(ctx, entityDAO); err != nil {
		return nil, err
	}

	clusters := clusterBatch(entities, r.similarityThreshold)

	resolved := make([]dao.Entity, 0, len(clusters))
	ids := make([]int, 0, len(clusters))

	for _, cluster := range clusters {
		canonical := canonicalProto(cluster)
		id, ent, err := r.resolveOne(ctx, entityDAO, canonical)
		if err != nil {
			return nil, err
		}
		if !containsInt(ids, id) {
			ids = append(ids, id)
			resolved = append(resolved, *ent)
		}
	}

	if err := entitySourceDAO.LinkBatch(ctx, docID, ids); err != nil {
		return nil, fmt.Errorf("link entities to document %d: %w", docID, err)
	}

	return resolved, nil
}

// hydrate lazily builds the in-memory blocking index from the database
// on the first call. Subsequent calls only see incremental updates.
func (r *Resolver) hydrate(ctx context.Context, entityDAO *dao.EntityDAO) error {
	if r.hydrated {
		return nil
	}

	ents, err := entityDAO.List(ctx)
	if err != nil {
		return fmt.Errorf("hydrate entity index: %w", err)
	}
	for i := range ents {
		r.index(&ents[i])
	}
	r.hydrated = true
	return nil
}

// rehydrate rebuilds the blocking index from the database, discarding
// in-memory state. Used to recover from entities deleted mid-run by GC.
func (r *Resolver) rehydrate(ctx context.Context, entityDAO *dao.EntityDAO) error {
	r.names = make(map[string]int)
	r.byID = make(map[int]string)
	r.blocks = make(map[string][]int)
	r.domains = make(map[int]string)
	r.hydrated = false
	return r.hydrate(ctx, entityDAO)
}

// index registers an entity in the in-memory blocking index.
func (r *Resolver) index(ent *dao.Entity) {
	dom := utils.Normalize(ent.Domain)
	normKey := dom + ":" + normalizeName(ent.Name)
	r.names[normKey] = ent.ID
	r.byID[ent.ID] = ent.Name
	r.domains[ent.ID] = dom

	for bg := range getBigrams(ent.Name) {
		key := dom + ":" + ent.Type + ":" + bg
		r.blocks[key] = append(r.blocks[key], ent.ID)
	}
}

// resolveOne merges the given entity into an existing canonical entity when a
// similar candidate is found, otherwise creates a new one.
func (r *Resolver) resolveOne(ctx context.Context, entityDAO *dao.EntityDAO, ent ner.Entity) (int, *dao.Entity, error) {
	ent.Domain = utils.Normalize(ent.Domain)
	id, name, score := r.findBestCandidate(ent)

	if score >= r.similarityThreshold {
		existing, err := entityDAO.GetByID(ctx, id)
		if err != nil {
			return 0, nil, fmt.Errorf("load candidate entity %d: %w", id, err)
		}
		if existing == nil {
			if err := r.rehydrate(ctx, entityDAO); err != nil {
				return 0, nil, err
			}
			return r.resolveOne(ctx, entityDAO, ent)
		}

		if len(ent.Name) > len(name) {
			if err := entityDAO.UpdateName(ctx, id, ent.Name); err != nil {
				return 0, nil, fmt.Errorf("promote canonical name %q: %w", ent.Name, err)
			}
			oldNormKey := r.domains[id] + ":" + normalizeName(name)
			newNormKey := r.domains[id] + ":" + normalizeName(ent.Name)
			r.names[oldNormKey] = id
			r.names[newNormKey] = id
			r.byID[id] = ent.Name
			for bg := range getBigrams(ent.Name) {
				key := r.domains[id] + ":" + ent.Type + ":" + bg
				r.blocks[key] = append(r.blocks[key], id)
			}
		}

		existing.Name = r.byID[id]
		return id, existing, nil
	}

	description := ent.Description
	var metadataJSON *string
	if len(ent.Metadata) > 0 {
		scopedMetadata := scopeEntityMetadata(ent.Name, ent.Metadata)
		if len(scopedMetadata) > 0 {
			raw, err := json.Marshal(scopedMetadata)
			if err == nil {
				s := string(raw)
				metadataJSON = &s
			}
		}
	}
	createdID, err := entityDAO.GetOrCreate(ctx, ent.Name, ent.Type, ent.Domain, ent.Confidence, metadataJSON, &description)
	if err != nil {
		return 0, nil, fmt.Errorf("create entity %q: %w", ent.Name, err)
	}

	stub := &dao.Entity{ID: createdID, Type: ent.Type, Name: ent.Name, Domain: ent.Domain}
	r.index(stub)

	return createdID, &dao.Entity{
		ID:          createdID,
		Type:        ent.Type,
		Name:        ent.Name,
		Domain:      ent.Domain,
		Confidence:  ent.Confidence,
		Description: &description,
	}, nil
}

// findBestCandidate returns the Predicate, canonical name, and similarity score of
// the most similar persisted entity of the same type AND domain as ent.
func (r *Resolver) findBestCandidate(ent ner.Entity) (int, string, float64) {
	dom := utils.Normalize(ent.Domain)
	normKey := dom + ":" + normalizeName(ent.Name)
	if id, ok := r.names[normKey]; ok {
		return id, r.byID[id], 1.0
	}

	bestID := 0
	bestName := ""
	bestScore := 0.0

	for bg := range getBigrams(ent.Name) {
		key := dom + ":" + ent.Type + ":" + bg
		for _, id := range r.blocks[key] {
			name := r.byID[id]
			score := jaroWinkler(ent.Name, name)
			if score > bestScore {
				bestID = id
				bestName = name
				bestScore = score
			}
		}
	}

	return bestID, bestName, bestScore
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// canonicalProto returns the cluster member with the longest, most complete
// name; ties resolve to the first encountered member.
func canonicalProto(cluster []ner.Entity) ner.Entity {
	best := cluster[0]
	for _, ent := range cluster[1:] {
		if len(ent.Name) > len(best.Name) {
			best = ent
		}
	}
	return best
}

// clusterBatch groups entities by similarity using bigram blocking and
// union-find, so that transitively similar names land in the same cluster.
// Entities from different domains are never clustered together (domain-qualified keys).
func clusterBatch(entities []ner.Entity, threshold float64) [][]ner.Entity {
	n := len(entities)
	uf := newUnionFind(n)

	blocks := make(map[string][]int)
	for i, ent := range entities {
		dom := utils.Normalize(ent.Domain)
		for bg := range getBigrams(ent.Name) {
			key := dom + ":" + ent.Type + ":" + bg
			blocks[key] = append(blocks[key], i)
		}
	}

	checked := make(map[[2]int]struct{})
	for _, idxs := range blocks {
		if len(idxs) < 2 {
			continue
		}
		for i := 0; i < len(idxs); i++ {
			for j := i + 1; j < len(idxs); j++ {
				a, b := idxs[i], idxs[j]
				if a == b {
					continue
				}
				if a > b {
					a, b = b, a
				}
				pair := [2]int{a, b}
				if _, ok := checked[pair]; ok {
					continue
				}
				checked[pair] = struct{}{}

				if jaroWinkler(entities[a].Name, entities[b].Name) >= threshold {
					uf.union(a, b)
				}
			}
		}
	}

	groups := make(map[int][]ner.Entity)
	var order []int
	for i := 0; i < n; i++ {
		root := uf.find(i)
		if _, ok := groups[root]; !ok {
			order = append(order, root)
		}
		groups[root] = append(groups[root], entities[i])
	}

	clusters := make([][]ner.Entity, 0, len(order))
	for _, root := range order {
		clusters = append(clusters, groups[root])
	}
	return clusters
}

// scopeEntityMetadata filters entity metadata to contain only entity-scoped fields.
// Document-level fields (url, image_paths, page_links, categories) are removed.
// Provenance fields (source_file, source_type, space) are KEPT as per approved decision.
// The "title" field is set to the entity name if present in the original metadata and is a string.
func scopeEntityMetadata(entityName string, raw map[string]interface{}) map[string]interface{} {
	// Fields that belong to document context and must NOT be copied into entity metadata.
	docLevelFields := map[string]struct{}{
		"url":         {},
		"image_paths": {},
		"page_links":  {},
		"categories":  {},
	}

	scoped := make(map[string]interface{})
	for k, v := range raw {
		if _, isDocLevel := docLevelFields[strings.ToLower(k)]; isDocLevel {
			continue
		}
		scoped[k] = v
	}

	// Set title to entity name if a "title" key exists in the original metadata and is a string.
	if origTitle, hasTitle := raw["title"]; hasTitle {
		if _, ok := origTitle.(string); ok {
			scoped["title"] = entityName
		}
	}

	return scoped
}

// unionFind is a disjoint-set data structure with path compression.
type unionFind struct {
	parent []int
}

func newUnionFind(size int) *unionFind {
	parent := make([]int, size)
	for i := range parent {
		parent[i] = i
	}
	return &unionFind{parent: parent}
}

func (uf *unionFind) find(i int) int {
	if uf.parent[i] != i {
		uf.parent[i] = uf.find(uf.parent[i])
	}
	return uf.parent[i]
}

func (uf *unionFind) union(i, j int) {
	ri, rj := uf.find(i), uf.find(j)
	if ri != rj {
		uf.parent[ri] = rj
	}
}
