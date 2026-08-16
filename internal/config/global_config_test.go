package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/config"
)

// writeGlobalXML creates an ontology directory with global.xml and returns the
// ontology directory path for LoadGlobalConfig.
func writeGlobalXML(t *testing.T, content string) (ontologyDir string) {
	t.Helper()
	dir := t.TempDir()
	ontologyDir = filepath.Join(dir, "ontology")
	if err := os.MkdirAll(ontologyDir, 0o755); err != nil {
		t.Fatalf("mkdir ontology: %v", err)
	}
	xmlFile := filepath.Join(ontologyDir, "global.xml")
	if err := os.WriteFile(xmlFile, []byte(content), 0o644); err != nil {
		t.Fatalf("write global.xml: %v", err)
	}
	return ontologyDir
}

func TestCrossDomainLinksConfig(t *testing.T) {
	t.Parallel()

	t.Run("EmptyPath", func(t *testing.T) {
		t.Parallel()
		cfg, err := config.LoadGlobalConfig("")
		if cfg != nil {
			t.Error("expected nil config for empty path")
		}
		if err != nil {
			t.Errorf("expected no error for empty path, got %v", err)
		}
	})

	t.Run("FileNotFound", func(t *testing.T) {
		t.Parallel()
		cfg, err := config.LoadGlobalConfig("/nonexistent/ontology")
		if cfg != nil {
			t.Error("expected nil config for nonexistent directory")
		}
		if err != nil {
			t.Errorf("expected no error for nonexistent directory, got %v", err)
		}
	})

	t.Run("ValidXML", func(t *testing.T) {
		t.Parallel()
		content := `<global>
   <cross-domain-links>
     <method>equals</method>
     <method>expression</method>
     <equals>
       <min-words>3</min-words>
     </equals>
     <llm-confidence-threshold>0.85</llm-confidence-threshold>
     <batch-size>10</batch-size>
     <expressions>
       <expression>
         <name>hr-eng-link</name>
         <priority>10</priority>
         <where>A.domain == 'hr' &amp;&amp; A.name == B.name</where>
         <relation-type>same_entity</relation-type>
       </expression>
     </expressions>
   </cross-domain-links>
 </global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		if cfg == nil || cfg.CrossDomainLinks == nil {
			t.Fatal("expected non-nil CrossDomainLinks")
		}

		cdl := cfg.CrossDomainLinks
		if len(cdl.Methods) != 2 || cdl.Methods[0] != "equals" || cdl.Methods[1] != "expression" {
			t.Errorf("Methods = %v, want [equals expression]", cdl.Methods)
		}
		if cdl.Equals == nil || cdl.Equals.MinWords != 3 {
			t.Errorf("Equals.MinWords = %d, want 3", ptrInt(cdl.Equals))
		}
		if cdl.LLmConfidenceThreshold != 0.85 {
			t.Errorf("LLmConfidenceThreshold = %f, want 0.85", cdl.LLmConfidenceThreshold)
		}
		if cdl.BatchSize != 10 {
			t.Errorf("BatchSize = %d, want 10", cdl.BatchSize)
		}
		if len(cdl.Expressions) != 1 {
			t.Fatalf("expected 1 expression, got %d", len(cdl.Expressions))
		}
		expr := cdl.Expressions[0]
		if expr.Name != "hr-eng-link" {
			t.Errorf("expression name = %s, want hr-eng-link", expr.Name)
		}
	})

	t.Run("DefaultsApplied", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <cross-domain-links>
    <method>equals</method>
    <equals></equals>
  </cross-domain-links>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		cdl := cfg.CrossDomainLinks
		if cdl.Equals.MinWords != config.DefaultEqualsMinWords {
			t.Errorf("Equals.MinWords = %d, want default %d", cdl.Equals.MinWords, config.DefaultEqualsMinWords)
		}
		if cdl.LLmConfidenceThreshold != config.DefaultLLMConfidenceThreshold {
			t.Errorf("LLmConfidenceThreshold = %f, want default %f", cdl.LLmConfidenceThreshold, config.DefaultLLMConfidenceThreshold)
		}
		if cdl.BatchSize != config.DefaultLLMBatchSize {
			t.Errorf("BatchSize = %d, want default %d", cdl.BatchSize, config.DefaultLLMBatchSize)
		}
	})

	t.Run("InvalidMethod", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <cross-domain-links>
    <method>equals</method>
    <method>invalid_method</method>
  </cross-domain-links>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		err = cfg.CrossDomainLinks.Validate()
		if err == nil {
			t.Error("expected validation error for invalid method")
		}
	})

	t.Run("NoMethods", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <cross-domain-links>
    <equals></equals>
  </cross-domain-links>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		err = cfg.CrossDomainLinks.Validate()
		if err == nil {
			t.Error("expected validation error for no methods")
		}
	})

	t.Run("NilBlock", func(t *testing.T) {
		t.Parallel()
		content := `<global></global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		if cfg == nil {
			t.Fatal("expected non-nil GlobalConfig")
		}
		if cfg.CrossDomainLinks != nil {
			t.Error("expected nil CrossDomainLinks when block is absent")
		}
	})

	t.Run("ExpressionDefaults", func(t *testing.T) {
		t.Parallel()
		content := `<global>
   <cross-domain-links>
     <method>expression</method>
     <expressions>
       <expression>
         <name>test-expr</name>
         <priority>1</priority>
         <where>A.domain == 'hr' &amp;&amp; A.name == B.name</where>
       </expression>
     </expressions>
   </cross-domain-links>
 </global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		cdl := cfg.CrossDomainLinks
		if len(cdl.Expressions) != 1 {
			t.Fatalf("expected 1 expression, got %d", len(cdl.Expressions))
		}
		if cdl.Expressions[0].RelationType != config.DefaultRelationType {
			t.Errorf("Expression.RelationType = %q, want default %q", cdl.Expressions[0].RelationType, config.DefaultRelationType)
		}
	})
}

func ptrInt(p *config.EqualsConfig) int {
	if p == nil {
		return -1
	}
	return p.MinWords
}

func TestLoadGlobalConfig_RealFile(t *testing.T) {
	t.Parallel()
	// Resolve data/ontology relative to the repository root.
	// The test is in internal/config/, so walk up two levels to reach repo root.
	repoRoot := filepath.Join("..", "..")
	ontologyDir := filepath.Join(repoRoot, "data", "ontology")

	cfg, err := config.LoadGlobalConfig(ontologyDir)
	if err != nil {
		t.Fatalf("LoadGlobalConfig(%q) error = %v", ontologyDir, err)
	}
	if cfg == nil || cfg.CrossDomainLinks == nil {
		t.Fatal("expected non-nil CrossDomainLinks from real global.xml")
	}

	cdl := cfg.CrossDomainLinks
	if len(cdl.Methods) < 1 {
		t.Errorf("expected at least one method, got %d", len(cdl.Methods))
	}
	if cdl.Equals == nil || cdl.Equals.MinWords != config.DefaultEqualsMinWords {
		t.Errorf("Equals.MinWords = %d, want default %d", ptrInt(cdl.Equals), config.DefaultEqualsMinWords)
	}
	if len(cdl.Expressions) < 1 {
		t.Error("expected at least one expression in real global.xml")
	}

	// Validate should pass for the real file.
	err = cdl.Validate()
	if err != nil {
		t.Fatalf("real global.xml validation failed: %v", err)
	}

	// Verify sources are loaded from real global.xml.
	if len(cfg.Sources) == 0 {
		t.Error("expected at least one source in real global.xml")
	}
	for i, src := range cfg.Sources {
		if src.Path == "" {
			t.Errorf("source %d: path is empty", i+1)
		}
		if src.Type == "" {
			t.Errorf("source %d: type is empty", i+1)
		}
		if !src.Domain.NonEmpty() {
			t.Errorf("source %d: domain should have default value", i+1)
		}
	}
}

func TestSourcesParsing(t *testing.T) {
	t.Parallel()

	t.Run("ValidSources", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <sources>
    <source path="./docs/hr" type="markdown">
      <domains><domain>hr</domain></domains>
    </source>
    <source path="./site/api.example.com" type="webpages" disabled="true">
      <domains><domain>engineering</domain></domains>
    </source>
  </sources>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		if len(cfg.Sources) != 2 {
			t.Fatalf("expected 2 sources, got %d", len(cfg.Sources))
		}

		s0 := cfg.Sources[0]
		if s0.Path != "./docs/hr" {
			t.Errorf("source[0].path = %q, want %q", s0.Path, "./docs/hr")
		}
		if s0.Type != "markdown" {
			t.Errorf("source[0].type = %q, want %q", s0.Type, "markdown")
		}
		if s0.Disabled {
			t.Error("source[0] should not be disabled")
		}
		if len(s0.Domain) != 1 || s0.Domain[0] != "hr" {
			t.Errorf("source[0].domain = %v, want [hr]", s0.Domain)
		}

		s1 := cfg.Sources[1]
		if !s1.Disabled {
			t.Error("source[1] should be disabled")
		}
	})

	t.Run("DefaultDomainApplied", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <sources>
    <source path="./docs" type="markdown"/>
  </sources>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		if len(cfg.Sources) != 1 {
			t.Fatalf("expected 1 source, got %d", len(cfg.Sources))
		}
		if !cfg.Sources[0].Domain.NonEmpty() || cfg.Sources[0].Domain[0] != "default" {
			t.Errorf("source domain = %v, want [default]", cfg.Sources[0].Domain)
		}
	})

	t.Run("MissingPathError", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <sources>
    <source type="markdown"/>
  </sources>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		_, err := config.LoadGlobalConfig(ontologyDir)
		if err == nil {
			t.Error("expected error for missing path")
		}
	})

	t.Run("MissingTypeError", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <sources>
    <source path="./docs"/>
  </sources>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		_, err := config.LoadGlobalConfig(ontologyDir)
		if err == nil {
			t.Error("expected error for missing type")
		}
	})

	t.Run("MultipleDomains", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <sources>
    <source path="./wiki" type="mediawiki">
      <domains><domain>hr</domain><domain>engineering</domain><domain>product</domain></domains>
    </source>
  </sources>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		domains := cfg.Sources[0].Domain
		if len(domains) != 3 {
			t.Fatalf("expected 3 domains, got %d: %v", len(domains), domains)
		}
		if domains[0] != "hr" || domains[1] != "engineering" || domains[2] != "product" {
			t.Errorf("domains = %v, want [hr engineering product]", domains)
		}
	})

	t.Run("SpaceAndDataset", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <sources>
    <source path="./wiki" type="mediawiki" space="Main"/>
    <source path="./data/raw" type="unstructured" dataset="my_dataset"/>
  </sources>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		if cfg.Sources[0].Space != "Main" {
			t.Errorf("source[0].space = %q, want %q", cfg.Sources[0].Space, "Main")
		}
		if cfg.Sources[1].Dataset != "my_dataset" {
			t.Errorf("source[1].dataset = %q, want %q", cfg.Sources[1].Dataset, "my_dataset")
		}
	})
}

func TestLoadGlobalConfig_SourcesFromTestdata(t *testing.T) {
	t.Parallel()
	// Use the testdata/global.xml fixture to verify real file parsing.
	repoRoot := filepath.Join("..", "..")
	testdataDir := filepath.Join(repoRoot, "internal", "ingestion", "runner", "testdata")

	cfg, err := config.LoadGlobalConfig(testdataDir)
	if err != nil {
		t.Fatalf("LoadGlobalConfig(%q) error = %v", testdataDir, err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil GlobalConfig from testdata/global.xml")
	}
	if len(cfg.Sources) != 2 {
		t.Fatalf("expected 2 sources in testdata/global.xml, got %d", len(cfg.Sources))
	}

	s0 := cfg.Sources[0]
	if s0.Path != "./testdata/srcA" {
		t.Errorf("source[0].path = %q, want %q", s0.Path, "./testdata/srcA")
	}
	if s0.Type != "markdown" {
		t.Errorf("source[0].type = %q, want %q", s0.Type, "markdown")
	}

	s1 := cfg.Sources[1]
	if s1.Path != "./testdata/srcB" {
		t.Errorf("source[1].path = %q, want %q", s1.Path, "./testdata/srcB")
	}
	if s1.Type != "webpages" {
		t.Errorf("source[1].type = %q, want %q", s1.Type, "webpages")
	}
}

func TestExpressionParsing(t *testing.T) {
	t.Parallel()

	t.Run("ExpressionsParsed", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <cross-domain-links>
    <method>equals</method>
    <method>expression</method>
    <expressions>
      <expression>
        <name>hr_to_policy_link</name>
        <description>Link HR employees to policy owners by name</description>
        <priority>10</priority>
        <where>A.domain == 'hr' &amp;&amp; A.name == 'Jane Smith' &amp;&amp; B.domain == 'policy' &amp;&amp; B.name == 'Jane Smith'</where>
        <relation-type>same_entity</relation-type>
      </expression>
    </expressions>
  </cross-domain-links>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		cdl := cfg.CrossDomainLinks
		if len(cdl.Expressions) != 1 {
			t.Fatalf("expected 1 expression, got %d", len(cdl.Expressions))
		}
		expr := cdl.Expressions[0]
		if expr.Name != "hr_to_policy_link" {
			t.Errorf("Name = %q, want %q", expr.Name, "hr_to_policy_link")
		}
		if expr.Description != "Link HR employees to policy owners by name" {
			t.Errorf("Description = %q, want description set", expr.Description)
		}
		if expr.Priority != 10 {
			t.Errorf("Priority = %d, want 10", expr.Priority)
		}
		if expr.Where == "" {
			t.Error("Where should not be empty")
		}
		if expr.RelationType != "same_entity" {
			t.Errorf("RelationType = %q, want %q", expr.RelationType, "same_entity")
		}
	})

	t.Run("ExpressionDefaultRelationType", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <cross-domain-links>
    <method>expression</method>
    <expressions>
      <expression>
        <name>test_expr</name>
        <priority>1</priority>
        <where>A.domain == 'hr' &amp;&amp; B.domain == 'policy'</where>
      </expression>
    </expressions>
  </cross-domain-links>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		cdl := cfg.CrossDomainLinks
		if cdl.Expressions[0].RelationType != config.DefaultRelationType {
			t.Errorf("RelationType = %q, want default %q", cdl.Expressions[0].RelationType, config.DefaultRelationType)
		}
	})

	t.Run("ExpressionMethodValid", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <cross-domain-links>
    <method>expression</method>
  </cross-domain-links>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		err = cfg.CrossDomainLinks.Validate()
		if err != nil {
			t.Errorf("expression method should be valid, got error: %v", err)
		}
	})

	t.Run("MultipleExpressions", func(t *testing.T) {
		t.Parallel()
		content := `<global>
   <cross-domain-links>
     <method>equals</method>
     <method>expression</method>
     <expressions>
       <expression>
         <name>hr-eng-link</name>
         <priority>10</priority>
         <where>A.domain == 'hr' &amp;&amp; A.name == B.name</where>
         <relation-type>same_entity</relation-type>
       </expression>
       <expression>
         <name>hr-product-link</name>
         <priority>5</priority>
         <where>A.domain == 'hr' &amp;&amp; A.type == B.type</where>
         <relation-type>related_to</relation-type>
       </expression>
     </expressions>
   </cross-domain-links>
 </global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		cdl := cfg.CrossDomainLinks
		if len(cdl.Expressions) != 2 {
			t.Fatalf("expected 2 expressions, got %d", len(cdl.Expressions))
		}
		if cdl.Expressions[0].Name != "hr-eng-link" {
			t.Errorf("expr[0] Name = %q, want %q", cdl.Expressions[0].Name, "hr-eng-link")
		}
		if cdl.Expressions[0].Priority != 10 {
			t.Errorf("expr[0] Priority = %d, want 10", cdl.Expressions[0].Priority)
		}
		if cdl.Expressions[1].Name != "hr-product-link" {
			t.Errorf("expr[1] Name = %q, want %q", cdl.Expressions[1].Name, "hr-product-link")
		}
		if cdl.Expressions[1].Priority != 5 {
			t.Errorf("expr[1] Priority = %d, want 5", cdl.Expressions[1].Priority)
		}
		if cdl.Expressions[1].RelationType != "related_to" {
			t.Errorf("expr[1] RelationType = %q, want %q", cdl.Expressions[1].RelationType, "related_to")
		}
	})
}

