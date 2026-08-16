// Package ingestion orchestrates document parsing, chunking, entity extraction,
// embedding generation, and database indexing for the RAG synopsis.
package ingestion

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/domain"
	"github.com/devmix/synopsis/internal/embedding"
	"github.com/devmix/synopsis/internal/gc"
	"github.com/devmix/synopsis/internal/ingestion/chunkers"
	"github.com/devmix/synopsis/internal/ingestion/entities"
	"github.com/devmix/synopsis/internal/ingestion/ner"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/utils"
)

// Ingester orchestrates the full ingestion pipeline: parse → chunk → embed → NER → store.
type Ingester struct {
	db                *sql.DB
	txMgr             *dao.TxManager
	documentDAO       *dao.DocumentDAO
	chunkDAO          *dao.ChunkDAO
	embeddingProvider embedding.Provider
	nerProvider       ner.Provider
	chunker           chunkers.Chunker
	parser            Parser
	config            config.IngestionConfig
	resolver          *entities.Resolver     // entity resolution for deduplication
	domainRegistry    *domain.DomainRegistry // multi-domain support
	log               *logger.Logger         // structured logger; required
}

type IngesterOption func(*Ingester)

func WithNERProvider(p ner.Provider) IngesterOption {
	return func(i *Ingester) { i.nerProvider = p }
}

func NewIngester(
	db *sql.DB,
	provider embedding.Provider,
	chunker chunkers.Chunker,
	parser Parser,
	cfg config.IngestionConfig,
	domainRegistry *domain.DomainRegistry,
	log *logger.Logger,
	opts ...IngesterOption,
) (*Ingester, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}
	if provider == nil {
		return nil, fmt.Errorf("embedding provider is required")
	}
	if chunker == nil {
		return nil, fmt.Errorf("chunker is required")
	}
	if parser == nil {
		return nil, fmt.Errorf("parser is required")
	}
	if domainRegistry == nil {
		return nil, fmt.Errorf("domain registry is required")
	}
	if log == nil {
		return nil, fmt.Errorf("logger is required")
	}

	i := &Ingester{
		db:                db,
		txMgr:             dao.NewTxManager(db),
		documentDAO:       dao.NewDocumentDAO(db),
		chunkDAO:          dao.NewChunkDAO(db),
		embeddingProvider: provider,
		chunker:           chunker,
		parser:            parser,
		config:            cfg,
		resolver:          entities.NewResolver(cfg.Resolver.SimilarityThreshold),
		domainRegistry:    domainRegistry,
		log:               log,
	}

	for _, opt := range opts {
		opt(i)
	}

	return i, nil
}

