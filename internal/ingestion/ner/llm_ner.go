package ner

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"text/template"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/domain"
	"github.com/devmix/synopsis/internal/llm"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/prompts"
	"github.com/devmix/synopsis/internal/utils"
)

type LLMNEROptions struct {
	Config        config.LLMConfig
	DomainConfigs []*domain.DomainConfig // effective (merged) domain configs (multi-domain)
	Cache         *LLMCache              // optional cache; nil disables caching
}

type LLMNER struct {
	llmClient     *llm.Client
	baseURL       string                 // cached for BuildCacheKey compatibility
	modelName     string                 // cached for BuildCacheKey compatibility
	temperature   float64                // cached for BuildCacheKey compatibility
	maxTokens     int                    // cached for BuildCacheKey compatibility
	domainConfigs []*domain.DomainConfig // multi-domain configs
	cache         *LLMCache              // optional cache; nil disables caching
	log           *logger.Logger         // structured logger; required
	promptLoader  *prompts.PromptLoader  // prompt template loader
	systemTmpl    *template.Template     // pre-loaded system prompt template
	userTmpl      *template.Template     // pre-loaded user prompt template
}

// attributeVersionRe validates version strings: optional 'v'/'V' prefix, digits with dots/dashes/underscores.
var attributeVersionRe = regexp.MustCompile(`^[vV]?[0-9]+([._-][a-zA-Z0-9]+)*$`)

func NewLLMNER(opts LLMNEROptions, log *logger.Logger, loader *prompts.PromptLoader) (*LLMNER, error) {
	if opts.Config.APIBaseURL == "" {
		return nil, fmt.Errorf("llm ner: base_url is required")
	}
	if opts.Config.ModelName == "" {
		return nil, fmt.Errorf("llm ner: model_name is required")
	}

	temp := opts.Config.Temperature
	if temp < 0 {
		temp = 0.0
	}

	maxTokens := opts.Config.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}

	llmClient, err := llm.NewClient(llm.ClientOptions{
		Config: opts.Config,
		Log:    log,
	})
	if err != nil {
		return nil, fmt.Errorf("llm ner: create client: %w", err)
	}

	l := &LLMNER{
		llmClient:    llmClient,
		baseURL:      opts.Config.APIBaseURL,
		modelName:    opts.Config.ModelName,
		temperature:  temp,
		maxTokens:    maxTokens,
		cache:        opts.Cache,
		log:          log,
		promptLoader: loader,
	}

	if len(opts.DomainConfigs) > 0 {
		for _, domainCfg := range opts.DomainConfigs {
			if domainCfg == nil {
				return nil, fmt.Errorf("llm ner: nil domain config in DomainConfigs")
			}
			l.domainConfigs = append(l.domainConfigs, domainCfg)
		}
	}

	if len(l.domainConfigs) <= 0 {
		return nil, fmt.Errorf("no valid domain configs")
	}

	// Load prompt templates.
	if loader != nil {
		sysInfo, err := loader.Load("ner/system")
		if err != nil {
			return nil, fmt.Errorf("llm ner: load system template: %w", err)
		}
		userInfo, err := loader.Load("ner/user")
		if err != nil {
			return nil, fmt.Errorf("llm ner: load user template: %w", err)
		}
		l.systemTmpl = sysInfo.Template
		l.userTmpl = userInfo.Template
	}

	return l, nil
}

func (l *LLMNER) Name() string {
	return "llm"
}

// nerSystemPromptData holds data for the NER system prompt template.
type nerSystemPromptData struct {
	Entities        []domain.EntityDef
	Relations       []domain.RelationDef
	WithJSONExample bool
}

func (l *LLMNER) renderSystemPrompt(cfg *domain.DomainConfig, withJSONExample bool) (string, error) {
	if l.systemTmpl == nil {
		return "", fmt.Errorf("ner system template not loaded")
	}

	data := nerSystemPromptData{
		Entities:        cfg.Entities,
		Relations:       cfg.Relations,
		WithJSONExample: withJSONExample,
	}

	var buf strings.Builder
	if err := l.systemTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("ner system template execution failed: %w", err)
	}
	return buf.String(), nil
}

