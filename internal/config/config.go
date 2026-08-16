// Package config provides loading and validation of application configuration from YAML files.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// LinkerConfig configures the cross-domain entity linker module.
type LinkerConfig struct {
	Disabled bool      `yaml:"disabled"`
	LLM      LLMConfig `yaml:"llm"`
}

// Domains — always []string. YAML: domain: ["hr", "engineering"]
type Domains []string

// First returns the first domain or empty string if empty.
func (d Domains) First() string {
	if len(d) == 0 {
		return ""
	}
	return d[0]
}

// Contains reports whether dom is in the domains list.
func (d Domains) Contains(dom string) bool {
	for _, v := range d {
		if v == dom {
			return true
		}
	}
	return false
}

// NonEmpty reports whether the domains list has at least one entry.
func (d Domains) NonEmpty() bool {
	return len(d) > 0
}

// Config holds the complete application configuration.
type Config struct {
	Database   DatabaseConfig   `yaml:"database"`
	Embeddings EmbeddingsConfig `yaml:"embeddings"`
	Ingestion  IngestionConfig  `yaml:"ingestion"`
	Paths      PathsConfig      `yaml:"paths"`
	Server     ServerConfig     `yaml:"server"`
	Search     SearchConfig     `yaml:"search"`
	Graph      GraphConfig      `yaml:"graph"`
	AutoUpdate AutoUpdateConfig `yaml:"auto_update"`
	Scheduler  SchedulerConfig  `yaml:"scheduler"`
	Logging    LoggingConfig    `yaml:"logging"`
	Linker     LinkerConfig     `yaml:"linker"`

	// ONNX is loaded from the external onnx.yaml file (not from main config YAML).
	ONNX *ONNXConfig

	// autoUpdateConfigured tracks whether the auto_update section was present
	// in YAML, so ApplyDefaults can tell "absent" (enable) from "explicitly disabled".
	autoUpdateConfigured bool
}

// IngestionConfig defines document ingestion pipeline settings.
type IngestionConfig struct {
	Chunking  ChunkingConfig `yaml:"chunking"`
	NER       NERConfig      `yaml:"ner"`
	BatchSize int            `yaml:"batch_size"` // batch size for embedding generation, default 100
	Resolver  ResolverConfig `yaml:"resolver"`   // entity resolver settings
}

// ResolverConfig configures entity resolution (deduplication) thresholds.
type ResolverConfig struct {
	SimilarityThreshold float64 `yaml:"similarity_threshold"` // Jaro-Winkler similarity threshold for entity merging, default 0.8
}

// SourceConfig defines a single data source for ingestion.
type SourceConfig struct {
	Path     string  `yaml:"path"`
	Type     string  `yaml:"type"`              // "mediawiki", "webpages", "unstructured"
	Disabled bool    `yaml:"disabled"`          // false by default (source is enabled)
	Space    string  `yaml:"space,omitempty"`   // for mediawiki
	Domain   Domains `yaml:"domain,omitempty"`  // domain names (always array, matches keys in domains config)
	Dataset  string  `yaml:"dataset,omitempty"` // for unstructured
}

// xmlSourceElement is the intermediate XML representation of a source element.
// It handles attributes and nested <domains><domain>...</domain></domains>.
type xmlSourceElement struct {
	Path     string   `xml:"path,attr"`
	Type     string   `xml:"type,attr"`
	Disabled bool     `xml:"disabled,attr"`
	Space    string   `xml:"space,attr,omitempty"`
	Dataset  string   `xml:"dataset,attr,omitempty"`
	Domains  []string `xml:"domains>domain"`
}

// xmlSourceToConfig converts an XML source element to SourceConfig.
func xmlSourceToConfig(xs xmlSourceElement) SourceConfig {
	return SourceConfig{
		Path:     xs.Path,
		Type:     xs.Type,
		Disabled: xs.Disabled,
		Space:    xs.Space,
		Domain:   xs.Domains,
		Dataset:  xs.Dataset,
	}
}

