package dao_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/devmix/synopsis/internal/database"
	"github.com/devmix/synopsis/internal/database/dao"
)

// setupTestChunks creates a document, entity, and N chunks linked to the entity.
func setupTestChunks(t *testing.T, d *database.Database, ctx context.Context, numChunks int) (entityID, docID int) {
	t.Helper()

	// Create document via raw SQL (FK target for chunks).
	res, err := d.DB().Exec(
		"INSERT INTO documents (source_type, original_path) VALUES ('test', '/test/doc.md');",
	)
	if err != nil {
		t.Fatalf("insert test document: %v", err)
	}
	docID64, _ := res.LastInsertId()
	docID = int(docID64)

	entityDAO := dao.NewEntityDAO(d.DB())
	desc := "test entity"
	entityID, err = entityDAO.Create(ctx, dao.Entity{
		Type:        "technology",
		Name:        "TestEntity",
		Domain:      "tech",
		Description: &desc,
	})
	if err != nil {
		t.Fatalf("create test entity: %v", err)
	}

	chunkDAO := dao.NewChunkDAO(d.DB())
	ceDAO := dao.NewChunkEntityDAO(d.DB())

	for i := 0; i < numChunks; i++ {
		chunkID, err := chunkDAO.Create(ctx, dao.Chunk{
			DocID:       docID,
			ChunkText:   fmt.Sprintf("chunk text seq %d for entity", i),
			SequenceNum: i,
		})
		if err != nil {
			t.Fatalf("create chunk %d: %v", i, err)
		}
		if err := ceDAO.Link(ctx, chunkID, entityID); err != nil {
			t.Fatalf("link chunk %d to entity: %v", chunkID, err)
		}
	}

	return entityID, docID
}

func TestChunkEntityDAOGetChunkTextsByEntity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		entityID      int
		limit         int
		wantCount     int
		wantFirstText string // exact text expected as first result
	}{
		{
			name:          "entity with chunks returns texts ordered by sequence",
			entityID:      1,
			limit:         3,
			wantCount:     3,
			wantFirstText: "chunk text seq 0 for entity",
		},
		{
			name:          "limit restricts result count",
			entityID:      1,
			limit:         2,
			wantCount:     2,
			wantFirstText: "chunk text seq 0 for entity",
		},
		{
			name:          "entity with no chunks returns placeholder",
			entityID:      999,
			limit:         5,
			wantCount:     1,
			wantFirstText: "<no context available>",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d, cleanup := setupTestDB(t)
			defer cleanup()

			ctx := context.Background()

			entityID, _ := setupTestChunks(t, d, ctx, 5)

			ceDAO := dao.NewChunkEntityDAO(d.DB())

			queryEntityID := entityID
			if tt.entityID == 999 {
				queryEntityID = 999 // nonexistent entity
			}

			texts, err := ceDAO.GetChunkTextsByEntity(ctx, queryEntityID, tt.limit)
			if err != nil {
				t.Fatalf("GetChunkTextsByEntity() error = %v", err)
			}

			if len(texts) != tt.wantCount {
				t.Errorf("got %d texts, want %d", len(texts), tt.wantCount)
			}

			if len(texts) > 0 && texts[0] != tt.wantFirstText {
				t.Errorf("first text = %q, want %q", texts[0], tt.wantFirstText)
			}
		})
	}
}