func (l *LLMNER) renderUserPrompt(cfg *domain.DomainConfig, content string) (string, error) {
	if l.userTmpl == nil {
		return "", fmt.Errorf("ner user template not loaded")
	}

	data := map[string]interface{}{
		"Content":       content,
		"EntityTypes":   cfg.GetEntityTypes(),
		"RelationTypes": cfg.GetRelationTypes(),
	}

	var buf strings.Builder
	if err := l.userTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("ner user template execution failed: %w", err)
	}
	return buf.String(), nil
}

func (l *LLMNER) ExtractEntities(ctx context.Context, content string, metadata map[string]interface{}) (*Result, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("llm ner: context cancelled: %w", ctx.Err())
	}

	var merged Result

	for _, cfg := range l.domainConfigs {
		domainName := utils.Normalize(cfg.Name)

		systemPrompt, err := l.renderSystemPrompt(cfg, true)
		if err != nil {
			return nil, fmt.Errorf("llm ner: render system userPrompt for domain %s: %w", cfg.Name, err)
		}
		userPrompt, err := l.renderUserPrompt(cfg, content)
		if err != nil {
			return nil, fmt.Errorf("llm ner: render user userPrompt for domain %s: %w", cfg.Name, err)
		}

		cacheKey := BuildCacheKey(
			l.baseURL, l.modelName, l.temperature, l.maxTokens,
			systemPrompt, userPrompt, content,
		)

		// Check cache before making API call.
		if l.cache != nil {
			if response, hit := l.cache.GetNerResponse(ctx, cacheKey); hit {
				for i := range response.Entities {
					response.Entities[i].Domain = domainName
				}
				for i := range response.Facts {
					response.Facts[i].Domain = domainName
				}
				merged.Entities = append(merged.Entities, response.Entities...)
				merged.Facts = append(merged.Facts, response.Facts...)
				continue
			}
		}

		rawContent, err := l.llmClient.Call(ctx, llm.CallOptions{
			SystemPrompt: systemPrompt,
			UserPrompt:   userPrompt,
			JSONSchema:   GenerateJSONSchema(cfg, l.llmClient.IsRequiresSchema()),
			SchemaName:   "ner_result",
			Attachments:  []string{"CONTENT:\n\n" + content},
		})
		if err != nil {
			return nil, fmt.Errorf("llm ner: call: %w", err)
		}

		response, err := parseLLMResponse(rawContent)
		if err != nil {
			return nil, fmt.Errorf("llm ner: parse response from LLM output: %w", err)
		}

		// Tag entities and facts with domain.
		for i := range response.Entities {
			response.Entities[i].Domain = domainName
		}
		for i := range response.Facts {
			response.Facts[i].Domain = domainName
		}

		// Store in cache before enriching with metadata.
		if l.cache != nil {
			if err := l.cache.SetNerResponse(ctx, cacheKey, response); err != nil {
				return nil, fmt.Errorf("llm ner: cache set: %w", err)
			}
		}

		merged.Entities = append(merged.Entities, response.Entities...)
		merged.Facts = append(merged.Facts, response.Facts...)
	}

	return &merged, nil
}

type nerOutput struct {
	Entities  []nerEntity   `json:"entities"`
	Relations []nerRelation `json:"relations"`
}

type nerEntity struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"`
	Confidence  *float64               `json:"confidence"`
	Description string                 `json:"description"`
	Attributes  map[string]interface{} `json:"attributes"`
}

type nerRelation struct {
	SubjectType string                 `json:"subject_type"`
	SubjectName string                 `json:"subject_name"`
	Predicate   string                 `json:"predicate"`
	ObjectType  string                 `json:"object_type"`
	ObjectName  string                 `json:"object_name"`
	Attributes  map[string]interface{} `json:"attributes"`
}

const defaultLLMConfidence = 0.5

