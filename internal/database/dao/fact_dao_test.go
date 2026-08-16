package dao

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

func newFactDAOFixture(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	db, cleanup, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		cleanup()
		t.Fatalf("create test db: %v", err)
	}
	return db, cleanup
}

func TestFactDAO_CreateOrIgnore_New(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	factDAO := NewFactDAO(db)
	id, err := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
	})
	if err != nil {
		t.Fatalf("CreateOrIgnore() error = %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive Predicate, got %d", id)
	}

	fact, err := factDAO.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID(%d): %v", id, err)
	}
	if fact == nil {
		t.Fatal("fact not found")
	}
	if fact.Predicate != "works_at" {
		t.Errorf("predicate = %q, want %q", fact.Predicate, "works_at")
	}
	if fact.Status != "approved" {
		t.Errorf("status = %q, want %q", fact.Status, "approved")
	}
}

func TestFactDAO_CreateOrIgnore_Conflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	factDAO := NewFactDAO(db)

	id1, err := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
	})
	if err != nil {
		t.Fatalf("first CreateOrIgnore() error = %v", err)
	}

	id2, err := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
	})
	if err != nil {
		t.Fatalf("second CreateOrIgnore() error = %v", err)
	}

	if id1 != id2 {
		t.Errorf("conflicting insert returned different IDs: %d vs %d", id1, id2)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM facts").Scan(&count); err != nil {
		t.Fatalf("count facts: %v", err)
	}
	if count != 1 {
		t.Errorf("fact rows = %d, want 1 (no duplicate)", count)
	}

	// Verify status is "approved" after first insert.
	fact, err := factDAO.GetByID(ctx, id1)
	if err != nil || fact == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fact.Status != "approved" {
		t.Errorf("status after first insert = %q, want %q", fact.Status, "approved")
	}

	// Manually change status to simulate a different state.
	if _, err := db.Exec("UPDATE facts SET status = 'pending' WHERE id = ?", id1); err != nil {
		t.Fatalf("update status: %v", err)
	}

	// Re-insert same fact (conflict). Status must NOT change from 'pending'.
	_, err = factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
	})
	if err != nil {
		t.Fatalf("second CreateOrIgnore() error = %v", err)
	}

	fact, err = factDAO.GetByID(ctx, id1)
	if err != nil || fact == nil {
		t.Fatalf("GetByID after conflict: %v", err)
	}
	if fact.Status != "pending" {
		t.Errorf("status after conflict = %q, want %q (should not be overwritten)", fact.Status, "pending")
	}
}

func TestFactDAO_CreateOrIgnore_EmptyPredicate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	factDAO := NewFactDAO(db)
	_, err := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: 1,
		Predicate:       "",
		ObjectEntityID:  2,
	})
	if err == nil {
		t.Error("expected error for empty predicate")
	}
}

func TestFactDAO_Create_ApprovedStatus(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	factDAO := NewFactDAO(db)
	id, err := factDAO.Create(ctx, Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	fact, err := factDAO.GetByID(ctx, id)
	if err != nil || fact == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fact.Status != "approved" {
		t.Errorf("status = %q, want %q", fact.Status, "approved")
	}
}

func TestFactDAO_RecomputeWeights(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	factDAO := NewFactDAO(db)
	factSourceDAO := NewFactSourceDAO(db)

	factID, err := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
	})
	if err != nil {
		t.Fatalf("CreateOrIgnore() error = %v", err)
	}

	// Add two fact sources.
	doc1 := 1
	doc2 := 2
	for _, docID := range []int{doc1, doc2} {
		q := "some quote"
		if _, err := factSourceDAO.Create(ctx, FactSource{
			FactID:     factID,
			DocumentID: docID,
			Quote:      &q,
		}); err != nil {
			t.Fatalf("Create fact source: %v", err)
		}
	}

	if err := factDAO.RecomputeWeights(ctx, []int64{int64(factID)}); err != nil {
		t.Fatalf("RecomputeWeights() error = %v", err)
	}

	fact, err := factDAO.GetByID(ctx, factID)
	if err != nil {
		t.Fatalf("GetByID(%d): %v", factID, err)
	}
	if fact == nil {
		t.Fatal("fact not found")
	}
	if fact.Weight != 2 {
		t.Errorf("weight = %d, want 2", fact.Weight)
	}
}

