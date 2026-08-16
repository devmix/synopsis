package onnx

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/config"
)

func testONNXConfigWithModels() config.ONNXConfig {
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

func TestDownloadModel_Success(t *testing.T) {
	t.Parallel()

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

	cfg := testONNXConfigWithModels()
	mm, err := NewModelManager(tmpDir, &cfg)
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

	dlCfg := DefaultDownloadConfig()
	dlCfg.SkipSSRFCheck = true
	mm.SetDownloader(NewModelDownloader(dlCfg))

	if err := mm.DownloadModel("bge-m3-int8"); err != nil {
		t.Fatalf("DownloadModel: %v", err)
	}

	for _, f := range info.Files {
		destPath := filepath.Join(testModelDir, f.Name)
		if _, err := os.Stat(destPath); err != nil {
			t.Errorf("file %s not found after download: %v", f.Name, err)
		}
	}

	if !mm.cache.IsInstalled("bge-m3-int8") {
		t.Error("expected model to be marked as installed after download")
	}
}

func TestDownloadModel_FilesAlreadyExist(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := testONNXConfigWithModels()
	mm, err := NewModelManager(tmpDir, &cfg)
	if err != nil {
		t.Fatalf("NewModelManager: %v", err)
	}

	modelsDir := filepath.Join(tmpDir, "models")
	testModelDir := filepath.Join(modelsDir, "bge-m3-int8")
	if err := os.MkdirAll(testModelDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	for _, f := range []string{"model.onnx", "model.onnx_data", "tokenizer.json"} {
		destPath := filepath.Join(testModelDir, f)
		if err := os.WriteFile(destPath, []byte("existing"), 0o644); err != nil {
			t.Fatalf("write existing file: %v", err)
		}
	}

	dlCfg := DefaultDownloadConfig()
	mm.SetDownloader(NewModelDownloader(dlCfg))

	if err := mm.DownloadModel("bge-m3-int8"); err != nil {
		t.Fatalf("DownloadModel with existing files: %v", err)
	}

	if !mm.cache.IsInstalled("bge-m3-int8") {
		t.Error("expected model to be marked as installed even when files exist")
	}
}

func TestEnsureModel_DownloadsWhenMissing(t *testing.T) {
	t.Parallel()

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

	cfg := testONNXConfigWithModels()
	mm, err := NewModelManager(tmpDir, &cfg)
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

	dlCfg := DefaultDownloadConfig()
	dlCfg.SkipSSRFCheck = true
	mm.SetDownloader(NewModelDownloader(dlCfg))

	modelPath, err := mm.EnsureModel("bge-m3-int8")
	if err != nil {
		t.Fatalf("EnsureModel: %v", err)
	}

	expectedFile := filepath.Join(tmpDir, "models", "bge-m3-int8", "model.onnx")
	if modelPath != expectedFile {
		t.Errorf("model path = %q, want %q", modelPath, expectedFile)
	}

	for _, f := range info.Files {
		destPath := filepath.Join(filepath.Dir(modelPath), f.Name)
		if _, err := os.Stat(destPath); err != nil {
			t.Errorf("file %s not found after EnsureModel: %v", f.Name, err)
		}
	}
}

func TestEnsureModel_DefaultModelName(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	cfg := testONNXConfigWithModels()
	cfg.Models.Default = "bge-small-en-v1.5"
	mm, err := NewModelManager(tmpDir, &cfg)
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
	installedInfo := InstalledModelInfo{
		Name:      "bge-small-en-v1.5",
		Version:   "1.5.0",
		VectorDim: 384,
	}
	if err := cache.MarkInstalled(installedInfo); err != nil {
		t.Fatalf("MarkInstalled: %v", err)
	}

	modelPath, err := mm.EnsureModel("")
	if err != nil {
		t.Fatalf("EnsureModel with empty name (should use default): %v", err)
	}

	expectedFile := filepath.Join(tmpDir, "models", "bge-small-en-v1.5", "model.onnx")
	if modelPath != expectedFile {
		t.Errorf("model path = %q, want %q", modelPath, expectedFile)
	}
}
