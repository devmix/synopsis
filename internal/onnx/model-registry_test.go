package onnx_test

import (
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/onnx"
)

func TestModelRegistry(t *testing.T) {
	t.Parallel()

	cfg := &config.ONNXConfig{
		Models: config.ONNXModelsConfig{
			Default: "bge-small-en-v1.5",
			Entries: []config.ModelInfo{
				{
					Name:        "bge-m3-int8",
					DisplayName: "BGE-M3 INT8 Quantized",
					Description: "BAAI bge-m3 int8 quantized ONNX model for multilingual embeddings",
					Version:     "1.0.0",
					VectorDim:   1024,
					Files: []config.ModelFile{
						{Name: "model.onnx", URL: "https://huggingface.co/BAAI/bge-m3/resolve/main/onnx/model.onnx", SizeBytes: 700 * 1024 * 1024},
						{Name: "tokenizer.json", URL: "https://huggingface.co/BAAI/bge-m3/resolve/main/onnx/tokenizer.json", SizeBytes: 17 * 1024 * 1024},
					},
					Source: "huggingface",
					Repo:   "BAAI/bge-m3",
				},
				{
					Name:        "bge-small-en-v1.5",
					DisplayName: "BGE Small English v1.5",
					Description: "Lightweight BGE model for English text embeddings",
					Version:     "1.5.0",
					VectorDim:   384,
					Files: []config.ModelFile{
						{Name: "model.onnx", URL: "https://huggingface.co/BAAI/bge-small-en-v1.5/resolve/main/onnx/model.onnx", SizeBytes: 66 * 1024 * 1024},
					},
					Source: "huggingface",
					Repo:   "BAAI/bge-small-en-v1.5",
				},
			},
		},
	}

	reg, err := onnx.NewModelRegistry(cfg)
	if err != nil {
		t.Fatalf("NewModelRegistry: %v", err)
	}

	t.Run("Get existing model", func(t *testing.T) {
		info, ok := reg.Get("bge-m3-int8")
		if !ok {
			t.Fatal("expected bge-m3-int8 to exist in registry")
		}
		if info.Name != "bge-m3-int8" {
			t.Errorf("Name = %q, want %q", info.Name, "bge-m3-int8")
		}
		if info.VectorDim != 1024 {
			t.Errorf("VectorDim = %d, want %d", info.VectorDim, 1024)
		}
		if len(info.Files) == 0 {
			t.Error("expected at least one file in model definition")
		}
	})

	t.Run("Get non-existent model", func(t *testing.T) {
		_, ok := reg.Get("nonexistent-model-xyz")
		if ok {
			t.Error("expected nonexistent model to return false")
		}
	})

	t.Run("List returns all models", func(t *testing.T) {
		models := reg.List()
		if len(models) < 1 {
			t.Errorf("expected at least 1 model, got %d", len(models))
		}
	})

	t.Run("Default model name", func(t *testing.T) {
		def := reg.Default()
		if def != "bge-small-en-v1.5" {
			t.Errorf("Default = %q, want %q", def, "bge-small-en-v1.5")
		}
	})

	t.Run("Model has files with URLs", func(t *testing.T) {
		info, ok := reg.Get("bge-m3-int8")
		if !ok {
			t.Fatal("model not found")
		}
		for _, f := range info.Files {
			if f.Name == "" {
				t.Error("file name must not be empty")
			}
			if f.URL == "" {
				t.Errorf("file %q URL must not be empty", f.Name)
			}
		}
	})

	t.Run("Model source is set", func(t *testing.T) {
		info, ok := reg.Get("bge-m3-int8")
		if !ok {
			t.Fatal("model not found")
		}
		if info.Source != "huggingface" {
			t.Errorf("Source = %q, want %q", info.Source, "huggingface")
		}
	})

	t.Run("Get returns independent copy of Files slice", func(t *testing.T) {
		info1, _ := reg.Get("bge-m3-int8")
		info2, _ := reg.Get("bge-m3-int8")
		info1.Files[0].URL = "https://modified.example.com/model.onnx"
		if info2.Files[0].URL == "https://modified.example.com/model.onnx" {
			t.Error("expected independent Files slices from Get()")
		}
	})
}