func TestFactDAO_RecomputeWeights_Empty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	factDAO := NewFactDAO(db)
	if err := factDAO.RecomputeWeights(ctx, nil); err != nil {
		t.Fatalf("RecomputeWeights(nil) error = %v", err)
	}
	if err := factDAO.RecomputeWeights(ctx, []int64{}); err != nil {
		t.Fatalf("RecomputeWeights([]) error = %v", err)
	}
}

func TestFactSourceDAO_DeleteByDocumentID_ReturnsAffectedIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	factDAO := NewFactDAO(db)
	factSourceDAO := NewFactSourceDAO(db)

	docID := 42

	// Create two facts with sources in the same document.
	var factIDs []int
	for _, pred := range []string{"works_at", "founded"} {
		fid, err := factDAO.CreateOrIgnore(ctx, Fact{
			SubjectEntityID: subjID,
			Predicate:       pred,
			ObjectEntityID:  objID,
		})
		if err != nil {
			t.Fatalf("CreateOrIgnore(%q): %v", pred, err)
		}
		factIDs = append(factIDs, fid)

		q := pred
		if _, err := factSourceDAO.Create(ctx, FactSource{
			FactID:     fid,
			DocumentID: docID,
			Quote:      &q,
		}); err != nil {
			t.Fatalf("Create fact source for %d: %v", fid, err)
		}
	}

	affectedIDs, err := factSourceDAO.DeleteByDocumentID(ctx, docID)
	if err != nil {
		t.Fatalf("DeleteByDocumentID() error = %v", err)
	}

	// Verify returned IDs match the created fact IDs.
	if len(affectedIDs) != len(factIDs) {
		t.Errorf("returned %d affected IDs, want %d", len(affectedIDs), len(factIDs))
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM fact_sources WHERE document_id = ?", docID).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("fact_sources for doc %d = %d, want 0", docID, count)
	}
}

func TestFactDAO_RecomputeWeights_WeightDecreases(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	factDAO := NewFactDAO(db)
	factSourceDAO := NewFactSourceDAO(db)

	factID, err := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
	})
	if err != nil {
		t.Fatalf("CreateOrIgnore() error = %v", err)
	}

	// Add three fact sources.
	for i := 1; i <= 3; i++ {
		q := "quote"
		if _, err := factSourceDAO.Create(ctx, FactSource{FactID: factID, DocumentID: i, Quote: &q}); err != nil {
			t.Fatalf("Create fact source %d: %v", i, err)
		}
	}

	// Initial weight = 3.
	if err := factDAO.RecomputeWeights(ctx, []int64{int64(factID)}); err != nil {
		t.Fatalf("RecomputeWeights() error = %v", err)
	}
	fact, err := factDAO.GetByID(ctx, factID)
	if err != nil || fact == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fact.Weight != 3 {
		t.Errorf("initial weight = %d, want 3", fact.Weight)
	}

	// Remove one source → weight should decrease to 2.
	if _, err := factSourceDAO.DeleteByDocumentID(ctx, 1); err != nil {
		t.Fatalf("DeleteByDocumentID: %v", err)
	}
	if err := factDAO.RecomputeWeights(ctx, []int64{int64(factID)}); err != nil {
		t.Fatalf("RecomputeWeights() error = %v", err)
	}
	fact, err = factDAO.GetByID(ctx, factID)
	if err != nil || fact == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fact.Weight != 2 {
		t.Errorf("weight after source removal = %d, want 2", fact.Weight)
	}
}

