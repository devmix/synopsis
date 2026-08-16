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

// extractImagePaths parses document metadata JSON and extracts image paths.
// It looks for common keys: "images", "image_paths", "attachments".
// Note: Image paths are extracted from the document's metadata_json field
// since the current DB schema has no dedicated images table.
func extractImagePaths(metadataJSON string) []string {
	if metadataJSON == "" {
		return nil
	}

	var meta map[string]interface{}
	if err := json.Unmarshal([]byte(metadataJSON), &meta); err != nil {
		return nil
	}

	var paths []string

	// Try common image-related keys.
	for _, key := range []string{"images", "image_paths", "attachments"} {
		val, ok := meta[key]
		if !ok {
			continue
		}

		arr, ok := val.([]interface{})
		if !ok {
			continue
		}

		for _, item := range arr {
			if pathStr, ok := item.(string); ok && pathStr != "" {
				paths = append(paths, pathStr)
			}
		}
	}

	return paths
}

// HandleGetDocumentContext processes a get_document_context tool call.
// It retrieves document metadata and associated chunks, entities, and fact IDs.
func HandleGetDocumentContext(
	ctx context.Context,
	req mcp.CallToolRequest,
	db *sql.DB,
) (*mcp.CallToolResult, error) {
	documentIDStr := req.GetString("document_id", "")
	if documentIDStr == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Error: 'document_id' argument is required"),
			},
			IsError: true,
		}, nil
	}

	documentID, err := strconv.Atoi(documentIDStr)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error: 'document_id' must be an integer, got %q", documentIDStr)),
			},
			IsError: true,
		}, nil
	}

	includeChunks := req.GetBool("include_chunks", true)
	includeEntities := req.GetBool("include_entities", true)
	includeFacts := req.GetBool("include_facts", false)

	docDAO := dao.NewDocumentDAO(db)
	chunkDAO := dao.NewChunkDAO(db)
	chunkEntityDAO := dao.NewChunkEntityDAO(db)
	entityDAO := dao.NewEntityDAO(db)

	doc, err := docDAO.GetByID(ctx, documentID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error retrieving document: %v", err)),
			},
			IsError: true,
		}, nil
	}

	if doc == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Document with Predicate %d not found", documentID)),
			},
			IsError: true,
		}, nil
	}

	response := DocumentContextResponse{
		Document: DocumentInfo{
			ID:           doc.ID,
			SourceType:   doc.SourceType,
			OriginalPath: doc.OriginalPath,
			CreatedAt:    doc.CreatedAt,
			UpdatedAt:    doc.UpdatedAt,
		},
		ImagePaths: make([]string, 0),
	}

	if doc.MetadataJSON != nil {
		response.Document.Metadata = *doc.MetadataJSON
		response.ImagePaths = extractImagePaths(*doc.MetadataJSON)
	}

	var domains []string
	if doc.MetadataJSON != nil {
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(*doc.MetadataJSON), &meta); err == nil {
			if d, ok := meta["domain"]; ok {
				switch v := d.(type) {
				case string:
					if v != "" {
						domains = append(domains, v)
					}
				case []interface{}:
					for _, item := range v {
						if s, ok := item.(string); ok && s != "" {
							domains = append(domains, s)
						}
					}
				}
			}
		}
	}
	response.Document.Domains = domains

	var chunks []dao.Chunk
	if includeChunks {
		chunks, err = chunkDAO.ListByDocID(ctx, documentID)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Error retrieving chunks: %v", err)),
				},
				IsError: true,
			}, nil
		}

		for _, c := range chunks {
			chunkInfo := ChunkWithContext{
				ID:          c.ID,
				SequenceNum: c.SequenceNum,
				Text:        c.ChunkText,
			}
			if c.StartOffset != nil {
				chunkInfo.StartOffset = *c.StartOffset
			}
			if c.EndOffset != nil {
				chunkInfo.EndOffset = *c.EndOffset
			}
			response.Chunks = append(response.Chunks, chunkInfo)
		}

		response.ChunkCount = len(chunks)
	} else {
		count, err := chunkDAO.CountByDocID(ctx, documentID)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Error counting chunks: %v", err)),
				},
				IsError: true,
			}, nil
		}
		response.ChunkCount = count
	}

	if includeEntities {
		var entityIDs []int
		if len(chunks) > 0 {
			for _, c := range chunks {
				ids, err := chunkEntityDAO.GetEntitiesByChunk(ctx, c.ID)
				if err != nil {
					return &mcp.CallToolResult{
						Content: []mcp.Content{
							mcp.NewTextContent(fmt.Sprintf("Error retrieving entities for chunk %d: %v", c.ID, err)),
						},
						IsError: true,
					}, nil
				}
				entityIDs = append(entityIDs, ids...)
			}
		} else {
			// Chunks not loaded; query entities directly by document Predicate.
			var err error
			entityIDs, err = chunkEntityDAO.GetEntityIDsByDocID(ctx, documentID)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error retrieving entities for document %d: %v", documentID, err)),
					},
					IsError: true,
				}, nil
			}
		}

		if len(entityIDs) > 0 {
			seen := make(map[int]bool)
			var uniqueIDs []int
			for _, id := range entityIDs {
				if !seen[id] {
					seen[id] = true
					uniqueIDs = append(uniqueIDs, id)
				}
			}

			entityMap, err := entityDAO.GetByIDs(ctx, uniqueIDs)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error retrieving entities: %v", err)),
					},
					IsError: true,
				}, nil
			}

			for _, id := range uniqueIDs {
				ent, ok := entityMap[id]
				if !ok {
					continue
				}
				response.Entities = append(response.Entities, EntityWithContext{
					ID:     ent.ID,
					Name:   ent.Name,
					Type:   ent.Type,
					Domain: ent.Domain,
				})
			}
		}
	}

	if includeFacts {
		var allEntityIDs []int
		if len(chunks) > 0 {
			allEntityIDs = collectAllEntityIDs(ctx, chunks, chunkEntityDAO)
		} else {
			// Chunks not loaded; query entity IDs directly by document Predicate.
			var err error
			allEntityIDs, err = chunkEntityDAO.GetEntityIDsByDocID(ctx, documentID)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error retrieving entities for facts lookup on document %d: %v", documentID, err)),
					},
					IsError: true,
				}, nil
			}
			// Deduplicate.
			seen := make(map[int]bool)
			var deduped []int
			for _, id := range allEntityIDs {
				if !seen[id] {
					seen[id] = true
					deduped = append(deduped, id)
				}
			}
			allEntityIDs = deduped
		}

		if len(allEntityIDs) > 0 {
			factDAO := dao.NewFactDAO(db)
			factsByEntity, err := factDAO.ListByEntityIDs(ctx, allEntityIDs)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error retrieving facts: %v", err)),
					},
					IsError: true,
				}, nil
			}

			seenFacts := make(map[int]bool)
			var factIDs []int
			for _, facts := range factsByEntity {
				for _, f := range facts {
					if !seenFacts[f.ID] {
						seenFacts[f.ID] = true
						factIDs = append(factIDs, f.ID)
					}
				}
			}
			response.FactIDs = factIDs
		}
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

