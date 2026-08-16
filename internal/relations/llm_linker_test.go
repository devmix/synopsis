package relations_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devmix/synopsis/internal/cache"
	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/prompts"
	"github.com/devmix/synopsis/internal/relations"
	"github.com/devmix/synopsis/internal/utils"
)

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	l, err := logger.New(logger.Options{Level: "debug"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}
	t.Cleanup(func() { _ = l.Sync() })
	return l
}

func testDB(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	dsn := dbPath + "?_journal_mode=WAL"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}

	schema := `
		CREATE TABLE chunks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			doc_id INTEGER NOT NULL,
			chunk_text TEXT NOT NULL,
			sequence_num INTEGER NOT NULL
		);
		CREATE TABLE chunk_entities (
			chunk_id INTEGER NOT NULL,
			entity_id INTEGER NOT NULL,
			PRIMARY KEY (chunk_id, entity_id)
		);`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}

	cleanup := func() {
		db.Close()                 //nolint:errcheck
		os.Remove(dbPath)          //nolint:errcheck
		os.Remove(dbPath + "-wal") //nolint:errcheck
		os.Remove(dbPath + "-shm") //nolint:errcheck
	}

	return db, cleanup
}

func testCacheStore(t *testing.T) (*cache.Store, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_cache.db")
	store, err := cache.NewStore(dbPath)
	if err != nil {
		t.Fatalf("create cache store: %v", err)
	}

	cleanup := func() {
		_ = store.Close()
		os.Remove(dbPath)          //nolint:errcheck
		os.Remove(dbPath + "-wal") //nolint:errcheck
		os.Remove(dbPath + "-shm") //nolint:errcheck
	}

	return store, cleanup
}

func llmMockServer(t *testing.T, response string) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": response}},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})
	return httptest.NewServer(handler)
}

func defaultCrossDomainConfig() *config.CrossDomainLinksConfig {
	cfg := &config.CrossDomainLinksConfig{
		LLmConfidenceThreshold: 0.7,
		BatchSize:              5,
	}
	cfg.ApplyDefaults()
	return cfg
}

// testPromptLoader creates a loader that uses embedded defaults by pointing to the prompts directory.
func testPromptLoader(t *testing.T) *prompts.PromptLoader {
	t.Helper()
	loader, err := prompts.NewLoader("../../configs/prompts", nil)
	if err != nil {
		t.Fatalf("create prompt loader: %v", err)
	}
	return loader
}

func TestName(t *testing.T) {
	t.Parallel()

	server := llmMockServer(t, `{"same_entity":true,"confidence":0.9}`)
	defer server.Close()

	db, dbCleanup := testDB(t)
	defer dbCleanup()
	cacheStore, cacheCleanup := testCacheStore(t)
	defer cacheCleanup()

	cfg := defaultCrossDomainConfig()
	loader := testPromptLoader(t)

	linker, err := relations.NewLLMCrossDomainLinker(
		cfg,
		config.LinkerConfig{
			LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"},
		},
		db, cacheStore, testLogger(t), loader,
	)
	if err != nil {
		t.Fatalf("NewLLMCrossDomainLinker: %v", err)
	}

	if linker.Name() != "llm" {
		t.Errorf("Name() = %q, want %q", linker.Name(), "llm")
	}
}

