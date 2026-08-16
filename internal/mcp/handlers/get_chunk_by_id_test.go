package handlers_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/mcp/handlers"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleGetChunkByID(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)
	chunkDAO := dao.NewChunkDAO(db)
	entityDAO := dao.NewEntityDAO(db)
	chunkEntityDAO := dao.NewChunkEntityDAO(db)

	metaJSON := `{"author":"test"}`
	docID, _ := docDAO.Create(ctx, dao.Document{
		SourceType:   "markdown",
		OriginalPath: "/docs/test.md",
		MetadataJSON: &metaJSON,
	})

	startOffset := 0
	endOffset := 150
	chunkID, _ := chunkDAO.Create(ctx, dao.Chunk{
		DocID:       docID,
		ChunkText:   "Alice works at Acme Corp.",
		SequenceNum: 1,
		StartOffset: &startOffset,
		EndOffset:   &endOffset,
	})

	entityAlice, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	chunkEntityDAO.Link(ctx, chunkID, entityAlice) //nolint:errcheck

	tests := []struct {
		name         string
		args         map[string]interface{}
		wantIsError  bool
		wantContains string
	}{
		{
			name:         "empty chunk id returns error",
			args:         map[string]interface{}{"chunk_id": ""},
			wantIsError:  true,
			wantContains: "'chunk_id' argument is required",
		},
		{
			name:         "missing chunk id returns error",
			args:         map[string]interface{}{},
			wantIsError:  true,
			wantContains: "'chunk_id' argument is required",
		},
		{
			name:         "non-integer chunk id returns error",
			args:         map[string]interface{}{"chunk_id": "abc"},
			wantIsError:  true,
			wantContains: "must be an integer",
		},
		{
			name:         "valid chunk returns data with document and entities",
			args:         map[string]interface{}{"chunk_id": strconv.Itoa(chunkID)},
			wantIsError:  false,
			wantContains: `"chunk_text"`,
		},
		{
			name:         "nonexistent chunk id returns error",
			args:         map[string]interface{}{"chunk_id": "99999"},
			wantIsError:  true,
			wantContains: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := buildRequest("get_chunk_by_id", tt.args)
			result, err := handlers.HandleGetChunkByID(context.Background(), req, db)
			if err != nil {
				t.Fatalf("HandleGetChunkByID() returned unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("result is nil")
			}

			if result.IsError != tt.wantIsError {
				t.Errorf("IsError = %v, want %v", result.IsError, tt.wantIsError)
			}

			if len(result.Content) == 0 {
				t.Fatal("response content is empty")
			}

			textContent := result.Content[0].(mcp.TextContent)

			if tt.wantContains != "" && !tt.wantIsError {
				found := false
				for i := 0; i <= len(textContent.Text)-len(tt.wantContains); i++ {
					if textContent.Text[i:i+len(tt.wantContains)] == tt.wantContains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("response does not contain %q, got: %s", tt.wantContains, textContent.Text)
				}
			}
		})
	}
}

func TestHandleGetChunkByID_ResponseFields(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)
	chunkDAO := dao.NewChunkDAO(db)

	docID, _ := docDAO.Create(ctx, dao.Document{
		SourceType:   "json",
		OriginalPath: "/data/test.json",
	})

	startOffset := 10
	endOffset := 250
	chunkID, _ := chunkDAO.Create(ctx, dao.Chunk{
		DocID:       docID,
		ChunkText:   "Test chunk content.",
		SequenceNum: 3,
		StartOffset: &startOffset,
		EndOffset:   &endOffset,
	})

	req := buildRequest("get_chunk_by_id", map[string]interface{}{"chunk_id": strconv.Itoa(chunkID)})
	result, err := handlers.HandleGetChunkByID(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetChunkByID() error = %v", err)
	}

	var resp handlers.ChunkByIDResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Chunk.ID != chunkID {
		t.Errorf("Chunk.Predicate = %d, want %d", resp.Chunk.ID, chunkID)
	}

	if resp.Chunk.DocID != docID {
		t.Errorf("Chunk.DocID = %d, want %d", resp.Chunk.DocID, docID)
	}

	if resp.Chunk.SequenceNum != 3 {
		t.Errorf("Chunk.SequenceNum = %d, want 3", resp.Chunk.SequenceNum)
	}

	if resp.Chunk.StartOffset != 10 {
		t.Errorf("Chunk.StartOffset = %d, want 10", resp.Chunk.StartOffset)
	}

	if resp.Chunk.EndOffset != 250 {
		t.Errorf("Chunk.EndOffset = %d, want 250", resp.Chunk.EndOffset)
	}

	if resp.Document == nil {
		t.Fatal("Document should not be nil")
	}

	if resp.Document.SourceType != "json" {
		t.Errorf("Document.SourceType = %q, want %q", resp.Document.SourceType, "json")
	}

	if resp.Document.OriginalPath != "/data/test.json" {
		t.Errorf("Document.OriginalPath = %q, want %q", resp.Document.OriginalPath, "/data/test.json")
	}
}
