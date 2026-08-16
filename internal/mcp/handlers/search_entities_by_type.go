package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/devmix/synopsis/internal/database/dao"

	"github.com/mark3labs/mcp-go/mcp"
)

// SearchEntitiesByTypeResponse is the JSON response for search_entities_by_type.
type SearchEntitiesByTypeResponse struct {
	Entities   []SearchEntityOut `json:"entities"`
	TotalCount int               `json:"total_count"`
	NextCursor *string           `json:"next_cursor,omitempty"`
}

// SearchEntityOut is an entity entry in the search_entities_by_type response.
type SearchEntityOut struct {
	ID          int         `json:"id"`
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Domain      string      `json:"domain"`
	Description interface{} `json:"description,omitempty"`
	Confidence  float64     `json:"confidence,omitempty"`
	Metadata    interface{} `json:"metadata,omitempty"`
}

// HandleSearchEntitiesByType processes a search_entities_by_type tool call.
// It returns entities filtered by type with cursor-based pagination.
func HandleSearchEntitiesByType(
	ctx context.Context,
	req mcp.CallToolRequest,
	db *sql.DB,
) (*mcp.CallToolResult, error) {
	entityType := req.GetString("entity_type", "")
	if entityType == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Error: 'entity_type' argument is required and must not be empty"),
			},
			IsError: true,
		}, nil
	}

	domain := req.GetString("domain", "")
	pageSize := req.GetInt("page_size", DefaultPageSize)
	cursorStr := req.GetString("cursor", "")

	pageSize = NormalizePageSize(pageSize)

	var offset int
	limit := pageSize

	if cursorStr != "" {
		var err error
		offset, limit, err = DecodeCursor(cursorStr)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Error decoding cursor: %v", err)),
				},
				IsError: true,
			}, nil
		}
	}

	entDAO := dao.NewEntityDAO(db)
	ents, totalCount, err := entDAO.ListPaginated(ctx, offset, limit, domain, entityType)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error listing entities: %v", err)),
			},
			IsError: true,
		}, nil
	}

	result := SearchEntitiesByTypeResponse{
		Entities:   make([]SearchEntityOut, 0, len(ents)),
		TotalCount: totalCount,
	}

	for _, ent := range ents {
		se := SearchEntityOut{
			ID:         ent.ID,
			Name:       ent.Name,
			Type:       ent.Type,
			Domain:     ent.Domain,
			Confidence: ent.Confidence,
		}

		if ent.Description != nil && *ent.Description != "" {
			se.Description = *ent.Description
		}

		if ent.MetadataJSON != nil && *ent.MetadataJSON != "" {
			var parsed interface{}
			if err := json.Unmarshal([]byte(*ent.MetadataJSON), &parsed); err != nil {
				parsed = *ent.MetadataJSON
			}
			se.Metadata = parsed
		}

		result.Entities = append(result.Entities, se)
	}

	if offset+limit < totalCount {
		next := EncodeCursor(offset+limit, limit)
		result.NextCursor = &next
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
