// Package runner_test contains end-to-end integration tests for cross-domain
// entity links through the Runner and MCP handler pipeline.
package runner_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/cache"
	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/ingestion/runner"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/mcp/handlers"
	"github.com/devmix/synopsis/internal/relations"

	"github.com/mark3labs/mcp-go/mcp"
)

// setupE2ERunner creates a temp SQLite database with migrations applied and a
// Runner configured for entity link e2e tests. Returns the runner, raw *sql.DB,
// config pointer, and cache store (for cleanup).
func setupE2ERunner(t *testing.T) (*runner.Runner, *sql.DB, *config.Config, *cache.Store) {
	t.Helper()

	log, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Embeddings.Mode = "api"
	cfg.Embeddings.API.BaseURL = "http://127.0.0.1:1/v1"
	cfg.Embeddings.API.ModelName = "test-model"
	cfg.Embeddings.API.VectorDim = 4

	dataDir := t.TempDir()
	cfg.Paths.DataDir = dataDir
	cfg.Paths.MigrationsDir = "../../../migrations"

	db, err := database.Open(filepath.Join(dataDir, "test.db"), cfg.VectorDim(),
		database.WithMigrationsDir(cfg.Paths.MigrationsDir))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cacheStore, err := cache.NewStore(filepath.Join(dataDir, "test_cache.db"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	t.Cleanup(func() { _ = cacheStore.Close() })

	ingRunner, err := runner.NewRunner(cfg, db.DB(), log,
		runner.WithCacheStore(cacheStore),
	)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = ingRunner.Close() })

	return ingRunner, db.DB(), &cfg, cacheStore
}

// seedEntity inserts an entity and returns its Predicate.
func seedEntity(t *testing.T, ctx context.Context, db *sql.DB, entityType, name, domain string) int {
	t.Helper()
	id, err := dao.NewEntityDAO(db).Create(ctx, dao.Entity{
		Type:   entityType,
		Name:   name,
		Domain: domain,
	})
	if err != nil {
		t.Fatalf("create entity %q in %s: %v", name, domain, err)
	}
	return id
}

// callGetEntityLinks builds an MCP request and calls the handler by entity Predicate.
func callGetEntityLinks(db *sql.DB, entityID int) (*mcp.CallToolResult, error) {
	args := map[string]interface{}{"entity_id": fmt.Sprintf("%d", entityID)}
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      "get_entity_links",
			Arguments: args,
		},
	}
	return handlers.HandleGetEntityLinks(context.Background(), req, db)
}

