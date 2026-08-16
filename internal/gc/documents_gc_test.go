package gc

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
)

func newGCFixture(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	db, cleanup, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		cleanup()
		t.Fatalf("create test db: %v", err)
	}
	return db, cleanup
}

func TestFullClearDocByID_RemovesAllData(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newGCFixture(t)
	defer cleanup()

	docDAO := dao.NewDocumentDAO(db)
	entityDAO := dao.NewEntityDAO(db)
	chunkDAO := dao.NewChunkDAO(db)
	factDAO := dao.NewFactDAO(db)
	factSourceDAO := dao.NewFactSourceDAO(db)

	// Create two documents.
	doc1ID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc1.md"})
	if err != nil {
		t.Fatalf("create doc1: %v", err)
	}
	doc2ID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc2.md"})
	if err != nil {
		t.Fatalf("create doc2: %v", err)
	}

	// Create entities.
	entity1ID, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create entity Alice: %v", err)
	}
	entity2ID, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create entity Acme: %v", err)
	}

	// Link entity1 to doc1 only.
	entitySourceDAO := dao.NewEntitySourceDAO(db)
	if _, err := entitySourceDAO.Create(ctx, dao.EntitySource{EntityID: entity1ID, DocumentID: doc1ID}); err != nil {
		t.Fatalf("create entity source for entity1-doc1: %v", err)
	}

	// Link entity2 to doc2 so it survives GC (it has no link to doc1).
	if _, err := entitySourceDAO.Create(ctx, dao.EntitySource{EntityID: entity2ID, DocumentID: doc2ID}); err != nil {
		t.Fatalf("create entity source for entity2-doc2: %v", err)
	}

	// Create a fact linking the two entities.
	factID, err := factDAO.CreateOrIgnore(ctx, dao.Fact{
		SubjectEntityID: entity1ID,
		Predicate:       "works_at",
		ObjectEntityID:  entity2ID,
	})
	if err != nil {
		t.Fatalf("create fact: %v", err)
	}

	// Create fact sources for the same fact in both documents.
	q1 := "Alice works at Acme"
	q2 := "Alice is employed by Acme Corp"
	if _, err := factSourceDAO.Create(ctx, dao.FactSource{FactID: factID, DocumentID: doc1ID, Quote: &q1}); err != nil {
		t.Fatalf("create fact source for doc1: %v", err)
	}
	if _, err := factSourceDAO.Create(ctx, dao.FactSource{FactID: factID, DocumentID: doc2ID, Quote: &q2}); err != nil {
		t.Fatalf("create fact source for doc2: %v", err)
	}

	// Create chunks for both documents.
	chunk1Text := "chunk text for doc1"
	if _, err := chunkDAO.Create(ctx, dao.Chunk{DocID: doc1ID, ChunkText: chunk1Text, SequenceNum: 0}); err != nil {
		t.Fatalf("create chunk for doc1: %v", err)
	}
	chunk2Text := "chunk text for doc2"
	if _, err := chunkDAO.Create(ctx, dao.Chunk{DocID: doc2ID, ChunkText: chunk2Text, SequenceNum: 0}); err != nil {
		t.Fatalf("create chunk for doc2: %v", err)
	}

	gc := NewDocumentGC(db)

	// Clear doc1.
	if err := gc.FullClearDocByID(ctx, doc1ID); err != nil {
		t.Fatalf("FullClearDocByID(doc1): %v", err)
	}

	// Verify entity_sources for doc1 are gone.
	docIDs, err := entitySourceDAO.GetDocumentsByEntityID(ctx, entity1ID)
	if err != nil {
		t.Fatalf("GetDocumentsByEntityID: %v", err)
	}
	for _, did := range docIDs {
		if did == doc1ID {
			t.Error("entity source for doc1 should be removed")
		}
	}

	// Verify fact_sources for doc1 are gone.
	sources, err := factSourceDAO.GetByFactID(ctx, factID)
	if err != nil {
		t.Fatalf("GetByFactID: %v", err)
	}
	for _, s := range sources {
		if s.DocumentID == doc1ID {
			t.Error("fact source for doc1 should be removed")
		}
	}

	// Verify the fact still exists (it has a source in doc2).
	fact, err := factDAO.GetByID(ctx, factID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fact == nil {
		t.Fatal("fact should still exist (has source in doc2)")
	}

	// Verify chunks for doc1 are gone.
	chunks, err := chunkDAO.ListByDocID(ctx, doc1ID)
	if err != nil {
		t.Fatalf("ListByDocID(doc1): %v", err)
	}
	if len(chunks) > 0 {
		t.Errorf("chunks for doc1 = %d, want 0", len(chunks))
	}

	// Verify chunks for doc2 are untouched.
	chunks2, err := chunkDAO.ListByDocID(ctx, doc2ID)
	if err != nil {
		t.Fatalf("ListByDocID(doc2): %v", err)
	}
	if len(chunks2) != 1 {
		t.Errorf("chunks for doc2 = %d, want 1", len(chunks2))
	}

	// Verify entity2 still exists (linked to doc2).
	ent2, err := entityDAO.GetByID(ctx, entity2ID)
	if err != nil {
		t.Fatalf("GetByID(entity2): %v", err)
	}
	if ent2 == nil {
		t.Fatal("entity2 should still exist")
	}

	// Verify entity1 SURVIVES because it is referenced by a fact (facts→entities FK has no CASCADE).
	// The scoped DeleteOrphanedByIDs excludes entities referenced by facts.
	ent1, err := entityDAO.GetByID(ctx, entity1ID)
	if err != nil {
		t.Fatalf("GetByID(entity1): %v", err)
	}
	if ent1 == nil {
		t.Error("entity1 should survive (referenced by a fact with sources in another document)")
	}
}

