package runner_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devmix/synopsis/internal/cache"
	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/domain"
	"github.com/devmix/synopsis/internal/ingestion/runner"
	"github.com/devmix/synopsis/internal/logger"
)

// newTestRunner creates a temp SQLite database with migrations applied and a
// Runner whose sources point at srcDirs. The embedding provider is API mode
// (never actually called by the tests that use this helper).
func newTestRunner(t *testing.T, srcDirs []string) (*runner.Runner, *sql.DB, *config.Config) {
	t.Helper()

	log, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Embeddings.Mode = "api"
	cfg.Embeddings.API.BaseURL = "http://127.0.0.1:1/v1"
	cfg.Embeddings.API.ModelName = "test-model"
	cfg.Embeddings.API.VectorDim = 4
	dataDir := t.TempDir()
	cfg.Paths.DataDir = dataDir
	cfg.Paths.MigrationsDir = "../../../migrations"

	// Build a global config with sources from srcDirs.
	sources := make([]config.SourceConfig, 0, len(srcDirs))
	for _, d := range srcDirs {
		sources = append(sources, config.SourceConfig{Path: d, Type: "markdown"})
	}
	globalCfg := &config.GlobalConfig{Sources: sources}

	db, err := database.Open(filepath.Join(dataDir, "test.db"), cfg.VectorDim(),
		database.WithMigrationsDir(cfg.Paths.MigrationsDir))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cacheStore, err := cache.NewStore(filepath.Join(dataDir, "test_cache.db"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	t.Cleanup(func() { _ = cacheStore.Close() })

	ingRunner, err := runner.NewRunner(cfg, db.DB(), log,
		runner.WithCacheStore(cacheStore),
		runner.WithGlobalConfig(globalCfg),
	)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = ingRunner.Close() })

	return ingRunner, db.DB(), &cfg
}

func seedDocument(t *testing.T, db *sql.DB, path string) {
	t.Helper()
	_, err := dao.NewDocumentDAO(db).Create(context.Background(), dao.Document{
		SourceType:   "markdown",
		OriginalPath: path,
	})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}
}

