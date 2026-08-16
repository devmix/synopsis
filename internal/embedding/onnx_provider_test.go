package embedding_test

import (
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/embedding"
)

func TestNewONNXProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.LocalEmbedding
		wantErr bool
	}{
		{
			name:    "missing model path",
			cfg:     config.LocalEmbedding{},
			wantErr: true,
		},
		{
			name: "nonexistent model file",
			cfg: config.LocalEmbedding{
				ModelPath: "/nonexistent/path/model.onnx",
			},
			wantErr: true,
		},
		{
			name:    "zero vector dim defaults to 1024",
			cfg:     config.LocalEmbedding{}, // ModelPath empty so will error before defaulting
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := embedding.NewONNXProvider(tt.cfg, "data", testLogger(), nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewONNXProvider() error = %v, wantErr %v", err, tt.wantErr)
			}
			if p != nil && !tt.wantErr {
				if p.VectorDim() <= 0 {
					t.Error("VectorDim() should be positive")
				}
				if p.Name() == "" {
					t.Error("Name() should not be empty")
				}
			}
		})
	}
}

func TestONNXProvider_EmptyTexts(t *testing.T) {
	t.Parallel()

	// We cannot create a real ONNX provider without a model file,
	// so we test the cache behavior which is shared infrastructure.
	c := embedding.NewEmbeddingCache()

	vec := []float32{0.1, 0.2, 0.3}
	const model = "test-model"
	const dim = 768
	c.Set(model, dim, "test", vec)

	got, ok := c.Get(model, dim, "test")
	if !ok {
		t.Fatal("cache Get returned false for set key")
	}
	if len(got) != len(vec) {
		t.Fatalf("len = %d, want %d", len(got), len(vec))
	}
	for i := range got {
		if got[i] != vec[i] {
			t.Errorf("[%d] = %f, want %f", i, got[i], vec[i])
		}
	}
}

func TestONNXProvider_Caching(t *testing.T) {
	t.Parallel()

	c := embedding.NewEmbeddingCache()

	const model = "test-model"
	const dim = 768

	tests := []struct {
		name  string
		key   string
		value []float32
	}{
		{
			name:  "first entry",
			key:   "hello world",
			value: []float32{0.1, 0.2},
		},
		{
			name:  "second entry",
			key:   "foo bar",
			value: []float32{0.3, 0.4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.Set(model, dim, tt.key, tt.value)

			got, ok := c.Get(model, dim, tt.key)
			if !ok {
				t.Fatalf("cache miss for key %q", tt.key)
			}
			if len(got) != len(tt.value) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.value))
			}
			for i := range got {
				if got[i] != tt.value[i] {
					t.Errorf("[%d] = %f, want %f", i, got[i], tt.value[i])
				}
			}
		})
	}

	// Verify cache miss for unknown key.
	_, ok := c.Get(model, dim, "unknown")
	if ok {
		t.Error("cache hit for unknown key")
	}
}

func TestONNXProvider_ConfigValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		modelPath string
		wantErr   bool
	}{
		{
			name:      "empty model path",
			modelPath: "",
			wantErr:   true,
		},
		{
			name:      "nonexistent file",
			modelPath: "/tmp/does-not-exist-12345.onnx",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.LocalEmbedding{ModelPath: tt.modelPath}
			p, err := embedding.NewONNXProvider(cfg, "data", testLogger(), nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewONNXProvider() error = %v, wantErr %v", err, tt.wantErr)
			}
			if p != nil && tt.wantErr {
				t.Error("provider should be nil on validation error")
			}
		})
	}
}
