// Package main implements the entry point for the Synopsis RAG service.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/embedding"
	"github.com/devmix/synopsis/internal/graph"
	"github.com/devmix/synopsis/internal/ingestion/runner"
	"github.com/devmix/synopsis/internal/logger"
	mcpserver "github.com/devmix/synopsis/internal/mcp"
	"github.com/devmix/synopsis/internal/scheduler"
	"github.com/devmix/synopsis/internal/search"
	"github.com/devmix/synopsis/internal/watcher"
)

// runServe starts the unified mode: an initial full sync (unless disabled by
// config or --no-initial-sync), the MCP server over HTTP (SSE transport), and
// a file watcher that incrementally re-indexes documents as files change.
func runServe(cfgPath, dbPath string, noInitialSync bool, port int, autoRebuildVectorsCLI bool) {
	cfg, log, domainRegistry := bootstrap(cfgPath, dbPath)
	defer log.Sync() //nolint:errcheck

	if port > 0 {
		cfg.Server.Port = port
	}

	// CLI flag overrides config.
	autoRebuildVectors := autoRebuildVectorsCLI || cfg.Embeddings.AutoRebuildVectors

	ctx := context.Background()

	db, openErr := openDatabase(log, cfg)
	if db != nil {
		defer db.Close() //nolint:errcheck
	}

	ensureEmbeddingModel(log, &cfg)

	cacheStore := openCacheStore(log, cfg)

	// Handle vector dimension mismatch.
	rebuilt, _ := rebuildVectorsIfNeeded(ctx, log, cfg, db, cacheStore, domainRegistry, openErr, autoRebuildVectors, false)
	if rebuilt {
		log.Info("vectors rebuilt due to dimension mismatch")
	}

	// Health check on startup (after model is ensured so embedding check works).
	if err := runHealthCheck(log, cfg, db); err != nil {
		log.Warn("startup health check", logger.Err(err))
	}

	// Shared ingestion runner: used for the initial sync and for every
	// watcher-triggered incremental update.
	ingRunner, err := runner.NewRunner(cfg, db.DB(), log,
		runner.WithCacheStore(cacheStore),
		runner.WithDomainRegistry(domainRegistry),
	)
	if err != nil {
		log.Fatal("create ingestion runner", logger.Err(err))
	}
	defer ingRunner.Close() //nolint:errcheck

	// Initial sync: scan all sources so the index is up to date when the
	// server starts accepting requests.
	if cfg.AutoUpdate.InitialSync && !noInitialSync {
		log.Info("initial sync started")
		initialStats := ingRunner.IngestAll(ctx, false)
		if len(initialStats.Errors) > 0 {
			log.Warn("initial sync completed with errors", "errors", len(initialStats.Errors))
		}
		log.Infow("initial sync finished",
			"documents_created", initialStats.DocumentsCreated,
			"documents_updated", initialStats.DocumentsUpdated,
			"documents_skipped", initialStats.DocumentsSkipped,
			"sources", initialStats.SourcesProcessed,
		)
	}

	// Universal job scheduler (gocron): periodic background tasks
	var sched *scheduler.Scheduler
	if jobCfg, ok := cfg.Scheduler.Jobs["orphan_cleanup"]; ok && jobCfg.Enabled {
		s, err := scheduler.New()
		if err != nil {
			log.Warn("create scheduler", logger.Err(err))
		} else {
			sched = s
		}
	}

	// Register orphan_cleanup job if enabled.
	if sched != nil {
		if jobCfg, ok := cfg.Scheduler.Jobs["orphan_cleanup"]; ok && jobCfg.Enabled {
			if err := sched.Register("orphan_cleanup", jobCfg.IntervalSeconds, func(jobCtx context.Context) {
				stats, err := ingRunner.CleanupOrphanedData(jobCtx)
				if err != nil {
					log.Warn("orphan_cleanup job", logger.Err(err))
					return
				}
				if stats.EntitiesDeleted > 0 || stats.FactsDeleted > 0 {
					log.Infow("orphan_cleanup job finished",
						"entities_deleted", stats.EntitiesDeleted,
						"facts_deleted", stats.FactsDeleted,
						"errors", len(stats.Errors),
					)
				}
			}); err != nil {
				log.Warn("register orphan_cleanup job", logger.Err(err))
			}
		}
	}

	if sched != nil {
		sched.Start()
		log.Infow("scheduler started")
	}

	// Create DAOs for searcher.
	chunkDAO := dao.NewChunkDAO(db.DB())
	docDAO := dao.NewDocumentDAO(db.DB())
	chunkEntityDAO := dao.NewChunkEntityDAO(db.DB())

	// Load knowledge graph (before creating searcher if graph expansion is enabled).
	var g *graph.Graph
	if cfg.Graph.EnableGraph && cfg.Graph.LoadOnStartup {
		g = loadGraph(ctx, log, db)
	}

	// Create searcher.
	searcher := search.NewSearcher(
		db.DB(),
		chunkDAO,
		docDAO,
		chunkEntityDAO,
		ingRunner.EmbeddingProvider(),
		cfg.Search,
		cfg.Graph,
		g,
	)
	log.Info("searcher ready")

	// Create MCP server.
	mcpSrv, err := mcpserver.NewServer(cfg, db, searcher, g, mcpserver.WithLogger(log))
	if err != nil {
		log.Fatal("create MCP server", logger.Err(err))
	}
	defer mcpSrv.Close() //nolint:errcheck

	// Set up auto-update file watcher.
	var fileWatcher *watcher.FileWatcher
	if cfg.AutoUpdate.Enabled && cfg.AutoUpdate.WatchSources {
		fw, err := setupFileWatcher(log, cfg, ingRunner, db, searcher, mcpSrv)
		if err != nil {
			log.Warn("setup file watcher", logger.Err(err))
		} else {
			fileWatcher = fw
			defer func() {
				if err := fw.Stop(); err != nil {
					log.Error("stop file watcher", logger.Err(err))
				}
			}()
		}
	}

	log.Infow("MCP server starting",
		"url", fmt.Sprintf("http://%s:%d", cfg.Server.Host, cfg.Server.Port),
		"endpoints", "GET /sse, POST /message, GET /health",
	)

	// Set up signal handling for graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Run MCP server in a goroutine so we can also handle signals and file watcher.
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- mcpSrv.Run(ctx)
	}()

	// Start file watcher if configured.
	if fileWatcher != nil {
		if err := fileWatcher.Start(ctx); err != nil {
			log.Error("start file watcher", logger.Err(err))
		} else {
			log.Info("file watcher started")
		}
	}

	// Wait for server to finish or signal.
	select {
	case <-sigCh:
		log.Info("received shutdown signal, stopping...")
	case err := <-serverDone:
		if err != nil {
			log.Error("MCP server error", logger.Err(err))
		}
	}

	// Graceful shutdown with timeout.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	// Stop the job scheduler concurrently with the MCP server shutdown.
	// ShutdownWithContext waits for any running job to finish; the WaitGroup
	// ensures the scheduler is fully stopped before the process exits.
	var schedWG sync.WaitGroup
	if sched != nil {
		schedWG.Add(1)
		go func() {
			defer schedWG.Done()
			if err := sched.Shutdown(shutdownCtx); err != nil {
				log.Warn("scheduler shutdown", logger.Err(err))
			}
		}()
	}

	// The MCP server's Run method already handles signal.NotifyContext internally,
	// so it should stop on its own. We just wait for it to finish.
	select {
	case err := <-serverDone:
		if err != nil && ctx.Err() == nil {
			log.Error("MCP server shutdown error", logger.Err(err))
		}
	case <-shutdownCtx.Done():
		log.Warn("forced shutdown after timeout")
	}

	// Wait for the scheduler to finish shutting down.
	schedWG.Wait()
	if sched != nil {
		log.Info("scheduler stopped gracefully")
	}

	log.Info("MCP server stopped gracefully")
}

