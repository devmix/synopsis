// Package runner executes the ingestion pipeline. A Runner is shared by the
// sync command and the auto-update watcher during serve mode.
package runner

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/devmix/synopsis/internal/cache"
	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/domain"
	"github.com/devmix/synopsis/internal/embedding"
	"github.com/devmix/synopsis/internal/gc"
	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/ingestion/chunkers"
	"github.com/devmix/synopsis/internal/ingestion/ner"
	"github.com/devmix/synopsis/internal/ingestion/sources"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/prompts"
	"github.com/devmix/synopsis/internal/relations"
)

// SummaryStats holds aggregate statistics across all ingested sources.
type SummaryStats struct {
	SourcesProcessed int
	DocumentsCreated int64
	DocumentsUpdated int64
	DocumentsSkipped int64
	Errors           []string
}

// OrphanCleanupStats holds counters returned by CleanupOrphanedData.
type OrphanCleanupStats struct {
	EntitiesDeleted  int
	FactsDeleted     int64
	VectorsDeleted   int64
	DocumentsDeleted int64
	Errors           []string
}

// Runner executes the ingestion pipeline for configured sources. A Runner is
// reusable: the same instance serves one-shot syncs and incremental updates
// triggered by the file watcher during serve mode.
type Runner struct {
	cfg          config.Config
	db           *sql.DB
	gc           *gc.DocumentGC
	log          *logger.Logger
	embed        embedding.Provider
	domain       *domain.DomainRegistry
	globalCfg    *config.GlobalConfig           // loaded from global.xml; mandatory when GlobalConfigPath is set
	sources      *sources.Registry              // source type → Source (parser + chunker)
	cacheStore   *cache.Store                   // cache database; nil if unavailable
	llmCache     *ner.LLMCache                  // LLM NER query cache; nil when NER is disabled
	promptLoader *prompts.PromptLoader          // prompt template loader for entity linker and NER
	sourceConfig map[string]config.SourceConfig // abs path -> source

	// mu serializes IngestAll/IngestSource/RecoverDegradedNER so the recovery
	// worker and the file watcher never write to SQLite concurrently.
	mu sync.Mutex
}

// RunnerOption configures a Runner.
type RunnerOption func(*Runner)

// WithCacheStore sets the cache store used by the LLM NER cache.
func WithCacheStore(store *cache.Store) RunnerOption {
	return func(r *Runner) {
		r.cacheStore = store
	}
}

// WithDomainRegistry sets the domain registry for the runner.
func WithDomainRegistry(registry *domain.DomainRegistry) RunnerOption {
	return func(r *Runner) {
		r.domain = registry
	}
}

// WithGlobalConfig injects a pre-loaded global config (for tests).
func WithGlobalConfig(gcfg *config.GlobalConfig) RunnerOption {
	return func(r *Runner) {
		r.globalCfg = gcfg
	}
}

