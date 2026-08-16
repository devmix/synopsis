package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/devmix/synopsis/internal/graph"

	"github.com/mark3labs/mcp-go/mcp"
)

// EntityRelationsResponse is the JSON response format for get_entity_relations.
type EntityRelationsResponse struct {
	CenterEntity    EntityNodeOut `json:"center_entity"`
	Nodes           []EntityNodeOut
	Edges           []EdgeOut
	TotalNodes      int   `json:"total_nodes"`
	TotalEdges      int   `json:"total_edges"`
	TraversalDepth  int   `json:"traversal_depth"`
	TraversalTimeMs int64 `json:"traversal_time_ms"`
}

// EntityNodeOut is a compact entity node for JSON responses.
type EntityNodeOut struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Domain string `json:"domain,omitempty"`
}

// EdgeOut represents an edge in the knowledge graph response.
type EdgeOut struct {
	SourceID     int                    `json:"source_id"`
	TargetID     int                    `json:"target_id"`
	SourceName   string                 `json:"source_name"`
	TargetName   string                 `json:"target_name"`
	RelationType string                 `json:"relation_type"`
	Domain       string                 `json:"domain,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// HandleGetEntityRelations processes a get_entity_relations tool call.
// It performs BFS traversal of the knowledge graph from the given entity Predicate or name.
func HandleGetEntityRelations(
	ctx context.Context,
	req mcp.CallToolRequest,
	db *sql.DB,
	g *graph.Graph,
) (*mcp.CallToolResult, error) {
	if g == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Knowledge graph is not available (disabled or not loaded yet)"),
			},
			IsError: true,
		}, nil
	}

	entityIDStr := req.GetString("entity_id", "")
	entityName := req.GetString("entity_name", "")
	domain := req.GetString("domain", "")

	depth := req.GetInt("depth", 2)
	if depth < 1 {
		depth = 1
	}
	if depth > 10 {
		depth = 10
	}

	includeCrossDomain := req.GetBool("include_cross_domain", false)

	var entityID int

	if db != nil && (entityName != "" || entityIDStr == "") {
		// Use ResolveEntity when DB is available or when name resolution is needed.
		ent, errResp := ResolveEntity(ctx, db, entityIDStr, entityName, "", domain)
		if errResp != nil {
			return errResp, nil
		}
		entityID = ent.ID
	} else if entityIDStr != "" {
		id, parseErr := strconv.Atoi(entityIDStr)
		if parseErr != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Error: 'entity_id' must be an integer, got %q", entityIDStr)),
				},
				IsError: true,
			}, nil
		}
		entityID = id
	} else {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Error: either 'entity_id' or 'entity_name' must be provided"),
			},
			IsError: true,
		}, nil
	}

	centerNode, ok := g.GetNode(entityID)
	if !ok {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Entity Predicate %d not found in graph", entityID)),
			},
			IsError: true,
		}, nil
	}

	start := time.Now()

	result, err := g.BFS(ctx, entityID, graph.BFSOptions{
		MaxDepth:          depth,
		FollowEntityLinks: includeCrossDomain,
	})
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error during BFS traversal: %v", err)),
			},
			IsError: true,
		}, nil
	}

	traversalTime := time.Since(start).Milliseconds()

	response := EntityRelationsResponse{
		CenterEntity:    EntityNodeOut{ID: centerNode.ID, Name: centerNode.Name, Type: centerNode.Type, Domain: centerNode.Domain},
		Nodes:           make([]EntityNodeOut, 0, len(result.Nodes)-1), // exclude center
		Edges:           make([]EdgeOut, 0, len(result.Edges)),
		TotalNodes:      len(result.Nodes) - 1, // exclude center from count
		TotalEdges:      len(result.Edges),
		TraversalDepth:  depth,
		TraversalTimeMs: traversalTime,
	}

	for _, node := range result.Nodes {
		if node.ID == entityID {
			continue // skip the center node in the nodes list
		}
		response.Nodes = append(response.Nodes, EntityNodeOut{
			ID:     node.ID,
			Name:   node.Name,
			Type:   node.Type,
			Domain: node.Domain,
		})
	}

	for _, edge := range result.Edges {
		sourceName := ""
		targetName := ""
		if sn, ok := g.GetNode(edge.SourceID); ok {
			sourceName = sn.Name
		}
		if tn, ok := g.GetNode(edge.TargetID); ok {
			targetName = tn.Name
		}

		outEdge := EdgeOut{
			SourceID:     edge.SourceID,
			TargetID:     edge.TargetID,
			SourceName:   sourceName,
			TargetName:   targetName,
			RelationType: edge.RelationType,
			Domain:       edge.Domain,
		}

		// Include provenance fields for cross-domain edges.
		if includeCrossDomain && edge.Method != "" {
			outEdge.Metadata = map[string]interface{}{
				"method":     edge.Method,
				"confidence": edge.Confidence,
			}
			if edge.Evidence != nil {
				outEdge.Metadata["evidence"] = *edge.Evidence
			}
		}

		response.Edges = append(response.Edges, outEdge)
	}

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error marshaling results: %v", err)),
			},
			IsError: true,
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(string(jsonBytes)),
		},
	}, nil
}