func TestPruneDeleted(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	existing := filepath.Join(srcDir, "keep.md")
	if err := os.WriteFile(existing, []byte("# Keep"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	removed := filepath.Join(srcDir, "gone.md") // never created on disk

	ingRunner, db, _ := newTestRunner(t, []string{srcDir})

	seedDocument(t, db, existing)
	seedDocument(t, db, removed)
	// A document outside the configured sources must never be pruned.
	seedDocument(t, db, filepath.Join(t.TempDir(), "outside.md"))

	pruned, err := ingRunner.PruneDeleted(context.Background())
	if err != nil {
		t.Fatalf("PruneDeleted: %v", err)
	}

	if pruned != 1 {
		t.Fatalf("PruneDeleted() = %d, want 1", pruned)
	}

	left, err := dao.NewDocumentDAO(db).Count(context.Background())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if left != 2 {
		t.Errorf("documents after prune = %d, want 2", left)
	}
}

// TestSourceForPath covers the changed-file → source mapping used by the
// auto-update watcher callback.
func TestSourceForPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	srcA := filepath.Join(root, "a")
	srcB := filepath.Join(root, "b")
	for _, d := range []string{srcA, srcB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}

	ingRunner, _, _ := newTestRunner(t, []string{srcA, srcB})

	tests := []struct {
		name    string
		path    string
		wantDir string
		wantOK  bool
	}{
		{name: "file inside source", path: filepath.Join(srcA, "doc.md"), wantDir: srcA, wantOK: true},
		{name: "file in nested subdirectory", path: filepath.Join(srcA, "sub", "doc.md"), wantDir: srcA, wantOK: true},
		{name: "source directory itself", path: srcB, wantDir: srcB, wantOK: true},
		{name: "file outside sources", path: filepath.Join(root, "other.md"), wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, ok := ingRunner.SourceForPath(tt.path)
			if ok != tt.wantOK {
				t.Fatalf("SourceForPath(%q) ok = %v, want %v", tt.path, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if src.Path != tt.wantDir {
				t.Fatalf("SourceForPath(%q) = %q, want %q", tt.path, src.Path, tt.wantDir)
			}
		})
	}
}

// newTestRunnerWithEmbeddingServer creates a test runner backed by an httptest
// server that serves the OpenAI-compatible /embeddings endpoint. The vector
// dimension is controlled by dim (default 4).
func newTestRunnerWithEmbeddingServer(t *testing.T, srcDirs []string, sourceType string) (*runner.Runner, *sql.DB, *config.Config) {
	t.Helper()

	dim := 4

	embedServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		respData := make([]struct {
			Embedding []float64 `json:"embedding"`
		}, len(req.Input))
		for i := range req.Input {
			vec := make([]float64, dim)
			for j := 0; j < dim; j++ {
				vec[j] = float64(j+1) / float64(dim)
			}
			respData[i].Embedding = vec
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"data": respData,
		}); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
	t.Cleanup(embedServer.Close)

	log, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Embeddings.Mode = "api"
	cfg.Embeddings.API.BaseURL = embedServer.URL
	cfg.Embeddings.API.ModelName = "test-model"
	cfg.Embeddings.API.VectorDim = dim
	dataDir := t.TempDir()
	cfg.Paths.DataDir = dataDir
	cfg.Paths.MigrationsDir = "../../../migrations"

	// Build a global config with sources from srcDirs.
	sources := make([]config.SourceConfig, 0, len(srcDirs))
	for _, d := range srcDirs {
		sources = append(sources, config.SourceConfig{Path: d, Type: sourceType})
	}
	globalCfg := &config.GlobalConfig{Sources: sources}

	db, err := database.Open(filepath.Join(dataDir, "test.db"), cfg.VectorDim(),
		database.WithMigrationsDir(cfg.Paths.MigrationsDir))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cacheStore, err := cache.NewStore(filepath.Join(dataDir, "test_cache.db"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	t.Cleanup(func() { _ = cacheStore.Close() })

	ingRunner, err := runner.NewRunner(cfg, db.DB(), log,
		runner.WithCacheStore(cacheStore),
		runner.WithGlobalConfig(globalCfg),
	)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = ingRunner.Close() })

	return ingRunner, db.DB(), &cfg
}

func TestE2EUnstructuredMarkdown(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "handbook.md"), []byte("# Employee Handbook\n\n## Policies\n\nAttendance rules."), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ingRunner, _, _ := newTestRunnerWithEmbeddingServer(t, []string{srcDir}, "unstructured")

	progress, err := ingRunner.IngestSource(context.Background(), config.SourceConfig{Path: srcDir, Type: "unstructured"}, true)
	if err != nil {
		t.Fatalf("IngestSource: %v", err)
	}

	if progress.DocumentsCreated <= 0 {
		t.Errorf("DocumentsCreated = %d, want > 0", progress.DocumentsCreated)
	}
}

func TestE2EJson(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "policies.json"), []byte(`[{"title":"NDA Policy","description":"Confidentiality agreement"}]`), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	ingRunner, _, _ := newTestRunnerWithEmbeddingServer(t, []string{srcDir}, "json")

	progress, err := ingRunner.IngestSource(context.Background(), config.SourceConfig{Path: srcDir, Type: "json"}, true)
	if err != nil {
		t.Fatalf("IngestSource: %v", err)
	}

	if progress.DocumentsCreated <= 0 {
		t.Errorf("DocumentsCreated = %d, want > 0", progress.DocumentsCreated)
	}
}

