package ner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/devmix/synopsis/internal/cache"
)

type LLMCache struct {
	store     *cache.Store
	tableName string
}

func NewLLMCache(store *cache.Store, tableName string) *LLMCache {
	if tableName == "" {
		tableName = "llm_ner_cache"
	}
	return &LLMCache{store: store, tableName: tableName}
}

func (c *LLMCache) GetNerResponse(ctx context.Context, key string) (*Result, bool) {
	if c.store == nil {
		return nil, false
	}
	if ctx.Err() != nil {
		return nil, false
	}

	result, hit := c.store.Get(ctx, c.tableName, key)
	if !hit {
		return nil, false
	}

	var response Result
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		return nil, false // corrupted entry treated as miss
	}

	return &response, true
}

func (c *LLMCache) SetNerResponse(ctx context.Context, key string, response *Result) error {
	if c.store == nil {
		return nil // no-op when store is unavailable — graceful degradation
	}
	if ctx.Err() != nil {
		return fmt.Errorf("cache set: context cancelled: %w", ctx.Err())
	}

	jsonBytes, err := json.Marshal(*response)
	if err != nil {
		return fmt.Errorf("cache set: marshal entities: %w", err)
	}

	return c.store.Set(ctx, c.tableName, key, string(jsonBytes))
}

// BuildCacheKey constructs a SHA-256 cache key from LLM parameters and content.
// Key format: sha256(server:model:temperature:max_tokens:system_prompt:user_prompt:chunk_content)
func BuildCacheKey(
	server string,
	model string,
	temperature float64,
	maxTokens int,
	systemPrompt string,
	userPrompt string,
	chunkContent string,
) string {
	parts := []string{
		server,
		model,
		fmt.Sprintf("%g", temperature),
		fmt.Sprintf("%d", maxTokens),
		systemPrompt,
		userPrompt,
		chunkContent,
	}
	raw := strings.Join(parts, ":")

	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}