// loadGraph builds the in-memory knowledge graph from the database.
func loadGraph(ctx context.Context, log *logger.Logger, db *database.Database) *graph.Graph {
	g, stats, err := graph.NewGraphFromDB(ctx, db.DB())
	if err != nil {
		log.Fatal("load knowledge graph", logger.Err(err))
	}
	log.Infow("knowledge graph loaded",
		"nodes", stats.NodeCount,
		"edges", stats.EdgeCount,
	)
	return g
}

// runHealthCheck performs a startup health check on all components.
func runHealthCheck(log *logger.Logger, cfg config.Config, db *database.Database) error {
	log.Info("startup health check started")

	// 1. Database connectivity.
	if err := db.DB().Ping(); err != nil {
		log.Error("startup health check", "component", "database", "status", "fail", logger.Err(err))
		return fmt.Errorf("database ping: %w", err)
	}
	log.Infow("startup health check", "component", "database", "status", "ok")

	// 2. Check for existing data.
	docDAO := dao.NewDocumentDAO(db.DB())
	count, err := docDAO.Count(context.Background())
	if err != nil {
		log.Warn("startup health check", "component", "document_count", logger.Err(err))
	} else {
		log.Infow("startup health check", "component", "documents", "status", "ok", "count", count)
	}

	// 3. Check embedding provider (no cache store for health check).
	_, err = embedding.NewProvider(cfg.Embeddings, cfg.Paths.DataDir, log, cfg.ONNX)
	if err != nil {
		log.Warn("startup health check", "component", "embedding_provider", logger.Err(err))
	} else {
		log.Infow("startup health check", "component", "embedding_provider", "status", "ok")
	}

	return nil
}