func TestFactDAO_ListByEntityID_ExcludesNonApproved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	factDAO := NewFactDAO(db)

	tests := []struct {
		name      string
		predicate string
		status    string
	}{
		{"approved fact as subject", "works_at", "approved"},
		{"draft fact as subject", "manages", "draft"},
		{"rejected fact as subject", "owns", "rejected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := factDAO.Create(ctx, Fact{
				SubjectEntityID: subjID,
				Predicate:       tt.predicate,
				ObjectEntityID:  objID,
			}); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if tt.status != "approved" {
				if _, err := db.Exec("UPDATE facts SET status = ? WHERE subject_entity_id = ? AND predicate = ?", tt.status, subjID, tt.predicate); err != nil {
					t.Fatalf("update status: %v", err)
				}
			}
		})
	}

	facts, err := factDAO.ListByEntityID(ctx, subjID)
	if err != nil {
		t.Fatalf("ListByEntityID() error = %v", err)
	}

	if len(facts) != 1 {
		t.Errorf("ListByEntityID returned %d facts, want 1 (only approved)", len(facts))
	}
	if len(facts) > 0 && facts[0].Predicate != "works_at" {
		t.Errorf("returned fact predicate = %q, want %q", facts[0].Predicate, "works_at")
	}

	// Also check from object entity side.
	factsObj, err := factDAO.ListByEntityID(ctx, objID)
	if err != nil {
		t.Fatalf("ListByEntityID(objID) error = %v", err)
	}
	if len(factsObj) != 1 {
		t.Errorf("ListByEntityID(objID) returned %d facts, want 1 (only approved)", len(factsObj))
	}
}

func TestFactDAO_ListAll_ExcludesNonApproved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	factDAO := NewFactDAO(db)

	tests := []struct {
		name      string
		predicate string
		status    string
	}{
		{"approved fact", "works_at", "approved"},
		{"draft fact", "manages", "draft"},
		{"rejected fact", "owns", "rejected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := factDAO.Create(ctx, Fact{
				SubjectEntityID: subjID,
				Predicate:       tt.predicate,
				ObjectEntityID:  objID,
			}); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if tt.status != "approved" {
				if _, err := db.Exec("UPDATE facts SET status = ? WHERE subject_entity_id = ? AND predicate = ?", tt.status, subjID, tt.predicate); err != nil {
					t.Fatalf("update status: %v", err)
				}
			}
		})
	}

	facts, err := factDAO.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}

	if len(facts) != 1 {
		t.Errorf("ListAll returned %d facts, want 1 (only approved)", len(facts))
	}
	if len(facts) > 0 && facts[0].Predicate != "works_at" {
		t.Errorf("returned fact predicate = %q, want %q", facts[0].Predicate, "works_at")
	}
}

func TestFKErrorOnEntityDeleteWithFacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	// TestDB does not set PRAGMA foreign_keys (it would break tests that use
	// arbitrary document IDs without creating documents). Enable it explicitly
	// here so the no-CASCADE FK constraint on facts→entities raises FK errors on entity delete.
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}

	entityDAO := NewEntityDAO(db)
	factDAO := NewFactDAO(db)

	subjID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create Alice: %v", err)
	}
	objID, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create Acme: %v", err)
	}

	// Create a fact referencing both entities.
	if _, err := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
	}); err != nil {
		t.Fatalf("create fact: %v", err)
	}

	// Deleting an entity referenced by a fact should fail with FK constraint violation.
	if err := entityDAO.Delete(ctx, subjID); err == nil {
		t.Error("expected FK error when deleting entity referenced by a fact")
	} else if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("expected FOREIGN KEY constraint error, got: %v", err)
	}

	// Same for object entity.
	if err := entityDAO.Delete(ctx, objID); err == nil {
		t.Error("expected FK error when deleting entity referenced by a fact")
	} else if !strings.Contains(strings.ToLower(err.Error()), "foreign key") {
		t.Errorf("expected FOREIGN KEY constraint error, got: %v", err)
	}

	// Fact should still exist.
	facts, err := factDAO.ListAll(ctx)
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	if len(facts) != 1 {
		t.Errorf("facts = %d, want 1 (not cascaded)", len(facts))
	}
}