func TestFullClearDocByID_WeightDecrease(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newGCFixture(t)
	defer cleanup()

	docDAO := dao.NewDocumentDAO(db)
	entityDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)
	factSourceDAO := dao.NewFactSourceDAO(db)

	// Create three documents.
	var docIDs []int
	for i := 1; i <= 3; i++ {
		id, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: fmt.Sprintf("/test/doc%d.md", i)})
		if err != nil {
			t.Fatalf("create doc%d: %v", i, err)
		}
		docIDs = append(docIDs, id)
	}

	subjID, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	factID, err := factDAO.CreateOrIgnore(ctx, dao.Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
	})
	if err != nil {
		t.Fatalf("create fact: %v", err)
	}

	// Add three fact sources (one per document).
	for _, docID := range docIDs {
		q := "quote"
		if _, err := factSourceDAO.Create(ctx, dao.FactSource{FactID: factID, DocumentID: docID, Quote: &q}); err != nil {
			t.Fatalf("create fact source: %v", err)
		}
	}

	// Set initial weight to 3.
	if err := factDAO.RecomputeWeights(ctx, []int64{int64(factID)}); err != nil {
		t.Fatalf("RecomputeWeights: %v", err)
	}

	fact, err := factDAO.GetByID(ctx, factID)
	if err != nil || fact == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fact.Weight != 3 {
		t.Errorf("initial weight = %d, want 3", fact.Weight)
	}

	gc := NewDocumentGC(db)

	// Clear doc1 → weight should decrease to 2.
	if err := gc.FullClearDocByID(ctx, docIDs[0]); err != nil {
		t.Fatalf("FullClearDocByID(doc1): %v", err)
	}

	fact, err = factDAO.GetByID(ctx, factID)
	if err != nil || fact == nil {
		t.Fatalf("GetByID after clear: %v", err)
	}
	if fact.Weight != 2 {
		t.Errorf("weight after clearing doc1 = %d, want 2", fact.Weight)
	}

	// Clear doc2 → weight should decrease to 1.
	if err := gc.FullClearDocByID(ctx, docIDs[1]); err != nil {
		t.Fatalf("FullClearDocByID(doc2): %v", err)
	}

	fact, err = factDAO.GetByID(ctx, factID)
	if err != nil || fact == nil {
		t.Fatalf("GetByID after clear: %v", err)
	}
	if fact.Weight != 1 {
		t.Errorf("weight after clearing doc2 = %d, want 1", fact.Weight)
	}
}

