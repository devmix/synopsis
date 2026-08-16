package embedding_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/embedding"
)

func TestNewAPIProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.APIEmbedding
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: config.APIEmbedding{
				BaseURL:   "http://localhost:11434/v1",
				ModelName: "text-embedding-ada-002",
				VectorDim: 1536,
			},
			wantErr: false,
		},
		{
			name:    "missing base URL",
			cfg:     config.APIEmbedding{},
			wantErr: true,
		},
		{
			name: "missing model name",
			cfg: config.APIEmbedding{
				BaseURL:   "http://localhost:11434/v1",
				VectorDim: 1536,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := embedding.NewAPIProvider(tt.cfg, testLogger())
			if (err != nil) != tt.wantErr {
				t.Errorf("NewAPIProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
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

func TestAPIProvider_GenerateEmbeddings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		texts      []string
		wantErr    bool
		wantDim    int
	}{
		{
			name: "successful embedding",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				resp := map[string]interface{}{
					"data": []map[string]interface{}{
						{"embedding": []float64{0.1, 0.2, 0.3}},
					},
				}
				json.NewEncoder(w).Encode(resp) //nolint:errcheck
			},
			texts:   []string{"hello"},
			wantErr: false,
			wantDim: 3,
		},
		{
			name: "429 rate limited",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprint(w, `{"error":"rate limit"}`) //nolint:errcheck
			},
			texts:   []string{"hello"},
			wantErr: true,
		},
		{
			name: "500 server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprint(w, `{"error":"internal"}`) //nolint:errcheck
			},
			texts:   []string{"hello"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			cfg := config.APIEmbedding{
				BaseURL:    srv.URL,
				ModelName:  "test-model",
				VectorDim:  3,
				MaxRetries: 1, // keep tests fast
				TimeoutMs:  5000,
			}

			p, err := embedding.NewAPIProvider(cfg, testLogger())
			if err != nil {
				t.Fatalf("NewAPIProvider() = %v", err)
			}

			results, err := p.GenerateEmbeddings(context.Background(), tt.texts)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateEmbeddings() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(results) > 0 && len(results[0]) != tt.wantDim {
				t.Errorf("embedding dim = %d, want %d", len(results[0]), tt.wantDim)
			}
		})
	}
}

func TestAPIProvider_EmptyTexts(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()

	cfg := config.APIEmbedding{
		BaseURL:   srv.URL,
		ModelName: "test",
		VectorDim: 3,
	}
	p, err := embedding.NewAPIProvider(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewAPIProvider() = %v", err)
	}

	_, err = p.GenerateEmbeddings(context.Background(), []string{})
	if err == nil {
		t.Error("expected error for empty texts")
	}
}

func TestAPIProvider_ContextCancellation(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second) // slow handler to trigger cancellation
	}))
	defer srv.Close()

	cfg := config.APIEmbedding{
		BaseURL:   srv.URL,
		ModelName: "test",
		VectorDim: 3,
	}
	p, err := embedding.NewAPIProvider(cfg, testLogger())
	if err != nil {
		t.Fatalf("NewAPIProvider() = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = p.GenerateEmbeddings(ctx, []string{"hello"})
	if err == nil {
		t.Error("expected error on cancelled context")
	}
}
