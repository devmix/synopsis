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

func TestHandleGetFactByID(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entityDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)
	factSourceDAO := dao.NewFactSourceDAO(db)

	subjID, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	objID, _ := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	factID, _ := factDAO.CreateOrIgnore(ctx, dao.Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
		Domain:          "hr",
	})

	quote := "Alice works at Acme Corp"
	factSourceDAO.Create(ctx, dao.FactSource{ //nolint:errcheck
		FactID:     factID,
		DocumentID: 1,
		Quote:      &quote,
	})

	tests := []struct {
		name         string
		args         map[string]interface{}
		wantIsError  bool
		wantContains string
	}{
		{
			name:         "empty fact id returns error",
			args:         map[string]interface{}{"fact_id": ""},
			wantIsError:  true,
			wantContains: "'fact_id' argument is required",
		},
		{
			name:         "missing fact id returns error",
			args:         map[string]interface{}{},
			wantIsError:  true,
			wantContains: "'fact_id' argument is required",
		},
		{
			name:         "non-integer fact id returns error",
			args:         map[string]interface{}{"fact_id": "abc"},
			wantIsError:  true,
			wantContains: "must be an integer",
		},
		{
			name:         "valid fact returns data with entities and sources",
			args:         map[string]interface{}{"fact_id": strconv.Itoa(factID)},
			wantIsError:  false,
			wantContains: `"predicate"`,
		},
		{
			name:         "nonexistent fact id returns error",
			args:         map[string]interface{}{"fact_id": "99999"},
			wantIsError:  true,
			wantContains: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := buildRequest("get_fact_by_id", tt.args)
			result, err := handlers.HandleGetFactByID(context.Background(), req, db)
			if err != nil {
				t.Fatalf("HandleGetFactByID() returned unexpected error: %v", err)
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

func TestHandleGetFactByID_ResponseFields(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entityDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)
	factSourceDAO := dao.NewFactSourceDAO(db)

	subjID, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	objID, _ := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	metaStr := `{"threshold_amount":100}`
	factID, _ := factDAO.CreateOrIgnore(ctx, dao.Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
		Domain:          "hr",
		Metadata:        &metaStr,
	})

	quote := "Alice works at Acme Corp"
	factSourceDAO.Create(ctx, dao.FactSource{ //nolint:errcheck
		FactID:     factID,
		DocumentID: 42,
		Quote:      &quote,
	})

	req := buildRequest("get_fact_by_id", map[string]interface{}{"fact_id": strconv.Itoa(factID)})
	result, err := handlers.HandleGetFactByID(context.Background(), req, db)
	if err != nil {
		t.Fatalf("HandleGetFactByID() error = %v", err)
	}

	var resp handlers.FactByIDResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Fact.ID != factID {
		t.Errorf("Fact.Predicate = %d, want %d", resp.Fact.ID, factID)
	}

	if resp.Fact.Predicate != "works_at" {
		t.Errorf("Fact.Predicate = %q, want %q", resp.Fact.Predicate, "works_at")
	}

	if resp.Fact.Status != "approved" {
		t.Errorf("Fact.Status = %q, want %q", resp.Fact.Status, "approved")
	}

	if resp.SubjectEntity == nil {
		t.Fatal("SubjectEntity should not be nil")
	}

	if resp.SubjectEntity.Name != "Alice" {
		t.Errorf("SubjectEntity.Name = %q, want %q", resp.SubjectEntity.Name, "Alice")
	}

	if resp.ObjectEntity == nil {
		t.Fatal("ObjectEntity should not be nil")
	}

	if resp.ObjectEntity.Name != "Acme Corp" {
		t.Errorf("ObjectEntity.Name = %q, want %q", resp.ObjectEntity.Name, "Acme Corp")
	}

	if len(resp.Sources) != 1 {
		t.Fatalf("Sources length = %d, want 1", len(resp.Sources))
	}

	if resp.Sources[0].DocumentID != 42 {
		t.Errorf("Source.DocumentID = %d, want 42", resp.Sources[0].DocumentID)
	}

	if resp.Sources[0].Quote != quote {
		t.Errorf("Source.Quote = %q, want %q", resp.Sources[0].Quote, quote)
	}
}
