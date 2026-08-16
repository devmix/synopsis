package domain

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
)

// GlobalXml holds schema definitions from global.xml that apply to every
// domain: entities and relations defined once in <entities>/<relations>
// blocks and merged into each domain configuration at load time.
type GlobalXml struct {
	XMLName    xml.Name      `xml:"global"`
	Entities   []EntityDef   `xml:"entities>entity"`    // global entity types
	Relations  []RelationDef `xml:"relations>relation"` // global relation types
	Extraction ExtractionDef `xml:"extraction"`
}

// LoadGlobalPool reads the global entity and relation pool from global.xml
// inside ontologyDir. A missing file or absent blocks yield an empty pool
// without an error.
func LoadGlobalPool(ontologyDir string) (*GlobalXml, error) {
	path := filepath.Join(ontologyDir, "global.xml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &GlobalXml{}, nil
		}
		return nil, fmt.Errorf("read global pool %s: %w", path, err)
	}

	var parsed GlobalXml
	if err := xml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse global pool %s: %w", path, err)
	}

	return &parsed, nil
}

// Validate checks the pool in isolation: unique definition IDs, valid
// attributes (unique names, ref targets present), and relations whose
// source/target entities exist within the pooled entity set.
func (p *GlobalXml) Validate() error {
	entityIDs := make(map[string]bool)
	for _, entity := range p.Entities {
		if entity.ID == "" {
			return fmt.Errorf("global entity Predicate is required")
		}
		if entityIDs[entity.ID] {
			return fmt.Errorf("duplicate global entity Predicate: %s", entity.ID)
		}
		entityIDs[entity.ID] = true

		attrNames := make(map[string]bool)
		for _, attr := range entity.Attributes {
			if attr.Name == "" {
				return fmt.Errorf("attribute name is required for global entity %s", entity.ID)
			}
			if attrNames[attr.Name] {
				return fmt.Errorf("duplicate attribute name %s in global entity %s", attr.Name, entity.ID)
			}
			attrNames[attr.Name] = true

			if attr.Type == "ref" && attr.Target == "" {
				return fmt.Errorf("attribute %s in global entity %s has type 'ref' but no target specified", attr.Name, entity.ID)
			}
		}
	}

	relationIDs := make(map[string]bool)
	for _, relation := range p.Relations {
		if relation.Predicate == "" {
			return fmt.Errorf("global relation Predicate is required")
		}
		if relationIDs[relation.Predicate] {
			return fmt.Errorf("duplicate global relation Predicate: %s", relation.Predicate)
		}
		relationIDs[relation.Predicate] = true

		if !entityIDs[relation.Source] {
			return fmt.Errorf("global relation %s references non-existent source entity %s", relation.Predicate, relation.Source)
		}
		if !entityIDs[relation.Target] {
			return fmt.Errorf("global relation %s references non-existent target entity %s", relation.Predicate, relation.Target)
		}
	}

	// Validate regex rules
	regexRules := p.Extraction.RegexRules
	if regexRules != nil {
		for i := 0; i < len(regexRules); i++ {
			if err := regexRules[i].ValidateAndCompile(); err != nil {
				return err
			}
		}
	}

	return nil
}

// MergeInto merges the pool into cfg in place. Domain definitions win: a
// global definition whose Predicate already exists in the domain is skipped and
// reported as a warning string. Returns all override warnings.
func (p *GlobalXml) MergeInto(cfg *DomainConfig) []string {
	var warnings []string

	existingEntities := make(map[string]bool, len(cfg.Entities))
	for _, e := range cfg.Entities {
		existingEntities[e.ID] = true
	}
	for _, ge := range p.Entities {
		if existingEntities[ge.ID] {
			warnings = append(warnings, fmt.Sprintf("domain %q overrides global entity %q", cfg.Name, ge.ID))
			continue
		}
		cfg.Entities = append(cfg.Entities, ge)
		existingEntities[ge.ID] = true
	}

	existingRelations := make(map[string]bool, len(cfg.Relations))
	for _, r := range cfg.Relations {
		existingRelations[r.Predicate] = true
	}
	for _, gr := range p.Relations {
		if existingRelations[gr.Predicate] {
			warnings = append(warnings, fmt.Sprintf("domain %q overrides global relation %q", cfg.Name, gr.Predicate))
			continue
		}
		cfg.Relations = append(cfg.Relations, gr)
		existingRelations[gr.Predicate] = true
	}

	existingExtractionRules := make(map[string]bool, len(cfg.Extraction.RegexRules))
	for _, r := range cfg.Extraction.RegexRules {
		existingExtractionRules[r.ID] = true
	}
	for _, gr := range p.Extraction.RegexRules {
		if existingExtractionRules[gr.ID] {
			warnings = append(warnings, fmt.Sprintf("domain %q overrides global extraction regexp's %q", cfg.Name, gr.ID))
			continue
		}
		cfg.Extraction.RegexRules = append(cfg.Extraction.RegexRules, gr)
		existingExtractionRules[gr.ID] = true
	}

	return warnings
}