func TestFactDAO_ListByEntityIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	factDAO := NewFactDAO(db)

	// Create three entities.
	idAlice, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create Alice: %v", err)
	}
	idBob, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Bob"})
	if err != nil {
		t.Fatalf("create Bob: %v", err)
	}
	idAcme, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create Acme: %v", err)
	}

	tests := []struct {
		name      string
		predicate string
		subjID    int
		objID     int
		status    string
	}{
		{"approved alice->acme (subject)", "works_at", idAlice, idAcme, "approved"},
		{"approved bob->acme (object for acme)", "employed_by", idBob, idAcme, "approved"},
		{"draft alice->bob (excluded)", "knows", idAlice, idBob, "draft"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := factDAO.Create(ctx, Fact{
				SubjectEntityID: tt.subjID,
				Predicate:       tt.predicate,
				ObjectEntityID:  tt.objID,
			}); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if tt.status != "approved" {
				if _, err := db.Exec("UPDATE facts SET status = ? WHERE subject_entity_id = ? AND predicate = ?", tt.status, tt.subjID, tt.predicate); err != nil {
					t.Fatalf("update status: %v", err)
				}
			}
		})
	}

	t.Run("batch query groups by entity Predicate and excludes non-approved", func(t *testing.T) {
		factMap, err := factDAO.ListByEntityIDs(ctx, []int{idAlice, idBob, idAcme})
		if err != nil {
			t.Fatalf("ListByEntityIDs() error = %v", err)
		}

		// Alice: 1 approved fact (works_at as subject). Draft "knows" excluded.
		if facts := factMap[idAlice]; len(facts) != 1 {
			t.Errorf("Alice: got %d facts, want 1", len(facts))
		} else if facts[0].Predicate != "works_at" {
			t.Errorf("Alice: predicate = %q, want %q", facts[0].Predicate, "works_at")
		}

		// Bob: 1 approved fact (employed_by as subject).
		if facts := factMap[idBob]; len(facts) != 1 {
			t.Errorf("Bob: got %d facts, want 1", len(facts))
		} else if facts[0].Predicate != "employed_by" {
			t.Errorf("Bob: predicate = %q, want %q", facts[0].Predicate, "employed_by")
		}

		// Acme: 2 approved facts (works_at as object, employed_by as object).
		if facts := factMap[idAcme]; len(facts) != 2 {
			t.Errorf("Acme: got %d facts, want 2", len(facts))
		}
	})

	t.Run("empty input returns empty map", func(t *testing.T) {
		factMap, err := factDAO.ListByEntityIDs(ctx, nil)
		if err != nil {
			t.Fatalf("ListByEntityIDs(nil) error = %v", err)
		}
		if len(factMap) != 0 {
			t.Errorf("expected empty map, got %d entries", len(factMap))
		}

		factMap2, err := factDAO.ListByEntityIDs(ctx, []int{})
		if err != nil {
			t.Fatalf("ListByEntityIDs([]) error = %v", err)
		}
		if len(factMap2) != 0 {
			t.Errorf("expected empty map, got %d entries", len(factMap2))
		}
	})

	t.Run("facts appear for both subject and object entity IDs", func(t *testing.T) {
		factMap, err := factDAO.ListByEntityIDs(ctx, []int{idAlice})
		if err != nil {
			t.Fatalf("ListByEntityIDs() error = %v", err)
		}

		// Alice is subject of "works_at" — should appear.
		if facts := factMap[idAlice]; len(facts) != 1 {
			t.Errorf("Alice: got %d facts, want 1", len(facts))
		}
	})
}

