package handlers_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/mcp/handlers"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleCatalogEntities(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)

	desc1 := "Senior engineer"
	metaJSON1 := `{"role":"senior_engineer"}`
	entDAO.Create(ctx, dao.Entity{ //nolint:errcheck
		Type:        "employee",
		Name:        "Alice",
		Domain:      "hr",
		Description: &desc1,
		MetadataJSON: &metaJSON1,
	})

	desc2 := "Team lead"
	entDAO.Create(ctx, dao.Entity{ //nolint:errcheck
		Type:        "employee",
		Name:        "Bob",
		Domain:      "engineering",
		Description: &desc2,
	})

	entDAO.Create(ctx, dao.Entity{ //nolint:errcheck
		Type:   "policy",
		Name:   "NDA",
		Domain: "hr",
	})

	tests := []struct {
		name         string
		args         map[string]interface{}
		wantIsError  bool
		wantContains string
	}{
		{
			name:         "empty request returns all entities",
			args:         map[string]interface{}{},
			wantIsError:  false,
			wantContains: `"total_count":3`,
		},
		{
			name:         "type filter works",
			args:         map[string]interface{}{"type": "employee"},
			wantIsError:  false,
			wantContains: `"total_count":2`,
		},
		{
			name:         "domain filter works",
			args:         map[string]interface{}{"domain": "hr"},
			wantIsError:  false,
			wantContains: `"total_count":2`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := buildRequest("catalog_entities", tt.args)
			result, err := handlers.HandleCatalogEntities(context.Background(), req, db)
			if err != nil {
				t.Fatalf("HandleCatalogEntities() returned unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("result is nil")
			}

			if result.IsError != tt.wantIsError {
				t.Errorf("IsError = %v, want %v; content: %s", result.IsError, tt.wantIsError, getContentText(result))
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

func TestHandleCatalogEntities_CursorPagination(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)

	for i := 0; i < 4; i++ {
		entDAO.Create(ctx, dao.Entity{ //nolint:errcheck
			Type:   "employee",
			Name:   "Char_" + string(rune('A'+i)),
			Domain: "hr",
		})
	}

	req := buildRequest("catalog_entities", map[string]interface{}{"page_size": float64(2)})
	result, err := handlers.HandleCatalogEntities(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleCatalogEntities() error = %v", err)
	}

	var resp handlers.CatalogEntitiesResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Entities) != 2 {
		t.Errorf("first page: got %d entities, want 2", len(resp.Entities))
	}

	if resp.TotalCount != 4 {
		t.Errorf("total_count = %d, want 4", resp.TotalCount)
	}

	if resp.NextCursor == nil {
		t.Fatal("next_cursor should not be nil on first page")
	}

	req2 := buildRequest("catalog_entities", map[string]interface{}{"cursor": *resp.NextCursor})
	result2, err := handlers.HandleCatalogEntities(context.Background(), req2, db)
	if err != nil {
		t.Fatalf("HandleCatalogEntities() error on second page = %v", err)
	}

	var resp2 handlers.CatalogEntitiesResponse
	if err := json.Unmarshal([]byte(result2.Content[0].(mcp.TextContent).Text), &resp2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp2.Entities) != 2 {
		t.Errorf("second page: got %d entities, want 2", len(resp2.Entities))
	}

	// Second page is the last page (4 total - 2 per page = exactly 2 pages), so next_cursor should be nil.
	if resp2.NextCursor != nil {
		t.Error("next_cursor should be nil on last page")
	}
}
