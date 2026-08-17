package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/devmix/synopsis/internal/benchmark"
	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/embedding"
	"github.com/devmix/synopsis/internal/graph"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/onnx"
	"github.com/devmix/synopsis/internal/search"
)

// runLoadTest implements the `load-test` subcommand: it optionally populates the
// database with deterministic synthetic data (real embeddings included), then
// benchmarks every MCP tool handler directly, without HTTP.
func runLoadTest(cfgPath, dbPath string, scaleName string, seed int64, iterations int, jsonPath string, noFill bool) {
	cfg, log, _ := bootstrap(cfgPath, dbPath)
	ctx := context.Background()

	db, openErr := openDatabase(log, cfg) // exits on hard failure; returns db even on dimension mismatch
	if db != nil {
		defer db.Close() //nolint:errcheck
	}

	// A dimension mismatch is only recoverable when we are going to refill the
	// vectors anyway. With --no-fill it is fatal.
	if openErr != nil {
		if noFill {
			log.Fatal("vector dimension mismatch in --no-fill mode; re-run without --no-fill to rebuild vectors or fix the embedding config", logger.Err(openErr))
		}
		if err := db.DropVectorTable(ctx); err != nil {
			log.Fatal("drop vector table after dimension mismatch", logger.Err(err))
		}
		if err := database.InitVectorTable(db, cfg.VectorDim()); err != nil {
			log.Fatal("recreate vector table", logger.Err(err))
		}
		log.Warn("vector dimension mismatch; dropped and recreated chunks_vec for the load-test fill")
	}

	requireEmbeddingModel(log, &cfg)

	cacheStore := openCacheStore(log, cfg)
	var providerOpts []embedding.ProviderOption
	if cacheStore != nil {
		providerOpts = append(providerOpts, embedding.WithCacheStore(cacheStore))
	}
	provider, err := embedding.NewProvider(cfg.Embeddings, cfg.Paths.DataDir, log, cfg.ONNX, providerOpts...)
	if err != nil {
		log.Fatal("create embedding provider", logger.Err(err))
	}

	scale, err := benchmark.ParseScale(scaleName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	report := &benchmark.Report{
		Scale:        scale,
		Seed:         seed,
		Filled:       !noFill,
		Iterations:   iterations,
		PagesPerCall: 0, // runner default (3), kept in sync below if customized
	}

	var samples *benchmark.Samples
	if noFill {
		samples, err = benchmark.LoadSamplesFromDB(ctx, db.DB(), 0)
		if err != nil {
			log.Fatal("load benchmark samples from database", logger.Err(err))
		}
		log.Infow("using existing database data for the benchmark")
	} else {
		gen := benchmark.NewGenerator(seed)
		ds, err := gen.Generate(scale)
		if err != nil {
			log.Fatal("generate dataset", logger.Err(err))
		}

		batchSize := cfg.Ingestion.BatchSize
		lastPct := -100
		fillOpts := benchmark.FillOptions{
			BatchSize: batchSize,
			Progress: func(done, total int) {
				if total <= 0 {
					return
				}
				pct := done * 100 / total
				if pct >= lastPct+25 || done == total {
					lastPct = pct
					log.Infow("embedding progress", "done", done, "total", total)
				}
			},
		}

		log.Infow("filling database with generated data", "scale", scale.Name, "documents", ds.Scale.Documents, "chunks", ds.Scale.Chunks)
		fillReport, err := benchmark.Fill(ctx, db.DB(), ds, provider, fillOpts)
		if err != nil {
			log.Fatal("fill database", logger.Err(err))
		}

		report.Fill = &benchmark.FillSummary{
			DurationMs: float64(fillReport.Duration.Microseconds()) / 1000.0,
			Vectors:    fillReport.Vectors,
			Tables:     fillReport.Tables,
		}
		log.Infow("database filled", "duration_ms", report.Fill.DurationMs)

		samples = ds.Samples
		if samples == nil {
			log.Fatal("generator produced no benchmark samples")
		}
	}

	// Load the knowledge graph and measure it separately, as required.
	graphStart := time.Now()
	g, gStats, err := graph.NewGraphFromDB(ctx, db.DB())
	if err != nil {
		log.Fatal("load knowledge graph", logger.Err(err))
	}
	report.Graph = &benchmark.GraphSummary{
		LoadMs: float64(time.Since(graphStart).Microseconds()) / 1000.0,
		Nodes:  gStats.NodeCount,
		Edges:  gStats.EdgeCount,
	}

	chunkDAO := dao.NewChunkDAO(db.DB())
	docDAO := dao.NewDocumentDAO(db.DB())
	chunkEntityDAO := dao.NewChunkEntityDAO(db.DB())
	searcher := search.NewSearcher(
		db.DB(), chunkDAO, docDAO, chunkEntityDAO,
		provider, cfg.Search, cfg.Graph, g,
	)

	runOpts := benchmark.Options{Iterations: iterations}.WithDefaults()
	report.Iterations = runOpts.Iterations
	report.PagesPerCall = runOpts.PagesPerCall

	runner := benchmark.NewRunner(db.DB(), searcher, g, samples)
	result, err := runner.Run(ctx, runOpts)
	if err != nil {
		log.Fatal("run tool benchmark", logger.Err(err))
	}
	report.Tools = result.Tools

	report.Print(os.Stdout)

	if jsonPath != "" {
		if err := report.WriteJSON(jsonPath); err != nil {
			log.Fatal("write JSON report", logger.Err(err))
		}
		log.Infow("json report written", "path", jsonPath)
	}
}

// requireEmbeddingModel verifies that the configured local embedding model is
// available and pins its resolved path. Unlike ensureEmbeddingModel it never
// downloads: a load test must fail loudly when the model is missing instead of
// silently pulling ~100 MB mid-run.
func requireEmbeddingModel(log *logger.Logger, cfg *config.Config) {
	if cfg.Embeddings.Mode != "local" {
		return // api mode resolves embeddings remotely; nothing to verify locally
	}

	if cfg.Embeddings.Local.ModelPath != "" {
		if _, err := os.Stat(cfg.Embeddings.Local.ModelPath); err != nil {
			log.Fatal("configured embedding model path does not exist",
				"path", cfg.Embeddings.Local.ModelPath, logger.Err(err))
		}
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

	modelDir, ok := manager.GetModelPath(modelName)
	if !ok {
		log.Fatal("embedding model not downloaded; run 'synopsis model download' first and retry",
			"model", modelName, "hint", fmt.Sprintf("synopsis model download %s", modelName))
	}

	// GetModelPath returns the model directory, but the local ONNX provider needs a path to
	// the primary model file (e.g. model.onnx), as EnsureModel does for serve/sync. Fall back
	// to the directory itself if the registry definition has no files.
	modelPath := modelDir
	if def, found := manager.Registry().Get(modelName); found && len(def.Files) > 0 {
		modelPath = filepath.Join(modelDir, def.Files[0].Name)
	}

	cfg.Embeddings.Local.ModelPath = modelPath
	log.Infow("embedding model verified", "name", modelName, "path", modelPath)
}