func TestConstructor_Validation(t *testing.T) {
	t.Parallel()

	server := llmMockServer(t, `{"same_entity":true,"confidence":0.9}`)
	defer server.Close()

	db, dbCleanup := testDB(t)
	defer dbCleanup()
	cacheStore, cacheCleanup := testCacheStore(t)
	defer cacheCleanup()

	loader := testPromptLoader(t)

	tests := []struct {
		name       string
		cdlCfg     *config.CrossDomainLinksConfig
		linkerCfg  config.LinkerConfig
		db         *sql.DB
		cacheStore *cache.Store
		loader     *prompts.PromptLoader
		wantErrSub string
	}{
		{
			name:       "nil cross-domain config",
			cdlCfg:     nil,
			linkerCfg:  config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
			db:         db,
			cacheStore: cacheStore,
			loader:     loader,
			wantErrSub: "cross-domain-links config is required",
		},
		{
			name:       "empty base URL",
			cdlCfg:     defaultCrossDomainConfig(),
			linkerCfg:  config.LinkerConfig{},
			db:         db,
			cacheStore: cacheStore,
			loader:     loader,
			wantErrSub: "api_base_url is required",
		},
		{
			name:       "empty model name",
			cdlCfg:     defaultCrossDomainConfig(),
			linkerCfg:  config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL}},
			db:         db,
			cacheStore: cacheStore,
			loader:     loader,
			wantErrSub: "model_name is required",
		},
		{
			name:       "nil database",
			cdlCfg:     defaultCrossDomainConfig(),
			linkerCfg:  config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
			db:         nil,
			cacheStore: cacheStore,
			loader:     loader,
			wantErrSub: "database connection is required",
		},
		{
			name:       "nil cache store",
			cdlCfg:     defaultCrossDomainConfig(),
			linkerCfg:  config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
			db:         db,
			cacheStore: nil,
			loader:     loader,
			wantErrSub: "cache store is required",
		},
		{
			name:       "nil prompt loader",
			cdlCfg:     defaultCrossDomainConfig(),
			linkerCfg:  config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
			db:         db,
			cacheStore: cacheStore,
			loader:     nil,
			wantErrSub: "prompt loader is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := relations.NewLLMCrossDomainLinker(
				tt.cdlCfg, tt.linkerCfg, tt.db, tt.cacheStore, testLogger(t), tt.loader,
			)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantErrSub)
			}
			if !strings.Contains(err.Error(), tt.wantErrSub) {
				t.Errorf("error = %v, want substring %q", err, tt.wantErrSub)
			}
		})
	}
}

func TestCacheHit(t *testing.T) {
	t.Parallel()

	server := llmMockServer(t, `{"same_entity":true,"confidence":0.95}`)
	defer server.Close()

	db, dbCleanup := testDB(t)
	defer dbCleanup()
	cacheStore, cacheCleanup := testCacheStore(t)
	defer cacheCleanup()

	cfg := defaultCrossDomainConfig()
	loader := testPromptLoader(t)

	linker, err := relations.NewLLMCrossDomainLinker(
		cfg,
		config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
		db, cacheStore, testLogger(t), loader,
	)
	if err != nil {
		t.Fatalf("NewLLMCrossDomainLinker: %v", err)
	}

	ctx := context.Background()

	a := relations.EntityCandidate{ID: 1, Name: "API Gateway", Type: "SERVICE", Domain: "it"}
	b := relations.EntityCandidate{ID: 2, Name: "API Gateway", Type: "FEATURE", Domain: "product"}

	// First call should hit the LLM.
	decision1, err := linker.LinkPair(ctx, a, b)
	if err != nil {
		t.Fatalf("LinkPair (first): %v", err)
	}
	if !decision1.SameEntity || decision1.Confidence < 0.9 {
		t.Errorf("first call: %+v", decision1)
	}

	// Second call should hit the cache (no additional LLM calls).
	decision2, err := linker.LinkPair(ctx, a, b)
	if err != nil {
		t.Fatalf("LinkPair (cached): %v", err)
	}
	if !decision2.SameEntity || decision2.Confidence < 0.9 {
		t.Errorf("cached call: %+v", decision2)
	}

	// Verify cache contains the entry.
	key := buildExpectedCacheKey(a, b, loader)
	raw, hit := cacheStore.Get(ctx, "llm_entity_link_cache", key)
	if !hit {
		t.Fatal("expected cache to contain result")
	}
	var cached relations.LinkDecision
	if err := json.Unmarshal([]byte(raw), &cached); err != nil {
		t.Fatalf("unmarshal cached value: %v", err)
	}
	if !cached.SameEntity {
		t.Error("cached decision should have SameEntity=true")
	}
}

func TestCacheMiss(t *testing.T) {
	t.Parallel()

	server := llmMockServer(t, `{"same_entity":false,"confidence":0.3}`)
	defer server.Close()

	db, dbCleanup := testDB(t)
	defer dbCleanup()
	cacheStore, cacheCleanup := testCacheStore(t)
	defer cacheCleanup()

	cfg := defaultCrossDomainConfig()
	loader := testPromptLoader(t)

	linker, err := relations.NewLLMCrossDomainLinker(
		cfg,
		config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
		db, cacheStore, testLogger(t), loader,
	)
	if err != nil {
		t.Fatalf("NewLLMCrossDomainLinker: %v", err)
	}

	ctx := context.Background()

	a := relations.EntityCandidate{ID: 10, Name: "Dragon", Type: "CREATURE", Domain: "fantasy"}
	b := relations.EntityCandidate{ID: 20, Name: "Car", Type: "VEHICLE", Domain: "transport"}

	decision, err := linker.LinkPair(ctx, a, b)
	if err != nil {
		t.Fatalf("LinkPair: %v", err)
	}

	// LLM returned same_entity=false directly; LinkPair returns raw decision.
	if decision.SameEntity {
		t.Error("expected SameEntity=false from LLM response")
	}
}

