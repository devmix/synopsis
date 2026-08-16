// Package config provides loading and validation of application configuration from YAML files.
package config

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
)

// Constants for cross-domain link configuration defaults.

const (
	DefaultEqualsMinWords         = 2
	DefaultLLMConfidenceThreshold = 0.7
	DefaultLLMBatchSize           = 5
	DefaultRelationType           = "same_entity"
)

// ValidMethods lists the allowed cross-domain linking methods.
var ValidMethods = []string{"expression", "equals", "llm"}

// ValidNERMethods lists the allowed NER extraction stages.
var ValidNERMethods = []string{"regex", "prose", "llm"}

// DefaultNERMethods is the fallback when <ner> block is absent from global.xml.
var DefaultNERMethods = []string{"regex", "llm"}

// GlobalConfig represents the top-level global configuration element.
type GlobalConfig struct {
	XMLName          xml.Name                `xml:"global"`
	Sources          []SourceConfig          `xml:"sources>source"`
	CrossDomainLinks *CrossDomainLinksConfig `xml:"cross-domain-links"`
	NER              *GlobalNERConfig        `xml:"ner"`
}

// GlobalNERConfig holds NER extraction method configuration from global.xml.
type GlobalNERConfig struct {
	XMLName xml.Name `xml:"ner"`
	Methods []string `xml:"method"`
}

// UnmarshalXML implements custom XML unmarshaling for SourceConfig.
// It reads attributes (path, type, disabled, space, dataset) and nested <domains><domain>...</domain></domains>.
func (s *SourceConfig) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var xs xmlSourceElement
	if err := d.DecodeElement(&xs, &start); err != nil {
		return err
	}
	*s = xmlSourceToConfig(xs)
	return nil
}

// CrossDomainLinksConfig holds cross-domain linking settings.
type CrossDomainLinksConfig struct {
	XMLName                xml.Name         `xml:"cross-domain-links"`
	Methods                []string         `xml:"method"`
	Equals                 *EqualsConfig    `xml:"equals"`
	LLmConfidenceThreshold float64          `xml:"llm-confidence-threshold"`
	BatchSize              int              `xml:"batch-size"`
	Expressions            []LinkExpression `xml:"expressions>expression"`
}

// EqualsConfig configures the equals linking method.
type EqualsConfig struct {
	MinWords int `xml:"min-words"`
}

// LinkExpression defines a CEL-based cross-domain entity linking expression.
type LinkExpression struct {
	Name         string `xml:"name"`
	Description  string `xml:"description,omitempty"`
	Priority     int    `xml:"priority"`
	Where        string `xml:"where"`
	RelationType string `xml:"relation-type"`
}

// LoadGlobalConfig loads and parses a global XML configuration file from the
// ontology directory. The path parameter is the ontology directory (e.g. "data/ontology"),
// not the file itself; it reads filepath.Join(ontologyDir, "global.xml").
// Returns nil GlobalConfig (with nil CrossDomainLinks) if the file does not exist.
func LoadGlobalConfig(ontologyDir string) (*GlobalConfig, error) {
	if ontologyDir == "" {
		return nil, nil
	}
	path := filepath.Join(ontologyDir, "global.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read global config %s: %w", path, err)
	}
	var cfg GlobalConfig
	if err := xml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse global config %s: %w", path, err)
	}
	if cfg.CrossDomainLinks != nil {
		cfg.CrossDomainLinks.ApplyDefaults()
	}

	// Apply NER defaults and validate.
	if cfg.NER != nil {
		cfg.NER.ApplyDefaults()
		if err := cfg.NER.Validate(); err != nil {
			return nil, fmt.Errorf("validate ner config: %w", err)
		}
	} else {
		// Fallback when <ner> block is absent.
		cfg.NER = &GlobalNERConfig{Methods: DefaultNERMethods}
	}

	// Validate sources and apply default domain.
	for i := range cfg.Sources {
		src := &cfg.Sources[i]
		if src.Path == "" {
			return nil, fmt.Errorf("source %d: path is required", i+1)
		}
		if src.Type == "" {
			return nil, fmt.Errorf("source %d: type is required", i+1)
		}
		if !src.Domain.NonEmpty() {
			src.Domain = Domains{"default"}
		}
	}

	return &cfg, nil
}

// ApplyDefaults fills zero-value fields with sensible defaults.
func (c *CrossDomainLinksConfig) ApplyDefaults() {
	if c.Equals != nil && c.Equals.MinWords <= 0 {
		c.Equals.MinWords = DefaultEqualsMinWords
	}
	if c.LLmConfidenceThreshold <= 0 {
		c.LLmConfidenceThreshold = DefaultLLMConfidenceThreshold
	}
	if c.BatchSize <= 0 {
		c.BatchSize = DefaultLLMBatchSize
	}
	for i := range c.Expressions {
		if c.Expressions[i].RelationType == "" {
			c.Expressions[i].RelationType = DefaultRelationType
		}
	}
}

// Validate checks that the configuration is valid.
func (c *CrossDomainLinksConfig) Validate() error {
	if c == nil {
		return nil
	}
	for _, m := range c.Methods {
		valid := false
		for _, vm := range ValidMethods {
			if m == vm {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid cross-domain link method %q, want one of %v", m, ValidMethods)
		}
	}
	if len(c.Methods) == 0 {
		return fmt.Errorf("cross-domain-links.methods must have at least one method")
	}
	return nil
}

// ApplyDefaults fills zero-value fields with sensible defaults.
func (n *GlobalNERConfig) ApplyDefaults() {
	if n != nil && len(n.Methods) == 0 {
		n.Methods = DefaultNERMethods
	}
}

// Validate checks that all NER methods are valid and at least one is present.
func (n *GlobalNERConfig) Validate() error {
	if n == nil {
		return nil
	}
	for _, m := range n.Methods {
		valid := false
		for _, vm := range ValidNERMethods {
			if m == vm {
				valid = true
				break
			}
		}
		if !valid {
			return fmt.Errorf("invalid NER method %q, want one of %v", m, ValidNERMethods)
		}
	}
	if len(n.Methods) == 0 {
		return fmt.Errorf("ner.methods must have at least one method")
	}
	return nil
}
