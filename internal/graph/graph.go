package graph

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/utils"
)

// Graph is an in-memory representation of the entity relation graph.
// It uses adjacency lists for O(1) edge traversal and hash maps for fast lookup.
type Graph struct {
	outgoingEdges            map[int][]*Edge           // nodeID -> outgoing edges
	incomingEdges            map[int][]*Edge           // nodeID -> incoming edges
	outgoingCrossDomainEdges map[int][]*EntityLinkEdge // sourceID -> entity link edges (cross-domain)
	incomingCrossDomainEdges map[int][]*EntityLinkEdge // targetID -> incoming entity link edges (inverse index)
	entityLinkTargetSet      map[int]map[int]bool      // sourceID -> set of targetIDs for O(1) existence check
	nodes                    map[int]*EntityNode
	nameToID                 map[string]int   // "domain:lowercase_name" -> nodeID (O(1) lookup)
	typeToNodes              map[string][]int // entity type -> list of nodeIDs
}

// NewGraphFromDB loads all entities and relations from the database into an in-memory graph.
// Returns the populated graph and load statistics.
func NewGraphFromDB(ctx context.Context, db *sql.DB) (*Graph, *GraphStats, error) {
	start := time.Now()

	g := &Graph{
		outgoingEdges:            make(map[int][]*Edge),
		incomingEdges:            make(map[int][]*Edge),
		outgoingCrossDomainEdges: make(map[int][]*EntityLinkEdge),
		incomingCrossDomainEdges: make(map[int][]*EntityLinkEdge),
		entityLinkTargetSet:      make(map[int]map[int]bool),
		nodes:                    make(map[int]*EntityNode),
		nameToID:                 make(map[string]int),
		typeToNodes:              make(map[string][]int),
	}

	if err := g.loadEntities(ctx, dao.NewEntityDAO(db)); err != nil {
		return nil, nil, fmt.Errorf("load entities: %w", err)
	}

	if err := g.loadRelations(ctx, dao.NewFactDAO(db)); err != nil {
		return nil, nil, fmt.Errorf("load relations: %w", err)
	}

	if err := g.loadEntityLinks(ctx, dao.NewEntityLinkDAO(db)); err != nil {
		return nil, nil, fmt.Errorf("load entity links: %w", err)
	}

	stats := &GraphStats{
		NodeCount:    len(g.nodes),
		EdgeCount:    edgeCount(g.outgoingEdges) + entityLinkEdgeCount(g.outgoingCrossDomainEdges),
		AvgDegree:    avgDegree(g),
		LoadDuration: time.Since(start),
	}

	return g, stats, nil
}

// loadEntities reads all rows from the entities table and populates node maps.
func (g *Graph) loadEntities(ctx context.Context, entityDAO *dao.EntityDAO) error {
	entities, err := entityDAO.List(ctx)
	if err != nil {
		return fmt.Errorf("query entities: %w", err)
	}

	for _, ent := range entities {
		var meta map[string]interface{}
		if ent.MetadataJSON != nil {
			meta, _ = parseMetadata(*ent.MetadataJSON)
		}

		description := ""
		if ent.Description != nil {
			description = *ent.Description
		}

		node := &EntityNode{
			ID:          ent.ID,
			Type:        ent.Type,
			Name:        ent.Name,
			Domain:      utils.Normalize(ent.Domain),
			Description: description,
			Metadata:    meta,
		}

		g.nodes[node.ID] = node
		g.nameToID[node.Domain+":"+strings.ToLower(node.Name)] = node.ID
		g.typeToNodes[node.Type] = append(g.typeToNodes[node.Type], node.ID)
	}

	return nil
}

// loadRelations reads all facts from the database and builds adjacency lists.
// Facts act as directed edges: subject -> [predicate] -> object.
func (g *Graph) loadRelations(ctx context.Context, factDAO *dao.FactDAO) error {
	facts, err := factDAO.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("query facts: %w", err)
	}

	for _, fact := range facts {
		var meta map[string]interface{}
		if fact.Metadata != nil {
			meta, _ = parseMetadata(*fact.Metadata)
		}

		edge := &Edge{
			SourceID:     fact.SubjectEntityID,
			TargetID:     fact.ObjectEntityID,
			RelationType: fact.Predicate,
			Domain:       fact.Domain,
			Metadata:     meta,
		}

		g.outgoingEdges[edge.SourceID] = append(g.outgoingEdges[edge.SourceID], edge)
		g.incomingEdges[edge.TargetID] = append(g.incomingEdges[edge.TargetID], edge)
	}

	return nil
}

// loadEntityLinks reads all entity links from the database and builds an adjacency list.
// Entity links are cross-domain edges that allow BFS to traverse domain boundaries.
func (g *Graph) loadEntityLinks(ctx context.Context, linkDAO *dao.EntityLinkDAO) error {
	links, err := linkDAO.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("query entity links: %w", err)
	}

	for _, link := range links {
		edge := &EntityLinkEdge{
			SourceID:     link.SubjectEntityID,
			TargetID:     link.TargetEntityID,
			RelationType: link.RelationType,
			Method:       link.Method,
			Confidence:   link.Confidence,
			Evidence:     link.Evidence,
		}

		g.outgoingCrossDomainEdges[edge.SourceID] = append(g.outgoingCrossDomainEdges[edge.SourceID], edge)
		// Build inverse index for O(1) incoming entity link lookup.
		g.incomingCrossDomainEdges[edge.TargetID] = append(g.incomingCrossDomainEdges[edge.TargetID], edge)
		// Build target set for O(1) bidirectional existence check.
		if g.entityLinkTargetSet[edge.SourceID] == nil {
			g.entityLinkTargetSet[edge.SourceID] = make(map[int]bool)
		}
		g.entityLinkTargetSet[edge.SourceID][edge.TargetID] = true
	}

	return nil
}