// ChunkingConfig holds chunker-specific settings per format.
type ChunkingConfig struct {
	Markdown MarkdownChunkerConfig `yaml:"markdown"`
	JSON     JSONChunkerConfig     `yaml:"json"`
}

// MarkdownChunkerConfig configures the markdown chunker strategy.
type MarkdownChunkerConfig struct {
	Strategy       string `yaml:"strategy"`         // "headers", "fixed", "hybrid"
	MaxChunkSize   int    `yaml:"max_chunk_size"`   // 1000 chars default
	OverlapSize    int    `yaml:"overlap_size"`     // 100 chars default
	MinSectionSize int    `yaml:"min_section_size"` // 500 chars
}

// JSONChunkerConfig configures the JSON chunker.
type JSONChunkerConfig struct {
	TextFields    []string `yaml:"text_fields"`    // fields to index
	CombineFields bool     `yaml:"combine_fields"` // merge fields into single chunk per object
	MaxObjects    int      `yaml:"max_objects"`    // limit objects processed
}

type NERConfig struct {
	Disabled bool           `yaml:"disabled"`
	Prose    ProseNERConfig `yaml:"prose,omitempty"`
	LLM      LLMConfig      `yaml:"llm,omitempty"`
}

type ProseNERConfig struct {
	EnablePOS      bool     `yaml:"enable_pos"`
	EnableNER      bool     `yaml:"enable_ner"`
	CustomPatterns []string `yaml:"custom_patterns"`
	EntityTypes    []string `yaml:"entity_types"` // filter entity types
	// Confidence thresholds for entity filtering
	MinConfidence         float64 `yaml:"min_confidence"`          // minimum confidence for any entity (default 0.5)
	LocationMinConfidence float64 `yaml:"location_min_confidence"` // higher threshold for LOCATION to filter common nouns (default 0.9)
}

// LLMConfig configures the LLM-based NER provider.
type LLMConfig struct {
	APIBaseURL     string  `yaml:"api_base_url"`
	APIKey         string  `yaml:"api_key"` // Bearer token; empty if not required
	ModelName      string  `yaml:"model_name"`
	Temperature    float64 `yaml:"temperature"` // 0.0 for determinism
	MaxTokens      int     `yaml:"max_tokens"`
	Seed           int     `yaml:"seed"`            // for determinism
	ResponseFormat string  `yaml:"response_format"` // "json_object" (default) or "json_schema"
	TimeoutMs      int     `yaml:"timeout_ms"`      // HTTP request timeout in ms, default 60000
	MaxRetries     int     `yaml:"max_retries"`     // max retry attempts after initial failure, default 3
}

// EmbeddingsConfig defines embedding provider settings.
type EmbeddingsConfig struct {
	Mode               string         `yaml:"mode"` // "local" or "api"
	Local              LocalEmbedding `yaml:"local,omitempty"`
	API                APIEmbedding   `yaml:"api,omitempty"`
	AutoRebuildVectors bool           `yaml:"auto_rebuild_vectors"` // auto-recreate vectors on dimension mismatch
}

// LocalEmbedding holds configuration for the local ONNX embedding provider.
type LocalEmbedding struct {
	ModelName     string `yaml:"model_name"` // model name from registry, e.g. "bge-m3-int8"
	ModelPath     string `yaml:"model_path"` // explicit path overrides model_name resolution
	TokenizerPath string `yaml:"tokenizer_path,omitempty"`
	VectorDim     int    `yaml:"vector_dim"`
}

// APIEmbedding holds configuration for the remote API embedding provider.
type APIEmbedding struct {
	BaseURL    string `yaml:"base_url"`
	APIKey     string `yaml:"api_key"`
	ModelName  string `yaml:"model_name"`
	VectorDim  int    `yaml:"vector_dim"`
	MaxRetries int    `yaml:"max_retries"`
	TimeoutMs  int    `yaml:"timeout_ms"`
}

