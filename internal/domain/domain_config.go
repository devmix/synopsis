package domain

import (
	"encoding/xml"
	"fmt"
	"os"
	"regexp"
)

// DomainConfig is the top-level domain configuration.
type DomainConfig struct {
	XMLName     xml.Name         `xml:"domain"`
	Name        string           `xml:"name,attr"`
	Version     string           `xml:"version,attr"`
	Description string           `xml:"description,attr"`
	Entities    []EntityDef      `xml:"entity"`
	Relations   []RelationDef    `xml:"relation"`
	Extraction  ExtractionDef    `xml:"extraction"`
	Confidence  ConfidencePolicy `xml:"confidence"`
}

// EntityDef defines an entity type for the domain.
type EntityDef struct {
	ID          string         `xml:"id,attr"`
	Name        string         `xml:"name,attr"`
	Description string         `xml:"description,attr"`
	Attributes  []AttributeDef `xml:"attribute"`
	Synonyms    []string       `xml:"synonym"`
}

// AttributeDef defines an attribute of an entity.
type AttributeDef struct {
	Name     string `xml:"name,attr"`
	Type     string `xml:"type,attr"` // string, date, number, ref
	Required bool   `xml:"required,attr"`
	Target   string `xml:"target,attr"` // for type=ref: target entity type
}

// RelationDef defines a relation type for the domain.
type RelationDef struct {
	Source      string       `xml:"source,attr"`
	Predicate   string       `xml:"predicate,attr"`
	Target      string       `xml:"target,attr"` // entity type
	Description string       `xml:"description,attr"`
	Attributes  []RelAttrDef `xml:"attribute"`
}

// RelAttrDef defines an attribute of a relation.
type RelAttrDef struct {
	Name string `xml:"name,attr"`
	Type string `xml:"type,attr"` // string, date, number, condition
}

// ExtractionDef defines how to extract entities and relations.
type ExtractionDef struct {
	RegexRules []RegexRuleDef `xml:"regex"`
}

// RegexRuleDef defines a regex-based extraction rule.
type RegexRuleDef struct {
	ID         string         `xml:"id,attr"`
	Entity     string         `xml:"entity,attr"`
	Pattern    string         `xml:"pattern,attr"`
	Confidence float64        `xml:"confidence,attr"`
	Compiled   *regexp.Regexp `xml:"-"`
}

// ConfidencePolicy defines thresholds for auto-publish / review / reject.
type ConfidencePolicy struct {
	AutoPublishThreshold float64 `xml:"auto_publish_threshold,attr"` // default 0.85
	ReviewThreshold      float64 `xml:"review_threshold,attr"`       // default 0.60
	RejectThreshold      float64 `xml:"reject_threshold,attr"`       // default 0.40
}

// LoadDomainConfig reads and parses a domain XML configuration file.
func LoadDomainConfig(path string) (*DomainConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read domain config file: %w", err)
	}

	var config DomainConfig
	if err := xml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse domain config XML: %w", err)
	}

	return &config, nil
}