// NewRunner builds the shared ingestion runtime for the given configuration.
// It creates the embedding provider, domain registry, parsers and chunkers once.
func NewRunner(cfg config.Config, db *sql.DB, log *logger.Logger, opts ...RunnerOption) (*Runner, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}
	if log == nil {
		return nil, fmt.Errorf("logger is required")
	}

	mdChunker := chunkers.NewMarkdownChunker(chunkers.MarkdownChunkerConfig{
		Strategy:       cfg.Ingestion.Chunking.Markdown.Strategy,
		MaxChunkSize:   cfg.Ingestion.Chunking.Markdown.MaxChunkSize,
		OverlapSize:    cfg.Ingestion.Chunking.Markdown.OverlapSize,
		MinSectionSize: cfg.Ingestion.Chunking.Markdown.MinSectionSize,
	}, log)

	jsonChunker := chunkers.NewJSONChunker(chunkers.JSONChunkerConfig{
		TextFields:    cfg.Ingestion.Chunking.JSON.TextFields,
		CombineFields: cfg.Ingestion.Chunking.JSON.CombineFields,
		MaxObjects:    cfg.Ingestion.Chunking.JSON.MaxObjects,
	}, log)

	reg := sources.NewRegistry()

	mdSrc := sources.NewMarkdownSource(mdChunker, log)
	if err := reg.Register("markdown", mdSrc); err != nil {
		return nil, fmt.Errorf("register markdown source: %w", err)
	}

	jsonSrc := sources.NewJsonSource(jsonChunker, log)
	if err := reg.Register("json", jsonSrc); err != nil {
		return nil, fmt.Errorf("register json source: %w", err)
	}

	mwSrc := sources.NewMediawikiSource(mdChunker, log)
	if err := reg.Register("mediawiki", mwSrc); err != nil {
		return nil, fmt.Errorf("register mediawiki source: %w", err)
	}

	unstructSrc := sources.NewUnstructuredSource(mdChunker, jsonChunker, log)
	if err := reg.Register("unstructured", unstructSrc); err != nil {
		return nil, fmt.Errorf("register unstructured source: %w", err)
	}

	wpSrc := sources.NewWebpageSource(mdChunker, log)
	if err := reg.Register("webpages", wpSrc); err != nil {
		return nil, fmt.Errorf("register webpages source: %w", err)
	}

	r := &Runner{
		cfg:          cfg,
		db:           db,
		gc:           gc.NewDocumentGC(db),
		log:          log,
		sources:      reg,
		sourceConfig: make(map[string]config.SourceConfig),
	}

	for _, opt := range opts {
		opt(r)
	}

	// Create prompt loader for template-based prompts.
	promptLoader, err := prompts.NewLoader(cfg.Paths.PromptsPath, log)
	if err != nil {
		return nil, fmt.Errorf("create prompt loader: %w", err)
	}
	r.promptLoader = promptLoader

	var providerOpts []embedding.ProviderOption
	if r.cacheStore != nil {
		providerOpts = append(providerOpts, embedding.WithCacheStore(r.cacheStore))
	}

	embedProvider, err := embedding.NewProvider(cfg.Embeddings, cfg.Paths.DataDir, log, cfg.ONNX, providerOpts...)
	if err != nil {
		return nil, fmt.Errorf("create embedding provider: %w", err)
	}

	r.embed = embedProvider

	// Load global config if not injected via option. Global config is mandatory.
	if r.globalCfg == nil && cfg.Paths.GlobalConfigPath != "" {
		gcfg, err := config.LoadGlobalConfig(cfg.Paths.GlobalConfigPath)
		if err != nil {
			return nil, fmt.Errorf("load global config: %w", err)
		}
		if gcfg == nil {
			return nil, fmt.Errorf("global config at %q is missing or empty", cfg.Paths.GlobalConfigPath)
		}
		r.globalCfg = gcfg
	}

	// Build sourceConfig map from global config sources.
	if r.globalCfg != nil {
		r.sourceConfig = make(map[string]config.SourceConfig, len(r.globalCfg.Sources))
		for _, src := range r.globalCfg.Sources {
			abs, err := filepath.Abs(src.Path)
			if err != nil {
				continue
			}
			r.sourceConfig[abs] = src
		}
	}

	// If no domain registry was provided via option, discover from config.
	if r.domain == nil {
		domainRegistry, err := domain.DiscoveryWithLogger(cfg.Paths.GlobalConfigPath, log)
		if err != nil {
			log.Warn("domain discovery", "error", err)
			r.domain = domain.NewDomainRegistry()
		} else {
			r.domain = domainRegistry
		}
	}

	if !cfg.Ingestion.NER.Disabled && r.cacheStore != nil {
		r.llmCache = ner.NewLLMCache(r.cacheStore, "llm_ner_cache")
	}

	return r, nil
}

// Close releases resources held by the Runner. Callers should defer this after
// creating a Runner that owns a cache store.
func (r *Runner) Close() error {
	if r.cacheStore != nil {
		return r.cacheStore.Close()
	}
	return nil
}

