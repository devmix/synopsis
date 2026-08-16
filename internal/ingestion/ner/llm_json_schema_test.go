package ner

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/devmix/synopsis/internal/domain"
	"github.com/devmix/synopsis/internal/prompts"
)

// TestGenerateJSONSchema tests the full JSON schema generation
func TestGenerateJSONSchema(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *domain.DomainConfig
		wantErr      bool
		errContains  string
		verifySchema func(t *testing.T, schema string)
	}{
		{
			name:    "valid domain config with entities and relations",
			wantErr: false,
			cfg: &domain.DomainConfig{
				Name:        "test-domain",
				Version:     "1.0",
				Description: "Test domain",
				Entities: []domain.EntityDef{
					{
						ID:          "person",
						Name:        "Person",
						Description: "A person entity",
						Attributes: []domain.AttributeDef{
							{Name: "full_name", Type: "string", Required: true},
							{Name: "position", Type: "string"},
						},
					},
					{
						ID:          "supplier",
						Name:        "Supplier",
						Description: "A supplier entity",
						Attributes: []domain.AttributeDef{
							{Name: "company_name", Type: "string"},
						},
					},
					{
						ID:          "contract",
						Name:        "Contract",
						Description: "A contract entity",
						Attributes: []domain.AttributeDef{
							{Name: "contract_number", Type: "string"},
							{Name: "date", Type: "date"},
						},
					},
				},
				Relations: []domain.RelationDef{
					{
						Predicate:   "owner_of",
						Description: "Person owns a contract",
						Source:      "person",
						Target:      "contract",
					},
					{
						Predicate:   "signed_by",
						Description: "Contract is signed by person",
						Source:      "contract",
						Target:      "person",
					},
				},
			},
			verifySchema: func(t *testing.T, schema string) {
				var parsed map[string]interface{}
				err := json.Unmarshal([]byte(schema), &parsed)
				if err != nil {
					t.Fatalf("Failed to parse generated JSON schema: %v", err)
				}

				// Check top-level structure
				if parsed["type"] != "object" {
					t.Errorf("Expected schema type 'object', got '%v'", parsed["type"])
				}

				// Check properties exist
				properties, ok := parsed["properties"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'properties' to be an object")
				}

				// Check entities property
				entities, exists := properties["entities"]
				if !exists {
					t.Error("Expected 'entities' property in schema")
				} else {
					entitiesMap, ok := entities.(map[string]interface{})
					if !ok {
						t.Error("Expected 'entities' to be an object")
					} else if entitiesMap["type"] != "array" {
						t.Errorf("Expected entities type 'array', got '%v'", entitiesMap["type"])
					}
				}

				// Check relations property
				relations, exists := properties["relations"]
				if !exists {
					t.Error("Expected 'relations' property in schema")
				} else {
					relationsMap, ok := relations.(map[string]interface{})
					if !ok {
						t.Error("Expected 'relations' to be an object")
					} else if relationsMap["type"] != "array" {
						t.Errorf("Expected relations type 'array', got '%v'", relationsMap["type"])
					}
				}

				// Check required fields
				required, ok := parsed["required"].([]interface{})
				if !ok {
					t.Error("Expected 'required' to be an array")
				} else {
					found := false
					for _, req := range required {
						if req == "entities" {
							found = true
							break
						}
					}
					if !found {
						t.Error("Expected 'entities' in required fields")
					}
				}
			},
		},
		{
			name:        "nil config returns error",
			cfg:         nil,
			wantErr:     true,
			errContains: "nil",
		},
		{
			name:    "empty config generates minimal schema",
			wantErr: false,
			cfg: &domain.DomainConfig{
				Name:        "empty-domain",
				Version:     "1.0",
				Description: "Empty domain",
				Entities:    []domain.EntityDef{},
				Relations:   []domain.RelationDef{},
			},
			verifySchema: func(t *testing.T, schema string) {
				var parsed map[string]interface{}
				err := json.Unmarshal([]byte(schema), &parsed)
				if err != nil {
					t.Fatalf("Failed to parse generated JSON schema: %v", err)
				}

				// Should have entities but no relations (since none defined)
				properties, ok := parsed["properties"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'properties' to be an object")
				}

				if _, exists := properties["entities"]; !exists {
					t.Error("Expected 'entities' property even with empty config")
				}

				if _, exists := properties["relations"]; exists {
					t.Error("Facts should not be present when no relations are defined")
				}
			},
		},
		{
			name:    "entities only - no relations",
			wantErr: false,
			cfg: &domain.DomainConfig{
				Name:        "entities-only",
				Version:     "1.0",
				Description: "Entities only domain",
				Entities: []domain.EntityDef{
					{ID: "person", Name: "Person", Description: "A person"},
					{ID: "organization", Name: "Organization", Description: "An organization"},
				},
				Relations: []domain.RelationDef{},
			},
			verifySchema: func(t *testing.T, schema string) {
				var parsed map[string]interface{}
				err := json.Unmarshal([]byte(schema), &parsed)
				if err != nil {
					t.Fatalf("Failed to parse generated JSON schema: %v", err)
				}

				properties, ok := parsed["properties"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'properties' to be an object")
				}

				if _, exists := properties["entities"]; !exists {
					t.Error("Expected 'entities' property")
				}

				if _, exists := properties["relations"]; exists {
					t.Error("Facts should not be present when no relations are defined")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateJSONSchema(tt.cfg, true)

			if tt.cfg == nil {
				if result != "" {
					t.Error("Expected empty schema for nil config")
				}
				return
			}

			if result == "" {
				t.Error("Expected non-empty schema string")
			}

			if tt.verifySchema != nil {
				tt.verifySchema(t, result)
			}
		})
	}
}

