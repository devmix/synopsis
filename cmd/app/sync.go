// Package main implements the entry point for the Synopsis RAG service.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/devmix/synopsis/internal/ingestion/runner"
	"github.com/devmix/synopsis/internal/logger"
)

// runSync forces a full re-index of all configured sources and exits.
// With rebuild=true all existing data is cleared before re-indexing.
func runSync(cfgPath, dbPath string, rebuild bool, autoRebuildVectorsCLI bool) {
	start := time.Now()

	cfg, log, domainRegistry := bootstrap(cfgPath, dbPath)
	defer log.Sync() //nolint:errcheck

	ctx := context.Background()

	// CLI flag overrides config.
	autoRebuildVectors := autoRebuildVectorsCLI || cfg.Embeddings.AutoRebuildVectors

	db, openErr := openDatabase(log, cfg)
	if db != nil {
		defer db.Close() //nolint:errcheck
	}

	ensureEmbeddingModel(log, &cfg)

	cacheStore := openCacheStore(log, cfg)

	// Handle vector dimension mismatch before ingestion.
	// When --rebuild is set, auto-rebuild-vectors is ignored (full reset anyway).
	rebuilt, _ := rebuildVectorsIfNeeded(ctx, log, cfg, db, cacheStore, domainRegistry, openErr, autoRebuildVectors, rebuild)
	if rebuilt {
		log.Info("vectors rebuilt due to dimension mismatch")
	}

	ingRunner, err := runner.NewRunner(cfg, db.DB(), log,
		runner.WithCacheStore(cacheStore),
		runner.WithDomainRegistry(domainRegistry),
	)
	if err != nil {
		log.Fatal("create ingestion runner", logger.Err(err))
	}
	defer ingRunner.Close() //nolint:errcheck

	log.Infow("starting full sync", "rebuild", rebuild)
	stats := ingRunner.IngestAll(ctx, rebuild)

	duration := time.Since(start)

	// Print summary.
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "=== Sync Summary ===")
	fmt.Fprintf(os.Stderr, "Sources processed: %d\n", stats.SourcesProcessed)
	fmt.Fprintf(os.Stderr, "Documents created:  %d\n", stats.DocumentsCreated)
	fmt.Fprintf(os.Stderr, "Documents updated:  %d\n", stats.DocumentsUpdated)
	fmt.Fprintf(os.Stderr, "Documents skipped:  %d\n", stats.DocumentsSkipped)
	if len(stats.Errors) > 0 {
		fmt.Fprintf(os.Stderr, "Errors:            %d\n", len(stats.Errors))
	}
	fmt.Fprintf(os.Stderr, "Duration:          %s\n", duration.Round(time.Second))
	fmt.Fprintln(os.Stderr, "==========================")

	log.Infow("sync finished",
		"sources", stats.SourcesProcessed,
		"documents_created", stats.DocumentsCreated,
		"documents_updated", stats.DocumentsUpdated,
		"documents_skipped", stats.DocumentsSkipped,
		"errors", len(stats.Errors),
		"duration", duration.Round(time.Second),
	)
}