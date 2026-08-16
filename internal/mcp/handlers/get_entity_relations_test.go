package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/graph"
	"github.com/devmix/synopsis/internal/mcp/handlers"

	"github.com/mark3labs/mcp-go/mcp"
)

// setupTestGraph creates a graph with test entities and relations.
func setupTestGraph(t *testing.T) (*sql.DB, *graph.Graph, map[string]int) {
	t.Helper()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	relDAO := dao.NewFactDAO(db)

	// Build a small graph:
	// Alice --works_in--> Engineering
	// Bob --works_in--> Engineering
	// Alice --owns--> NDA
	// Policy --requires--> Bob
	// Alice --reports_to--> Policy

	descAlice := "Senior engineer"
	idAlice, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Description: &descAlice})
	descEngineering := "Engineering department"
	idEngineering, _ := entDAO.Create(ctx, dao.Entity{Type: "department", Name: "Engineering", Description: &descEngineering})
	descNDA := "Confidentiality agreement"
	idNDA, _ := entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "NDA", Description: &descNDA})
	descBob := "Team lead"
	idBob, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Bob", Description: &descBob})
	descPolicy := "Security policy"
	idPolicy, _ := entDAO.Create(ctx, dao.Entity{Type: "policy", Name: "Security Policy", Description: &descPolicy})

	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "works_in", ObjectEntityID: idEngineering}) //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idBob, Predicate: "works_in", ObjectEntityID: idEngineering})   //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "owns", ObjectEntityID: idNDA})             //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idPolicy, Predicate: "requires", ObjectEntityID: idBob})        //nolint:errcheck
	relDAO.Create(ctx, dao.Fact{SubjectEntityID: idAlice, Predicate: "reports_to", ObjectEntityID: idPolicy})    //nolint:errcheck

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

	entityIDs := map[string]int{
		"Alice":       idAlice,
		"Bob":         idBob,
		"Engineering": idEngineering,
	}

	return db, g, entityIDs
}

