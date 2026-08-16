package onnx

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/utils"
)

// ModelManager orchestrates model registry, downloading, caching, and lifecycle.
type ModelManager struct {
	modelsDir  string
	registry   *ModelRegistry
	downloader *ModelDownloader
	cache      *ModelCache
}

// NewModelManager creates a manager using the given data directory for model storage.
func NewModelManager(dataDir string, cfg *config.ONNXConfig) (*ModelManager, error) {
	registry, err := NewModelRegistry(cfg)
	if err != nil {
		return nil, fmt.Errorf("init model registry: %w", err)
	}

	modelsDir := filepath.Join(dataDir, "models")
	cache, err := NewModelCache(modelsDir)
	if err != nil {
		return nil, fmt.Errorf("init model cache: %w", err)
	}

	downloader := NewModelDownloader(DefaultDownloadConfig())

	return &ModelManager{
		modelsDir:  modelsDir,
		registry:   registry,
		downloader: downloader,
		cache:      cache,
	}, nil
}

// EnsureModel checks if the named model is installed; downloads it automatically if not.
// Returns the absolute path to the model directory on success.
func (m *ModelManager) EnsureModel(modelName string) (string, error) {
	if modelName == "" {
		modelName = m.registry.Default()
	}

	if _, ok := m.registry.Get(modelName); !ok {
		return "", fmt.Errorf("model %q not found in registry", modelName)
	}

	modelDef, _ := m.registry.Get(modelName)
	modelDir := filepath.Join(m.modelsDir, modelName)

	// Check if already installed and files exist.
	if m.cache.IsInstalled(modelName) && utils.DirExists(modelDir) {
		// Return path to the primary model file (first file in definition).
		if len(modelDef.Files) > 0 {
			return filepath.Join(modelDir, modelDef.Files[0].Name), nil
		}
		return modelDir, nil
	}

	if err := m.DownloadModel(modelName); err != nil {
		return "", fmt.Errorf("download model %q: %w", modelName, err)
	}

	// Return path to the primary model file.
	if len(modelDef.Files) > 0 {
		return filepath.Join(modelDir, modelDef.Files[0].Name), nil
	}
	return modelDir, nil
}

// ListModels returns all registered models with their installation status.
func (m *ModelManager) ListModels() ([]config.ModelInfo, error) {
	models := m.registry.List()
	for i := range models {
		if m.cache.IsInstalled(models[i].Name) && utils.DirExists(filepath.Join(m.modelsDir, models[i].Name)) {
			// Mark as installed in the returned info.
			info, _ := m.cache.GetModelInfo(models[i].Name)
			models[i].Version = info.Version // use cached version if available
		}
	}
	return models, nil
}

// DownloadModel downloads all files for the named model into the local cache directory.
func (m *ModelManager) DownloadModel(modelName string) error {
	info, ok := m.registry.Get(modelName)
	if !ok {
		return fmt.Errorf("model %q not found in registry", modelName)
	}

	modelDir := filepath.Join(m.modelsDir, modelName)
	if err := os.MkdirAll(modelDir, 0o755); err != nil {
		return fmt.Errorf("create model dir: %w", err)
	}

	for _, f := range info.Files {
		destPath := filepath.Join(modelDir, f.Name)

		// Skip if file already exists.
		if _, err := os.Stat(destPath); err == nil {
			continue
		}

		totalSize := f.SizeBytes
		if err := m.downloader.DownloadFile(f.URL, destPath, f.Checksum, totalSize); err != nil {
			return fmt.Errorf("download file %s: %w", f.Name, err)
		}
	}

	installedInfo := InstalledModelInfo{
		Name:        info.Name,
		Version:     info.Version,
		VectorDim:   info.VectorDim,
		InstalledAt: timeNow(),
	}
	if err := m.cache.MarkInstalled(installedInfo); err != nil {
		return fmt.Errorf("update cache: %w", err)
	}

	return nil
}

// DeleteModel removes the model files and cache entry.
func (m *ModelManager) DeleteModel(modelName string) error {
	if !m.cache.IsInstalled(modelName) {
		return fmt.Errorf("model %q is not installed", modelName)
	}

	modelDir := filepath.Join(m.modelsDir, modelName)
	if err := os.RemoveAll(modelDir); err != nil {
		return fmt.Errorf("remove model dir: %w", err)
	}

	if err := m.cache.Remove(modelName); err != nil {
		return fmt.Errorf("update cache after delete: %w", err)
	}

	return nil
}

// GetModelPath returns the local path for a registered model and whether it is installed.
func (m *ModelManager) GetModelPath(modelName string) (string, bool) {
	if _, ok := m.registry.Get(modelName); !ok {
		return "", false
	}

	modelDir := filepath.Join(m.modelsDir, modelName)
	installed := m.cache.IsInstalled(modelName) && utils.DirExists(modelDir)
	return modelDir, installed
}

// Registry returns the underlying model registry.
func (m *ModelManager) Registry() *ModelRegistry {
	return m.registry
}

// Cache returns the underlying model cache.
func (m *ModelManager) Cache() *ModelCache {
	return m.cache
}

// SetDownloader replaces the downloader instance. Primarily useful for testing with mock servers.
func (m *ModelManager) SetDownloader(dl *ModelDownloader) {
	m.downloader = dl
}

// OverrideModelURLs replaces all file URLs for a model in the registry.
// This is primarily useful for testing with mock HTTP servers.
func (m *ModelManager) OverrideModelURLs(modelName string, urls []string) {
	if len(urls) == 0 {
		return
	}
	info, ok := m.registry.Get(modelName)
	if !ok {
		return
	}
	for i := range info.Files {
		if i < len(urls) {
			info.Files[i].URL = urls[i]
		}
	}
	m.registry.SetModel(info)
}

// GetModelsDir returns the models directory path.
func (m *ModelManager) GetModelsDir() string {
	return m.modelsDir
}

// ModelPathForFile returns the local path to a specific file within an installed model directory.
func (m *ModelManager) ModelPathForFile(modelName, fileName string) (string, bool) {
	modelDir := filepath.Join(m.modelsDir, modelName)
	if !utils.DirExists(modelDir) {
		return "", false
	}

	filePath := filepath.Join(modelDir, fileName)
	if _, err := os.Stat(filePath); err != nil {
		return "", false
	}
	return filePath, true
}

// timeNow is a testable reference to current time.
var timeNow = func() time.Time { return time.Now() }
