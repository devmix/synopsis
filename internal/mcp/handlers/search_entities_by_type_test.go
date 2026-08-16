package handlers_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/mcp/handlers"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleSearchEntitiesByType(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)

	desc1 := "Senior engineer"
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc1})
	desc2 := "Team lead"
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Bob", Description: &desc2})
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering"})

	tests := []struct {
		name         string
		args         map[string]interface{}
		wantIsError  bool
		wantContains string // substring expected in response text
	}{
		{
			name:         "MissingEntityType",
			args:         map[string]interface{}{"entity_type": ""},
			wantIsError:  true,
			wantContains: "'entity_type' argument is required",
		},
		{
			name:         "NoEntityTypeProvided",
			args:         map[string]interface{}{},
			wantIsError:  true,
			wantContains: "'entity_type' argument is required",
		},
		{
			name:         "ValidEmployeeType",
			args:         map[string]interface{}{"entity_type": "employee"},
			wantIsError:  false,
			wantContains: `"entities"`,
		},
		{
			name:         "ValidDepartmentType",
			args:         map[string]interface{}{"entity_type": "department"},
			wantIsError:  false,
			wantContains: `"Engineering"`,
		},
		{
			name:         "NonExistentTypeReturnsEmpty",
			args:         map[string]interface{}{"entity_type": "nonexistent_type_xyz"},
			wantIsError:  false,
			wantContains: `"total_count":0`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := buildRequest("search_entities_by_type", tt.args)
			result, err := handlers.HandleSearchEntitiesByType(context.Background(), req, db)
			if err != nil {
				t.Fatalf("HandleSearchEntitiesByType() returned unexpected error: %v", err)
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

func TestHandleSearchEntitiesByType_ResponseFields(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)

	desc1 := "Senior engineer"
	metaJSON := `{"level": 42, "department": "engineering"}`
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &desc1, MetadataJSON: &metaJSON})

	req := buildRequest("search_entities_by_type", map[string]interface{}{"entity_type": "employee"})
	result, err := handlers.HandleSearchEntitiesByType(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleSearchEntitiesByType() error = %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	var resp handlers.SearchEntitiesByTypeResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Entities) != 1 {
		t.Fatalf("expected 1 entity, got %d", len(resp.Entities))
	}

	entity := resp.Entities[0]
	if entity.Name != "Alice" {
		t.Errorf("Entity.Name = %q, want %q", entity.Name, "Alice")
	}
	if entity.Type != "employee" {
		t.Errorf("Entity.Type = %q, want %q", entity.Type, "employee")
	}
	if resp.TotalCount != 1 {
		t.Errorf("TotalCount = %d, want 1", resp.TotalCount)
	}
}

func TestHandleSearchEntitiesByType_Pagination(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)

	// Create multiple entities of the same type.
	for i := 1; i <= 5; i++ {
		name := "Employee" + string(rune('A'+i-1))
		desc := "Description for " + name
		_, _ = entDAO.Create(ctx, dao.Entity{Type: "employee", Name: name, Description: &desc})
	}

	t.Run("first page with size 2", func(t *testing.T) {
		req := buildRequest("search_entities_by_type", map[string]interface{}{
			"entity_type": "employee",
			"page_size":   float64(2),
		})
		result, err := handlers.HandleSearchEntitiesByType(context.Background(), req, db)
		if err != nil {
			t.Fatalf("HandleSearchEntitiesByType() error = %v", err)
		}

		var resp handlers.SearchEntitiesByTypeResponse
		if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if len(resp.Entities) != 2 {
			t.Errorf("Entities length = %d, want 2", len(resp.Entities))
		}
		if resp.TotalCount != 5 {
			t.Errorf("TotalCount = %d, want 5", resp.TotalCount)
		}
		if resp.NextCursor == nil {
			t.Error("expected next_cursor to be set")
		}
	})

	t.Run("second page via cursor", func(t *testing.T) {
		req := buildRequest("search_entities_by_type", map[string]interface{}{
			"entity_type": "employee",
			"page_size":   float64(2),
		})
		result, err := handlers.HandleSearchEntitiesByType(context.Background(), req, db)
		if err != nil {
			t.Fatalf("HandleSearchEntitiesByType() error = %v", err)
		}

		var resp1 handlers.SearchEntitiesByTypeResponse
		if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp1); err != nil {
			t.Fatalf("unmarshal first page: %v", err)
		}

		if resp1.NextCursor == nil {
			t.Fatal("expected next_cursor on first page")
		}

		req2 := buildRequest("search_entities_by_type", map[string]interface{}{
			"entity_type": "employee",
			"page_size":   float64(2),
			"cursor":      *resp1.NextCursor,
		})
		result2, err := handlers.HandleSearchEntitiesByType(context.Background(), req2, db)
		if err != nil {
			t.Fatalf("HandleSearchEntitiesByType() error = %v", err)
		}

		var resp2 handlers.SearchEntitiesByTypeResponse
		if err := json.Unmarshal([]byte(result2.Content[0].(mcp.TextContent).Text), &resp2); err != nil {
			t.Fatalf("unmarshal second page: %v", err)
		}

		if len(resp2.Entities) != 2 {
			t.Errorf("Entities length = %d, want 2", len(resp2.Entities))
		}

		// Verify no overlap between pages.
		firstPageNames := make(map[string]bool)
		for _, e := range resp1.Entities {
			firstPageNames[e.Name] = true
		}
		for _, e := range resp2.Entities {
			if firstPageNames[e.Name] {
				t.Errorf("entity %q appears in both pages", e.Name)
			}
		}
	})

	t.Run("invalid cursor returns error", func(t *testing.T) {
		req := buildRequest("search_entities_by_type", map[string]interface{}{
			"entity_type": "employee",
			"cursor":      "not-valid-base64!!!",
		})
		result, err := handlers.HandleSearchEntitiesByType(context.Background(), req, db)
		if err != nil {
			t.Fatalf("HandleSearchEntitiesByType() error = %v", err)
		}

		if !result.IsError {
			t.Error("expected IsError=true for invalid cursor")
		}
	})
}

func TestHandleSearchEntitiesByType_DomainFilter(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	ctx := context.Background()

	entDAO := dao.NewEntityDAO(db)

	_, _ = entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Domain: "hr"})
	_, _ = entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Bob", Domain: "it"})

	req := buildRequest("search_entities_by_type", map[string]interface{}{
		"entity_type": "employee",
		"domain":      "hr",
	})
	result, err := handlers.HandleSearchEntitiesByType(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleSearchEntitiesByType() error = %v", err)
	}

	var resp handlers.SearchEntitiesByTypeResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Entities) != 1 {
		t.Errorf("Entities length = %d, want 1 (domain filter)", len(resp.Entities))
	}
	if resp.TotalCount != 1 {
		t.Errorf("TotalCount = %d, want 1 (domain filter)", resp.TotalCount)
	}
	if resp.Entities[0].Name != "Alice" {
		t.Errorf("Entity.Name = %q, want %q", resp.Entities[0].Name, "Alice")
	}
}
