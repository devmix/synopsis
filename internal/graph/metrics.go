package graph

import (
	"context"
	"fmt"
	"strings"
)

// Stats returns summary statistics about the loaded graph.
func (g *Graph) Stats() GraphStats {
	return GraphStats{
		NodeCount: len(g.nodes),
		EdgeCount: edgeCount(g.outgoingEdges),
		AvgDegree: avgDegree(g),
	}
}

// MaxConnectedDepth computes the maximum BFS depth reachable from any node.
// This is an expensive operation and should only be called for diagnostics.
func (g *Graph) MaxConnectedDepth(ctx context.Context) int {
	if len(g.nodes) == 0 {
		return 0
	}

	maxDepth := 0
	opts := BFSOptions{MaxDepth: 10, Direction: DirectionBoth, MaxNodes: 5000}

	for id := range g.nodes {
		result, err := g.BFS(ctx, id, opts)
		if err != nil {
			continue
		}
		// Depth is approximated by the number of BFS levels explored.
		depth := estimateDepth(result, id)
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	return maxDepth
}

// ToDOT exports a subgraph reachable from startNodeID as Graphviz DOT format.
func (g *Graph) ToDOT(ctx context.Context, startNodeID int, opts BFSOptions) (string, error) {
	result, err := g.BFS(ctx, startNodeID, opts)
	if err != nil {
		return "", fmt.Errorf("BFS for DOT export: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("digraph KnowledgeBase {\n")
	sb.WriteString("  rankdir=LR;\n\n")

	// Write nodes.
	for _, node := range result.Nodes {
		label := escapeDOT(node.Name)
		if node.Description != "" {
			label += "\\n" + truncate(escapeDOT(node.Description), 80)
		}
		sb.WriteString(fmt.Sprintf("  n%d [label=%q, shape=ellipse, style=filled, fillcolor=\"#%s\"];\n",
			node.ID, label, typeColor(node.Type)))
	}

	sb.WriteString("\n")

	// Write edges.
	for _, edge := range result.Edges {
		label := escapeDOT(edge.RelationType)
		sb.WriteString(fmt.Sprintf("  n%d -> n%d [label=%q];\n",
			edge.SourceID, edge.TargetID, label))
	}

	sb.WriteString("}\n")
	return sb.String(), nil
}

// estimateDepth approximates the BFS depth by counting how many unique distances exist.
func estimateDepth(result *GraphResult, startID int) int {
	if len(result.Nodes) <= 1 {
		return 0
	}

	distances := make(map[int]int)
	distances[startID] = 0

	queue := []int{startID}
	head := 0
	for head < len(queue) {
		current := queue[head]
		head++

		for _, edge := range result.Edges {
			var neighbor int
			if edge.SourceID == current {
				neighbor = edge.TargetID
			} else if edge.TargetID == current {
				neighbor = edge.SourceID
			} else {
				continue
			}

			if _, visited := distances[neighbor]; !visited {
				distances[neighbor] = distances[current] + 1
				queue = append(queue, neighbor)
			}
		}
	}

	maxD := 0
	for _, d := range distances {
		if d > maxD {
			maxD = d
		}
	}
	return maxD
}

// typeColor returns a hex color for DOT node styling based on entity type.
func typeColor(entityType string) string {
	switch entityType {
	case "employee":
		return "ADD8E6" // light blue
	case "department":
		return "90EE90" // light green
	case "policy":
		return "FFD700" // gold
	case "system":
		return "DDA0DD" // plum
	default:
		return "F5F5DC" // beige
	}
}

// escapeDOT replaces characters that have special meaning in DOT labels.
func escapeDOT(s string) string {
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// truncate cuts the string to maxLen and appends "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