func TestCacheKeyDeterministic(t *testing.T) {
	t.Parallel()

	server := llmMockServer(t, `{"same_entity":true,"confidence":0.9}`)
	defer server.Close()

	db, dbCleanup := testDB(t)
	defer dbCleanup()
	cacheStore, cacheCleanup := testCacheStore(t)
	defer cacheCleanup()

	cfg := defaultCrossDomainConfig()
	loader := testPromptLoader(t)

	linker, err := relations.NewLLMCrossDomainLinker(
		cfg,
		config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
		db, cacheStore, testLogger(t), loader,
	)
	if err != nil {
		t.Fatalf("NewLLMCrossDomainLinker: %v", err)
	}

	a := relations.EntityCandidate{ID: 1, Name: "X", Type: "T", Domain: "D"}
	b := relations.EntityCandidate{ID: 2, Name: "Y", Type: "U", Domain: "E"}

	ctx := context.Background()

	// Call twice; both should produce the same cache key.
	_, _ = linker.LinkPair(ctx, a, b)
	_, _ = linker.LinkPair(ctx, a, b)

	key := buildExpectedCacheKey(a, b, loader)
	raw1, hit1 := cacheStore.Get(ctx, "llm_entity_link_cache", key)
	if !hit1 {
		t.Fatal("expected cache to contain result after two identical calls")
	}

	var d relations.LinkDecision
	if err := json.Unmarshal([]byte(raw1), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
}

func TestCacheKeyOrdered(t *testing.T) {
	t.Parallel()

	server := llmMockServer(t, `{"same_entity":true,"confidence":0.9}`)
	defer server.Close()

	db, dbCleanup := testDB(t)
	defer dbCleanup()
	cacheStore, cacheCleanup := testCacheStore(t)
	defer cacheCleanup()

	cfg := defaultCrossDomainConfig()
	loader := testPromptLoader(t)

	linker, err := relations.NewLLMCrossDomainLinker(
		cfg,
		config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
		db, cacheStore, testLogger(t), loader,
	)
	if err != nil {
		t.Fatalf("NewLLMCrossDomainLinker: %v", err)
	}

	ctx := context.Background()

	a := relations.EntityCandidate{ID: 1, Name: "Alpha", Type: "T", Domain: "D"}
	b := relations.EntityCandidate{ID: 2, Name: "Beta", Type: "U", Domain: "E"}

	// Call with (a, b) — should call LLM and cache.
	_, err = linker.LinkPair(ctx, a, b)
	if err != nil {
		t.Fatalf("LinkPair(a,b): %v", err)
	}

	// Call with (b, a) — should hit cache because key is ordered by normalized name/domain.
	decision, err := linker.LinkPair(ctx, b, a)
	if err != nil {
		t.Fatalf("LinkPair(b,a): %v", err)
	}
	if !decision.SameEntity {
		t.Error("expected SameEntity=true from cache hit on reversed pair")
	}
}

func TestCacheKeyStableAcrossIDChange(t *testing.T) {
	t.Parallel()

	// The cache key is based on (type, normalized_name, normalized_domain), not entity IDs.
	// Changing IDs must produce the same cache key so that rebuilds don't invalidate cache.
	a1 := relations.EntityCandidate{ID: 1, Name: "API Gateway", Type: "SERVICE", Domain: "it"}
	b1 := relations.EntityCandidate{ID: 2, Name: "API Gateway", Type: "FEATURE", Domain: "product"}

	a2 := relations.EntityCandidate{ID: 99, Name: "API Gateway", Type: "SERVICE", Domain: "it"}
	b2 := relations.EntityCandidate{ID: 100, Name: "API Gateway", Type: "FEATURE", Domain: "product"}

	loader := testPromptLoader(t)
	key1 := buildExpectedCacheKey(a1, b1, loader)
	key2 := buildExpectedCacheKey(a2, b2, loader)

	if key1 != key2 {
		t.Errorf("cache keys differ despite same (type,name,domain): %q vs %q", key1, key2)
	}
}

func TestPromptFormat(t *testing.T) {
	t.Parallel()

	var capturedUserPrompt string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		if msgs, ok := body["messages"].([]interface{}); ok && len(msgs) >= 2 {
			if msg, ok := msgs[1].(map[string]interface{}); ok {
				if content, ok := msg["content"].([]interface{}); ok && len(content) > 0 {
					if block, ok := content[0].(map[string]interface{}); ok {
						capturedUserPrompt = block["text"].(string)
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"same_entity":true,"confidence":0.9}`}},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	db, dbCleanup := testDB(t)
	defer dbCleanup()
	cacheStore, cacheCleanup := testCacheStore(t)
	defer cacheCleanup()

	cfg := defaultCrossDomainConfig()
	loader := testPromptLoader(t)

	linker, err := relations.NewLLMCrossDomainLinker(
		cfg,
		config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
		db, cacheStore, testLogger(t), loader,
	)
	if err != nil {
		t.Fatalf("NewLLMCrossDomainLinker: %v", err)
	}

	ctx := context.Background()

	a := relations.EntityCandidate{ID: 1, Name: "API Gateway", Type: "SERVICE", Domain: "it"}
	b := relations.EntityCandidate{ID: 2, Name: "Load Balancer", Type: "FEATURE", Domain: "product"}

	_, _ = linker.LinkPair(ctx, a, b)

	if !strings.Contains(capturedUserPrompt, "API Gateway") {
		t.Error("user prompt should contain entity A name 'API Gateway'")
	}
	if !strings.Contains(capturedUserPrompt, "Load Balancer") {
		t.Error("user prompt should contain entity B name 'Load Balancer'")
	}
	if !strings.Contains(capturedUserPrompt, "it") {
		t.Error("user prompt should contain domain 'it'")
	}
	if !strings.Contains(capturedUserPrompt, "product") {
		t.Error("user prompt should contain domain 'product'")
	}
}

func TestContextLoading(t *testing.T) {
	t.Parallel()

	var capturedUserPrompt string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		if msgs, ok := body["messages"].([]interface{}); ok && len(msgs) >= 2 {
			if msg, ok := msgs[1].(map[string]interface{}); ok {
				if content, ok := msg["content"].([]interface{}); ok && len(content) > 0 {
					if block, ok := content[0].(map[string]interface{}); ok {
						capturedUserPrompt = block["text"].(string)
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"same_entity":true,"confidence":0.9}`}},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	db, dbCleanup := testDB(t)
	defer dbCleanup()
	cacheStore, cacheCleanup := testCacheStore(t)
	defer cacheCleanup()

	// Insert chunks for entity 1.
	for i := 0; i < 5; i++ {
		_, err := db.Exec(
			`INSERT INTO chunks (id, doc_id, chunk_text, sequence_num) VALUES (?, ?, ?, ?)`,
			i+1, 1, fmt.Sprintf("chunk text %d for entity", i), i,
		)
		if err != nil {
			t.Fatalf("insert chunk: %v", err)
		}
		_, err = db.Exec(`INSERT INTO chunk_entities (chunk_id, entity_id) VALUES (?, ?)`, i+1, 1)
		if err != nil {
			t.Fatalf("insert chunk_entity: %v", err)
		}
	}

	cfg := defaultCrossDomainConfig()
	loader := testPromptLoader(t)

	linker, err := relations.NewLLMCrossDomainLinker(
		cfg,
		config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
		db, cacheStore, testLogger(t), loader,
	)
	if err != nil {
		t.Fatalf("NewLLMCrossDomainLinker: %v", err)
	}

	ctx := context.Background()

	a := relations.EntityCandidate{ID: 1, Name: "EntityA", Type: "T", Domain: "D"}
	b := relations.EntityCandidate{ID: 2, Name: "EntityB", Type: "U", Domain: "E"}

	_, _ = linker.LinkPair(ctx, a, b)

	// Should contain first 3 chunks (contextLimit=3).
	if !strings.Contains(capturedUserPrompt, "chunk text 0 for entity") {
		t.Error("prompt should contain chunk 0")
	}
	if !strings.Contains(capturedUserPrompt, "chunk text 1 for entity") {
		t.Error("prompt should contain chunk 1")
	}
	if !strings.Contains(capturedUserPrompt, "chunk text 2 for entity") {
		t.Error("prompt should contain chunk 2")
	}
	// Should NOT contain chunks beyond limit.
	if strings.Contains(capturedUserPrompt, "chunk text 3 for entity") {
		t.Error("prompt should not contain chunk 3 (beyond context limit)")
	}
}

func TestContext_Empty(t *testing.T) {
	t.Parallel()

	var capturedUserPrompt string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck
		if msgs, ok := body["messages"].([]interface{}); ok && len(msgs) >= 2 {
			if msg, ok := msgs[1].(map[string]interface{}); ok {
				if content, ok := msg["content"].([]interface{}); ok && len(content) > 0 {
					if block, ok := content[0].(map[string]interface{}); ok {
						capturedUserPrompt = block["text"].(string)
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"same_entity":true,"confidence":0.9}`}},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	db, dbCleanup := testDB(t)
	defer dbCleanup()
	cacheStore, cacheCleanup := testCacheStore(t)
	defer cacheCleanup()

	cfg := defaultCrossDomainConfig()
	loader := testPromptLoader(t)

	linker, err := relations.NewLLMCrossDomainLinker(
		cfg,
		config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
		db, cacheStore, testLogger(t), loader,
	)
	if err != nil {
		t.Fatalf("NewLLMCrossDomainLinker: %v", err)
	}

	ctx := context.Background()

	a := relations.EntityCandidate{ID: 99, Name: "NoContextA", Type: "T", Domain: "D"}
	b := relations.EntityCandidate{ID: 100, Name: "NoContextB", Type: "U", Domain: "E"}

	_, _ = linker.LinkPair(ctx, a, b)

	if !strings.Contains(capturedUserPrompt, "<no context available>") {
		t.Error("prompt should contain '<no context available>' for entities with no chunks")
	}
}

func TestParseLinkDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		raw      string
		wantSame bool
		wantConf float64
		wantErr  bool
	}{
		{
			name:     "valid positive",
			raw:      `{"same_entity":true,"confidence":0.95,"reasoning":"clear match"}`,
			wantSame: true,
			wantConf: 0.95,
			wantErr:  false,
		},
		{
			name:     "valid negative",
			raw:      `{"same_entity":false,"confidence":0.3}`,
			wantSame: false,
			wantConf: 0.3,
			wantErr:  false,
		},
		{
			name:     "confidence over 1 clamped",
			raw:      `{"same_entity":true,"confidence":1.5}`,
			wantSame: true,
			wantConf: 1.0,
			wantErr:  false,
		},
		{
			name:     "negative confidence clamped",
			raw:      `{"same_entity":false,"confidence":-0.2}`,
			wantSame: false,
			wantConf: 0.0,
			wantErr:  false,
		},
		{
			name:    "invalid JSON",
			raw:     `{not valid json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := llmMockServer(t, `{"same_entity":true,"confidence":0.9}`)
			defer server.Close()

			db, dbCleanup := testDB(t)
			defer dbCleanup()
			cacheStore, cacheCleanup := testCacheStore(t)
			defer cacheCleanup()

			cfg := defaultCrossDomainConfig()
			loader := testPromptLoader(t)

			linker, err := relations.NewLLMCrossDomainLinker(
				cfg,
				config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
				db, cacheStore, testLogger(t), loader,
			)
			if err != nil {
				t.Fatalf("NewLLMCrossDomainLinker: %v", err)
			}

			decision, err := linker.ParseLinkDecision(tt.raw) //nolint:staticcheck
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.SameEntity != tt.wantSame {
				t.Errorf("SameEntity = %v, want %v", decision.SameEntity, tt.wantSame)
			}
			if decision.Confidence != tt.wantConf {
				t.Errorf("Confidence = %f, want %f", decision.Confidence, tt.wantConf)
			}
		})
	}
}

func TestMockLinker(t *testing.T) {
	t.Parallel()

	var mock relations.MockCrossDomainLinker
	mock.Decide = func(a, b relations.EntityCandidate) (*relations.LinkDecision, error) {
		return &relations.LinkDecision{SameEntity: a.Name == b.Name, Confidence: 0.8}, nil
	}

	ctx := context.Background()

	a := relations.EntityCandidate{ID: 1, Name: "API Gateway", Type: "SERVICE", Domain: "it"}
	b := relations.EntityCandidate{ID: 2, Name: "API Gateway", Type: "FEATURE", Domain: "product"}
	c := relations.EntityCandidate{ID: 3, Name: "Database", Type: "SERVICE", Domain: "it"}

	// Test same name.
	decision, err := mock.LinkPair(ctx, a, b)
	if err != nil {
		t.Fatalf("LinkPair(same): %v", err)
	}
	if !decision.SameEntity {
		t.Error("expected SameEntity=true for matching names")
	}

	// Test different name.
	decision, err = mock.LinkPair(ctx, a, c)
	if err != nil {
		t.Fatalf("LinkPair(diff): %v", err)
	}
	if decision.SameEntity {
		t.Error("expected SameEntity=false for different names")
	}

	// Verify calls were recorded.
	if len(mock.CalledInOrder) != 2 {
		t.Errorf("expected 2 recorded calls, got %d", len(mock.CalledInOrder))
	}

	// Test Name().
	if mock.Name() != "mock" {
		t.Errorf("Name() = %q, want %q", mock.Name(), "mock")
	}

	// Test nil Decide fallback.
	var emptyMock relations.MockCrossDomainLinker
	decision, err = emptyMock.LinkPair(ctx, a, b)
	if err != nil {
		t.Fatalf("LinkPair(nil decide): %v", err)
	}
	if decision.SameEntity {
		t.Error("expected SameEntity=false from nil Decide fallback")
	}
}

func TestLLMCall_Integration(t *testing.T) {
	t.Parallel()

	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{
					"content": `{"same_entity":true,"confidence":0.92,"reasoning":"both refer to the same service"}`,
				}},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	db, dbCleanup := testDB(t)
	defer dbCleanup()
	cacheStore, cacheCleanup := testCacheStore(t)
	defer cacheCleanup()

	cfg := defaultCrossDomainConfig()
	loader := testPromptLoader(t)

	linker, err := relations.NewLLMCrossDomainLinker(
		cfg,
		config.LinkerConfig{LLM: config.LLMConfig{APIBaseURL: server.URL, ModelName: "test"}},
		db, cacheStore, testLogger(t), loader,
	)
	if err != nil {
		t.Fatalf("NewLLMCrossDomainLinker: %v", err)
	}

	ctx := context.Background()

	a := relations.EntityCandidate{ID: 1, Name: "API Gateway", Type: "SERVICE", Domain: "it"}
	b := relations.EntityCandidate{ID: 2, Name: "API Gateway", Type: "FEATURE", Domain: "product"}

	decision, err := linker.LinkPair(ctx, a, b)
	if err != nil {
		t.Fatalf("LinkPair: %v", err)
	}

	if !decision.SameEntity {
		t.Error("expected SameEntity=true")
	}
	if decision.Confidence < 0.9 || decision.Confidence > 0.93 {
		t.Errorf("Confidence = %f, expected ~0.92", decision.Confidence)
	}
	if !strings.Contains(decision.Reasoning, "service") {
		t.Error("expected reasoning to contain 'service'")
	}

	if callCount != 1 {
		t.Errorf("expected exactly 1 LLM call, got %d", callCount)
	}

	// Second call should use cache.
	callCount = 0
	_, err = linker.LinkPair(ctx, a, b)
	if err != nil {
		t.Fatalf("LinkPair (cached): %v", err)
	}
	if callCount != 0 {
		t.Errorf("expected 0 LLM calls on cache hit, got %d", callCount)
	}
}

// buildExpectedCacheKey replicates the deterministic key generation for test assertions.
// Key is based on normalized (type, name, domain) + template hashes — not entity IDs — so it's stable across rebuilds.
func buildExpectedCacheKey(a, b relations.EntityCandidate, loader *prompts.PromptLoader) string {
	keyA := a.Type + "\x1F" + utils.Normalize(a.Name) + "\x1F" + utils.Normalize(a.Domain)
	keyB := b.Type + "\x1F" + utils.Normalize(b.Name) + "\x1F" + utils.Normalize(b.Domain)

	first, second := keyA, keyB
	if first > second {
		first, second = second, first
	}

	// Load template hashes from the same loader.
	sysInfo, _ := loader.Load("entity-linker/system") //nolint:errcheck
	userInfo, _ := loader.Load("entity-linker/user")  //nolint:errcheck
	sysHash := ""
	userHash := ""
	if sysInfo != nil {
		sysHash = sysInfo.Hash
	}
	if userInfo != nil {
		userHash = userInfo.Hash
	}

	h := fnv.New128a()
	fmt.Fprint(h, first)
	fmt.Fprint(h, "|")
	fmt.Fprint(h, second)
	fmt.Fprint(h, "|")
	fmt.Fprint(h, sysHash)
	fmt.Fprint(h, "|")
	fmt.Fprint(h, userHash)

	return fmt.Sprintf("%x", h.Sum(nil))
}
