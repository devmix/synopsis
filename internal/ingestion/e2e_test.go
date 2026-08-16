// Package ingestion_test contains end-to-end integration tests for cross-domain
// entity links. Tests use a temporary SQLite database, insert documents/chunks/
// entities/links directly (simulating pipeline output), then call the MCP handler
// to verify the full ingest → entity links → MCP read pipeline.
package ingestion_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/mcp/handlers"

	"github.com/mark3labs/mcp-go/mcp"
)

// setupTestDB creates a temporary SQLite database with schema migrations applied.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)
	return db
}

// insertDocument inserts a document into the documents table and returns its Predicate.
func insertDocument(ctx context.Context, db *sql.DB, domain, name, content string) int {
	metaJSON := `{"domain":["` + domain + `"]}`
	query := `INSERT INTO documents (source_type, original_path, metadata_json, content_hash) VALUES (?, ?, ?, ?)`
	result, err := db.ExecContext(ctx, query, "test", "/test/"+name, metaJSON, "test-hash-"+name)
	if err != nil {
		panic("insert document: " + err.Error()) //nolint:forbidigo
	}
	id, _ := result.LastInsertId()
	return int(id)
}

// insertChunks splits content into 2 chunks and inserts them into the chunks table.
func insertChunks(ctx context.Context, db *sql.DB, docID int, content string) {
	splitIdx := strings.Index(content, ". ")
	if splitIdx < 0 || splitIdx > len(content)/2 {
		splitIdx = len(content) / 2
	}
	chunk1 := content[:splitIdx]
	chunk2 := content[splitIdx:]

	query := `INSERT INTO chunks (doc_id, chunk_text, sequence_num) VALUES (?, ?, ?)`
	if _, err := db.ExecContext(ctx, query, docID, chunk1, 1); err != nil {
		panic("insert chunk 1: " + err.Error()) //nolint:forbidigo
	}
	if _, err := db.ExecContext(ctx, query, docID, chunk2, 2); err != nil {
		panic("insert chunk 2: " + err.Error()) //nolint:forbidigo
	}
}

// insertEntity inserts an entity into the entities table and returns its Predicate.
func insertEntity(ctx context.Context, db *sql.DB, name, domain, entityType string) int {
	query := `INSERT INTO entities (type, name, domain, confidence, description, metadata_json) VALUES (?, ?, ?, ?, ?, ?)`
	desc := "Test entity: " + name
	result, err := db.ExecContext(ctx, query, entityType, name, domain, 0.9, &desc, nil)
	if err != nil {
		panic("insert entity: " + err.Error()) //nolint:forbidigo
	}
	id, _ := result.LastInsertId()
	return int(id)
}

// insertEntityLink inserts a link between two entities.
func insertEntityLink(ctx context.Context, db *sql.DB, srcID, tgtID int, relationType, method string, confidence float64, evidence *string) {
	query := `INSERT INTO entity_links (subject_entity_id, target_entity_id, relation_type, method, confidence, evidence) VALUES (?, ?, ?, ?, ?, ?)`
	if _, err := db.ExecContext(ctx, query, srcID, tgtID, relationType, method, confidence, evidence); err != nil {
		panic("insert entity link: " + err.Error()) //nolint:forbidigo
	}
}

// callGetEntityLinks builds an MCP request and calls the handler by entity Predicate.
func callGetEntityLinks(db *sql.DB, entityID int) (*mcp.CallToolResult, error) {
	args := map[string]interface{}{"entity_id": fmt.Sprintf("%d", entityID)}
	req := buildMCPRequest("get_entity_links", args)
	return handlers.HandleGetEntityLinks(context.Background(), req, db)
}

// buildMCPRequest creates a CallToolRequest with the given tool name and arguments.
func buildMCPRequest(toolName string, args map[string]interface{}) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      toolName,
			Arguments: args,
		},
	}
}