func TestFullClearDocByID_EntityReferencedByFactSurvives(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newGCFixture(t)
	defer cleanup()

	docDAO := dao.NewDocumentDAO(db)
	entityDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)
	factSourceDAO := dao.NewFactSourceDAO(db)
	entitySourceDAO := dao.NewEntitySourceDAO(db)

	// Create two documents.
	doc1ID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc1.md"})
	if err != nil {
		t.Fatalf("create doc1: %v", err)
	}
	doc2ID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc2.md"})
	if err != nil {
		t.Fatalf("create doc2: %v", err)
	}

	subjID, err := entityDAO.Create(ctx, dao.Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, dao.Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	// Link both entities to doc1 only.
	if _, err := entitySourceDAO.Create(ctx, dao.EntitySource{EntityID: subjID, DocumentID: doc1ID}); err != nil {
		t.Fatalf("entity source subj-doc1: %v", err)
	}
	if _, err := entitySourceDAO.Create(ctx, dao.EntitySource{EntityID: objID, DocumentID: doc1ID}); err != nil {
		t.Fatalf("entity source obj-doc1: %v", err)
	}

	factID, err := factDAO.CreateOrIgnore(ctx, dao.Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
	})
	if err != nil {
		t.Fatalf("create fact: %v", err)
	}

	// Fact source in doc2 (so the fact survives clearing doc1).
	q := "quote"
	if _, err := factSourceDAO.Create(ctx, dao.FactSource{FactID: factID, DocumentID: doc2ID, Quote: &q}); err != nil {
		t.Fatalf("create fact source for doc2: %v", err)
	}

	gc := NewDocumentGC(db)

	// Clear doc1. Both entities are linked only to doc1 and have no remaining entity_sources,
	// but they should survive because they're referenced by a fact (with sources in doc2).
	if err := gc.FullClearDocByID(ctx, doc1ID); err != nil {
		t.Fatalf("FullClearDocByID(doc1): %v", err)
	}

	subj, err := entityDAO.GetByID(ctx, subjID)
	if err != nil {
		t.Fatalf("GetByID(subj): %v", err)
	}
	if subj == nil {
		t.Error("subject entity should survive (referenced by a fact with sources in another document)")
	}

	obj, err := entityDAO.GetByID(ctx, objID)
	if err != nil {
		t.Fatalf("GetByID(obj): %v", err)
	}
	if obj == nil {
		t.Error("object entity should survive (referenced by a fact with sources in another document)")
	}
}

func TestFullClearDocByID_OtherDocumentsUntouched(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newGCFixture(t)
	defer cleanup()

	docDAO := dao.NewDocumentDAO(db)
	chunkDAO := dao.NewChunkDAO(db)

	// Create two documents with chunks.
	doc1ID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc1.md"})
	if err != nil {
		t.Fatalf("create doc1: %v", err)
	}
	doc2ID, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: "/test/doc2.md"})
	if err != nil {
		t.Fatalf("create doc2: %v", err)
	}

	for _, docID := range []int{doc1ID, doc2ID} {
		if _, err := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "chunk text", SequenceNum: 0}); err != nil {
			t.Fatalf("create chunk for doc %d: %v", docID, err)
		}
	}

	gc := NewDocumentGC(db)

	if err := gc.FullClearDocByID(ctx, doc1ID); err != nil {
		t.Fatalf("FullClearDocByID(doc1): %v", err)
	}

	chunks2, err := chunkDAO.ListByDocID(ctx, doc2ID)
	if err != nil {
		t.Fatalf("ListByDocID(doc2): %v", err)
	}
	if len(chunks2) != 1 {
		t.Errorf("chunks for doc2 = %d, want 1 (untouched)", len(chunks2))
	}
}

func TestDeleteOrphanedDocuments(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newGCFixture(t)
	defer cleanup()

	docDAO := dao.NewDocumentDAO(db)
	chunkDAO := dao.NewChunkDAO(db)

	// Create three documents.
	var docIDs []int
	for i := 1; i <= 3; i++ {
		id, err := docDAO.Create(ctx, dao.Document{SourceType: "markdown", OriginalPath: fmt.Sprintf("/test/doc%d.md", i)})
		if err != nil {
			t.Fatalf("create doc%d: %v", i, err)
		}
		docIDs = append(docIDs, id)
	}

	// Only doc2 has a chunk → it should survive.
	if _, err := chunkDAO.Create(ctx, dao.Chunk{DocID: docIDs[1], ChunkText: "chunk text", SequenceNum: 0}); err != nil {
		t.Fatalf("create chunk for doc2: %v", err)
	}

	gc := NewDocumentGC(db)
	deleted, err := gc.DeleteOrphanedDocuments(ctx)
	if err != nil {
		t.Fatalf("DeleteOrphanedDocuments: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2 (doc1 and doc3 have no chunks/sources)", deleted)
	}

	// Verify doc2 survives.
	doc2, err := docDAO.GetByID(ctx, docIDs[1])
	if err != nil {
		t.Fatalf("GetByID(doc2): %v", err)
	}
	if doc2 == nil {
		t.Error("doc2 should survive (has a chunk)")
	}

	// Verify doc1 and doc3 are gone.
	for i, id := range []int{docIDs[0], docIDs[2]} {
		doc, err := docDAO.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID(doc%d): %v", i+1, err)
		}
		if doc != nil {
			t.Errorf("doc%d should be deleted (no chunks/sources)", i+1)
		}
	}
}
