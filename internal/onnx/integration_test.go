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

func testONNXConfigIntegration() config.ONNXConfig {
	return config.ONNXConfig{
		Runtime: config.ONNXRuntimeConfig{
			Version: "1.20.0",
			Platforms: []config.ONNXPlatformConfig{
				{Key: "linux-amd64", OS: "linux", Arch: "amd64", ArchiveURL: "https://example.com/test.zip", LibraryName: "libonnxruntime.so"},
			},
		},
		Models: config.ONNXModelsConfig{
			Default: "bge-m3-int8",
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
					},
				},
			},
		},
	}
}

// TestManager_Integration tests the full manager lifecycle with mock servers.
func TestManager_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	modelContent := []byte("mock onnx model data")
	tokenizerContent := []byte(`{"version":"1.0"}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		base := filepath.Base(r.URL.Path)
		if base == "model.onnx" || base == "model.onnx_data" {
			w.Write(modelContent) //nolint:errcheck
		} else if base == "tokenizer.json" {
			w.Write(tokenizerContent) //nolint:errcheck
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cfg := testONNXConfigIntegration()
	mm, err := onnx.NewModelManager(tmpDir, &cfg)
	if err != nil {
		t.Fatalf("NewModelManager: %v", err)
	}

	reg := mm.Registry()
	info, ok := reg.Get("bge-m3-int8")
	if !ok {
		t.Fatal("model not found in registry")
	}

	urls := make([]string, len(info.Files))
	for i := range info.Files {
		urls[i] = srv.URL + "/" + info.Files[i].Name
	}
	mm.OverrideModelURLs("bge-m3-int8", urls)

	modelsDir := filepath.Join(tmpDir, "models")
	testModelDir := filepath.Join(modelsDir, "bge-m3-int8")
	if err := os.MkdirAll(testModelDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	dlCfg := onnx.DefaultDownloadConfig()
	dlCfg.SkipSSRFCheck = true
	dl := onnx.NewModelDownloader(dlCfg)

	for i, f := range info.Files {
		destPath := filepath.Join(testModelDir, f.Name)
		if err := dl.DownloadFile(urls[i], destPath, "", f.SizeBytes); err != nil {
			t.Fatalf("download %s: %v", f.Name, err)
		}

		if _, err := os.Stat(destPath); err != nil {
			t.Errorf("file %s not found after download: %v", f.Name, err)
		}
	}

	cache := mm.Cache()
	installedInfo := onnx.InstalledModelInfo{
		Name:      "bge-m3-int8",
		Version:   info.Version,
		VectorDim: info.VectorDim,
	}
	if err := cache.MarkInstalled(installedInfo); err != nil {
		t.Fatalf("MarkInstalled: %v", err)
	}

	t.Run("GetModelPath returns installed path", func(t *testing.T) {
		path, installed := mm.GetModelPath("bge-m3-int8")
		if !installed {
			t.Error("expected model to be installed")
		}
		if path != testModelDir {
			t.Errorf("path = %q, want %q", path, testModelDir)
		}
	})

	t.Run("ModelPathForFile returns correct file path", func(t *testing.T) {
		path, ok := mm.ModelPathForFile("bge-m3-int8", "model.onnx")
		if !ok {
			t.Error("expected model file to be found")
		}
		expected := filepath.Join(testModelDir, "model.onnx")
		if path != expected {
			t.Errorf("path = %q, want %q", path, expected)
		}
	})

	t.Run("DeleteModel removes files and cache entry", func(t *testing.T) {
		if err := mm.DeleteModel("bge-m3-int8"); err != nil {
			t.Fatalf("DeleteModel: %v", err)
		}

		if _, err := os.Stat(testModelDir); !os.IsNotExist(err) {
			t.Error("expected model directory to be deleted")
		}

		if cache.IsInstalled("bge-m3-int8") {
			t.Error("expected model to be uninstalled from cache")
		}
	})

	t.Run("DeleteModel fails for already deleted model", func(t *testing.T) {
		err := mm.DeleteModel("bge-m3-int8")
		if err == nil {
			t.Error("expected error when deleting already-deleted model")
		}
	})
}

func TestManager_EnsureModel_CacheHit(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := testONNXConfigIntegration()
	mm, err := onnx.NewModelManager(tmpDir, &cfg)
	if err != nil {
		t.Fatalf("NewModelManager: %v", err)
	}

	modelsDir := filepath.Join(tmpDir, "models")
	testModelDir := filepath.Join(modelsDir, "bge-m3-int8")
	if err := os.MkdirAll(testModelDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	dummyFile := filepath.Join(testModelDir, "model.onnx")
	if err := os.WriteFile(dummyFile, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write dummy: %v", err)
	}

	cache := mm.Cache()
	installedInfo := onnx.InstalledModelInfo{
		Name:      "bge-m3-int8",
		Version:   "1.0.0",
		VectorDim: 1024,
	}
	if err := cache.MarkInstalled(installedInfo); err != nil {
		t.Fatalf("MarkInstalled: %v", err)
	}

	modelPath, err := mm.EnsureModel("bge-m3-int8")
	if err != nil {
		t.Fatalf("EnsureModel with cache hit: %v", err)
	}
	expectedFile := filepath.Join(testModelDir, "model.onnx")
	if modelPath != expectedFile {
		t.Errorf("model path = %q, want %q", modelPath, expectedFile)
	}
}

func TestManager_ListModels_WithInstalled(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := testONNXConfigIntegration()
	mm, err := onnx.NewModelManager(tmpDir, &cfg)
	if err != nil {
		t.Fatalf("NewModelManager: %v", err)
	}

	modelsDir := filepath.Join(tmpDir, "models")
	testModelDir := filepath.Join(modelsDir, "bge-small-en-v1.5")
	if err := os.MkdirAll(testModelDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	dummyFile := filepath.Join(testModelDir, "model.onnx")
	if err := os.WriteFile(dummyFile, []byte("dummy"), 0o644); err != nil {
		t.Fatalf("write dummy: %v", err)
	}

	cache := mm.Cache()
	installedInfo := onnx.InstalledModelInfo{
		Name:      "bge-small-en-v1.5",
		Version:   "1.5.0",
		VectorDim: 384,
	}
	if err := cache.MarkInstalled(installedInfo); err != nil {
		t.Fatalf("MarkInstalled: %v", err)
	}

	models, err := mm.ListModels()
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}

	found := false
	for _, m := range models {
		if m.Name == "bge-small-en-v1.5" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected bge-small-en-v1.5 in model list")
	}
}