// IngestAll processes every enabled source. If rebuild is true, existing data
// for each source is cleared before re-indexing.
func (r *Runner) IngestAll(ctx context.Context, rebuild bool) SummaryStats {
	r.mu.Lock()
	defer r.mu.Unlock()

	var stats SummaryStats

	var sources []config.SourceConfig
	if r.globalCfg != nil {
		sources = r.globalCfg.Sources
	}
	if sources == nil {
		sources = []config.SourceConfig{}
	}
	for _, src := range sources {
		if src.Disabled {
			r.log.Infow("skipping disabled source", "path", src.Path)
			continue
		}

		progress, err := r.ingestSourceLocked(ctx, src, rebuild)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("%s: %v", src.Path, err))
			r.log.Error("ingest source", "source", src.Path, "error", err)
			continue
		}
		stats.SourcesProcessed++
		stats.DocumentsCreated += progress.DocumentsCreated
		stats.DocumentsUpdated += progress.DocumentsUpdated
		stats.DocumentsSkipped += progress.DocumentsSkipped
	}

	// Run orphan cleanup (already holding r.mu).
	cleanupStats, err := r.cleanupOrphanedDataLocked(ctx)
	if err != nil {
		stats.Errors = append(stats.Errors, fmt.Sprintf("cleanup orphaned data: %v", err))
		r.log.Error("cleanup orphaned data", "error", err)
	} else {
		r.log.Infow("orphan cleanup completed",
			"entities_deleted", cleanupStats.EntitiesDeleted,
			"facts_deleted", cleanupStats.FactsDeleted,
			"vectors_deleted", cleanupStats.VectorsDeleted,
			"documents_deleted", cleanupStats.DocumentsDeleted)
	}

	// Build cross-domain entity links.
	linkResult, err := r.BuildEntityLinks(ctx)
	if err != nil {
		stats.Errors = append(stats.Errors, fmt.Sprintf("build entity links: %v", err))
		r.log.Error("build entity links", "error", err)
	} else {
		// Propagate per-link errors into summary stats.
		stats.Errors = append(stats.Errors, linkResult.Errors...)
		r.log.Infow("entity links post-processing",
			"created", linkResult.LinksCreated,
			"skipped", linkResult.LinksSkipped)
	}

	return stats
}

// IngestSource runs the ingestion pipeline for a single source.
// With rebuild=false the pipeline is incremental: unchanged documents are
// skipped via content-hash deduplication and changed documents are refreshed.
func (r *Runner) IngestSource(ctx context.Context, src config.SourceConfig, rebuild bool) (ingestion.ProgressStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ingestSourceLocked(ctx, src, rebuild)
}

// ingestSourceLocked is the shared implementation of IngestSource. Callers must
// hold r.mu.
func (r *Runner) ingestSourceLocked(ctx context.Context, src config.SourceConfig, rebuild bool) (ingestion.ProgressStats, error) {
	sourceType := src.Type
	if sourceType == "" {
		sourceType = detectSourceType(src.Path)
	}

	srcUnit, ok := r.sources.Get(sourceType)
	if !ok {
		return ingestion.ProgressStats{}, fmt.Errorf("no source for type %q", sourceType)
	}

	enrichedParser := &domainEnrichedParser{
		delegate: srcUnit,
		domains:  src.Domain,
	}

	nerProvider := r.buildNERProvider(src)

	ing, err := ingestion.NewIngester(
		r.db,
		r.embed,
		srcUnit,
		enrichedParser,
		r.cfg.Ingestion,
		r.domain,
		r.log,
		ingestion.WithNERProvider(nerProvider),
	)
	if err != nil {
		return ingestion.ProgressStats{}, fmt.Errorf("create ingester: %w", err)
	}

	r.log.Debug("chunking configuration",
		"markdown_strategy", r.cfg.Ingestion.Chunking.Markdown.Strategy,
		"markdown_max_chunk_size", r.cfg.Ingestion.Chunking.Markdown.MaxChunkSize,
		"markdown_overlap_size", r.cfg.Ingestion.Chunking.Markdown.OverlapSize,
		"json_combine_fields", r.cfg.Ingestion.Chunking.JSON.CombineFields,
		"json_max_objects", r.cfg.Ingestion.Chunking.JSON.MaxObjects,
	)

	r.log.Infow("processing source...", "path", src.Path, "type", sourceType, "rebuild", rebuild)
	progress, err := ing.Ingest(ctx, src.Path, sourceType, rebuild)
	if err != nil {
		return progress, err
	}

	r.log.Infow("source ingestion summary",
		"path", src.Path,
		"type", sourceType,
		"documents_created", progress.DocumentsCreated,
		"documents_updated", progress.DocumentsUpdated,
		"documents_skipped", progress.DocumentsSkipped,
		"chunks_created", progress.ChunksCreated,
		"entities_extracted", progress.EntitiesExtracted,
		"errors", progress.Errors,
	)

	return progress, nil
}

// SyncSource re-indexes the source directory that contains the given changed
// file, performing an incremental update.
func (r *Runner) SyncSource(ctx context.Context, changedPath string) (ingestion.ProgressStats, error) {
	src, ok := r.findSourceForPath(changedPath)
	if !ok {
		return ingestion.ProgressStats{}, fmt.Errorf("no configured source contains %s", changedPath)
	}
	return r.IngestSource(ctx, src, false)
}

