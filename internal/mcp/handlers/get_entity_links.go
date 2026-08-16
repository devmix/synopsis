package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/devmix/synopsis/internal/database/dao"

	"github.com/mark3labs/mcp-go/mcp"
)

// EntityLinksResponse is the JSON response format for get_entity_links.
type EntityLinksResponse struct {
	Entity EntityNodeOut   `json:"entity"`
	Links  []EntityLinkOut `json:"links"`
}

// EntityLinkOut represents a single cross-domain entity link with provenance.
type EntityLinkOut struct {
	TargetEntityID int     `json:"target_entity_id"`
	TargetName     string  `json:"target_name"`
	TargetDomain   string  `json:"target_domain"`
	RelationType   string  `json:"relation_type"`
	Method         string  `json:"method"`
	Confidence     float64 `json:"confidence"`
	Evidence       string  `json:"evidence,omitempty"`
}

// HandleGetEntityLinks processes a get_entity_links tool call.
// It retrieves cross-domain entity links for the given entity Predicate or name with provenance info.
func HandleGetEntityLinks(
	ctx context.Context,
	req mcp.CallToolRequest,
	db *sql.DB,
) (*mcp.CallToolResult, error) {
	entityIDStr := req.GetString("entity_id", "")
	entityName := req.GetString("entity_name", "")
	domain := req.GetString("domain", "")

	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	centerEnt, errResp := ResolveEntity(ctx, db, entityIDStr, entityName, "", domain)
	if errResp != nil {
		return errResp, nil
	}

	entityID := centerEnt.ID

	// Fetch links for the resolved entity.
	links, err := linkDAO.ListByEntity(ctx, entityID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error retrieving entity links: %v", err)),
			},
			IsError: true,
		}, nil
	}

	response := EntityLinksResponse{
		Entity: EntityNodeOut{
			ID:     centerEnt.ID,
			Name:   centerEnt.Name,
			Type:   centerEnt.Type,
			Domain: centerEnt.Domain,
		},
		Links: make([]EntityLinkOut, 0, len(links)),
	}

	// Links are stored bidirectionally (A→B and B→A). Dedup by
	// (target_entity_id, relation_type) so the caller sees each unique
	// target once. First occurrence wins; order is deterministic because
	// ListByEntity uses ORDER BY in the DAO query.
	seen := make(map[string]struct{}, len(links))

	for _, link := range links {
		targetID := link.TargetEntityID
		if targetID == entityID {
			// Link is in reverse direction (entity is the target).
			targetID = link.SubjectEntityID
		}

		targetEnt, err := entDAO.GetByID(ctx, targetID)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Error retrieving target entity %d: %v", targetID, err)),
				},
				IsError: true,
			}, nil
		}

		// Nil-target guard: skip links that reference deleted entities.
		if targetEnt == nil {
			continue
		}

		dedupKey := fmt.Sprintf("%d:%s", targetEnt.ID, link.RelationType)
		if _, exists := seen[dedupKey]; exists {
			continue
		}
		seen[dedupKey] = struct{}{}

		evidence := ""
		if link.Evidence != nil {
			evidence = *link.Evidence
		}

		response.Links = append(response.Links, EntityLinkOut{
			TargetEntityID: targetEnt.ID,
			TargetName:     targetEnt.Name,
			TargetDomain:   targetEnt.Domain,
			RelationType:   link.RelationType,
			Method:         link.Method,
			Confidence:     link.Confidence,
			Evidence:       evidence,
		})
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
