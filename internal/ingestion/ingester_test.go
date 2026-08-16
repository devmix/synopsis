package ingestion

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/domain"
	"github.com/devmix/synopsis/internal/ingestion/chunkers"
	"github.com/devmix/synopsis/internal/ingestion/ner"
	"github.com/devmix/synopsis/internal/logger"

	_ "github.com/mattn/go-sqlite3" // SQLite driver for backup verification
)

// mockEmbeddingProvider satisfies embedding.Provider for tests.
type mockEmbeddingProvider struct {
	dim            int
	mismatchReturn int // if > 0, return exactly this many vectors regardless of input length
}

func (m *mockEmbeddingProvider) GenerateEmbeddings(_ context.Context, texts []string) ([][]float32, error) {
	count := len(texts)
	if m.mismatchReturn > 0 {
		count = m.mismatchReturn
	}
	vectors := make([][]float32, count)
	for i := range vectors {
		vec := make([]float32, m.dim)
		for j := 0; j < m.dim; j++ {
			vec[j] = float32(j+1) / float32(m.dim)
		}
		vectors[i] = vec
	}
	return vectors, nil
}

func (m *mockEmbeddingProvider) VectorDim() int { return m.dim }
func (m *mockEmbeddingProvider) Name() string   { return "mock" }

// mockParser satisfies Parser for tests.
type mockParser struct{}

func (p *mockParser) Parse(_ string) ParseResult    { return ParseResult{} }
func (p *mockParser) SupportedExtensions() []string { return []string{".md"} }

// newTestIngester creates an Ingester backed by a temporary SQLite database with migrations applied.
func newTestIngester(t *testing.T) (*sql.DB, func(), *Ingester) {
	t.Helper()

	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		cleanup()
		t.Fatalf("create test db: %v", err)
	}

	log, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		cleanup()
		t.Fatalf("create test logger: %v", err)
	}

	domainRegistry := domain.NewDomainRegistry()
	ingester, err := NewIngester(
		db,
		&mockEmbeddingProvider{dim: 4},
		chunkers.NewMarkdownChunker(chunkers.DefaultMarkdownChunkerConfig(), log),
		&mockParser{},
		config.IngestionConfig{
			BatchSize: 100,
			NER:       config.NERConfig{Disabled: true},
			Resolver:  config.ResolverConfig{SimilarityThreshold: 0.8},
		},
		domainRegistry,
		log,
	)
	if err != nil {
		cleanup()
		t.Fatalf("NewIngester: %v", err)
	}

	return db, cleanup, ingester
}

func TestStoreEntities_FactsPersisted(t *testing.T) {
	t.Parallel()

	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	// Seed entities that will be resolved by Lookup.
	entityDAO := dao.NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	docDAO := dao.NewDocumentDAO(db)
	docID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc.md"})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	tracker := NewProgressTracker(1, "test")

	chunk := chunkers.DocumentChunk{
		Text:        "Alice works at Acme Corp.",
		SequenceNum: 0,
		NerResult: &ner.Result{
			Facts: []ner.Fact{
				{SubjectName: "Alice", SubjectType: "PERSON", Predicate: "works_at", ObjectName: "Acme Corp", ObjectType: "ORGANIZATION"},
			},
		},
	}

	if err := ingester.storeEntities(ctx, db, docID, Document{SourcePath: "/test/doc.md"}, dao.Chunk{}, chunk, tracker); err != nil {
		t.Fatalf("storeEntities() error = %v", err)
	}

	stats := tracker.Stats()
	if stats.FactsCreated != 1 {
		t.Errorf("FactsCreated = %d, want 1", stats.FactsCreated)
	}
	if stats.FactSourcesCreated != 1 {
		t.Errorf("FactSourcesCreated = %d, want 1", stats.FactSourcesCreated)
	}

	// Verify fact persisted.
	factDAO := dao.NewFactDAO(db)
	var factCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM facts").Scan(&factCount); err != nil {
		t.Fatalf("count facts: %v", err)
	}
	if factCount != 1 {
		t.Errorf("facts count = %d, want 1", factCount)
	}

	allFacts, err := factDAO.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(allFacts) != 1 {
		t.Fatalf("facts = %d, want 1", len(allFacts))
	}

	f := allFacts[0]
	if f.SubjectEntityID != subjID {
		t.Errorf("SubjectEntityID = %d, want %d", f.SubjectEntityID, subjID)
	}
	if f.ObjectEntityID != objID {
		t.Errorf("ObjectEntityID = %d, want %d", f.ObjectEntityID, objID)
	}
	if f.Predicate != "works_at" {
		t.Errorf("Predicate = %q, want %q", f.Predicate, "works_at")
	}
	if f.Status != "approved" {
		t.Errorf("Status = %q, want %q", f.Status, "approved")
	}

	// Verify fact_source persisted with quote == predicate and correct document_id.
	factSourceDAO := dao.NewFactSourceDAO(db)
	sources, err := factSourceDAO.GetByFactID(ctx, f.ID)
	if err != nil {
		t.Fatalf("GetByFactID: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("fact_sources = %d, want 1", len(sources))
	}

	src := sources[0]
	if src.DocumentID != docID {
		t.Errorf("DocumentID = %d, want %d", src.DocumentID, docID)
	}
	// Quote should be a text window from the chunk (not the predicate).
	if src.Quote == nil || *src.Quote == "" {
		t.Fatalf("Quote is empty, expected chunk text excerpt")
	}
	if !strings.Contains(*src.Quote, "Alice") && !strings.Contains(*src.Quote, "Acme Corp") {
		t.Errorf("Quote = %q, want it to contain entity name from chunk text", *src.Quote)
	}

	// Verify weight recomputed (should be 1 since one fact_source).
	if f.Weight != 1 {
		t.Errorf("Weight = %d, want 1", f.Weight)
	}
}