// PathsConfig defines filesystem paths used by the application.
type PathsConfig struct {
	DataDir          string `yaml:"data_dir"`           // data directory for DB, cache, etc.
	DocumentsDir     string `yaml:"documents_dir"`      // documents storage directory
	MigrationsDir    string `yaml:"migrations_dir"`     // database migrations directory
	GlobalConfigPath string `yaml:"global_config_path"` // ontology directory (contains global.xml and domains/)
	PromptsPath      string `yaml:"prompts_path"`       // prompt template files directory, default "configs/prompts"
	ONNXConfigPath   string `yaml:"onnx_config"`        // path to onnx.yaml config file
}

// ServerConfig holds MCP server settings.
type ServerConfig struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Host    string `yaml:"host"` // HTTP listen host, default 0.0.0.0
	Port    int    `yaml:"port"` // HTTP listen port, default 8080
}

// SearchConfig configures hybrid search behavior.
type SearchConfig struct {
	RRFK            int                `yaml:"rrf_k"`            // RRF constant k, default 20
	LexicalTopK     int                `yaml:"lexical_top_k"`    // top-K for lexical (FTS5), default 20
	SemanticTopK    int                `yaml:"semantic_top_k"`   // top-K for semantic (vector), default 20
	FinalTopK       int                `yaml:"final_top_k"`      // final merged top-K, default 10
	EnableLexical   bool               `yaml:"enable_lexical"`   // enable FTS5 search, default true
	EnableSemantic  bool               `yaml:"enable_semantic"`  // enable vector search, default true
	TimeoutMs       int                `yaml:"timeout_ms"`       // per-search timeout in ms, default 10000
	DeprecatedBoost float64            `yaml:"deprecated_boost"` // boost penalty for deprecated content
	OfficialBoost   float64            `yaml:"official_boost"`   // boost for official sources
	RecentBoost     float64            `yaml:"recent_boost"`     // boost for recent content
	RecentDays      int                `yaml:"recent_days"`      // days threshold for recent boost
	AuthorityBoost  map[string]float64 `yaml:"authority_boost"`  // authority-based boost factors
}

// DatabaseConfig holds SQLite database settings.
type DatabaseConfig struct {
	Path      string            `yaml:"path"`       // path to SQLite database file
	CachePath string            `yaml:"cache_path"` // explicit cache DB path (overrides default)
	Pragma    map[string]string `yaml:"pragma"`     // custom PRAGMA overrides
}

// GraphConfig configures the knowledge graph module.
type GraphConfig struct {
	EnableGraph   bool `yaml:"enable_graph"`    // enable graph traversal, default true
	MaxDepth      int  `yaml:"max_depth"`       // max BFS depth, default 5
	MaxNodes      int  `yaml:"max_nodes"`       // max nodes to return per query, default 1000
	LoadOnStartup bool `yaml:"load_on_startup"` // load graph into memory on server start, default true
}

// AutoUpdateConfig configures automatic file watching and re-indexing during serve mode.
type AutoUpdateConfig struct {
	Enabled         bool `yaml:"enabled"`          // enable filesystem monitoring, default true
	DebounceSeconds int  `yaml:"debounce_seconds"` // minimum interval between re-indexings, default 30
	WatchSources    bool `yaml:"watch_sources"`    // watch all sources from ingestion.sources, default true
	InitialSync     bool `yaml:"initial_sync"`     // run a full source scan on startup, default true
}

// SchedulerConfig configures the universal job scheduler (gocron) used during
// serve mode. Jobs are registered by name; each job can be enabled/disabled
// and given its own interval.
type SchedulerConfig struct {
	Jobs map[string]JobConfig `yaml:"jobs"`
}

// JobConfig configures a single scheduled job.
type JobConfig struct {
	Enabled         bool `yaml:"enabled"`          // whether the job is scheduled, default true
	IntervalSeconds int  `yaml:"interval_seconds"` // run interval in seconds, default 300
}

// LoggingConfig configures application logging.
type LoggingConfig struct {
	Level  string `yaml:"level"`  // "debug", "info", "warn", "error", default "info"
	Format string `yaml:"format"` // "console" or "json", default "console"
	Output string `yaml:"output"` // "stderr" or "stdout", default "stderr"
}

