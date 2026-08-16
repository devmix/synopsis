package dao

import (
	"context"
	"path/filepath"
	"testing"
)

func TestFactDAO_GetByIDs(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entityDAO := NewEntityDAO(db)
	factDAO := NewFactDAO(db)

	subjID, _ := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	objID, _ := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	fact1ID, _ := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID, Predicate: "works_at", ObjectEntityID: objID, Domain: "hr",
	})
	fact2ID, _ := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID, Predicate: "lives_in", ObjectEntityID: 1, Domain: "hr",
	})

	tests := []struct {
		name    string
		ids     []int
		wantLen int
	}{
		{"empty ids returns empty map", []int{}, 0},
		{"nil ids returns empty map", nil, 0},
		{"single valid id returns one fact", []int{fact1ID}, 1},
		{"multiple valid ids return all facts", []int{fact1ID, fact2ID}, 2},
		{"nonexistent id returns empty map", []int{99999}, 0},
		{"mixed valid and nonexistent ids return only existing facts", []int{fact1ID, 99999, fact2ID}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			facts, err := factDAO.GetByIDs(ctx, tt.ids)
			if err != nil {
				t.Fatalf("GetByIDs() error = %v", err)
			}

			if len(facts) != tt.wantLen {
				t.Errorf("GetByIDs() returned %d facts, want %d", len(facts), tt.wantLen)
			}
		})
	}
}

func TestFactDAO_GetByIDs_ReturnsCorrectFacts(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entityDAO := NewEntityDAO(db)
	factDAO := NewFactDAO(db)

	subjID, _ := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	objID, _ := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	fact1ID, _ := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID, Predicate: "works_at", ObjectEntityID: objID, Domain: "hr",
	})
	fact2ID, _ := factDAO.CreateOrIgnore(ctx, Fact{
		SubjectEntityID: subjID, Predicate: "lives_in", ObjectEntityID: 1, Domain: "hr",
	})

	facts, err := factDAO.GetByIDs(ctx, []int{fact1ID, fact2ID})
	if err != nil {
		t.Fatalf("GetByIDs() error = %v", err)
	}

	if len(facts) != 2 {
		t.Fatalf("expected 2 facts, got %d", len(facts))
	}

	f1, ok := facts[fact1ID]
	if !ok {
		t.Fatalf("fact %d not found in results", fact1ID)
	}
	if f1.Predicate != "works_at" {
		t.Errorf("fact %d predicate = %q, want %q", fact1ID, f1.Predicate, "works_at")
	}

	f2, ok := facts[fact2ID]
	if !ok {
		t.Fatalf("fact %d not found in results", fact2ID)
	}
	if f2.Predicate != "lives_in" {
		t.Errorf("fact %d predicate = %q, want %q", fact2ID, f2.Predicate, "lives_in")
	}
}
