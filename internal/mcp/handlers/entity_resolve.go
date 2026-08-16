package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"

	"github.com/devmix/synopsis/internal/database/dao"

	"github.com/mark3labs/mcp-go/mcp"
)

// ResolveEntity resolves an entity from either entity_id or entity_name.
// Exactly one of the two must be provided; optional type/domain params disambiguate name lookup.
// Returns (entity, nil) on success, (nil, errorResponse) on failure.
func ResolveEntity(
	ctx context.Context, db *sql.DB,
	entityIDStr string, entityName string, entityType string, domain string,
) (*dao.Entity, *mcp.CallToolResult) {

	hasID := entityIDStr != ""
	hasName := entityName != ""

	if !hasID && !hasName {
		return nil, &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Error: either 'entity_id' or 'entity_name' must be provided"),
			},
			IsError: true,
		}
	}

	if hasID && hasName {
		return nil, &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Error: provide either 'entity_id' or 'entity_name', not both"),
			},
			IsError: true,
		}
	}

	if hasID {
		entityID, err := strconv.Atoi(entityIDStr)
		if err != nil {
			return nil, &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Error: 'entity_id' must be an integer, got %q", entityIDStr)),
				},
				IsError: true,
			}
		}

		entDAO := dao.NewEntityDAO(db)
		ent, err := entDAO.GetByID(ctx, entityID)
		if err != nil {
			return nil, &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Error retrieving entity %d: %v", entityID, err)),
				},
				IsError: true,
			}
		}

		if ent == nil {
			return nil, &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Entity Predicate %d not found", entityID)),
				},
				IsError: true,
			}
		}

		return ent, nil
	}

	// Resolve by name.
	entDAO := dao.NewEntityDAO(db)

	if domain != "" {
		// Domain filter: case-insensitive lookup by name + domain.
		centerEnt, err := entDAO.GetByNameFold(ctx, entityName, domain)
		if err != nil {
			return nil, &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Error looking up entity: %v", err)),
				},
				IsError: true,
			}
		}

		if centerEnt == nil {
			return nil, &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf(
						"Entity %q not found in domain %q. Check spelling.", entityName, domain)),
				},
				IsError: true,
			}
		}

		return centerEnt, nil
	}

	// No domain filter: case-insensitive search with deterministic order.
	matches, err := entDAO.ListByNameFold(ctx, entityName)
	if err != nil {
		return nil, &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error listing entities: %v", err)),
			},
			IsError: true,
		}
	}

	switch len(matches) {
	case 0:
		return nil, &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf(
					"Entity %q not found. Check spelling.", entityName)),
			},
			IsError: true,
		}
	case 1:
		return &matches[0], nil
	default:
		return nil, entitySuggestions(entityName, matches)
	}
}

// entitySuggestions builds a deterministic error response listing the matching
// entities (id + type + domain) so the caller can disambiguate.
func entitySuggestions(entityName string, entities []dao.Entity) *mcp.CallToolResult {
	var suggestions []string
	for _, e := range entities {
		suggestions = append(suggestions, fmt.Sprintf("- %s (id=%d, type=%s) [%s]", e.Name, e.ID, e.Type, e.Domain))
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(fmt.Sprintf(
				"Multiple entities match %q. Please specify one (optionally with a domain):\n%s", entityName, strings.Join(suggestions, "\n"))),
		},
		IsError: true,
	}
}