// Validate checks that the domain config is well-formed.
func (c *DomainConfig) Validate() error {
	if c.Name == "" {
		return fmt.Errorf("domain name is required")
	}

	if c.Version == "" {
		return fmt.Errorf("domain version is required")
	}

	// Validate entities
	entityIDs := make(map[string]bool)
	for _, entity := range c.Entities {
		if entity.ID == "" {
			return fmt.Errorf("entity Predicate is required")
		}
		if entityIDs[entity.ID] {
			return fmt.Errorf("duplicate entity Predicate: %s", entity.ID)
		}
		entityIDs[entity.ID] = true

		// Validate entity attributes
		attrNames := make(map[string]bool)
		for _, attr := range entity.Attributes {
			if attr.Name == "" {
				return fmt.Errorf("attribute name is required for entity %s", entity.ID)
			}
			if attrNames[attr.Name] {
				return fmt.Errorf("duplicate attribute name %s in entity %s", attr.Name, entity.ID)
			}
			attrNames[attr.Name] = true

			// Validate reference type
			if attr.Type == "ref" && attr.Target == "" {
				return fmt.Errorf("attribute %s in entity %s has type 'ref' but no target specified", attr.Name, entity.ID)
			}
		}
	}

	// Validate relations
	relationIDs := make(map[string]bool)
	for _, relation := range c.Relations {
		if relation.Predicate == "" {
			return fmt.Errorf("relation name is required")
		}
		if relationIDs[relation.Predicate] {
			return fmt.Errorf("duplicate relation Predicate: %s", relation.Predicate)
		}
		relationIDs[relation.Predicate] = true

		if relation.Source == "" {
			return fmt.Errorf("relation %s has no source entity specified", relation.Predicate)
		}
		if relation.Target == "" {
			return fmt.Errorf("relation %s has no target entity specified", relation.Predicate)
		}

		// Check that source and target entities exist
		if !entityIDs[relation.Source] {
			return fmt.Errorf("relation %s references non-existent source entity %s", relation.Predicate, relation.Source)
		}
		if !entityIDs[relation.Target] {
			return fmt.Errorf("relation %s references non-existent target entity %s", relation.Predicate, relation.Target)
		}

		// Validate relation attributes
		attrNames := make(map[string]bool)
		for _, attr := range relation.Attributes {
			if attr.Name == "" {
				return fmt.Errorf("attribute name is required for relation %s", relation.Predicate)
			}
			if attrNames[attr.Name] {
				return fmt.Errorf("duplicate attribute name %s in relation %s", attr.Name, relation.Predicate)
			}
			attrNames[attr.Name] = true
		}
	}

	// ValidateAndCompile regex rules
	regexRules := c.Extraction.RegexRules
	if regexRules != nil {
		for i := 0; i < len(regexRules); i++ {
			if err := regexRules[i].ValidateAndCompile(); err != nil {
				return err
			}
		}
	}

	// Validate confidence thresholds
	if c.Confidence.AutoPublishThreshold < 0 || c.Confidence.AutoPublishThreshold > 1 {
		return fmt.Errorf("auto_publish_threshold must be between 0 and 1")
	}
	if c.Confidence.ReviewThreshold < 0 || c.Confidence.ReviewThreshold > 1 {
		return fmt.Errorf("review_threshold must be between 0 and 1")
	}
	if c.Confidence.RejectThreshold < 0 || c.Confidence.RejectThreshold > 1 {
		return fmt.Errorf("reject_threshold must be between 0 and 1")
	}

	return nil
}

// GetEntityTypes returns all entity type IDs.
func (c *DomainConfig) GetEntityTypes() []string {
	result := make([]string, len(c.Entities))
	for i, entity := range c.Entities {
		result[i] = entity.ID
	}
	return result
}

// GetRelationTypes returns all relation type IDs.
func (c *DomainConfig) GetRelationTypes() []string {
	result := make([]string, len(c.Relations))
	for i, relation := range c.Relations {
		result[i] = relation.Predicate
	}
	return result
}

// DefaultConfidencePolicy returns the confidence thresholds, using defaults for zero values.
func (c *DomainConfig) DefaultConfidencePolicy() (autoPublish, review, reject float64) {
	// Default values
	const (
		defaultAutoPublish = 0.85
		defaultReview      = 0.60
		defaultReject      = 0.40
	)

	if c.Confidence.AutoPublishThreshold == 0 {
		autoPublish = defaultAutoPublish
	} else {
		autoPublish = c.Confidence.AutoPublishThreshold
	}

	if c.Confidence.ReviewThreshold == 0 {
		review = defaultReview
	} else {
		review = c.Confidence.ReviewThreshold
	}

	if c.Confidence.RejectThreshold == 0 {
		reject = defaultReject
	} else {
		reject = c.Confidence.RejectThreshold
	}

	return autoPublish, review, reject
}

// GetEntityByID returns the EntityDef for a given entity Predicate, or nil if not found.
func (c *DomainConfig) GetEntityByID(id string) *EntityDef {
	for _, entity := range c.Entities {
		if entity.ID == id {
			return &entity
		}
	}
	return nil
}

// GetRelationByID returns the RelationDef for a given relation Predicate, or nil if not found.
func (c *DomainConfig) GetRelationByID(id string) *RelationDef {
	for _, relation := range c.Relations {
		if relation.Predicate == id {
			return &relation
		}
	}
	return nil
}

func (r *RegexRuleDef) ValidateAndCompile() error {
	if r.ID == "" {
		return fmt.Errorf("regex rule has empty ID")
	}
	if r.Pattern == "" {
		return fmt.Errorf("regex rule %q has empty pattern", r.ID)
	}
	if r.Confidence < 0 || r.Confidence > 1 {
		return fmt.Errorf("regex rule %q confidence %f must be in [0, 1]", r.ID, r.Confidence)
	}
	if r.Entity == "" {
		return fmt.Errorf("regex rule %q entity name is empty", r.ID)
	}
	r.Compiled = regexp.MustCompile(r.Pattern)
	return nil
}