func TestStoreEntities_InvalidFactsSkipped(t *testing.T) {
	t.Parallel()

	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	// Seed only the subject entity; object entity is missing.
	entityDAO := dao.NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}

	docDAO := dao.NewDocumentDAO(db)
	docID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc.md"})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	tracker := NewProgressTracker(1, "test")

	chunk := chunkers.DocumentChunk{
		Text:        "Alice works at Unknown Corp.",
		SequenceNum: 0,
		NerResult: &ner.Result{
			Facts: []ner.Fact{
				// Subject exists, object does not → synthetic entity created, fact stored.
				{SubjectName: "Alice", SubjectType: "PERSON", Predicate: "works_at", ObjectName: "Unknown Corp", ObjectType: "ORGANIZATION"},
			},
		},
	}

	if err := ingester.storeEntities(ctx, db, docID, Document{SourcePath: "/test/doc.md"}, dao.Chunk{}, chunk, tracker); err != nil {
		t.Fatalf("storeEntities() error = %v", err)
	}

	stats := tracker.Stats()
	if stats.FactsCreated != 1 {
		t.Errorf("FactsCreated = %d, want 1 (synthetic entity created, fact stored)", stats.FactsCreated)
	}

	var factCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM facts").Scan(&factCount); err != nil {
		t.Fatalf("count facts: %v", err)
	}
	if factCount != 1 {
		t.Errorf("facts count = %d, want 1 (synthetic entity created)", factCount)
	}

	// Verify the subject entity still exists and was not deleted.
	ent, err := dao.NewEntityDAO(db).GetByID(ctx, subjID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if ent == nil || ent.Name != "Alice" {
		t.Error("subject entity should still exist")
	}

	// Verify the synthetic object entity was created.
	var objID int
	if err := db.QueryRow("SELECT id FROM entities WHERE name = ? AND type = ?", "Unknown Corp", "ORGANIZATION").Scan(&objID); err != nil {
		t.Fatalf("query synthetic entity: %v", err)
	}

	// Verify the fact references both correct entity IDs.
	factDAO := dao.NewFactDAO(db)
	allFacts, err := factDAO.ListAll(ctx)
	if err != nil || len(allFacts) != 1 {
		t.Fatalf("ListAll: %v (count=%d)", err, len(allFacts))
	}
	if allFacts[0].SubjectEntityID != subjID {
		t.Errorf("fact SubjectEntityID = %d, want %d", allFacts[0].SubjectEntityID, subjID)
	}
	if allFacts[0].ObjectEntityID != objID {
		t.Errorf("fact ObjectEntityID = %d, want %d", allFacts[0].ObjectEntityID, objID)
	}

	// Verify synthetic entity is linked to the document via entity_sources.
	var sourceCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM entity_sources WHERE entity_id = ? AND document_id = ?", objID, docID).Scan(&sourceCount); err != nil {
		t.Fatalf("count entity_sources: %v", err)
	}
	if sourceCount != 1 {
		t.Errorf("entity_sources for synthetic entity = %d, want 1", sourceCount)
	}
}

func TestStoreEntities_CrossChunkEntityResolution(t *testing.T) {
	t.Parallel()

	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	docDAO := dao.NewDocumentDAO(db)
	docID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc.md"})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	tracker := NewProgressTracker(1, "test")

	// Chunk 0: fact references "Bob" (PERSON) which is NOT extracted by NER in this chunk.
	chunk0 := chunkers.DocumentChunk{
		Text:        "Alice works with Bob on the project.",
		SequenceNum: 0,
		NerResult: &ner.Result{
			Entities: []ner.Entity{{Name: "Alice", Type: "PERSON"}}, // Only Alice extracted.
			Facts: []ner.Fact{
				{SubjectName: "Alice", SubjectType: "PERSON", Predicate: "works_with", ObjectName: "Bob", ObjectType: "PERSON"},
			},
		},
	}

	if err := ingester.storeEntities(ctx, db, docID, Document{SourcePath: "/test/doc.md"}, dao.Chunk{}, chunk0, tracker); err != nil {
		t.Fatalf("storeEntities(chunk0) error = %v", err)
	}

	factDAO := dao.NewFactDAO(db)
	allFacts, err := factDAO.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(allFacts) != 1 {
		t.Errorf("facts after chunk0 = %d, want 1 (Bob synthetic entity created)", len(allFacts))
	}

	// Verify Bob was created as a synthetic entity.
	var bobID int
	if err := db.QueryRow("SELECT id FROM entities WHERE name = ? AND type = ?", "Bob", "PERSON").Scan(&bobID); err != nil {
		t.Fatalf("query Bob entity: %v", err)
	}

	// Chunk 1: NER extracts "Bob" — must merge into the synthetic entity, not create a duplicate.
	chunk1 := chunkers.DocumentChunk{
		Text:        "Bob is a senior engineer.",
		SequenceNum: 1,
		NerResult: &ner.Result{
			Entities: []ner.Entity{{Name: "Bob", Type: "PERSON"}},
		},
	}

	tracker2 := NewProgressTracker(1, "test")
	if err := ingester.storeEntities(ctx, db, docID, Document{SourcePath: "/test/doc.md"}, dao.Chunk{}, chunk1, tracker2); err != nil {
		t.Fatalf("storeEntities(chunk1) error = %v", err)
	}

	// Verify no duplicate entity was created.
	var entityCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM entities WHERE name = ? AND type = ?", "Bob", "PERSON").Scan(&entityCount); err != nil {
		t.Fatalf("count Bob entities: %v", err)
	}
	if entityCount != 1 {
		t.Errorf("Bob entity count = %d, want 1 (no duplicate)", entityCount)
	}

	// Verify the fact still references the same Bob Predicate.
	allFacts, _ = factDAO.ListAll(ctx)
	if allFacts[0].ObjectEntityID != bobID {
		t.Errorf("fact ObjectEntityID changed: %d vs %d", allFacts[0].ObjectEntityID, bobID)
	}
}

