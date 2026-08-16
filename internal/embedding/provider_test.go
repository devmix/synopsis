package embedding_test

import (
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/embedding"
	"github.com/devmix/synopsis/internal/logger"
)

// testLogger creates a logger for use in tests.
func testLogger() *logger.Logger {
	l, err := logger.New(logger.Options{Level: "info"})
	if err != nil {
		panic(err) // should never fail with valid level
	}
	return l
}

func TestNewProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.EmbeddingsConfig
		wantErr bool
	}{
		{
			name: "unknown mode",
			cfg: config.EmbeddingsConfig{Mode: "unknown"},
			wantErr: true,
		},
		{
			name: "api mode",
			cfg: config.EmbeddingsConfig{
				Mode: "api",
				API: config.APIEmbedding{
					BaseURL:   "https://api.example.com/v1",
					ModelName: "text-embedding-3-small",
					VectorDim: 1536,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := embedding.NewProvider(tt.cfg, "data", testLogger(), nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewProvider() error = %v, wantErr %v", err, tt.wantErr)
			}
			if p != nil && !tt.wantErr {
				if p.Name() == "" {
					t.Error("Name() should not be empty")
				}
			}
		})
	}
}
