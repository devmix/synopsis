package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/devmix/synopsis/internal/database/dao"

	"github.com/mark3labs/mcp-go/mcp"
)

// CatalogEntity is an entity entry in the catalog_entities response.
type CatalogEntity struct {
	ID          int         `json:"id"`
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Domain      string      `json:"domain"`
	Description interface{} `json:"description,omitempty"`
	Confidence  float64     `json:"confidence,omitempty"`
	Metadata    interface{} `json:"metadata,omitempty"`
}

// CatalogEntitiesResponse is the JSON response for catalog_entities.
type CatalogEntitiesResponse struct {
	Entities   []CatalogEntity `json:"entities"`
	TotalCount int             `json:"total_count"`
	NextCursor *string         `json:"next_cursor,omitempty"`
}

// HandleCatalogEntities processes a catalog_entities tool call.
func HandleCatalogEntities(
	ctx context.Context,
	req mcp.CallToolRequest,
	db *sql.DB,
) (*mcp.CallToolResult, error) {
	pageSize := req.GetInt("page_size", DefaultPageSize)
	cursorStr := req.GetString("cursor", "")
	entityType := req.GetString("type", "")
	domain := req.GetString("domain", "")
	nameFilter := req.GetString("name", "")

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
	var ents []dao.Entity
	var totalCount int
	var err error

	if nameFilter != "" {
		ents, totalCount, err = entDAO.ListPaginatedWithName(ctx, offset, limit, domain, entityType, nameFilter)
	} else {
		ents, totalCount, err = entDAO.ListPaginated(ctx, offset, limit, domain, entityType)
	}
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error listing entities: %v", err)),
			},
			IsError: true,
		}, nil
	}

	result := CatalogEntitiesResponse{
		Entities:   make([]CatalogEntity, 0, len(ents)),
		TotalCount: totalCount,
	}

	for _, ent := range ents {
		ce := CatalogEntity{
			ID:       ent.ID,
			Name:     ent.Name,
			Type:     ent.Type,
			Domain:   ent.Domain,
			Confidence: ent.Confidence,
		}

		if ent.Description != nil && *ent.Description != "" {
			ce.Description = *ent.Description
		}

		if ent.MetadataJSON != nil && *ent.MetadataJSON != "" {
			var parsed interface{}
			if err := json.Unmarshal([]byte(*ent.MetadataJSON), &parsed); err != nil {
				parsed = *ent.MetadataJSON
			}
			ce.Metadata = parsed
		}

		result.Entities = append(result.Entities, ce)
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