func (i *Ingester) Ingest(
	ctx context.Context,
	sourcePath string,
	sourceType string,
	rebuild bool,
) (ProgressStats, error) {
	if ctx.Err() != nil {
		return ProgressStats{}, fmt.Errorf("context cancelled before ingestion: %w", ctx.Err())
	}

	// Validate source path exists.
	info, err := os.Stat(sourcePath)
	if err != nil {
		return ProgressStats{}, fmt.Errorf("check source path %s: %w", sourcePath, err)
	}
	if !info.IsDir() {
		return ProgressStats{}, fmt.Errorf("source path %s is not a directory", sourcePath)
	}

	// Count files for progress bar.
	totalFiles, err := countFiles(sourcePath, i.parser)
	if err != nil {
		return ProgressStats{}, fmt.Errorf("count source files: %w", err)
	}

	tracker := NewProgressTracker(totalFiles, fmt.Sprintf("Ingesting %s from %s", sourceType, sourcePath))

	// Create backup before indexing (and before rebuild clears data).
	if err := i.createBackup(); err != nil {
		i.log.Warn("create backup", logger.Err(err))
	}

	// Rebuild if requested.
	if rebuild {
		if err := i.clearSourceData(ctx, sourcePath); err != nil {
			return ProgressStats{}, fmt.Errorf("clear existing data for rebuild: %w", err)
		}
	}

	// Parse documents.
	result := i.parser.Parse(sourcePath)
	for range result.Errors {
		tracker.IncrementErrors()
	}

	if len(result.Documents) == 0 && len(result.Errors) > 0 {
		return ProgressStats{}, fmt.Errorf("no documents parsed, %d errors occurred", len(result.Errors))
	}

	// Process each document.
	for idx, doc := range result.Documents {
		if err := ctx.Err(); err != nil {
			return tracker.Stats(), fmt.Errorf("ingestion cancelled: %w", err)
		}

		i.log.Infow("processing document...", "index", idx+1, "total", len(result.Documents), "path", doc.SourcePath)

		if err := i.processDocument(ctx, doc, tracker); err != nil {
			tracker.IncrementErrors()
			// Log error but continue with next document.
			i.log.Warn("processed document", "path", doc.SourcePath, logger.Err(err))
		} else {
			tracker.IncrementFiles()
		}
	}

	stats := tracker.Stats()
	i.log.Infow("ingestion complete",
		"files_processed", stats.FilesProcessed,
		"chunks_created", stats.ChunksCreated,
		"entities_extracted", stats.EntitiesExtracted,
		"errors", stats.Errors,
		"duration", tracker.Elapsed().Round(time.Second),
	)
	if stats.DocumentsSkipped > 0 || stats.DocumentsUpdated > 0 {
		i.log.Infow("document summary",
			"created", stats.DocumentsCreated,
			"updated", stats.DocumentsUpdated,
			"skipped", stats.DocumentsSkipped,
		)
	}

	return stats, nil
}

func (i *Ingester) processDocument(
	ctx context.Context,
	doc Document,
	tracker *ProgressTracker,
) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Compute SHA-256 hash of document content for deduplication.
	contentHash := computeContentHash(doc.Content)

	// Check if document already exists by path (outside transaction — read-only).
	existingDoc, err := i.documentDAO.GetByPath(ctx, doc.SourcePath)
	if err != nil {
		return fmt.Errorf("lookup existing document %s: %w", doc.SourcePath, err)
	}

	// Document exists and content hash matches → skip entirely.
	if existingDoc != nil && existingDoc.ContentHash == contentHash {
		i.log.Debug("skipped unchanged document", "path", doc.SourcePath)
		tracker.IncrementDocumentsSkipped()
		return nil
	}

	// 1. Chunk the document (outside transaction — no DB writes yet).

	tChunk := time.Now()
	chunks, err := i.chunker.Chunk(doc.Content, doc.Metadata)
	i.log.Debug("chunk stage", "path", doc.SourcePath, "duration", time.Since(tChunk).Round(time.Millisecond), "chunks", len(chunks))
	if err != nil {
		return fmt.Errorf("chunk document %s: %w", doc.SourcePath, err)
	}

	if len(chunks) == 0 {
		i.log.Warn("empty document, skip", "source", doc.SourcePath)
		tracker.IncrementDocumentsSkipped()
		return nil // empty document, skip
	}

	tEmbed := time.Now()
	allVectors, err := i.generateEmbeddings(ctx, chunks, tracker)
	i.log.Debug("embed stage", "path", doc.SourcePath, "duration", time.Since(tEmbed).Round(time.Millisecond), "vectors", len(allVectors))
	if err != nil {
		return fmt.Errorf("generate embeddings for %s: %w", doc.SourcePath, err)
	}

	// 2. Extract entities via NER if configured (outside transaction — external API call).

	tNer := time.Now()
	if err := i.extractNerForChunks(ctx, doc, chunks); err != nil {
		return fmt.Errorf("collect NER for %s: %w", doc.SourcePath, err)
	}
	i.log.Debug("ner stage", "path", doc.SourcePath, "duration", time.Since(tNer).Round(time.Millisecond))

	// All DB writes for this document run in a single transaction managed by
	// txMgr. DAOs constructed over tx ensure atomic commit; entity resolution
	// writes through the same transaction (a second connection would hit the
	// SQLite write lock and fail with "database is locked").
	tStore := time.Now()
	if err := i.txMgr.ExecTx(ctx, func(ctx context.Context, tx dao.DBTX) error {
		// DAOs bound to the transaction ensure all writes commit atomically.
		txDocDAO := dao.NewDocumentDAO(tx)
		txChunkDAO := dao.NewChunkDAO(tx)
		txGc := gc.NewDocumentGC(tx)

		// 3. Insert or update document in DB and get Predicate.

		metaJSON, docID, isNewDocument, err := i.storeDocument(ctx, doc, existingDoc, txDocDAO, txGc, contentHash)
		if err != nil {
			return err
		}

		if isNewDocument {
			tracker.IncrementDocumentsCreated()
		} else {
			tracker.IncrementDocumentsUpdated()
		}

		// 4. Insert chunks and store pre-computed vectors within transaction.

		newChunks, err := i.storeChunks(ctx, doc, chunks, docID, tx, tracker)
		if err != nil {
			return err
		}

		tracker.AddChunks(int64(len(chunks)))

		// Store pre-computed vectors in vec0 table within transaction.
		if err = i.storeVectors(ctx, allVectors, newChunks, txChunkDAO); err != nil {
			return err
		}

		return txDocDAO.Update(ctx, dao.Document{
			ID:           docID,
			OriginalPath: doc.SourcePath,
			MetadataJSON: stringPtr(string(metaJSON)),
			ContentHash:  contentHash,
		})
	}); err != nil {
		return fmt.Errorf("process document %s: %w", doc.SourcePath, err)
	}
	i.log.Debug("store stage", "path", doc.SourcePath, "duration", time.Since(tStore).Round(time.Millisecond))

	return nil
}

