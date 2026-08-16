package onnx

import (
	"fmt"
	"strings"

	"github.com/devmix/synopsis/internal/config"
)

// ModelRegistry holds the list of available embedding models loaded from external config.
type ModelRegistry struct {
	models  map[string]config.ModelInfo
	default_ string
}

// NewModelRegistry loads the model registry from an ONNX config.
func NewModelRegistry(cfg *config.ONNXConfig) (*ModelRegistry, error) {
	if cfg == nil {
		return nil, fmt.Errorf("onnx config is nil")
	}

	models := make(map[string]config.ModelInfo, len(cfg.Models.Entries))
	for _, m := range cfg.Models.Entries {
		if strings.TrimSpace(m.Name) == "" {
			continue
		}
		m := m // capture for map value
		models[m.Name] = m
	}

	return &ModelRegistry{
		models:   models,
		default_: cfg.Models.Default,
	}, nil
}

// Get returns a copy of the model info by name, or false if not found.
// The returned ModelInfo has its own Files slice to prevent callers from
// mutating shared registry state (important for parallel test safety).
func (r *ModelRegistry) Get(name string) (config.ModelInfo, bool) {
	m, ok := r.models[name]
	if !ok {
		return m, false
	}
	cp := m
	cp.Files = make([]config.ModelFile, len(m.Files))
	copy(cp.Files, m.Files)
	return cp, true
}

// List returns all registered models sorted by name.
func (r *ModelRegistry) List() []config.ModelInfo {
	result := make([]config.ModelInfo, 0, len(r.models))
	for _, m := range r.models {
		result = append(result, m)
	}
	return result
}

// SetModel updates or adds a model in the registry. Primarily useful for testing.
func (r *ModelRegistry) SetModel(info config.ModelInfo) {
	r.models[info.Name] = info
}

// Default returns the default model name.
func (r *ModelRegistry) Default() string {
	return r.default_
}
