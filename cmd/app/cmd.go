// Package main implements the entry point for the Synopsis RAG service.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/devmix/synopsis/internal/cache"
	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database"
	"github.com/devmix/synopsis/internal/domain"
	"github.com/devmix/synopsis/internal/ingestion/runner"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/onnx"
)

// bootstrap loads and validates the configuration, initializes the logger,
// discovers domains from the ontology directory, and returns the effective
// database path along with a domain registry.
func bootstrap(cfgPath, dbPath string) (config.Config, *logger.Logger, *domain.DomainRegistry) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config %s: %v\n", cfgPath, err)
		os.Exit(1)
	}

	cfg.ApplyDefaults()

	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// Create the logger before domain discovery so merge warnings are reported.
	logLevel := cfg.Logging.Level
	if logLevel == "" {
		logLevel = "info"
	}

	log, err := logger.New(logger.Options{Level: logLevel})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	// Discover domains from ontology directory (global pool merged in).
	domainRegistry, err := domain.DiscoveryWithLogger(cfg.Paths.GlobalConfigPath, log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "domain discovery failed: %v\n", err)
		os.Exit(1)
	}

	// Load ONNX configuration from external file.
	onnxCfg, err := config.LoadONNXConfig(cfg.Paths.ONNXConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load ONNX config %s: %v\n", cfg.Paths.ONNXConfigPath, err)
		os.Exit(1)
	}
	cfg.ONNX = &onnxCfg

	// Effective database path: CLI flag > config file.
	if dbPath != "" {
		cfg.Database.Path = dbPath
	}

	log.Infow("configuration loaded", "config", cfgPath, "database", cfg.DBPath())

	return cfg, log, domainRegistry
}

// openDatabase opens the SQLite database and applies pending migrations.
// If a vector dimension mismatch is detected, it returns the error instead of
// calling log.Fatal so the caller can decide whether to auto-rebuild vectors.
func openDatabase(log *logger.Logger, cfg config.Config) (*database.Database, error) {
	db, err := database.Open(cfg.DBPath(), cfg.VectorDim(),
		database.WithMigrationsDir(cfg.Paths.MigrationsDir))
	if err != nil {
		log.Fatal("open database", logger.Err(err))
	}

	if err := db.Migrate(context.Background()); err != nil {
		if database.IsDimensionMismatchError(err) {
			return db, err
		}
		log.Fatal("run migrations", logger.Err(err))
	}
	log.Infow("database ready", "path", cfg.DBPath())

	return db, nil
}

// ensureEmbeddingModel provisions the configured local embedding model,
// auto-downloading it on first use. It is a no-op in api mode.
func ensureEmbeddingModel(log *logger.Logger, cfg *config.Config) {
	if cfg.Embeddings.Mode != "local" {
		return
	}

	// Skip auto-download if ModelPath is explicitly set (legacy mode).
	if cfg.Embeddings.Local.ModelPath != "" {
		log.Infow("using explicit model path", "path", cfg.Embeddings.Local.ModelPath)
		return
	}

	modelName := cfg.Embeddings.Local.ModelName
	if modelName == "" {
		modelName = "bge-m3-int8"
	}

	manager, err := onnx.NewModelManager(cfg.Paths.DataDir, cfg.ONNX)
	if err != nil {
		log.Fatal("init model manager", logger.Err(err))
	}

	modelPath, err := manager.EnsureModel(modelName)
	if err != nil {
		log.Fatal("ensure model", logger.Err(err))
	}

	cfg.Embeddings.Local.ModelPath = modelPath
	log.Infow("model ensured", "name", modelName, "path", modelPath)
}

// openCacheStore opens the separate cache database. Returns nil (not an error)
// if the cache DB cannot be opened — the application continues without caching.
func openCacheStore(log *logger.Logger, cfg config.Config) *cache.Store {
	store, err := cache.NewStore(cfg.CacheDBPath())
	if err != nil {
		log.Warn("open cache database (continuing without cache)",
			"path", cfg.CacheDBPath(), "error", err)
		return nil
	}
	log.Infow("cache database opened", "path", cfg.CacheDBPath())
	return store
}

// rebuildVectorsIfNeeded checks for a vector dimension mismatch and, if detected,
// either auto-rebuilds embeddings or exits fatally. When cliRebuild is true
// (full --rebuild mode), re-embed is skipped because the caller will reset everything anyway.
// Returns true if vectors were rebuilt, false otherwise.
func rebuildVectorsIfNeeded(
	ctx context.Context,
	log *logger.Logger,
	cfg config.Config,
	db *database.Database,
	cacheStore *cache.Store,
	domainRegistry *domain.DomainRegistry,
	openErr error,
	autoRebuildVectors bool,
	cliRebuild bool,
) (bool, error) {
	if openErr == nil || cliRebuild {
		return false, nil
	}

	if !autoRebuildVectors {
		log.Fatal("vector dimension mismatch detected; run with --auto-rebuild-vectors flag (or set embeddings.auto_rebuild_vectors in config) to automatically rebuild vectors, or delete data/knowledge.db",
			logger.Err(openErr))
	}

	ingRunner, err := runner.NewRunner(cfg, db.DB(), log,
		runner.WithCacheStore(cacheStore),
		runner.WithDomainRegistry(domainRegistry),
	)
	if err != nil {
		log.Fatal("create ingestion runner for re-embed", logger.Err(err))
	}
	defer ingRunner.Close() //nolint:errcheck

	if err := runner.ReEmbedChunks(ctx, db, ingRunner.EmbeddingProvider(), log); err != nil {
		log.Fatal("re-embed chunks", logger.Err(err))
	}

	return true, nil
}