func TestChunkEntityDAOGetChunkTextsByEntity_Ordering(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	chunkDAO := dao.NewChunkDAO(d.DB())
	entityDAO := dao.NewEntityDAO(d.DB())
	ceDAO := dao.NewChunkEntityDAO(d.DB())

	desc := "ordering test"
	entityID, err := entityDAO.Create(ctx, dao.Entity{
		Type:        "technology",
		Name:        "OrderTest",
		Domain:      "tech",
		Description: &desc,
	})
	if err != nil {
		t.Fatalf("create entity: %v", err)
	}

	// Create document first.
	res, err := d.DB().Exec(
		"INSERT INTO documents (source_type, original_path) VALUES ('test', '/test/doc.md');",
	)
	if err != nil {
		t.Fatalf("insert test document: %v", err)
	}
	docID64, _ := res.LastInsertId()
	docID := int(docID64)

	// Insert chunks with out-of-order IDs but sequential sequence_num.
	chunkData := []struct{ seq int }{{seq: 2}, {seq: 0}, {seq: 1}}

	for _, cd := range chunkData {
		chunkID, err := chunkDAO.Create(ctx, dao.Chunk{
			DocID:       docID,
			ChunkText:   fmt.Sprintf("seq_%d", cd.seq),
			SequenceNum: cd.seq,
		})
		if err != nil {
			t.Fatalf("create chunk: %v", err)
		}
		if err := ceDAO.Link(ctx, chunkID, entityID); err != nil {
			t.Fatalf("link chunk: %v", err)
		}
	}

	texts, err := ceDAO.GetChunkTextsByEntity(ctx, entityID, 10)
	if err != nil {
		t.Fatalf("GetChunkTextsByEntity() error = %v", err)
	}

	// Verify ordering by sequence_num.
	wantOrder := []string{"seq_0", "seq_1", "seq_2"}
	if len(texts) != len(wantOrder) {
		t.Fatalf("got %d texts, want %d", len(texts), len(wantOrder))
	}
	for i, want := range wantOrder {
		if texts[i] != want {
			t.Errorf("texts[%d] = %q, want %q", i, texts[i], want)
		}
	}
}

func TestChunkEntityDAOGetChunkTextsByEntity_ContextCancellation(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	ceDAO := dao.NewChunkEntityDAO(d.DB())

	_, err := ceDAO.GetChunkTextsByEntity(ctx, 1, 5)
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
}

func TestChunkEntityDAOGetChunkTextsByEntity_EmptyLimit(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	entityID, _ := setupTestChunks(t, d, ctx, 1)

	ceDAO := dao.NewChunkEntityDAO(d.DB())

	texts, err := ceDAO.GetChunkTextsByEntity(ctx, entityID, 0)
	if err != nil {
		t.Fatalf("GetChunkTextsByEntity() error = %v", err)
	}

	// With limit=0, no rows returned → placeholder.
	if len(texts) != 1 || texts[0] != "<no context available>" {
		t.Errorf("expected placeholder for limit=0, got %q", texts)
	}
}

func TestChunkEntityDAOGetChunkTextsByEntity_MultipleEntities(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	chunkDAO := dao.NewChunkDAO(d.DB())
	entityDAO := dao.NewEntityDAO(d.DB())
	ceDAO := dao.NewChunkEntityDAO(d.DB())

	// Create document first.
	res, err := d.DB().Exec(
		"INSERT INTO documents (source_type, original_path) VALUES ('test', '/test/doc.md');",
	)
	if err != nil {
		t.Fatalf("insert test document: %v", err)
	}
	docID64, _ := res.LastInsertId()
	docID := int(docID64)

	desc1 := "entity A"
	entityA, err := entityDAO.Create(ctx, dao.Entity{
		Type: "technology", Name: "EntityA", Domain: "tech", Description: &desc1,
	})
	if err != nil {
		t.Fatalf("create entity A: %v", err)
	}

	desc2 := "entity B"
	entityB, err := entityDAO.Create(ctx, dao.Entity{
		Type: "technology", Name: "EntityB", Domain: "tech", Description: &desc2,
	})
	if err != nil {
		t.Fatalf("create entity B: %v", err)
	}

	// Link chunk 1 to entity A.
	chunkA, err := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "text for A", SequenceNum: 0})
	if err != nil {
		t.Fatalf("create chunk A: %v", err)
	}
	if err := ceDAO.Link(ctx, chunkA, entityA); err != nil {
		t.Fatalf("link chunk to entity A: %v", err)
	}

	// Link chunk 2 to entity B.
	chunkB, err := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "text for B", SequenceNum: 0})
	if err != nil {
		t.Fatalf("create chunk B: %v", err)
	}
	if err := ceDAO.Link(ctx, chunkB, entityB); err != nil {
		t.Fatalf("link chunk to entity B: %v", err)
	}

	textsA, err := ceDAO.GetChunkTextsByEntity(ctx, entityA, 5)
	if err != nil {
		t.Fatalf("GetChunkTextsByEntity(A): %v", err)
	}
	if len(textsA) != 1 || textsA[0] != "text for A" {
		t.Errorf("entity A texts = %q, want [\"text for A\"]", textsA)
	}

	textsB, err := ceDAO.GetChunkTextsByEntity(ctx, entityB, 5)
	if err != nil {
		t.Fatalf("GetChunkTextsByEntity(B): %v", err)
	}
	if len(textsB) != 1 || textsB[0] != "text for B" {
		t.Errorf("entity B texts = %q, want [\"text for B\"]", textsB)
	}
}