// GetEntityIDByName returns the node Predicate for an exact (case-insensitive) name match.
// If domain is non-empty, it matches only within that domain.
// If domain is empty, it searches all domains and returns the lowest matching
// node Predicate (deterministic; use GetEntityIDsByName to enumerate all matches).
func (g *Graph) GetEntityIDByName(name string, domain string) (int, bool) {
	domName := utils.Normalize(domain)
	if domName != "" {
		id, ok := g.nameToID[domName+":"+strings.ToLower(name)]
		return id, ok
	}
	// Search all domains when no domain filter is specified.
	ids := g.GetEntityIDsByName(name)
	if len(ids) == 0 {
		return 0, false
	}
	return ids[0], true
}

// GetEntityIDsByName returns the IDs of all nodes with an exact
// (case-insensitive) name match across all domains, sorted by Predicate for
// deterministic results. Used when no domain filter is specified.
func (g *Graph) GetEntityIDsByName(name string) []int {
	lowerName := strings.ToLower(name)
	var ids []int
	for _, node := range g.nodes {
		if strings.ToLower(node.Name) == lowerName {
			ids = append(ids, node.ID)
		}
	}
	sort.Ints(ids)
	return ids
}

// GetNode returns the entity node by Predicate in O(1).
func (g *Graph) GetNode(id int) (*EntityNode, bool) {
	node, ok := g.nodes[id]
	return node, ok
}

// NodesByType returns all node IDs of a given entity type, optionally filtered by domain.
func (g *Graph) NodesByType(entityType string, domain string) []int {
	ids := g.typeToNodes[entityType]
	if len(ids) == 0 || domain == "" {
		copied := make([]int, len(ids))
		copy(copied, ids)
		return copied
	}
	domName := utils.Normalize(domain)
	var filtered []int
	for _, id := range ids {
		node, ok := g.nodes[id]
		if ok && node.Domain == domName {
			filtered = append(filtered, id)
		}
	}
	copied := make([]int, len(filtered))
	copy(copied, filtered)
	return copied
}

// GetOutgoingEdges returns all outgoing edges from a node.
func (g *Graph) GetOutgoingEdges(nodeID int) []*Edge {
	edges, ok := g.outgoingEdges[nodeID]
	if !ok {
		return nil
	}
	// Return a copy to prevent external modification.
	result := make([]*Edge, len(edges))
	copy(result, edges)
	return result
}

// GetIncomingEdges returns all incoming edges to a node.
func (g *Graph) GetIncomingEdges(nodeID int) []*Edge {
	edges, ok := g.incomingEdges[nodeID]
	if !ok {
		return nil
	}
	// Return a copy to prevent external modification.
	result := make([]*Edge, len(edges))
	copy(result, edges)
	return result
}

// GetEntityLinkEdges returns all entity link edges from a node.
func (g *Graph) GetEntityLinkEdges(nodeID int) []*EntityLinkEdge {
	edges, ok := g.outgoingCrossDomainEdges[nodeID]
	if !ok {
		return nil
	}
	result := make([]*EntityLinkEdge, len(edges))
	copy(result, edges)
	return result
}

// IsEntityLinkEdge checks if there is an entity link edge from sourceID to targetID.
func (g *Graph) IsEntityLinkEdge(sourceID, targetID int) bool {
	for _, e := range g.outgoingCrossDomainEdges[sourceID] {
		if e.TargetID == targetID {
			return true
		}
	}
	return false
}

// HasEntityLinkBetween checks if there is an entity link between two nodes in either direction.
func (g *Graph) HasEntityLinkBetween(a, b int) bool {
	return g.entityLinkTargetSet[a][b] || g.entityLinkTargetSet[b][a]
}

// parseMetadata unmarshals a JSON string into a map. Returns nil on error or empty input.
func parseMetadata(raw string) (map[string]interface{}, error) {
	if raw == "" {
		return nil, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("unmarshal metadata: %w", err)
	}
	return m, nil
}

// edgeCount returns the total number of edges across all adjacency lists.
func edgeCount(edges map[int][]*Edge) int {
	count := 0
	for _, list := range edges {
		count += len(list)
	}
	return count
}

// entityLinkEdgeCount returns the total number of entity link edges.
func entityLinkEdgeCount(edges map[int][]*EntityLinkEdge) int {
	count := 0
	for _, list := range edges {
		count += len(list)
	}
	return count
}

// avgDegree calculates the average degree (incoming + outgoing) per node.
// Includes both fact edges and cross-domain entity link edges (m-11).
func avgDegree(g *Graph) float64 {
	if len(g.nodes) == 0 {
		return 0
	}
	total := edgeCount(g.outgoingEdges) + edgeCount(g.incomingEdges)
	total += entityLinkEdgeCount(g.outgoingCrossDomainEdges) + entityLinkEdgeCount(g.incomingCrossDomainEdges)
	return float64(total) / float64(len(g.nodes))
}