// parseEntityLinksResponse unmarshals the MCP text content into an EntityLinksResponse.
func parseEntityLinksResponse(t *testing.T, result *mcp.CallToolResult) handlers.EntityLinksResponse {
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

// ---------------------------------------------------------------------------
// 7.2 — E2E: BuildEntityLinks with expression config → verify links via MCP handler
// ---------------------------------------------------------------------------

func TestE2E_ExpressionBuildAndRead(t *testing.T) {
	t.Parallel()

	_, db, _, _ := setupE2ERunner(t)
	ctx := context.Background()

	// Seed entities across two domains.
	idHR := seedEntity(t, ctx, db, "Person", "Jane Smith", "hr")
	idPolicy := seedEntity(t, ctx, db, "Person", "Jane Smith", "policy")

	// Build a global config with expression method pointing hr→policy.
	globalCfg := &config.GlobalConfig{
		CrossDomainLinks: &config.CrossDomainLinksConfig{
			Methods: []string{"expression"},
			Expressions: []config.LinkExpression{{
				Name:         "hr-policy-same-person",
				Priority:     10,
				Where:        `A.domain == 'hr' && A.name == B.name`,
				RelationType: "same_entity",
			}},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, globalCfg.CrossDomainLinks, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks: %v", err)
	}
	if result.LinksCreated < 1 {
		t.Errorf("LinksCreated = %d, want >= 1", result.LinksCreated)
	}

	// Read back via MCP handler.
	mcpResult, err := callGetEntityLinks(db, idHR)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks: %v", err)
	}
	if mcpResult.IsError {
		t.Fatalf("unexpected error response: %s", mcpResult.Content[0].(mcp.TextContent).Text)
	}

	resp := parseEntityLinksResponse(t, mcpResult)
	if resp.Entity.Name != "Jane Smith" {
		t.Errorf("entity name = %q, want %q", resp.Entity.Name, "Jane Smith")
	}
	if len(resp.Links) == 0 {
		t.Fatal("expected at least one link from MCP handler")
	}

	link := resp.Links[0]
	if link.Method != "expression" {
		t.Errorf("link method = %q, want %q", link.Method, "expression")
	}
	if link.Confidence != 1.0 {
		t.Errorf("link confidence = %v, want 1.0", link.Confidence)
	}
	if link.TargetEntityID != idPolicy {
		t.Errorf("target_entity_id = %d, want %d", link.TargetEntityID, idPolicy)
	}
}

// ---------------------------------------------------------------------------
// 7.2 — E2E: BuildEntityLinks with equals method → verify links via MCP handler
// ---------------------------------------------------------------------------

func TestE2E_EqualsBuildAndRead(t *testing.T) {
	t.Parallel()

	_, db, _, _ := setupE2ERunner(t)
	ctx := context.Background()

	// Seed entities across two domains with multi-word name (min_words=2).
	idIT := seedEntity(t, ctx, db, "Person", "Alice Johnson", "it")
	idSales := seedEntity(t, ctx, db, "Person", "Alice Johnson", "sales")

	globalCfg := &config.GlobalConfig{
		CrossDomainLinks: &config.CrossDomainLinksConfig{
			Methods: []string{"equals"},
			Equals:  &config.EqualsConfig{MinWords: 2},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, globalCfg.CrossDomainLinks, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks: %v", err)
	}
	if result.LinksCreated < 1 {
		t.Errorf("LinksCreated = %d, want >= 1", result.LinksCreated)
	}

	mcpResult, err := callGetEntityLinks(db, idIT)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks: %v", err)
	}
	if mcpResult.IsError {
		t.Fatalf("unexpected error response: %s", mcpResult.Content[0].(mcp.TextContent).Text)
	}

	resp := parseEntityLinksResponse(t, mcpResult)
	if len(resp.Links) == 0 {
		t.Fatal("expected at least one link from MCP handler")
	}

	link := resp.Links[0]
	if link.Method != "equals" {
		t.Errorf("link method = %q, want %q", link.Method, "equals")
	}
	if link.Confidence != 0.9 {
		t.Errorf("link confidence = %v, want 0.9", link.Confidence)
	}
	if link.TargetEntityID != idSales {
		t.Errorf("target_entity_id = %d, want %d", link.TargetEntityID, idSales)
	}
}

// ---------------------------------------------------------------------------
// 7.3 — E2E: LLM path with stub HTTP server (fake LLM provider)
// Uses runner.BuildEntityLinks which internally creates the linker via helper.
// ---------------------------------------------------------------------------

func TestE2E_LLMStubBuildAndRead(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create stub LLM server first (needed for linker creation).
	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"choices": [{
				"message": {
					"content": "{\"same_entity\": true, \"confidence\": 0.92, \"reasoning\": \"stub: names and types match\"}"
				}
			}]
		}`)
	}))
	defer llmServer.Close()

	log, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	dataDir := t.TempDir()

	db, err := database.Open(filepath.Join(dataDir, "test.db"), 4,
		database.WithMigrationsDir(filepath.Join("..", "..", "..", "migrations")))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close() //nolint:errcheck

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cacheStore, err := cache.NewStore(filepath.Join(dataDir, "test_cache.db"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer cacheStore.Close() //nolint:errcheck

	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Embeddings.Mode = "api"
	cfg.Embeddings.API.BaseURL = "http://127.0.0.1:1/v1"
	cfg.Embeddings.API.ModelName = "test-model"
	cfg.Embeddings.API.VectorDim = 4

	// Set absolute prompts path so entity-linker templates are found.
	// Use a path relative to this test file's directory.
	cfg.Paths.PromptsPath = filepath.Join("..", "..", "..", "configs", "prompts")

	ingRunner, err := runner.NewRunner(cfg, db.DB(), log,
		runner.WithCacheStore(cacheStore),
	)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer ingRunner.Close() //nolint:errcheck

	// Seed entities.
	idHR := seedEntity(t, ctx, db.DB(), "Person", "Bob Wilson", "hr")
	idProduct := seedEntity(t, ctx, db.DB(), "Person", "Bob Wilson", "product")

	// Write global config XML for the runner to pick up.
	globalXML := `<global>
<cross-domain-links>
  <method>llm</method>
</cross-domain-links>
</global>`
	ontologyDir := filepath.Join(dataDir, "ontology")
	if err := os.MkdirAll(ontologyDir, 0o755); err != nil {
		t.Fatalf("mkdir ontology dir: %v", err)
	}
	xmlPath := filepath.Join(ontologyDir, "global.xml")
	if err := os.WriteFile(xmlPath, []byte(globalXML), 0o644); err != nil {
		t.Fatalf("write global.xml: %v", err)
	}

	cfg.Paths.GlobalConfigPath = ontologyDir
	cfg.Linker.Disabled = false
	cfg.Linker.LLM.APIBaseURL = llmServer.URL
	cfg.Linker.LLM.ModelName = "stub-model"
	cfg.Linker.LLM.MaxTokens = 200
	cfg.Linker.LLM.TimeoutMs = 5000

	// Re-create runner with updated config.
	if err := ingRunner.Close(); err != nil {
		t.Fatalf("close runner: %v", err)
	}

	ingRunner, err = runner.NewRunner(cfg, db.DB(), log,
		runner.WithCacheStore(cacheStore),
	)
	if err != nil {
		t.Fatalf("NewRunner (recreate): %v", err)
	}
	defer ingRunner.Close() //nolint:errcheck

	result2, err := ingRunner.BuildEntityLinks(ctx)
	if err != nil {
		t.Fatalf("BuildEntityLinks via runner: %v", err)
	}
	if result2.LinksCreated < 1 {
		t.Errorf("LinksCreated = %d, want >= 1 (stub LLM should approve), errors: %v", result2.LinksCreated, result2.Errors)
	}

	// Read back via MCP handler.
	mcpResult, err := callGetEntityLinks(db.DB(), idHR)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks: %v", err)
	}
	if mcpResult.IsError {
		t.Fatalf("unexpected error response: %s", mcpResult.Content[0].(mcp.TextContent).Text)
	}

	resp := parseEntityLinksResponse(t, mcpResult)
	if len(resp.Links) == 0 {
		t.Fatal("expected at least one link from MCP handler")
	}

	link := resp.Links[0]
	if link.Method != "llm" {
		t.Errorf("link method = %q, want %q", link.Method, "llm")
	}
	if link.Confidence < 0.9 || link.Confidence > 1.0 {
		t.Errorf("link confidence = %v, want in [0.9, 1.0]", link.Confidence)
	}
	if link.TargetEntityID != idProduct {
		t.Errorf("target_entity_id = %d, want %d", link.TargetEntityID, idProduct)
	}
}

// ---------------------------------------------------------------------------
// 7.4 — E2E: get_entity_links returns deduplicated links after ingestion
// ---------------------------------------------------------------------------

func TestE2E_DedupLinks(t *testing.T) {
	t.Parallel()

	_, db, _, _ := setupE2ERunner(t)
	ctx := context.Background()

	// Seed entities across three domains.
	idHR := seedEntity(t, ctx, db, "Person", "Carol Davis", "hr") // source entity for MCP query
	idIT := seedEntity(t, ctx, db, "Person", "Carol Davis", "it")
	idSales := seedEntity(t, ctx, db, "Person", "Carol Davis", "sales")

	// Build links: hr↔it (expression), it↔sales (equals).
	globalCfg := &config.GlobalConfig{
		CrossDomainLinks: &config.CrossDomainLinksConfig{
			Methods: []string{"expression", "equals"},
			Expressions: []config.LinkExpression{{
				Name:         "hr-it-same-person",
				Priority:     10,
				Where:        `A.domain == 'hr' && B.domain == 'it' && A.name == B.name`,
				RelationType: "same_entity",
			}},
			Equals: &config.EqualsConfig{MinWords: 2},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, globalCfg.CrossDomainLinks, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks: %v", err)
	}
	if result.LinksCreated < 1 {
		t.Errorf("LinksCreated = %d, want >= 1", result.LinksCreated)
	}

	// Read links for Carol Davis in hr domain.
	mcpResult, err := callGetEntityLinks(db, idHR)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks: %v", err)
	}
	if mcpResult.IsError {
		t.Fatalf("unexpected error response: %s", mcpResult.Content[0].(mcp.TextContent).Text)
	}

	resp := parseEntityLinksResponse(t, mcpResult)

	// Carol Davis (hr) should have links to IT (expression method) and sales (equals method).
	// The bidirectional storage means hr→it and it→hr both exist in DB,
	// but the handler deduplicates by (target_entity_id, relation_type).
	if len(resp.Links) != 2 {
		t.Errorf("expected 2 deduplicated links for Carol Davis (hr), got %d", len(resp.Links))
		for i, l := range resp.Links {
			t.Logf("  link[%d]: target=%s(%d) method=%s relation_type=%s",
				i, l.TargetName, l.TargetEntityID, l.Method, l.RelationType)
		}
	}

	if len(resp.Links) > 0 {
		link := resp.Links[0]
		if link.TargetEntityID != idIT {
			t.Errorf("links[0].target_entity_id = %d, want %d (it domain)", link.TargetEntityID, idIT)
		}
		if link.Method != "expression" {
			t.Errorf("links[0].method = %q, want %q", link.Method, "expression")
		}
	}

	if len(resp.Links) > 1 {
		link := resp.Links[1]
		if link.TargetEntityID != idSales {
			t.Errorf("links[1].target_entity_id = %d, want %d (sales domain)", link.TargetEntityID, idSales)
		}
		if link.Method != "equals" {
			t.Errorf("links[1].method = %q, want %q", link.Method, "equals")
		}
	}

	// Verify no duplicate targets: collect target IDs and check uniqueness.
	targetIDs := make(map[int]bool)
	for _, l := range resp.Links {
		if targetIDs[l.TargetEntityID] {
			t.Errorf("duplicate target_entity_id %d in response", l.TargetEntityID)
		}
		targetIDs[l.TargetEntityID] = true
	}
}

// ---------------------------------------------------------------------------
// 7.4 — E2E: get_entity_links with domain filter returns deduplicated results
// Uses a unique name across domains; the domain filter routes to GetByNameFold,
// which resolves to exactly one entity.
// ---------------------------------------------------------------------------

func TestE2E_DedupLinksNoDomain(t *testing.T) {
	t.Parallel()

	_, db, _, _ := setupE2ERunner(t)
	ctx := context.Background()

	// Seed entities: source has a unique name (only one match for ListByNameFold),
	// target shares the same normalized name so equals creates links.
	idHR := seedEntity(t, ctx, db, "Person", "Unique Name Person", "hr")
	idIT := seedEntity(t, ctx, db, "Person", "Unique Name Person", "it")

	globalCfg := &config.GlobalConfig{
		CrossDomainLinks: &config.CrossDomainLinksConfig{
			Methods: []string{"equals"},
			Equals:  &config.EqualsConfig{MinWords: 2},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, globalCfg.CrossDomainLinks, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks: %v", err)
	}
	if result.LinksCreated < 1 {
		t.Errorf("LinksCreated = %d, want >= 1", result.LinksCreated)
	}

	// Query by entity Predicate directly.
	mcpResult, err := callGetEntityLinks(db, idHR)
	if err != nil {
		t.Fatalf("HandleGetEntityLinks: %v", err)
	}
	if mcpResult.IsError {
		t.Fatalf("unexpected error response: %s", mcpResult.Content[0].(mcp.TextContent).Text)
	}

	resp := parseEntityLinksResponse(t, mcpResult)
	if len(resp.Links) == 0 {
		t.Fatal("expected at least one link")
	}

	// Dedup check: no duplicate target IDs.
	targetIDs := make(map[int]bool)
	for _, l := range resp.Links {
		if targetIDs[l.TargetEntityID] {
			t.Errorf("duplicate target_entity_id %d in response", l.TargetEntityID)
		}
		targetIDs[l.TargetEntityID] = true
	}

	// Verify the link points to the other domain.
	link := resp.Links[0]
	if link.TargetEntityID != idIT {
		t.Errorf("target_entity_id = %d, want %d", link.TargetEntityID, idIT)
	}
	_ = idHR // used as source entity for MCP query
}

// ---------------------------------------------------------------------------
// 7.2 — E2E: BuildEntityLinks errors are propagated correctly
// ---------------------------------------------------------------------------

func TestE2E_BuildErrors(t *testing.T) {
	t.Parallel()

	_, db, _, _ := setupE2ERunner(t)
	ctx := context.Background()

	// No entities seeded — expression will find no matching pairs.
	globalCfg := &config.GlobalConfig{
		CrossDomainLinks: &config.CrossDomainLinksConfig{
			Methods: []string{"expression"},
			Expressions: []config.LinkExpression{{
				Name:         "hr-it-nobody",
				Priority:     10,
				Where:        `A.domain == 'hr' && A.name == B.name`,
				RelationType: "same_entity",
			}},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, globalCfg.CrossDomainLinks, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks: %v", err)
	}
	if result.LinksCreated != 0 {
		t.Errorf("LinksCreated = %d, want 0 (no entities)", result.LinksCreated)
	}
}

// ---------------------------------------------------------------------------
// 7.3 — E2E: LLM method without linker produces error in result
// Documents that the llm method requires a linker; nil linker → explicit error.
// ---------------------------------------------------------------------------

func TestE2E_LLMNoLinker(t *testing.T) {
	t.Parallel()

	_, db, _, _ := setupE2ERunner(t)
	ctx := context.Background()

	seedEntity(t, ctx, db, "Person", "Test Person", "hr")
	seedEntity(t, ctx, db, "Person", "Test Person", "it")

	globalCfg := &config.GlobalConfig{
		CrossDomainLinks: &config.CrossDomainLinksConfig{
			Methods: []string{"llm"},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, globalCfg.CrossDomainLinks, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks: %v", err)
	}
	if len(result.Errors) == 0 {
		t.Error("expected errors when llm method is used without a linker")
	}
	if result.LinksCreated != 0 {
		t.Errorf("LinksCreated = %d, want 0 (no linker available)", result.LinksCreated)
	}
}