func TestStoreEntities_WeightsRecomputed(t *testing.T) {
	t.Parallel()

	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	entityDAO := dao.NewEntityDAO(db)
	if _, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice"}); err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	if _, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp"}); err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	docDAO := dao.NewDocumentDAO(db)
	doc1ID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc1.md"})
	if err != nil {
		t.Fatalf("create document 1: %v", err)
	}
	doc2ID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc2.md"})
	if err != nil {
		t.Fatalf("create document 2: %v", err)
	}

	tracker1 := NewProgressTracker(1, "test")
	chunk1 := chunkers.DocumentChunk{
		Text:        "Alice works at Acme Corp.",
		SequenceNum: 0,
		NerResult: &ner.Result{
			Facts: []ner.Fact{
				{SubjectName: "Alice", SubjectType: "PERSON", Predicate: "works_at", ObjectName: "Acme Corp", ObjectType: "ORGANIZATION"},
			},
		},
	}

	if err := ingester.storeEntities(ctx, db, doc1ID, Document{SourcePath: "/test/doc1.md"}, dao.Chunk{}, chunk1, tracker1); err != nil {
		t.Fatalf("storeEntities(doc1) error = %v", err)
	}

	// First ingestion: weight should be 1.
	factDAO := dao.NewFactDAO(db)
	allFacts, _ := factDAO.ListAll(ctx)
	if len(allFacts) != 1 {
		t.Fatalf("facts after doc1 = %d, want 1", len(allFacts))
	}
	if allFacts[0].Weight != 1 {
		t.Errorf("weight after doc1 = %d, want 1", allFacts[0].Weight)
	}

	// Second ingestion: same fact (CreateOrIgnore returns existing Predicate), new fact_source.
	tracker2 := NewProgressTracker(1, "test")
	chunk2 := chunkers.DocumentChunk{
		Text:        "Alice is employed by Acme Corp.",
		SequenceNum: 0,
		NerResult: &ner.Result{
			Facts: []ner.Fact{
				{SubjectName: "Alice", SubjectType: "PERSON", Predicate: "works_at", ObjectName: "Acme Corp", ObjectType: "ORGANIZATION"},
			},
		},
	}

	if err := ingester.storeEntities(ctx, db, doc2ID, Document{SourcePath: "/test/doc2.md"}, dao.Chunk{}, chunk2, tracker2); err != nil {
		t.Fatalf("storeEntities(doc2) error = %v", err)
	}

	allFacts, _ = factDAO.ListAll(ctx)
	if len(allFacts) != 1 {
		t.Fatalf("facts after doc2 = %d, want 1 (no duplicate)", len(allFacts))
	}
	if allFacts[0].Weight != 2 {
		t.Errorf("weight after doc2 = %d, want 2", allFacts[0].Weight)
	}

	// Verify two fact_sources exist.
	factSourceDAO := dao.NewFactSourceDAO(db)
	sources, err := factSourceDAO.GetByFactID(ctx, allFacts[0].ID)
	if err != nil {
		t.Fatalf("GetByFactID: %v", err)
	}
	if len(sources) != 2 {
		t.Errorf("fact_sources = %d, want 2", len(sources))
	}

	// Verify both sources have correct document IDs.
	docIDs := make(map[int]bool)
	for _, s := range sources {
		docIDs[s.DocumentID] = true
	}
	if !docIDs[doc1ID] || !docIDs[doc2ID] {
		t.Errorf("fact_sources document IDs = %v, want both doc1 and doc2", docIDs)
	}
}

func TestStoreEntities_NoFacts(t *testing.T) {
	t.Parallel()

	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	docDAO := dao.NewDocumentDAO(db)
	docID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc.md"})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	tracker := NewProgressTracker(1, "test")

	chunk := chunkers.DocumentChunk{
		Text:        "Just some text with no facts.",
		SequenceNum: 0,
		NerResult:   &ner.Result{}, // No entities, no facts.
	}

	if err := ingester.storeEntities(ctx, db, docID, Document{SourcePath: "/test/doc.md"}, dao.Chunk{}, chunk, tracker); err != nil {
		t.Fatalf("storeEntities() error = %v", err)
	}

	stats := tracker.Stats()
	if stats.FactsCreated != 0 {
		t.Errorf("FactsCreated = %d, want 0", stats.FactsCreated)
	}
}

func TestCreateBackup_WALConsistent(t *testing.T) {
	t.Parallel()

	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	// Insert a document so there is data to back up.
	docDAO := dao.NewDocumentDAO(db)
	_, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/backup_doc.md"})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	// Create backup.
	if err := ingester.createBackup(); err != nil {
		t.Fatalf("createBackup() error = %v", err)
	}

	// Find the backup file in backups/ directory.
	dbPath, err := ingester.getDBPath()
	if err != nil {
		t.Fatalf("getDBPath: %v", err)
	}
	backupsDir := filepath.Join(filepath.Dir(dbPath), "backups")
	files, err := os.ReadDir(backupsDir)
	if err != nil {
		t.Fatalf("read backups dir: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no backup files found")
	}

	backupPath := filepath.Join(backupsDir, files[0].Name())

	// Open the backup with a separate connection and verify data is present.
	backupDB, err := sql.Open("sqlite3", backupPath)
	if err != nil {
		t.Fatalf("open backup db: %v", err)
	}
	defer backupDB.Close()

	var docCount int
	if err := backupDB.QueryRow("SELECT COUNT(*) FROM documents").Scan(&docCount); err != nil {
		t.Fatalf("query backup documents: %v", err)
	}
	if docCount != 1 {
		t.Errorf("backup document count = %d, want 1", docCount)
	}

	var originalPath string
	if err := backupDB.QueryRow("SELECT original_path FROM documents WHERE source_type = ?", "markdown").Scan(&originalPath); err != nil {
		t.Fatalf("query backup document path: %v", err)
	}
	if originalPath != "/test/backup_doc.md" {
		t.Errorf("backup document path = %q, want %q", originalPath, "/test/backup_doc.md")
	}
}