// IngestSourceByPath runs an incremental sync on the source whose configured
// directory matches path (exact match against ingestion.sources entries).
func (r *Runner) IngestSourceByPath(ctx context.Context, path string) (ingestion.ProgressStats, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return ingestion.ProgressStats{}, err
	}
	src, ok := r.sourceConfig[abs]
	if !ok {
		return r.SyncSource(ctx, path)
	}
	return r.IngestSource(ctx, src, false)
}

// CleanupOrphanedData removes entities that have no entity_sources links (except
// EntityType which is shared infrastructure) and facts that have no fact_sources
// (excluding approved facts). Also removes orphaned vectors (chunks_vec rows with
// no matching chunk) and orphaned documents (no chunks, entity_sources or fact_sources).
// Runs the entire cleanup in a single transaction.
// Serialized with IngestAll/IngestSource via the runner mutex to protect SQLite
// from concurrent writes.
func (r *Runner) CleanupOrphanedData(ctx context.Context) (OrphanCleanupStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cleanupOrphanedDataLocked(ctx)
}

// cleanupOrphanedDataLocked is the internal implementation of CleanupOrphanedData.
// Callers must hold r.mu.
func (r *Runner) cleanupOrphanedDataLocked(ctx context.Context) (OrphanCleanupStats, error) {
	var stats OrphanCleanupStats

	if err := dao.NewTxManager(r.db).ExecTx(ctx, func(ctx context.Context, tx dao.DBTX) error {
		txGC := gc.NewDocumentGC(tx)

		// 1. Batch-delete orphaned entities (excludes EntityType and fact-referenced) via subquery.
		deletedEntities, err := txGC.DeleteOrphanedEntityIDs(ctx)
		if err != nil {
			return fmt.Errorf("delete orphaned entities: %w", err)
		}
		stats.EntitiesDeleted = int(deletedEntities)

		// 2. Batch-delete orphaned facts (excludes approved).
		deletedFacts, err := txGC.DeleteOrphanedFacts(ctx)
		if err != nil {
			return fmt.Errorf("delete orphaned facts: %w", err)
		}
		stats.FactsDeleted = deletedFacts

		// 3. Delete orphaned vectors (chunks_vec rows with no matching chunk).
		chunkDAO := dao.NewChunkDAO(tx)
		deletedVectors, err := chunkDAO.DeleteOrphanedVectors(ctx)
		if err != nil {
			return fmt.Errorf("delete orphaned vectors: %w", err)
		}
		stats.VectorsDeleted = deletedVectors

		// 4. Delete orphaned documents (no chunks, entity_sources or fact_sources).
		deletedDocs, err := txGC.DeleteOrphanedDocuments(ctx)
		if err != nil {
			return fmt.Errorf("delete orphaned documents: %w", err)
		}
		stats.DocumentsDeleted = deletedDocs

		return nil
	}); err != nil {
		return stats, err
	}

	return stats, nil
}

// BuildEntityLinks runs post-ingestion cross-domain entity link building.
// It reads the last linking run timestamp from app_kv and processes only changed entities
// (incremental mode), or all entities on first run. Old links for changed entities are deleted and recreated.
func (r *Runner) BuildEntityLinks(ctx context.Context) (*relations.BuildEntityLinksResult, error) {
	if r.globalCfg == nil || r.globalCfg.CrossDomainLinks == nil {
		r.log.Info("build entity links: no cross-domain-links config, skipping")
		return &relations.BuildEntityLinksResult{}, nil
	}

	linker, err := r.newCrossDomainLinker(ctx, r.globalCfg.CrossDomainLinks)
	if err != nil {
		r.log.Warn("build entity links: create LLM linker", "error", err)
		// Continue without LLM linker; the relations layer will record an error.
		linker = nil
	}

	// Determine incremental linking window from app_kv.
	since := ""
	kv := dao.NewAppKV(r.db)
	if lastRun, ok := kv.Get(ctx, relations.KVKeyLastLinkingRun); ok {
		since = lastRun
		r.log.Infow("incremental entity linking", "since", since)
	} else {
		r.log.Info("full entity link rebuild (no previous run)")
	}

	result, err := relations.BuildEntityLinksIncremental(ctx, r.db, r.globalCfg.CrossDomainLinks, linker, since)
	if err != nil {
		return nil, fmt.Errorf("build entity links: %w", err)
	}

	// Record the current timestamp for next incremental run.
	now := time.Now().Format(time.RFC3339)
	if err := kv.Set(ctx, relations.KVKeyLastLinkingRun, now); err != nil {
		r.log.Warn("failed to record linking run timestamp", "error", err)
	}

	if len(result.Errors) > 0 {
		r.log.Warn("entity links built with errors",
			"links_created", result.LinksCreated,
			"links_skipped", result.LinksSkipped,
			"errors", result.Errors)
	} else {
		r.log.Infow("entity links built",
			"links_created", result.LinksCreated,
			"links_skipped", result.LinksSkipped)
	}

	return result, nil
}

