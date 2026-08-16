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

// HandleGetChunkByID processes a get_chunk_by_id tool call.
// It retrieves chunk data with document info and associated entities.
func HandleGetChunkByID(
	ctx context.Context,
	req mcp.CallToolRequest,
	db *sql.DB,
) (*mcp.CallToolResult, error) {
	chunkIDStr := req.GetString("chunk_id", "")
	if chunkIDStr == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Error: 'chunk_id' argument is required"),
			},
			IsError: true,
		}, nil
	}

	chunkID, err := strconv.Atoi(chunkIDStr)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error: 'chunk_id' must be an integer, got %q", chunkIDStr)),
			},
			IsError: true,
		}, nil
	}

	chunkDAO := dao.NewChunkDAO(db)
	docDAO := dao.NewDocumentDAO(db)
	chunkEntityDAO := dao.NewChunkEntityDAO(db)
	entityDAO := dao.NewEntityDAO(db)

	chunk, err := chunkDAO.GetByID(ctx, chunkID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error retrieving chunk: %v", err)),
			},
			IsError: true,
		}, nil
	}

	if chunk == nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Chunk with Predicate %d not found", chunkID)),
			},
			IsError: true,
		}, nil
	}

	doc, err := docDAO.GetByID(ctx, chunk.DocID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error retrieving document: %v", err)),
			},
			IsError: true,
		}, nil
	}

	response := ChunkByIDResponse{
		Chunk: ChunkInfo{
			ID:          chunk.ID,
			DocID:       chunk.DocID,
			ChunkText:   chunk.ChunkText,
			SequenceNum: chunk.SequenceNum,
			CreatedAt:   chunk.CreatedAt,
		},
	}

	if chunk.StartOffset != nil {
		response.Chunk.StartOffset = *chunk.StartOffset
	}
	if chunk.EndOffset != nil {
		response.Chunk.EndOffset = *chunk.EndOffset
	}

	if doc != nil {
		response.Document = &DocumentBrief{
			ID:           doc.ID,
			SourceType:   doc.SourceType,
			OriginalPath: doc.OriginalPath,
		}
	}

	entityIDs, err := chunkEntityDAO.GetEntitiesByChunk(ctx, chunkID)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error retrieving entities for chunk %d: %v", chunkID, err)),
			},
			IsError: true,
		}, nil
	}

	if len(entityIDs) > 0 {
		entityMap, err := entityDAO.GetByIDs(ctx, entityIDs)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Error retrieving entities: %v", err)),
				},
				IsError: true,
			}, nil
		}

		for _, id := range entityIDs {
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

// ChunkByIDResponse is the JSON response format for get_chunk_by_id.
type ChunkByIDResponse struct {
	Chunk    ChunkInfo           `json:"chunk"`
	Document *DocumentBrief      `json:"document,omitempty"`
	Entities []EntityWithContext `json:"entities,omitempty"`
}

// ChunkInfo contains chunk data with offsets and sequence number.
type ChunkInfo struct {
	ID          int    `json:"id"`
	DocID       int    `json:"doc_id"`
	ChunkText   string `json:"chunk_text"`
	SequenceNum int    `json:"sequence_num"`
	StartOffset int    `json:"start_offset,omitempty"`
	EndOffset   int    `json:"end_offset,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// DocumentBrief contains minimal document metadata.
type DocumentBrief struct {
	ID           int    `json:"id"`
	SourceType   string `json:"source_type"`
	OriginalPath string `json:"original_path"`
}
