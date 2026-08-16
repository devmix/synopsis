package entities

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/ingestion/ner"
)

type resolverFixture struct {
	db       *sql.DB
	cleanup  func()
	resolver *Resolver
	docID    int
}

func newResolverFixture(t *testing.T) *resolverFixture {
	t.Helper()

	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}

	docID, err := dao.NewDocumentDAO(db).Create(context.Background(), dao.Document{
		SourceType:   "markdown",
		OriginalPath: "/test/doc.md",
	})
	if err != nil {
		cleanup()
		t.Fatalf("create document: %v", err)
	}

	return &resolverFixture{
		db:       db,
		cleanup:  cleanup,
		resolver: NewResolver(0.8),
		docID:    docID,
	}
}

func (f *resolverFixture) entityCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM entities").Scan(&count); err != nil {
		t.Fatalf("count entities: %v", err)
	}
	return count
}

func (f *resolverFixture) sourceDocIDs(t *testing.T, entityID int) []int {
	t.Helper()
	ids, err := dao.NewEntitySourceDAO(f.db).GetDocumentsByEntityID(context.Background(), entityID)
	if err != nil {
		t.Fatalf("get source docs: %v", err)
	}
	return ids
}

func TestAddEntitiesBatchDedup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		ents         []ner.Entity
		wantResolved int
		wantName     string
	}{
		{
			name: "ascii synonyms merged",
			ents: []ner.Entity{
				{Name: "Apple Inc.", Type: "ORGANIZATION"},
				{Name: "Apple", Type: "ORGANIZATION"},
			},
			wantResolved: 1,
			wantName:     "Apple Inc.",
		},
		{
			name: "cyrillic initials merged",
			ents: []ner.Entity{
				{Name: "Стив Джобс", Type: "PERSON"},
				{Name: "С. Джобс", Type: "PERSON"},
			},
			wantResolved: 1,
			wantName:     "Стив Джобс",
		},
		{
			name: "different types not merged",
			ents: []ner.Entity{
				{Name: "Apple", Type: "ORGANIZATION"},
				{Name: "Стив Джобс", Type: "PERSON"},
			},
			wantResolved: 2,
		},
		{
			name: "different names not merged",
			ents: []ner.Entity{
				{Name: "Иван Иванов", Type: "PERSON"},
				{Name: "Петр Петров", Type: "PERSON"},
			},
			wantResolved: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			f := newResolverFixture(t)
			defer f.cleanup()

			resolved, err := f.resolver.AddEntities(context.Background(), f.db, f.docID, tt.ents)
			if err != nil {
				t.Fatalf("AddEntities() error = %v", err)
			}
			if len(resolved) != tt.wantResolved {
				t.Fatalf("resolved %d entities, want %d", len(resolved), tt.wantResolved)
			}
			if got := f.entityCount(t); got != tt.wantResolved {
				t.Errorf("entity rows = %d, want %d", got, tt.wantResolved)
			}
			for _, ent := range resolved {
				if ent.ID <= 0 {
					t.Errorf("resolved entity %q has invalid Predicate %d", ent.Name, ent.ID)
				}
				if ent.ID != 0 && len(f.sourceDocIDs(t, ent.ID)) != 1 {
					t.Errorf("entity %q (%d) not linked to source document", ent.Name, ent.ID)
				}
			}
			if tt.wantName != "" && resolved[0].Name != tt.wantName {
				t.Errorf("canonical name = %q, want %q", resolved[0].Name, tt.wantName)
			}
		})
	}
}

