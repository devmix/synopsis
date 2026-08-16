package handlers_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/mcp/handlers"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleCatalogOverview(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)
	chunkDAO := dao.NewChunkDAO(db)
	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	// Create documents.
	metaJSON1 := `{"author":"alice","domain":["hr"]}`
	docID1, _ := docDAO.Create(ctx, dao.Document{ //nolint:errcheck
		SourceType:   "markdown",
		OriginalPath: "/docs/hr.md",
		MetadataJSON: &metaJSON1,
	})

	metaJSON2 := `{"domain":["engineering"]}`
	docID2, _ := docDAO.Create(ctx, dao.Document{ //nolint:errcheck
		SourceType:   "json",
		OriginalPath: "/data/engineering.json",
		MetadataJSON: &metaJSON2,
	})

	// Create chunks.
	chunkDAO.Create(ctx, dao.Chunk{DocID: docID1, ChunkText: "HR policy text.", SequenceNum: 1})    //nolint:errcheck
	chunkDAO.Create(ctx, dao.Chunk{DocID: docID1, ChunkText: "More HR text.", SequenceNum: 2})      //nolint:errcheck
	chunkDAO.Create(ctx, dao.Chunk{DocID: docID2, ChunkText: "Engineering specs.", SequenceNum: 1}) //nolint:errcheck

	// Create entities.
	desc := "Senior engineer"
	entID1, _ := entDAO.Create(ctx, dao.Entity{ //nolint:errcheck
		Type:        "employee",
		Name:        "Alice",
		Domain:      "hr",
		Description: &desc,
	})

	entID2, _ := entDAO.Create(ctx, dao.Entity{ //nolint:errcheck
		Type:   "policy",
		Name:   "NDA",
		Domain: "engineering",
	})

	// Create a fact.
	factDAO.Create(ctx, dao.Fact{ //nolint:errcheck
		SubjectEntityID: entID1,
		Predicate:       "owns",
		ObjectEntityID:  entID2,
		Domain:          "hr",
	})

	// Create an entity link.
	linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
		SubjectEntityID: entID1,
		TargetEntityID:  entID2,
		RelationType:    "same_entity",
		Method:          "rule",
		Confidence:      0.95,
	})

	req := buildRequest("catalog_overview", map[string]interface{}{})
	result, err := handlers.HandleCatalogOverview(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleCatalogOverview() returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	if result.IsError {
		t.Errorf("unexpected error response: %s", getContentText(result))
	}

	var resp handlers.CatalogOverviewResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.DocumentCount != 2 {
		t.Errorf("document_count = %d, want 2", resp.DocumentCount)
	}

	if resp.ChunkCount != 3 {
		t.Errorf("chunk_count = %d, want 3", resp.ChunkCount)
	}

	if resp.EntityCount != 2 {
		t.Errorf("entity_count = %d, want 2", resp.EntityCount)
	}

	if resp.FactCount != 1 {
		t.Errorf("fact_count = %d, want 1", resp.FactCount)
	}

	if resp.DocumentsByType["markdown"] != 1 {
		t.Errorf("documents_by_type[markdown] = %d, want 1", resp.DocumentsByType["markdown"])
	}

	if resp.DocumentsByType["json"] != 1 {
		t.Errorf("documents_by_type[json] = %d, want 1", resp.DocumentsByType["json"])
	}

	if resp.EntitiesByType["employee"] != 1 {
		t.Errorf("entities_by_type[employee] = %d, want 1", resp.EntitiesByType["employee"])
	}

	if resp.EntitiesByDomain["hr"] != 1 {
		t.Errorf("entities_by_domain[hr] = %d, want 1", resp.EntitiesByDomain["hr"])
	}

	if len(resp.Domains) == 0 {
		t.Error("domains should not be empty")
	}

	if len(resp.EntityTypes) == 0 {
		t.Error("entity_types should not be empty")
	}

	if resp.GraphNodeCount != 2 {
		t.Errorf("graph_node_count = %d, want 2", resp.GraphNodeCount)
	}

	if resp.GraphEdgeCount != 1 {
		t.Errorf("graph_edge_count = %d, want 1", resp.GraphEdgeCount)
	}
}

