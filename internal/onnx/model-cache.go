package onnx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// InstalledModelInfo stores metadata about a locally installed model.
type InstalledModelInfo struct {
	Name        string    `json:"name"`
	Version     string    `json:"version"`
	VectorDim   int       `json:"vector_dim"`
	InstalledAt time.Time `json:"installed_at"`
}

// ModelCache persists metadata about installed models to a JSON file.
type ModelCache struct {
	mu      sync.RWMutex
	dataDir string
	cache   map[string]InstalledModelInfo // name -> info
}

// NewModelCache creates a cache backed by a JSON file in the given data directory.
func NewModelCache(dataDir string) (*ModelCache, error) {
	mc := &ModelCache{
		dataDir: dataDir,
		cache:   make(map[string]InstalledModelInfo),
	}

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create model cache dir %s: %w", dataDir, err)
	}

	if err := mc.Load(); err != nil {
		return nil, fmt.Errorf("load model cache: %w", err)
	}

	return mc, nil
}

// Load reads installed model metadata from the JSON cache file.
func (mc *ModelCache) Load() error {
	cachePath := filepath.Join(mc.dataDir, ".cache.json")

	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no cache yet, start fresh
		}
		return fmt.Errorf("read cache file: %w", err)
	}

	var models map[string]InstalledModelInfo
	if err := json.Unmarshal(data, &models); err != nil {
		return fmt.Errorf("parse cache JSON: %w", err)
	}

	mc.cache = models
	return nil
}

// Save writes the current model metadata to disk.
func (mc *ModelCache) Save() error {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	cachePath := filepath.Join(mc.dataDir, ".cache.json")

	data, err := json.MarshalIndent(mc.cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache JSON: %w", err)
	}

	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		return fmt.Errorf("write cache file: %w", err)
	}

	return nil
}

// IsInstalled returns true if the model with the given name is marked as installed.
func (mc *ModelCache) IsInstalled(name string) bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	_, ok := mc.cache[name]
	return ok
}

// GetModelInfo returns metadata for an installed model, or false if not found.
func (mc *ModelCache) GetModelInfo(name string) (InstalledModelInfo, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	info, ok := mc.cache[name]
	return info, ok
}

// MarkInstalled records that a model has been installed.
func (mc *ModelCache) MarkInstalled(info InstalledModelInfo) error {
	mc.mu.Lock()
	mc.cache[info.Name] = info
	mc.mu.Unlock()

	return mc.Save()
}

// Remove deletes the installation record for a model.
func (mc *ModelCache) Remove(name string) error {
	mc.mu.Lock()
	delete(mc.cache, name)
	mc.mu.Unlock()

	return mc.Save()
}

// ListInstalled returns all installed model names.
func (mc *ModelCache) ListInstalled() []string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()

	names := make([]string, 0, len(mc.cache))
	for name := range mc.cache {
		names = append(names, name)
	}
	return names
}