// SourceForPath returns the source config whose directory contains the path.
func (r *Runner) SourceForPath(path string) (config.SourceConfig, bool) {
	return r.findSourceForPath(path)
}

// EmbeddingProvider returns the shared embedding provider.
func (r *Runner) EmbeddingProvider() embedding.Provider {
	return r.embed
}

// findSourceForPath looks up the source config whose directory contains the path.
// Also works for paths that have just been removed from disk.
func (r *Runner) findSourceForPath(path string) (config.SourceConfig, bool) {
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return config.SourceConfig{}, false
	}
	// Exact directory match wins (watch root itself).
	if src, ok := r.sourceConfig[pathAbs]; ok {
		return src, true
	}
	var best config.SourceConfig
	bestLen := 0
	found := false
	for abs, src := range r.sourceConfig {
		if strings.HasPrefix(pathAbs, abs) && len(abs) > bestLen {
			best = src
			bestLen = len(abs)
			found = true
		}
	}
	return best, found
}

// PruneDeleted removes indexed documents whose source files no longer exist on
// disk. For each pruned document, performs full per-document cleanup (FullClearDocByID)
// followed by document deletion in a single transaction to avoid fact_sources/vector leaks.
// Returns the number of removed documents.
func (r *Runner) PruneDeleted(ctx context.Context) (int, error) {
	docDAO := dao.NewDocumentDAO(r.db)
	docs, err := docDAO.List(ctx)
	if err != nil {
		return 0, fmt.Errorf("list documents: %w", err)
	}

	removed := 0
	for _, doc := range docs {
		if !r.belongsToSource(doc.OriginalPath) {
			continue
		}
		if _, err := os.Stat(doc.OriginalPath); err != nil {
			docID := doc.ID // capture for closure
			if err := dao.NewTxManager(r.db).ExecTx(ctx, func(ctx context.Context, tx dao.DBTX) error {
				txGC := gc.NewDocumentGC(tx)
				if err := txGC.FullClearDocByID(ctx, docID); err != nil {
					return fmt.Errorf("full clear pruned document %d: %w", docID, err)
				}
				txDocDAO := dao.NewDocumentDAO(tx)
				if err := txDocDAO.Delete(ctx, docID); err != nil {
					return fmt.Errorf("delete pruned document %d: %w", docID, err)
				}
				return nil
			}); err != nil {
				return removed, err
			}
			removed++
		}
	}
	return removed, nil
}

// buildNERProvider creates the composite NER provider for a source, or returns
// nil when NER is disabled or cannot be constructed.
func (r *Runner) buildNERProvider(src config.SourceConfig) ner.Provider {
	if r.cfg.Ingestion.NER.Disabled {
		return nil
	}

	var domainConfigs []*domain.DomainConfig
	for _, name := range src.Domain {
		dc, err := r.domain.Get(name)
		if err != nil {
			r.log.Warn("load domain config for NER", "domain", name, "error", err)
			continue
		}
		r.log.Debug("domain config for NER", "domain", dc.Name, "entities", len(dc.Entities), "relations", len(dc.Relations))
		domainConfigs = append(domainConfigs, dc)
	}

	// Read NER methods from global XML config (the only source of method list).
	var methods []string
	if r.globalCfg != nil && r.globalCfg.NER != nil {
		methods = r.globalCfg.NER.Methods
	}

	composite, err := ner.BuildCompositeFromStages(
		methods,
		domainConfigs,
		ner.ProseNEROptions{
			Config: r.cfg.Ingestion.NER.Prose,
		},
		ner.LLMNEROptions{
			Config:        r.cfg.Ingestion.NER.LLM,
			DomainConfigs: domainConfigs,
			Cache:         r.llmCache,
		},
		r.log,
		r.promptLoader,
	)
	if err != nil {
		r.log.Warn("create composite NER provider", "error", err)
		return nil
	}
	return composite
}

