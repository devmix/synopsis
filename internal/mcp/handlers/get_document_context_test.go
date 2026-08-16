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

func TestHandleGetDocumentContext(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)
	chunkDAO := dao.NewChunkDAO(db)

	metaJSON := `{"author":"test","tags":["hr","policy"],"domain":["hr","policy"]}`
	docID, _ := docDAO.Create(ctx, dao.Document{
		SourceType:   "markdown",
		OriginalPath: "/docs/test.md",
		MetadataJSON: &metaJSON,
	})

	chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "First chunk text.", SequenceNum: 1})  //nolint:errcheck
	chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Second chunk text.", SequenceNum: 2}) //nolint:errcheck

	tests := []struct {
		name         string
		args         map[string]interface{}
		wantIsError  bool
		wantContains string // substring expected in response text
	}{
		{
			name:         "empty document id returns error",
			args:         map[string]interface{}{"document_id": ""},
			wantIsError:  true,
			wantContains: "'document_id' argument is required",
		},
		{
			name:         "missing document id returns error",
			args:         map[string]interface{}{},
			wantIsError:  true,
			wantContains: "'document_id' argument is required",
		},
		{
			name:         "non-integer document id returns error",
			args:         map[string]interface{}{"document_id": "not_a_number"},
			wantIsError:  true,
			wantContains: "must be an integer",
		},
		{
			name:         "valid document returns metadata with chunks",
			args:         map[string]interface{}{"document_id": strconv.Itoa(docID)},
			wantIsError:  false,
			wantContains: `"chunk_count"`,
		},
		{
			name:         "nonexistent document id returns error",
			args:         map[string]interface{}{"document_id": "99999"},
			wantIsError:  true,
			wantContains: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := buildRequest("get_document_context", tt.args)
			result, err := handlers.HandleGetDocumentContext(context.Background(), req, db)
			if err != nil {
				t.Fatalf("HandleGetDocumentContext() returned unexpected error: %v", err)
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

			if tt.wantContains != "" {
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

func TestHandleGetDocumentContext_ResponseFields(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)
	chunkDAO := dao.NewChunkDAO(db)

	metaJSON := `{"author":"test","domain":["hr"]}`
	docID, _ := docDAO.Create(ctx, dao.Document{
		SourceType:   "json",
		OriginalPath: "/data/policy.json",
		MetadataJSON: &metaJSON,
	})

	chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Chunk one.", SequenceNum: 1})   //nolint:errcheck
	chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Chunk two.", SequenceNum: 2})   //nolint:errcheck
	chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Chunk three.", SequenceNum: 3}) //nolint:errcheck

	req := buildRequest("get_document_context", map[string]interface{}{"document_id": strconv.Itoa(docID)})
	result, err := handlers.HandleGetDocumentContext(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetDocumentContext() error = %v", err)
	}

	var resp handlers.DocumentContextResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Document.SourceType != "json" {
		t.Errorf("SourceType = %q, want %q", resp.Document.SourceType, "json")
	}

	if resp.Document.OriginalPath != "/data/policy.json" {
		t.Errorf("OriginalPath = %q, want %q", resp.Document.OriginalPath, "/data/policy.json")
	}

	if resp.ChunkCount != 3 {
		t.Errorf("ChunkCount = %d, want 3", resp.ChunkCount)
	}

	if len(resp.Chunks) != 3 {
		t.Errorf("Chunks length = %d, want 3", len(resp.Chunks))
	}

	if resp.Document.Metadata == "" {
		t.Error("Metadata should not be empty when metadata_json is set")
	}

	if len(resp.Document.Domains) != 1 || resp.Document.Domains[0] != "hr" {
		t.Errorf("Domains = %v, want [\"hr\"]", resp.Document.Domains)
	}
}

func TestHandleGetDocumentContext_NoMetadata(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)

	docID, _ := docDAO.Create(ctx, dao.Document{
		SourceType:   "markdown",
		OriginalPath: "/docs/plain.md",
		MetadataJSON: nil, // no metadata
	})

	req := buildRequest("get_document_context", map[string]interface{}{"document_id": strconv.Itoa(docID)})
	result, err := handlers.HandleGetDocumentContext(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetDocumentContext() error = %v", err)
	}

	var resp handlers.DocumentContextResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ChunkCount != 0 {
		t.Errorf("ChunkCount = %d, want 0 (no chunks created)", resp.ChunkCount)
	}

	if resp.Document.Metadata != "" {
		t.Error("Metadata should be empty when metadata_json is nil")
	}
}

func TestHandleGetDocumentContext_IncludeChunksFalse(t *testing.T) {
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
		SourceType:   "markdown",
		OriginalPath: "/docs/test.md",
	})

	chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "First chunk.", SequenceNum: 1})  //nolint:errcheck
	chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Second chunk.", SequenceNum: 2}) //nolint:errcheck

	req := buildRequest("get_document_context", map[string]interface{}{
		"document_id":      strconv.Itoa(docID),
		"include_chunks":   false,
		"include_entities": false,
	})
	result, err := handlers.HandleGetDocumentContext(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetDocumentContext() error = %v", err)
	}

	var resp handlers.DocumentContextResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.ChunkCount != 2 {
		t.Errorf("ChunkCount = %d, want 2", resp.ChunkCount)
	}

	if len(resp.Chunks) != 0 {
		t.Errorf("Chunks should be empty when include_chunks=false, got %d chunks", len(resp.Chunks))
	}
}

