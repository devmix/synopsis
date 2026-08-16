package handlers_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/graph"
	"github.com/devmix/synopsis/internal/mcp/handlers"

	"github.com/mark3labs/mcp-go/mcp"
)

func TestHandleGetEntityDossier(t *testing.T) {
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
	entitySourceDAO := dao.NewEntitySourceDAO(db)
	docDAO := dao.NewDocumentDAO(db)

	// Create entities.
	entityAlice, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	entityAcme, _ := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	// Create a fact linking them.
	factDAO.CreateOrIgnore(ctx, dao.Fact{ //nolint:errcheck
		SubjectEntityID: entityAlice,
		Predicate:       "works_at",
		ObjectEntityID:  entityAcme,
		Domain:          "hr",
	})

	// Create a fact source.
	quote := "Alice works at Acme Corp"
	factDAO2 := dao.NewFactDAO(db)
	facts, _ := factDAO2.ListByEntityID(ctx, entityAlice) //nolint:errcheck
	if len(facts) > 0 {
		factSourceDAO.Create(ctx, dao.FactSource{ //nolint:errcheck
			FactID:     facts[0].ID,
			DocumentID: 1,
			Quote:      &quote,
		})
	}

	// Create a document and link it to the entity.
	docID, _ := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/docs/hr.md"})
	entitySourceDAO.LinkBatch(ctx, docID, []int{entityAlice}) //nolint:errcheck

	tests := []struct {
		name         string
		args         map[string]interface{}
		wantIsError  bool
		wantContains string
	}{
		{
			name:         "empty entity id returns error",
			args:         map[string]interface{}{"entity_id": ""},
			wantIsError:  true,
			wantContains: "must be provided",
		},
		{
			name:         "missing entity id returns error",
			args:         map[string]interface{}{},
			wantIsError:  true,
			wantContains: "must be provided",
		},
		{
			name:         "non-integer entity id returns error",
			args:         map[string]interface{}{"entity_id": "abc"},
			wantIsError:  true,
			wantContains: "must be an integer",
		},
		{
			name:         "valid entity returns dossier with facts and sources",
			args:         map[string]interface{}{"entity_id": strconv.Itoa(entityAlice)},
			wantIsError:  false,
			wantContains: `"name"`,
		},
		{
			name:         "nonexistent entity id returns error",
			args:         map[string]interface{}{"entity_id": "99999"},
			wantIsError:  true,
			wantContains: "not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := buildRequest("get_entity_dossier", tt.args)
			result, err := handlers.HandleGetEntityDossier(context.Background(), req, db, nil)
			if err != nil {
				t.Fatalf("HandleGetEntityDossier() returned unexpected error: %v", err)
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

func TestHandleGetEntityDossier_ResponseFields(t *testing.T) {
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
	entitySourceDAO := dao.NewEntitySourceDAO(db)
	docDAO := dao.NewDocumentDAO(db)

	// Create entities.
	desc := "Senior engineer"
	metaJSON := `{"role":"senior_engineer"}`
	entityAlice, _ := entityDAO.Create(ctx, dao.Entity{
		Type:         "PERSON",
		Name:         "Alice",
		Domain:       "hr",
		Description:  &desc,
		MetadataJSON: &metaJSON,
	})

	entityAcme, _ := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	// Create a fact linking them.
	factDAO.CreateOrIgnore(ctx, dao.Fact{ //nolint:errcheck
		SubjectEntityID: entityAlice,
		Predicate:       "works_at",
		ObjectEntityID:  entityAcme,
		Domain:          "hr",
	})

	// Create a fact source.
	facts, _ := factDAO.ListByEntityID(ctx, entityAlice) //nolint:errcheck
	if len(facts) > 0 {
		quote := "Alice works at Acme Corp"
		factSourceDAO.Create(ctx, dao.FactSource{ //nolint:errcheck
			FactID:     facts[0].ID,
			DocumentID: 1,
			Quote:      &quote,
		})
	}

	// Create a document and link it to the entity.
	docID, _ := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/docs/hr.md"})
	entitySourceDAO.LinkBatch(ctx, docID, []int{entityAlice}) //nolint:errcheck

	req := buildRequest("get_entity_dossier", map[string]interface{}{"entity_id": strconv.Itoa(entityAlice)})
	result, err := handlers.HandleGetEntityDossier(context.Background(), req, db, nil)
	if err != nil {
		t.Fatalf("HandleGetEntityDossier() error = %v", err)
	}

	var resp handlers.EntityDossierResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Entity.ID != entityAlice {
		t.Errorf("Entity.Predicate = %d, want %d", resp.Entity.ID, entityAlice)
	}

	if resp.Entity.Name != "Alice" {
		t.Errorf("Entity.Name = %q, want %q", resp.Entity.Name, "Alice")
	}

	if resp.Entity.Type != "PERSON" {
		t.Errorf("Entity.Type = %q, want %q", resp.Entity.Type, "PERSON")
	}

	if resp.Entity.Domain != "hr" {
		t.Errorf("Entity.Domain = %q, want %q", resp.Entity.Domain, "hr")
	}

	if resp.Entity.Description != desc {
		t.Errorf("Entity.Description = %q, want %q", resp.Entity.Description, desc)
	}

	// Check facts are included.
	if len(resp.Facts) == 0 {
		t.Error("Facts should not be empty when include_facts=true (default)")
	} else if resp.Facts[0].Predicate != "works_at" {
		t.Errorf("Fact.Predicate = %q, want %q", resp.Facts[0].Predicate, "works_at")
	}

	// Check sources are included.
	if len(resp.Sources) == 0 {
		t.Error("Sources should not be empty when include_sources=true (default)")
	} else if resp.Sources[0].ID != docID {
		t.Errorf("Source.Predicate = %d, want %d", resp.Sources[0].ID, docID)
	}
}

func TestHandleGetEntityDossier_ExcludeFactsAndSources(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entityDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)
	entitySourceDAO := dao.NewEntitySourceDAO(db)
	docDAO := dao.NewDocumentDAO(db)

	entityAlice, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	entityAcme, _ := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	factDAO.CreateOrIgnore(ctx, dao.Fact{ //nolint:errcheck
		SubjectEntityID: entityAlice,
		Predicate:       "works_at",
		ObjectEntityID:  entityAcme,
		Domain:          "hr",
	})

	docID, _ := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/docs/hr.md"})
	entitySourceDAO.LinkBatch(ctx, docID, []int{entityAlice}) //nolint:errcheck

	req := buildRequest("get_entity_dossier", map[string]interface{}{
		"entity_id":       strconv.Itoa(entityAlice),
		"include_facts":   false,
		"include_sources": false,
	})
	result, err := handlers.HandleGetEntityDossier(context.Background(), req, db, nil)
	if err != nil {
		t.Fatalf("HandleGetEntityDossier() error = %v", err)
	}

	var resp handlers.EntityDossierResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Facts) != 0 {
		t.Errorf("Facts should be empty when include_facts=false, got %d facts", len(resp.Facts))
	}

	if len(resp.Sources) != 0 {
		t.Errorf("Sources should be empty when include_sources=false, got %d sources", len(resp.Sources))
	}
}

func TestHandleGetEntityDossier_DepthClamping(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entityDAO := dao.NewEntityDAO(db)

	entityAlice, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})

	tests := []struct {
		name  string
		depth float64
	}{
		{"zero depth defaults to 1", 0},
		{"negative depth defaults to 1", -1},
		{"max depth clamped to 5", 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := buildRequest("get_entity_dossier", map[string]interface{}{
				"entity_id": strconv.Itoa(entityAlice),
				"depth":     tt.depth,
			})
			result, err := handlers.HandleGetEntityDossier(context.Background(), req, db, nil)
			if err != nil {
				t.Fatalf("HandleGetEntityDossier() error = %v", err)
			}

			if result.IsError {
				t.Errorf("unexpected error response: %s", result.Content[0].(mcp.TextContent).Text)
			}
		})
	}
}

