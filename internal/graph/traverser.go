package graph

import (
	"context"
	"fmt"
	"sort"

	"github.com/devmix/synopsis/internal/utils"
)

// Direction specifies which edges to follow during traversal.
type Direction string

const (
	DirectionOutgoing Direction = "outgoing"
	DirectionIncoming Direction = "incoming"
	DirectionBoth     Direction = "both"
)

// BFSOptions configures the breadth-first search traversal.
type BFSOptions struct {
	MaxDepth          int       // maximum depth to traverse (default 5, max 10)
	Direction         Direction // which edges to follow (default "both")
	RelationTypes     []string  // filter by relation types (empty = no filter)
	MaxNodes          int       // maximum number of nodes in BFS result; edges are only added for included nodes, so len(Edges) ≤ len(Nodes) (default 1000)
	FollowEntityLinks bool      // if true, allow crossing domain boundaries via entity links only
}

// ApplyDefaults fills zero-value fields with sensible defaults.
func (o *BFSOptions) ApplyDefaults() {
	if o.MaxDepth <= 0 {
		o.MaxDepth = 5
	}
	if o.MaxDepth > 10 {
		o.MaxDepth = 10
	}
	if o.Direction == "" {
		o.Direction = DirectionBoth
	}
	if o.MaxNodes <= 0 {
		o.MaxNodes = 1000
	}
}

