package dao

import (
	"context"
	"path/filepath"
	"testing"
)

func TestEntityDAO_GetByIDs(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entityDAO := NewEntityDAO(db)

	e1, _ := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	e2, _ := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})
	e3, _ := entityDAO.Create(ctx, Entity{Type: "LOCATION", Name: "New York", Domain: "geo"})

	tests := []struct {
		name    string
		ids     []int
		wantLen int
	}{
		{"empty ids returns empty map", []int{}, 0},
		{"nil ids returns empty map", nil, 0},
		{"single valid id returns one entity", []int{e1}, 1},
		{"multiple valid ids return all entities", []int{e1, e2, e3}, 3},
		{"nonexistent id returns empty map", []int{99999}, 0},
		{"mixed valid and nonexistent ids return only existing entities", []int{e1, 99999, e3}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			entities, err := entityDAO.GetByIDs(ctx, tt.ids)
			if err != nil {
				t.Fatalf("GetByIDs() error = %v", err)
			}

			if len(entities) != tt.wantLen {
				t.Errorf("GetByIDs() returned %d entities, want %d", len(entities), tt.wantLen)
			}
		})
	}
}

func TestEntityDAO_GetByIDs_ReturnsCorrectEntities(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	ctx := context.Background()
	entityDAO := NewEntityDAO(db)

	e1, _ := entityDAO.Create(ctx, Entity{Type: "PERSON", Name: "Alice", Domain: "hr"})
	e2, _ := entityDAO.Create(ctx, Entity{Type: "ORGANIZATION", Name: "Acme Corp", Domain: "hr"})

	entities, err := entityDAO.GetByIDs(ctx, []int{e1, e2})
	if err != nil {
		t.Fatalf("GetByIDs() error = %v", err)
	}

	if len(entities) != 2 {
		t.Fatalf("expected 2 entities, got %d", len(entities))
	}

	ent1, ok := entities[e1]
	if !ok {
		t.Fatalf("entity %d not found in results", e1)
	}
	if ent1.Name != "Alice" {
		t.Errorf("entity %d name = %q, want %q", e1, ent1.Name, "Alice")
	}
	if ent1.Type != "PERSON" {
		t.Errorf("entity %d type = %q, want %q", e1, ent1.Type, "PERSON")
	}

	ent2, ok := entities[e2]
	if !ok {
		t.Fatalf("entity %d not found in results", e2)
	}
	if ent2.Name != "Acme Corp" {
		t.Errorf("entity %d name = %q, want %q", e2, ent2.Name, "Acme Corp")
	}
	if ent2.Type != "ORGANIZATION" {
		t.Errorf("entity %d type = %q, want %q", e2, ent2.Type, "ORGANIZATION")
	}
}
