package ner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/domain"
	"github.com/devmix/synopsis/internal/llm"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/prompts"
)

// --- Helper to create a test logger ---

func newTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create test logger: %v", err)
	}
	return log
}

// --- RegexNER Domain Tests ---

func TestRegexNER_DomainTagging(t *testing.T) {
	tests := []struct {
		name          string
		domainConfigs []*domain.DomainConfig
		content       string
		wantEntities  int
		checkDomain   func(t *testing.T, entities []Entity)
	}{
		{
			name: "single domain tags entities correctly",
			domainConfigs: []*domain.DomainConfig{
				{
					Name: "hr",
					Extraction: domain.ExtractionDef{
						RegexRules: []domain.RegexRuleDef{
							{ID: "r1", Pattern: `CEO`, Entity: "ROLE"},
						},
					},
				},
			},
			content:      "The CEO announced changes.",
			wantEntities: 1,
			checkDomain: func(t *testing.T, entities []Entity) {
				if len(entities) != 1 {
					t.Fatalf("want 1 entity, got %d", len(entities))
				}
				if entities[0].Domain != "hr" {
					t.Errorf("entity Domain = %q, want %q", entities[0].Domain, "hr")
				}
			},
		},
		{
			name: "two domains tag entities with correct domain",
			domainConfigs: []*domain.DomainConfig{
				{
					Name: "hr",
					Extraction: domain.ExtractionDef{
						RegexRules: []domain.RegexRuleDef{
							{ID: "r1", Pattern: `CEO`, Entity: "ROLE"},
						},
					},
				},
				{
					Name: "legal",
					Extraction: domain.ExtractionDef{
						RegexRules: []domain.RegexRuleDef{
							{ID: "r2", Pattern: `§\d+`, Entity: "SECTION"},
						},
					},
				},
			},
			content:      "The CEO referenced §10 of the policy.",
			wantEntities: 2,
			checkDomain: func(t *testing.T, entities []Entity) {
				if len(entities) != 2 {
					t.Fatalf("want 2 entities, got %d", len(entities))
				}
				domains := make(map[string]int)
				for _, e := range entities {
					domains[e.Domain]++
				}
				if domains["hr"] != 1 {
					t.Errorf("expected 1 entity with domain 'hr', got %d", domains["hr"])
				}
				if domains["legal"] != 1 {
					t.Errorf("expected 1 entity with domain 'legal', got %d", domains["legal"])
				}
			},
		},
		{
			name: "same entity name in different domains is not deduplicated",
			domainConfigs: []*domain.DomainConfig{
				{
					Name: "hr",
					Extraction: domain.ExtractionDef{
						RegexRules: []domain.RegexRuleDef{
							{ID: "r1", Pattern: `EMPLOYEE`, Entity: "ROLE"},
						},
					},
				},
				{
					Name: "legal",
					Extraction: domain.ExtractionDef{
						RegexRules: []domain.RegexRuleDef{
							{ID: "r2", Pattern: `EMPLOYEE`, Entity: "ROLE"},
						},
					},
				},
			},
			content:      "Every EMPLOYEE must comply.",
			wantEntities: 2, // same name+type but different domains → both kept
			checkDomain: func(t *testing.T, entities []Entity) {
				if len(entities) != 2 {
					t.Fatalf("want 2 entities (one per domain), got %d", len(entities))
				}
				domains := make(map[string]bool)
				for _, e := range entities {
					domains[e.Domain] = true
				}
				if !domains["hr"] || !domains["legal"] {
					t.Errorf("expected both 'hr' and 'legal' domains, got %v", domains)
				}
			},
		},
		{
			name: "same entity in same domain is deduplicated",
			domainConfigs: []*domain.DomainConfig{
				{
					Name: "hr",
					Extraction: domain.ExtractionDef{
						RegexRules: []domain.RegexRuleDef{
							{ID: "r1", Pattern: `CEO`, Entity: "ROLE"},
							{ID: "r2", Pattern: `(?:C|c)EO`, Entity: "ROLE"}, // overlaps with r1
						},
					},
				},
			},
			content:      "The CEO spoke.",
			wantEntities: 1, // deduplicated by name+type+domain within same domain
			checkDomain: func(t *testing.T, entities []Entity) {
				if len(entities) != 1 {
					t.Fatalf("want 1 entity (deduped), got %d", len(entities))
				}
				if entities[0].Domain != "hr" {
					t.Errorf("entity Domain = %q, want %q", entities[0].Domain, "hr")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := newTestLogger(t)
			rner, err := NewRegexNER(tt.domainConfigs, log)
			if err != nil {
				t.Fatalf("NewRegexNER: %v", err)
			}

			result, err := rner.ExtractEntities(context.Background(), tt.content, nil)
			if err != nil {
				t.Fatalf("ExtractEntities: %v", err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}
			if len(result.Entities) != tt.wantEntities {
				t.Errorf("entities count = %d, want %d", len(result.Entities), tt.wantEntities)
			}

			tt.checkDomain(t, result.Entities)
		})
	}
}

// --- CompositeNER Domain Preservation Tests ---

func TestCompositeNER_PreservesDomain(t *testing.T) {
	tests := []struct {
		name    string
		content string
		check   func(t *testing.T, result *Result)
	}{
		{
			name:    "domain from regex provider is preserved",
			content: "The CEO announced changes.",
			check: func(t *testing.T, result *Result) {
				if len(result.Entities) != 1 {
					t.Fatalf("want 1 entity, got %d", len(result.Entities))
				}
				if result.Entities[0].Domain != "hr" {
					t.Errorf("entity Domain = %q, want %q", result.Entities[0].Domain, "hr")
				}
			},
		},
		{
			name:    "domain from multiple providers is preserved",
			content: "The CEO referenced §10.",
			check: func(t *testing.T, result *Result) {
				domains := make(map[string]int)
				for _, e := range result.Entities {
					domains[e.Domain]++
				}
				if domains["hr"] != 1 || domains["legal"] != 1 {
					t.Errorf("expected entities from both 'hr' and 'legal', got %v", domains)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			log := newTestLogger(t)

			domainConfigs := []*domain.DomainConfig{
				{
					Name: "hr",
					Extraction: domain.ExtractionDef{
						RegexRules: []domain.RegexRuleDef{
							{ID: "r1", Pattern: `CEO`, Entity: "ROLE", Confidence: 0.95},
						},
					},
				},
				{
					Name: "legal",
					Extraction: domain.ExtractionDef{
						RegexRules: []domain.RegexRuleDef{
							{ID: "r2", Pattern: `§\d+`, Entity: "SECTION", Confidence: 0.95},
						},
					},
				},
			}

			rner, err := NewRegexNER(domainConfigs, log)
			if err != nil {
				t.Fatalf("NewRegexNER: %v", err)
			}

			cner := NewCompositeNER([]Provider{rner}, domainConfigs, log)
			result, err := cner.ExtractEntities(context.Background(), tt.content, map[string]interface{}{"doc_id": "1"})
			if err != nil {
				t.Fatalf("ExtractEntities: %v", err)
			}
			if result == nil {
				t.Fatal("expected non-nil result")
			}

			tt.check(t, result)
		})
	}
}

// --- LLMNER Per-Domain Tests ---

func TestLLMNER_MultiDomainCalls(t *testing.T) {
	tests := []struct {
		name          string
		domainConfigs []*domain.DomainConfig
		content       string
		wantCalls     int // expected number of HTTP calls (one per domain)
		checkResult   func(t *testing.T, result *Result)
	}{
		{
			name: "single domain produces one call",
			domainConfigs: []*domain.DomainConfig{
				{Name: "hr"},
			},
			content:   "John Smith is the manager.",
			wantCalls: 1,
			checkResult: func(t *testing.T, result *Result) {
				if len(result.Entities) != 2 {
					t.Fatalf("want 2 entities, got %d", len(result.Entities))
				}
				for _, e := range result.Entities {
					if e.Domain != "hr" {
						t.Errorf("entity Domain = %q, want %q", e.Domain, "hr")
					}
				}
			},
		},
		{
			name: "two domains produce two calls with correct domain tags",
			domainConfigs: []*domain.DomainConfig{
				{Name: "hr"},
				{Name: "legal"},
			},
			content:   "John Smith signed the contract.",
			wantCalls: 2,
			checkResult: func(t *testing.T, result *Result) {
				domains := make(map[string]int)
				for _, e := range result.Entities {
					domains[e.Domain]++
				}
				if domains["hr"] == 0 {
					t.Error("expected entities with domain 'hr'")
				}
				if domains["legal"] == 0 {
					t.Error("expected entities with domain 'legal'")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				callCount++
				w.Header().Set("Content-Type", "application/json")
				resp := map[string]interface{}{
					"choices": []map[string]interface{}{
						{
							"message": map[string]interface{}{
								"content": `{
									"entities": [
										{"name": "John Smith", "type": "PERSON", "description": "A person"},
										{"name": "Acme Corp", "type": "ORGANIZATION", "description": "A company"}
									],
									"relations": []
								}`},
						},
					},
				}
				json.NewEncoder(w).Encode(resp) //nolint:errcheck
			}))
			defer server.Close()

			log := newTestLogger(t)

			// Construct LLMNER directly to avoid file-based domain config loading.
			llmClient, err := llm.NewClient(llm.ClientOptions{
				Config: config.LLMConfig{
					APIBaseURL:     server.URL,
					ModelName:      "test-model",
					Temperature:    0.0,
					MaxTokens:      2048,
					ResponseFormat: "json_object",
					MaxRetries:     0,
				},
			})
			if err != nil {
				t.Fatalf("create llm client: %v", err)
			}

			loader, err := prompts.NewLoader("../../../configs/prompts", log)
			if err != nil {
				t.Fatalf("NewLoader: %v", err)
			}

			llmNER := &LLMNER{
				llmClient:     llmClient,
				baseURL:       server.URL,
				modelName:     "test-model",
				temperature:   0.0,
				maxTokens:     2048,
				domainConfigs: tt.domainConfigs,
				log:           log,
				promptLoader:  loader,
			}

			// Load templates so renderSystemPrompt/renderUserPrompt don't fail.
			sysInfo, err := loader.Load("ner/system")
			if err != nil {
				t.Fatalf("load system template: %v", err)
			}
			userInfo, err := loader.Load("ner/user")
			if err != nil {
				t.Fatalf("load user template: %v", err)
			}
			llmNER.systemTmpl = sysInfo.Template
			llmNER.userTmpl = userInfo.Template

			result, err := llmNER.ExtractEntities(context.Background(), tt.content, nil)
			if err != nil {
				t.Fatalf("ExtractEntities: %v", err)
			}

			if callCount != tt.wantCalls {
				t.Errorf("HTTP calls = %d, want %d", callCount, tt.wantCalls)
			}

			tt.checkResult(t, result)
		})
	}
}

// --- LLMNER Multi-Domain + JSON Schema Test ---

func TestLLMNER_MultiDomainJSONSchema(t *testing.T) {
	hrCfg := &domain.DomainConfig{
		Name: "hr",
		Entities: []domain.EntityDef{
			{ID: "person", Name: "Person"},
			{ID: "department", Name: "Department"},
		},
	}

	legalCfg := &domain.DomainConfig{
		Name: "legal",
		Entities: []domain.EntityDef{
			{ID: "contract", Name: "Contract"},
			{ID: "clause", Name: "Clause"},
		},
	}

	var capturedBodies [][]byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		capturedBodies = append(capturedBodies, bodyBytes)

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": `{"entities":[],"relations":[]}`}},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	}))
	defer server.Close()

	log := newTestLogger(t)

	llmClient, err := llm.NewClient(llm.ClientOptions{
		Config: config.LLMConfig{
			APIBaseURL:     server.URL,
			ModelName:      "test-model",
			Temperature:    0.0,
			MaxTokens:      2048,
			ResponseFormat: "json_schema",
			MaxRetries:     0,
		},
	})
	if err != nil {
		t.Fatalf("create llm client: %v", err)
	}

	loader, err := prompts.NewLoader("../../../configs/prompts", log)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	llmNER := &LLMNER{
		llmClient:     llmClient,
		baseURL:       server.URL,
		modelName:     "test-model",
		temperature:   0.0,
		maxTokens:     2048,
		domainConfigs: []*domain.DomainConfig{hrCfg, legalCfg},
		log:           log,
		promptLoader:  loader,
	}

	// Load templates so renderSystemPrompt/renderUserPrompt don't fail.
	sysInfo, err := loader.Load("ner/system")
	if err != nil {
		t.Fatalf("load system template: %v", err)
	}
	userInfo, err := loader.Load("ner/user")
	if err != nil {
		t.Fatalf("load user template: %v", err)
	}
	llmNER.systemTmpl = sysInfo.Template
	llmNER.userTmpl = userInfo.Template

	result, err := llmNER.ExtractEntities(context.Background(), "test content", nil)
	if err != nil {
		t.Fatalf("ExtractEntities: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	if len(capturedBodies) != 2 {
		t.Fatalf("captured %d requests, want 2", len(capturedBodies))
	}

	// Verify each request has json_schema format with domain-specific schema.
	for i, bodyBytes := range capturedBodies {
		var req map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			t.Errorf("request %d: unmarshal: %v", i, err)
			continue
		}

		respFormat, ok := req["response_format"].(map[string]interface{})
		if !ok {
			t.Errorf("request %d: missing response_format", i)
			continue
		}

		if respFormat["type"] != "json_schema" {
			t.Errorf("request %d: response_format.type = %q, want %q", i, respFormat["type"], "json_schema")
			continue
		}

		jsonSchemaRaw, ok := respFormat["json_schema"].(map[string]interface{})
		if !ok {
			t.Errorf("request %d: missing json_schema in response_format", i)
			continue
		}

		schemaObj, ok := jsonSchemaRaw["schema"].(map[string]interface{})
		if !ok {
			t.Errorf("request %d: schema is not an object (json.RawMessage decoded as map)", i)
			continue
		}

		properties, ok := schemaObj["properties"].(map[string]interface{})
		if !ok {
			t.Errorf("request %d: missing properties in schema", i)
			continue
		}

		entitiesSchema, ok := properties["entities"].(map[string]interface{})
		if !ok {
			t.Errorf("request %d: missing entities in schema properties", i)
			continue
		}

		items, ok := entitiesSchema["items"].(map[string]interface{})
		if !ok {
			t.Errorf("request %d: missing items in entities schema", i)
			continue
		}

		entityProperties, ok := items["properties"].(map[string]interface{})
		if !ok {
			t.Errorf("request %d: missing properties in entity items", i)
			continue
		}

		typeProp, ok := entityProperties["type"].(map[string]interface{})
		if !ok {
			t.Errorf("request %d: missing type property in entity schema", i)
			continue
		}

		enum, ok := typeProp["enum"].([]interface{})
		if !ok {
			t.Errorf("request %d: missing enum for entity type", i)
			continue
		}

		entityTypes := make(map[string]bool)
		for _, v := range enum {
			if s, ok := v.(string); ok {
				entityTypes[s] = true
			}
		}

		// Verify domain-specific entity types.
		switch i {
		case 0: // hr domain
			if !entityTypes["person"] || !entityTypes["department"] {
				t.Errorf("request 0 (hr): expected entity types [person, department], got %v", entityTypes)
			}
			if entityTypes["contract"] || entityTypes["clause"] {
				t.Errorf("request 0 (hr): should not contain legal entity types, got %v", entityTypes)
			}
		case 1: // legal domain
			if !entityTypes["contract"] || !entityTypes["clause"] {
				t.Errorf("request 1 (legal): expected entity types [contract, clause], got %v", entityTypes)
			}
			if entityTypes["person"] || entityTypes["department"] {
				t.Errorf("request 1 (legal): should not contain hr entity types, got %v", entityTypes)
			}
		}
	}
}

// --- parseLLMResponse backward compatibility test ---

func TestParseLLMResponse_BackwardCompatible(t *testing.T) {
	tests := []struct {
		name         string
		raw          string
		wantErr      bool
		wantEntities int
		wantFacts    int
	}{
		{
			name: "valid response with entities and facts",
			raw: `{
				"entities": [
					{"name": "Alice", "type": "PERSON", "description": "A person"}
				],
				"relations": [
					{"subject_type": "PERSON", "subject_name": "Alice", "predicate": "works_at", "object_type": "ORGANIZATION", "object_name": "Acme"}
				]
			}`,
			wantErr:      false,
			wantEntities: 1,
			wantFacts:    1,
		},
		{
			name:         "empty response",
			raw:          `{"entities": [], "relations": []}`,
			wantErr:      false,
			wantEntities: 0,
			wantFacts:    0,
		},
		{
			name:    "invalid JSON",
			raw:     `{not valid json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseLLMResponse(tt.raw)

			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(result.Entities) != tt.wantEntities {
				t.Errorf("entities = %d, want %d", len(result.Entities), tt.wantEntities)
			}
			if len(result.Facts) != tt.wantFacts {
				t.Errorf("facts = %d, want %d", len(result.Facts), tt.wantFacts)
			}

			// Domain should be empty string (set by caller in ExtractEntities).
			for _, e := range result.Entities {
				if e.Domain != "" {
					t.Errorf("parseLLMResponse should not set Domain, got %q", e.Domain)
				}
			}
			for _, f := range result.Facts {
				if f.Domain != "" {
					t.Errorf("parseLLMResponse should not set Domain on facts, got %q", f.Domain)
				}
			}
		})
	}
}

// --- Entity and Fact struct field tests ---

func TestEntity_DomainField(t *testing.T) {
	e := Entity{
		Name:     "Alice",
		Type:     "PERSON",
		Domain:   "hr",
		Metadata: map[string]interface{}{"key": "value"},
	}

	if e.Domain != "hr" {
		t.Errorf("Entity.Domain = %q, want %q", e.Domain, "hr")
	}

	// Zero value test.
	e2 := Entity{Name: "Bob", Type: "PERSON"}
	if e2.Domain != "" {
		t.Errorf("Entity.Domain zero value should be empty string, got %q", e2.Domain)
	}
}

func TestFact_DomainField(t *testing.T) {
	f := Fact{
		SubjectType: "PERSON",
		SubjectName: "Alice",
		Predicate:   "works_at",
		ObjectType:  "ORGANIZATION",
		ObjectName:  "Acme",
		Domain:      "hr",
		Metadata:    map[string]interface{}{"key": "value"},
	}

	if f.Domain != "hr" {
		t.Errorf("Fact.Domain = %q, want %q", f.Domain, "hr")
	}

	// Zero value test.
	f2 := Fact{SubjectType: "PERSON", SubjectName: "Bob", Predicate: "knows", ObjectType: "PERSON", ObjectName: "Alice"}
	if f2.Domain != "" {
		t.Errorf("Fact.Domain zero value should be empty string, got %q", f2.Domain)
	}
}

// --- parseLLMResponse confidence parsing tests (entity-level) ---

func TestParseLLMResponse_ConfidenceFromEntityLevel(t *testing.T) {
	tests := []struct {
		name           string
		raw            string
		wantConfidence float64
	}{
		{
			name: "confidence from entity level as number",
			raw: `{
				"entities": [
					{"name": "Alice", "type": "PERSON", "description": "", "confidence": 0.95}
				],
				"relations": []
			}`,
			wantConfidence: 0.95,
		},
		{
			name: "no confidence field uses default 0.5",
			raw: `{
				"entities": [
					{"name": "Charlie", "type": "PERSON", "description": ""}
				],
				"relations": []
			}`,
			wantConfidence: 0.5,
		},
		{
			name: "out-of-range confidence (too high) uses default",
			raw: `{
				"entities": [
					{"name": "Eve", "type": "PERSON", "description": "", "confidence": 1.5}
				],
				"relations": []
			}`,
			wantConfidence: 0.5,
		},
		{
			name: "out-of-range confidence (negative) uses default",
			raw: `{
				"entities": [
					{"name": "Frank", "type": "PERSON", "description": "", "confidence": -0.3}
				],
				"relations": []
			}`,
			wantConfidence: 0.5,
		},
		{
			name: "zero confidence is valid",
			raw: `{
				"entities": [
					{"name": "Grace", "type": "PERSON", "description": "", "confidence": 0}
				],
				"relations": []
			}`,
			wantConfidence: 0,
		},
		{
			name: "one confidence is valid",
			raw: `{
				"entities": [
					{"name": "Heidi", "type": "PERSON", "description": "", "confidence": 1.0}
				],
				"relations": []
			}`,
			wantConfidence: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseLLMResponse(tt.raw)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(result.Entities) != 1 {
				t.Fatalf("entities = %d, want 1", len(result.Entities))
			}
			got := result.Entities[0].Confidence
			if got != tt.wantConfidence {
				t.Errorf("confidence = %f, want %f", got, tt.wantConfidence)
			}
		})
	}
}

// --- filterByConfidence tests ---

func TestFilterByConfidence(t *testing.T) {
	tests := []struct {
		name      string
		entities  []Entity
		threshold float64
		wantCount int
	}{
		{
			name: "all above threshold",
			entities: []Entity{
				{Name: "A", Confidence: 0.9},
				{Name: "B", Confidence: 0.85},
				{Name: "C", Confidence: 1.0},
			},
			threshold: 0.85,
			wantCount: 3,
		},
		{
			name: "some below threshold",
			entities: []Entity{
				{Name: "A", Confidence: 0.9},
				{Name: "B", Confidence: 0.4},
				{Name: "C", Confidence: 1.0},
			},
			threshold: 0.5,
			wantCount: 2,
		},
		{
			name: "all below threshold",
			entities: []Entity{
				{Name: "A", Confidence: 0.3},
				{Name: "B", Confidence: 0.1},
			},
			threshold: 0.5,
			wantCount: 0,
		},
		{
			name:      "empty input",
			entities:  []Entity{},
			threshold: 0.85,
			wantCount: 0,
		},
		{
			name: "exactly at threshold is kept",
			entities: []Entity{
				{Name: "A", Confidence: 0.85},
			},
			threshold: 0.85,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByConfidence(tt.entities, tt.threshold)
			if len(got) != tt.wantCount {
				t.Errorf("filterByConfidence returned %d entities, want %d", len(got), tt.wantCount)
			}
		})
	}
}