func parseLLMResponse(raw string) (*Result, error) {
	var output nerOutput
	if err := json.Unmarshal([]byte(raw), &output); err != nil {
		return nil, fmt.Errorf("invalid JSON from LLM: %w", err)
	}

	entities := make([]Entity, 0, len(output.Entities))
	for _, e := range output.Entities {
		if e.Name == "" {
			continue // skip empty entities
		}
		confidence := defaultLLMConfidence
		if e.Confidence != nil && *e.Confidence >= 0 && *e.Confidence <= 1 {
			confidence = *e.Confidence
		}

		description := truncateDescription(e.Description)
		metadata := validateEntityMetadata(e.Attributes)

		entities = append(entities, Entity{
			Name:        e.Name,
			Type:        e.Type,
			Description: description,
			Confidence:  confidence,
			Metadata:    metadata,
		})
	}

	facts := make([]Fact, 0, len(output.Relations))
	for _, e := range output.Relations {
		if e.ObjectName == "" || e.ObjectType == "" || e.SubjectName == "" || e.SubjectType == "" || e.Predicate == "" {
			continue // skip invalid facts
		}
		facts = append(facts, Fact{
			ObjectName:  e.ObjectName,
			ObjectType:  e.ObjectType,
			Predicate:   e.Predicate,
			SubjectName: e.SubjectName,
			SubjectType: e.SubjectType,
			Metadata:    validateEntityMetadata(e.Attributes),
		})
	}

	return &Result{
		Entities: entities,
		Facts:    facts,
	}, nil
}

// truncateDescription limits description to ~500 characters at a sentence boundary.
// It first tries to find the last sentence-ending punctuation within the limit;
// if none is found, it hard-caps at maxDescLen runes.
func truncateDescription(desc string) string {
	const maxDescLen = 500

	runes := []rune(strings.TrimSpace(desc))
	if len(runes) <= maxDescLen {
		return string(runes)
	}

	// Try to find the last sentence boundary within the limit.
	cutAt := -1
	for i := maxDescLen - 1; i >= 0; i-- {
		r := runes[i]
		if r == '.' || r == '!' || r == '?' || r == ';' {
			cutAt = i + 1 // include the punctuation
			break
		}
	}

	if cutAt > 0 {
		return strings.TrimSpace(string(runes[:cutAt]))
	}

	// Hard cap.
	return string(runes[:maxDescLen])
}

// validateEntityMetadata removes metadata values containing LLM uncertainty comments
// and validates the "version" field format. Returns a cleaned copy of the map.
func validateEntityMetadata(attrs map[string]interface{}) map[string]interface{} {
	if len(attrs) == 0 {
		return nil
	}

	cleaned := make(map[string]interface{}, len(attrs))
	for k, v := range attrs {
		valStr, ok := v.(string)
		if !ok {
			cleaned[k] = v
			continue
		}

		lowerVal := strings.ToLower(valStr)

		// Drop values containing LLM uncertainty comments.
		if strings.Contains(lowerVal, "implied by context") ||
			strings.Contains(lowerVal, "not explicitly stated") {
			continue
		}

		// Validate version field: must look like a version string (digits, dots, dashes, letters).
		// Reject values that contain year ranges or non-version patterns.
		if strings.EqualFold(k, "version") {
			if !isValidVersion(valStr) {
				continue
			}
		}

		cleaned[k] = v
	}

	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

// isValidVersion checks if a string looks like a valid version identifier.
// Accepts patterns like "1", "1.0", "2.3.1", "v1.0", "V2", "1.0-beta", etc.
// Rejects year ranges ("0-2 years"), dates, and other non-version strings.
func isValidVersion(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}

	lower := strings.ToLower(v)

	// Reject obvious non-version patterns.
	rejectPatterns := []string{
		" years", " year ", "-to-", "0-2", "1-3", "2-5",
		"approximately", "about", "around", "estimated",
	}
	for _, p := range rejectPatterns {
		if strings.Contains(lower, p) {
			return false
		}
	}

	// Accept version-like patterns: optional 'v' prefix, digits with dots/dashes/underscores.
	// Examples: 1, v2.3.1, 1.0-beta, 2_5, etc.
	return attributeVersionRe.MatchString(v)
}