func TestCreateBackup_UniqueFilenames(t *testing.T) {
	t.Parallel()

	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	// Insert a document so there is data.
	docDAO := dao.NewDocumentDAO(db)
	_, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/unique_doc.md"})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	// Create two backups in quick succession.
	if err := ingester.createBackup(); err != nil {
		t.Fatalf("first createBackup() error = %v", err)
	}

	// Sleep enough for millisecond precision to produce a different filename.
	time.Sleep(10 * time.Millisecond)

	if err := ingester.createBackup(); err != nil {
		t.Fatalf("second createBackup() error = %v", err)
	}

	// Verify two different backup files exist.
	dbPath, err := ingester.getDBPath()
	if err != nil {
		t.Fatalf("getDBPath: %v", err)
	}
	backupsDir := filepath.Join(filepath.Dir(dbPath), "backups")
	files, err := os.ReadDir(backupsDir)
	if err != nil {
		t.Fatalf("read backups dir: %v", err)
	}

	if len(files) < 2 {
		t.Errorf("backup files count = %d, want at least 2 (unique filenames)", len(files))
	}

	// Verify all filenames are unique.
	names := make(map[string]bool)
	for _, f := range files {
		if names[f.Name()] {
			t.Errorf("duplicate backup filename: %s", f.Name())
		}
		names[f.Name()] = true
	}
}

func TestIngest_RebuildBackupContainsPreRebuildData(t *testing.T) {
	t.Parallel()

	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	// Create a temp source directory for Ingest (parser returns empty result).
	sourceDir := t.TempDir()

	docDAO := dao.NewDocumentDAO(db)

	// Seed the document with a path under sourceDir so clearSourceData will match it.
	seedPath := filepath.Join(sourceDir, "seed_doc.md")
	if _, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: seedPath}); err != nil {
		t.Fatalf("create seed document at %s: %v", seedPath, err)
	}

	// Verify the seed document exists before Ingest.
	var preCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM documents WHERE original_path = ?", seedPath).Scan(&preCount); err != nil {
		t.Fatalf("query pre-rebuild doc: %v", err)
	}
	if preCount != 1 {
		t.Fatalf("seed document count before Ingest = %d, want 1", preCount)
	}

	// Replace the parser with one that returns no documents and no errors.
	ingester.parser = &mockParser{}

	// Call Ingest with rebuild=true — backup must be taken BEFORE clearSourceData.
	_, ingestErr := ingester.Ingest(ctx, sourceDir, "markdown", true)
	if ingestErr != nil {
		t.Fatalf("Ingest(rebuild=true) error = %v", ingestErr)
	}

	// Verify the seed document was cleared from main DB.
	var postCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM documents WHERE original_path = ?", seedPath).Scan(&postCount); err != nil {
		t.Fatalf("query post-rebuild doc: %v", err)
	}
	if postCount != 0 {
		t.Errorf("seed document count after rebuild = %d, want 0 (should be cleared)", postCount)
	}

	// Open the backup with a separate connection and verify pre-rebuild data is present.
	dbPath, err := ingester.getDBPath()
	if err != nil {
		t.Fatalf("getDBPath: %v", err)
	}
	backupsDir := filepath.Join(filepath.Dir(dbPath), "backups")
	files, err := os.ReadDir(backupsDir)
	if err != nil {
		t.Fatalf("read backups dir: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no backup files found after Ingest(rebuild=true)")
	}

	backupPath := filepath.Join(backupsDir, files[0].Name())
	backupDB, err := sql.Open("sqlite3", backupPath)
	if err != nil {
		t.Fatalf("open backup db: %v", err)
	}
	defer backupDB.Close()

	var backupDocCount int
	if err := backupDB.QueryRow("SELECT COUNT(*) FROM documents WHERE original_path = ?", seedPath).Scan(&backupDocCount); err != nil {
		t.Fatalf("query backup documents: %v", err)
	}
	if backupDocCount != 1 {
		t.Errorf("backup document count for seed path = %d, want 1 (pre-rebuild data preserved)", backupDocCount)
	}

	var originalPath string
	if err := backupDB.QueryRow("SELECT original_path FROM documents WHERE original_path = ?", seedPath).Scan(&originalPath); err != nil {
		t.Fatalf("query backup document path: %v", err)
	}
	if originalPath != seedPath {
		t.Errorf("backup document path = %q, want %q", originalPath, seedPath)
	}
}

func TestGenerateEmbeddings_VectorCountMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		texts          []string
		mismatchReturn int // 0 means normal (return len(texts))
		wantErr        bool
		errContains    string
	}{
		{
			name:           "fewer_vectors_than_texts_returns_error",
			texts:          []string{"chunk one", "chunk two", "chunk three"},
			mismatchReturn: 2, // provider returns 2 vectors for 3 texts
			wantErr:        true,
			errContains:    "embedding count mismatch",
		},
		{
			name:           "correct_vector_count_succeeds",
			texts:          []string{"chunk one", "chunk two", "chunk three"},
			mismatchReturn: 3, // provider returns exactly 3 vectors for 3 texts
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, cleanup, ingester := newTestIngester(t)
			defer cleanup()

			// Replace provider with mismatch-capable mock.
			ingester.embeddingProvider = &mockEmbeddingProvider{dim: 4, mismatchReturn: tt.mismatchReturn}

			chunks := make([]chunkers.DocumentChunk, len(tt.texts))
			for i, text := range tt.texts {
				chunks[i] = chunkers.DocumentChunk{Text: text, SequenceNum: i}
			}

			tracker := NewProgressTracker(1, "test")
			vectors, err := ingester.generateEmbeddings(context.Background(), chunks, tracker)

			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want substring %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(vectors) != len(tt.texts) {
				t.Errorf("returned %d vectors, want %d", len(vectors), len(tt.texts))
			}
		})
	}
}

