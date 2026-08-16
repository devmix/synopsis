package domain

import (
	"os"
	"testing"
)

// TestLoadDomainConfig tests the LoadDomainConfig function with various inputs.
func TestLoadDomainConfig(t *testing.T) {
	t.Parallel()

	t.Run("valid XML", func(t *testing.T) {
		t.Parallel()
		xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<domain name="product" version="1.0" description="Product domain">
    <entity id="product" name="Product" description="A product in the catalog">
        <attribute name="title" type="string" required="true"/>
        <attribute name="price" type="number" required="true"/>
        <synonym>item</synonym>
        <synonym>goods</synonym>
    </entity>
    <entity id="category" name="Category" description="Product category">
        <attribute name="name" type="string" required="true"/>
    </entity>
    <relation predicate="belongs_to" description="Product belongs to category" source="product" target="category">
        <attribute name="primary" type="condition"/>
    </relation>
    <extraction>
        <method>regex</method>
        <method>dictionary</method>
        <regex id="price_rule" entity="product" attribute="price" pattern="\$\d+\.\d{2}" confidence="0.9"/>
        <dictionary name="product_keywords">
            <keyword>laptop</keyword>
            <keyword>phone</keyword>
        </dictionary>
        <llm enabled="true" model="gpt-4" temperature="0.7" max_tokens="1000" confidence_threshold="0.75"/>
    </extraction>
    <confidence auto_publish_threshold="0.85" review_threshold="0.60" reject_threshold="0.40"/>
</domain>`

		tmpFile, err := os.CreateTemp("", "domain-*.xml")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		defer os.Remove(tmpFile.Name()) //nolint:errcheck

		if _, err := tmpFile.WriteString(xmlContent); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		_ = tmpFile.Close()

		config, err := LoadDomainConfig(tmpFile.Name())
		if err != nil {
			t.Fatalf("LoadDomainConfig failed: %v", err)
		}

		if config.Name != "product" {
			t.Errorf("expected name 'product', got '%s'", config.Name)
		}
		if config.Version != "1.0" {
			t.Errorf("expected version '1.0', got '%s'", config.Version)
		}
		if len(config.Entities) != 2 {
			t.Errorf("expected 2 entities, got %d", len(config.Entities))
		}
		if len(config.Relations) != 1 {
			t.Errorf("expected 1 relation, got %d", len(config.Relations))
		}
	})

	t.Run("empty XML domain", func(t *testing.T) {
		t.Parallel()
		xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<domain name="empty" version="1.0">
    <extraction>
        <llm enabled="false"/>
    </extraction>
    <confidence/>
</domain>`

		tmpFile, err := os.CreateTemp("", "domain-*.xml")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		defer os.Remove(tmpFile.Name()) //nolint:errcheck

		if _, err := tmpFile.WriteString(xmlContent); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		_ = tmpFile.Close()

		config, err := LoadDomainConfig(tmpFile.Name())
		if err != nil {
			t.Fatalf("LoadDomainConfig failed: %v", err)
		}

		if config.Name != "empty" {
			t.Errorf("expected name 'empty', got '%s'", config.Name)
		}
		if len(config.Entities) != 0 {
			t.Errorf("expected 0 entities, got %d", len(config.Entities))
		}
	})

	t.Run("invalid XML", func(t *testing.T) {
		t.Parallel()
		xmlContent := `<?xml version="1.0" encoding="UTF-8"?>
<domain name="invalid" version="1.0">
    <entity id="test"  <!-- missing closing bracket -->
</domain>`

		tmpFile, err := os.CreateTemp("", "domain-*.xml")
		if err != nil {
			t.Fatalf("failed to create temp file: %v", err)
		}
		defer os.Remove(tmpFile.Name()) //nolint:errcheck

		if _, err := tmpFile.WriteString(xmlContent); err != nil {
			t.Fatalf("failed to write temp file: %v", err)
		}
		_ = tmpFile.Close()

		_, err = LoadDomainConfig(tmpFile.Name())
		if err == nil {
			t.Error("expected error for invalid XML, got nil")
		}
	})

	t.Run("non-existent file", func(t *testing.T) {
		t.Parallel()
		_, err := LoadDomainConfig("/non/existent/path/domain.xml")
		if err == nil {
			t.Error("expected error for non-existent file, got nil")
		}
	})
}