// collectAllEntityIDs gathers all unique entity IDs associated with the given chunks.
func collectAllEntityIDs(ctx context.Context, chunks []dao.Chunk, chunkEntityDAO *dao.ChunkEntityDAO) []int {
	seen := make(map[int]bool)
	var ids []int
	for _, c := range chunks {
		entityIDs, err := chunkEntityDAO.GetEntitiesByChunk(ctx, c.ID)
		if err != nil {
			continue
		}
		for _, id := range entityIDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

// DocumentContextResponse is the JSON response format for get_document_context.
type DocumentContextResponse struct {
	Document   DocumentInfo        `json:"document"`
	ImagePaths []string            `json:"image_paths,omitempty"`
	ChunkCount int                 `json:"chunk_count"`
	Chunks     []ChunkWithContext  `json:"chunks,omitempty"`
	Entities   []EntityWithContext `json:"entities,omitempty"`
	FactIDs    []int               `json:"fact_ids,omitempty"`
}

// DocumentInfo contains metadata about a document.
type DocumentInfo struct {
	ID           int      `json:"id"`
	SourceType   string   `json:"source_type"`
	OriginalPath string   `json:"original_path"`
	Metadata     string   `json:"metadata,omitempty"`
	CreatedAt    string   `json:"created_at"`
	UpdatedAt    string   `json:"updated_at"`
	Domains      []string `json:"domains,omitempty"`
}

// ChunkWithContext contains chunk data with offsets and sequence number.
type ChunkWithContext struct {
	ID          int    `json:"id"`
	SequenceNum int    `json:"sequence_num"`
	StartOffset int    `json:"start_offset"`
	EndOffset   int    `json:"end_offset"`
	Text        string `json:"text"`
}

// EntityWithContext contains entity data for document context.
type EntityWithContext struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Domain string `json:"domain"`
}
