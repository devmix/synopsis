package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/mcp/handlers"

	"github.com/mark3labs/mcp-go/mcp"
)

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)
	return db
}

func TestHandleGetEntityLinks(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	// Create test entities.
	descA := "Entity A description"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &descA})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering"})
	idC, _ := entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "NDA"})

	// Create links: Alice -> Engineering (rule), Alice -> NDA (llm).
	evidence1 := "Rule-based matching on keyword 'Alice'"
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idB, RelationType: "located_in",
		Method: "rule", Confidence: 0.95, Evidence: &evidence1,
	})
	evidence2 := "LLM inference from context"
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idC, RelationType: "owns",
		Method: "llm", Confidence: 0.78, Evidence: &evidence2,
	})

	tests := []struct {
		name         string
		args         map[string]interface{}
		wantIsError  bool
		wantContains string // substring expected in response text
	}{
		{
			name:         "MissingEntityID",
			args:         map[string]interface{}{"entity_id": ""},
			wantIsError:  true,
			wantContains: "must be provided",
		},
		{
			name:         "NonIntegerEntityID",
			args:         map[string]interface{}{"entity_id": "abc"},
			wantIsError:  true,
			wantContains: "must be an integer",
		},
		{
			name:         "EntityNotFound",
			args:         map[string]interface{}{"entity_id": "99999"},
			wantIsError:  true,
			wantContains: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := buildRequest("get_entity_links", tt.args)
			result, err := handlers.HandleGetEntityLinks(context.Background(), req, db)
			if err != nil {
				t.Fatalf("HandleGetEntityLinks() returned unexpected error: %v", err)
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

func TestHandleGetEntityLinks_EntityWithNoLinks(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)
	desc := "Lone entity"
	id, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "SoloEmployee", Description: &desc})

	req := buildRequest("get_entity_links", map[string]interface{}{"entity_id": fmt.Sprintf("%d", id)})
	result, err := handlers.HandleGetEntityLinks(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks() error = %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	var resp handlers.EntityLinksResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Entity.ID != id {
		t.Errorf("Entity.Predicate = %d, want %d", resp.Entity.ID, id)
	}

	if len(resp.Links) != 0 {
		t.Errorf("Links length = %d, want 0 (entity has no links)", len(resp.Links))
	}
}

func TestHandleGetEntityLinks_EntityWithLinks(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	descA := "Employee entity"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &descA})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering"})

	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idB, RelationType: "works_in",
		Method: "rule", Confidence: 0.9, Evidence: nil,
	})

	req := buildRequest("get_entity_links", map[string]interface{}{"entity_id": fmt.Sprintf("%d", idA)})
	result, err := handlers.HandleGetEntityLinks(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks() error = %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	var resp handlers.EntityLinksResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Links) != 1 {
		t.Fatalf("Links length = %d, want 1", len(resp.Links))
	}

	link := resp.Links[0]
	if link.TargetEntityID != idB {
		t.Errorf("TargetEntityID = %d, want %d", link.TargetEntityID, idB)
	}
	if link.TargetName != "Engineering" {
		t.Errorf("TargetName = %q, want %q", link.TargetName, "Engineering")
	}
	if link.RelationType != "works_in" {
		t.Errorf("RelationType = %q, want %q", link.RelationType, "works_in")
	}
}

func TestHandleGetEntityLinks_LinksProvenance(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	descA := "Source entity"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Bob", Description: &descA})
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "department", Name: "IT"})

	evidenceText := "Found in chapter 3, paragraph 2"
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: 2, RelationType: "resides_in",
		Method: "llm", Confidence: 0.85, Evidence: &evidenceText,
	})

	req := buildRequest("get_entity_links", map[string]interface{}{"entity_id": fmt.Sprintf("%d", idA)})
	result, err := handlers.HandleGetEntityLinks(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks() error = %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	var resp handlers.EntityLinksResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Links) == 0 {
		t.Fatal("expected at least one link")
	}

	link := resp.Links[0]
	if link.Method != "llm" {
		t.Errorf("Method = %q, want %q", link.Method, "llm")
	}
	if link.Confidence != 0.85 {
		t.Errorf("Confidence = %v, want 0.85", link.Confidence)
	}
	if !contains(link.Evidence, "chapter 3") {
		t.Errorf("Evidence should contain 'chapter 3', got: %q", link.Evidence)
	}
}