// TestDomainConfigValidate tests the Validate method.
func TestDomainConfigValidate(t *testing.T) {
	t.Parallel()

	t.Run("valid config", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:        "test",
			Version:     "1.0",
			Description: "Test domain",
			Entities: []EntityDef{
				{
					ID:   "product",
					Name: "Product",
					Attributes: []AttributeDef{
						{Name: "title", Type: "string", Required: true},
						{Name: "price", Type: "number", Required: true},
					},
				},
			},
			Relations: []RelationDef{
				{
					Predicate: "has_price",
					Source:    "product",
					Target:    "product",
				},
			},
			Confidence: ConfidencePolicy{
				AutoPublishThreshold: 0.85,
				ReviewThreshold:      0.60,
				RejectThreshold:      0.40,
			},
		}

		err := config.Validate()
		if err != nil {
			t.Errorf("unexpected validation error: %v", err)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Version: "1.0",
		}

		err := config.Validate()
		if err == nil {
			t.Error("expected error for missing name, got nil")
		}
		if err != nil && err.Error() != "domain name is required" {
			t.Errorf("expected 'domain name is required', got '%s'", err.Error())
		}
	})

	t.Run("missing version", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name: "test",
		}

		err := config.Validate()
		if err == nil {
			t.Error("expected error for missing version, got nil")
		}
		if err != nil && err.Error() != "domain version is required" {
			t.Errorf("expected 'domain version is required', got '%s'", err.Error())
		}
	})

	t.Run("duplicate entity Predicate", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:    "test",
			Version: "1.0",
			Entities: []EntityDef{
				{ID: "product", Name: "Product"},
				{ID: "product", Name: "Another Product"},
			},
		}

		err := config.Validate()
		if err == nil {
			t.Error("expected error for duplicate entity Predicate, got nil")
		}
		if err != nil && err.Error() != "duplicate entity Predicate: product" {
			t.Errorf("expected 'duplicate entity Predicate: product', got '%s'", err.Error())
		}
	})

	t.Run("missing entity attribute name", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:    "test",
			Version: "1.0",
			Entities: []EntityDef{
				{
					ID: "product",
					Attributes: []AttributeDef{
						{Name: "", Type: "string"},
					},
				},
			},
		}

		err := config.Validate()
		if err == nil {
			t.Error("expected error for missing attribute name, got nil")
		}
	})

	t.Run("ref attribute without target", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:    "test",
			Version: "1.0",
			Entities: []EntityDef{
				{
					ID: "product",
					Attributes: []AttributeDef{
						{Name: "category", Type: "ref", Required: true},
					},
				},
			},
		}

		err := config.Validate()
		if err == nil {
			t.Error("expected error for ref without target, got nil")
		}
		if err != nil && err.Error() != "attribute category in entity product has type 'ref' but no target specified" {
			t.Errorf("unexpected error: %s", err.Error())
		}
	})

	t.Run("duplicate relation Predicate", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:    "test",
			Version: "1.0",
			Entities: []EntityDef{
				{ID: "product", Name: "Product"},
				{ID: "category", Name: "Category"},
			},
			Relations: []RelationDef{
				{Predicate: "belongs_to", Source: "product", Target: "category"},
				{Predicate: "belongs_to", Source: "product", Target: "category"},
			},
		}

		err := config.Validate()
		if err == nil {
			t.Error("expected error for duplicate relation Predicate, got nil")
		}
	})

	t.Run("relation with missing source", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:    "test",
			Version: "1.0",
			Entities: []EntityDef{
				{ID: "product", Name: "Product"},
			},
			Relations: []RelationDef{
				{Predicate: "test_rel", Target: "product"},
			},
		}

		err := config.Validate()
		if err == nil {
			t.Error("expected error for missing source, got nil")
		}
	})

	t.Run("relation with missing target", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:    "test",
			Version: "1.0",
			Entities: []EntityDef{
				{ID: "product", Name: "Product"},
			},
			Relations: []RelationDef{
				{Predicate: "test_rel", Source: "product"},
			},
		}

		err := config.Validate()
		if err == nil {
			t.Error("expected error for missing target, got nil")
		}
	})

	t.Run("relation with non-existent source entity", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:    "test",
			Version: "1.0",
			Entities: []EntityDef{
				{ID: "product", Name: "Product"},
			},
			Relations: []RelationDef{
				{Predicate: "test_rel", Source: "nonexistent", Target: "product"},
			},
		}

		err := config.Validate()
		if err == nil {
			t.Error("expected error for non-existent source, got nil")
		}
		if err != nil && err.Error() != "relation test_rel references non-existent source entity nonexistent" {
			t.Errorf("unexpected error: %s", err.Error())
		}
	})

	t.Run("invalid auto_publish_threshold", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:    "test",
			Version: "1.0",
			Confidence: ConfidencePolicy{
				AutoPublishThreshold: 1.5,
				ReviewThreshold:      0.60,
				RejectThreshold:      0.40,
			},
		}

		err := config.Validate()
		if err == nil {
			t.Error("expected error for invalid threshold, got nil")
		}
	})

	t.Run("negative threshold", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:    "test",
			Version: "1.0",
			Confidence: ConfidencePolicy{
				AutoPublishThreshold: -0.1,
				ReviewThreshold:      0.60,
				RejectThreshold:      0.40,
			},
		}

		err := config.Validate()
		if err == nil {
			t.Error("expected error for negative threshold, got nil")
		}
	})
}

