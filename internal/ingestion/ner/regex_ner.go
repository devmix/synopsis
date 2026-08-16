// Package ner provides named entity recognition providers for extracting entities from text.
package ner

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/devmix/synopsis/internal/domain"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/utils"
)

type preparedRule struct {
	entityType string
	domain     string
	def        domain.RegexRuleDef
}

type RegexNER struct {
	rules []preparedRule
	log   *logger.Logger // structured logger; required
}

func NewRegexNER(domainConfigs []*domain.DomainConfig, log *logger.Logger) (*RegexNER, error) {
	var rules []preparedRule

	for _, dc := range domainConfigs {
		if dc == nil {
			continue
		}
		regexRules := dc.Extraction.RegexRules
		for i := 0; i < len(regexRules); i++ {
			rule := regexRules[i]
			if rule.Compiled == nil {
				compiled, err := regexp.Compile(rule.Pattern)
				if err != nil {
					return nil, fmt.Errorf("regex ner: compile pattern %q: %w", rule.Pattern, err)
				}
				rule.Compiled = compiled
			}
			rules = append(rules, preparedRule{
				def:        rule,
				entityType: strings.ToLower(rule.Entity),
				domain:     utils.Normalize(dc.Name),
			})
		}
	}

	return &RegexNER{rules: rules, log: log}, nil
}

func (r *RegexNER) Name() string {
	return "regex"
}

func (r *RegexNER) ExtractEntities(ctx context.Context, content string, metadata map[string]interface{}) (*Result, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("regex ner: context cancelled: %w", ctx.Err())
	}

	if strings.TrimSpace(content) == "" || len(r.rules) == 0 {
		return nil, nil
	}

	seen := make(map[string]bool) // deduplicate by name+type+domain within regex results
	var entities []Entity

	for _, rule := range r.rules {
		matches := rule.def.Compiled.FindAllStringSubmatch(content, -1)
		if matches == nil {
			continue
		}

		hasCaptureGroup := rule.def.Compiled.NumSubexp() > 0

		for _, match := range matches {
			// Use first capture group if present, otherwise fall back to full match.
			entityName := strings.TrimSpace(match[0])
			if hasCaptureGroup && len(match) > 1 {
				entityName = strings.TrimSpace(match[1])
			}
			if entityName == "" {
				continue
			}

			key := entityName + "|" + rule.entityType + "|" + rule.domain
			if seen[key] {
				continue
			}
			seen[key] = true

			ent := Entity{
				Name:       entityName,
				Type:       rule.entityType,
				Domain:     rule.domain,
				Confidence: rule.def.Confidence,
				Metadata:   make(map[string]interface{}),
			}
			ent.Metadata["rule_id"] = rule.def.ID

			entities = append(entities, ent)
		}
	}

	if len(entities) == 0 {
		return nil, nil
	}

	return &Result{Entities: entities}, nil
}