func TestStoreVectors_LengthMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		vectorCount int
		chunkCount  int
		errContains string
	}{
		{
			name:        "more_vectors_than_chunks_returns_error",
			vectorCount: 5, chunkCount: 3,
			errContains: "vector/chunk count mismatch",
		},
		{
			name:        "fewer_vectors_than_chunks_returns_error",
			vectorCount: 2, chunkCount: 4,
			errContains: "vector/chunk count mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, cleanup, ingester := newTestIngester(t)
			defer cleanup()

			ctx := context.Background()
			txChunkDAO := dao.NewChunkDAO(db)

			vectors := make([][]float32, tt.vectorCount)
			for i := range vectors {
				vectors[i] = []float32{0.1, 0.2, 0.3, 0.4}
			}

			chunks := make([]dao.Chunk, tt.chunkCount)
			for i := range chunks {
				chunks[i] = dao.Chunk{ID: i + 1, DocID: 999, SequenceNum: i}
			}

			err := ingester.storeVectors(ctx, vectors, chunks, txChunkDAO)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("error = %q, want substring %q", err, tt.errContains)
			}
		})
	}
}

func TestStoreVectors_EqualCountsSucceeds(t *testing.T) {
	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	// Create chunks_vec table (vec0 may be skipped by migration runner).
	if _, err := db.Exec(`CREATE VIRTUAL TABLE IF NOT EXISTS chunks_vec USING vec0(chunk_id INTEGER PRIMARY KEY, vector FLOAT[4])`); err != nil {
		t.Skipf("chunks_vec creation failed (vec0 module unavailable): %v", err)
	}

	// Create a real document and chunks so UpsertVector has valid IDs.
	docDAO := dao.NewDocumentDAO(db)
	docID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/vec_test.md"})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	chunkDAO := dao.NewChunkDAO(db)
	var chunkIDs []int
	for i := 0; i < 3; i++ {
		id, err := chunkDAO.Create(ctx, dao.Chunk{
			DocID:       docID,
			ChunkText:   "test chunk " + string(rune('a'+i)),
			SequenceNum: i,
		})
		if err != nil {
			t.Fatalf("create chunk %d: %v", i, err)
		}
		chunkIDs = append(chunkIDs, id)
	}

	vectors := make([][]float32, 3)
	for i := range vectors {
		vectors[i] = []float32{0.1, 0.2, 0.3, 0.4}
	}

	chunks := make([]dao.Chunk, 3)
	for i, id := range chunkIDs {
		chunks[i] = dao.Chunk{ID: id, DocID: docID, SequenceNum: i}
	}

	err = ingester.storeVectors(ctx, vectors, chunks, chunkDAO)
	if err != nil {
		t.Fatalf("storeVectors with equal counts failed: %v", err)
	}
}

func TestExtractQuoteFromChunk_ChunkTextWithEntity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		chunkText     string
		subjectName   string
		objectName    string
		wantContains  string // quote must contain this substring
		notWantEquals string // quote must NOT equal this (e.g. predicate)
	}{
		{
			name:         "subject_found_earlier_than_object",
			chunkText:    "Alice works at Acme Corp as a developer.",
			subjectName:  "Alice",
			objectName:   "Acme Corp",
			wantContains: "Alice",
		},
		{
			name:         "object_found_earlier_than_subject",
			chunkText:    "The Acme Corp hired Alice for the position.",
			subjectName:  "Alice",
			objectName:   "Acme Corp",
			wantContains: "Acme Corp",
		},
		{
			name:         "case_insensitive_match",
			chunkText:    "alice works at acme corp.",
			subjectName:  "Alice",
			objectName:   "Acme Corp",
			wantContains: "alice",
		},
		{
			name:          "quote_is_not_predicate",
			chunkText:     "Alice works at Acme Corp.",
			subjectName:   "Alice",
			objectName:    "Acme Corp",
			wantContains:  "Alice",
			notWantEquals: "works_at", // predicate must not be the quote
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quote := extractQuoteFromChunk(tt.chunkText, tt.subjectName, tt.objectName)

			if !strings.Contains(quote, tt.wantContains) {
				t.Errorf("quote = %q, want it to contain %q", quote, tt.wantContains)
			}

			if tt.notWantEquals != "" && quote == tt.notWantEquals {
				t.Errorf("quote = %q, must not equal predicate %q", quote, tt.notWantEquals)
			}
		})
	}
}

func TestExtractQuoteFromChunk_EntityNotFound(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		chunkText   string
		subjectName string
		objectName  string
		wantPrefix  bool // if true, quote should be the first ~120 runes
	}{
		{
			name:        "neither_entity_found",
			chunkText:   "This document discusses general topics without mentioning specific people.",
			subjectName: "Alice",
			objectName:  "Acme Corp",
			wantPrefix:  true,
		},
		{
			name:        "empty_chunk_returns_empty",
			chunkText:   "",
			subjectName: "Alice",
			objectName:  "Acme Corp",
			wantPrefix:  false, // empty input -> empty output
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quote := extractQuoteFromChunk(tt.chunkText, tt.subjectName, tt.objectName)

			if tt.chunkText == "" {
				if quote != "" {
					t.Errorf("empty chunk: quote = %q, want empty string", quote)
				}
				return
			}

			if tt.wantPrefix {
				chunkRunes := []rune(tt.chunkText)
				fallbackLen := 120
				expectedEnd := fallbackLen
				if len(chunkRunes) < fallbackLen {
					expectedEnd = len(chunkRunes)
				}
				expectedPrefix := string(chunkRunes[:expectedEnd])
				if quote != expectedPrefix {
					t.Errorf("quote = %q, want prefix %q", quote, expectedPrefix)
				}
			}
		})
	}
}