func TestHandleGetEntityDossier_DomainDisambiguation(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entityDAO := dao.NewEntityDAO(db)

	// Create two entities with the same name in different domains.
	idA, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	idB, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "product"})

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
			req := buildRequest("get_entity_dossier", tt.args)
			result, err := handlers.HandleGetEntityDossier(context.Background(), req, db, nil)
			if err != nil {
				t.Fatalf("HandleGetEntityDossier() returned unexpected error: %v", err)
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
			if tt.wantContains != "" && !tt.wantIsError && !contains(textContent.Text, tt.wantContains) {
				t.Errorf("response does not contain %q, got: %s", tt.wantContains, textContent.Text)
			}
		})
	}

	// Test: no domain + single match succeeds.
	t.Run("NoDomainSingleMatchSucceeds", func(t *testing.T) {
		idC, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "UniqueBob", Domain: "hr"})

		req := buildRequest("get_entity_dossier", map[string]interface{}{"entity_name": "UniqueBob"})
		result, err := handlers.HandleGetEntityDossier(context.Background(), req, db, nil)
		if err != nil {
			t.Fatalf("HandleGetEntityDossier() returned unexpected error: %v", err)
		}

		if result.IsError {
			t.Errorf("expected success for single match without domain, got error: %s", result.Content[0].(mcp.TextContent).Text)
		}

		var resp handlers.EntityDossierResponse
		if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if resp.Entity.ID != idC {
			t.Errorf("Entity.Predicate = %d, want %d", resp.Entity.ID, idC)
		}
	})
}