// setupFileWatcher creates and configures a file watcher for auto-update.
func setupFileWatcher(
	log *logger.Logger,
	cfg config.Config,
	ingRunner *runner.Runner,
	db *database.Database,
	searcher search.Searcher,
	mcpSrv *mcpserver.Server,
) (*watcher.FileWatcher, error) {
	debounce := time.Duration(cfg.AutoUpdate.DebounceSeconds) * time.Second

	cb := func(ctx context.Context, changedPaths []string) error {
		if len(changedPaths) == 0 {
			return nil
		}

		log.Infow("auto-update triggered", "files_changed", len(changedPaths))

		// Re-index each affected source once (deduplicated by source path).
		affected := make(map[string]bool)
		for _, fp := range changedPaths {
			ingSrc, ok := ingRunner.SourceForPath(fp)
			if !ok {
				log.Infow("changed file not in any configured source", "path", fp)
				continue
			}
			if affected[ingSrc.Path] {
				continue
			}
			affected[ingSrc.Path] = true

			if _, err := ingRunner.IngestSourceByPath(ctx, ingSrc.Path); err != nil {
				log.Error("auto-update reindex", logger.Err(err), "source", ingSrc.Path)
			} else {
				log.Infow("auto-update re-indexed", "source", ingSrc.Path)
			}
		}

		// Remove indexed documents whose files were deleted.
		pruned, err := ingRunner.PruneDeleted(ctx)
		if err != nil {
			log.Error("prune deleted documents", logger.Err(err))
		} else if pruned > 0 {
			log.Infow("pruned deleted documents", "count", pruned)
		}

		// Refresh the in-memory knowledge graph so search stays current.
		if cfg.Graph.EnableGraph {
			newGraph := loadGraph(ctx, log, db)
			if graphUpdater, ok := searcher.(interface{ SetGraph(*graph.Graph) }); ok {
				graphUpdater.SetGraph(newGraph)
			}
			mcpSrv.SetGraph(newGraph)
		}

		return nil
	}

	fw, err := watcher.New(debounce, cb, watcher.WithLogger(log))
	if err != nil {
		return nil, fmt.Errorf("create file watcher: %w", err)
	}

	// Add all enabled sources to the watch list.
	globalCfg, err := config.LoadGlobalConfig(cfg.Paths.GlobalConfigPath)
	if err != nil {
		return nil, fmt.Errorf("load global config for file watcher: %w", err)
	}
	sources := []config.SourceConfig{}
	if globalCfg != nil {
		sources = globalCfg.Sources
	}
	for _, src := range sources {
		if src.Disabled {
			continue
		}
		if err := fw.Add(src.Path); err != nil {
			return nil, fmt.Errorf("watch source %s: %w", src.Path, err)
		}
		log.Infow("watching source directory", "type", src.Type, "path", src.Path)
	}

	return fw, nil
}