// BFS performs a breadth-first search starting from the given node Predicate.
// It returns discovered nodes and edges up to maxDepth levels, with cycle protection.
// BFS does not cross domain boundaries: only entities within the same domain as
// the start node are traversed. If ctx is cancelled during traversal, it returns
// partial results and ctx.Err().
func (g *Graph) BFS(ctx context.Context, startNodeID int, opts BFSOptions) (*GraphResult, error) {
	opts.ApplyDefaults()

	startNode, ok := g.GetNode(startNodeID)
	if !ok {
		return nil, fmt.Errorf("start node %d not found", startNodeID)
	}

	result := &GraphResult{
		CenterEntity: startNode,
		Nodes:        []*EntityNode{startNode},
		Edges:        make([]*Edge, 0),
	}

	startDomain := utils.Normalize(startNode.Domain)

	visited := make(map[int]bool, opts.MaxNodes)
	visited[startNodeID] = true

	// BFS queue stores node IDs at the current frontier.
	currentLevel := []int{startNodeID}

	for depth := 0; depth < opts.MaxDepth; depth++ {
		if len(currentLevel) == 0 {
			break
		}

		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("BFS cancelled at depth %d: %w", depth, err)
		}

		nextLevel := make(map[int]bool)

		for _, nodeID := range currentLevel {
			var edges []*Edge

			switch opts.Direction {
			case DirectionOutgoing:
				edges = g.outgoingEdges[nodeID]
			case DirectionIncoming:
				edges = g.incomingEdges[nodeID]
			case DirectionBoth:
				// Merge outgoing and incoming fact edges, deduplicating by source+target.
				factEdgeSet := make(map[string]*Edge)
				for _, e := range g.outgoingEdges[nodeID] {
					key := fmt.Sprintf("%d-%d", e.SourceID, e.TargetID)
					factEdgeSet[key] = e
				}
				for _, e := range g.incomingEdges[nodeID] {
					key := fmt.Sprintf("%d-%d", e.SourceID, e.TargetID)
					if _, exists := factEdgeSet[key]; !exists {
						factEdgeSet[key] = e
					}
				}
				// Sort keys for deterministic iteration order.
				sortedFactKeys := make([]string, 0, len(factEdgeSet))
				for k := range factEdgeSet {
					sortedFactKeys = append(sortedFactKeys, k)
				}
				sort.Strings(sortedFactKeys)
				for _, k := range sortedFactKeys {
					edges = append(edges, factEdgeSet[k])
				}
			default:
				return nil, fmt.Errorf("unknown direction %q", opts.Direction)
			}

			// When FollowEntityLinks is enabled, also include entity link edges
			// as traversal targets so cross-domain neighbors are discoverable.
			if opts.FollowEntityLinks {
				switch opts.Direction {
				case DirectionOutgoing:
					// Dedup and sort outgoing entity links for deterministic order (m-9).
					elSet := make(map[string]*Edge)
					for _, el := range g.outgoingCrossDomainEdges[nodeID] {
						key := fmt.Sprintf("%d-%d", el.SourceID, el.TargetID)
						if _, exists := elSet[key]; !exists {
							elSet[key] = entityLinkToEdge(el)
						}
					}
					sortedElKeys := make([]string, 0, len(elSet))
					for k := range elSet {
						sortedElKeys = append(sortedElKeys, k)
					}
					sort.Strings(sortedElKeys)
					for _, k := range sortedElKeys {
						edges = append(edges, elSet[k])
					}
				case DirectionIncoming:
					// Use inverse index for O(1) incoming entity link lookup.
					// Dedup and sort for deterministic order (m-9).
					elSet := make(map[string]*Edge)
					for _, el := range g.incomingCrossDomainEdges[nodeID] {
						key := fmt.Sprintf("%d-%d", el.SourceID, el.TargetID)
						if _, exists := elSet[key]; !exists {
							elSet[key] = entityLinkToEdge(el)
						}
					}
					sortedElKeys := make([]string, 0, len(elSet))
					for k := range elSet {
						sortedElKeys = append(sortedElKeys, k)
					}
					sort.Strings(sortedElKeys)
					for _, k := range sortedElKeys {
						edges = append(edges, elSet[k])
					}
				case DirectionBoth:
					// Merge outgoing and incoming entity links into a separate slice,
					// deduplicating by source+target. Both fact edges and entity link
					// edges are preserved (a pair may have both types without overwrite).
					elSet := make(map[string]*Edge)
					for _, el := range g.outgoingCrossDomainEdges[nodeID] {
						key := fmt.Sprintf("%d-%d", el.SourceID, el.TargetID)
						elSet[key] = entityLinkToEdge(el)
					}
					for _, el := range g.incomingCrossDomainEdges[nodeID] {
						key := fmt.Sprintf("%d-%d", el.SourceID, el.TargetID)
						if _, exists := elSet[key]; !exists {
							elSet[key] = entityLinkToEdge(el)
						}
					}
					// Sort keys for deterministic iteration order (m-9).
					sortedElKeys := make([]string, 0, len(elSet))
					for k := range elSet {
						sortedElKeys = append(sortedElKeys, k)
					}
					sort.Strings(sortedElKeys)
					for _, k := range sortedElKeys {
						edges = append(edges, elSet[k])
					}
				}
			}

			for _, edge := range edges {
				if err := ctx.Err(); err != nil {
					return result, fmt.Errorf("BFS cancelled during edge processing: %w", err)
				}

				// Filter by relation type if specified.
				if !matchesRelationType(edge.RelationType, opts.RelationTypes) {
					continue
				}

				var neighborID int
				switch opts.Direction {
				case DirectionOutgoing:
					neighborID = edge.TargetID
				case DirectionIncoming:
					neighborID = edge.SourceID
				case DirectionBoth:
					// For "both", the neighbor is whichever side isn't the current node.
					if edge.SourceID == nodeID {
						neighborID = edge.TargetID
					} else {
						neighborID = edge.SourceID
					}
				}

				if visited[neighborID] {
					continue // cycle protection: skip already visited nodes
				}

				// Check hard node limit.
				if len(result.Nodes) >= opts.MaxNodes {
					break
				}

				neighbor, ok := g.GetNode(neighborID)
				if !ok {
					continue // dangling edge — target entity doesn't exist in graph
				}

				// Domain boundary check: do not cross domain boundaries unless
				// FollowEntityLinks is true and an entity link exists between the
				// current node and the neighbor. Fact edges crossing domains are
				// never traversable even with FollowEntityLinks == true.
				if neighbor.Domain != startDomain {
					if !opts.FollowEntityLinks || !g.HasEntityLinkBetween(nodeID, neighborID) {
						continue
					}
				}

				visited[neighborID] = true
				result.Nodes = append(result.Nodes, neighbor)
				result.Edges = append(result.Edges, edge)
				nextLevel[neighborID] = true
			}

			if len(result.Nodes) >= opts.MaxNodes {
				break
			}
		}

		// Convert next level map to slice for the next iteration.
		// Sort keys for deterministic BFS order.
		currentLevel = make([]int, 0, len(nextLevel))
		for id := range nextLevel {
			currentLevel = append(currentLevel, id)
		}
		sort.Ints(currentLevel)
	}

	return result, nil
}

// matchesRelationType checks if an edge's relation type passes the filter.
// An empty filter list means all types pass.
func matchesRelationType(relationType string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, t := range allowed {
		if t == relationType {
			return true
		}
	}
	return false
}

// entityLinkToEdge converts an EntityLinkEdge to a traversable Edge, preserving provenance.
func entityLinkToEdge(el *EntityLinkEdge) *Edge {
	return &Edge{
		SourceID:     el.SourceID,
		TargetID:     el.TargetID,
		RelationType: el.RelationType,
		Method:       el.Method,
		Confidence:   el.Confidence,
		Evidence:     el.Evidence,
	}
}