func TestHandleGetDocumentContext_WithEntities(t *testing.T) {
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

	docID, _ := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/docs/test.md"})

	chunkID1, _ := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Alice works at Acme.", SequenceNum: 1}) //nolint:errcheck
	chunkID2, _ := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Bob manages team.", SequenceNum: 2})    //nolint:errcheck

	entityAlice, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	entityAcme, _ := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	chunkEntityDAO.Link(ctx, chunkID1, entityAlice) //nolint:errcheck
	chunkEntityDAO.Link(ctx, chunkID1, entityAcme)  //nolint:errcheck
	chunkEntityDAO.Link(ctx, chunkID2, entityAlice) //nolint:errcheck

	req := buildRequest("get_document_context", map[string]interface{}{
		"document_id":      strconv.Itoa(docID),
		"include_entities": true,
	})
	result, err := handlers.HandleGetDocumentContext(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetDocumentContext() error = %v", err)
	}

	var resp handlers.DocumentContextResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Entities) < 2 {
		t.Errorf("Entities = %d, want at least 2 (Alice and Acme Corp)", len(resp.Entities))
	}
}

func TestHandleGetDocumentContext_FactsMultipleEntitiesPerChunk(t *testing.T) {
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
	factDAO := dao.NewFactDAO(db)

	// Create document with two chunks.
	docID, _ := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/docs/team.md"})

	chunkID1, _ := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Alice works at Acme.", SequenceNum: 1})    //nolint:errcheck
	chunkID2, _ := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Bob leads team at Acme.", SequenceNum: 2}) //nolint:errcheck

	// Create three entities.
	entityAlice, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	entityBob, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Bob", Domain: "hr"})
	entityAcme, _ := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	// Link multiple entities per chunk.
	chunkEntityDAO.Link(ctx, chunkID1, entityAlice) //nolint:errcheck
	// Chunk 1 has Alice + Acme.
	chunkEntityDAO.Link(ctx, chunkID1, entityAcme) //nolint:errcheck
	chunkEntityDAO.Link(ctx, chunkID2, entityBob)  //nolint:errcheck
	// Chunk 2 has Bob + Acme.
	chunkEntityDAO.Link(ctx, chunkID2, entityAcme) //nolint:errcheck

	// Create facts for ALL three entities (not just the first per chunk).
	fact1, _ := factDAO.CreateOrIgnore(ctx, dao.Fact{ //nolint:errcheck
		SubjectEntityID: entityAlice, Predicate: "works_at", ObjectEntityID: entityAcme, Domain: "hr",
	})
	fact2, _ := factDAO.CreateOrIgnore(ctx, dao.Fact{ //nolint:errcheck
		SubjectEntityID: entityBob, Predicate: "leads", ObjectEntityID: entityAcme, Domain: "hr",
	})
	fact3, _ := factDAO.CreateOrIgnore(ctx, dao.Fact{ //nolint:errcheck
		SubjectEntityID: entityAlice, Predicate: "reports_to", ObjectEntityID: entityBob, Domain: "hr",
	})

	req := buildRequest("get_document_context", map[string]interface{}{
		"document_id":      strconv.Itoa(docID),
		"include_facts":    true,
		"include_entities": false,
	})
	result, err := handlers.HandleGetDocumentContext(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetDocumentContext() error = %v", err)
	}

	var resp handlers.DocumentContextResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Verify all three fact IDs are present.
	expectedFacts := map[int]bool{fact1: true, fact2: true, fact3: true}
	if len(resp.FactIDs) < 3 {
		t.Errorf("FactIDs = %v (len=%d), want at least 3 facts", resp.FactIDs, len(resp.FactIDs))
	}

	for _, fid := range resp.FactIDs {
		if !expectedFacts[fid] {
			t.Errorf("unexpected fact_id %d in response", fid)
		}
	}

	// Verify no duplicate fact IDs.
	seen := make(map[int]bool)
	for _, fid := range resp.FactIDs {
		if seen[fid] {
			t.Errorf("duplicate fact_id %d in response", fid)
		}
		seen[fid] = true
	}
}