// detectSourceType infers the source type from path components.
func detectSourceType(path string) string {
	lower := strings.ToLower(filepath.Base(path))
	if strings.Contains(lower, "wiki") || strings.Contains(lower, "mediawiki") {
		return "mediawiki"
	}
	if strings.Contains(lower, "webpage") {
		return "webpages"
	}
	return "unstructured"
}

// belongsToSource reports whether the document path is under any configured source.
func (r *Runner) belongsToSource(path string) bool {
	if r.globalCfg == nil || r.globalCfg.Sources == nil {
		return false
	}
	sources := r.globalCfg.Sources
	for _, src := range sources {
		if src.Disabled {
			continue
		}
		srcAbs, err := filepath.Abs(src.Path)
		if err != nil {
			continue
		}
		pathAbs, err := filepath.Abs(path)
		if err != nil {
			continue
		}
		if strings.HasPrefix(pathAbs, srcAbs) {
			return true
		}
	}
	return false
}

// domainEnrichedParser wraps a parser and adds domain metadata to each document.
type domainEnrichedParser struct {
	delegate ingestion.Parser
	domains  config.Domains
}

// Parse delegates to the inner parser and annotates each document with domain metadata.
func (p *domainEnrichedParser) Parse(sourcePath string) ingestion.ParseResult {
	result := p.delegate.Parse(sourcePath)
	for i := range result.Documents {
		if result.Documents[i].Metadata == nil {
			result.Documents[i].Metadata = make(map[string]interface{})
		}
		result.Documents[i].Metadata["domain"] = p.domains
	}
	return result
}

// SupportedExtensions delegates to the inner parser.
func (p *domainEnrichedParser) SupportedExtensions() []string {
	return p.delegate.SupportedExtensions()
}

// ReEmbedChunks drops the existing vector table, recreates it with the current
// dimension, and re-generates embeddings for all chunks. This is used when the
// embedding model changes (e.g., 384 → 768). It does NOT re-parse documents;
// it only re-embeds existing chunk text.
func ReEmbedChunks(ctx context.Context, db *database.Database, provider embedding.Provider, log *logger.Logger) error {
	if db == nil {
		return fmt.Errorf("database is nil")
	}
	if provider == nil {
		return fmt.Errorf("embedding provider is nil")
	}

	chunkDAO := dao.NewChunkDAO(db.DB())
	chunks, err := chunkDAO.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("list all chunks: %w", err)
	}

	total := len(chunks)
	if total == 0 {
		log.Info("re-embed: no chunks to process")
		return nil
	}

	log.Infow("re-embed started", "chunks", total, "dimension", db.VectorDim())

	// Drop and recreate vector table with new dimension.
	if err := db.DropVectorTable(ctx); err != nil {
		return fmt.Errorf("drop existing vector table: %w", err)
	}
	if err := database.InitVectorTable(db, db.VectorDim()); err != nil {
		return fmt.Errorf("recreate vector table with dim %d: %w", db.VectorDim(), err)
	}

	batchSize := 100
	processed := 0

	for i := 0; i < total; i += batchSize {
		select {
		case <-ctx.Done():
			return fmt.Errorf("re-embed cancelled after %d chunks: %w", processed, ctx.Err())
		default:
		}

		end := i + batchSize
		if end > total {
			end = total
		}
		batch := chunks[i:end]

		texts := make([]string, len(batch))
		for j, c := range batch {
			texts[j] = c.ChunkText
		}

		vectors, err := provider.GenerateEmbeddings(ctx, texts)
		if err != nil {
			return fmt.Errorf("generate embeddings for batch %d-%d: %w", i, end, err)
		}

		for j, c := range batch {
			vectorStr := dao.FormatVector(vectors[j])
			if _, err := db.DB().ExecContext(
				ctx, "INSERT OR REPLACE INTO chunks_vec (chunk_id, vector) VALUES (?, ?)",
				c.ID, vectorStr,
			); err != nil {
				return fmt.Errorf("insert vector for chunk %d: %w", c.ID, err)
			}
		}

		processed += len(batch)
		log.Infow("re-embed progress", "processed", processed, "total", total)
	}

	log.Infow("re-embed completed", "chunks", processed, "dimension", db.VectorDim())
	return nil
}
