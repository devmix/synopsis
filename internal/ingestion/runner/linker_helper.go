// linker_helper.go contains the newCrossDomainLinker helper for creating an LLM
// cross-domain linker when prerequisites are met.
package runner

import (
	"context"
	"fmt"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/relations"
)

// newCrossDomainLinker creates an LLM cross-domain linker when all prerequisites
// are met: Linker.Disabled, "llm" method in config, cache store available, and
// constructor succeeds. Returns nil without error when the llm method is not
// configured or prerequisites are unmet (the caller handles nil gracefully).
func (r *Runner) newCrossDomainLinker(ctx context.Context, globalCfg *config.CrossDomainLinksConfig) (relations.CrossDomainLinker, error) {
	if r.cfg.Linker.Disabled {
		return nil, nil
	}

	hasLLMMethod := false
	for _, m := range globalCfg.Methods {
		if m == "llm" {
			hasLLMMethod = true
			break
		}
	}
	if !hasLLMMethod {
		return nil, nil
	}

	if r.cacheStore == nil {
		r.log.Warn("build entity links: cache store not available, skipping LLM method")
		return nil, nil
	}

	llmLinker, err := relations.NewLLMCrossDomainLinker(
		globalCfg,
		r.cfg.Linker,
		r.db,
		r.cacheStore,
		r.log,
		r.promptLoader,
	)
	if err != nil {
		return nil, fmt.Errorf("create LLM linker: %w", err)
	}

	return llmLinker, nil
}
