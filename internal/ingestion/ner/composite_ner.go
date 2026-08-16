package ner

import (
	"context"
	"fmt"
	"slices"

	"github.com/devmix/synopsis/internal/domain"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/prompts"
)

type CompositeNER struct {
	providers     []Provider
	domainConfigs []*domain.DomainConfig
	log           *logger.Logger // structured logger; required
}

func NewCompositeNER(providers []Provider, domainConfigs []*domain.DomainConfig, log *logger.Logger) *CompositeNER {
	return &CompositeNER{
		providers:     providers,
		domainConfigs: domainConfigs,
		log:           log,
	}
}

func (c *CompositeNER) Name() string {
	return "composite"
}

func (c *CompositeNER) ExtractEntities(ctx context.Context, content string, metadata map[string]interface{}) (*Result, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("composite ner: context cancelled: %w", ctx.Err())
	}
	if len(c.providers) == 0 {
		return nil, nil
	}

	result := Result{
		Entities: make([]Entity, 0),
		Facts:    make([]Fact, 0),
	}

	for _, provider := range c.providers {
		c.log.Debug("running NER stage", "provider", provider.Name())
		response, err := provider.ExtractEntities(ctx, content, metadata)
		if err != nil {
			return nil, err
		}

		if response != nil {
			c.enrichMetadata(provider, response, metadata)
			result.Entities = append(result.Entities, response.Entities...)
			result.Facts = append(result.Facts, response.Facts...)
		}
	}

	result = c.filterByAutoPublishThreshold(result)

	c.log.Debug("NER extraction complete", "entities", len(result.Entities), "facts", len(result.Facts))

	return &result, nil
}

// filterByConfidence removes entities whose confidence is below the threshold.
func filterByConfidence(entities []Entity, threshold float64) []Entity {
	filtered := make([]Entity, 0, len(entities))
	for _, e := range entities {
		if e.Confidence >= threshold {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// filterByAutoPublishThreshold filters entities below the AutoPublishThreshold
// per domain, then removes facts referencing filtered-out entities.
func (c *CompositeNER) filterByAutoPublishThreshold(result Result) Result {
	if len(c.domainConfigs) == 0 {
		return result
	}

	thresholdByDomain := make(map[string]float64)
	for _, dc := range c.domainConfigs {
		autoPublish, _, _ := dc.DefaultConfidencePolicy()
		thresholdByDomain[dc.Name] = autoPublish
	}

	var entities []Entity
	for _, e := range result.Entities {
		if threshold, ok := thresholdByDomain[e.Domain]; ok {
			if e.Confidence >= threshold {
				entities = append(entities, e)
			}
		} else {
			entities = append(entities, e)
		}
	}

	entitySet := make(map[string]bool, len(entities))
	for _, e := range entities {
		entitySet[e.Name+"|"+e.Type] = true
	}

	var facts []Fact
	for _, f := range result.Facts {
		subjectKey := f.SubjectName + "|" + f.SubjectType
		objectKey := f.ObjectName + "|" + f.ObjectType
		if entitySet[subjectKey] && entitySet[objectKey] {
			facts = append(facts, f)
		}
	}

	return Result{
		Entities: entities,
		Facts:    facts,
	}
}

// enrichMetadata copies source metadata into each entity and fact.
// Domain fields set by individual providers are preserved (not overwritten).
func (c *CompositeNER) enrichMetadata(provider Provider, response *Result, metadata map[string]interface{}) {
	entities := response.Entities
	for j := range entities {
		entity := &entities[j]
		if entity.Metadata == nil {
			entity.Metadata = make(map[string]interface{})
		}
		for k, v := range metadata {
			entity.Metadata[k] = v
		}
		if _, exists := entity.Metadata["provider"]; !exists {
			entity.Metadata["provider"] = provider.Name()
		}
	}

	relations := response.Facts
	for j := range relations {
		relation := &relations[j]
		if relation.Metadata == nil {
			relation.Metadata = make(map[string]interface{})
		}
		for k, v := range metadata {
			relation.Metadata[k] = v
		}
		if _, exists := relation.Metadata["provider"]; !exists {
			relation.Metadata["provider"] = provider.Name()
		}
	}
}

func StageNameToProvider(
	stage string,
	domainConfigs []*domain.DomainConfig,
	proseCfg ProseNEROptions,
	llmCfg LLMNEROptions,
	log *logger.Logger,
	promptLoader *prompts.PromptLoader,
) (Provider, error) {
	switch stage {
	case "regex":
		return NewRegexNER(domainConfigs, log)
	case "prose":
		return NewProseNER(proseCfg, log), nil
	case "llm":
		return NewLLMNER(llmCfg, log, promptLoader)
	default:
		return nil, fmt.Errorf("unknown NER stage %q, want one of: regex, prose, llm", stage)
	}
}

func BuildCompositeFromStages(
	stages []string,
	domainConfigs []*domain.DomainConfig,
	proseCfg ProseNEROptions,
	llmCfg LLMNEROptions,
	log *logger.Logger,
	promptLoader *prompts.PromptLoader,
) (*CompositeNER, error) {
	if len(stages) == 0 {
		return NewCompositeNER(nil, nil, log), nil
	}

	validStages := []string{"regex", "prose", "llm"}
	for _, s := range stages {
		if !slices.Contains(validStages, s) {
			return nil, fmt.Errorf("invalid NER stage %q, want one of: %v", s, validStages)
		}
	}

	var providers []Provider
	for _, stage := range stages {
		p, err := StageNameToProvider(stage, domainConfigs, proseCfg, llmCfg, log, promptLoader)
		if err != nil {
			return nil, fmt.Errorf("build NER provider for stage %q: %w", stage, err)
		}
		providers = append(providers, p)
	}

	return NewCompositeNER(providers, domainConfigs, log), nil
}