func TestHandleGetEntityRelations(t *testing.T) {
	t.Parallel()

	db, g, ids := setupTestGraph(t)

	tests := []struct {
		name         string
		args         map[string]interface{}
		wantIsError  bool
		wantContains string // substring expected in response text
	}{
		{
			name:         "empty entity_id and entity_name returns error",
			args:         map[string]interface{}{},
			wantIsError:  true,
			wantContains: "must be provided",
		},
		{
			name:         "non-integer entity_id returns error",
			args:         map[string]interface{}{"entity_id": "not_a_number"},
			wantIsError:  true,
			wantContains: "must be an integer",
		},
		{
			name:         "valid entity_id finds Alice relations",
			args:         map[string]interface{}{"entity_id": fmt.Sprintf("%d", ids["Alice"]), "depth": float64(1)},
			wantIsError:  false,
			wantContains: `"center_entity"`,
		},
		{
			name:         "valid entity_name finds Alice relations",
			args:         map[string]interface{}{"entity_name": "Alice", "depth": float64(1)},
			wantIsError:  false,
			wantContains: `"center_entity"`,
		},
		{
			name:         "nonexistent entity returns error",
			args:         map[string]interface{}{"entity_id": "99999"},
			wantIsError:  true,
			wantContains: "not found",
		},
		{
			name:         "depth zero defaults to 1",
			args:         map[string]interface{}{"entity_id": fmt.Sprintf("%d", ids["Alice"]), "depth": float64(0)},
			wantIsError:  false,
			wantContains: `"center_entity"`,
		},
		{
			name:         "depth over 10 capped at 10",
			args:         map[string]interface{}{"entity_id": fmt.Sprintf("%d", ids["Alice"]), "depth": float64(50)},
			wantIsError:  false,
			wantContains: `"center_entity"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := buildRequest("get_entity_relations", tt.args)
			result, err := handlers.HandleGetEntityRelations(context.Background(), req, db, g)
			if err != nil {
				t.Fatalf("HandleGetEntityRelations() returned unexpected error: %v", err)
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

func TestHandleGetEntityRelations_ResponseFields(t *testing.T) {
	t.Parallel()

	db, g, ids := setupTestGraph(t)

	req := buildRequest("get_entity_relations", map[string]interface{}{
		"entity_id": fmt.Sprintf("%d", ids["Alice"]),
		"depth":     float64(1),
	})

	result, err := handlers.HandleGetEntityRelations(context.Background(), req, db, g)
	if err != nil {
		t.Fatalf("HandleGetEntityRelations() error = %v", err)
	}

	var resp handlers.EntityRelationsResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.CenterEntity.Name != "Alice" {
		t.Errorf("CenterEntity.Name = %q, want %q", resp.CenterEntity.Name, "Alice")
	}

	if resp.CenterEntity.ID != ids["Alice"] {
		t.Errorf("CenterEntity.Predicate = %d, want %d", resp.CenterEntity.ID, ids["Alice"])
	}

	if resp.TotalNodes < 1 {
		t.Errorf("TotalNodes = %d, expected at least 1 connected node", resp.TotalNodes)
	}

	if resp.TraversalDepth != 1 {
		t.Errorf("TraversalDepth = %d, want 1", resp.TraversalDepth)
	}

	if resp.TraversalTimeMs < 0 {
		t.Errorf("TraversalTimeMs should be non-negative, got %d", resp.TraversalTimeMs)
	}

	// Verify edge format has new fields.
	for _, e := range resp.Edges {
		if e.SourceID == 0 || e.TargetID == 0 {
			t.Errorf("Edge missing source_id or target_id: %+v", e)
		}
		if e.SourceName == "" || e.TargetName == "" {
			t.Errorf("Edge missing source_name or target_name: %+v", e)
		}
	}
}

func TestHandleGetEntityRelations_NilGraph(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)

	req := buildRequest("get_entity_relations", map[string]interface{}{
		"entity_id": "1",
	})

	result, err := handlers.HandleGetEntityRelations(context.Background(), req, db, nil)
	if err != nil {
		t.Fatalf("HandleGetEntityRelations() returned unexpected error: %v", err)
	}

	if result == nil {
		t.Fatal("result is nil")
	}

	if !result.IsError {
		t.Error("expected IsError=true for nil graph")
	}

	if len(result.Content) == 0 {
		t.Fatal("response content is empty")
	}

	textContent := result.Content[0].(mcp.TextContent)
	expectedMsg := "Knowledge graph is not available"
	found := false
	for i := 0; i <= len(textContent.Text)-len(expectedMsg); i++ {
		if textContent.Text[i:i+len(expectedMsg)] == expectedMsg {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("response does not contain %q, got: %s", expectedMsg, textContent.Text)
	}
}

func TestHandleGetEntityRelations_EntityName(t *testing.T) {
	t.Parallel()

	db, g, ids := setupTestGraph(t)

	req := buildRequest("get_entity_relations", map[string]interface{}{
		"entity_name": "Alice",
		"depth":       float64(1),
	})

	result, err := handlers.HandleGetEntityRelations(context.Background(), req, db, g)
	if err != nil {
		t.Fatalf("HandleGetEntityRelations() error = %v", err)
	}

	if result.IsError {
		t.Fatalf("expected success, got error: %s", result.Content[0].(mcp.TextContent).Text)
	}

	var resp handlers.EntityRelationsResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.CenterEntity.Name != "Alice" {
		t.Errorf("CenterEntity.Name = %q, want %q", resp.CenterEntity.Name, "Alice")
	}

	if resp.CenterEntity.ID != ids["Alice"] {
		t.Errorf("CenterEntity.Predicate = %d, want %d", resp.CenterEntity.ID, ids["Alice"])
	}
}

func TestHandleGetEntityRelations_EntityNameNotFound(t *testing.T) {
	t.Parallel()

	db, g, _ := setupTestGraph(t)

	req := buildRequest("get_entity_relations", map[string]interface{}{
		"entity_name": "NonExistentEntity",
	})

	result, err := handlers.HandleGetEntityRelations(context.Background(), req, db, g)
	if err != nil {
		t.Fatalf("HandleGetEntityRelations() error = %v", err)
	}

	if !result.IsError {
		t.Error("expected IsError=true for nonexistent entity name")
	}

	textContent := result.Content[0].(mcp.TextContent)
	if !contains(textContent.Text, "not found") {
		t.Errorf("response should contain 'not found', got: %s", textContent.Text)
	}
}

func TestHandleGetEntityRelations_BothIDAndName(t *testing.T) {
	t.Parallel()

	db, g, _ := setupTestGraph(t)

	req := buildRequest("get_entity_relations", map[string]interface{}{
		"entity_id":   "1",
		"entity_name": "Alice",
	})

	result, err := handlers.HandleGetEntityRelations(context.Background(), req, db, g)
	if err != nil {
		t.Fatalf("HandleGetEntityRelations() error = %v", err)
	}

	if !result.IsError {
		t.Error("expected IsError=true when both entity_id and entity_name provided")
	}

	textContent := result.Content[0].(mcp.TextContent)
	if !contains(textContent.Text, "not both") {
		t.Errorf("response should mention 'not both', got: %s", textContent.Text)
	}
}

func TestHandleGetEntityRelations_DomainDisambiguation(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)

	// Create two entities with the same name in different domains.
	idA, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Domain: "hr"})
	idB, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice", Domain: "product"})

	// Create a third entity and link it to Alice in hr domain.
	idC, _ := entDAO.Create(ctx, dao.Entity{Type: "organization", Name: "Acme Corp", Domain: "hr"})
	factDAO.CreateOrIgnore(ctx, dao.Fact{ //nolint:errcheck
		SubjectEntityID: idA, Predicate: "works_at", ObjectEntityID: idC, Domain: "hr",
	})

	g, _, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("NewGraphFromDB() error = %v", err)
	}

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
			name:         "NoDomainMultipleMatchesReturnsCandidates",
			args:         map[string]interface{}{"entity_name": "Alice"},
			wantIsError:  true,
			wantContains: "Multiple entities match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := buildRequest("get_entity_relations", tt.args)
			result, err := handlers.HandleGetEntityRelations(context.Background(), req, db, g)
			if err != nil {
				t.Fatalf("HandleGetEntityRelations() returned unexpected error: %v", err)
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
		idD, _ := entDAO.Create(ctx, dao.Entity{Type: "employee", Name: "UniqueBob", Domain: "hr"})

		// Rebuild graph to include the new entity.
		g2, _, err := graph.NewGraphFromDB(ctx, db)
		if err != nil {
			t.Fatalf("NewGraphFromDB() error = %v", err)
		}

		req := buildRequest("get_entity_relations", map[string]interface{}{"entity_name": "UniqueBob"})
		result, err := handlers.HandleGetEntityRelations(context.Background(), req, db, g2)
		if err != nil {
			t.Fatalf("HandleGetEntityRelations() returned unexpected error: %v", err)
		}

		if result.IsError {
			t.Errorf("expected success for single match without domain, got error: %s", result.Content[0].(mcp.TextContent).Text)
		}

		var resp handlers.EntityRelationsResponse
		if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if resp.CenterEntity.ID != idD {
			t.Errorf("CenterEntity.Predicate = %d, want %d", resp.CenterEntity.ID, idD)
		}
	})
}