func TestChunkEntityCascade_DeleteDocument(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	entityID, docID := setupTestChunks(t, d, ctx, 3)

	// Verify chunk_entities has entries before deletion.
	var ceCountBefore int
	if err := d.DB().QueryRow("SELECT COUNT(*) FROM chunk_entities WHERE entity_id = ?", entityID).Scan(&ceCountBefore); err != nil {
		t.Fatalf("count chunk_entities: %v", err)
	}
	if ceCountBefore == 0 {
		t.Fatal("expected chunk_entities to have entries before deletion")
	}

	// Delete the document — should cascade through chunks → chunk_entities.
	if _, err := d.DB().Exec("DELETE FROM documents WHERE id = ?", docID); err != nil {
		t.Fatalf("delete document: %v", err)
	}

	// Verify chunks were cascaded-deleted.
	var chunkCount int
	if err := d.DB().QueryRow("SELECT COUNT(*) FROM chunks WHERE doc_id = ?", docID).Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunkCount != 0 {
		t.Errorf("chunks after document delete = %d, want 0 (CASCADE)", chunkCount)
	}

	// Verify chunk_entities were cascaded-deleted.
	var ceCountAfter int
	if err := d.DB().QueryRow("SELECT COUNT(*) FROM chunk_entities").Scan(&ceCountAfter); err != nil {
		t.Fatalf("count chunk_entities after: %v", err)
	}
	if ceCountAfter != 0 {
		t.Errorf("chunk_entities after document delete = %d, want 0 (CASCADE)", ceCountAfter)
	}
}

func TestChunkEntityCascade_DeleteChunk(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	entityID, _ := setupTestChunks(t, d, ctx, 3)

	// Get one chunk Predicate linked to the entity.
	var chunkID int
	if err := d.DB().QueryRow("SELECT ce.chunk_id FROM chunk_entities ce WHERE ce.entity_id = ? LIMIT 1", entityID).Scan(&chunkID); err != nil {
		t.Fatalf("get chunk id: %v", err)
	}

	// Verify the chunk is linked to the entity.
	var ceCountBefore int
	if err := d.DB().QueryRow("SELECT COUNT(*) FROM chunk_entities WHERE chunk_id = ?", chunkID).Scan(&ceCountBefore); err != nil {
		t.Fatalf("count chunk_entities for chunk: %v", err)
	}
	if ceCountBefore == 0 {
		t.Fatal("expected chunk to have entity links before deletion")
	}

	// Delete the chunk — should cascade to chunk_entities.
	if _, err := d.DB().Exec("DELETE FROM chunks WHERE id = ?", chunkID); err != nil {
		t.Fatalf("delete chunk: %v", err)
	}

	// Verify chunk_entities for this chunk were cascaded-deleted.
	var ceCountAfter int
	if err := d.DB().QueryRow("SELECT COUNT(*) FROM chunk_entities WHERE chunk_id = ?", chunkID).Scan(&ceCountAfter); err != nil {
		t.Fatalf("count chunk_entities after: %v", err)
	}
	if ceCountAfter != 0 {
		t.Errorf("chunk_entities for deleted chunk = %d, want 0 (CASCADE)", ceCountAfter)
	}

	// Verify other chunks still have their links.
	var remainingLinks int
	if err := d.DB().QueryRow("SELECT COUNT(*) FROM chunk_entities WHERE entity_id = ?", entityID).Scan(&remainingLinks); err != nil {
		t.Fatalf("count remaining links: %v", err)
	}
	if remainingLinks == 0 {
		t.Error("expected other chunks to still have entity links")
	}
}

