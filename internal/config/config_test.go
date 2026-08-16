package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/config"
)

func TestLoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name: "valid local config",
			content: `embeddings:
  mode: "local"
  local:
    model_path: "./models/bge-m3-int8.onnx"
    vector_dim: 1024
paths:
  data_dir: "data"
  documents_dir: "documents"
server:
  name: "synopsis"
`,
			wantErr: false,
		},
		{
			name: "valid api config",
			content: `embeddings:
  mode: "api"
  api:
    base_url: "http://localhost:11434/v1"
    model_name: "text-embedding-3-large"
    vector_dim: 3072
paths: {}
server: {}
`,
			wantErr: false,
		},
		{
			name:    "invalid YAML",
			content: `{{invalid yaml content}}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgFile := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(cfgFile, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			_, err := config.Load(cfgFile)
			if (err != nil) != tt.wantErr {
				t.Errorf("Load() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := config.Load("/nonexistent/path/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.Config
		wantErr bool
	}{
		{
			name: "valid local mode",
			cfg: config.Config{
				Embeddings: config.EmbeddingsConfig{
					Mode: "local",
					Local: config.LocalEmbedding{
						ModelPath: "./models/model.onnx",
						VectorDim: 1024,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid api mode",
			cfg: config.Config{
				Embeddings: config.EmbeddingsConfig{
					Mode: "api",
					API: config.APIEmbedding{
						BaseURL:   "http://localhost:11434/v1",
						ModelName: "text-embedding-3-large",
						VectorDim: 3072,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "local mode missing model path",
			cfg: config.Config{
				Embeddings: config.EmbeddingsConfig{
					Mode:  "local",
					Local: config.LocalEmbedding{VectorDim: 1024},
				},
			},
			wantErr: true,
		},
		{
			name: "local mode zero vector dim",
			cfg: config.Config{
				Embeddings: config.EmbeddingsConfig{
					Mode: "local",
					Local: config.LocalEmbedding{
						ModelPath: "./models/model.onnx",
						VectorDim: 0,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "api mode missing base url",
			cfg: config.Config{
				Embeddings: config.EmbeddingsConfig{
					Mode: "api",
					API: config.APIEmbedding{
						ModelName: "text-embedding-3-large",
						VectorDim: 3072,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "api mode missing model name",
			cfg: config.Config{
				Embeddings: config.EmbeddingsConfig{
					Mode: "api",
					API: config.APIEmbedding{
						BaseURL:   "http://localhost:11434/v1",
						VectorDim: 3072,
					},
				},
			},
			wantErr: true,
		},
		{
			name: "unknown mode",
			cfg: config.Config{
				Embeddings: config.EmbeddingsConfig{Mode: "unknown"},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestApplyDefaults_Reranker(t *testing.T) {
	t.Parallel()

	t.Run("zero reranker values get defaults", func(t *testing.T) {
		cfg := config.Config{}
		cfg.ApplyDefaults()

		if cfg.Search.DeprecatedBoost != 0.2 {
			t.Errorf("expected deprecated_boost 0.2, got %f", cfg.Search.DeprecatedBoost)
		}
		if cfg.Search.OfficialBoost != 1.5 {
			t.Errorf("expected official_boost 1.5, got %f", cfg.Search.OfficialBoost)
		}
		if cfg.Search.RecentBoost != 1.2 {
			t.Errorf("expected recent_boost 1.2, got %f", cfg.Search.RecentBoost)
		}
		if cfg.Search.RecentDays != 90 {
			t.Errorf("expected recent_days 90, got %d", cfg.Search.RecentDays)
		}
		if cfg.Search.AuthorityBoost == nil {
			t.Error("expected authority_boost to be initialized")
		}
	})

	t.Run("existing reranker values preserved", func(t *testing.T) {
		cfg := config.Config{
			Search: config.SearchConfig{
				DeprecatedBoost: 0.3,
				OfficialBoost:   2.0,
				RecentBoost:     1.5,
				RecentDays:      30,
				AuthorityBoost:  map[string]float64{"custom": 1.2},
			},
		}
		cfg.ApplyDefaults()

		if cfg.Search.DeprecatedBoost != 0.3 {
			t.Error("expected deprecated_boost to be preserved")
		}
		if cfg.Search.AuthorityBoost["custom"] != 1.2 {
			t.Error("expected authority_boost to be preserved")
		}
	})
}

func TestApplyDefaults_OrphanCleanup(t *testing.T) {
	t.Parallel()

	t.Run("zero-value job gets defaults — disabled with 3600s interval", func(t *testing.T) {
		cfg := config.Config{}
		cfg.ApplyDefaults()

		job, ok := cfg.Scheduler.Jobs["orphan_cleanup"]
		if !ok {
			t.Fatal("expected orphan_cleanup job to exist after ApplyDefaults")
		}
		if job.Enabled {
			t.Error("expected orphan_cleanup to be disabled by default")
		}
		if job.IntervalSeconds != 3600 {
			t.Errorf("expected interval_seconds=3600, got %d", job.IntervalSeconds)
		}
	})

	t.Run("explicitly enabled job preserved", func(t *testing.T) {
		cfg := config.Config{
			Scheduler: config.SchedulerConfig{
				Jobs: map[string]config.JobConfig{
					"orphan_cleanup": {Enabled: true},
				},
			},
		}
		cfg.ApplyDefaults()

		job := cfg.Scheduler.Jobs["orphan_cleanup"]
		if !job.Enabled {
			t.Error("expected orphan_cleanup to remain enabled")
		}
		if job.IntervalSeconds != 3600 {
			t.Errorf("expected interval_seconds=3600 (default), got %d", job.IntervalSeconds)
		}
	})

	t.Run("explicitly configured interval preserved", func(t *testing.T) {
		cfg := config.Config{
			Scheduler: config.SchedulerConfig{
				Jobs: map[string]config.JobConfig{
					"orphan_cleanup": {Enabled: true, IntervalSeconds: 7200},
				},
			},
		}
		cfg.ApplyDefaults()

		job := cfg.Scheduler.Jobs["orphan_cleanup"]
		if !job.Enabled {
			t.Error("expected orphan_cleanup to remain enabled")
		}
		if job.IntervalSeconds != 7200 {
			t.Errorf("expected interval_seconds=7200, got %d", job.IntervalSeconds)
		}
	})

	t.Run("explicitly disabled with custom interval preserved", func(t *testing.T) {
		cfg := config.Config{
			Scheduler: config.SchedulerConfig{
				Jobs: map[string]config.JobConfig{
					"orphan_cleanup": {Enabled: false, IntervalSeconds: 1800},
				},
			},
		}
		cfg.ApplyDefaults()

		job := cfg.Scheduler.Jobs["orphan_cleanup"]
		if job.Enabled {
			t.Error("expected orphan_cleanup to remain disabled")
		}
		if job.IntervalSeconds != 1800 {
			t.Errorf("expected interval_seconds=1800, got %d", job.IntervalSeconds)
		}
	})
}

func TestGlobalConfigPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "global_config_path parsed from YAML under paths",
			content: `embeddings:
  mode: "local"
  local:
    model_path: "./models/model.onnx"
    vector_dim: 1024
paths:
  global_config_path: "data/ontology"
server: {}
`,
			want: "data/ontology",
		},
		{
			name: "global_config_path empty when absent",
			content: `embeddings:
  mode: "local"
  local:
    model_path: "./models/model.onnx"
    vector_dim: 1024
paths: {}
server: {}
`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgFile := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(cfgFile, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			cfg, err := config.Load(cfgFile)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if cfg.Paths.GlobalConfigPath != tt.want {
				t.Errorf("Paths.GlobalConfigPath = %q, want %q", cfg.Paths.GlobalConfigPath, tt.want)
			}
		})
	}
}

func TestAutoRebuildVectors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		content  string
		wantBool bool
	}{
		{
			name: "default false when not set",
			content: `embeddings:
  mode: "local"
  local:
    model_path: "./models/model.onnx"
    vector_dim: 1024
paths: {}
server: {}
`,
			wantBool: false,
		},
		{
			name: "explicitly true",
			content: `embeddings:
  mode: "local"
  local:
    model_path: "./models/model.onnx"
    vector_dim: 1024
  auto_rebuild_vectors: true
paths: {}
server: {}
`,
			wantBool: true,
		},
		{
			name: "explicitly false",
			content: `embeddings:
  mode: "local"
  local:
    model_path: "./models/model.onnx"
    vector_dim: 1024
  auto_rebuild_vectors: false
paths: {}
server: {}
`,
			wantBool: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfgFile := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(cfgFile, []byte(tt.content), 0o644); err != nil {
				t.Fatalf("failed to write test config: %v", err)
			}

			cfg, err := config.Load(cfgFile)
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}

			if cfg.Embeddings.AutoRebuildVectors != tt.wantBool {
				t.Errorf("AutoRebuildVectors = %v, want %v", cfg.Embeddings.AutoRebuildVectors, tt.wantBool)
			}
		})
	}
}