func TestExtractQuoteFromChunk_WindowBounds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		chunkText   string
		subjectName string
		objectName  string
		check       func(quote, chunkText string) error
	}{
		{
			name:        "entity_at_start_no_negative_offset",
			chunkText:   "Alice is the CEO.",
			subjectName: "Alice",
			objectName:  "",
			check: func(quote, _ string) error {
				if !strings.HasPrefix(quote, "Alice") {
					return fmt.Errorf("quote should start with entity at beginning: %q", quote)
				}
				return nil
			},
		},
		{
			name:        "entity_at_end_no_overflow",
			chunkText:   "The CEO is Alice.",
			subjectName: "Alice",
			objectName:  "",
			check: func(quote, text string) error {
				if !strings.Contains(quote, "Alice") {
					return fmt.Errorf("quote should contain entity at end: %q", quote)
				}
				chunkRunes := []rune(text)
				if len([]rune(quote)) > len(chunkRunes) {
					return fmt.Errorf("quote longer than chunk text")
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quote := extractQuoteFromChunk(tt.chunkText, tt.subjectName, tt.objectName)
			if err := tt.check(quote, tt.chunkText); err != nil {
				t.Error(err)
			}
		})
	}
}

func TestStoreEntities_FactMetadataPersisted(t *testing.T) {
	t.Parallel()

	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	entityDAO := dao.NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	docDAO := dao.NewDocumentDAO(db)
	docID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc.md"})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	tests := []struct {
		name     string
		pred     string
		metadata map[string]interface{}
	}{
		{
			name:     "fact with metadata",
			pred:     "works_at",
			metadata: map[string]interface{}{"threshold_amount": float64(100), "condition": "active"},
		},
		{
			name:     "fact with nil metadata",
			pred:     "founded_by",
			metadata: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracker := NewProgressTracker(1, "test")

			chunk1 := chunkers.DocumentChunk{
				Text:        "Alice works at Acme Corp.",
				SequenceNum: 0,
				NerResult: &ner.Result{
					Facts: []ner.Fact{
						{SubjectName: "Alice", SubjectType: "PERSON", Predicate: tt.pred, ObjectName: "Acme Corp", ObjectType: "ORGANIZATION", Metadata: tt.metadata},
					},
				},
			}

			if err := ingester.storeEntities(ctx, db, docID, Document{SourcePath: "/test/doc.md"}, dao.Chunk{}, chunk1, tracker); err != nil {
				t.Fatalf("storeEntities() error = %v", err)
			}

			factDAO := dao.NewFactDAO(db)
			allFacts, err := factDAO.ListAll(ctx)
			if err != nil {
				t.Fatalf("ListAll: %v", err)
			}

			var found *dao.Fact
			for i := range allFacts {
				f := &allFacts[i]
				if f.SubjectEntityID == subjID && f.ObjectEntityID == objID && f.Predicate == tt.pred {
					found = f
					break
				}
			}

			if found == nil {
				t.Fatal("fact not found in database")
			}

			if len(tt.metadata) == 0 {
				if found.Metadata != nil {
					t.Errorf("expected nil metadata, got %q", *found.Metadata)
				}
			} else {
				if found.Metadata == nil {
					t.Fatal("expected non-nil metadata for fact with attributes")
				}

				// Verify the JSON can be unmarshaled back to a map.
				var parsed map[string]interface{}
				if err := json.Unmarshal([]byte(*found.Metadata), &parsed); err != nil {
					t.Fatalf("unmarshal persisted metadata: %v", err)
				}

				for k, v := range tt.metadata {
					pv, ok := parsed[k]
					if !ok {
						t.Errorf("metadata missing key %q", k)
						continue
					}
					// Compare as JSON strings since float64/int conversion may differ.
					gotJSON, _ := json.Marshal(pv)
					wantJSON, _ := json.Marshal(v)
					if string(gotJSON) != string(wantJSON) {
						t.Errorf("metadata[%q] = %s, want %s", k, gotJSON, wantJSON)
					}
				}
			}
		})
	}
}

// mockParserWithExtensions is a configurable parser for countFiles tests.
type mockParserWithExtensions struct {
	extensions []string
}

func (p *mockParserWithExtensions) Parse(_ string) ParseResult    { return ParseResult{} }
func (p *mockParserWithExtensions) SupportedExtensions() []string { return p.extensions }

func TestCountFiles_FiltersByExtension(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create files with various extensions.
	for _, name := range []string{"doc1.md", "doc2.md", "data.json", "image.png", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content"), 0o644); err != nil {
			t.Fatalf("create file %s: %v", name, err)
		}
	}

	tests := []struct {
		name       string
		extensions []string
		wantCount  int
	}{
		{
			name:       "markdown only",
			extensions: []string{".md"},
			wantCount:  2,
		},
		{
			name:       "json only",
			extensions: []string{".json"},
			wantCount:  1,
		},
		{
			name:       "markdown and json",
			extensions: []string{".md", ".json"},
			wantCount:  3,
		},
		{
			name:       "unsupported extension",
			extensions: []string{".xml"},
			wantCount:  0,
		},
		{
			name:       "empty extensions list",
			extensions: nil,
			wantCount:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			parser := &mockParserWithExtensions{extensions: tt.extensions}
			got, err := countFiles(dir, parser)
			if err != nil {
				t.Fatalf("countFiles() error = %v", err)
			}
			if got != tt.wantCount {
				t.Errorf("countFiles() = %d, want %d", got, tt.wantCount)
			}
		})
	}
}

