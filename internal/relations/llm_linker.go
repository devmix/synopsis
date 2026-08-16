// Package relations provides cross-domain entity linking functionality.
package relations

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"strings"
	"text/template"

	"github.com/devmix/synopsis/internal/cache"
	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/llm"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/prompts"
	"github.com/devmix/synopsis/internal/utils"
)

const (
	cacheTableName  = "llm_entity_link_cache"
	defaultDescLen  = 200
	defaultChunkLen = 500
	contextLimit    = 3
)

// LLMCrossDomainLinker uses an LLM to determine whether two entities from different domains
// refer to the same real-world entity. Results are cached in SQLite for determinism.
type LLMCrossDomainLinker struct {
	llmClient           *llm.Client
	chunkEntityDAO      *dao.ChunkEntityDAO
	cache               *cache.Store
	cacheTable          string
	log                 *logger.Logger
	confidenceThreshold float64
	systemTmpl          *prompts.TemplateInfo
	userTmpl            *prompts.TemplateInfo
	sysHash             string // SHA-256 hash of system template content
	userHash            string // SHA-256 hash of user template content
}

// NewLLMCrossDomainLinker creates a linker backed by an OpenAI-compatible LLM API.
func NewLLMCrossDomainLinker(
	cfg *config.CrossDomainLinksConfig,
	linkerCfg config.LinkerConfig,
	db *sql.DB,
	cacheStore *cache.Store,
	log *logger.Logger,
	loader *prompts.PromptLoader,
) (*LLMCrossDomainLinker, error) {
	if cfg == nil {
		return nil, fmt.Errorf("llm linker: cross-domain-links config is required")
	}
	if db == nil {
		return nil, fmt.Errorf("llm linker: database connection is required")
	}
	if cacheStore == nil {
		return nil, fmt.Errorf("llm linker: cache store is required")
	}
	if loader == nil {
		return nil, fmt.Errorf("llm linker: prompt loader is required")
	}

	llmClient, err := llm.NewClient(llm.ClientOptions{
		Config: linkerCfg.LLM,
		Log:    log,
	})
	if err != nil {
		return nil, fmt.Errorf("llm linker: create client: %w", err)
	}

	sysTmpl, err := loader.Load("entity-linker/system")
	if err != nil {
		return nil, fmt.Errorf("llm linker: load system template: %w", err)
	}
	userTmpl, err := loader.Load("entity-linker/user")
	if err != nil {
		return nil, fmt.Errorf("llm linker: load user template: %w", err)
	}

	confidenceThreshold := cfg.LLmConfidenceThreshold
	if confidenceThreshold <= 0 {
		confidenceThreshold = config.DefaultLLMConfidenceThreshold
	}

	return &LLMCrossDomainLinker{
		llmClient:           llmClient,
		chunkEntityDAO:      dao.NewChunkEntityDAO(db),
		cache:               cacheStore,
		cacheTable:          cacheTableName,
		log:                 log,
		confidenceThreshold: confidenceThreshold,
		systemTmpl:          sysTmpl,
		userTmpl:            userTmpl,
		sysHash:             sysTmpl.Hash,
		userHash:            userTmpl.Hash,
	}, nil
}

// Name returns the linker identifier.
func (l *LLMCrossDomainLinker) Name() string { return "llm" }

// LinkPair evaluates whether two entities refer to the same real-world entity using an LLM.
func (l *LLMCrossDomainLinker) LinkPair(ctx context.Context, a, b EntityCandidate) (*LinkDecision, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("llm linker: context cancelled: %w", ctx.Err())
	}

	cacheKey := l.buildCacheKey(a, b)

	// Check cache first.
	if decision, hit := l.getCache(ctx, cacheKey); hit {
		if l.log != nil {
			l.log.Debug("llm linker cache hit")
		}
		return decision, nil
	}

	// Load context chunks for both entities.
	aChunks, err := l.loadEntityContext(ctx, a.ID, contextLimit)
	if err != nil {
		return nil, fmt.Errorf("llm linker: load entity A context: %w", err)
	}
	bChunks, err := l.loadEntityContext(ctx, b.ID, contextLimit)
	if err != nil {
		return nil, fmt.Errorf("llm linker: load entity B context: %w", err)
	}

	systemPrompt, err := l.executeTemplate(l.systemTmpl.Template, struct{}{})
	if err != nil {
		return nil, fmt.Errorf("llm linker: execute system template: %w", err)
	}

	aContext := make([]string, 0, len(aChunks))
	for _, c := range aChunks {
		aContext = append(aContext, utils.Truncate(c, defaultChunkLen))
	}
	bContext := make([]string, 0, len(bChunks))
	for _, c := range bChunks {
		bContext = append(bContext, utils.Truncate(c, defaultChunkLen))
	}

	promptData := promptData{
		EntityA: entityData{
			Name:        a.Name,
			Type:        a.Type,
			Domain:      a.Domain,
			Description: utils.Truncate(a.Description, defaultDescLen),
			Context:     aContext,
		},
		EntityB: entityData{
			Name:        b.Name,
			Type:        b.Type,
			Domain:      b.Domain,
			Description: utils.Truncate(b.Description, defaultDescLen),
			Context:     bContext,
		},
	}

	userPrompt, err := l.executeTemplate(l.userTmpl.Template, promptData)
	if err != nil {
		return nil, fmt.Errorf("llm linker: execute user template: %w", err)
	}

	raw, err := l.llmClient.Call(ctx, llm.CallOptions{
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		JSONSchema:   GenerateJSONSchema(l.llmClient.IsRequiresSchema()),
		SchemaName:   "entity_linker",
	})
	if err != nil {
		return nil, fmt.Errorf("llm linker: LLM call: %w", err)
	}

	decision, err := l.ParseLinkDecision(raw)
	if err != nil {
		return nil, fmt.Errorf("llm linker: parse decision: %w", err)
	}

	// Cache the result.
	if err := l.setCache(ctx, cacheKey, decision); err != nil {
		if l.log != nil {
			l.log.Debug("llm linker: cache write failed", logger.Err(err))
		}
	}

	return decision, nil
}