// ONNXConfig holds external ONNX configuration loaded from onnx.yaml.
type ONNXConfig struct {
	Runtime ONNXRuntimeConfig `yaml:"runtime"`
	Models  ONNXModelsConfig  `yaml:"models"`
}

// ONNXRuntimeConfig holds ONNX Runtime version and platform definitions.
type ONNXRuntimeConfig struct {
	Version   string               `yaml:"version"`
	Platforms []ONNXPlatformConfig `yaml:"platforms"`
}

// ONNXPlatformConfig defines download info for a single platform.
type ONNXPlatformConfig struct {
	Key           string `yaml:"key"`
	OS            string `yaml:"os"`
	Arch          string `yaml:"arch"`
	ArchiveURL    string `yaml:"archive_url"`
	ArchiveFormat string `yaml:"archive_format"` // "zip" or "tgz"
	LibraryName   string `yaml:"library_name"`
	LibraryPath   string `yaml:"library_path"`
}

// ONNXModelsConfig holds model registry and default model name.
type ONNXModelsConfig struct {
	Default string      `yaml:"default"`
	Entries []ModelInfo `yaml:"entries"`
}

// ModelFile describes a single file belonging to an embedding model.
type ModelFile struct {
	Name      string `yaml:"name"`
	URL       string `yaml:"url"`
	SizeBytes int64  `yaml:"size_bytes"`         // exact size in bytes, 0 if unknown
	Checksum  string `yaml:"checksum,omitempty"` // "sha256:hex" format
}

// ModelInfo describes a model available in the registry.
type ModelInfo struct {
	Name        string      `yaml:"name"`
	DisplayName string      `yaml:"display_name"`
	Description string      `yaml:"description"`
	Version     string      `yaml:"version"`
	VectorDim   int         `yaml:"vector_dim"`
	Files       []ModelFile `yaml:"files"`
	Source      string      `yaml:"source"` // "huggingface", "github"
	Repo        string      `yaml:"repo,omitempty"`
}

// LoadONNXConfig reads and parses the external ONNX YAML configuration file.
func LoadONNXConfig(path string) (ONNXConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ONNXConfig{}, fmt.Errorf("read onnx config %s: %w", path, err)
	}

	var cfg ONNXConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return ONNXConfig{}, fmt.Errorf("parse onnx YAML: %w", err)
	}

	return cfg, nil
}

// PlatformForKey returns the platform config matching the given key (e.g., "linux-amd64").
func (c *ONNXRuntimeConfig) PlatformForKey(key string) (*ONNXPlatformConfig, bool) {
	for i := range c.Platforms {
		if c.Platforms[i].Key == key {
			p := &c.Platforms[i]
			return p, true
		}
	}
	return nil, false
}

// ModelForName returns the model info matching the given name.
func (c *ONNXModelsConfig) ModelForName(name string) (*ModelInfo, bool) {
	for i := range c.Entries {
		if c.Entries[i].Name == name {
			m := &c.Entries[i]
			return m, true
		}
	}
	return nil, false
}

// Load reads and parses a YAML configuration file into a Config struct.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config file %s: %w", path, err)
	}

	var node yaml.Node
	if err := yaml.Unmarshal(data, &node); err != nil {
		return Config{}, fmt.Errorf("parse config YAML: %w", err)
	}

	var cfg Config
	cfg.detectAutoUpdatePresence(&node)
	if err := node.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse config YAML: %w", err)
	}

	return cfg, nil
}

// detectAutoUpdatePresence marks whether the top-level auto_update section
// appears in the parsed YAML document.
func (c *Config) detectAutoUpdatePresence(root *yaml.Node) {
	if root == nil || root.Kind != yaml.DocumentNode || len(root.Content) == 0 {
		return
	}
	mapping := root.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == "auto_update" {
			c.autoUpdateConfigured = true
			return
		}
	}
}

