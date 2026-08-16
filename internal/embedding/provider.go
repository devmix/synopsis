// Package embedding provides text embedding generation via pluggable providers.

package embedding

import (
	"context"
	"fmt"

	"github.com/devmix/synopsis/internal/cache"
	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/logger"
)

// Provider generates vector embeddings for text inputs.
type Provider interface {
	// GenerateEmbeddings creates vectors for a list of texts.
	GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error)

	// VectorDim returns the dimensionality of generated vectors.
	VectorDim() int

	// Name returns a human-readable provider name for logging.
	Name() string
}

// ProviderOption configures how a Provider is created.
type ProviderOption func(*providerConfig)

type providerConfig struct {
	cacheStore *cache.Store
}

// WithCacheStore sets the persistent cache store for embedding results.
// When nil, the provider uses memory-only caching (previous behavior).
func WithCacheStore(store *cache.Store) ProviderOption {
	return func(pc *providerConfig) {
		pc.cacheStore = store
	}
}

// NewProvider creates a Provider based on the configuration mode.
// Supported modes: "local" (ONNX Runtime), "api" (OpenAI-compatible HTTP).
// dataDir is used by the local provider to locate or download the ONNX Runtime library.
func NewProvider(cfg config.EmbeddingsConfig, dataDir string, log *logger.Logger, onnxCfg *config.ONNXConfig, opts ...ProviderOption) (Provider, error) {
	pc := &providerConfig{}
	for _, opt := range opts {
		opt(pc)
	}

	switch cfg.Mode {
	case "local":
		return newONNXProvider(cfg.Local, dataDir, log, pc.cacheStore, onnxCfg)
	case "api":
		return newAPIProvider(cfg.API, log, pc.cacheStore)
	default:
		return nil, fmt.Errorf("unknown embeddings mode %q, want \"local\" or \"api\"", cfg.Mode)
	}
}