func TestNERConfig(t *testing.T) {
	t.Parallel()

	t.Run("ValidMethods", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <ner>
    <method>regex</method>
    <method>llm</method>
  </ner>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		if cfg.NER == nil {
			t.Fatal("expected non-nil NER config")
		}
		if len(cfg.NER.Methods) != 2 || cfg.NER.Methods[0] != "regex" || cfg.NER.Methods[1] != "llm" {
			t.Errorf("Methods = %v, want [regex llm]", cfg.NER.Methods)
		}
	})

	t.Run("AllValidStages", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <ner>
    <method>regex</method>
    <method>prose</method>
    <method>llm</method>
  </ner>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		if len(cfg.NER.Methods) != 3 {
			t.Errorf("Methods = %v, want [regex prose llm]", cfg.NER.Methods)
		}
	})

	t.Run("InvalidMethod", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <ner>
    <method>regex</method>
    <method>invalid_method</method>
  </ner>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		_, err := config.LoadGlobalConfig(ontologyDir)
		if err == nil {
			t.Error("expected validation error for invalid NER method")
		}
	})

	t.Run("FallbackWhenAbsent", func(t *testing.T) {
		t.Parallel()
		content := `<global></global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		if cfg.NER == nil {
			t.Fatal("expected non-nil NER config (fallback)")
		}
		if len(cfg.NER.Methods) != 2 || cfg.NER.Methods[0] != "regex" || cfg.NER.Methods[1] != "llm" {
			t.Errorf("Methods = %v, want default [regex llm]", cfg.NER.Methods)
		}
	})

	t.Run("EmptyMethodsBlock", func(t *testing.T) {
		t.Parallel()
		content := `<global>
   <ner></ner>
 </global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		// Empty <ner> block should get defaults applied by ApplyDefaults.
		if cfg.NER == nil || len(cfg.NER.Methods) != 2 {
			t.Errorf("expected default methods [regex llm], got %v", cfg.NER)
		}
	})

	t.Run("NERWithCrossDomainLinks", func(t *testing.T) {
		t.Parallel()
		content := `<global>
  <cross-domain-links>
    <method>equals</method>
  </cross-domain-links>
  <ner>
    <method>regex</method>
  </ner>
</global>`
		ontologyDir := writeGlobalXML(t, content)

		cfg, err := config.LoadGlobalConfig(ontologyDir)
		if err != nil {
			t.Fatalf("LoadGlobalConfig() error = %v", err)
		}
		if cfg.CrossDomainLinks == nil || len(cfg.CrossDomainLinks.Methods) != 1 {
			t.Error("expected cross-domain-links to be parsed")
		}
		if cfg.NER == nil || len(cfg.NER.Methods) != 1 || cfg.NER.Methods[0] != "regex" {
			t.Errorf("NER = %v, want [regex]", cfg.NER)
		}
	})

	t.Run("ValidateNil", func(t *testing.T) {
		t.Parallel()
		var ncfg *config.GlobalNERConfig
		err := ncfg.Validate()
		if err != nil {
			t.Errorf("expected no error for nil NER config, got %v", err)
		}
	})

	t.Run("ValidateEmptyMethods", func(t *testing.T) {
		t.Parallel()
		ncfg := &config.GlobalNERConfig{Methods: []string{}}
		err := ncfg.Validate()
		if err == nil {
			t.Error("expected validation error for empty methods")
		}
	})
}