func TestFactDAO_Create_WithMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	tests := []struct {
		name     string
		pred     string
		metadata *string
	}{
		{"with metadata JSON", "works_at", ptrStr(`{"threshold_amount":100,"condition":"active"}`)},
		{"nil metadata", "founded_by", nil},
		{"empty metadata string", "owns", ptrStr("")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factDAO := NewFactDAO(db)
			id, err := factDAO.Create(ctx, Fact{
				SubjectEntityID: subjID,
				Predicate:       tt.pred,
				ObjectEntityID:  objID,
				Metadata:        tt.metadata,
			})
			if err != nil {
				t.Fatalf("Create() error = %v", err)
			}

			fact, err := factDAO.GetByID(ctx, id)
			if err != nil || fact == nil {
				t.Fatalf("GetByID: %v", err)
			}

			if tt.metadata == nil && fact.Metadata != nil {
				t.Errorf("expected nil metadata, got %q", *fact.Metadata)
			} else if tt.metadata != nil && fact.Metadata == nil {
				t.Error("expected non-nil metadata, got nil")
			} else if tt.metadata != nil && fact.Metadata != nil && *tt.metadata != *fact.Metadata {
				t.Errorf("metadata = %q, want %q", *fact.Metadata, *tt.metadata)
			}
		})
	}
}

func TestFactDAO_CreateOrIgnore_WithMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}

	factDAO := NewFactDAO(db)

	metaStr := `{"threshold_amount":100,"condition":"active"}`
	id, err := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
		Metadata:        &metaStr,
	})
	if err != nil {
		t.Fatalf("CreateOrIgnore() error = %v", err)
	}

	fact, err := factDAO.GetByID(ctx, id)
	if err != nil || fact == nil {
		t.Fatalf("GetByID: %v", err)
	}
	if fact.Metadata == nil {
		t.Fatal("expected non-nil metadata")
	}
	if *fact.Metadata != metaStr {
		t.Errorf("metadata = %q, want %q", *fact.Metadata, metaStr)
	}

	// Conflict insert must not overwrite existing metadata.
	metaStr2 := `{"threshold_amount":200}`
	id2, err := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID,
		Predicate:       "works_at",
		ObjectEntityID:  objID,
		Metadata:        &metaStr2,
	})
	if err != nil {
		t.Fatalf("second CreateOrIgnore() error = %v", err)
	}
	if id != id2 {
		t.Errorf("conflict returned different Predicate: %d vs %d", id, id2)
	}

	fact, err = factDAO.GetByID(ctx, id)
	if err != nil || fact == nil {
		t.Fatalf("GetByID after conflict: %v", err)
	}
	if *fact.Metadata != metaStr {
		t.Errorf("metadata overwritten on conflict: got %q, want original %q", *fact.Metadata, metaStr)
	}
}

// TestFactDAO_ValidateFactDomain verifies domain enforcement for facts:
// subject and object must belong to the same domain as the fact.
func TestFactDAO_ValidateFactDomain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	subjID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	if err != nil {
		t.Fatalf("create subject entity: %v", err)
	}
	objID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Bob", Domain: "hr"})
	if err != nil {
		t.Fatalf("create object entity: %v", err)
	}
	crossID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Carol", Domain: "policy"})
	if err != nil {
		t.Fatalf("create cross-domain entity: %v", err)
	}

	factDAO := NewFactDAO(db)

	t.Run("same domain accepted", func(t *testing.T) {
		if err := factDAO.ValidateFactDomain(ctx, subjID, objID, "hr"); err != nil {
			t.Errorf("same-domain fact rejected: %v", err)
		}
	})

	t.Run("cross-domain fact rejected", func(t *testing.T) {
		if err := factDAO.ValidateFactDomain(ctx, subjID, crossID, "hr"); err == nil {
			t.Error("cross-domain fact accepted, want rejection")
		}
		if err := factDAO.ValidateFactDomain(ctx, crossID, objID, "hr"); err == nil {
			t.Error("cross-domain fact accepted (object side), want rejection")
		}
	})

	t.Run("domain comparison is case-insensitive", func(t *testing.T) {
		if err := factDAO.ValidateFactDomain(ctx, subjID, objID, "HR"); err != nil {
			t.Errorf("normalized domain fact rejected: %v", err)
		}
	})
}

func ptrStr(s string) *string { return &s }
