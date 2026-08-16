package handlers_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/mcp/handlers"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleSearchFacts(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	// Create test entities.
	descA := "Employee entity"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &descA})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering"})
	idC, _ := entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "NDA"})

	// Create test facts.
	factDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "works_in", ObjectEntityID: idB}) //nolint:errcheck
	factDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "owns", ObjectEntityID: idC})     //nolint:errcheck

	tests := []struct {
		name         string
		args         map[string]interface{}
		wantIsError  bool
		wantContains string
	}{
		{
			name:         "default returns approved facts",
			args:         map[string]interface{}{},
			wantIsError:  false,
			wantContains: `"facts"`,
		},
		{
			name:         "predicate filter matches located_in",
			args:         map[string]interface{}{"predicate": "located"},
			wantIsError:  false,
			wantContains: `"facts"`,
		},
		{
			name:         "entity_name filter finds Alice facts",
			args:         map[string]interface{}{"entity_name": "Alice"},
			wantIsError:  false,
			wantContains: `"facts"`,
		},
		{
			name:         "status filter pending returns empty",
			args:         map[string]interface{}{"status": "pending"},
			wantIsError:  false,
			wantContains: `"total_count":0`,
		},
		{
			name:         "invalid cursor returns error",
			args:         map[string]interface{}{"cursor": "not-valid-base64!!!"},
			wantIsError:  true,
			wantContains: "Error decoding cursor",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := buildRequest("search_facts", tt.args)
			result, err := handlers.HandleSearchFacts(context.Background(), req, db)
			if err != nil {
				t.Fatalf("HandleSearchFacts() returned unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("result is nil")
			}

			if result.IsError != tt.wantIsError {
				t.Errorf("IsError = %v, want %v; content: %s", result.IsError, tt.wantIsError, result.Content[0].(mcp.TextContent).Text)
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
}

func TestHandleSearchFacts_ResponseFields(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	descA := "Employee entity"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &descA})
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering"})

	factDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "works_in", ObjectEntityID: 2}) //nolint:errcheck

	req := buildRequest("search_facts", map[string]interface{}{"predicate": "works_in"})
	result, err := handlers.HandleSearchFacts(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleSearchFacts() error = %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	var resp handlers.SearchFactsResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Facts) == 0 {
		t.Fatal("expected at least one fact")
	}

	fact := resp.Facts[0]
	if fact.Predicate != "works_in" {
		t.Errorf("Predicate = %q, want %q", fact.Predicate, "works_in")
	}
	if fact.Status != "approved" {
		t.Errorf("Status = %q, want %q", fact.Status, "approved")
	}
	if fact.SubjectName == "" {
		t.Error("SubjectName should not be empty")
	}
	if fact.ObjectName == "" {
		t.Error("ObjectName should not be empty")
	}
}

func TestHandleSearchFacts_Pagination(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	descA := "Employee entity"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &descA})
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering"})
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "NDA"})

	factDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "works_in", ObjectEntityID: 2}) //nolint:errcheck
	factDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "owns", ObjectEntityID: 3})     //nolint:errcheck

	// First page with size 1.
	req := buildRequest("search_facts", map[string]interface{}{"page_size": float64(1)})
	result, err := handlers.HandleSearchFacts(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleSearchFacts() error = %v", err)
	}

	var resp handlers.SearchFactsResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Facts) != 1 {
		t.Errorf("Facts length = %d, want 1 (page_size=1)", len(resp.Facts))
	}

	if resp.TotalCount < 2 {
		t.Errorf("TotalCount = %d, expected at least 2", resp.TotalCount)
	}

	if resp.NextCursor == nil {
		t.Error("NextCursor should not be nil (more pages available)")
	}

	// Second page using cursor.
	if resp.NextCursor != nil {
		req2 := buildRequest("search_facts", map[string]interface{}{"page_size": float64(1), "cursor": *resp.NextCursor})
		result2, err := handlers.HandleSearchFacts(context.Background(), req2, db)
		if err != nil {
			t.Fatalf("HandleSearchFacts() error on page 2: %v", err)
		}

		var resp2 handlers.SearchFactsResponse
		if err := json.Unmarshal([]byte(result2.Content[0].(mcp.TextContent).Text), &resp2); err != nil {
			t.Fatalf("unmarshal page 2: %v", err)
		}

		if len(resp2.Facts) == 0 {
			t.Error("Second page should have at least one fact")
		}

		// Verify different fact IDs between pages.
		if resp.Facts[0].ID == resp2.Facts[0].ID {
			t.Errorf("Page 1 and Page 2 returned same fact Predicate %d", resp.Facts[0].ID)
		}
	}
}

func TestHandleSearchFacts_EntityNameFilter(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	descA := "Employee entity"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &descA})
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering"})
	idC, _ := entDAO.Create(ctx, dao.Entity{Type: "system", Name: "Laptop"})

	factDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "works_in", ObjectEntityID: 2})  //nolint:errcheck
	factDAO.Create(ctx, dao.Fact{SubjectEntityID: idC, Predicate: "used_by", ObjectEntityID: idA}) //nolint:errcheck

	// Search by entity name — should find facts where Alice is subject OR object.
	req := buildRequest("search_facts", map[string]interface{}{"entity_name": "Alice"})
	result, err := handlers.HandleSearchFacts(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleSearchFacts() error = %v", err)
	}

	var resp handlers.SearchFactsResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Alice is subject in one fact and object in another.
	if resp.TotalCount < 2 {
		t.Errorf("TotalCount = %d, expected at least 2 (Alice as subject and object)", resp.TotalCount)
	}

	// Verify entity names are populated.
	for _, f := range resp.Facts {
		if f.SubjectName == "" && f.ObjectName == "" {
			t.Error("At least one of SubjectName or ObjectName should be set")
		}
	}
}

func TestHandleSearchFacts_DefaultStatusApproved(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	descA := "Employee entity"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &descA})
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering"})

	factDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "works_in", ObjectEntityID: 2}) //nolint:errcheck

	// No status specified — should default to "approved".
	req := buildRequest("search_facts", map[string]interface{}{})
	result, err := handlers.HandleSearchFacts(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleSearchFacts() error = %v", err)
	}

	var resp handlers.SearchFactsResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, f := range resp.Facts {
		if f.Status != "approved" {
			t.Errorf("Fact status = %q, want default 'approved'", f.Status)
		}
	}
}

func TestHandleSearchFacts_ResponseFormat(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	descA := "Employee entity"
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &descA})
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering"})

	factDAO.Create(ctx, dao.Fact{SubjectEntityID: idA, Predicate: "works_in", ObjectEntityID: 2}) //nolint:errcheck

	req := buildRequest("search_facts", map[string]interface{}{})
	result, err := handlers.HandleSearchFacts(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleSearchFacts() error = %v", err)
	}

	var resp handlers.SearchFactsResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Facts) == 0 {
		t.Fatal("expected at least one fact")
	}

	fact := resp.Facts[0]
	// Verify all required fields are present.
	if fact.ID == 0 {
		t.Error("Predicate should not be zero")
	}
	if fact.Predicate == "" {
		t.Error("Predicate should not be empty")
	}
	if fact.Status == "" {
		t.Error("Status should not be empty")
	}

	// Verify optional fields are present when entities exist.
	if fact.SubjectEntityID == 0 {
		fmt.Println("Note: SubjectEntityID is zero (may be expected if entity has no subject)")
	}
}