func TestAddEntitiesIncremental(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	first, err := f.resolver.AddEntities(ctx, f.db, f.docID, []ner.Entity{{Name: "Apple Inc.", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("first AddEntities() error = %v", err)
	}

	// A shorter synonym in a second document must reuse the existing entity.
	second, err := f.resolver.AddEntities(ctx, f.db, f.docID, []ner.Entity{{Name: "Apple", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("second AddEntities() error = %v", err)
	}

	if got := f.entityCount(t); got != 1 {
		t.Errorf("entity rows = %d, want 1", got)
	}
	if second[0].ID != first[0].ID {
		t.Errorf("resolved Predicate = %d, want %d (reused)", second[0].ID, first[0].ID)
	}
	if second[0].Name != "Apple Inc." {
		t.Errorf("canonical name = %q, want %q", second[0].Name, "Apple Inc.")
	}
}

func TestAddEntitiesCaseInsensitiveExactMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	existing, err := dao.NewEntityDAO(f.db).Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Apple"})
	if err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	resolved, err := f.resolver.AddEntities(ctx, f.db, f.docID, []ner.Entity{{Name: "apple", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("AddEntities() error = %v", err)
	}

	if got := f.entityCount(t); got != 1 {
		t.Errorf("entity rows = %d, want 1", got)
	}
	if resolved[0].ID != existing {
		t.Errorf("resolved Predicate = %d, want %d", resolved[0].ID, existing)
	}
}

func TestAddEntitiesCanonicalPromotion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	existingID, err := dao.NewEntityDAO(f.db).Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Apple"})
	if err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	resolved, err := f.resolver.AddEntities(ctx, f.db, f.docID, []ner.Entity{{Name: "Apple Inc.", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("AddEntities() error = %v", err)
	}
	if resolved[0].ID != existingID {
		t.Errorf("resolved Predicate = %d, want %d", resolved[0].ID, existingID)
	}

	ent, err := dao.NewEntityDAO(f.db).GetByID(ctx, existingID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if ent == nil || ent.Name != "Apple Inc." {
		t.Errorf("promoted name = %v, want %q", ent, "Apple Inc.")
	}

	// Old name must still resolve to the same canonical entity.
	again, err := f.resolver.AddEntities(ctx, f.db, f.docID, []ner.Entity{{Name: "Apple", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("re-add old name: %v", err)
	}
	if again[0].ID != existingID {
		t.Errorf("old name resolved Predicate = %d, want %d", again[0].ID, existingID)
	}
	if got := f.entityCount(t); got != 1 {
		t.Errorf("entity rows = %d, want 1", got)
	}
}

func TestAddEntitiesProvenanceAcrossDocuments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	doc2ID, err := dao.NewDocumentDAO(f.db).Create(ctx, dao.Document{
		SourceType:   "markdown",
		OriginalPath: "/test/doc2.md",
	})
	if err != nil {
		t.Fatalf("create document 2: %v", err)
	}

	first, err := f.resolver.AddEntities(ctx, f.db, f.docID, []ner.Entity{{Name: "Apple", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("first AddEntities() error = %v", err)
	}
	second, err := f.resolver.AddEntities(ctx, f.db, doc2ID, []ner.Entity{{Name: "Apple", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("second AddEntities() error = %v", err)
	}

	if first[0].ID != second[0].ID {
		t.Fatalf("IDs differ across documents: %d vs %d", first[0].ID, second[0].ID)
	}

	got := f.sourceDocIDs(t, first[0].ID)
	if len(got) != 2 {
		t.Fatalf("entity linked to %d documents, want 2: %v", len(got), got)
	}
}

func TestAddEntitiesEmpty(t *testing.T) {
	t.Parallel()

	f := newResolverFixture(t)
	defer f.cleanup()

	resolved, err := f.resolver.AddEntities(context.Background(), f.db, f.docID, nil)
	if err != nil {
		t.Fatalf("AddEntities() error = %v", err)
	}
	if resolved != nil {
		t.Errorf("resolved = %v, want nil", resolved)
	}
}

func TestAddEntitiesInvalidDocID(t *testing.T) {
	t.Parallel()

	f := newResolverFixture(t)
	defer f.cleanup()

	if _, err := f.resolver.AddEntities(context.Background(), f.db, 0, []ner.Entity{{Name: "Apple", Type: "ORGANIZATION"}}); err == nil {
		t.Error("expected error for invalid document Predicate")
	}
}

// TestAddEntitiesWithinTransaction reproduces the ingestion flow: a write
// transaction holds the SQLite write lock while the resolver persists
// entities through the same transaction. Writing via the connection pool
// instead would fail with "database is locked".
func TestAddEntitiesWithinTransaction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Acquire the write lock through the transaction, as processDocument does.
	if _, err := dao.NewDocumentDAO(tx).Create(ctx, dao.Document{
		SourceType:   "markdown",
		OriginalPath: "/test/txdoc.md",
	}); err != nil {
		t.Fatalf("write through tx: %v", err)
	}

	txResolver := NewResolver(0.8)
	resolved, err := txResolver.AddEntities(ctx, tx, f.docID, []ner.Entity{{Name: "Junior", Type: "PERSON"}})
	if err != nil {
		t.Fatalf("AddEntities() within transaction error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved %d entities, want 1", len(resolved))
	}

	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}

	if got := f.entityCount(t); got != 1 {
		t.Errorf("entity rows = %d, want 1", got)
	}
}

// TestIndexSurvivesHandleChange verifies that switching the DBTX handle
// between callers (connection pool vs transaction) keeps the in-memory
// deduplication index intact.
func TestIndexSurvivesHandleChange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	first, err := f.resolver.AddEntities(ctx, f.db, f.docID, []ner.Entity{{Name: "Apple", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("first AddEntities() error = %v", err)
	}

	tx, err := f.db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck

	second, err := f.resolver.AddEntities(ctx, tx, f.docID, []ner.Entity{{Name: "Apple Inc.", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("second AddEntities() error = %v", err)
	}

	if second[0].ID != first[0].ID {
		t.Errorf("resolved Predicate = %d, want %d (index lost across handle change)", second[0].ID, first[0].ID)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit tx: %v", err)
	}
}

func TestLookup_ExactMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	// Seed an entity directly.
	entityDAO := dao.NewEntityDAO(f.db)
	id, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Apple Inc."})
	if err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	ids, err := f.resolver.Lookup(ctx, f.db, []ner.Entity{{Name: "Apple Inc.", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != id {
		t.Errorf("ids = %v, want [%d]", ids, id)
	}
}

func TestLookup_SimilarMatch(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	entityDAO := dao.NewEntityDAO(f.db)
	id, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Apple Inc."})
	if err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	// "Apple" should resolve to "Apple Inc." via similarity >= 0.8.
	ids, err := f.resolver.Lookup(ctx, f.db, []ner.Entity{{Name: "Apple", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != id {
		t.Errorf("ids = %v, want [%d]", ids, id)
	}
}

func TestLookup_NotFound(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	entityDAO := dao.NewEntityDAO(f.db)
	_, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Apple Inc."})
	if err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	// Completely different name should return 0.
	ids, err := f.resolver.Lookup(ctx, f.db, []ner.Entity{{Name: "Google LLC", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != 0 {
		t.Errorf("ids = %v, want [0]", ids)
	}
}

func TestLookup_NoCreation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	beforeCount := f.entityCount(t)

	// Lookup for a non-existent entity must not create anything.
	ids, err := f.resolver.Lookup(ctx, f.db, []ner.Entity{{Name: "NonExistent Corp", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != 0 {
		t.Errorf("ids = %v, want [0]", ids)
	}

	afterCount := f.entityCount(t)
	if afterCount != beforeCount {
		t.Errorf("entity count changed from %d to %d (Lookup must not create)", beforeCount, afterCount)
	}
}

func TestLookup_InputOrderAlignment(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	entityDAO := dao.NewEntityDAO(f.db)
	id1, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("seed Alice: %v", err)
	}
	id2, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("seed Acme: %v", err)
	}

	inputs := []ner.Entity{
		{Name: "Alice", Type: "PERSON"},
		{Name: "Unknown Person", Type: "PERSON"},
		{Name: "Acme Corp", Type: "ORGANIZATION"},
	}
	ids, err := f.resolver.Lookup(ctx, f.db, inputs)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}

	want := []int{id1, 0, id2}
	for i, got := range ids {
		if got != want[i] {
			t.Errorf("ids[%d] = %d, want %d", i, got, want[i])
		}
	}
}

func TestLookup_HydrationFromDB(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	// Seed entity directly in DB without going through resolver.
	entityDAO := dao.NewEntityDAO(f.db)
	id, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Bob"})
	if err != nil {
		t.Fatalf("seed Bob: %v", err)
	}

	// Resolver is not hydrated yet; Lookup must hydrate from DB.
	ids, err := f.resolver.Lookup(ctx, f.db, []ner.Entity{{Name: "Bob", Type: "PERSON"}})
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != id {
		t.Errorf("ids = %v, want [%d]", ids, id)
	}
}

func TestLookup_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	ids, err := f.resolver.Lookup(ctx, f.db, nil)
	if err != nil {
		t.Fatalf("Lookup(nil) error = %v", err)
	}
	if ids != nil {
		t.Errorf("expected nil for empty input, got %v", ids)
	}
}

func TestLookupOrCreate_ExistingEntityReturnsExistingID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	entityDAO := dao.NewEntityDAO(f.db)
	id, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Apple Inc."})
	if err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	ids, err := f.resolver.LookupOrCreate(ctx, f.db, f.docID, []ner.Entity{{Name: "Apple Inc.", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("LookupOrCreate() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != id {
		t.Errorf("ids = %v, want [%d]", ids, id)
	}

	// Entity count must not change.
	if got := f.entityCount(t); got != 1 {
		t.Errorf("entity rows = %d, want 1", got)
	}
}

func TestLookupOrCreate_MissingEntityCreatedAndReturnsNewID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	beforeCount := f.entityCount(t)

	ids, err := f.resolver.LookupOrCreate(ctx, f.db, f.docID, []ner.Entity{{Name: "NewCorp", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("LookupOrCreate() error = %v", err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		t.Errorf("ids = %v, want [positive_id]", ids)
	}

	afterCount := f.entityCount(t)
	if afterCount != beforeCount+1 {
		t.Errorf("entity count changed from %d to %d, want %d", beforeCount, afterCount, beforeCount+1)
	}

	// Verify entity was created with correct name and type.
	ent, err := dao.NewEntityDAO(f.db).GetByID(ctx, ids[0])
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if ent == nil || ent.Name != "NewCorp" || ent.Type != "ORGANIZATION" {
		t.Errorf("entity = %+v, want Name=NewCorp Type=ORGANIZATION", ent)
	}

	// Verify entity is linked to the document via entity_sources.
	sources := f.sourceDocIDs(t, ids[0])
	if len(sources) != 1 || sources[0] != f.docID {
		t.Errorf("entity_sources = %v, want [%d]", sources, f.docID)
	}
}

func TestLookupOrCreate_DedupRepeatedCallsReturnSameID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	firstIDs, err := f.resolver.LookupOrCreate(ctx, f.db, f.docID, []ner.Entity{{Name: "DedupCorp", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("first LookupOrCreate() error = %v", err)
	}

	secondIDs, err := f.resolver.LookupOrCreate(ctx, f.db, f.docID, []ner.Entity{{Name: "DedupCorp", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("second LookupOrCreate() error = %v", err)
	}

	if firstIDs[0] != secondIDs[0] {
		t.Errorf("dedup failed: first Predicate = %d, second Predicate = %d", firstIDs[0], secondIDs[0])
	}

	// Only one entity row should exist.
	if got := f.entityCount(t); got != 1 {
		t.Errorf("entity rows = %d, want 1 (dedup)", got)
	}
}

func TestLookupOrCreate_EmptyInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	ids, err := f.resolver.LookupOrCreate(ctx, f.db, f.docID, nil)
	if err != nil {
		t.Fatalf("LookupOrCreate(nil) error = %v", err)
	}
	if ids != nil {
		t.Errorf("expected nil for empty input, got %v", ids)
	}
}

func TestLookupOrCreate_InvalidDocID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	if _, err := f.resolver.LookupOrCreate(ctx, f.db, 0, []ner.Entity{{Name: "Corp", Type: "ORGANIZATION"}}); err == nil {
		t.Error("expected error for invalid document Predicate")
	}
	if _, err := f.resolver.LookupOrCreate(ctx, f.db, -1, []ner.Entity{{Name: "Corp", Type: "ORGANIZATION"}}); err == nil {
		t.Error("expected error for negative document Predicate")
	}
}

func TestLookupOrCreate_MixedExistingAndNew(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	entityDAO := dao.NewEntityDAO(f.db)
	existingID, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("seed Alice: %v", err)
	}

	inputs := []ner.Entity{
		{Name: "Alice", Type: "PERSON"},
		{Name: "Bob", Type: "PERSON"},
	}
	ids, err := f.resolver.LookupOrCreate(ctx, f.db, f.docID, inputs)
	if err != nil {
		t.Fatalf("LookupOrCreate() error = %v", err)
	}

	if len(ids) != 2 {
		t.Fatalf("ids length = %d, want 2", len(ids))
	}
	if ids[0] != existingID {
		t.Errorf("ids[0] = %d, want %d (existing Alice)", ids[0], existingID)
	}
	if ids[1] <= 0 || ids[1] == existingID {
		t.Errorf("ids[1] = %d, want positive Predicate != %d", ids[1], existingID)
	}

	// Bob must be linked to the document.
	bobSources := f.sourceDocIDs(t, ids[1])
	if len(bobSources) != 1 || bobSources[0] != f.docID {
		t.Errorf("Bob entity_sources = %v, want [%d]", bobSources, f.docID)
	}

	// Two entities total: Alice (seeded) + Bob (created).
	if got := f.entityCount(t); got != 2 {
		t.Errorf("entity rows = %d, want 2", got)
	}
}

func TestLookupOrCreate_LaterAddEntitiesMergesIntoSynthetic(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	// LookupOrCreate creates a synthetic entity.
	syntheticIDs, err := f.resolver.LookupOrCreate(ctx, f.db, f.docID, []ner.Entity{{Name: "SynthCorp", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("LookupOrCreate() error = %v", err)
	}

	// AddEntities with the same name must merge into the synthetic entity.
	resolved, err := f.resolver.AddEntities(ctx, f.db, f.docID, []ner.Entity{{Name: "SynthCorp", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("AddEntities() error = %v", err)
	}

	if resolved[0].ID != syntheticIDs[0] {
		t.Errorf("AddEntities merged into different Predicate: %d vs %d", resolved[0].ID, syntheticIDs[0])
	}

	// Only one entity row.
	if got := f.entityCount(t); got != 1 {
		t.Errorf("entity rows = %d, want 1 (no duplicate)", got)
	}
}

// TestAddEntities_DomainIsolation verifies that identical (name, type) pairs in
// different domains resolve to distinct entities.
func TestAddEntities_DomainIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	ents := []ner.Entity{
		{Name: "Архитектор", Type: "ROLE", Domain: "construction"},
		{Name: "Архитектор", Type: "ROLE", Domain: "it"},
	}
	resolved, err := f.resolver.AddEntities(ctx, f.db, f.docID, ents)
	if err != nil {
		t.Fatalf("AddEntities() error = %v", err)
	}
	if len(resolved) != 2 {
		t.Fatalf("resolved %d entities, want 2 (domain isolation)", len(resolved))
	}
	if resolved[0].ID == resolved[1].ID {
		t.Errorf("entities in different domains share Predicate %d", resolved[0].ID)
	}
	if got := f.entityCount(t); got != 2 {
		t.Errorf("entity rows = %d, want 2", got)
	}
}

// TestAddEntities_SameDomainDedup verifies that identical (name, type) pairs in
// the same domain resolve to a single entity.
func TestAddEntities_SameDomainDedup(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	ents := []ner.Entity{
		{Name: "Архитектор", Type: "ROLE", Domain: "construction"},
		{Name: "Архитектор", Type: "ROLE", Domain: "construction"},
	}
	resolved, err := f.resolver.AddEntities(ctx, f.db, f.docID, ents)
	if err != nil {
		t.Fatalf("AddEntities() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved %d entities, want 1 (same domain dedup)", len(resolved))
	}
	if got := f.entityCount(t); got != 1 {
		t.Errorf("entity rows = %d, want 1", got)
	}
}

// TestAddEntities_DomainNormalization verifies that domain strings differing
// only in case/whitespace resolve to the same entity.
func TestAddEntities_DomainNormalization(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	ents := []ner.Entity{
		{Name: "Alice", Type: "PERSON", Domain: "HR"},
		{Name: "Alice", Type: "PERSON", Domain: " hr "},
	}
	resolved, err := f.resolver.AddEntities(ctx, f.db, f.docID, ents)
	if err != nil {
		t.Fatalf("AddEntities() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved %d entities, want 1 (normalized domain dedup)", len(resolved))
	}
	if got := f.entityCount(t); got != 1 {
		t.Errorf("entity rows = %d, want 1", got)
	}
	ent, err := dao.NewEntityDAO(f.db).GetByID(ctx, resolved[0].ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if ent == nil || ent.Domain != "hr" {
		t.Errorf("entity domain = %q, want %q", ent.Domain, "hr")
	}
}

func TestAddEntities_PersistsConfidenceAndMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	desc := "CEO of Acme Corp"
	resolved, err := f.resolver.AddEntities(ctx, f.db, f.docID, []ner.Entity{
		{
			Name:        "Alice Smith",
			Type:        "PERSON",
			Description: desc,
			Confidence:  0.92,
			Metadata:    map[string]interface{}{"source": "llm", "model": "gpt-4"},
		},
	})
	if err != nil {
		t.Fatalf("AddEntities() error = %v", err)
	}
	if len(resolved) != 1 {
		t.Fatalf("resolved %d entities, want 1", len(resolved))
	}

	// Verify confidence is returned in resolved entity.
	if resolved[0].Confidence != 0.92 {
		t.Errorf("resolved Confidence = %f, want 0.92", resolved[0].Confidence)
	}

	// Verify confidence and metadata are persisted in DB.
	entityDAO := dao.NewEntityDAO(f.db)
	ent, err := entityDAO.GetByID(ctx, resolved[0].ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if ent == nil {
		t.Fatal("entity not found in DB")
	}
	if ent.Confidence != 0.92 {
		t.Errorf("DB Confidence = %f, want 0.92", ent.Confidence)
	}
	if ent.MetadataJSON == nil {
		t.Fatal("DB MetadataJSON is nil, expected non-nil")
	}
}

func TestLookupOrCreateWithStats_ExistingEntityReturnsZeroCreated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	entityDAO := dao.NewEntityDAO(f.db)
	id, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Apple Inc."})
	if err != nil {
		t.Fatalf("seed entity: %v", err)
	}

	ids, created, err := f.resolver.LookupOrCreateWithStats(ctx, f.db, f.docID, []ner.Entity{{Name: "Apple Inc.", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("LookupOrCreateWithStats() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != id {
		t.Errorf("ids = %v, want [%d]", ids, id)
	}
	if created != 0 {
		t.Errorf("created = %d, want 0 (entity already exists)", created)
	}

	// Entity count must not change.
	if got := f.entityCount(t); got != 1 {
		t.Errorf("entity rows = %d, want 1", got)
	}
}

func TestLookupOrCreateWithStats_NewEntityReturnsOneCreated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	beforeCount := f.entityCount(t)

	ids, created, err := f.resolver.LookupOrCreateWithStats(ctx, f.db, f.docID, []ner.Entity{{Name: "NewCorp", Type: "ORGANIZATION"}})
	if err != nil {
		t.Fatalf("LookupOrCreateWithStats() error = %v", err)
	}
	if len(ids) != 1 || ids[0] <= 0 {
		t.Errorf("ids = %v, want [positive_id]", ids)
	}
	if created != 1 {
		t.Errorf("created = %d, want 1", created)
	}

	afterCount := f.entityCount(t)
	if afterCount != beforeCount+1 {
		t.Errorf("entity count changed from %d to %d, want %d", beforeCount, afterCount, beforeCount+1)
	}
}

func TestLookupOrCreateWithStats_MixedExistingAndNew(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	entityDAO := dao.NewEntityDAO(f.db)
	existingID, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("seed Alice: %v", err)
	}

	inputs := []ner.Entity{
		{Name: "Alice", Type: "PERSON"},
		{Name: "Bob", Type: "PERSON"},
	}
	ids, created, err := f.resolver.LookupOrCreateWithStats(ctx, f.db, f.docID, inputs)
	if err != nil {
		t.Fatalf("LookupOrCreateWithStats() error = %v", err)
	}

	if len(ids) != 2 {
		t.Fatalf("ids length = %d, want 2", len(ids))
	}
	if ids[0] != existingID {
		t.Errorf("ids[0] = %d, want %d (existing Alice)", ids[0], existingID)
	}
	if created != 1 {
		t.Errorf("created = %d, want 1 (only Bob is new)", created)
	}

	// Two entities total: Alice (seeded) + Bob (created).
	if got := f.entityCount(t); got != 2 {
		t.Errorf("entity rows = %d, want 2", got)
	}
}

func TestLookupOrCreateWithStats_DuplicateIngestionZeroCreated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	entities := []ner.Entity{
		{Name: "ProjectAlpha", Type: "PROJECT"},
		{Name: "TeamBeta", Type: "TEAM"},
	}

	// First ingestion: both entities are new.
	_, created1, err := f.resolver.LookupOrCreateWithStats(ctx, f.db, f.docID, entities)
	if err != nil {
		t.Fatalf("first LookupOrCreateWithStats() error = %v", err)
	}
	if created1 != 2 {
		t.Errorf("first ingestion: created = %d, want 2", created1)
	}

	// Second ingestion of same entities: all already exist.
	_, created2, err := f.resolver.LookupOrCreateWithStats(ctx, f.db, f.docID, entities)
	if err != nil {
		t.Fatalf("second LookupOrCreateWithStats() error = %v", err)
	}
	if created2 != 0 {
		t.Errorf("second ingestion: created = %d, want 0 (duplicate ingestion)", created2)
	}

	// Still only 2 entities.
	if got := f.entityCount(t); got != 2 {
		t.Errorf("entity rows = %d, want 2", got)
	}
}

func TestLookupOrCreateWithStats_EmptyInput(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	ids, created, err := f.resolver.LookupOrCreateWithStats(ctx, f.db, f.docID, nil)
	if err != nil {
		t.Fatalf("LookupOrCreateWithStats(nil) error = %v", err)
	}
	if ids != nil {
		t.Errorf("expected nil for empty input, got %v", ids)
	}
	if created != 0 {
		t.Errorf("created = %d, want 0", created)
	}
}

func TestLookupOrCreateWithStats_InvalidDocID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	f := newResolverFixture(t)
	defer f.cleanup()

	if _, _, err := f.resolver.LookupOrCreateWithStats(ctx, f.db, 0, []ner.Entity{{Name: "Corp", Type: "ORGANIZATION"}}); err == nil {
		t.Error("expected error for invalid document Predicate")
	}
	if _, _, err := f.resolver.LookupOrCreateWithStats(ctx, f.db, -1, []ner.Entity{{Name: "Corp", Type: "ORGANIZATION"}}); err == nil {
		t.Error("expected error for negative document Predicate")
	}
}

func TestScopeEntityMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		entityName string
		raw        map[string]interface{}
		wantKeys   []string // keys expected in output
		notWant    []string // keys that should NOT be in output
	}{
		{
			name:       "empty_metadata",
			entityName: "Alice",
			raw:        nil,
			wantKeys:   nil,
		},
		{
			name:       "entity_scoped_fields_preserved",
			entityName: "Alice",
			raw: map[string]interface{}{
				"provider":   "internal",
				"confidence": 0.95,
				"title":      "Software Engineer",
			},
			wantKeys: []string{"provider", "confidence"},
		},
		{
			name:       "document_level_fields_removed_but_provenance_kept",
			entityName: "Alice",
			raw: map[string]interface{}{
				"url":         "https://example.com/doc.md",
				"image_paths": []string{"img.png"},
				"page_links":  []string{"#section-1"},
				"categories":  []string{"engineering"},
				"source_file": "/path/to/file.md",
				"provider":    "internal",
			},
			wantKeys: []string{"provider", "source_file"},
			notWant:  []string{"url", "image_paths", "page_links", "categories"},
		},
		{
			name:       "title_set_to_entity_name",
			entityName: "Alice Smith",
			raw: map[string]interface{}{
				"title":    "Software Engineer",
				"provider": "internal",
			},
			wantKeys: []string{"title", "provider"},
		},
		{
			name:       "case_insensitive_doc_level_filtering",
			entityName: "Bob",
			raw: map[string]interface{}{
				"URL":         "https://example.com",
				"source_file": "/path/to/file.md",
				"provider":    "internal",
			},
			wantKeys: []string{"source_file", "provider"},
			notWant:  []string{"URL"},
		},
		{
			name:       "all_fields_are_doc_level_except_provenance",
			entityName: "Charlie",
			raw: map[string]interface{}{
				"url":         "https://example.com",
				"image_paths": []string{"img.png"},
				"source_file": "/path/to/file.md",
			},
			wantKeys: []string{"source_file"}, // source_file is provenance, kept
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scoped := scopeEntityMetadata(tt.entityName, tt.raw)

			if len(tt.wantKeys) == 0 && scoped != nil && len(scoped) > 0 {
				t.Errorf("expected empty/nil result, got %v", scoped)
				return
			}

			for _, k := range tt.wantKeys {
				if _, ok := scoped[k]; !ok {
					t.Errorf("missing expected key %q in scoped metadata: %v", k, scoped)
				}
			}

			// Check title is set to entity name when present and is a string.
			if origTitle, hasTitle := tt.raw["title"]; hasTitle {
				if _, ok := origTitle.(string); ok {
					if titleVal, ok := scoped["title"].(string); !ok || titleVal != tt.entityName {
						t.Errorf("scoped[\"title\"] = %v, want %q", scoped["title"], tt.entityName)
					}
				}
			}

			for _, k := range tt.notWant {
				if _, ok := scoped[k]; ok {
					t.Errorf("should not contain document-level key %q in scoped metadata: %v", k, scoped)
				}
			}
		})
	}
}