// crossDomainLinkTestCase defines a single table-driven test case for cross_domain_links behavior.
type crossDomainLinkTestCase struct {
	name       string
	withGraph  bool                                      // whether to build graph from DB (for BFS testing)
	setup      func(ctx context.Context, db *sql.DB) int // returns source entity ID
	assertions func(t *testing.T, resp handlers.EntityDossierResponse, jsonText string)
}

// TestCrossDomainLinks is a table-driven test covering all cross_domain_links requirements:
// same-domain filtering, dedup by target_entity_id, ID↔name correspondence, provenance,
// incident-edge-only processing, incoming links, cross-section dedup, relation_types merging.
func TestCrossDomainLinks(t *testing.T) {
	t.Parallel()

	tests := []crossDomainLinkTestCase{
		{
			name:      "FilterSameDomain",
			withGraph: true,
			setup: func(ctx context.Context, db *sql.DB) int {
				entityDAO := dao.NewEntityDAO(db)
				factDAO := dao.NewFactDAO(db)
				linkDAO := dao.NewEntityLinkDAO(db)

				idHR, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
				_, _ = entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme HR", Domain: "hr"})
				idSameDomain := 2 // second entity created

				// Create a same-domain fact edge.
				factDAO.CreateOrIgnore(ctx, dao.Fact{ //nolint:errcheck
					SubjectEntityID: idHR,
					Predicate:       "works_at",
					ObjectEntityID:  idSameDomain,
					Domain:          "hr",
				})

				idProduct, _ := entityDAO.Create(ctx, dao.Entity{Type: "POLICY", Name: "Hiring Policy", Domain: "product"})
				idIT, _ := entityDAO.Create(ctx, dao.Entity{Type: "POLICY", Name: "IT Policy", Domain: "it"})

				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: idProduct, RelationType: "related_to", Method: "rule", Confidence: 0.95,
				})
				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: idIT, RelationType: "related_to", Method: "equals", Confidence: 0.85,
				})

				return idHR
			},
			assertions: func(t *testing.T, resp handlers.EntityDossierResponse, _ string) {
				t.Helper()
				for _, link := range resp.CrossDomainLinks {
					if link.TargetDomain == "hr" {
						t.Errorf("cross_domain_links contains same-domain entry target_domain=%q for entity %d (%s)",
							link.TargetDomain, link.TargetEntityID, link.TargetName)
					}
				}

				crossDomains := make(map[string]bool)
				for _, link := range resp.CrossDomainLinks {
					crossDomains[link.TargetDomain] = true
				}
				if !crossDomains["product"] {
					t.Error("expected product domain in cross_domain_links")
				}
				if !crossDomains["it"] {
					t.Error("expected it domain in cross_domain_links")
				}

				foundSameDomain := false
				for _, rel := range resp.RelatedEntities {
					if rel.Domain == "hr" && rel.ID != 1 {
						foundSameDomain = true
						break
					}
				}
				if !foundSameDomain {
					t.Error("same-domain entity should be in related_entities")
				}
			},
		},
		{
			name:      "DedupByTargetEntityID",
			withGraph: false,
			setup: func(ctx context.Context, db *sql.DB) int {
				entityDAO := dao.NewEntityDAO(db)
				linkDAO := dao.NewEntityLinkDAO(db)

				idHR, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
				idProduct, _ := entityDAO.Create(ctx, dao.Entity{Type: "POLICY", Name: "Hiring Policy", Domain: "product"})

				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: idProduct, RelationType: "related_to", Method: "rule", Confidence: 0.7,
				})
				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: idProduct, RelationType: "equals", Method: "llm", Confidence: 0.95,
				})

				return idHR
			},
			assertions: func(t *testing.T, resp handlers.EntityDossierResponse, _ string) {
				t.Helper()
				count := 0
				var keptLink handlers.CrossDomainLink
				for _, link := range resp.CrossDomainLinks {
					if link.TargetEntityID == 2 { // idProduct is second entity created
						count++
						keptLink = link
					}
				}

				if count != 1 {
					t.Errorf("expected exactly 1 cross_domain_link for target, got %d", count)
				}
				if keptLink.Confidence < 0.95 {
					t.Errorf("expected higher-confidence link to be kept, got confidence=%.2f", keptLink.Confidence)
				}

				hasRelatedTo := false
				hasEquals := false
				for _, rt := range keptLink.RelationTypes {
					if rt == "related_to" {
						hasRelatedTo = true
					}
					if rt == "equals" {
						hasEquals = true
					}
				}
				if !hasRelatedTo {
					t.Error("relation_types missing 'related_to'")
				}
				if !hasEquals {
					t.Error("relation_types missing 'equals'")
				}
			},
		},
		{
			name:      "TargetNameMatchesID",
			withGraph: false,
			setup: func(ctx context.Context, db *sql.DB) int {
				entityDAO := dao.NewEntityDAO(db)
				linkDAO := dao.NewEntityLinkDAO(db)

				idHR, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
				_, _ = entityDAO.Create(ctx, dao.Entity{Type: "POLICY", Name: "Hiring Policy", Domain: "product"})
				_, _ = entityDAO.Create(ctx, dao.Entity{Type: "POLICY", Name: "Firing Policy", Domain: "product"})

				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: 2, RelationType: "related_to", Method: "rule", Confidence: 0.95,
				})
				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: 3, RelationType: "related_to", Method: "rule", Confidence: 0.95,
				})

				return idHR
			},
			assertions: func(t *testing.T, resp handlers.EntityDossierResponse, _ string) {
				t.Helper()
				expectedNames := map[int]string{2: "Hiring Policy", 3: "Firing Policy"}
				for _, link := range resp.CrossDomainLinks {
					wantName, ok := expectedNames[link.TargetEntityID]
					if !ok {
						continue
					}
					if link.TargetName != wantName {
						t.Errorf("target_name mismatch for entity %d: got %q, want %q",
							link.TargetEntityID, link.TargetName, wantName)
					}
				}
			},
		},
		{
			name:      "ProvenancePresent",
			withGraph: true,
			setup: func(ctx context.Context, db *sql.DB) int {
				entityDAO := dao.NewEntityDAO(db)
				linkDAO := dao.NewEntityLinkDAO(db)

				idHR, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
				_, _ = entityDAO.Create(ctx, dao.Entity{Type: "POLICY", Name: "Hiring Policy", Domain: "product"})

				evidence := "name match with confidence 0.95"
				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: 2, RelationType: "equals", Method: "rule", Confidence: 0.95, Evidence: &evidence,
				})

				return idHR
			},
			assertions: func(t *testing.T, resp handlers.EntityDossierResponse, _ string) {
				t.Helper()
				for _, link := range resp.CrossDomainLinks {
					if link.TargetEntityID == 2 {
						if link.Method == "" {
							t.Error("expected method to be set for cross-domain link")
						}
						if link.Confidence == 0 {
							t.Error("expected confidence to be set for cross-domain link")
						}
						return
					}
				}
				t.Error("expected cross-domain link for product entity not found")
			},
		},
		{
			name:      "BFSOnlyIncidentEdges",
			withGraph: true,
			setup: func(ctx context.Context, db *sql.DB) int {
				entityDAO := dao.NewEntityDAO(db)
				linkDAO := dao.NewEntityLinkDAO(db)

				idHR, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
				_, _ = entityDAO.Create(ctx, dao.Entity{Type: "POLICY", Name: "Policy A", Domain: "product"})
				_, _ = entityDAO.Create(ctx, dao.Entity{Type: "POLICY", Name: "Policy B", Domain: "product"})

				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: 2, RelationType: "related_to", Method: "rule", Confidence: 0.95,
				})
				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: 2, TargetEntityID: 3, RelationType: "related_to", Method: "rule", Confidence: 0.85,
				})

				return idHR
			},
			assertions: func(t *testing.T, resp handlers.EntityDossierResponse, _ string) {
				t.Helper()
				for _, link := range resp.CrossDomainLinks {
					if link.TargetEntityID == 3 {
						t.Errorf("unexpected cross_domain_link to non-incident entity %d (%s)",
							link.TargetEntityID, link.TargetName)
					}
				}

				found := false
				for _, link := range resp.CrossDomainLinks {
					if link.TargetEntityID == 2 {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected cross-domain link to Policy A (id=2)")
				}
			},
		},
		{
			name:      "IncomingLink",
			withGraph: false,
			setup: func(ctx context.Context, db *sql.DB) int {
				entityDAO := dao.NewEntityDAO(db)
				linkDAO := dao.NewEntityLinkDAO(db)

				_, _ = entityDAO.Create(ctx, dao.Entity{Type: "POLICY", Name: "Hiring Policy", Domain: "product"})
				idHR, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})

				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: 1, TargetEntityID: idHR, RelationType: "related_to", Method: "rule", Confidence: 0.95,
				})

				return idHR
			},
			assertions: func(t *testing.T, resp handlers.EntityDossierResponse, _ string) {
				t.Helper()
				found := false
				for _, link := range resp.CrossDomainLinks {
					if link.TargetEntityID == 1 && link.TargetName == "Hiring Policy" {
						found = true
						break
					}
				}
				if !found {
					t.Error("expected incoming cross-domain link to Hiring Policy")
				}
			},
		},
		{
			name:      "NoDuplicatesAcrossSections",
			withGraph: true,
			setup: func(ctx context.Context, db *sql.DB) int {
				entityDAO := dao.NewEntityDAO(db)
				linkDAO := dao.NewEntityLinkDAO(db)

				idHR, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
				_, _ = entityDAO.Create(ctx, dao.Entity{Type: "POLICY", Name: "Hiring Policy", Domain: "product"})

				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: 2, RelationType: "related_to", Method: "rule", Confidence: 0.95,
				})

				return idHR
			},
			assertions: func(t *testing.T, resp handlers.EntityDossierResponse, _ string) {
				t.Helper()
				count := 0
				for _, link := range resp.CrossDomainLinks {
					if link.TargetEntityID == 2 {
						count++
					}
				}
				if count != 1 {
					t.Errorf("expected exactly 1 cross_domain_link for target across both sections, got %d", count)
				}
			},
		},
		{
			name:      "RelationTypesMerging",
			withGraph: false,
			setup: func(ctx context.Context, db *sql.DB) int {
				entityDAO := dao.NewEntityDAO(db)
				linkDAO := dao.NewEntityLinkDAO(db)

				idHR, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
				_, _ = entityDAO.Create(ctx, dao.Entity{Type: "POLICY", Name: "Hiring Policy", Domain: "product"})
				_, _ = entityDAO.Create(ctx, dao.Entity{Type: "SERVER", Name: "Server A", Domain: "it"})

				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: 2, RelationType: "related_to", Method: "rule", Confidence: 0.95,
				})
				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: 2, RelationType: "equals", Method: "llm", Confidence: 0.85,
				})
				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: 2, RelationType: "related_to", Method: "equals", Confidence: 0.6,
				})
				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: 3, RelationType: "manages", Method: "rule", Confidence: 0.9,
				})

				return idHR
			},
			assertions: func(t *testing.T, resp handlers.EntityDossierResponse, jsonText string) {
				t.Helper()
				var productLink, itLink *handlers.CrossDomainLink
				for i := range resp.CrossDomainLinks {
					switch resp.CrossDomainLinks[i].TargetEntityID {
					case 2:
						productLink = &resp.CrossDomainLinks[i]
					case 3:
						itLink = &resp.CrossDomainLinks[i]
					}
				}

				if productLink == nil {
					t.Fatal("expected cross-domain link to product entity")
				}
				if itLink == nil {
					t.Fatal("expected cross-domain link to IT entity")
				}

				hasRelatedTo := false
				hasEquals := false
				for _, rt := range productLink.RelationTypes {
					if rt == "related_to" {
						hasRelatedTo = true
					}
					if rt == "equals" {
						hasEquals = true
					}
				}
				if !hasRelatedTo {
					t.Error("product: relation_types missing 'related_to'")
				}
				if !hasEquals {
					t.Error("product: relation_types missing 'equals'")
				}

				if productLink.Confidence != 0.95 {
					t.Errorf("product: expected confidence 0.95, got %.2f", productLink.Confidence)
				}

				if len(itLink.RelationTypes) != 1 || itLink.RelationTypes[0] != "manages" {
					t.Errorf("IT: expected relation_types [\"manages\"], got %v", itLink.RelationTypes)
				}

				if !contains(jsonText, `"relation_types"`) {
					t.Error("JSON response should contain 'relation_types' field")
				}
				if contains(jsonText, `"relation_type":`) {
					t.Error("JSON response should NOT contain old 'relation_type' field (singular)")
				}
			},
		},
		{
			name:      "RelationTypesFromBFS",
			withGraph: true,
			setup: func(ctx context.Context, db *sql.DB) int {
				entityDAO := dao.NewEntityDAO(db)
				linkDAO := dao.NewEntityLinkDAO(db)

				idHR, _ := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
				_, _ = entityDAO.Create(ctx, dao.Entity{Type: "POLICY", Name: "Hiring Policy", Domain: "product"})

				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: 2, RelationType: "related_to", Method: "rule", Confidence: 0.95,
				})
				linkDAO.Create(ctx, dao.EntityLink{ //nolint:errcheck
					SubjectEntityID: idHR, TargetEntityID: 2, RelationType: "equals", Method: "llm", Confidence: 0.85,
				})

				return idHR
			},
			assertions: func(t *testing.T, resp handlers.EntityDossierResponse, _ string) {
				t.Helper()
				count := 0
				var keptLink handlers.CrossDomainLink
				for _, link := range resp.CrossDomainLinks {
					if link.TargetEntityID == 2 {
						count++
						keptLink = link
					}
				}

				if count != 1 {
					t.Errorf("expected exactly 1 cross_domain_link for target, got %d", count)
				}

				hasRelatedTo := false
				hasEquals := false
				for _, rt := range keptLink.RelationTypes {
					if rt == "related_to" {
						hasRelatedTo = true
					}
					if rt == "equals" {
						hasEquals = true
					}
				}
				if !hasRelatedTo {
					t.Error("BFS: relation_types missing 'related_to'")
				}
				if !hasEquals {
					t.Error("BFS: relation_types missing 'equals'")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
			if err != nil {
				t.Fatalf("create test db: %v", err)
			}
			t.Cleanup(cleanFn)

			ctx := context.Background()
			sourceID := tt.setup(ctx, db)

			var g *graph.Graph
			if tt.withGraph {
				g, _, err = graph.NewGraphFromDB(ctx, db)
				if err != nil {
					t.Fatalf("NewGraphFromDB() error = %v", err)
				}
			}

			req := buildRequest("get_entity_dossier", map[string]interface{}{"entity_id": strconv.Itoa(sourceID)})
			result, err := handlers.HandleGetEntityDossier(ctx, req, db, g)
			if err != nil {
				t.Fatalf("HandleGetEntityDossier() error = %v", err)
			}

			var resp handlers.EntityDossierResponse
			if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}

			jsonText := result.Content[0].(mcp.TextContent).Text
			tt.assertions(t, resp, jsonText)
		})
	}
}