func TestCleanupOrphanedData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		seedSetup          func(*sql.DB, context.Context)
		wantEntitiesDel    int
		wantFactsDel       int64
		wantEntityTypeKept bool // EntityType entities must never be deleted
	}{
		{
			name: "no orphans — nothing deleted",
			seedSetup: func(db *sql.DB, ctx context.Context) {
				// Create entity with a source link → not orphaned.
				entityDAO := dao.NewEntityDAO(db)
				eid, err := entityDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice"})
				if err != nil {
					panic(err)
				}
				docDAO := dao.NewDocumentDAO(db)
				did, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc.md"})
				if err != nil {
					panic(err)
				}
				srcDAO := dao.NewEntitySourceDAO(db)
				if _, err := srcDAO.Create(ctx, dao.EntitySource{EntityID: eid, DocumentID: did}); err != nil {
					panic(err)
				}
			},
			wantEntitiesDel:    0,
			wantFactsDel:       0,
			wantEntityTypeKept: false, // no EntityType created
		},
		{
			name: "orphaned entity deleted",
			seedSetup: func(db *sql.DB, ctx context.Context) {
				// Create entity WITHOUT a source link → orphaned.
				entityDAO := dao.NewEntityDAO(db)
				if _, err := entityDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Orphan"}); err != nil {
					panic(err)
				}
			},
			wantEntitiesDel:    1,
			wantFactsDel:       0,
			wantEntityTypeKept: false,
		},
		{
			name: "orphaned fact deleted",
			seedSetup: func(db *sql.DB, ctx context.Context) {
				entityDAO := dao.NewEntityDAO(db)
				subjID, err := entityDAO.Create(ctx, dao.Entity{Type: "employee", Name: "Alice"})
				if err != nil {
					panic(err)
				}
				objID, err := entityDAO.Create(ctx, dao.Entity{Type: "EntityType", Name: "employee"})
				if err != nil {
					panic(err)
				}
				docDAO := dao.NewDocumentDAO(db)
				did, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc.md"})
				if err != nil {
					panic(err)
				}
				srcDAO := dao.NewEntitySourceDAO(db)
				// Link Alice to a document so it's NOT an orphaned entity.
				if _, err := srcDAO.Create(ctx, dao.EntitySource{EntityID: subjID, DocumentID: did}); err != nil {
					panic(err)
				}
				factDAO := dao.NewFactDAO(db)
				if _, err := factDAO.Create(ctx, dao.Fact{
					SubjectEntityID: subjID,
					Predicate:       "is_a",
					ObjectEntityID:  objID,
				}); err != nil {
					panic(err)
				}
				// No fact_source created → orphaned fact.
				// Set status to 'draft' so cleanup will delete it (approved facts are protected).
				if _, err := db.Exec("UPDATE facts SET status = 'draft'"); err != nil {
					panic(err)
				}
			},
			wantEntitiesDel:    0, // Alice has entity_sources; employee is EntityType (protected)
			wantFactsDel:       1,
			wantEntityTypeKept: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			log, err := logger.New(logger.Options{Level: "error"})
			if err != nil {
				t.Fatalf("create logger: %v", err)
			}

			cfg := config.Config{}
			cfg.ApplyDefaults()
			cfg.Embeddings.Mode = "api"
			cfg.Embeddings.API.BaseURL = "http://127.0.0.1:1/v1"
			cfg.Embeddings.API.ModelName = "test-model"
			cfg.Embeddings.API.VectorDim = 4
			dataDir := t.TempDir()
			cfg.Paths.DataDir = dataDir
			cfg.Paths.MigrationsDir = "../../../migrations"

			db, err := database.Open(filepath.Join(dataDir, "test.db"), cfg.VectorDim(),
				database.WithMigrationsDir(cfg.Paths.MigrationsDir))
			if err != nil {
				t.Fatalf("open database: %v", err)
			}
			defer db.Close() //nolint:errcheck

			if err := db.Migrate(context.Background()); err != nil {
				t.Fatalf("migrate: %v", err)
			}

			cacheStore, err := cache.NewStore(filepath.Join(dataDir, "test_cache.db"))
			if err != nil {
				t.Fatalf("open cache store: %v", err)
			}
			defer cacheStore.Close() //nolint:errcheck

			ingRunner, err := runner.NewRunner(cfg, db.DB(), log, runner.WithCacheStore(cacheStore))
			if err != nil {
				t.Fatalf("NewRunner: %v", err)
			}
			defer ingRunner.Close() //nolint:errcheck

			ctx := context.Background()

			// Record entity count before cleanup.
			var entityCountBefore int
			db.DB().QueryRow("SELECT COUNT(*) FROM entities").Scan(&entityCountBefore) //nolint:errcheck

			tt.seedSetup(db.DB(), ctx)

			stats, err := ingRunner.CleanupOrphanedData(ctx)
			if err != nil {
				t.Fatalf("CleanupOrphanedData: %v", err)
			}

			if stats.EntitiesDeleted != tt.wantEntitiesDel {
				t.Errorf("EntitiesDeleted = %d, want %d", stats.EntitiesDeleted, tt.wantEntitiesDel)
			}
			if stats.FactsDeleted != tt.wantFactsDel {
				t.Errorf("FactsDeleted = %d, want %d", stats.FactsDeleted, tt.wantFactsDel)
			}

			// Verify EntityType entities are never deleted.
			var entityTypeCount int
			db.DB().QueryRow("SELECT COUNT(*) FROM entities WHERE type = 'EntityType'").Scan(&entityTypeCount) //nolint:errcheck
			if tt.wantEntityTypeKept && entityTypeCount == 0 {
				t.Error("expected EntityType entity to be preserved")
			}
		})
	}
}

