package dao

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func newEntityLinkDAOFixture(t *testing.T) (*sql.DB, func()) {
	t.Helper()
	db, cleanup, err := TestDB(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		cleanup()
		t.Fatalf("create test db: %v", err)
	}
	return db, cleanup
}

func createTestEntity(t *testing.T, dao *EntityDAO, name string, entityType string, domain string) int {
	t.Helper()
	id, err := dao.Create(context.Background(), Entity{Type: entityType, Name: name, Domain: domain})
	if err != nil {
		t.Fatalf("create entity %q: %v", name, err)
	}
	return id
}

func TestEntityLinkDAO(t *testing.T) {
	t.Parallel()

	db, cleanup := newEntityLinkDAOFixture(t)
	defer cleanup()

	ctx := context.Background()
	entityDAO := NewEntityDAO(db)
	linkDAO := NewEntityLinkDAO(db)

	// Create test entities.
	subjID := createTestEntity(t, entityDAO, "Alice", "PERSON", "hr")
	targetID := createTestEntity(t, entityDAO, "Bob", "PERSON", "hr")

	t.Run("Create", func(t *testing.T) {
		created, err := linkDAO.Create(ctx, EntityLink{
			SubjectEntityID: subjID,
			TargetEntityID:  targetID,
			RelationType:    "same_entity",
			Method:          "rule",
			Confidence:      0.95,
		})
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if !created {
			t.Error("expected Create to return true for new link")
		}

		links, err := linkDAO.ListByEntity(ctx, subjID)
		if err != nil {
			t.Fatalf("ListByEntity() error = %v", err)
		}
		if len(links) != 1 {
			t.Errorf("expected 1 link, got %d", len(links))
		}
	})

	t.Run("CreateDuplicate_ReturnsFalse", func(t *testing.T) {
		link := EntityLink{
			SubjectEntityID: subjID,
			TargetEntityID:  targetID,
			RelationType:    "dup_test",
			Method:          "rule",
			Confidence:      0.95,
		}

		// First insert should succeed (new relation_type).
		created1, err := linkDAO.Create(ctx, link)
		if err != nil {
			t.Fatalf("first Create() error = %v", err)
		}
		if !created1 {
			t.Error("expected first Create to return true")
		}

		// Second insert (duplicate) should return false without error.
		created2, err := linkDAO.Create(ctx, link)
		if err != nil {
			t.Fatalf("second Create() error = %v", err)
		}
		if created2 {
			t.Error("expected second Create to return false for duplicate")
		}

		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM entity_links WHERE subject_entity_id = ? AND target_entity_id = ? AND relation_type = ?", subjID, targetID, "dup_test").Scan(&count); err != nil {
			t.Fatalf("count links: %v", err)
		}
		if count != 1 {
			t.Errorf("expected exactly 1 link row, got %d", count)
		}
	})

	t.Run("CreateSelfLink_ReturnsError", func(t *testing.T) {
		_, err := linkDAO.Create(ctx, EntityLink{
			SubjectEntityID: subjID,
			TargetEntityID:  subjID, // same entity — CHECK violation
			RelationType:    "same_entity",
			Method:          "rule",
			Confidence:      0.95,
		})
		if err == nil {
			t.Fatal("expected error for self-link (subject == target)")
		}
	})

	t.Run("CreateIdempotent", func(t *testing.T) {
		link := EntityLink{
			SubjectEntityID: subjID,
			TargetEntityID:  targetID,
			RelationType:    "related_to",
			Method:          "rule",
			Confidence:      0.8,
		}

		if _, err := linkDAO.Create(ctx, link); err != nil {
			t.Fatalf("first Create() error = %v", err)
		}
		if _, err := linkDAO.Create(ctx, link); err != nil {
			t.Fatalf("second Create() should return nil for duplicate, got: %v", err)
		}

		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM entity_links WHERE subject_entity_id = ? AND target_entity_id = ? AND relation_type = ?", subjID, targetID, "related_to").Scan(&count); err != nil {
			t.Fatalf("count links: %v", err)
		}
		if count != 1 {
			t.Errorf("expected exactly 1 row for (subj, target, related_to), got %d", count)
		}
	})

	t.Run("ListByEntity_Outgoing", func(t *testing.T) {
		link := EntityLink{
			SubjectEntityID: subjID,
			TargetEntityID:  targetID,
			RelationType:    "manages",
			Method:          "rule",
			Confidence:      0.9,
		}
		if _, err := linkDAO.Create(ctx, link); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		links, err := linkDAO.ListByEntity(ctx, subjID)
		if err != nil {
			t.Fatalf("ListByEntity() error = %v", err)
		}

		foundOutgoing := false
		for _, l := range links {
			if l.SubjectEntityID == subjID && l.TargetEntityID == targetID && l.RelationType == "manages" {
				foundOutgoing = true
				break
			}
		}
		if !foundOutgoing {
			t.Error("expected outgoing link (subj→target) in ListByEntity result")
		}
	})

	t.Run("ListByEntity_Incoming", func(t *testing.T) {
		link := EntityLink{
			SubjectEntityID: targetID,
			TargetEntityID:  subjID,
			RelationType:    "reports_to",
			Method:          "rule",
			Confidence:      0.9,
		}
		if _, err := linkDAO.Create(ctx, link); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		links, err := linkDAO.ListByEntity(ctx, subjID)
		if err != nil {
			t.Fatalf("ListByEntity() error = %v", err)
		}

		foundIncoming := false
		for _, l := range links {
			if l.SubjectEntityID == targetID && l.TargetEntityID == subjID && l.RelationType == "reports_to" {
				foundIncoming = true
				break
			}
		}
		if !foundIncoming {
			t.Error("expected incoming link (target→subj) in ListByEntity result")
		}
	})

	t.Run("ListByMethod", func(t *testing.T) {
		link := EntityLink{
			SubjectEntityID: subjID,
			TargetEntityID:  targetID,
			RelationType:    "llm_matched",
			Method:          "llm",
			Confidence:      0.75,
		}
		if _, err := linkDAO.Create(ctx, link); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		llmLinks, err := linkDAO.ListByMethod(ctx, "llm")
		if err != nil {
			t.Fatalf("ListByMethod(llm) error = %v", err)
		}
		foundLLM := false
		for _, l := range llmLinks {
			if l.Method == "llm" && l.SubjectEntityID == subjID && l.TargetEntityID == targetID {
				foundLLM = true
				break
			}
		}
		if !foundLLM {
			t.Error("expected LLM link in ListByMethod result")
		}

		ruleLinks, err := linkDAO.ListByMethod(ctx, "rule")
		if err != nil {
			t.Fatalf("ListByMethod(rule) error = %v", err)
		}
		for _, l := range ruleLinks {
			if l.Method == "llm" {
				t.Error("rule method query returned an llm link")
			}
		}
	})

	t.Run("Delete", func(t *testing.T) {
		link1 := EntityLink{
			SubjectEntityID: subjID,
			TargetEntityID:  targetID,
			RelationType:    "deletable",
			Method:          "rule",
			Confidence:      0.95,
		}
		link2 := EntityLink{
			SubjectEntityID: targetID,
			TargetEntityID:  subjID,
			RelationType:    "deletable",
			Method:          "rule",
			Confidence:      0.95,
		}
		if _, err := linkDAO.Create(ctx, link1); err != nil {
			t.Fatalf("Create forward: %v", err)
		}
		if _, err := linkDAO.Create(ctx, link2); err != nil {
			t.Fatalf("Create reverse: %v", err)
		}

		if err := linkDAO.Delete(ctx, subjID, targetID); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM entity_links WHERE (subject_entity_id = ? AND target_entity_id = ?) OR (subject_entity_id = ? AND target_entity_id = ?)`, subjID, targetID, targetID, subjID).Scan(&count); err != nil {
			t.Fatalf("count: %v", err)
		}
		if count != 0 {
			t.Errorf("both directions should be deleted, got %d remaining", count)
		}
	})

	t.Run("CascadeDelete", func(t *testing.T) {
		link := EntityLink{
			SubjectEntityID: subjID,
			TargetEntityID:  targetID,
			RelationType:    "cascade_test",
			Method:          "rule",
			Confidence:      0.95,
		}
		if _, err := linkDAO.Create(ctx, link); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		// Enable FK enforcement for cascade test.
		if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
			t.Fatalf("enable foreign keys: %v", err)
		}

		if err := entityDAO.Delete(ctx, subjID); err != nil {
			t.Fatalf("Delete entity: %v", err)
		}

		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM entity_links WHERE subject_entity_id = ?", subjID).Scan(&count); err != nil {
			t.Fatalf("count links after cascade: %v", err)
		}
		if count != 0 {
			t.Errorf("links should be cascaded, got %d remaining", count)
		}

		// Disable FK enforcement for subsequent tests.
		if _, err := db.Exec("PRAGMA foreign_keys=OFF"); err != nil {
			t.Fatalf("disable foreign keys: %v", err)
		}
	})

	t.Run("Provenance", func(t *testing.T) {
		evidence := "Rule-based matching on name similarity"
		link := EntityLink{
			SubjectEntityID: subjID,
			TargetEntityID:  targetID,
			RelationType:    "provenance_test",
			Method:          "equals",
			Confidence:      0.87,
			Evidence:        &evidence,
		}
		if _, err := linkDAO.Create(ctx, link); err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		links, err := linkDAO.ListByEntity(ctx, subjID)
		if err != nil {
			t.Fatalf("ListByEntity() error = %v", err)
		}

		var found bool
		for _, l := range links {
			if l.SubjectEntityID == subjID && l.TargetEntityID == targetID && l.RelationType == "provenance_test" && l.Method == "equals" {
				found = true
				if l.Confidence != 0.87 {
					t.Errorf("confidence = %f, want 0.87", l.Confidence)
				}
				if l.Evidence == nil || *l.Evidence != evidence {
					t.Errorf("evidence = %v, want %q", l.Evidence, evidence)
				}
				break
			}
		}
		if !found {
			t.Error("expected link with provenance data in ListByEntity result")
		}
	})
}