// Validate checks that the configuration contains all required fields.
func (c Config) Validate() error {
	switch c.Embeddings.Mode {
	case "local":
		if c.Embeddings.Local.ModelPath == "" && c.Embeddings.Local.ModelName == "" {
			return fmt.Errorf("embeddings.local.model_path or model_name is required in local mode")
		}
		if c.Embeddings.Local.VectorDim <= 0 {
			return fmt.Errorf("embeddings.local.vector_dim must be positive")
		}
	case "api":
		if c.Embeddings.API.BaseURL == "" {
			return fmt.Errorf("embeddings.api.base_url is required in api mode")
		}
		if c.Embeddings.API.ModelName == "" {
			return fmt.Errorf("embeddings.api.model_name is required in api mode")
		}
		if c.Embeddings.API.VectorDim <= 0 {
			return fmt.Errorf("embeddings.api.vector_dim must be positive")
		}
	default:
		return fmt.Errorf("unknown embeddings mode %q, want \"local\" or \"api\"", c.Embeddings.Mode)
	}

	return nil
}

// VectorDim returns the configured embedding vector dimension.
func (c Config) VectorDim() int {
	switch c.Embeddings.Mode {
	case "local":
		return c.Embeddings.Local.VectorDim
	case "api":
		return c.Embeddings.API.VectorDim
	default:
		return 0
	}
}

// DBPath returns the configured database path, falling back to default.
func (c Config) DBPath() string {
	if c.Database.Path != "" {
		return c.Database.Path
	}
	return filepath.Join(c.Paths.DataDir, "knowledge.db")
}

// CacheDBPath returns the cache database path. If explicitly configured via
// Database.CachePath it is used; otherwise a sibling of the main DB file
// named "cache.db" is returned (e.g., data/cache.db).
func (c Config) CacheDBPath() string {
	if c.Database.CachePath != "" {
		return c.Database.CachePath
	}
	return filepath.Join(filepath.Dir(c.DBPath()), "cache.db")
}