func TestHandleCatalogOverview_EmptyDB(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	req := buildRequest("catalog_overview", map[string]interface{}{})
	result, err := handlers.HandleCatalogOverview(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleCatalogOverview() returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	if result.IsError {
		t.Errorf("unexpected error response: %s", getContentText(result))
	}

	var resp handlers.CatalogOverviewResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.DocumentCount != 0 {
		t.Errorf("document_count = %d, want 0", resp.DocumentCount)
	}

	if resp.ChunkCount != 0 {
		t.Errorf("chunk_count = %d, want 0", resp.ChunkCount)
	}

	if resp.EntityCount != 0 {
		t.Errorf("entity_count = %d, want 0", resp.EntityCount)
	}

	if resp.FactCount != 0 {
		t.Errorf("fact_count = %d, want 0", resp.FactCount)
	}
}

func TestHandleCatalogOverview_MultiEdgePath(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	// Create three entities forming a chain: 1→2, 2→3.
	entID1, _ := entDAO.Create(ctx, dao.Entity{ //nolint:errcheck
		Type: "employee", Name: "Node1", Domain: "test",
	})
	entID2, _ := entDAO.Create(ctx, dao.Entity{ //nolint:errcheck
		Type: "employee", Name: "Node2", Domain: "test",
	})
	entID3, _ := entDAO.Create(ctx, dao.Entity{ //nolint:errcheck
		Type: "employee", Name: "Node3", Domain: "test",
	})

	// Link 1→2 and 2→3. The old formula (|subjects|+|targets|-|intersection|) would
	// compute |{1,2}| + |{2,3}| - |{2}| = 2+2-1 = 3, which happens to be correct here,
	// but the actual CASE-WHEN intersection logic was broken for multi-edge paths.
	linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
		SubjectEntityID: entID1, TargetEntityID: entID2, RelationType: "related", Method: "rule", Confidence: 0.9,
	})
	linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
		SubjectEntityID: entID2, TargetEntityID: entID3, RelationType: "related", Method: "rule", Confidence: 0.9,
	})

	req := buildRequest("catalog_overview", map[string]interface{}{})
	result, err := handlers.HandleCatalogOverview(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleCatalogOverview() returned unexpected error: %v", err)
	}

	if result.IsError {
		t.Errorf("unexpected error response: %s", getContentText(result))
	}

	var resp handlers.CatalogOverviewResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Three distinct nodes (1, 2, 3) in the graph.
	if resp.GraphNodeCount != 3 {
		t.Errorf("graph_node_count = %d, want 3 for multi-edge path 1→2, 2→3", resp.GraphNodeCount)
	}

	// Two edges (links).
	if resp.GraphEdgeCount != 2 {
		t.Errorf("graph_edge_count = %d, want 2", resp.GraphEdgeCount)
	}
}

func TestHandleCatalogOverview_DomainsFromMetadata(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)

	// Create a document where domain is in metadata_json.
	metaJSON := `{"domain":["product","hr","engineering"]}`
	_, err = docDAO.Create(ctx, dao.Document{
		SourceType:   "markdown",
		OriginalPath: "/docs/multi-domain.md",
		MetadataJSON: &metaJSON,
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	req := buildRequest("catalog_overview", map[string]interface{}{})
	result, err := handlers.HandleCatalogOverview(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleCatalogOverview() returned unexpected error: %v", err)
	}

	if result.IsError {
		t.Errorf("unexpected error response: %s", getContentText(result))
	}

	var resp handlers.CatalogOverviewResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Domains must be read from metadata when domain column is empty.
	if len(resp.Domains) == 0 {
		t.Error("domains should not be empty — domains exist in metadata")
	}

	expectedDomains := map[string]bool{"product": true, "hr": true, "engineering": true}
	for _, d := range resp.Domains {
		if !expectedDomains[d] {
			t.Errorf("unexpected domain %q", d)
		}
		delete(expectedDomains, d)
	}
	for d := range expectedDomains {
		t.Errorf("missing expected domain %q", d)
	}
}

func TestHandleCatalogOverview_EmptyDB_ReturnsEmptyArrays(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	req := buildRequest("catalog_overview", map[string]interface{}{})
	result, err := handlers.HandleCatalogOverview(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleCatalogOverview() returned unexpected error: %v", err)
	}

	if result.IsError {
		t.Errorf("unexpected error response: %s", getContentText(result))
	}

	var resp handlers.CatalogOverviewResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Domains and EntityTypes must be empty arrays ([]), not null.
	if resp.Domains == nil {
		t.Error("domains should be [] (empty array), not null")
	}
	if resp.EntityTypes == nil {
		t.Error("entity_types should be [] (empty array), not null")
	}

	// Verify JSON serialization produces "[]" not "null".
	jsonBytes, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonStr := string(jsonBytes)
	if !strings.Contains(jsonStr, `"domains":[]`) {
		t.Errorf("JSON should contain \"domains\":[] but got domains field as: %s", extractJSONField(jsonStr, "domains"))
	}
	if !strings.Contains(jsonStr, `"entity_types":[]`) {
		t.Errorf("JSON should contain \"entity_types\":[] but got entity_types field as: %s", extractJSONField(jsonStr, "entity_types"))
	}
}

func extractJSONField(jsonStr, key string) string {
	keyPrefix := `"` + key + `":`
	idx := strings.Index(jsonStr, keyPrefix)
	if idx < 0 {
		return "<not found>"
	}
	start := idx + len(keyPrefix)
	for start < len(jsonStr) && (jsonStr[start] == ' ' || jsonStr[start] == '\n' || jsonStr[start] == '\t') {
		start++
	}
	if start >= len(jsonStr) {
		return "<truncated>"
	}
	if jsonStr[start] == '[' {
		end := strings.Index(jsonStr[start:], "]")
		if end < 0 {
			return "<unclosed array>"
		}
		return jsonStr[start : start+end+1]
	}
	if jsonStr[start] == '"' {
		end := strings.Index(jsonStr[start+1:], `"`)
		if end < 0 {
			return "<unclosed string>"
		}
		return jsonStr[start : start+end+2]
	}
	// number or null
	end := start
	for end < len(jsonStr) && jsonStr[end] != ',' && jsonStr[end] != '}' && jsonStr[end] != '\n' {
		end++
	}
	return strings.TrimSpace(jsonStr[start:end])
}