func (i *Ingester) extractNerForChunks(ctx context.Context, doc Document, chunks []chunkers.DocumentChunk) error {
	if i.nerProvider == nil || i.config.NER.Disabled {
		return nil
	}

	for idx := range chunks {
		i.log.Debug("\t NER from chunk ", "path", doc.SourcePath, "chunk", idx+1, "total", len(chunks))

		response, err := i.nerProvider.ExtractEntities(ctx, chunks[idx].Text, chunks[idx].Metadata)
		if err != nil {
			i.log.Warn("NER for chunk", "path", doc.SourcePath, "sequence", chunks[idx].SequenceNum, logger.Err(err))
			return fmt.Errorf("extract response for %s: %w", doc.SourcePath, err)
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		chunks[idx].NerResult = response
	}

	return nil
}

func (i *Ingester) storeVectors(ctx context.Context, allVectors [][]float32, newChunks []dao.Chunk, txChunkDAO *dao.ChunkDAO) error {
	if len(allVectors) != len(newChunks) {
		return fmt.Errorf("vector/chunk count mismatch: expected %d vectors, got %d", len(newChunks), len(allVectors))
	}

	for j, vec := range allVectors {
		chunk := newChunks[j]
		if err := txChunkDAO.UpsertVector(ctx, chunk.ID, vec); err != nil {
			i.log.Warn("store vector for chunk", "chunk_id", chunk.ID, logger.Err(err))
			return fmt.Errorf("store vector for chunk for doc %d, seq %d: %w", chunk.DocID, chunk.SequenceNum, err)
		}
	}
	return nil
}

func (i *Ingester) generateEmbeddings(ctx context.Context, chunks []chunkers.DocumentChunk, tracker *ProgressTracker) ([][]float32, error) {
	// Collect chunk texts for embedding generation.
	chunkTexts := make([]string, len(chunks))
	for idx, ch := range chunks {
		chunkTexts[idx] = ch.Text
	}

	// 2. Generate embeddings in batches (outside transaction — external API call).
	batchSize := i.config.BatchSize
	if batchSize <= 0 {
		batchSize = 100
	}

	allVectors := make([][]float32, 0, len(chunkTexts))
	for start := 0; start < len(chunkTexts); start += batchSize {
		end := start + batchSize
		if end > len(chunkTexts) {
			end = len(chunkTexts)
		}

		batchNum := start/batchSize + 1
		totalBatches := (len(chunkTexts) + batchSize - 1) / batchSize

		batch := chunkTexts[start:end]
		i.log.Debug("embedding batch", "batch", batchNum, "total_batches", totalBatches, "size", len(batch))
		vectors, err := i.embeddingProvider.GenerateEmbeddings(ctx, batch)
		if err != nil {
			return nil, fmt.Errorf("generate embeddings for %d chunks: %w", len(batch), err)
		}

		if len(vectors) != len(batch) {
			return nil, fmt.Errorf("embedding count mismatch in batch %d of %d: expected %d vectors, got %d",
				batchNum, totalBatches, len(batch), len(vectors))
		}

		allVectors = append(allVectors, vectors...)

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	tracker.AddEmbeddings(int64(len(allVectors)))
	return allVectors, nil
}

func (i *Ingester) storeDocument(ctx context.Context, doc Document, oldDoc *dao.Document, txDocDAO *dao.DocumentDAO, txGc *gc.DocumentGC, contentHash string) ([]byte, int, bool, error) {
	metaJSON, _ := json.Marshal(doc.Metadata)

	if oldDoc == nil {
		docID, err := txDocDAO.Create(ctx, dao.Document{
			SourceType:   getSourceType(doc.Metadata),
			OriginalPath: doc.SourcePath,
			MetadataJSON: stringPtr(string(metaJSON)),
			ContentHash:  contentHash,
		})
		if err != nil {
			return nil, -1, false, fmt.Errorf("insert document %s: %w", doc.SourcePath, err)
		}
		return metaJSON, docID, true, err
	}

	// Update existing document's metadata and hash.
	if err := txDocDAO.Update(ctx, dao.Document{
		ID:           oldDoc.ID,
		OriginalPath: doc.SourcePath,
		MetadataJSON: stringPtr(string(metaJSON)),
		ContentHash:  contentHash,
	}); err != nil {
		return nil, -1, false, fmt.Errorf("update document %s: %w", doc.SourcePath, err)
	}

	if err := txGc.FullClearDocByID(ctx, oldDoc.ID); err != nil {
		return nil, -1, false, fmt.Errorf("clear document data %s: %w", doc.SourcePath, err)
	}

	return metaJSON, oldDoc.ID, false, nil
}

func (i *Ingester) storeChunks(ctx context.Context, doc Document, chunks []chunkers.DocumentChunk, docID int, tx dao.DBTX, tracker *ProgressTracker) ([]dao.Chunk, error) {
	result := make([]dao.Chunk, 0, len(chunks))
	txChunkDAO := dao.NewChunkDAO(tx)

	for _, ch := range chunks {
		ch.DocID = docID

		chunk := dao.Chunk{
			DocID:       docID,
			ChunkText:   ch.Text,
			SequenceNum: ch.SequenceNum,
			StartOffset: intPtr(ch.StartOffset),
			EndOffset:   intPtr(ch.EndOffset),
		}

		chunkID, err := txChunkDAO.Create(ctx, chunk)
		if err != nil {
			return nil, fmt.Errorf("insert chunk %s:%d: %w", doc.SourcePath, ch.SequenceNum, err)
		}

		chunk.ID = chunkID

		result = append(result, chunk)

		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if err := i.storeEntities(ctx, tx, docID, doc, chunk, ch, tracker); err != nil {
			return nil, fmt.Errorf("store entities for doc %s:%d: %w", doc.SourcePath, ch.SequenceNum, err)
		}
	}

	return result, nil
}

// computeContentHash returns a hex-encoded SHA-256 hash of the given content.
func computeContentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// createBackup creates a WAL-consistent snapshot using SQLite VACUUM INTO.
func (i *Ingester) createBackup() error {
	// Determine DB path from connection.
	dbPath, err := i.getDBPath()
	if err != nil {
		return fmt.Errorf("get db path: %w", err)
	}

	// Create backups directory.
	backupsDir := filepath.Join(filepath.Dir(dbPath), "backups")
	if err := os.MkdirAll(backupsDir, 0o755); err != nil {
		return fmt.Errorf("create backups dir: %w", err)
	}

	now := time.Now()
	ts := now.Format("2006-01-02T15-04-05") + "-" + fmt.Sprintf("%03d", now.Nanosecond()/1e6)
	baseName := strings.TrimSuffix(filepath.Base(dbPath), filepath.Ext(dbPath))
	ext := filepath.Ext(dbPath)
	backupName := fmt.Sprintf("%s_backup_%s%s", baseName, ts, ext)
	backupPath := filepath.Join(backupsDir, backupName)

	// VACUUM INTO writes a consistent snapshot including WAL data.
	// SQLite does not support bound parameters in VACUUM INTO — the path is
	// inserted as a string literal. This is safe because the path is generated
	// internally (not user input).
	_, err = i.db.Exec(fmt.Sprintf("VACUUM INTO '%s'", backupPath)) //nolint:gosec // internal path, not user input
	if err != nil {
		return fmt.Errorf("vacuum into backup %s: %w", backupPath, err)
	}

	return nil
}

// getDBPath attempts to extract the database file path from the sql.DB connection.
func (i *Ingester) getDBPath() (string, error) {
	// sqlite3 stores the filename in PRAGMA database_list (column 2 = file).
	var seq int
	var name string
	var dbPath string
	row := i.db.QueryRow("PRAGMA database_list")
	if err := row.Scan(&seq, &name, &dbPath); err != nil {
		return "", fmt.Errorf("query database path: %w", err)
	}
	if dbPath == ":memory:" || dbPath == "" {
		return "", fmt.Errorf("cannot backup in-memory or unnamed database")
	}
	return dbPath, nil
}

// clearSourceData removes all documents and associated data for a given source path.
// Deletions run in a single transaction so a rebuild that fails mid-way does
// not leave the database partially cleared.
func (i *Ingester) clearSourceData(ctx context.Context, sourcePath string) error {
	docs, err := i.documentDAO.List(ctx)
	if err != nil {
		return fmt.Errorf("list documents: %w", err)
	}

	var toDelete []int
	sourceRoot := filepath.Clean(sourcePath)
	for _, doc := range docs {
		if strings.HasPrefix(filepath.Clean(doc.OriginalPath), sourceRoot) {
			toDelete = append(toDelete, doc.ID)
		}
	}
	if len(toDelete) == 0 {
		return nil
	}

	if err := i.txMgr.ExecTx(ctx, func(ctx context.Context, tx dao.DBTX) error {
		docDAO := dao.NewDocumentDAO(tx)
		for _, id := range toDelete {
			if err := docDAO.Delete(ctx, id); err != nil {
				return fmt.Errorf("delete document %d: %w", id, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (i *Ingester) storeEntities(ctx context.Context, tx dao.DBTX, docID int, doc Document, dbChunk dao.Chunk, chunk chunkers.DocumentChunk, tracker *ProgressTracker) error {
	result := chunk.NerResult
	if result == nil || (len(result.Entities) == 0 && len(result.Facts) == 0) {
		return nil
	}

	resolved, err := i.resolver.AddEntities(ctx, tx, docID, result.Entities)
	if err != nil {
		return fmt.Errorf("add entities %s:%d: %w", doc.SourcePath, chunk.SequenceNum, err)
	}

	if tracker != nil {
		tracker.AddEntities(int64(len(resolved)))
	}

	// Link each resolved NER entity to the current chunk.
	txChunkEntityDAO := dao.NewChunkEntityDAO(tx)
	for _, ent := range resolved {
		if err := txChunkEntityDAO.Link(ctx, dbChunk.ID, ent.ID); err != nil {
			return fmt.Errorf("link chunk %d to entity %d: %w", dbChunk.ID, ent.ID, err)
		}
	}

	if len(result.Facts) == 0 {
		return nil
	}

	// Collect unique (Name, Type, Domain) pairs from all facts' Subject and Object.
	type entityKey struct {
		Name   string
		Type   string
		Domain string
	}
	keySet := make(map[entityKey]struct{})
	for _, f := range result.Facts {
		factDomain := utils.Normalize(f.Domain)
		keySet[entityKey{Name: f.SubjectName, Type: f.SubjectType, Domain: factDomain}] = struct{}{}
		keySet[entityKey{Name: f.ObjectName, Type: f.ObjectType, Domain: factDomain}] = struct{}{}
	}

	syntheticEntities := make([]ner.Entity, 0, len(keySet))
	for k := range keySet {
		syntheticEntities = append(syntheticEntities, ner.Entity{
			Name:   k.Name,
			Type:   k.Type,
			Domain: k.Domain,
		})
	}

	entityIDs, syntheticCreated, err := i.resolver.LookupOrCreateWithStats(ctx, tx, docID, syntheticEntities)
	if err != nil {
		return fmt.Errorf("lookup or create entities for facts %s:%d: %w", doc.SourcePath, chunk.SequenceNum, err)
	}

	// Count only truly new synthetic entities in the tracker.
	if tracker != nil && syntheticCreated > 0 {
		tracker.AddEntities(int64(syntheticCreated))
	}

	// Link each synthetic entity (from facts) to the current chunk.
	for _, eid := range entityIDs {
		if err := txChunkEntityDAO.Link(ctx, dbChunk.ID, eid); err != nil {
			return fmt.Errorf("link chunk %d to synthetic entity %d: %w", dbChunk.ID, eid, err)
		}
	}

	// Build name+"\x00"+type+"\x00"+domain -> Predicate map. LookupOrCreate guarantees all entries are non-zero.
	entityMap := make(map[string]int)
	for idx, ent := range syntheticEntities {
		key := ent.Name + "\x00" + ent.Type + "\x00" + ent.Domain
		entityMap[key] = entityIDs[idx]
	}

	txFactDAO := dao.NewFactDAO(tx)
	txFactSourceDAO := dao.NewFactSourceDAO(tx)

	var factIDs []int64
	factsCreated := 0
	sourcesCreated := 0

	for _, f := range result.Facts {
		factDomain := utils.Normalize(f.Domain)
		subjectKey := f.SubjectName + "\x00" + f.SubjectType + "\x00" + factDomain
		objectKey := f.ObjectName + "\x00" + f.ObjectType + "\x00" + factDomain

		subjectID, subjectOK := entityMap[subjectKey]
		objectID, objectOK := entityMap[objectKey]

		if !subjectOK || !objectOK {
			i.log.Warn("skip fact with unresolved entity",
				"subject", f.SubjectName,
				"subject_type", f.SubjectType,
				"predicate", f.Predicate,
				"object", f.ObjectName,
				"object_type", f.ObjectType,
				"path", doc.SourcePath,
				"sequence", chunk.SequenceNum,
			)
			continue
		}

		// Enforce: subject and object must be in the same domain as the fact.
		if err := txFactDAO.ValidateFactDomain(ctx, subjectID, objectID, factDomain); err != nil {
			i.log.Warn("skip cross-domain fact",
				"subject", f.SubjectName,
				"predicate", f.Predicate,
				"object", f.ObjectName,
				"fact_domain", factDomain,
				"path", doc.SourcePath,
				"sequence", chunk.SequenceNum,
				"error", err,
			)
			continue
		}

		var metadataJSON *string
		if len(f.Metadata) > 0 {
			raw, err := json.Marshal(f.Metadata)
			if err != nil {
				return fmt.Errorf("marshal fact metadata %s:%d: %w", doc.SourcePath, chunk.SequenceNum, err)
			}
			s := string(raw)
			metadataJSON = &s
		}

		factID, err := txFactDAO.CreateOrIgnore(ctx, dao.Fact{
			SubjectEntityID: subjectID,
			Predicate:       f.Predicate,
			ObjectEntityID:  objectID,
			Domain:          factDomain,
			Metadata:        metadataJSON,
		})
		if err != nil {
			return fmt.Errorf("create or ignore fact %s:%d: %w", doc.SourcePath, chunk.SequenceNum, err)
		}

		quote := extractQuoteFromChunk(chunk.Text, f.SubjectName, f.ObjectName)
		factSource := dao.FactSource{
			FactID:      factID,
			Quote:       &quote,
			DocumentID:  docID,
			ExtractedAt: time.Now().Format(time.RFC3339),
		}
		if _, err := txFactSourceDAO.Create(ctx, factSource); err != nil {
			return fmt.Errorf("create fact source for fact %d: %w", factID, err)
		}

		factIDs = append(factIDs, int64(factID))
		factsCreated++
		sourcesCreated++

		if ctx.Err() != nil {
			return ctx.Err()
		}
	}

	if len(factIDs) > 0 {
		if err := txFactDAO.RecomputeWeights(ctx, factIDs); err != nil {
			return fmt.Errorf("recompute fact weights %s:%d: %w", doc.SourcePath, chunk.SequenceNum, err)
		}
	}

	if tracker != nil && factsCreated > 0 {
		tracker.AddFacts(int64(factsCreated))
		tracker.AddFactSources(int64(sourcesCreated))
	}

	return nil
}

// extractQuoteFromChunk extracts a text window around the first occurrence of
// subjectName or objectName (case-insensitive) from chunkText. If neither name
// is found, it returns the first ~120 runes as fallback. Empty input yields "".
// The quote is trimmed to line boundaries and "..." appended when truncated.
func extractQuoteFromChunk(chunkText, subjectName, objectName string) string {
	if chunkText == "" {
		return ""
	}

	chunkRunes := []rune(chunkText)
	textLen := len(chunkRunes)

	type match struct {
		name  string
		index int // rune index of first occurrence
	}

	var bestMatch *match

	lowerChunk := strings.ToLower(string(chunkRunes))
	for _, name := range []string{subjectName, objectName} {
		if name == "" {
			continue
		}
		idx := strings.Index(lowerChunk, strings.ToLower(name))
		if idx < 0 {
			continue
		}
		if bestMatch == nil || idx < bestMatch.index {
			bestMatch = &match{name: name, index: idx}
		}
	}

	const windowSize = 60
	const fallbackLen = 120

	var quote string
	truncated := false

	if bestMatch != nil {
		start := bestMatch.index - windowSize
		if start < 0 {
			start = 0
		}
		end := bestMatch.index + utf8.RuneCountInString(bestMatch.name) + windowSize
		if end > textLen {
			end = textLen
		}
		quote = string(chunkRunes[start:end])
		truncated = start > 0 || end < textLen
	} else {
		// Fallback: first ~120 runes.
		if textLen <= fallbackLen {
			return chunkText
		}
		quote = string(chunkRunes[:fallbackLen])
		truncated = true
	}

	// Trim to line boundary: if truncated, find the last newline within the quote
	// and cut there so we never split mid-line.
	if truncated {
		quote = trimToLineBoundary(quote)
	}

	// Append "..." indicator when truncated.
	if truncated && !strings.HasSuffix(quote, "...") {
		quote = quote + "..."
	}

	return quote
}

// trimToLineBoundary trims the text to the last newline boundary so that
// quotes are never cut mid-line. If no newline is found, returns the original text.
func trimToLineBoundary(text string) string {
	runes := []rune(text)
	// Search backwards for a newline character.
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] == '\n' || runes[i] == '\r' {
			return strings.TrimSpace(string(runes[:i]))
		}
	}
	return text
}

// countFiles returns the number of processable files in a directory tree,
// filtered by the extensions supported by the given parser.
func countFiles(dir string, parser Parser) (int, error) {
	exts := make(map[string]struct{}, len(parser.SupportedExtensions()))
	for _, e := range parser.SupportedExtensions() {
		exts[strings.ToLower(e)] = struct{}{}
	}

	count := 0
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err // skip dirs and errors
		}
		if _, ok := exts[strings.ToLower(filepath.Ext(d.Name()))]; ok {
			count++
		}
		return nil
	})
	return count, err
}

func getSourceType(metadata map[string]interface{}) string {
	if metadata == nil {
		return "unknown"
	}
	v, _ := metadata["source_type"].(string)
	if v == "" {
		return "unknown"
	}
	return v
}

func intPtr(v int) *int { return &v }

func stringPtr(v string) *string { return &v }