func TestChunkEntityCascade_DeleteEntity(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	entityID, _ := setupTestChunks(t, d, ctx, 3)

	// Verify chunk_entities has entries for this entity.
	var ceCountBefore int
	if err := d.DB().QueryRow("SELECT COUNT(*) FROM chunk_entities WHERE entity_id = ?", entityID).Scan(&ceCountBefore); err != nil {
		t.Fatalf("count chunk_entities: %v", err)
	}
	if ceCountBefore == 0 {
		t.Fatal("expected chunk_entities to have entries before deletion")
	}

	// Delete the entity — should cascade to chunk_entities.
	if _, err := d.DB().Exec("DELETE FROM entities WHERE id = ?", entityID); err != nil {
		t.Fatalf("delete entity: %v", err)
	}

	// Verify chunk_entities for this entity were cascaded-deleted.
	var ceCountAfter int
	if err := d.DB().QueryRow("SELECT COUNT(*) FROM chunk_entities WHERE entity_id = ?", entityID).Scan(&ceCountAfter); err != nil {
		t.Fatalf("count chunk_entities after: %v", err)
	}
	if ceCountAfter != 0 {
		t.Errorf("chunk_entities for deleted entity = %d, want 0 (CASCADE)", ceCountAfter)
	}

	// Verify chunks still exist.
	var chunkCount int
	if err := d.DB().QueryRow("SELECT COUNT(*) FROM chunks").Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunkCount == 0 {
		t.Error("expected chunks to still exist after entity deletion")
	}
}

func TestForeignKeysEnabled(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	var fkOn int
	if err := d.DB().QueryRow("PRAGMA foreign_keys;").Scan(&fkOn); err != nil {
		t.Fatalf("query PRAGMA foreign_keys: %v", err)
	}
	if fkOn != 1 {
		t.Errorf("PRAGMA foreign_keys = %d, want 1 (CASCADE requires FK enabled)", fkOn)
	}
}

func TestChunkEntityDAOGetEntityIDsByDocID(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	chunkDAO := dao.NewChunkDAO(d.DB())
	entityDAO := dao.NewEntityDAO(d.DB())
	ceDAO := dao.NewChunkEntityDAO(d.DB())

	// Create document.
	res, err := d.DB().Exec(
		"INSERT INTO documents (source_type, original_path) VALUES ('test', '/test/doc.md');",
	)
	if err != nil {
		t.Fatalf("insert test document: %v", err)
	}
	docID64, _ := res.LastInsertId()
	docID := int(docID64)

	desc1 := "entity A"
	entityA, err := entityDAO.Create(ctx, dao.Entity{
		Type: "technology", Name: "EntityA", Domain: "tech", Description: &desc1,
	})
	if err != nil {
		t.Fatalf("create entity A: %v", err)
	}

	desc2 := "entity B"
	entityB, err := entityDAO.Create(ctx, dao.Entity{
		Type: "technology", Name: "EntityB", Domain: "tech", Description: &desc2,
	})
	if err != nil {
		t.Fatalf("create entity B: %v", err)
	}

	desc3 := "entity C"
	entityC, err := entityDAO.Create(ctx, dao.Entity{
		Type: "technology", Name: "EntityC", Domain: "tech", Description: &desc3,
	})
	if err != nil {
		t.Fatalf("create entity C: %v", err)
	}

	// Link chunk 1 to entities A and B.
	chunk1, err := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "text for A and B", SequenceNum: 0})
	if err != nil {
		t.Fatalf("create chunk 1: %v", err)
	}
	if err := ceDAO.Link(ctx, chunk1, entityA); err != nil {
		t.Fatalf("link chunk to entity A: %v", err)
	}
	if err := ceDAO.Link(ctx, chunk1, entityB); err != nil {
		t.Fatalf("link chunk to entity B: %v", err)
	}

	// Link chunk 2 to entity C.
	chunk2, err := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "text for C", SequenceNum: 1})
	if err != nil {
		t.Fatalf("create chunk 2: %v", err)
	}
	if err := ceDAO.Link(ctx, chunk2, entityC); err != nil {
		t.Fatalf("link chunk to entity C: %v", err)
	}

	// Get entity IDs by doc Predicate.
	entityIDs, err := ceDAO.GetEntityIDsByDocID(ctx, docID)
	if err != nil {
		t.Fatalf("GetEntityIDsByDocID() error = %v", err)
	}

	if len(entityIDs) != 3 {
		t.Errorf("got %d entity IDs, want 3", len(entityIDs))
	}

	// Verify all three entities are present.
	entitySet := make(map[int]bool)
	for _, id := range entityIDs {
		entitySet[id] = true
	}
	if !entitySet[entityA] || !entitySet[entityB] || !entitySet[entityC] {
		t.Errorf("missing expected entities: got %v, want [%d, %d, %d]", entityIDs, entityA, entityB, entityC)
	}

	// Verify ordering (should be ordered by entity_id).
	for i := 1; i < len(entityIDs); i++ {
		if entityIDs[i] <= entityIDs[i-1] {
			t.Errorf("entity IDs not sorted: %v", entityIDs)
		}
	}
}