func TestHandleGetEntityLinks_DedupBidirectional(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	// Create two entities.
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice"})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Office"})

	// Create bidirectional link (A→B and B→A) with same relation_type.
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: idB, RelationType: "works_in",
		Method: "rule", Confidence: 1.0, Evidence: nil,
	})
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idB, TargetEntityID: idA, RelationType: "works_in",
		Method: "rule", Confidence: 1.0, Evidence: nil,
	})

	req := buildRequest("get_entity_links", map[string]interface{}{"entity_id": fmt.Sprintf("%d", idA)})
	result, err := handlers.HandleGetEntityLinks(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks() error = %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	var resp handlers.EntityLinksResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Bidirectional pair (A→B and B→A) with same relation_type must produce ONE link.
	if len(resp.Links) != 1 {
		t.Errorf("Links length = %d, want 1 (dedup bidirectional)", len(resp.Links))
		for i, l := range resp.Links {
			t.Logf("  link[%d]: target=%d relation=%s", i, l.TargetEntityID, l.RelationType)
		}
	}

	if len(resp.Links) > 0 && resp.Links[0].TargetEntityID != idB {
		t.Errorf("TargetEntityID = %d, want %d", resp.Links[0].TargetEntityID, idB)
	}
}

func TestHandleGetEntityLinks_NilTargetGuard(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	// Create source entity.
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Charlie"})

	// Create a link pointing to an entity that doesn't exist (simulates deleted target).
	_, _ = linkDAO.Create(ctx, dao.EntityLink{
		SubjectEntityID: idA, TargetEntityID: 99999, RelationType: "knows",
		Method: "rule", Confidence: 1.0, Evidence: nil,
	})

	req := buildRequest("get_entity_links", map[string]interface{}{"entity_id": fmt.Sprintf("%d", idA)})
	result, err := handlers.HandleGetEntityLinks(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks() error = %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success (nil-target guard), got error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	var resp handlers.EntityLinksResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// The link to non-existent entity 99999 must be silently skipped.
	if len(resp.Links) != 0 {
		t.Errorf("Links length = %d, want 0 (nil-target guard should skip missing target)", len(resp.Links))
	}
}

func TestHandleGetEntityLinks_DomainDisambiguation(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)

	// Create two entities with the same name in different domains.
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Domain: "hr"})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Domain: "product"})

	tests := []struct {
		name         string
		args         map[string]interface{}
		wantIsError  bool
		wantContains string
	}{
		{
			name:         "DomainDisambiguatesMultipleMatches",
			args:         map[string]interface{}{"entity_name": "Alice", "domain": "hr"},
			wantIsError:  false,
			wantContains: fmt.Sprintf(`"id":%d`, idA),
		},
		{
			name:         "CaseInsensitiveDomain",
			args:         map[string]interface{}{"entity_name": "Alice", "domain": "PRODUCT"},
			wantIsError:  false,
			wantContains: fmt.Sprintf(`"id":%d`, idB),
		},
		{
			name:         "NoDomainSingleMatchSucceeds",
			args:         map[string]interface{}{"entity_name": "Alice", "domain": ""},
			wantIsError:  true, // multiple matches → error with suggestions
			wantContains: "Multiple entities match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := buildRequest("get_entity_links", tt.args)
			result, err := handlers.HandleGetEntityLinks(context.Background(), req, db)
			if err != nil {
				t.Fatalf("HandleGetEntityLinks() returned unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("result is nil")
			}

			if result.IsError != tt.wantIsError {
				t.Errorf("IsError = %v, want %v; response: %s", result.IsError, tt.wantIsError, result.Content[0].(mcp.TextContent).Text)
			}

			if len(result.Content) == 0 {
				t.Fatal("response content is empty")
			}

			textContent := result.Content[0].(mcp.TextContent)
			if tt.wantContains != "" && !contains(textContent.Text, tt.wantContains) {
				t.Errorf("response does not contain %q, got: %s", tt.wantContains, textContent.Text)
			}
		})
	}

	// Test: no domain + single match succeeds.
	t.Run("NoDomainSingleMatchSucceeds", func(t *testing.T) {
		// Create a unique entity name so there's only one match.
		idC, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "UniqueBob", Domain: "hr"})

		req := buildRequest("get_entity_links", map[string]interface{}{"entity_name": "UniqueBob"})
		result, err := handlers.HandleGetEntityLinks(context.Background(), req, db)
		if err != nil {
			t.Fatalf("HandleGetEntityLinks() returned unexpected error: %v", err)
		}

		if result.IsError {
			t.Errorf("expected success for single match without domain, got error: %s", result.Content[0].(mcp.TextContent).Text)
		}

		var resp handlers.EntityLinksResponse
		if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if resp.Entity.ID != idC {
			t.Errorf("Entity.Predicate = %d, want %d", resp.Entity.ID, idC)
		}
	})
}