// parseResponse unmarshals the MCP text content into an EntityLinksResponse.
func parseResponse(t *testing.T, result *mcp.CallToolResult) handlers.EntityLinksResponse {
	t.Helper()
	if len(result.Content) == 0 {
		t.Fatal("response content is empty")
	}
	textContent := result.Content[0].(mcp.TextContent)
	var resp handlers.EntityLinksResponse
	if err := json.Unmarshal([]byte(textContent.Text), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	return resp
}

// TestE2E_EntityLinksWithLlmLinker verifies that LLM-based entity links are returned
// with correct method, confidence, and evidence through the MCP handler.
func TestE2E_EntityLinksWithLlmLinker(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	_ = insertDocument(ctx, db, "hr", "Employee Handbook",
		"John Smith is the lead engineer. He manages the HR team.")
	insertChunks(ctx, db, 1, "John Smith is the lead engineer. He manages the HR team.")

	idA := insertEntity(ctx, db, "John Smith", "hr", "Person")
	idB := insertEntity(ctx, db, "John Smith", "product", "Person")

	evidence := "LLM matched based on context similarity"
	insertEntityLink(ctx, db, idA, idB, "same_as", "llm", 0.75, &evidence)

	result, err := callGetEntityLinks(db, idA)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks error: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	resp := parseResponse(t, result)

	if resp.Entity.Name != "John Smith" {
		t.Errorf("entity name = %q, want %q", resp.Entity.Name, "John Smith")
	}
	if resp.Entity.Domain != "hr" {
		t.Errorf("entity domain = %q, want %q", resp.Entity.Domain, "hr")
	}

	if len(resp.Links) == 0 {
		t.Fatal("expected at least one link")
	}

	link := resp.Links[0]
	if link.Method != "llm" {
		t.Errorf("link method = %q, want %q", link.Method, "llm")
	}
	if link.Confidence != 0.75 {
		t.Errorf("link confidence = %v, want 0.75", link.Confidence)
	}
	if !strings.Contains(link.Evidence, "LLM matched based on context similarity") {
		t.Errorf("link evidence should contain 'LLM matched...', got: %q", link.Evidence)
	}
	if link.TargetName != "John Smith" {
		t.Errorf("target name = %q, want %q", link.TargetName, "John Smith")
	}
	if link.TargetDomain != "product" {
		t.Errorf("target domain = %q, want %q", link.TargetDomain, "product")
	}
}

// TestE2E_RuleLinks_NoLlm verifies that rule-based entity links are returned with
// method="rule", confidence=1.0, and empty evidence (omitempty).
func TestE2E_RuleLinks_NoLlm(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	_ = insertDocument(ctx, db, "hr", "Employee Handbook",
		"John Smith is the lead engineer. He manages the HR team.")
	insertChunks(ctx, db, 1, "John Smith is the lead engineer. He manages the HR team.")

	idA := insertEntity(ctx, db, "John Smith", "hr", "Person")
	idB := insertEntity(ctx, db, "John Smith", "product", "Person")

	// Rule-based link with no evidence (nil).
	insertEntityLink(ctx, db, idA, idB, "same_as", "rule", 1.0, nil)

	result, err := callGetEntityLinks(db, idA)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks error: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	resp := parseResponse(t, result)

	if resp.Entity.Name != "John Smith" {
		t.Errorf("entity name = %q, want %q", resp.Entity.Name, "John Smith")
	}
	if resp.Entity.Domain != "hr" {
		t.Errorf("entity domain = %q, want %q", resp.Entity.Domain, "hr")
	}

	if len(resp.Links) == 0 {
		t.Fatal("expected at least one link")
	}

	link := resp.Links[0]
	if link.Method != "rule" {
		t.Errorf("link method = %q, want %q", link.Method, "rule")
	}
	if link.Confidence != 1.0 {
		t.Errorf("link confidence = %v, want 1.0", link.Confidence)
	}
	// Evidence should be empty string (omitempty in JSON).
	if link.Evidence != "" {
		t.Errorf("link evidence should be empty for rule method, got: %q", link.Evidence)
	}
}

// TestE2E_McpReadAfterIngestion verifies the full pipeline with multiple links
// of different methods (rule + equals) and correct provenance.
func TestE2E_McpReadAfterIngestion(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	_ = insertDocument(ctx, db, "it", "IT Policy",
		"Alice is the IT administrator. She also handles HR tasks.")
	insertChunks(ctx, db, 1, "Alice is the IT administrator. She also handles HR tasks.")

	idA := insertEntity(ctx, db, "Alice", "it", "Person")
	idB := insertEntity(ctx, db, "Alice", "hr", "Person")
	idC := insertEntity(ctx, db, "Alice", "sales", "Person")

	// Rule link: IT → HR.
	insertEntityLink(ctx, db, idA, idB, "same_as", "rule", 1.0, nil)

	// Equals link: IT → Sales with evidence.
	evidence := "exact match on name+email"
	insertEntityLink(ctx, db, idA, idC, "same_as", "equals", 0.95, &evidence)

	result, err := callGetEntityLinks(db, idA)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks error: %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	resp := parseResponse(t, result)

	if resp.Entity.Name != "Alice" {
		t.Errorf("entity name = %q, want %q", resp.Entity.Name, "Alice")
	}
	if resp.Entity.Domain != "it" {
		t.Errorf("entity domain = %q, want %q", resp.Entity.Domain, "it")
	}

	if len(resp.Links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(resp.Links))
	}

	// Links are ordered by target_entity_id (ORDER BY in ListByEntity).
	// idB < idC so rule link comes first.
	link0 := resp.Links[0]
	link1 := resp.Links[1]

	if link0.Method != "rule" {
		t.Errorf("links[0].method = %q, want %q", link0.Method, "rule")
	}
	if link0.Confidence != 1.0 {
		t.Errorf("links[0].confidence = %v, want 1.0", link0.Confidence)
	}
	if link0.Evidence != "" {
		t.Errorf("links[0].evidence should be empty for rule method, got: %q", link0.Evidence)
	}

	if link1.Method != "equals" {
		t.Errorf("links[1].method = %q, want %q", link1.Method, "equals")
	}
	if link1.Confidence != 0.95 {
		t.Errorf("links[1].confidence = %v, want 0.95", link1.Confidence)
	}
	if !strings.Contains(link1.Evidence, "exact match on name+email") {
		t.Errorf("links[1].evidence should contain 'exact match...', got: %q", link1.Evidence)
	}
}
