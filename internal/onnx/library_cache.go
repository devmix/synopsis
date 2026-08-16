package onnx

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// LibraryCache stores metadata about installed ONNX Runtime library.
type LibraryCache struct {
	Version      string `json:"version"`
	LibraryPath  string `json:"library_path"`
	InstallTime  string `json:"install_time"`
	Platform     string `json:"platform"`
}

// LibraryCacheManager handles persistence of library installation metadata.
type LibraryCacheManager struct {
	cacheDir string
	cacheFile string
}

// NewLibraryCacheManager creates a new cache manager.
func NewLibraryCacheManager(dataDir string) *LibraryCacheManager {
	cacheDir := filepath.Join(dataDir, "onnxruntime")
	return &LibraryCacheManager{
		cacheDir:  cacheDir,
		cacheFile: filepath.Join(cacheDir, ".cache.json"),
	}
}

// Load reads cache from disk.
func (m *LibraryCacheManager) Load() (*LibraryCache, error) {
	data, err := os.ReadFile(m.cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read cache: %w", err)
	}

	var cache LibraryCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("parse cache: %w", err)
	}
	return &cache, nil
}

// Save writes cache to disk.
func (m *LibraryCacheManager) Save(cache *LibraryCache) error {
	if err := os.MkdirAll(m.cacheDir, 0755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}

	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal cache: %w", err)
	}

	if err := os.WriteFile(m.cacheFile, data, 0644); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	return nil
}

// IsInstalled checks if library is installed and valid.
func (m *LibraryCacheManager) IsInstalled(version string) (bool, string) {
	cache, err := m.Load()
	if err != nil || cache == nil {
		return false, ""
	}

	if cache.Version != version {
		return false, ""
	}

	if _, err := os.Stat(cache.LibraryPath); os.IsNotExist(err) {
		return false, ""
	}

	return true, cache.LibraryPath
}

// Remove deletes the cache file.
func (m *LibraryCacheManager) Remove() error {
	return os.Remove(m.cacheFile)
}

// GetCacheDir returns the cache directory path.
func (m *LibraryCacheManager) GetCacheDir() string {
	return m.cacheDir
}