// TestGenerateEntitySchema tests entity schema generation
func TestGenerateEntitySchema(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *domain.DomainConfig
		verifySchema func(t *testing.T, schema map[string]interface{})
	}{
		{
			name: "entities with attributes",
			cfg: &domain.DomainConfig{
				Entities: []domain.EntityDef{
					{
						ID:          "person",
						Name:        "Person",
						Description: "A person",
						Attributes: []domain.AttributeDef{
							{Name: "full_name", Type: "string", Required: true},
							{Name: "position", Type: "string"},
							{Name: "age", Type: "number"},
						},
					},
				},
			},
			verifySchema: func(t *testing.T, schema map[string]interface{}) {
				// Check type is array
				if schema["type"] != "array" {
					t.Errorf("Expected schema type 'array', got '%v'", schema["type"])
				}

				// Check items structure
				items, ok := schema["items"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'items' to be an object")
				}

				// Check properties
				properties, ok := items["properties"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'properties' to be an object")
				}

				// Check required fields (at items level, not properties level)
				required, ok := items["required"].([]interface{})
				if !ok {
					t.Fatal("Expected 'required' to be an array")
				}

				hasName := false
				hasType := false
				for _, req := range required {
					if reqStr, ok := req.(string); ok && reqStr == "name" {
						hasName = true
					}
					if reqStr, ok := req.(string); ok && reqStr == "type" {
						hasType = true
					}
				}
				if !hasName || !hasType {
					t.Error("Expected 'name' and 'type' in required fields")
				}

				// Check enum for entity types
				typeProp, ok := properties["type"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'type' property")
				}
				enum, ok := typeProp["enum"].([]string)
				if !ok {
					t.Error("Expected 'enum' for type property")
				} else {
					if len(enum) != 1 || enum[0] != "person" {
						t.Errorf("Expected enum ['person'], got %v", enum)
					}
				}

				// Check attributes schema
				attrs, ok := properties["attributes"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'attributes' property")
				}
				if attrs["type"] != "object" {
					t.Errorf("Expected attributes type 'object', got '%v'", attrs["type"])
				}
			},
		},
		{
			name: "entities with different attribute types",
			cfg: &domain.DomainConfig{
				Entities: []domain.EntityDef{
					{
						ID: "product",
						Attributes: []domain.AttributeDef{
							{Name: "name", Type: "string"},
							{Name: "price", Type: "number"},
							{Name: "available", Type: "boolean"},
							{Name: "created_at", Type: "date"},
							{Name: "category_ref", Type: "ref", Target: "category"},
						},
					},
				},
			},
			verifySchema: func(t *testing.T, schema map[string]interface{}) {
				items := schema["items"].(map[string]interface{})
				properties := items["properties"].(map[string]interface{})
				attrs := properties["attributes"].(map[string]interface{})
				attrsProps := attrs["properties"].(map[string]interface{})

				// Check each attribute type mapping
				nameAttr := attrsProps["name"].(map[string]interface{})
				if nameAttr["type"] != "string" {
					t.Errorf("Expected 'name' type 'string', got '%v'", nameAttr["type"])
				}

				priceAttr := attrsProps["price"].(map[string]interface{})
				if priceAttr["type"] != "number" {
					t.Errorf("Expected 'price' type 'number', got '%v'", priceAttr["type"])
				}

				availableAttr := attrsProps["available"].(map[string]interface{})
				if availableAttr["type"] != "boolean" {
					t.Errorf("Expected 'available' type 'boolean', got '%v'", availableAttr["type"])
				}

				createdAtAttr := attrsProps["created_at"].(map[string]interface{})
				if createdAtAttr["type"] != "string" {
					t.Errorf("Expected 'created_at' type 'string' (for date), got '%v'", createdAtAttr["type"])
				}

				categoryRefAttr := attrsProps["category_ref"].(map[string]interface{})
				if categoryRefAttr["type"] != "string" {
					t.Errorf("Expected 'category_ref' type 'string' (for ref), got '%v'", categoryRefAttr["type"])
				}
			},
		},
		{
			name: "empty entities",
			cfg: &domain.DomainConfig{
				Entities: []domain.EntityDef{},
			},
			verifySchema: func(t *testing.T, schema map[string]interface{}) {
				// Should still have basic structure
				if schema["type"] != "array" {
					t.Errorf("Expected schema type 'array', got '%v'", schema["type"])
				}

				items, ok := schema["items"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'items' to be an object")
				}

				properties, ok := items["properties"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'properties' to be an object")
				}

				// Should not have enum when no entities
				typeProp, ok := properties["type"].(map[string]interface{})
				if ok {
					if _, hasEnum := typeProp["enum"]; hasEnum {
						t.Error("Expected no enum when entities list is empty")
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateEntitySchema(tt.cfg)

			if len(result) == 0 {
				t.Error("Expected non-empty schema")
			}

			if tt.verifySchema != nil {
				tt.verifySchema(t, result)
			}
		})
	}
}

// TestGenerateRelationSchema tests relation schema generation
func TestGenerateRelationSchema(t *testing.T) {
	tests := []struct {
		name         string
		cfg          *domain.DomainConfig
		verifySchema func(t *testing.T, schema map[string]interface{})
	}{
		{
			name: "relations with attributes",
			cfg: &domain.DomainConfig{
				Entities: []domain.EntityDef{
					{ID: "person", Name: "Person"},
					{ID: "contract", Name: "Contract"},
				},
				Relations: []domain.RelationDef{
					{
						Predicate:   "signed_by",
						Description: "Contract signed by person",
						Source:      "contract",
						Target:      "person",
						Attributes: []domain.RelAttrDef{
							{Name: "signature_date", Type: "date"},
							{Name: "witness", Type: "string"},
						},
					},
				},
			},
			verifySchema: func(t *testing.T, schema map[string]interface{}) {
				// Check type is array
				if schema["type"] != "array" {
					t.Errorf("Expected schema type 'array', got '%v'", schema["type"])
				}

				// Check items structure
				items, ok := schema["items"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'items' to be an object")
				}

				// Check properties
				properties, ok := items["properties"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'properties' to be an object")
				}

				// Check required fields (at items level, not properties level)
				required, ok := items["required"].([]interface{})
				if !ok {
					t.Fatal("Expected 'required' to be an array")
				}

				expectedRequired := []string{"subject_type", "subject_name", "predicate", "object_type", "object_name"}
				for _, exp := range expectedRequired {
					found := false
					for _, req := range required {
						if reqStr, ok := req.(string); ok && reqStr == exp {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("Expected '%s' in required fields", exp)
					}
				}

				// Check enum for predicate
				predicateProp, ok := properties["predicate"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'predicate' property")
				}
				enum, ok := predicateProp["enum"].([]string)
				if !ok {
					t.Error("Expected 'enum' for predicate property")
				} else {
					if len(enum) != 1 || enum[0] != "signed_by" {
						t.Errorf("Expected enum ['signed_by'], got %v", enum)
					}
				}

				// Check attributes schema
				attrs, ok := properties["attributes"].(map[string]interface{})
				if !ok {
					t.Fatal("Expected 'attributes' property")
				}
				if attrs["type"] != "object" {
					t.Errorf("Expected attributes type 'object', got '%v'", attrs["type"])
				}
			},
		},
		{
			name: "multiple relations",
			cfg: &domain.DomainConfig{
				Entities: []domain.EntityDef{
					{ID: "person", Name: "Person"},
					{ID: "contract", Name: "Contract"},
				},
				Relations: []domain.RelationDef{
					{Predicate: "owner_of", Source: "person", Target: "contract"},
					{Predicate: "signed_by", Source: "contract", Target: "person"},
					{Predicate: "manages", Source: "person", Target: "person"},
				},
			},
			verifySchema: func(t *testing.T, schema map[string]interface{}) {
				items := schema["items"].(map[string]interface{})
				properties := items["properties"].(map[string]interface{})
				predicateProp := properties["predicate"].(map[string]interface{})
				enum := predicateProp["enum"].([]string)

				if len(enum) != 3 {
					t.Errorf("Expected 3 enum values, got %d", len(enum))
				}

				enumSet := make(map[string]bool)
				for _, v := range enum {
					enumSet[v] = true
				}

				expected := []string{"owner_of", "signed_by", "manages"}
				for _, exp := range expected {
					if !enumSet[exp] {
						t.Errorf("Expected enum to contain '%s'", exp)
					}
				}
			},
		},
		{
			name: "empty relations",
			cfg: &domain.DomainConfig{
				Entities: []domain.EntityDef{
					{ID: "person", Name: "Person"},
				},
				Relations: []domain.RelationDef{},
			},
			verifySchema: func(t *testing.T, schema map[string]interface{}) {
				items := schema["items"].(map[string]interface{})
				properties := items["properties"].(map[string]interface{})
				predicateProp := properties["predicate"].(map[string]interface{})

				// Should not have enum when no relations
				if _, hasEnum := predicateProp["enum"]; hasEnum {
					t.Error("Expected no enum when relations list is empty")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GenerateRelationSchema(tt.cfg)

			if len(result) == 0 {
				t.Error("Expected non-empty schema")
			}

			if tt.verifySchema != nil {
				tt.verifySchema(t, result)
			}
		})
	}
}

// TestTemplateRenderSystemPrompt tests NER system prompt rendering via template loader.
func TestTemplateRenderSystemPrompt(t *testing.T) {
	loader, err := prompts.NewLoader("../../../configs/prompts", nil)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	tests := []struct {
		name         string
		cfg          *domain.DomainConfig
		verifyPrompt func(t *testing.T, prompt string)
	}{
		{
			name: "full domain config",
			cfg: &domain.DomainConfig{
				Name:        "test-domain",
				Version:     "1.0",
				Description: "Test domain",
				Entities: []domain.EntityDef{
					{
						ID:          "person",
						Name:        "Person",
						Description: "A person entity",
						Attributes: []domain.AttributeDef{
							{Name: "full_name", Type: "string", Required: true},
							{Name: "position", Type: "string"},
						},
						Synonyms: []string{"individual", "human"},
					},
				},
				Relations: []domain.RelationDef{
					{
						Predicate:   "owner_of",
						Description: "Person owns something",
						Source:      "person",
						Target:      "contract",
					},
				},
			},
			verifyPrompt: func(t *testing.T, prompt string) {
				sections := []string{
					"ENTITY TYPES",
					"RELATION TYPES",
					"INSTRUCTIONS",
					"OUTPUT FORMAT",
				}

				for _, section := range sections {
					if !strings.Contains(prompt, section) {
						t.Errorf("Expected prompt to contain section '%s'", section)
					}
				}

				if !strings.Contains(prompt, "person") {
					t.Error("Expected prompt to mention entity type 'person'")
				}
				if !strings.Contains(prompt, "owner_of") {
					t.Error("Expected prompt to mention relation type 'owner_of'")
				}
				if !strings.Contains(prompt, "full_name") {
					t.Error("Expected prompt to mention attribute 'full_name'")
				}
				if !strings.Contains(prompt, "individual") {
					t.Error("Expected prompt to mention synonym 'individual'")
				}
				if !strings.Contains(prompt, `"entities"`) {
					t.Error("Expected prompt to contain JSON format example with 'entities'")
				}
				if !strings.Contains(prompt, `"relations"`) {
					t.Error("Expected prompt to contain JSON format example with 'relations'")
				}
			},
		},
		{
			name: "entities only",
			cfg: &domain.DomainConfig{
				Entities: []domain.EntityDef{
					{ID: "person", Name: "Person", Description: "A person"},
				},
				Relations: []domain.RelationDef{},
			},
			verifyPrompt: func(t *testing.T, prompt string) {
				if !strings.Contains(prompt, "ENTITY TYPES") {
					t.Error("Expected prompt to contain ENTITY TYPES section")
				}
				if strings.Contains(prompt, "RELATION TYPES:") {
					t.Error("Expected prompt to NOT contain RELATION TYPES section when no relations defined")
				}
			},
		},
		{
			name: "empty config",
			cfg: &domain.DomainConfig{
				Entities:  []domain.EntityDef{},
				Relations: []domain.RelationDef{},
			},
			verifyPrompt: func(t *testing.T, prompt string) {
				if !strings.Contains(prompt, "ENTITY TYPES") {
					t.Error("Expected prompt to contain ENTITY TYPES section")
				}
				if !strings.Contains(prompt, "No entity types defined") {
					t.Error("Expected prompt to indicate no entity types defined")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := renderSystemPromptWithLoader(loader, tt.cfg, true)
			if err != nil {
				t.Fatalf("render system prompt: %v", err)
			}

			if result == "" {
				t.Error("Expected non-empty prompt")
			}

			if tt.verifyPrompt != nil {
				tt.verifyPrompt(t, result)
			}
		})
	}
}

// TestTemplateRenderUserPrompt tests NER user prompt rendering via template loader.
func TestTemplateRenderUserPrompt(t *testing.T) {
	loader, err := prompts.NewLoader("../../../configs/prompts", nil)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	tests := []struct {
		name         string
		cfg          *domain.DomainConfig
		content      string
		verifyPrompt func(t *testing.T, prompt string)
	}{
		{
			name: "with entities and relations",
			cfg: &domain.DomainConfig{
				Entities: []domain.EntityDef{
					{ID: "person", Name: "Person"},
					{ID: "contract", Name: "Contract"},
				},
				Relations: []domain.RelationDef{
					{Predicate: "signed_by", Source: "contract", Target: "person"},
				},
			},
			content: "John Smith signed contract ABC-123 on behalf of Acme Corp.",
			verifyPrompt: func(t *testing.T, prompt string) {
				if !strings.Contains(prompt, "person") {
					t.Error("Expected prompt to mention entity type 'person'")
				}
				if !strings.Contains(prompt, "contract") {
					t.Error("Expected prompt to mention entity type 'contract'")
				}
				if !strings.Contains(prompt, "signed_by") {
					t.Error("Expected prompt to mention relation type 'signed_by'")
				}
				instructions := []string{
					"extract entities",
					"valid json",
					"empty arrays",
				}
				for _, instr := range instructions {
					if !strings.Contains(strings.ToLower(prompt), instr) {
						t.Errorf("Expected prompt to contain instruction about '%s'", instr)
					}
				}
			},
		},
		{
			name: "entities only",
			cfg: &domain.DomainConfig{
				Entities: []domain.EntityDef{
					{ID: "person", Name: "Person"},
				},
				Relations: []domain.RelationDef{},
			},
			content: "Jane Doe is a software engineer.",
			verifyPrompt: func(t *testing.T, prompt string) {
				if !strings.Contains(prompt, "person") {
					t.Error("Expected prompt to mention entity type 'person'")
				}
			},
		},
		{
			name: "multi-line content",
			cfg: &domain.DomainConfig{
				Entities: []domain.EntityDef{
					{ID: "organization", Name: "Organization"},
				},
			},
			content: "First line.\nSecond line with more info.\nThird line.",
			verifyPrompt: func(t *testing.T, prompt string) {
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, attachment, err := renderUserPromptWithLoader(loader, tt.cfg, tt.content)
			if err != nil {
				t.Fatalf("render user prompt: %v", err)
			}

			if result == "" {
				t.Error("Expected non-empty prompt")
			}

			if attachment == "" {
				t.Fatal("Expected non-empty attachment")
			}

			if !strings.Contains(attachment, tt.content) {
				t.Error("Attachment must contain the full content")
			}

			if strings.Contains(result, tt.content) {
				t.Error("Content must NOT be in the user prompt — it is passed via Attachments")
			}

			if tt.verifyPrompt != nil {
				tt.verifyPrompt(t, result)
			}
		})
	}
}

// renderSystemPromptWithLoader renders the NER system template using a PromptLoader.
func renderSystemPromptWithLoader(loader *prompts.PromptLoader, cfg *domain.DomainConfig, withJSONExample bool) (string, error) {
	info, err := loader.Load("ner/system")
	if err != nil {
		return "", fmt.Errorf("load ner system template: %w", err)
	}

	data := nerSystemPromptData{
		Entities:        cfg.Entities,
		Relations:       cfg.Relations,
		WithJSONExample: withJSONExample,
	}

	var buf strings.Builder
	if err := info.Template.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute system template: %w", err)
	}
	return buf.String(), nil
}

// renderUserPromptWithLoader renders the NER user template using a PromptLoader.
// Returns (prompt, attachment, error) to match production CallOptions structure.
func renderUserPromptWithLoader(loader *prompts.PromptLoader, cfg *domain.DomainConfig, content string) (string, string, error) {
	info, err := loader.Load("ner/user")
	if err != nil {
		return "", "", fmt.Errorf("load ner user template: %w", err)
	}

	data := map[string]interface{}{
		"EntityTypes":   cfg.GetEntityTypes(),
		"RelationTypes": cfg.GetRelationTypes(),
	}

	var buf strings.Builder
	if err := info.Template.Execute(&buf, data); err != nil {
		return "", "", fmt.Errorf("execute user template: %w", err)
	}
	return buf.String(), "CONTENT:\n\n" + content, nil
}

// TestJSONSchemaValidity tests that generated schemas are valid JSON
func TestJSONSchemaValidity(t *testing.T) {
	cfg := &domain.DomainConfig{
		Name:        "validation-test",
		Version:     "1.0",
		Description: "Test for schema validity",
		Entities: []domain.EntityDef{
			{
				ID:          "person",
				Name:        "Person",
				Description: "A person",
				Attributes: []domain.AttributeDef{
					{Name: "name", Type: "string", Required: true},
					{Name: "age", Type: "number"},
				},
			},
			{
				ID:          "company",
				Name:        "Company",
				Description: "A company",
				Attributes: []domain.AttributeDef{
					{Name: "company_name", Type: "string"},
					{Name: "founded", Type: "date"},
				},
			},
		},
		Relations: []domain.RelationDef{
			{
				Predicate:   "works_for",
				Description: "Person works for company",
				Source:      "person",
				Target:      "company",
				Attributes: []domain.RelAttrDef{
					{Name: "start_date", Type: "date"},
					{Name: "position", Type: "string"},
				},
			},
		},
	}

	t.Run("schema is valid JSON", func(t *testing.T) {
		schema := GenerateJSONSchema(cfg, true)

		var parsed interface{}
		if err := json.Unmarshal([]byte(schema), &parsed); err != nil {
			t.Fatalf("Generated schema is not valid JSON: %v", err)
		}
	})

	t.Run("entity schema is valid JSON", func(t *testing.T) {
		entitySchema := GenerateEntitySchema(cfg)

		if _, err := json.Marshal(entitySchema); err != nil {
			t.Fatalf("Entity schema is not valid JSON: %v", err)
		}
	})

	t.Run("relation schema is valid JSON", func(t *testing.T) {
		relationSchema := GenerateRelationSchema(cfg)

		if _, err := json.Marshal(relationSchema); err != nil {
			t.Fatalf("Fact schema is not valid JSON: %v", err)
		}
	})
}

// TestPromptContent tests that prompts contain expected instructional content
func TestPromptContent(t *testing.T) {
	cfg := &domain.DomainConfig{
		Entities: []domain.EntityDef{
			{ID: "person", Name: "Person", Description: "A person entity"},
		},
		Relations: []domain.RelationDef{
			{Predicate: "knows", Source: "person", Target: "person", Description: "Person knows person"},
		},
	}

	loader, err := prompts.NewLoader("../../../configs/prompts", nil)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	t.Run("system prompt has instructions", func(t *testing.T) {
		prompt, err := renderSystemPromptWithLoader(loader, cfg, false)
		if err != nil {
			t.Fatalf("render system prompt: %v", err)
		}

		instructions := []string{
			"extract",
			"entities",
			"relations",
			"json",
		}

		for _, instr := range instructions {
			if !strings.Contains(strings.ToLower(prompt), instr) {
				t.Errorf("System prompt should contain instruction about '%s'", instr)
			}
		}
	})

	t.Run("NER prompt references schema", func(t *testing.T) {
		prompt, _, err := renderUserPromptWithLoader(loader, cfg, "Sample text content")
		if err != nil {
			t.Fatalf("render user prompt: %v", err)
		}

		if !strings.Contains(strings.ToLower(prompt), "schema") &&
			!strings.Contains(strings.ToLower(prompt), "entity type") {
			t.Error("NER prompt should reference the schema or entity types")
		}
	})
}
