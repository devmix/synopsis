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

func TestHandleCatalogDocuments(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)

	metaJSON1 := `{"author":"alice","domain":["hr","policy"]}`
	docDAO.Create(ctx, dao.Document{ //nolint:errcheck
		SourceType:   "markdown",
		OriginalPath: "/docs/hr_policy.md",
		MetadataJSON: &metaJSON1,
	})

	metaJSON2 := `{"author":"bob","domain":["engineering"]}`
	docDAO.Create(ctx, dao.Document{ //nolint:errcheck
		SourceType:   "json",
		OriginalPath: "/data/engineering.json",
		MetadataJSON: &metaJSON2,
	})

	tests := []struct {
		name         string
		args         map[string]interface{}
		wantIsError  bool
		wantContains string
	}{
		{
			name:         "empty request returns all documents",
			args:         map[string]interface{}{},
			wantIsError:  false,
			wantContains: `"total_count":2`,
		},
		{
			name:         "page_size limits results",
			args:         map[string]interface{}{"page_size": float64(1)},
			wantIsError:  false,
			wantContains: `"next_cursor"`,
		},
		{
			name:         "domain filter works",
			args:         map[string]interface{}{"domain": "hr"},
			wantIsError:  false,
			wantContains: `"total_count":1`,
		},
		{
			name:         "source_type filter works",
			args:         map[string]interface{}{"source_type": "json"},
			wantIsError:  false,
			wantContains: `"total_count":1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := buildRequest("catalog_documents", tt.args)
			result, err := handlers.HandleCatalogDocuments(context.Background(), req, db)
			if err != nil {
				t.Fatalf("HandleCatalogDocuments() returned unexpected error: %v", err)
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

func TestHandleCatalogDocuments_CursorPagination(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)

	for i := 0; i < 5; i++ {
		docDAO.Create(ctx, dao.Document{ //nolint:errcheck
			SourceType:   "markdown",
			OriginalPath: "/docs/doc_" + string(rune('A'+i)) + ".md",
		})
	}

	// First page.
	req := buildRequest("catalog_documents", map[string]interface{}{"page_size": float64(2)})
	result, err := handlers.HandleCatalogDocuments(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleCatalogDocuments() error = %v", err)
	}

	var resp handlers.CatalogDocumentsResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Documents) != 2 {
		t.Errorf("first page: got %d documents, want 2", len(resp.Documents))
	}

	if resp.TotalCount != 5 {
		t.Errorf("total_count = %d, want 5", resp.TotalCount)
	}

	if resp.NextCursor == nil {
		t.Fatal("next_cursor should not be nil on first page")
	}

	// Second page.
	req2 := buildRequest("catalog_documents", map[string]interface{}{"cursor": *resp.NextCursor})
	result2, err := handlers.HandleCatalogDocuments(context.Background(), req2, db)
	if err != nil {
		t.Fatalf("HandleCatalogDocuments() error on second page = %v", err)
	}

	var resp2 handlers.CatalogDocumentsResponse
	if err := json.Unmarshal([]byte(result2.Content[0].(mcp.TextContent).Text), &resp2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp2.Documents) != 2 {
		t.Errorf("second page: got %d documents, want 2", len(resp2.Documents))
	}

	// Verify no overlap between pages.
	for _, d1 := range resp.Documents {
		for _, d2 := range resp2.Documents {
			if d1.ID == d2.ID {
				t.Errorf("page overlap: document Predicate %d appears in both pages", d1.ID)
			}
		}
	}

	// Third page (last).
	req3 := buildRequest("catalog_documents", map[string]interface{}{"cursor": *resp2.NextCursor})
	result3, err := handlers.HandleCatalogDocuments(context.Background(), req3, db)
	if err != nil {
		t.Fatalf("HandleCatalogDocuments() error on third page = %v", err)
	}

	var resp3 handlers.CatalogDocumentsResponse
	if err := json.Unmarshal([]byte(result3.Content[0].(mcp.TextContent).Text), &resp3); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp3.Documents) != 1 {
		t.Errorf("third page: got %d documents, want 1", len(resp3.Documents))
	}

	if resp3.NextCursor != nil {
		t.Error("next_cursor should be nil on last page")
	}
}

func TestHandleCatalogDocuments_InvalidCursor(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	req := buildRequest("catalog_documents", map[string]interface{}{"cursor": "not-valid-base64!!!"})
	result, err := handlers.HandleCatalogDocuments(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleCatalogDocuments() returned unexpected error: %v", err)
	}

	if !result.IsError {
		t.Error("expected error for invalid cursor")
	}
}

func TestPaginationHelper(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cursor  string
		wantOff int
		wantLim int
		wantErr bool
	}{
		{
			name:    "empty cursor returns defaults",
			cursor:  "",
			wantOff: 0,
			wantLim: handlers.DefaultPageSize,
			wantErr: false,
		},
		{
			name:    "valid cursor decodes correctly",
			cursor:  handlers.EncodeCursor(40, 15),
			wantOff: 40,
			wantLim: 15,
			wantErr: false,
		},
		{
			name:    "invalid base64 returns error",
			cursor:  "!!!not-base64!!!",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			offset, limit, err := handlers.DecodeCursor(tt.cursor)
			if (err != nil) != tt.wantErr {
				t.Errorf("DecodeCursor() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if offset != tt.wantOff {
					t.Errorf("offset = %d, want %d", offset, tt.wantOff)
				}
				if limit != tt.wantLim {
					t.Errorf("limit = %d, want %d", limit, tt.wantLim)
				}
			}
		})
	}

	// Test NormalizePageSize.
	if handlers.NormalizePageSize(0) != handlers.DefaultPageSize {
		t.Error("NormalizePageSize(0) should return default")
	}
	if handlers.NormalizePageSize(-1) != handlers.DefaultPageSize {
		t.Error("NormalizePageSize(-1) should return default")
	}
	if handlers.NormalizePageSize(500) != handlers.DefaultPageSize {
		t.Error("NormalizePageSize(500) should return default")
	}
	if handlers.NormalizePageSize(50) != 50 {
		t.Error("NormalizePageSize(50) should return 50")
	}
}
