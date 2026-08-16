package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/devmix/synopsis/internal/database/dao"

	"github.com/mark3labs/mcp-go/mcp"
)

// SearchFactsResponse is the JSON response for search_facts.
type SearchFactsResponse struct {
	Facts      []SearchFactOut `json:"facts"`
	TotalCount int             `json:"total_count"`
	NextCursor *string         `json:"next_cursor,omitempty"`
}

// SearchFactOut is a fact entry in the search_facts response.
type SearchFactOut struct {
	ID              int    `json:"id"`
	Predicate       string `json:"predicate"`
	SubjectEntityID int    `json:"subject_entity_id,omitempty"`
	SubjectName     string `json:"subject_name,omitempty"`
	ObjectEntityID  int    `json:"object_entity_id,omitempty"`
	ObjectName      string `json:"object_name,omitempty"`
	Domain          string `json:"domain"`
	Status          string `json:"status"`
	ValidFrom       string `json:"valid_from,omitempty"`
	ValidTo         string `json:"valid_to,omitempty"`
	Weight          int    `json:"weight"`
}

// HandleSearchFacts processes a search_facts tool call.
func HandleSearchFacts(
	ctx context.Context,
	req mcp.CallToolRequest,
	db *sql.DB,
) (*mcp.CallToolResult, error) {
	predicateFilter := req.GetString("predicate", "")
	entityNameFilter := req.GetString("entity_name", "")
	statusFilter := req.GetString("status", "approved")
	domainFilter := req.GetString("domain", "")
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

	factDAO := dao.NewFactDAO(db)
	entDAO := dao.NewEntityDAO(db)

	facts, totalCount, err := factDAO.SearchPaginated(ctx, offset, limit, predicateFilter, entityNameFilter, statusFilter, domainFilter)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error searching facts: %v", err)),
			},
			IsError: true,
		}, nil
	}

	result := SearchFactsResponse{
		Facts:      make([]SearchFactOut, 0, len(facts)),
		TotalCount: totalCount,
	}

	// Collect unique entity IDs for batch lookup.
	entityIDSet := make(map[int]struct{})
	for _, f := range facts {
		if f.SubjectEntityID != 0 {
			entityIDSet[f.SubjectEntityID] = struct{}{}
		}
		if f.ObjectEntityID != 0 {
			entityIDSet[f.ObjectEntityID] = struct{}{}
		}
	}
	entityIDs := make([]int, 0, len(entityIDSet))
	for id := range entityIDSet {
		entityIDs = append(entityIDs, id)
	}

	var entityMap map[int]*dao.Entity
	if len(entityIDs) > 0 {
		entityMap, _ = entDAO.GetByIDs(ctx, entityIDs) //nolint:errcheck
	}

	for _, f := range facts {
		sf := SearchFactOut{
			ID:        f.ID,
			Predicate: f.Predicate,
			Domain:    f.Domain,
			Status:    f.Status,
			Weight:    f.Weight,
		}

		if f.SubjectEntityID != 0 {
			sf.SubjectEntityID = f.SubjectEntityID
			if ent, ok := entityMap[f.SubjectEntityID]; ok {
				sf.SubjectName = ent.Name
			}
		}

		if f.ObjectEntityID != 0 {
			sf.ObjectEntityID = f.ObjectEntityID
			if ent, ok := entityMap[f.ObjectEntityID]; ok {
				sf.ObjectName = ent.Name
			}
		}

		if f.ValidFrom != nil {
			sf.ValidFrom = *f.ValidFrom
		}
		if f.ValidTo != nil {
			sf.ValidTo = *f.ValidTo
		}

		result.Facts = append(result.Facts, sf)
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