// ApplyDefaults fills zero-value fields with sensible defaults.
func (c *Config) ApplyDefaults() {
	if c.Ingestion.BatchSize <= 0 {
		c.Ingestion.BatchSize = 100
	}
	if c.Ingestion.Chunking.Markdown.Strategy == "" {
		c.Ingestion.Chunking.Markdown.Strategy = "headers"
	}
	if c.Ingestion.Chunking.Markdown.MaxChunkSize <= 0 {
		c.Ingestion.Chunking.Markdown.MaxChunkSize = 1000
	}
	if c.Ingestion.Chunking.Markdown.OverlapSize < 0 {
		c.Ingestion.Chunking.Markdown.OverlapSize = 100
	}
	if c.Ingestion.Chunking.Markdown.MinSectionSize <= 0 {
		c.Ingestion.Chunking.Markdown.MinSectionSize = 500
	}
	if len(c.Ingestion.Chunking.JSON.TextFields) == 0 {
		c.Ingestion.Chunking.JSON.TextFields = []string{"description", "title", "wikitext", "html"}
	}

	// Search defaults.
	if c.Search.RRFK <= 0 {
		c.Search.RRFK = 20 // Calibrated k: lower value increases rank sensitivity (~8× vs k=60).
	}
	if c.Search.LexicalTopK <= 0 {
		c.Search.LexicalTopK = 20
	}
	if c.Search.SemanticTopK <= 0 {
		c.Search.SemanticTopK = 20
	}
	if c.Search.FinalTopK <= 0 {
		c.Search.FinalTopK = 10
	}
	if !c.Search.EnableLexical && !c.Search.EnableSemantic {
		c.Search.EnableLexical = true
		c.Search.EnableSemantic = true
	}
	if c.Search.TimeoutMs <= 0 {
		c.Search.TimeoutMs = 10000
	}

	// Graph defaults.
	if c.Graph.MaxDepth <= 0 {
		c.Graph.MaxDepth = 5
	}
	if c.Graph.MaxNodes <= 0 {
		c.Graph.MaxNodes = 1000
	}
	if !c.Graph.LoadOnStartup {
		c.Graph.LoadOnStartup = true
	}

	// Auto-update defaults.
	if c.AutoUpdate.DebounceSeconds <= 0 {
		c.AutoUpdate.DebounceSeconds = 30
	}
	if !c.AutoUpdate.WatchSources {
		c.AutoUpdate.WatchSources = true
	}
	// Unset sections default to enabled; explicit `auto_update:` in YAML is respected.
	if !c.autoUpdateConfigured {
		c.AutoUpdate.Enabled = true
		c.AutoUpdate.InitialSync = true
	}

	// Scheduler defaults
	if c.Scheduler.Jobs == nil {
		c.Scheduler.Jobs = make(map[string]JobConfig)
	}

	// The orphan_cleanup job runs every 3600s by default and is disabled by
	// default since it performs full-table scans on large databases. A zero-value
	// entry means the job was not configured in YAML at all; any explicit
	// configuration (enabled or interval_seconds) is respected.
	orphanCleanup := c.Scheduler.Jobs["orphan_cleanup"]
	if orphanCleanup == (JobConfig{}) {
		orphanCleanup.Enabled = false
	}
	if orphanCleanup.IntervalSeconds <= 0 {
		orphanCleanup.IntervalSeconds = 3600
	}
	c.Scheduler.Jobs["orphan_cleanup"] = orphanCleanup

	// Logging defaults.
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
	if c.Logging.Format == "" {
		c.Logging.Format = "console"
	}
	if c.Logging.Output == "" {
		c.Logging.Output = "stderr"
	}

	// Paths defaults.
	if c.Paths.DataDir == "" {
		c.Paths.DataDir = "data"
	}
	if c.Paths.DocumentsDir == "" {
		c.Paths.DocumentsDir = "documents"
	}
	if c.Paths.MigrationsDir == "" {
		c.Paths.MigrationsDir = "migrations"
	}
	if c.Paths.PromptsPath == "" {
		c.Paths.PromptsPath = "configs/prompts"
	}
	if c.Paths.ONNXConfigPath == "" {
		c.Paths.ONNXConfigPath = "configs/onnx.yaml"
	}

	// LLM NER defaults.
	if c.Ingestion.NER.LLM.ResponseFormat == "" {
		c.Ingestion.NER.LLM.ResponseFormat = "json_object"
	}
	if c.Ingestion.NER.LLM.TimeoutMs <= 0 {
		c.Ingestion.NER.LLM.TimeoutMs = 60000
	}
	if c.Ingestion.NER.LLM.MaxRetries <= 0 {
		c.Ingestion.NER.LLM.MaxRetries = 3
	}

	// Resolver defaults.
	if c.Ingestion.Resolver.SimilarityThreshold <= 0 {
		c.Ingestion.Resolver.SimilarityThreshold = 0.8
	}

	// Local embedding defaults.
	if c.Embeddings.Local.ModelName == "" && c.Embeddings.Local.ModelPath == "" {
		c.Embeddings.Local.ModelName = "bge-m3-int8"
	}

	// Server defaults.
	if c.Server.Name == "" {
		c.Server.Name = "synopsis"
	}
	if c.Server.Version == "" {
		c.Server.Version = "0.1.0-dev"
	}
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Server.Port <= 0 {
		c.Server.Port = 8080
	}

	// Reranker defaults.
	if c.Search.DeprecatedBoost <= 0 {
		c.Search.DeprecatedBoost = 0.2
	}
	if c.Search.OfficialBoost <= 0 {
		c.Search.OfficialBoost = 1.5
	}
	if c.Search.RecentBoost <= 0 {
		c.Search.RecentBoost = 1.2
	}
	if c.Search.RecentDays <= 0 {
		c.Search.RecentDays = 90
	}
	if c.Search.AuthorityBoost == nil {
		c.Search.AuthorityBoost = map[string]float64{
			"default": 1.0,
		}
	}
}
