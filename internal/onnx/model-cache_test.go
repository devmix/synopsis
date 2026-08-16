package onnx_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devmix/synopsis/internal/onnx"
)

func TestModelCache(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cache, err := onnx.NewModelCache(tmpDir)
	if err != nil {
		t.Fatalf("NewModelCache: %v", err)
	}

	t.Run("Empty cache loads without error", func(t *testing.T) {
		if !cache.IsInstalled("any-model") {
			// expected — nothing installed yet
		} else {
			t.Error("expected no models to be installed on fresh cache")
		}
	})

	t.Run("MarkInstalled and IsInstalled", func(t *testing.T) {
		info := onnx.InstalledModelInfo{
			Name:        "test-model",
			Version:     "1.0.0",
			VectorDim:   384,
			InstalledAt: time.Now(),
		}

		if err := cache.MarkInstalled(info); err != nil {
			t.Fatalf("MarkInstalled: %v", err)
		}

		if !cache.IsInstalled("test-model") {
			t.Error("expected test-model to be installed after MarkInstalled")
		}
	})

	t.Run("GetModelInfo returns stored data", func(t *testing.T) {
		got, ok := cache.GetModelInfo("test-model")
		if !ok {
			t.Fatal("GetModelInfo returned false for known model")
		}
		if got.Name != "test-model" {
			t.Errorf("Name = %q, want %q", got.Name, "test-model")
		}
		if got.VectorDim != 384 {
			t.Errorf("VectorDim = %d, want %d", got.VectorDim, 384)
		}
	})

	t.Run("ListInstalled returns all names", func(t *testing.T) {
		names := cache.ListInstalled()
		found := false
		for _, n := range names {
			if n == "test-model" {
				found = true
				break
			}
		}
		if !found {
			t.Error("ListInstalled did not contain test-model")
		}
	})

	t.Run("Remove deletes model record", func(t *testing.T) {
		if err := cache.Remove("test-model"); err != nil {
			t.Fatalf("Remove: %v", err)
		}

		if cache.IsInstalled("test-model") {
			t.Error("expected test-model to be uninstalled after Remove")
		}
	})

	t.Run("Persistence survives reload", func(t *testing.T) {
		info := onnx.InstalledModelInfo{
			Name:        "persisted-model",
			Version:     "2.0.0",
			VectorDim:   1024,
			InstalledAt: time.Now(),
		}

		if err := cache.MarkInstalled(info); err != nil {
			t.Fatalf("MarkInstalled: %v", err)
		}

		// Create a new cache instance to test persistence.
		cache2, err := onnx.NewModelCache(tmpDir)
		if err != nil {
			t.Fatalf("reload NewModelCache: %v", err)
		}

		if !cache2.IsInstalled("persisted-model") {
			t.Error("expected persisted-model to survive cache reload")
		}

		got, ok := cache2.GetModelInfo("persisted-model")
		if !ok {
			t.Fatal("GetModelInfo returned false for persisted model after reload")
		}
		if got.VectorDim != 1024 {
			t.Errorf("VectorDim = %d, want %d", got.VectorDim, 1024)
		}
	})

	t.Run("Cache file is valid JSON", func(t *testing.T) {
		cachePath := filepath.Join(tmpDir, ".cache.json")
		data, err := os.ReadFile(cachePath)
		if err != nil {
			t.Fatalf("read cache file: %v", err)
		}

		var models map[string]onnx.InstalledModelInfo
		if err := json.Unmarshal(data, &models); err != nil {
			t.Errorf("cache JSON is not valid: %v", err)
		}
	})
}
