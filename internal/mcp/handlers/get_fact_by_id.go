package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/devmix/synopsis/internal/database/dao"

	"github.com/mark3labs/mcp-go/mcp"
)

// HandleGetFactByID processes a get_fact_by_id tool call.
// It retrieves fact data with subject/object entities and sources.
func HandleGetFactByID(
	ctx context.Context,
	req mcp.CallToolRequest,
	db *sql.DB,
) (*mcp.CallToolResult, error) {
	factIDStr := req.GetString("fact_id", "")
	if factIDStr == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Error: 'fact_id' argument is required"),
			},
			IsError: true,
		}, nil
	}

	factID, err := strconv.Atoi(factIDStr)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error: 'fact_id' must be an integer, got %q", factIDStr)),
			},
			IsError: true,
		}, nil
	}

	factDAO := dao.NewFactDAO(db)
	entityDAO := dao.NewEntityDAO(db)
	factSourceDAO := dao.NewFactSourceDAO(db)

	fact, err := factDAO.GetByID(ctx, factID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error retrieving fact: %v", err)),
			},
			IsError: true,
		}, nil
	}

	if fact == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Fact with Predicate %d not found", factID)),
			},
			IsError: true,
		}, nil
	}

	response := FactByIDResponse{
		Fact: FactInfo{
			ID:        fact.ID,
			Predicate: fact.Predicate,
			Domain:    fact.Domain,
			Status:    fact.Status,
			Weight:    fact.Weight,
		},
	}

	if fact.SubjectEntityID != 0 {
		response.Fact.SubjectEntityID = fact.SubjectEntityID
	}
	if fact.ObjectEntityID != 0 {
		response.Fact.ObjectEntityID = fact.ObjectEntityID
	}
	if fact.Metadata != nil {
		response.Fact.Metadata = *fact.Metadata
	}
	if fact.ValidFrom != nil {
		response.Fact.ValidFrom = *fact.ValidFrom
	}
	if fact.ValidTo != nil {
		response.Fact.ValidTo = *fact.ValidTo
	}

	if fact.SubjectEntityID != 0 {
		subj, err := entityDAO.GetByID(ctx, fact.SubjectEntityID)
		if err == nil && subj != nil {
			response.SubjectEntity = &EntityWithContext{
				ID:     subj.ID,
				Name:   subj.Name,
				Type:   subj.Type,
				Domain: subj.Domain,
			}
		}
	}

	if fact.ObjectEntityID != 0 {
		obj, err := entityDAO.GetByID(ctx, fact.ObjectEntityID)
		if err == nil && obj != nil {
			response.ObjectEntity = &EntityWithContext{
				ID:     obj.ID,
				Name:   obj.Name,
				Type:   obj.Type,
				Domain: obj.Domain,
			}
		}
	}

	sources, err := factSourceDAO.GetByFactID(ctx, factID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error retrieving fact sources: %v", err)),
			},
			IsError: true,
		}, nil
	}

	for _, src := range sources {
		factSource := FactSourceInfo{
			DocumentID:  src.DocumentID,
			ExtractedAt: src.ExtractedAt,
		}
		if src.Quote != nil {
			factSource.Quote = *src.Quote
		}
		response.Sources = append(response.Sources, factSource)
	}

	jsonBytes, err := json.Marshal(response)
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

// FactByIDResponse is the JSON response format for get_fact_by_id.
type FactByIDResponse struct {
	Fact          FactInfo           `json:"fact"`
	SubjectEntity *EntityWithContext `json:"subject_entity,omitempty"`
	ObjectEntity  *EntityWithContext `json:"object_entity,omitempty"`
	Sources       []FactSourceInfo   `json:"sources,omitempty"`
}

// FactInfo contains fact data.
type FactInfo struct {
	ID              int    `json:"id"`
	Predicate       string `json:"predicate"`
	SubjectEntityID int    `json:"subject_entity_id,omitempty"`
	ObjectEntityID  int    `json:"object_entity_id,omitempty"`
	Domain          string `json:"domain"`
	Metadata        string `json:"metadata,omitempty"`
	Status          string `json:"status"`
	ValidFrom       string `json:"valid_from,omitempty"`
	ValidTo         string `json:"valid_to,omitempty"`
	Weight          int    `json:"weight"`
}

// FactSourceInfo contains fact source data.
type FactSourceInfo struct {
	DocumentID  int    `json:"document_id"`
	Quote       string `json:"quote,omitempty"`
	ExtractedAt string `json:"extracted_at"`
}