func TestPruneDeleted_FullCleanup(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	goneFile := filepath.Join(srcDir, "gone.md") // never created on disk

	ingRunner, db, _ := newTestRunner(t, []string{srcDir})

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)
	entityDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)
	factSourceDAO := dao.NewFactSourceDAO(db)
	chunkDAO := dao.NewChunkDAO(db)

	// Create a document that will be pruned (file doesn't exist).
	docID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: goneFile})
	if err != nil {
		t.Fatalf("create document: %v", err)
	}

	// Seed entities.
	subjID, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create Alice: %v", err)
	}
	objID, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create Acme: %v", err)
	}

	// Create a fact.
	factID, err := factDAO.CreateOrIgnore(ctx, dao.Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
	})
	if err != nil {
		t.Fatalf("create fact: %v", err)
	}

	// Create a fact source for the pruned document.
	q := "quote"
	if _, err := factSourceDAO.Create(ctx, dao.FactSource{FactID: factID, DocumentID: docID, Quote: &q}); err != nil {
		t.Fatalf("create fact source: %v", err)
	}

	// Create a chunk for the pruned document.
	chunkID, err := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "chunk text", SequenceNum: 0})
	if err != nil {
		t.Fatalf("create chunk: %v", err)
	}

	// Upsert a vector for the chunk.
	vec := make([]float32, 4)
	for i := range vec {
		vec[i] = float32(i+1) / 4
	}
	if err := chunkDAO.UpsertVector(ctx, chunkID, vec); err != nil && !gcIsNoSuchTable(err) {
		t.Fatalf("upsert vector: %v", err)
	}

	pruned, err := ingRunner.PruneDeleted(ctx)
	if err != nil {
		t.Fatalf("PruneDeleted: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	// Verify document is gone.
	doc, err := docDAO.GetByID(ctx, docID)
	if err != nil || doc != nil {
		t.Error("document should be deleted after PruneDeleted")
	}

	// Verify fact_source for the pruned document is gone.
	sources, err := factSourceDAO.GetByFactID(ctx, factID)
	if err != nil {
		t.Fatalf("GetByFactID: %v", err)
	}
	for _, s := range sources {
		if s.DocumentID == docID {
			t.Error("fact_source for pruned document should be removed")
		}
	}

	// Verify chunk is gone.
	chunk, err := chunkDAO.GetByID(ctx, chunkID)
	if err != nil || chunk != nil {
		t.Error("chunk should be deleted after PruneDeleted")
	}
}

func TestCleanupOrphanedData_VectorsAndDocuments(t *testing.T) {
	t.Parallel()

	srcDir := t.TempDir()
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	ingRunner, db, _ := newTestRunner(t, []string{srcDir})

	ctx := context.Background()
	docDAO := dao.NewDocumentDAO(db)
	chunkDAO := dao.NewChunkDAO(db)

	// Create an orphaned document (no chunks, no entity_sources, no fact_sources).
	orphanDocID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/orphan.md"})
	if err != nil {
		t.Fatalf("create orphan document: %v", err)
	}

	// Create a chunk and then delete it to leave an orphaned vector.
	chunkID, err := chunkDAO.Create(ctx, dao.Chunk{DocID: orphanDocID, ChunkText: "chunk text", SequenceNum: 0})
	if err != nil {
		t.Fatalf("create chunk: %v", err)
	}

	// Upsert a vector for the chunk.
	vec := make([]float32, 4)
	for i := range vec {
		vec[i] = float32(i+1) / 4
	}
	if err := chunkDAO.UpsertVector(ctx, chunkID, vec); err != nil && !gcIsNoSuchTable(err) {
		t.Fatalf("upsert vector: %v", err)
	}

	// Delete the chunk to leave an orphaned vector.
	if err := chunkDAO.Delete(ctx, chunkID); err != nil {
		t.Fatalf("delete chunk: %v", err)
	}

	stats, err := ingRunner.CleanupOrphanedData(ctx)
	if err != nil {
		t.Fatalf("CleanupOrphanedData: %v", err)
	}

	// Verify orphaned document was deleted.
	doc, err := docDAO.GetByID(ctx, orphanDocID)
	if err != nil || doc != nil {
		t.Error("orphaned document should be deleted")
	}

	// Check stats (vectors may or may not have been counted depending on sqlite-vec availability).
	t.Logf("CleanupOrphanedData stats: entities=%d, facts=%d, vectors=%d, documents=%d",
		stats.EntitiesDeleted, stats.FactsDeleted, stats.VectorsDeleted, stats.DocumentsDeleted)

	if stats.DocumentsDeleted < 1 {
		t.Errorf("DocumentsDeleted = %d, want >= 1", stats.DocumentsDeleted)
	}
}

// TestWithDomainRegistry verifies that WithDomainRegistry option injects the
// provided domain registry into the Runner, bypassing config-based discovery.
func TestWithDomainRegistry(t *testing.T) {
	t.Parallel()

	// 1. Build a fixture: temp dir with a minimal valid domain XML.
	ontologyDir := t.TempDir()
	domainDir := filepath.Join(ontologyDir, "domains")
	if err := os.MkdirAll(domainDir, 0o755); err != nil {
		t.Fatalf("mkdir domains: %v", err)
	}

	const testDomainXML = `<?xml version="1.0" encoding="UTF-8"?>
<domain name="test_domain" version="1.0">
    <entity id="TestEntity" name="Test Entity"/>
</domain>`
	xmlPath := filepath.Join(domainDir, "test.xml")
	if err := os.WriteFile(xmlPath, []byte(testDomainXML), 0o644); err != nil {
		t.Fatalf("write domain xml: %v", err)
	}

	// Discover registry from the temp directory.
	testRegistry, err := domain.Discovery(ontologyDir)
	if err != nil {
		t.Fatalf("domain discovery: %v", err)
	}

	// Verify the fixture is correct before proceeding.
	if !testRegistry.HasDomain("test_domain") {
		t.Fatal("fixture registry should contain test_domain")
	}
	cfg, err := testRegistry.Get("test_domain")
	if err != nil {
		t.Fatalf("get domain config: %v", err)
	}
	if cfg.Name != "test_domain" {
		t.Errorf("domain name = %q, want %q", cfg.Name, "test_domain")
	}

	// 2. Create a Runner with WithDomainRegistry option.
	srcDir := t.TempDir()
	log, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	cfg_ := config.Config{}
	cfg_.ApplyDefaults()
	cfg_.Embeddings.Mode = "api"
	cfg_.Embeddings.API.BaseURL = "http://127.0.0.1:1/v1"
	cfg_.Embeddings.API.ModelName = "test-model"
	cfg_.Embeddings.API.VectorDim = 4
	dataDir := t.TempDir()
	cfg_.Paths.DataDir = dataDir
	cfg_.Paths.MigrationsDir = "../../../migrations"
	// Point Paths.GlobalConfigPath at a non-existent directory so that without the
	// option, domain discovery would produce an empty registry.
	cfg_.Paths.GlobalConfigPath = filepath.Join(dataDir, "nonexistent")

	globalCfg := &config.GlobalConfig{
		Sources: []config.SourceConfig{{Path: srcDir, Type: "markdown"}},
	}

	db, err := database.Open(filepath.Join(dataDir, "test.db"), cfg_.VectorDim(),
		database.WithMigrationsDir(cfg_.Paths.MigrationsDir))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cacheStore, err := cache.NewStore(filepath.Join(dataDir, "test_cache.db"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	t.Cleanup(func() { _ = cacheStore.Close() })

	ingRunner, err := runner.NewRunner(cfg_, db.DB(), log,
		runner.WithCacheStore(cacheStore),
		runner.WithDomainRegistry(testRegistry),
		runner.WithGlobalConfig(globalCfg),
	)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = ingRunner.Close() })

	// 3. Assert the option took effect by verifying observable behavior:
	//    The runner's domain registry must resolve "test_domain" to a path,
	//    which would not happen if config-based discovery ran instead (since
	//    Paths.GlobalConfigPath points at a non-existent directory).
	paths := testRegistry.Paths([]string{"test_domain"})
	if len(paths) != 1 {
		t.Fatalf("registry.Paths(test_domain) returned %d paths, want 1", len(paths))
	}
	if paths[0] != xmlPath {
		t.Errorf("Paths[0] = %q, want %q", paths[0], xmlPath)
	}

	// Verify that a runner created WITHOUT WithGlobalConfig and with a
	// non-existent Paths.GlobalConfigPath returns an error (global config is mandatory).
	_, err = runner.NewRunner(cfg_, db.DB(), log,
		runner.WithCacheStore(cacheStore),
	)
	if err == nil {
		t.Fatal("expected NewRunner to fail when global config file is missing")
	}
}

// gcIsNoSuchTable checks if an error indicates a missing SQLite table.
func gcIsNoSuchTable(err error) bool {
	if err == nil {
		return false
	}
	return len(err.Error()) > 0 && containsStrInErr(err.Error(), "no such table")
}

func containsStrInErr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
		match := true
		for j := 0; j < len(substr); j++ {
			a, b := s[i+j], substr[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'A' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestRunnerWithGlobalConfigFile verifies that the Runner loads sources from
// a real global.xml file via Paths.GlobalConfigPath.
func TestRunnerWithGlobalConfigFile(t *testing.T) {
	t.Parallel()

	log, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Embeddings.Mode = "api"
	cfg.Embeddings.API.BaseURL = "http://127.0.0.1:1/v1"
	cfg.Embeddings.API.ModelName = "test-model"
	cfg.Embeddings.API.VectorDim = 4
	dataDir := t.TempDir()
	cfg.Paths.DataDir = dataDir
	cfg.Paths.MigrationsDir = "../../../migrations"

	// Point Paths.GlobalConfigPath at the testdata directory which contains global.xml.
	cfg.Paths.GlobalConfigPath = "testdata"

	db, err := database.Open(filepath.Join(dataDir, "test.db"), cfg.VectorDim(),
		database.WithMigrationsDir(cfg.Paths.MigrationsDir))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer db.Close() //nolint:errcheck

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cacheStore, err := cache.NewStore(filepath.Join(dataDir, "test_cache.db"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	defer cacheStore.Close() //nolint:errcheck

	ingRunner, err := runner.NewRunner(cfg, db.DB(), log,
		runner.WithCacheStore(cacheStore),
	)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	defer ingRunner.Close() //nolint:errcheck

	// The testdata/global.xml has 2 sources; verify they were loaded.
	srcA, ok := ingRunner.SourceForPath("./testdata/srcA/doc.md")
	if !ok {
		t.Fatal("expected to find source for ./testdata/srcA/doc.md")
	}
	if srcA.Type != "markdown" {
		t.Errorf("source type = %q, want %q", srcA.Type, "markdown")
	}

	srcB, ok := ingRunner.SourceForPath("./testdata/srcB/page.html")
	if !ok {
		t.Fatal("expected to find source for ./testdata/srcB/page.html")
	}
	if srcB.Type != "webpages" {
		t.Errorf("source type = %q, want %q", srcB.Type, "webpages")
	}

	// A path outside both sources should not match.
	_, ok = ingRunner.SourceForPath("./testdata/other/file.md")
	if ok {
		t.Error("expected no source match for ./testdata/other/file.md")
	}
}

func TestPruneDeleted_NilGlobalConfig(t *testing.T) {
	log, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create logger: %v", err)
	}

	cfg := config.Config{}
	cfg.ApplyDefaults()
	cfg.Embeddings.Mode = "api"
	cfg.Embeddings.API.BaseURL = "http://127.0.0.1:1/v1"
	cfg.Embeddings.API.ModelName = "test-model"
	cfg.Embeddings.API.VectorDim = 4
	cfg.Ingestion.NER.Disabled = true
	dataDir := t.TempDir()
	cfg.Paths.DataDir = dataDir
	cfg.Paths.MigrationsDir = "../../../migrations"

	db, err := database.Open(filepath.Join(dataDir, "test.db"), cfg.VectorDim(),
		database.WithMigrationsDir(cfg.Paths.MigrationsDir))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cacheStore, err := cache.NewStore(filepath.Join(dataDir, "test_cache.db"))
	if err != nil {
		t.Fatalf("open cache store: %v", err)
	}
	t.Cleanup(func() { _ = cacheStore.Close() })

	// Create runner WITHOUT global config — globalCfg will be nil.
	ingRunner, err := runner.NewRunner(cfg, db.DB(), log,
		runner.WithCacheStore(cacheStore),
	)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}
	t.Cleanup(func() { _ = ingRunner.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// PruneDeleted calls belongsToSource internally; must not panic with nil globalCfg.
	count, err := ingRunner.PruneDeleted(ctx)
	if err != nil {
		t.Fatalf("PruneDeleted: %v", err)
	}
	if count != 0 {
		t.Error("expected 0 pruned documents with empty database")
	}
}
