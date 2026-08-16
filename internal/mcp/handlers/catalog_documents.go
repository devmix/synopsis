package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/devmix/synopsis/internal/database/dao"

	"github.com/mark3labs/mcp-go/mcp"
)

// CatalogDocument is a document entry in the catalog_documents response.
type CatalogDocument struct {
	ID           int         `json:"id"`
	SourceType   string      `json:"source_type"`
	OriginalPath string      `json:"original_path"`
	Domain       interface{} `json:"domain"` // parsed JSON array or raw string
	Metadata     interface{} `json:"metadata,omitempty"`
	CreatedAt    string      `json:"created_at"`
	UpdatedAt    string      `json:"updated_at"`
}

// CatalogDocumentsResponse is the JSON response for catalog_documents.
type CatalogDocumentsResponse struct {
	Documents  []CatalogDocument `json:"documents"`
	TotalCount int               `json:"total_count"`
	NextCursor *string           `json:"next_cursor,omitempty"`
}

// HandleCatalogDocuments processes a catalog_documents tool call.
func HandleCatalogDocuments(
	ctx context.Context,
	req mcp.CallToolRequest,
	db *sql.DB,
) (*mcp.CallToolResult, error) {
	pageSize := req.GetInt("page_size", DefaultPageSize)
	cursorStr := req.GetString("cursor", "")
	domain := req.GetString("domain", "")
	sourceType := req.GetString("source_type", "")
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

	docDAO := dao.NewDocumentDAO(db)
	var docs []dao.Document
	var totalCount int
	var err error

	if nameFilter != "" {
		docs, totalCount, err = docDAO.ListPaginatedWithName(ctx, offset, limit, domain, sourceType, nameFilter)
	} else {
		docs, totalCount, err = docDAO.ListPaginated(ctx, offset, limit, domain, sourceType)
	}
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error listing documents: %v", err)),
			},
			IsError: true,
		}, nil
	}

	result := CatalogDocumentsResponse{
		Documents:  make([]CatalogDocument, 0, len(docs)),
		TotalCount: totalCount,
	}

	for _, doc := range docs {
		cd := CatalogDocument{
			ID:           doc.ID,
			SourceType:   doc.SourceType,
			OriginalPath: doc.OriginalPath,
			CreatedAt:    doc.CreatedAt,
			UpdatedAt:    doc.UpdatedAt,
		}

		cd.Domain = []string{}
		if doc.MetadataJSON != nil && *doc.MetadataJSON != "" {
			var meta map[string]interface{}
			if err := json.Unmarshal([]byte(*doc.MetadataJSON), &meta); err == nil {
				if d, ok := meta["domain"]; ok {
					switch v := d.(type) {
					case string:
						if v != "" {
							cd.Domain = []string{v}
						}
					case []interface{}:
						domains := make([]string, 0, len(v))
						for _, item := range v {
							if s, ok := item.(string); ok && s != "" {
								domains = append(domains, s)
							}
						}
						cd.Domain = domains
					}
				}
			}

			var parsed interface{}
			if err := json.Unmarshal([]byte(*doc.MetadataJSON), &parsed); err != nil {
				parsed = *doc.MetadataJSON
			}
			cd.Metadata = parsed
		}

		result.Documents = append(result.Documents, cd)
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
