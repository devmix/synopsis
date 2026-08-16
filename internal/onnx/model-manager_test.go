package onnx_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/onnx"
)

func testONNXConfigManagerTest() config.ONNXConfig {
	return config.ONNXConfig{
		Runtime: config.ONNXRuntimeConfig{
			Version: "1.20.0",
			Platforms: []config.ONNXPlatformConfig{
				{Key: "linux-amd64", OS: "linux", Arch: "amd64", ArchiveURL: "https://example.com/test.zip", LibraryName: "libonnxruntime.so"},
			},
		},
		Models: config.ONNXModelsConfig{
			Default: "bge-small-en-v1.5",
			Entries: []config.ModelInfo{
				{
					Name:        "bge-m3-int8",
					Version:     "1.0.0",
					VectorDim:   1024,
					Description: "BGE M3 INT8 quantized model",
					Files: []config.ModelFile{
						{Name: "model.onnx", SizeBytes: 2 * 1024 * 1024, Checksum: "", URL: ""},
						{Name: "model.onnx_data", SizeBytes: 1 * 1024 * 1024, Checksum: "", URL: ""},
						{Name: "tokenizer.json", SizeBytes: 0, Checksum: "", URL: ""},
					},
				},
				{
					Name:        "bge-small-en-v1.5",
					Version:     "1.5.0",
					VectorDim:   384,
					Description: "BGE Small English v1.5",
					Files: []config.ModelFile{
						{Name: "model.onnx", SizeBytes: 1 * 1024 * 1024, Checksum: "", URL: ""},
						{Name: "tokenizer.json", SizeBytes: 0, Checksum: "", URL: ""},
						{Name: "tokenizer_config.json", SizeBytes: 0, Checksum: "", URL: ""},
					},
				},
			},
		},
	}
}

func TestModelManager(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cfg := testONNXConfigManagerTest()

	mm, err := onnx.NewModelManager(tmpDir, &cfg)
	if err != nil {
		t.Fatalf("NewModelManager: %v", err)
	}

	t.Run("EnsureModel with unknown model fails", func(t *testing.T) {
		_, err := mm.EnsureModel("nonexistent-model-xyz")
		if err == nil {
			t.Error("expected error for nonexistent model")
		}
	})

	t.Run("GetModelPath returns false for uninstalled model", func(t *testing.T) {
		path, installed := mm.GetModelPath("bge-m3-int8")
		if installed {
			t.Error("expected bge-m3-int8 to not be installed yet")
		}
		if path == "" {
			t.Error("GetModelPath should return a path even for uninstalled models")
		}
	})

	t.Run("GetModelPath returns false for unknown model", func(t *testing.T) {
		_, ok := mm.GetModelPath("nonexistent-model-xyz")
		if ok {
			t.Error("expected false for nonexistent model")
		}
	})

	t.Run("ListModels returns registered models", func(t *testing.T) {
		models, err := mm.ListModels()
		if err != nil {
			t.Fatalf("ListModels: %v", err)
		}
		if len(models) == 0 {
			t.Error("expected at least one model in registry")
		}
	})

	t.Run("DeleteModel fails for uninstalled model", func(t *testing.T) {
		err := mm.DeleteModel("bge-m3-int8")
		if err == nil {
			t.Error("expected error when deleting uninstalled model")
		}
	})

	t.Run("EnsureModel with empty name uses default via mock server", func(t *testing.T) {
		modelContent := []byte("mock onnx model data")
		tokenizerContent := []byte(`{"version":"1.0"}`)
		configContent := []byte(`{"model":"test"}`)

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			switch base := filepath.Base(r.URL.Path); base {
			case "model.onnx":
				w.Write(modelContent) //nolint:errcheck
			case "tokenizer.json":
				w.Write(tokenizerContent) //nolint:errcheck
			case "tokenizer_config.json":
				w.Write(configContent) //nolint:errcheck
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer srv.Close()

		reg := mm.Registry()
		info, ok := reg.Get("bge-small-en-v1.5")
		if !ok {
			t.Fatal("default model not found in registry")
		}

		urls := make([]string, len(info.Files))
		for i := range info.Files {
			urls[i] = srv.URL + "/" + info.Files[i].Name
		}
		mm.OverrideModelURLs("bge-small-en-v1.5", urls)

		dlCfg := onnx.DefaultDownloadConfig()
		dlCfg.SkipSSRFCheck = true
		mm.SetDownloader(onnx.NewModelDownloader(dlCfg))

		modelPath, err := mm.EnsureModel("")
		if err != nil {
			t.Fatalf("EnsureModel with empty name (should use default): %v", err)
		}

		expectedFile := filepath.Join(tmpDir, "models", "bge-small-en-v1.5", "model.onnx")
		if modelPath != expectedFile {
			t.Errorf("model path = %q, want %q", modelPath, expectedFile)
		}
	})

	t.Run("Registry returns valid registry", func(t *testing.T) {
		reg := mm.Registry()
		if reg == nil {
			t.Fatal("Registry returned nil")
		}
		info, ok := reg.Get("bge-m3-int8")
		if !ok {
			t.Error("registry should contain bge-m3-int8")
		}
		if info.Name != "bge-m3-int8" {
			t.Errorf("Name = %q, want %q", info.Name, "bge-m3-int8")
		}
	})

	t.Run("Cache returns valid cache", func(t *testing.T) {
		cache := mm.Cache()
		if cache == nil {
			t.Fatal("Cache returned nil")
		}
	})

	t.Run("ModelPathForFile returns false for missing model dir", func(t *testing.T) {
		_, ok := mm.ModelPathForFile("bge-m3-int8", "model_int8.onnx")
		if ok {
			t.Error("expected false when model directory does not exist")
		}
	})

	t.Run("ModelPathForFile returns true for existing file", func(t *testing.T) {
		modelsDir := filepath.Join(tmpDir, "models")
		testModelDir := filepath.Join(modelsDir, "test-manual-model")
		if err := os.MkdirAll(testModelDir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}

		testFile := filepath.Join(testModelDir, "model.onnx")
		if err := os.WriteFile(testFile, []byte("dummy"), 0o644); err != nil {
			t.Fatalf("write test file: %v", err)
		}

		path, ok := mm.ModelPathForFile("test-manual-model", "model.onnx")
		if !ok {
			t.Error("expected true for existing model file")
		}
		if path != testFile {
			t.Errorf("path = %q, want %q", path, testFile)
		}
	})
}