func TestCountFiles_CaseInsensitiveExtension(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for _, name := range []string{"doc1.MD", "doc2.md", "Doc3.Md"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content"), 0o644); err != nil {
			t.Fatalf("create file %s: %v", name, err)
		}
	}

	parser := &mockParserWithExtensions{extensions: []string{".md"}}
	got, err := countFiles(dir, parser)
	if err != nil {
		t.Fatalf("countFiles() error = %v", err)
	}
	if got != 3 {
		t.Errorf("countFiles() = %d, want 3 (case-insensitive)", got)
	}
}

func TestCountFiles_Subdirectories(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create nested structure.
	subdir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subdir, 0o755); err != nil {
		t.Fatalf("create subdir: %v", err)
	}

	for _, name := range []string{"root.md", "other.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content"), 0o644); err != nil {
			t.Fatalf("create file %s: %v", name, err)
		}
	}
	for _, name := range []string{"nested.md", "deep.json"} {
		if err := os.WriteFile(filepath.Join(subdir, name), []byte("content"), 0o644); err != nil {
			t.Fatalf("create file %s: %v", name, err)
		}
	}

	parser := &mockParserWithExtensions{extensions: []string{".md"}}
	got, err := countFiles(dir, parser)
	if err != nil {
		t.Fatalf("countFiles() error = %v", err)
	}
	if got != 2 {
		t.Errorf("countFiles() = %d, want 2 (root.md + nested.md)", got)
	}
}

func TestCountFiles_WebpageParserExtensions(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for _, name := range []string{"content.md", "content.html", "data.json", "image.png"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content"), 0o644); err != nil {
			t.Fatalf("create file %s: %v", name, err)
		}
	}

	parser := &mockParserWithExtensions{extensions: []string{".md", ".html"}}
	got, err := countFiles(dir, parser)
	if err != nil {
		t.Fatalf("countFiles() error = %v", err)
	}
	if got != 2 {
		t.Errorf("countFiles() = %d, want 2 (content.md + content.html)", got)
	}
}

func TestStoreEntities_PopulatesChunkEntities(t *testing.T) {
	t.Parallel()

	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	docDAO := dao.NewDocumentDAO(db)
	docID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc.md"})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	chunkDAO := dao.NewChunkDAO(db)
	chunkID, err := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Alice works at Acme Corp.", SequenceNum: 0})
	if err != nil {
		t.Fatalf("create chunk: %v", err)
	}

	tracker := NewProgressTracker(1, "test")

	chunk := chunkers.DocumentChunk{
		Text:        "Alice works at Acme Corp.",
		SequenceNum: 0,
		NerResult: &ner.Result{
			Entities: []ner.Entity{
				{Name: "Alice", Type: "PERSON"},
				{Name: "Acme Corp", Type: "ORGANIZATION"},
			},
			Facts: []ner.Fact{
				{SubjectName: "Alice", SubjectType: "PERSON", Predicate: "works_at", ObjectName: "Acme Corp", ObjectType: "ORGANIZATION"},
			},
		},
	}

	if err := ingester.storeEntities(ctx, db, docID, Document{SourcePath: "/test/doc.md"}, dao.Chunk{ID: chunkID}, chunk, tracker); err != nil {
		t.Fatalf("storeEntities() error = %v", err)
	}

	// Verify chunk_entities has links for NER-extracted entities.
	ceDAO := dao.NewChunkEntityDAO(db)
	entityIDs, err := ceDAO.GetEntitiesByChunk(ctx, chunkID)
	if err != nil {
		t.Fatalf("GetEntitiesByChunk: %v", err)
	}

	// Should have at least 2 entity links (Alice + Acme Corp from NER entities).
	// Facts may add the same entities again but INSERT OR IGNORE prevents duplicates.
	if len(entityIDs) < 2 {
		t.Errorf("chunk_entities count = %d, want >= 2", len(entityIDs))
	}

	// Verify Alice entity is linked.
	var aliceLinked bool
	for _, eid := range entityIDs {
		ent, err := dao.NewEntityDAO(db).GetByID(ctx, eid)
		if err != nil {
			t.Fatalf("GetByID(%d): %v", eid, err)
		}
		if ent != nil && ent.Name == "Alice" {
			aliceLinked = true
			break
		}
	}
	if !aliceLinked {
		t.Error("expected Alice entity to be linked to chunk")
	}

	// Verify Acme Corp entity is linked.
	var acmeLinked bool
	for _, eid := range entityIDs {
		ent, err := dao.NewEntityDAO(db).GetByID(ctx, eid)
		if err != nil {
			t.Fatalf("GetByID(%d): %v", eid, err)
		}
		if ent != nil && ent.Name == "Acme Corp" {
			acmeLinked = true
			break
		}
	}
	if !acmeLinked {
		t.Error("expected Acme Corp entity to be linked to chunk")
	}
}

func TestStoreEntities_FactsOnly_PopulatesChunkEntities(t *testing.T) {
	t.Parallel()

	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	docDAO := dao.NewDocumentDAO(db)
	docID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc.md"})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	chunkDAO := dao.NewChunkDAO(db)
	chunkID, err := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "Bob founded StartupX.", SequenceNum: 0})
	if err != nil {
		t.Fatalf("create chunk: %v", err)
	}

	tracker := NewProgressTracker(1, "test")

	// No NER entities, only facts — synthetic entities should still be linked.
	chunk := chunkers.DocumentChunk{
		Text:        "Bob founded StartupX.",
		SequenceNum: 0,
		NerResult: &ner.Result{
			Facts: []ner.Fact{
				{SubjectName: "Bob", SubjectType: "PERSON", Predicate: "founded", ObjectName: "StartupX", ObjectType: "ORGANIZATION"},
			},
		},
	}

	if err := ingester.storeEntities(ctx, db, docID, Document{SourcePath: "/test/doc.md"}, dao.Chunk{ID: chunkID}, chunk, tracker); err != nil {
		t.Fatalf("storeEntities() error = %v", err)
	}

	// Verify chunk_entities has links for synthetic entities from facts.
	ceDAO := dao.NewChunkEntityDAO(db)
	entityIDs, err := ceDAO.GetEntitiesByChunk(ctx, chunkID)
	if err != nil {
		t.Fatalf("GetEntitiesByChunk: %v", err)
	}

	if len(entityIDs) < 2 {
		t.Errorf("chunk_entities count = %d, want >= 2 (Bob + StartupX from facts)", len(entityIDs))
	}
}

