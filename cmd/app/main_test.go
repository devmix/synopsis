package main_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/config"
)

func TestConfigVectorDim(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  config.Config
		want int
	}{
		{
			name: "local mode",
			cfg: config.Config{
				Embeddings: config.EmbeddingsConfig{
					Mode:  "local",
					Local: config.LocalEmbedding{VectorDim: 1024},
				},
			},
			want: 1024,
		},
		{
			name: "api mode",
			cfg: config.Config{
				Embeddings: config.EmbeddingsConfig{
					Mode: "api",
					API:  config.APIEmbedding{VectorDim: 3072},
				},
			},
			want: 3072,
		},
		{
			name: "unknown mode returns zero",
			cfg: config.Config{
				Embeddings: config.EmbeddingsConfig{Mode: "unknown"},
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.cfg.VectorDim()
			if got != tt.want {
				t.Errorf("VectorDim() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestConfigDBPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  config.Config
		want string
	}{
		{
			name: "explicit database path",
			cfg: config.Config{
				Database: config.DatabaseConfig{Path: "/custom/path.db"},
			},
			want: "/custom/path.db",
		},
		{
			name: "default from data_dir",
			cfg: config.Config{
				Paths: config.PathsConfig{DataDir: "data"},
			},
			want: filepath.Join("data", "knowledge.db"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.cfg.DBPath()
			if got != tt.want {
				t.Errorf("DBPath() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfigApplyDefaults(t *testing.T) {
	t.Parallel()

	cfg := config.Config{}
	cfg.ApplyDefaults()

	if cfg.Ingestion.BatchSize != 100 {
		t.Errorf("BatchSize = %d, want 100", cfg.Ingestion.BatchSize)
	}
	if cfg.Search.RRFK != 20 {
		t.Errorf("RRFK = %d, want 20", cfg.Search.RRFK)
	}
	if cfg.Graph.MaxDepth != 5 {
		t.Errorf("Graph.MaxDepth = %d, want 5", cfg.Graph.MaxDepth)
	}
	if cfg.Graph.MaxNodes != 1000 {
		t.Errorf("Graph.MaxNodes = %d, want 1000", cfg.Graph.MaxNodes)
	}
	if !cfg.Graph.LoadOnStartup {
		t.Error("Graph.LoadOnStartup should default to true")
	}
	if cfg.AutoUpdate.DebounceSeconds != 30 {
		t.Errorf("AutoUpdate.DebounceSeconds = %d, want 30", cfg.AutoUpdate.DebounceSeconds)
	}
	if !cfg.AutoUpdate.WatchSources {
		t.Error("AutoUpdate.WatchSources should default to true")
	}
	if !cfg.AutoUpdate.Enabled {
		t.Error("AutoUpdate.Disabled should default to true")
	}
	if !cfg.AutoUpdate.InitialSync {
		t.Error("AutoUpdate.InitialSync should default to true")
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("Logging.Level = %q, want \"info\"", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "console" {
		t.Errorf("Logging.Format = %q, want \"console\"", cfg.Logging.Format)
	}
	if cfg.Logging.Output != "stderr" {
		t.Errorf("Logging.Output = %q, want \"stderr\"", cfg.Logging.Output)
	}
	if cfg.Paths.DataDir != "data" {
		t.Errorf("Paths.DataDir = %q, want \"data\"", cfg.Paths.DataDir)
	}
	if cfg.Server.Name != "synopsis" {
		t.Errorf("Server.Name = %q, want \"synopsis\"", cfg.Server.Name)
	}
	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("Server.Host = %q, want \"0.0.0.0\"", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
}

func TestConfigLoadFullExample(t *testing.T) {
	t.Parallel()

	yamlContent := `database:
  path: "./data/knowledge.db"
embeddings:
  mode: "local"
  local:
    model_path: "./models/bge-m3-int8.onnx"
    vector_dim: 1024
ingestion:
  sources:
    - path: "./workspace/maps"
      type: "markdown"
      enabled: true
search:
  rrf_k: 20
graph:
  enable_graph: true
  max_depth: 5
auto_update:
  enabled: false
  debounce_seconds: 30
logging:
  level: "debug"
paths:
  data_dir: "data"
server:
  name: "synopsis"
`

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Database.Path != "./data/knowledge.db" {
		t.Errorf("Database.Path = %q, want \"./data/knowledge.db\"", cfg.Database.Path)
	}
	if cfg.Graph.MaxDepth != 5 {
		t.Errorf("Graph.MaxDepth = %d, want 5", cfg.Graph.MaxDepth)
	}
	if cfg.AutoUpdate.DebounceSeconds != 30 {
		t.Errorf("AutoUpdate.DebounceSeconds = %d, want 30", cfg.AutoUpdate.DebounceSeconds)
	}
	if cfg.AutoUpdate.Enabled {
		t.Error("AutoUpdate.Disabled should stay false when explicitly configured")
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("Logging.Level = %q, want \"debug\"", cfg.Logging.Level)
	}
}

func TestConfigLoadDefaultsAbsentSections(t *testing.T) {
	t.Parallel()

	yamlContent := `database:
  path: "./data/knowledge.db"
embeddings:
  mode: "local"
  local:
    model_path: "./models/bge-m3-int8.onnx"
    vector_dim: 1024
ingestion:
  sources:
    - path: "./workspace/maps"
      type: "markdown"
logging:
  level: "info"
`

	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgFile, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	cfg.ApplyDefaults()

	if !cfg.AutoUpdate.Enabled {
		t.Error("AutoUpdate.Disabled should default to true when auto_update section is absent")
	}
	if !cfg.AutoUpdate.InitialSync {
		t.Error("AutoUpdate.InitialSync should default to true when auto_update section is absent")
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.Port)
	}
}