// TestHandleGetDocumentContext_EntitiesWithoutChunks verifies that entities are returned
// even when include_chunks=false (Subtask 6 regression test).
func TestHandleGetDocumentContext_EntitiesWithoutChunks(t *testing.T) {
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

	docID, _ := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/docs/test.md"})

	chunkID1, _ := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Alice works at Acme.", SequenceNum: 1}) //nolint:errcheck
	chunkID2, _ := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Bob manages team.", SequenceNum: 2})    //nolint:errcheck

	entityAlice, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	entityAcme, _ := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	chunkEntityDAO.Link(ctx, chunkID1, entityAlice) //nolint:errcheck
	chunkEntityDAO.Link(ctx, chunkID1, entityAcme)  //nolint:errcheck
	chunkEntityDAO.Link(ctx, chunkID2, entityAlice) //nolint:errcheck

	req := buildRequest("get_document_context", map[string]interface{}{
		"document_id":      strconv.Itoa(docID),
		"include_chunks":   false,
		"include_entities": true,
	})
	result, err := handlers.HandleGetDocumentContext(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetDocumentContext() error = %v", err)
	}

	var resp handlers.DocumentContextResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Chunks) != 0 {
		t.Errorf("Chunks should be empty when include_chunks=false, got %d chunks", len(resp.Chunks))
	}

	if resp.ChunkCount != 2 {
		t.Errorf("ChunkCount = %d, want 2", resp.ChunkCount)
	}

	if len(resp.Entities) < 2 {
		t.Errorf("Entities = %d, want at least 2 (Alice and Acme Corp)", len(resp.Entities))
	}
}