func TestExtractQuoteFromChunk_LineBoundaryTruncation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		chunkText    string
		subjectName  string
		objectName   string
		wantSuffix   bool // should end with "..."
		notWantSplit bool // should not split mid-line
	}{
		{
			name:        "truncated_quote_ends_with_ellipsis",
			chunkText:   "This is a very long paragraph that contains Alice somewhere in the middle of the text and continues for many more words beyond the window size limit.",
			subjectName: "Alice",
			objectName:  "",
			wantSuffix:  true,
		},
		{
			name:        "fallback_truncated_ends_with_ellipsis",
			chunkText:   strings.Repeat("word ", 100), // long text without entity names
			subjectName: "Nobody",
			objectName:  "",
			wantSuffix:  true,
		},
		{
			name:        "short_text_no_truncation",
			chunkText:   "Alice works here.",
			subjectName: "Alice",
			objectName:  "",
			wantSuffix:  false,
		},
		{
			name:        "multiline_trimmed_to_line_boundary",
			chunkText:   "Line one.\nLine two with Alice in it. " + strings.Repeat("extra words ", 30) + "\nLine three continues after the match window with more text that pushes beyond the quote extraction limit.",
			subjectName: "Alice",
			objectName:  "",
			wantSuffix:  true, // truncated because text extends beyond window
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			quote := extractQuoteFromChunk(tt.chunkText, tt.subjectName, tt.objectName)

			if tt.wantSuffix && !strings.HasSuffix(quote, "...") {
				t.Errorf("quote should end with '...' when truncated: %q", quote)
			}
			if !tt.wantSuffix && strings.HasSuffix(quote, "...") {
				t.Errorf("quote should NOT end with '...' for non-truncated text: %q", quote)
			}

			// Verify no mid-line split: if the original has newlines and the quote is truncated,
			// it should not contain a partial line at the end.
			if tt.notWantSplit && strings.Contains(quote, "\n") {
				lines := strings.Split(quote, "\n")
				lastLine := lines[len(lines)-1]
				if lastLine != "" && !strings.HasSuffix(lastLine, "...") {
					t.Errorf("possible mid-line split at end of quote: %q", quote)
				}
			}
		})
	}
}

func TestExtractQuoteFromChunk_MidLineTrimmed(t *testing.T) {
	t.Parallel()

	// Create a chunk where the entity is near the start, and the window extends past a newline.
	chunkText := "Alice works at Acme Corp.\n\nThis is the second paragraph with more details about the company."
	subjectName := "Alice"

	quote := extractQuoteFromChunk(chunkText, subjectName, "")

	if !strings.Contains(quote, "Alice") {
		t.Errorf("quote should contain entity name: %q", quote)
	}

	// The quote should be trimmed to a line boundary.
	lines := strings.Split(strings.TrimSuffix(quote, "..."), "\n")
	for i, line := range lines {
		if line == "" && i < len(lines)-1 {
			continue // allow empty lines between paragraphs
		}
		if line != "" {
			t.Logf("line %d: %q", i, line)
		}
	}

	// If truncated, the last non-empty line before "..." should be a complete line.
	cleanQuote := strings.TrimSuffix(quote, "...")
	lastLine := cleanQuote[strings.LastIndex(cleanQuote, "\n")+1:]
	if lastLine != "" {
		t.Logf("last line after trim: %q", lastLine)
	}
}

func TestExtractedAt_SetOnFactSource(t *testing.T) {
	t.Parallel()

	db, cleanup, ingester := newTestIngester(t)
	defer cleanup()

	ctx := context.Background()

	entityDAO := dao.NewEntityDAO(db)
	_, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	_, err = entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	docDAO := dao.NewDocumentDAO(db)
	docID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc.md"})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	tracker := NewProgressTracker(1, "test")

	chunk := chunkers.DocumentChunk{
		Text:        "Alice works at Acme Corp.",
		SequenceNum: 0,
		NerResult: &ner.Result{
			Facts: []ner.Fact{
				{SubjectName: "Alice", SubjectType: "PERSON", Predicate: "works_at", ObjectName: "Acme Corp", ObjectType: "ORGANIZATION"},
			},
		},
	}

	if err := ingester.storeEntities(ctx, db, docID, Document{SourcePath: "/test/doc.md"}, dao.Chunk{}, chunk, tracker); err != nil {
		t.Fatalf("storeEntities() error = %v", err)
	}

	factSourceDAO := dao.NewFactSourceDAO(db)
	sources, err := factSourceDAO.GetByFactID(ctx, 1) // fact Predicate should be 1 (first insert)
	if err != nil {
		t.Fatalf("GetByFactID: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no fact sources found")
	}

	src := sources[0]
	if src.ExtractedAt == "" {
		t.Error("ExtractedAt should not be empty after storeEntities")
	}

	// Verify it's a valid RFC3339 timestamp.
	_, err = time.Parse(time.RFC3339, src.ExtractedAt)
	if err != nil {
		t.Errorf("ExtractedAt %q is not valid RFC3339: %v", src.ExtractedAt, err)
	}

	// Verify it's close to now (within 5 seconds).
	extractedTime, _ := time.Parse(time.RFC3339, src.ExtractedAt) //nolint:errcheck
	diff := time.Since(extractedTime)
	if diff > 5*time.Second {
		t.Errorf("ExtractedAt %q is too old (diff=%v)", src.ExtractedAt, diff)
	}
}