// loadEntityContext retrieves up to limit chunk texts associated with an entity.
func (l *LLMCrossDomainLinker) loadEntityContext(ctx context.Context, entityID int, limit int) ([]string, error) {
	return l.chunkEntityDAO.GetChunkTextsByEntity(ctx, entityID, limit)
}

// entityCacheKey returns a normalized key for an entity based on (type, name, domain).
// Uses '\x1F' (Unit Separator) to avoid ambiguity if names contain ':'.
func entityCacheKey(e EntityCandidate) string {
	return e.Type + "\x1F" + utils.Normalize(e.Name) + "\x1F" + utils.Normalize(e.Domain)
}

// buildCacheKey produces a deterministic FNV-128a key from an ordered entity pair.
// Pair ordering is by lexicographic comparison of normalized (type, name, domain) keys,
// so the cache is stable across database rebuilds that change entity IDs.
// Template content hashes are included to invalidate cache when templates change.
func (l *LLMCrossDomainLinker) buildCacheKey(a, b EntityCandidate) string {
	keyA := entityCacheKey(a)
	keyB := entityCacheKey(b)

	// Lexicographic ordering for determinism regardless of argument order.
	first, second := keyA, keyB
	if first > second {
		first, second = second, first
	}

	h := fnv.New128a()
	_, _ = fmt.Fprint(h, first, "|", second, "|", l.sysHash, "|", l.userHash)

	return fmt.Sprintf("%x", h.Sum(nil))
}

// getCache retrieves a cached LinkDecision.
func (l *LLMCrossDomainLinker) getCache(ctx context.Context, key string) (*LinkDecision, bool) {
	raw, hit := l.cache.Get(ctx, l.cacheTable, key)
	if !hit {
		return nil, false
	}

	var decision LinkDecision
	if err := json.Unmarshal([]byte(raw), &decision); err != nil {
		if l.log != nil {
			l.log.Debug("llm linker: cache parse failed", "key", chunkHash(key), logger.Err(err))
		}
		return nil, false
	}

	return &decision, true
}

// setCache stores a LinkDecision in the cache.
func (l *LLMCrossDomainLinker) setCache(ctx context.Context, key string, decision *LinkDecision) error {
	data, err := json.Marshal(decision)
	if err != nil {
		return fmt.Errorf("marshal link decision: %w", err)
	}

	return l.cache.Set(ctx, l.cacheTable, key, string(data))
}

// ParseLinkDecision unmarshals raw LLM JSON into a LinkDecision.
func (l *LLMCrossDomainLinker) ParseLinkDecision(raw string) (*LinkDecision, error) {
	var decision LinkDecision
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &decision); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}

	// Clamp confidence to [0, 1].
	if decision.Confidence < 0 {
		decision.Confidence = 0
	}
	if decision.Confidence > 1 {
		decision.Confidence = 1
	}

	return &decision, nil
}

// executeTemplate runs a template with the given data and returns the result string.
func (l *LLMCrossDomainLinker) executeTemplate(tmpl *template.Template, data interface{}) (string, error) {
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// promptData is the template input for the user prompt.
type promptData struct {
	EntityA entityData `json:"entity_a"`
	EntityB entityData `json:"entity_b"`
}

// entityData holds formatted entity information for templates.
type entityData struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Domain      string   `json:"domain"`
	Description string   `json:"description"`
	Context     []string `json:"context"`
}

// chunkHash returns a short hash of s for log redaction.
func chunkHash(s string) string {
	h := fnv.New64a()
	h.Write([]byte(s)) //nolint:errcheck
	return fmt.Sprintf("%x", h.Sum(nil))[:8]
}

func GenerateJSONSchema(useSchema bool) string {
	if !useSchema {
		return ""
	}

	return `
{
  "title": "EntityComparison",
  "type": "object",
  "properties": {
    "same_entity": {
      "type": "boolean",
      "description": "Indicates whether entities are the same"
    },
    "confidence": {
      "type": "number",
      "minimum": 0.0,
      "maximum": 1.0,
      "description": "The level of confidence in the answer is from 0.0 to 1.0"
    },
    "reasoning": {
      "type": "string",
      "description": "Explanation or logic for decision making"
    }
  },
  "required": ["same_entity", "confidence"],
  "additionalProperties": false
}`
}