// TestHandleGetDocumentContext_FactsWithoutChunks verifies that fact IDs are returned
// even when include_chunks=false (Subtask 6 regression test).
func TestHandleGetDocumentContext_FactsWithoutChunks(t *testing.T) {
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
	factDAO := dao.NewFactDAO(db)

	docID, _ := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/docs/team.md"})

	chunkID1, _ := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Alice works at Acme.", SequenceNum: 1})    //nolint:errcheck
	chunkID2, _ := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Bob leads team at Acme.", SequenceNum: 2}) //nolint:errcheck

	entityAlice, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	entityBob, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Bob", Domain: "hr"})
	entityAcme, _ := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	chunkEntityDAO.Link(ctx, chunkID1, entityAlice) //nolint:errcheck
	chunkEntityDAO.Link(ctx, chunkID1, entityAcme)  //nolint:errcheck
	chunkEntityDAO.Link(ctx, chunkID2, entityBob)   //nolint:errcheck
	chunkEntityDAO.Link(ctx, chunkID2, entityAcme)  //nolint:errcheck

	fact1, _ := factDAO.CreateOrIgnore(ctx, dao.Fact{SubjectEntityID: entityAlice, Predicate: "works_at", ObjectEntityID: entityAcme, Domain: "hr"})   //nolint:errcheck
	fact2, _ := factDAO.CreateOrIgnore(ctx, dao.Fact{SubjectEntityID: entityBob, Predicate: "leads", ObjectEntityID: entityAcme, Domain: "hr"})          //nolint:errcheck
	fact3, _ := factDAO.CreateOrIgnore(ctx, dao.Fact{SubjectEntityID: entityAlice, Predicate: "reports_to", ObjectEntityID: entityBob, Domain: "hr"})     //nolint:errcheck

	req := buildRequest("get_document_context", map[string]interface{}{
		"document_id":      strconv.Itoa(docID),
		"include_chunks":   false,
		"include_entities": false,
		"include_facts":    true,
	})
	result, err := handlers.HandleGetDocumentContext(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetDocumentContext() error = %v", err)
	}

	var resp handlers.DocumentContextResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	expectedFacts := map[int]bool{fact1: true, fact2: true, fact3: true}
	if len(resp.FactIDs) < 3 {
		t.Errorf("FactIDs = %v (len=%d), want at least 3 facts", resp.FactIDs, len(resp.FactIDs))
	}

	for _, fid := range resp.FactIDs {
		if !expectedFacts[fid] {
			t.Errorf("unexpected fact_id %d in response", fid)
		}
	}
}

// TestHandleGetDocumentContext_EntitiesAndFactsWithoutChunks verifies both entities and facts
// are returned when include_chunks=false (Subtask 6 regression test).
func TestHandleGetDocumentContext_EntitiesAndFactsWithoutChunks(t *testing.T) {
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
	factDAO := dao.NewFactDAO(db)

	docID, _ := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/docs/team.md"})

	chunkID1, _ := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Alice works at Acme.", SequenceNum: 1}) //nolint:errcheck
	entityAlice, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	entityAcme, _ := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	chunkEntityDAO.Link(ctx, chunkID1, entityAlice) //nolint:errcheck
	chunkEntityDAO.Link(ctx, chunkID1, entityAcme)  //nolint:errcheck

	fact1, _ := factDAO.CreateOrIgnore(ctx, dao.Fact{SubjectEntityID: entityAlice, Predicate: "works_at", ObjectEntityID: entityAcme, Domain: "hr"}) //nolint:errcheck

	req := buildRequest("get_document_context", map[string]interface{}{
		"document_id":      strconv.Itoa(docID),
		"include_chunks":   false,
		"include_entities": true,
		"include_facts":    true,
	})
	result, err := handlers.HandleGetDocumentContext(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetDocumentContext() error = %v", err)
	}

	var resp handlers.DocumentContextResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Chunks) != 0 {
		t.Errorf("Chunks should be empty when include_chunks=false, got %d chunks", len(resp.Chunks))
	}

	if len(resp.Entities) < 2 {
		t.Errorf("Entities = %d, want at least 2", len(resp.Entities))
	}

	if len(resp.FactIDs) < 1 {
		t.Errorf("FactIDs = %v (len=%d), want at least 1 fact", resp.FactIDs, len(resp.FactIDs))
	}

	found := false
	for _, fid := range resp.FactIDs {
		if fid == fact1 {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected fact_id %d in response", fact1)
	}
}
