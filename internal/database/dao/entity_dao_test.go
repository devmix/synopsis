package dao

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestUpdateName(t *testing.T) {
	t.Parallel()

	db, cleanup, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entityDAO := NewEntityDAO(db)

	id, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Apple"})
	if err != nil {
		t.Fatalf("create entity: %v", err)
	}

	t.Run("renames existing entity", func(t *testing.T) {
		if err := entityDAO.UpdateName(ctx, id, "Apple Inc."); err != nil {
			t.Fatalf("UpdateName() error = %v", err)
		}

		ent, err := entityDAO.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID() error = %v", err)
		}
		if ent == nil {
			t.Fatal("entity not found after rename")
		}
		if ent.Name != "Apple Inc." {
			t.Errorf("name = %q, want %q", ent.Name, "Apple Inc.")
		}
	})

	t.Run("missing entity returns error", func(t *testing.T) {
		if err := entityDAO.UpdateName(ctx, 9999, "Ghost"); err == nil {
			t.Error("expected error for missing entity")
		}
	})
}

func TestEntityDAO_ListCreatedSince(t *testing.T) {
	t.Parallel()

	db, cleanup, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entityDAO := NewEntityDAO(db)

	// Insert entities with known created_at timestamps.
	_, err = db.Exec(`INSERT INTO entities (type, name, domain, confidence, description, metadata_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"PERSON", "Alice", "hr", 0.9, nil, nil, "2024-01-01 10:00:00")
	if err != nil {
		t.Fatalf("insert entity Alice: %v", err)
	}
	_, err = db.Exec(`INSERT INTO entities (type, name, domain, confidence, description, metadata_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"PERSON", "Bob", "hr", 0.9, nil, nil, "2024-01-15 12:00:00")
	if err != nil {
		t.Fatalf("insert entity Bob: %v", err)
	}
	_, err = db.Exec(`INSERT INTO entities (type, name, domain, confidence, description, metadata_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"ORGANIZATION", "Acme Corp", "it", 0.95, nil, nil, "2024-02-01 08:00:00")
	if err != nil {
		t.Fatalf("insert entity Acme: %v", err)
	}

	tests := []struct {
		name      string
		since     string
		wantCount int
	}{
		{
			name:      "no entities before first creation",
			since:     "2023-12-31 23:59:59",
			wantCount: 3, // all three are after this time
		},
		{
			name:      "only recent entities",
			since:     "2024-01-15 12:00:00",
			wantCount: 1, // only Acme Corp (strictly greater)
		},
		{
			name:      "entities after mid-point",
			since:     "2024-01-10 00:00:00",
			wantCount: 2, // Bob and Acme Corp
		},
		{
			name:      "no entities after last creation",
			since:     "2024-03-01 00:00:00",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ents, err := entityDAO.ListCreatedSince(ctx, tt.since)
			if err != nil {
				t.Fatalf("ListCreatedSince(%q) error = %v", tt.since, err)
			}
			if len(ents) != tt.wantCount {
				t.Errorf("got %d entities, want %d", len(ents), tt.wantCount)
			}
		})
	}
}

func TestEntityDAO_DeleteOrphanedByIDs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)
	entitySourceDAO := NewEntitySourceDAO(db)

	// Create entities.
	ent1ID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice"})
	if err != nil {
		t.Fatalf("create Alice: %v", err)
	}
	ent2ID, err := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp"})
	if err != nil {
		t.Fatalf("create Acme: %v", err)
	}
	ent3ID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Bob"})
	if err != nil {
		t.Fatalf("create Bob: %v", err)
	}

	// Link ent1 to doc 1 (has a source -> should survive).
	if _, err := entitySourceDAO.Create(ctx, EntitySource{EntityID: ent1ID, DocumentID: 1}); err != nil {
		t.Fatalf("entity source ent1-doc1: %v", err)
	}

	// ent2 and ent3 have no sources -> candidates for deletion.
	deleted, err := entityDAO.DeleteOrphanedByIDs(ctx, []int{ent1ID, ent2ID, ent3ID})
	if err != nil {
		t.Fatalf("DeleteOrphanedByIDs: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2 (ent2 and ent3)", deleted)
	}

	// Verify ent1 survives (has a source).
	e1, err := entityDAO.GetByID(ctx, ent1ID)
	if err != nil || e1 == nil {
		t.Error("ent1 should survive (has an entity_source)")
	}

	// Verify ent2 and ent3 are deleted.
	for i, id := range []int{ent2ID, ent3ID} {
		e, err := entityDAO.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID(ent%d): %v", i+2, err)
		}
		if e != nil {
			t.Errorf("ent%d should be deleted (no sources)", i+2)
		}
	}
}

func TestEntityDAO_DeleteOrphanedByIDs_ExcludesEntityType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	entityDAO := NewEntityDAO(db)

	typeID, err := entityDAO.Create(ctx, Entity{Type: "EntityType", Name: "PERSON"})
	if err != nil {
		t.Fatalf("create EntityType: %v", err)
	}

	deleted, err := entityDAO.DeleteOrphanedByIDs(ctx, []int{typeID})
	if err != nil {
		t.Fatalf("DeleteOrphanedByIDs: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0 (EntityType excluded)", deleted)
	}

	e, err := entityDAO.GetByID(ctx, typeID)
	if err != nil || e == nil {
		t.Error("EntityType should survive DeleteOrphanedByIDs")
	}
}

func TestEntityDAO_DeleteOrphanedByIDs_ExcludesFactReferenced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

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

	// Both entities have no entity_sources but are referenced by a fact -> should survive.
	deleted, err := entityDAO.DeleteOrphanedByIDs(ctx, []int{subjID, objID})
	if err != nil {
		t.Fatalf("DeleteOrphanedByIDs: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0 (both entities referenced by a fact)", deleted)
	}

	for i, id := range []int{subjID, objID} {
		e, err := entityDAO.GetByID(ctx, id)
		if err != nil || e == nil {
			t.Errorf("entity %d should survive (referenced by a fact)", i)
		}
	}
}

func TestEntityDAO_DeleteOrphanedEntityIDs_ExcludesFactReferenced(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

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

	// Both entities have no entity_sources but are referenced by a fact -> should survive.
	deleted, err := entityDAO.DeleteOrphanedEntityIDs(ctx)
	if err != nil {
		t.Fatalf("DeleteOrphanedEntityIDs: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0 (both entities referenced by a fact)", deleted)
	}

	for i, id := range []int{subjID, objID} {
		e, err := entityDAO.GetByID(ctx, id)
		if err != nil || e == nil {
			t.Errorf("entity %d should survive (referenced by a fact)", i)
		}
	}
}

func TestEntityDAO_Create_WithConfidenceAndMetadata(t *testing.T) {
	t.Parallel()

	db, cleanup, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entityDAO := NewEntityDAO(db)

	metaJSON := `{"source":"llm","model":"gpt-4"}`
	id, err := entityDAO.Create(ctx, Entity{
		Type:         "PERSON",
		Name:         "Alice Smith",
		Confidence:   0.92,
		MetadataJSON: &metaJSON,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	ent, err := entityDAO.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if ent == nil {
		t.Fatal("entity not found after Create")
	}
	if ent.Confidence != 0.92 {
		t.Errorf("confidence = %f, want 0.92", ent.Confidence)
	}
	if ent.MetadataJSON == nil {
		t.Fatal("metadata_json is nil, expected non-nil")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(*ent.MetadataJSON), &parsed); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if parsed["source"] != "llm" {
		t.Errorf("metadata source = %v, want llm", parsed["source"])
	}
}

func TestEntityDAO_GetOrCreate_WithConfidenceAndMetadata(t *testing.T) {
	t.Parallel()

	db, cleanup, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entityDAO := NewEntityDAO(db)

	metaJSON := `{"key":"value"}`
	desc := "Test entity description"
	id, err := entityDAO.GetOrCreate(ctx, "Test Entity", "PERSON", "", 0.88, &metaJSON, &desc)
	if err != nil {
		t.Fatalf("GetOrCreate() error = %v", err)
	}

	ent, err := entityDAO.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if ent == nil {
		t.Fatal("entity not found after GetOrCreate")
	}
	if ent.Confidence != 0.88 {
		t.Errorf("confidence = %f, want 0.88", ent.Confidence)
	}
	if ent.MetadataJSON == nil || *ent.MetadataJSON != `{"key":"value"}` {
		t.Errorf("metadata_json = %v, want {\"key\":\"value\"}", ent.MetadataJSON)
	}

	// Second call should return same Predicate (existing entity).
	id2, err := entityDAO.GetOrCreate(ctx, "Test Entity", "PERSON", "", 0.50, nil, nil)
	if err != nil {
		t.Fatalf("GetOrCreate() second call error = %v", err)
	}
	if id2 != id {
		t.Errorf("second GetOrCreate returned different Predicate: %d vs %d", id2, id)
	}
}

func TestEntityDAO_CascadeDeleteEntityLinksOnEntityRemoval(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, cleanup := newFactDAOFixture(t)
	defer cleanup()

	// Enable foreign keys for cascade delete testing (SQLite disables FK by default).
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("enable foreign_keys: %v", err)
	}

	entityDAO := NewEntityDAO(db)
	linkDAO := NewEntityLinkDAO(db)

	// 1. Create entity A in domain hr
	aID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	if err != nil {
		t.Fatalf("create entity A: %v", err)
	}

	// 2. Create entity B in domain policy
	bID, err := entityDAO.Create(ctx, Entity{Type: "POLICY", Name: "PTO Policy", Domain: "policy"})
	if err != nil {
		t.Fatalf("create entity B: %v", err)
	}

	// 3. Create entity_link A→B (both directions)
	if _, err := linkDAO.Create(ctx, EntityLink{SubjectEntityID: aID, TargetEntityID: bID, RelationType: "same_entity", Method: "rule", Confidence: 0.9}); err != nil {
		t.Fatalf("create link A->B: %v", err)
	}
	if _, err := linkDAO.Create(ctx, EntityLink{SubjectEntityID: bID, TargetEntityID: aID, RelationType: "same_entity", Method: "rule", Confidence: 0.9}); err != nil {
		t.Fatalf("create link B->A: %v", err)
	}

	// 4. Create entity C without entity_sources (orphan)
	cID, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Charlie", Domain: "hr"})
	if err != nil {
		t.Fatalf("create entity C: %v", err)
	}

	// 5. Call DeleteOrphanedEntityIDs — should delete C, A, B (all orphans, A and B have links)
	deleted, err := entityDAO.DeleteOrphanedEntityIDs(ctx)
	if err != nil {
		t.Fatalf("DeleteOrphanedEntityIDs: %v", err)
	}
	if deleted != 3 {
		t.Errorf("deleted = %d, want 3 (A, B, C — all orphans)", deleted)
	}

	// 6. Verify: all entities deleted
	for i, id := range []int{aID, bID, cID} {
		e, err := entityDAO.GetByID(ctx, id)
		if err != nil {
			t.Fatalf("GetByID(entity %d): %v", i+1, err)
		}
		if e != nil {
			t.Errorf("entity %d should be deleted (orphan, links cascade-deleted)", i+1)
		}
	}

	// 7. Verify: entity_links cascade-deleted (should return empty)
	links, err := linkDAO.ListByEntity(ctx, aID)
	if err != nil {
		t.Fatalf("ListByEntity(A): %v", err)
	}
	if len(links) != 0 {
		t.Errorf("expected 0 entity_links for entity A after cascade delete, got %d", len(links))
	}
}

func TestEntityDAO_GetByNameFold(t *testing.T) {
	t.Parallel()

	db, cleanup, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entityDAO := NewEntityDAO(db)

	id, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "John Smith", Domain: "hr"})
	if err != nil {
		t.Fatalf("create entity: %v", err)
	}

	tests := []struct {
		name    string
		query   string
		domain  string
		wantID  int
		wantNil bool
	}{
		{
			name:   "exact case match",
			query:  "John Smith",
			domain: "hr",
			wantID: id,
		},
		{
			name:   "lowercase query",
			query:  "john smith",
			domain: "hr",
			wantID: id,
		},
		{
			name:   "uppercase query",
			query:  "JOHN SMITH",
			domain: "hr",
			wantID: id,
		},
		{
			name:    "wrong domain returns nil",
			query:   "John Smith",
			domain:  "policy",
			wantNil: true,
		},
		{
			name:    "nonexistent name returns nil",
			query:   "Nobody",
			domain:  "hr",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ent, err := entityDAO.GetByNameFold(ctx, tt.query, tt.domain)
			if err != nil {
				t.Fatalf("GetByNameFold(%q, %q) error = %v", tt.query, tt.domain, err)
			}
			if tt.wantNil {
				if ent != nil {
					t.Errorf("expected nil entity, got Predicate=%d", ent.ID)
				}
				return
			}
			if ent == nil {
				t.Fatal("expected entity, got nil")
			}
			if ent.ID != tt.wantID {
				t.Errorf("entity Predicate = %d, want %d", ent.ID, tt.wantID)
			}
		})
	}
}

func TestEntityDAO_ListByNameFold(t *testing.T) {
	t.Parallel()

	db, cleanup, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanup()

	ctx := context.Background()
	entityDAO := NewEntityDAO(db)

	// Create entities with the same name in different domains.
	id1, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Jane Doe", Domain: "hr"})
	if err != nil {
		t.Fatalf("create Jane/hr: %v", err)
	}
	id2, err := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Jane Doe", Domain: "policy"})
	if err != nil {
		t.Fatalf("create Jane/policy: %v", err)
	}

	tests := []struct {
		name      string
		query     string
		wantCount int
		wantIDs   []int
	}{
		{
			name:      "exact case match finds all domains",
			query:     "Jane Doe",
			wantCount: 2,
			wantIDs:   []int{id1, id2},
		},
		{
			name:      "lowercase query finds all domains",
			query:     "jane doe",
			wantCount: 2,
			wantIDs:   []int{id1, id2},
		},
		{
			name:      "uppercase query finds all domains",
			query:     "JANE DOE",
			wantCount: 2,
			wantIDs:   []int{id1, id2},
		},
		{
			name:      "nonexistent name returns empty list",
			query:     "Nobody",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ents, err := entityDAO.ListByNameFold(ctx, tt.query)
			if err != nil {
				t.Fatalf("ListByNameFold(%q) error = %v", tt.query, err)
			}
			if len(ents) != tt.wantCount {
				t.Errorf("got %d entities, want %d", len(ents), tt.wantCount)
			}
			if len(tt.wantIDs) > 0 {
				gotIDs := make([]int, len(ents))
				for i, e := range ents {
					gotIDs[i] = e.ID
				}
				// Verify order is deterministic (by id).
				for i := 1; i < len(gotIDs); i++ {
					if gotIDs[i] <= gotIDs[i-1] {
						t.Errorf("results not ordered by id: %v", gotIDs)
					}
				}
			}
		})
	}
}