// TestGetEntityTypes tests the GetEntityTypes method.
func TestGetEntityTypes(t *testing.T) {
	t.Parallel()

	config := &DomainConfig{
		Entities: []EntityDef{
			{ID: "product", Name: "Product"},
			{ID: "category", Name: "Category"},
			{ID: "brand", Name: "Brand"},
		},
	}

	types := config.GetEntityTypes()
	expected := []string{"product", "category", "brand"}

	if len(types) != len(expected) {
		t.Errorf("expected %d entity types, got %d", len(expected), len(types))
	}

	for i, expectedType := range expected {
		if types[i] != expectedType {
			t.Errorf("expected type[%d] '%s', got '%s'", i, expectedType, types[i])
		}
	}
}

// TestDefaultConfidencePolicy tests the DefaultConfidencePolicy method.
func TestDefaultConfidencePolicy(t *testing.T) {
	t.Parallel()

	t.Run("zero thresholds use defaults", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:       "test",
			Version:    "1.0",
			Confidence: ConfidencePolicy{},
		}

		autoPub, review, reject := config.DefaultConfidencePolicy()

		if autoPub != 0.85 {
			t.Errorf("expected autoPublish 0.85, got %f", autoPub)
		}
		if review != 0.60 {
			t.Errorf("expected review 0.60, got %f", review)
		}
		if reject != 0.40 {
			t.Errorf("expected reject 0.40, got %f", reject)
		}
	})

	t.Run("custom thresholds used", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:    "test",
			Version: "1.0",
			Confidence: ConfidencePolicy{
				AutoPublishThreshold: 0.90,
				ReviewThreshold:      0.70,
				RejectThreshold:      0.50,
			},
		}

		autoPub, review, reject := config.DefaultConfidencePolicy()

		if autoPub != 0.90 {
			t.Errorf("expected autoPublish 0.90, got %f", autoPub)
		}
		if review != 0.70 {
			t.Errorf("expected review 0.70, got %f", review)
		}
		if reject != 0.50 {
			t.Errorf("expected reject 0.50, got %f", reject)
		}
	})

	t.Run("partial custom thresholds", func(t *testing.T) {
		t.Parallel()
		config := &DomainConfig{
			Name:    "test",
			Version: "1.0",
			Confidence: ConfidencePolicy{
				AutoPublishThreshold: 0.95,
				// ReviewThreshold and RejectThreshold use defaults
			},
		}

		autoPub, review, reject := config.DefaultConfidencePolicy()

		if autoPub != 0.95 {
			t.Errorf("expected autoPublish 0.95, got %f", autoPub)
		}
		if review != 0.60 {
			t.Errorf("expected review 0.60 (default), got %f", review)
		}
		if reject != 0.40 {
			t.Errorf("expected reject 0.40 (default), got %f", reject)
		}
	})
}