func TestChunkEntityDAOGetEntityIDsByDocID_NoEntities(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create document with no chunks or entities.
	res, err := d.DB().Exec(
		"INSERT INTO documents (source_type, original_path) VALUES ('test', '/test/empty.md');",
	)
	if err != nil {
		t.Fatalf("insert test document: %v", err)
	}
	docID64, _ := res.LastInsertId()
	docID := int(docID64)

	ceDAO := dao.NewChunkEntityDAO(d.DB())

	entityIDs, err := ceDAO.GetEntityIDsByDocID(ctx, docID)
	if err != nil {
		t.Fatalf("GetEntityIDsByDocID() error = %v", err)
	}

	if len(entityIDs) != 0 {
		t.Errorf("got %d entity IDs for empty document, want 0", len(entityIDs))
	}
}

func TestChunkEntityDAOGetEntityIDsByDocID_Deduplication(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	chunkDAO := dao.NewChunkDAO(d.DB())
	entityDAO := dao.NewEntityDAO(d.DB())
	ceDAO := dao.NewChunkEntityDAO(d.DB())

	res, err := d.DB().Exec(
		"INSERT INTO documents (source_type, original_path) VALUES ('test', '/test/doc.md');",
	)
	if err != nil {
		t.Fatalf("insert test document: %v", err)
	}
	docID64, _ := res.LastInsertId()
	docID := int(docID64)

	desc := "entity A"
	entityA, err := entityDAO.Create(ctx, dao.Entity{
		Type: "technology", Name: "EntityA", Domain: "tech", Description: &desc,
	})
	if err != nil {
		t.Fatalf("create entity A: %v", err)
	}

	// Link the same entity to two different chunks of the same document.
	chunk1, err := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "chunk 1 mentions EntityA", SequenceNum: 0})
	if err != nil {
		t.Fatalf("create chunk 1: %v", err)
	}
	chunk2, err := chunkDAO.Create(ctx, dao.Chunk{DocID: docID, ChunkText: "chunk 2 also mentions EntityA", SequenceNum: 1})
	if err != nil {
		t.Fatalf("create chunk 2: %v", err)
	}

	if err := ceDAO.Link(ctx, chunk1, entityA); err != nil {
		t.Fatalf("link chunk 1 to entity A: %v", err)
	}
	if err := ceDAO.Link(ctx, chunk2, entityA); err != nil {
		t.Fatalf("link chunk 2 to entity A: %v", err)
	}

	entityIDs, err := ceDAO.GetEntityIDsByDocID(ctx, docID)
	if err != nil {
		t.Fatalf("GetEntityIDsByDocID() error = %v", err)
	}

	// Should return exactly one Predicate (DISTINCT).
	if len(entityIDs) != 1 {
		t.Errorf("got %d entity IDs, want 1 (deduplication)", len(entityIDs))
	}
	if entityIDs[0] != entityA {
		t.Errorf("entity Predicate = %d, want %d", entityIDs[0], entityA)
	}
}

func TestChunkEntityDAOGetEntityIDsByDocID_ContextCancellation(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	ceDAO := dao.NewChunkEntityDAO(d.DB())

	_, err := ceDAO.GetEntityIDsByDocID(ctx, 1)
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
}
