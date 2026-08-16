// Package graph provides in-memory entity relation traversal using BFS.
//
// It loads entities and relations from SQLite into adjacency lists at startup,
// enabling O(1) name lookup and fast breadth-first search up to 10 levels deep.
package graph

import "time"

// EntityNode represents a node in the knowledge graph.
type EntityNode struct {
	ID          int                    `json:"id"`
	Type        string                 `json:"type"`
	Name        string                 `json:"name"`
	Domain      string                 `json:"domain"`
	Description string                 `json:"description,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Edge represents a directed relation between two entities.
type Edge struct {
	SourceID     int                    `json:"source_id"`
	TargetID     int                    `json:"target_id"`
	RelationType string                 `json:"relation_type"`
	Domain       string                 `json:"domain,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	// Provenance fields for entity-link edges (m-10).
	Method     string  `json:"method,omitempty"`
	Confidence float64 `json:"confidence,omitempty"`
	Evidence   *string `json:"evidence,omitempty"`
}

// GraphResult contains the nodes and edges discovered during graph traversal.
type GraphResult struct {
	Nodes        []*EntityNode `json:"nodes"`
	Edges        []*Edge       `json:"edges"`
	CenterEntity *EntityNode   `json:"center_entity"` // central entity of the query
}

// EntityLinkEdge represents a cross-domain link between two entities.
type EntityLinkEdge struct {
	SourceID     int     `json:"source_id"`
	TargetID     int     `json:"target_id"`
	RelationType string  `json:"relation_type"`
	Method       string  `json:"method"`
	Confidence   float64 `json:"confidence"`
	Evidence     *string `json:"evidence,omitempty"`
}

// GraphStats holds summary statistics about the loaded graph.
type GraphStats struct {
	NodeCount    int           `json:"node_count"`
	EdgeCount    int           `json:"edge_count"`
	AvgDegree    float64       `json:"avg_degree"`
	LoadDuration time.Duration `json:"load_duration_ms"`
}
