package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/devmix/synopsis/internal/database/dao"

	"github.com/mark3labs/mcp-go/mcp"
)

// CatalogOverviewResponse is the JSON response for catalog_overview.
type CatalogOverviewResponse struct {
	DocumentCount    int            `json:"document_count"`
	ChunkCount       int            `json:"chunk_count"`
	EntityCount      int            `json:"entity_count"`
	FactCount        int            `json:"fact_count"`
	DocumentsByType  map[string]int `json:"documents_by_type"`
	EntitiesByType   map[string]int `json:"entities_by_type"`
	EntitiesByDomain map[string]int `json:"entities_by_domain"`
	Domains          []string       `json:"domains"`
	EntityTypes      []string       `json:"entity_types"`
	GraphNodeCount   int            `json:"graph_node_count"`
	GraphEdgeCount   int            `json:"graph_edge_count"`
}

// HandleCatalogOverview processes a catalog_overview tool call.
func HandleCatalogOverview(
	ctx context.Context,
	req mcp.CallToolRequest,
	db *sql.DB,
) (*mcp.CallToolResult, error) {
	docDAO := dao.NewDocumentDAO(db)
	chunkDAO := dao.NewChunkDAO(db)
	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	result := CatalogOverviewResponse{
		DocumentsByType:  make(map[string]int),
		EntitiesByType:   make(map[string]int),
		EntitiesByDomain: make(map[string]int),
		Domains:          []string{},
		EntityTypes:      []string{},
	}

	// Document count.
	var err error
	result.DocumentCount, err = docDAO.Count(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error counting documents: %v", err)),
			},
			IsError: true,
		}, nil
	}

	// Chunk count.
	result.ChunkCount, err = chunkDAO.Count(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error counting chunks: %v", err)),
			},
			IsError: true,
		}, nil
	}

	// Entity count.
	result.EntityCount, err = entDAO.Count(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error counting entities: %v", err)),
			},
			IsError: true,
		}, nil
	}

	// Fact count.
	result.FactCount, err = factDAO.Count(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error counting facts: %v", err)),
			},
			IsError: true,
		}, nil
	}

	// Documents by type.
	result.DocumentsByType, err = docDAO.DocumentsByType(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error querying documents by type: %v", err)),
			},
			IsError: true,
		}, nil
	}

	// Entities by type.
	result.EntitiesByType, err = entDAO.TypesByCount(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error querying entities by type: %v", err)),
			},
			IsError: true,
		}, nil
	}

	// Entities by domain.
	result.EntitiesByDomain, err = entDAO.DomainsByCount(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error querying entities by domain: %v", err)),
			},
			IsError: true,
		}, nil
	}

	// Domains from metadata_json only.
	result.Domains, err = docDAO.UniqueDomains(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error querying domains: %v", err)),
			},
			IsError: true,
		}, nil
	}

	// Entity types (unique values).
	result.EntityTypes, err = entDAO.UniqueTypes(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error querying entity types: %v", err)),
			},
			IsError: true,
		}, nil
	}

	// Graph node count (distinct entities referenced in entity_links).
	result.GraphNodeCount, err = linkDAO.GraphNodeCount(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error counting graph nodes: %v", err)),
			},
			IsError: true,
		}, nil
	}

	// Graph edge count (entity_links rows).
	result.GraphEdgeCount, err = linkDAO.Count(ctx)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error counting graph edges: %v", err)),
			},
			IsError: true,
		}, nil
	}

	jsonBytes, err := json.Marshal(result)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error marshaling response: %v", err)),
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
