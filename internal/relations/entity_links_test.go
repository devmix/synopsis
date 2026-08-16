package relations_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/relations"
)

// insertEntity creates an entity in the database and returns its Predicate.
func insertEntity(t *testing.T, ctx context.Context, db dao.DBTX, entityType, name, dom string, desc *string) int {
	t.Helper()
	id, err := dao.NewEntityDAO(db).Create(ctx, dao.Entity{
		Type:        entityType,
		Name:        name,
		Domain:      dom,
		Description: desc,
	})
	if err != nil {
		t.Fatalf("create entity %q: %v", name, err)
	}
	return id
}

func TestBuildEntityLinks_NoConfig_Noop(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()
	result, err := relations.BuildEntityLinks(ctx, db, nil, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks(nil config) error = %v", err)
	}
	if result.LinksCreated != 0 || result.LinksSkipped != 0 || len(result.Errors) > 0 {
		t.Errorf("expected empty result, got %+v", result)
	}
}

func TestBuildEntityLinks_Expression(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	desc1 := "Go programming language"
	insertEntity(t, ctx, db, "technology", "Golang", "hr", &desc1)
	desc2 := "Go programming language from policy domain"
	insertEntity(t, ctx, db, "technology", "Golang", "policy", &desc2)

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{
				Name:         "hr-policy-same-name",
				Priority:     10,
				Where:        `A.domain == 'hr' && A.name == B.name`,
				RelationType: "same_entity",
			},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated != 1 {
		t.Errorf("expected 1 link created, got %d", result.LinksCreated)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	links, err := linkDAO.ListAll(ctx)
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(links) != 2 {
		t.Errorf("expected 2 bidirectional links, got %d", len(links))
	}
	for _, l := range links {
		if l.Method != "expression" {
			t.Errorf("expected method 'expression', got %q", l.Method)
		}
		if l.Evidence == nil || !strings.Contains(*l.Evidence, "expression:") {
			t.Errorf("expected evidence to contain 'expression:', got %v", l.Evidence)
		}
	}
}

func TestBuildEntityLinks_Expression_NoMatch(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	insertEntity(t, ctx, db, "technology", "Golang", "hr", nil)
	insertEntity(t, ctx, db, "technology", "Rust", "policy", nil) // different name → no match

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{
				Name:         "hr-policy-same-name",
				Priority:     10,
				Where:        `A.domain == 'hr' && A.name == B.name`,
				RelationType: "same_entity",
			},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated != 0 {
		t.Errorf("expected 0 links created (no match), got %d", result.LinksCreated)
	}
}

func TestBuildEntityLinks_Expression_DomainFilter(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	insertEntity(t, ctx, db, "technology", "Golang", "hr", nil)
	insertEntity(t, ctx, db, "technology", "Golang", "engineering", nil) // not policy → no match

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{
				Name:         "hr-policy-only",
				Priority:     10,
				Where:        `A.domain == 'hr' && B.domain == 'policy'`,
				RelationType: "same_entity",
			},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated != 0 {
		t.Errorf("expected 0 links created (domain filter), got %d", result.LinksCreated)
	}
}

func TestBuildEntityLinks_Expression_TypeFilter(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	insertEntity(t, ctx, db, "Person", "Jane Smith", "hr", nil)
	insertEntity(t, ctx, db, "Person", "Jane Smith", "policy", nil)
	insertEntity(t, ctx, db, "technology", "Golang", "hr", nil)
	insertEntity(t, ctx, db, "technology", "Golang", "policy", nil)

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{
				Name:         "hr-policy-person",
				Priority:     10,
				Where:        `A.domain == 'hr' && A.name == B.name && A.type == 'Person'`,
				RelationType: "same_entity",
			},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated != 1 {
		t.Errorf("expected 1 link created (Person only), got %d", result.LinksCreated)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	links, _ := linkDAO.ListAll(ctx)
	for _, l := range links {
		if l.Method != "expression" {
			t.Errorf("expected method 'expression', got %q", l.Method)
		}
	}
}

func TestBuildEntityLinks_Expression_RelationType(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	insertEntity(t, ctx, db, "technology", "Golang", "hr", nil)
	insertEntity(t, ctx, db, "technology", "Golang", "policy", nil)

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{
				Name:         "related-tech",
				Priority:     10,
				Where:        `A.domain == 'hr' && A.name == B.name`,
				RelationType: "related_to",
			},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated != 1 {
		t.Errorf("expected 1 link created, got %d", result.LinksCreated)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	links, _ := linkDAO.ListAll(ctx)
	for _, l := range links {
		if l.RelationType != "related_to" {
			t.Errorf("expected relation_type 'related_to', got %q", l.RelationType)
		}
	}
}

func TestBuildEntityLinks_Expression_EmptyExpressions(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	insertEntity(t, ctx, db, "technology", "Golang", "hr", nil)
	insertEntity(t, ctx, db, "technology", "Golang", "policy", nil)

	cfg := &config.CrossDomainLinksConfig{
		Methods:     []string{"expression"},
		Expressions: []config.LinkExpression{},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated != 0 {
		t.Errorf("expected 0 links created (no expressions), got %d", result.LinksCreated)
	}
}

func TestBuildEntityLinks_EqualsMethod_MultiWordOnly(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	// Single-word name — should NOT be linked.
	insertEntity(t, ctx, db, "person", "Alice", "hr", nil)
	insertEntity(t, ctx, db, "person", "Alice", "engineering", nil)

	// Multi-word name — SHOULD be linked.
	desc1 := "Senior engineer"
	insertEntity(t, ctx, db, "person", "John Smith", "hr", &desc1)
	desc2 := "Team lead"
	insertEntity(t, ctx, db, "person", "John Smith", "engineering", &desc2)

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"equals"},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	links, _ := linkDAO.ListAll(ctx)

	hasAlice := false
	hasJohnSmith := false
	for _, l := range links {
		subjEnt, _ := dao.NewEntityDAO(db).GetByID(ctx, l.SubjectEntityID)
		if subjEnt != nil && subjEnt.Name == "Alice" {
			hasAlice = true
		}
		if subjEnt != nil && subjEnt.Name == "John Smith" {
			hasJohnSmith = true
		}
	}

	if hasAlice {
		t.Error("single-word name 'Alice' should not be linked")
	}
	if !hasJohnSmith {
		t.Error("multi-word name 'John Smith' should be linked")
	}
	if result.LinksCreated != 1 {
		t.Errorf("expected 1 link created (bidirectional), got %d", result.LinksCreated)
	}
}

func TestBuildEntityLinks_EqualsMethod_MinWords(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	insertEntity(t, ctx, db, "person", "John Smith", "hr", nil)
	insertEntity(t, ctx, db, "person", "John Smith", "engineering", nil)

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"equals"},
		Equals:  &config.EqualsConfig{MinWords: 3},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated != 0 {
		t.Errorf("expected 0 links created (min_words=3), got %d", result.LinksCreated)
	}
}

func TestBuildEntityLinks_LLMMethod_Mock(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	desc1 := "Go programming language"
	insertEntity(t, ctx, db, "technology", "Golang", "tech", &desc1)
	desc2 := "Go programming language from engineering domain"
	insertEntity(t, ctx, db, "technology", "Golang", "engineering", &desc2)

	mockLinker := &relations.MockCrossDomainLinker{
		Decide: func(a, b relations.EntityCandidate) (*relations.LinkDecision, error) {
			return &relations.LinkDecision{
				SameEntity: true,
				Confidence: 0.95,
				Reasoning:  "Both refer to the Go programming language",
			}, nil
		},
	}

	cfg := &config.CrossDomainLinksConfig{
		Methods:                []string{"llm"},
		LLmConfidenceThreshold: 0.7,
		BatchSize:              5,
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, mockLinker)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated != 1 {
		t.Errorf("expected 1 link created, got %d", result.LinksCreated)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	links, _ := linkDAO.ListAll(ctx)
	if len(links) != 2 {
		t.Errorf("expected 2 bidirectional links, got %d", len(links))
	}
	for _, l := range links {
		if l.Method != "llm" {
			t.Errorf("expected method 'llm', got %q", l.Method)
		}
	}
}

func TestBuildEntityLinks_LLMMethod_ConfidenceBelowThreshold(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	insertEntity(t, ctx, db, "technology", "Golang", "tech", nil)
	insertEntity(t, ctx, db, "technology", "Golang", "engineering", nil)

	mockLinker := &relations.MockCrossDomainLinker{
		Decide: func(a, b relations.EntityCandidate) (*relations.LinkDecision, error) {
			return &relations.LinkDecision{
				SameEntity: true,
				Confidence: 0.3,
				Reasoning:  "Not confident",
			}, nil
		},
	}

	cfg := &config.CrossDomainLinksConfig{
		Methods:                []string{"llm"},
		LLmConfidenceThreshold: 0.7,
		BatchSize:              5,
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, mockLinker)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated != 0 {
		t.Errorf("expected 0 links created (below threshold), got %d", result.LinksCreated)
	}
	if result.LinksSkipped != 1 {
		t.Errorf("expected 1 link skipped, got %d", result.LinksSkipped)
	}
}

func TestBuildEntityLinks_LLMMethod_NilLinker(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	insertEntity(t, ctx, db, "technology", "Golang", "tech", nil)
	insertEntity(t, ctx, db, "technology", "Golang", "engineering", nil)

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"llm"},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated != 0 {
		t.Errorf("expected 0 links created (nil linker), got %d", result.LinksCreated)
	}
	if len(result.Errors) == 0 {
		t.Error("expected error about nil linker")
	} else if !strings.Contains(result.Errors[0], "llm method requires a linker") {
		t.Errorf("unexpected error message: %s", result.Errors[0])
	}
}

func TestBuildEntityLinks_MethodOrder(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	desc1 := "Go programming language"
	insertEntity(t, ctx, db, "technology", "Golang", "tech", &desc1)
	desc2 := "Go programming language from engineering domain"
	insertEntity(t, ctx, db, "technology", "Golang", "engineering", &desc2)

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression", "equals"},
		Expressions: []config.LinkExpression{
			{
				Name:         "tech-eng-same-name",
				Priority:     10,
				Where:        `(A.domain == 'tech' && B.domain == 'engineering' && A.name == B.name) || (A.domain == 'engineering' && B.domain == 'tech' && A.name == B.name)`,
				RelationType: "same_entity",
			},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated < 1 {
		t.Errorf("expected at least 1 link created, got %d", result.LinksCreated)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	links, _ := linkDAO.ListAll(ctx)
	if len(links) != 2 {
		t.Errorf("expected 2 links total (idempotent), got %d", len(links))
	}
}

func TestBuildEntityLinks_ExpressionNotOverwrittenByEquals(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	// Use multi-word name so equals method also processes the pair.
	desc1 := "Go programming language"
	insertEntity(t, ctx, db, "technology", "Go Lang", "tech", &desc1)
	desc2 := "Go programming language from engineering domain"
	insertEntity(t, ctx, db, "technology", "Go Lang", "engineering", &desc2)

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression", "equals"},
		Expressions: []config.LinkExpression{
			{
				Name:         "tech-eng-same-name",
				Priority:     10,
				Where:        `(A.domain == 'tech' && B.domain == 'engineering' && A.name == B.name) || (A.domain == 'engineering' && B.domain == 'tech' && A.name == B.name)`,
				RelationType: "same_entity",
			},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	links, _ := linkDAO.ListAll(ctx)

	// Expression creates the links first; equals tries to create same pair but gets duplicates.
	// All existing links should have method "expression" (not overwritten).
	for _, l := range links {
		if l.Method != "expression" {
			t.Errorf("expected method 'expression' (not overwritten by equals), got %q", l.Method)
		}
	}

	// Expression created 1, equals skipped 1 (duplicate).
	if result.LinksCreated != 1 {
		t.Errorf("expected 1 link created (by expression only), got %d", result.LinksCreated)
	}
	if result.LinksSkipped != 1 {
		t.Errorf("expected 1 link skipped (equals duplicate), got %d", result.LinksSkipped)
	}
}

func TestBuildEntityLinks_Idempotent(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	desc1 := "Go programming language"
	insertEntity(t, ctx, db, "technology", "Golang", "tech", &desc1)
	desc2 := "Go programming language from engineering domain"
	insertEntity(t, ctx, db, "technology", "Golang", "engineering", &desc2)

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{
				Name:         "tech-eng-same-name",
				Priority:     10,
				Where:        `(A.domain == 'tech' && B.domain == 'engineering' && A.name == B.name) || (A.domain == 'engineering' && B.domain == 'tech' && A.name == B.name)`,
				RelationType: "same_entity",
			},
		},
	}

	result1, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("first BuildEntityLinks error = %v", err)
	}

	result2, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("second BuildEntityLinks error = %v", err)
	}

	// First run creates 1 link; second run skips it (duplicate).
	if result1.LinksCreated != 1 {
		t.Errorf("first run: expected 1 created, got %d", result1.LinksCreated)
	}
	if result2.LinksCreated != 0 {
		t.Errorf("second run: expected 0 created, got %d", result2.LinksCreated)
	}
	if result2.LinksSkipped != 1 {
		t.Errorf("second run: expected 1 skipped, got %d", result2.LinksSkipped)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	links, _ := linkDAO.ListAll(ctx)
	if len(links) != 2 {
		t.Errorf("expected 2 links after two calls (idempotent), got %d", len(links))
	}
}

func TestBuildEntityLinks_Bidirectional(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	idA := insertEntity(t, ctx, db, "technology", "Golang", "tech", nil)
	insertEntity(t, ctx, db, "technology", "Golang", "engineering", nil)

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{
				Name:         "tech-eng-same-name",
				Priority:     10,
				Where:        `(A.domain == 'tech' && B.domain == 'engineering' && A.name == B.name) || (A.domain == 'engineering' && B.domain == 'tech' && A.name == B.name)`,
				RelationType: "same_entity",
			},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated != 1 {
		t.Errorf("expected 1 link created, got %d", result.LinksCreated)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	links, _ := linkDAO.ListAll(ctx)

	foundAtoB := false
	foundBtoA := false
	for _, l := range links {
		if l.SubjectEntityID == idA {
			foundAtoB = true
		}
		if l.TargetEntityID == idA {
			foundBtoA = true
		}
	}

	if !foundAtoB {
		t.Error("expected link from A to B")
	}
	if !foundBtoA {
		t.Error("expected link from B to A")
	}
}

func TestBuildEntityLinks_Provenance(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	insertEntity(t, ctx, db, "technology", "Golang", "tech", nil)
	insertEntity(t, ctx, db, "technology", "Golang", "engineering", nil)

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{
				Name:         "tech-eng-same-name",
				Priority:     10,
				Where:        `(A.domain == 'tech' && B.domain == 'engineering' && A.name == B.name) || (A.domain == 'engineering' && B.domain == 'tech' && A.name == B.name)`,
				RelationType: "same_entity",
			},
		},
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, nil)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	if result.LinksCreated != 1 {
		t.Errorf("expected 1 link created, got %d", result.LinksCreated)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	links, _ := linkDAO.ListAll(ctx)

	for _, l := range links {
		if l.Method != "expression" {
			t.Errorf("expected method 'expression', got %q", l.Method)
		}
		if l.Confidence != 1.0 {
			t.Errorf("expected confidence 1.0 (expression), got %f", l.Confidence)
		}
		if l.Evidence == nil || !strings.Contains(*l.Evidence, "expression:") {
			t.Errorf("expected evidence to contain 'expression:', got %v", l.Evidence)
		}
	}
}

func TestBuildEntityLinks_LLMPairErrorDoesNotStopOthers(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	insertEntity(t, ctx, db, "technology", "Golang", "tech", nil)
	insertEntity(t, ctx, db, "technology", "Golang", "engineering", nil)
	insertEntity(t, ctx, db, "person", "John Smith", "hr", nil)
	insertEntity(t, ctx, db, "person", "John Smith", "engineering", nil)

	callCount := 0
	mockLinker := &relations.MockCrossDomainLinker{
		Decide: func(a, b relations.EntityCandidate) (*relations.LinkDecision, error) {
			callCount++
			if callCount == 1 {
				return nil, fmt.Errorf("simulated LLM error") // first pair fails
			}
			return &relations.LinkDecision{
				SameEntity: true,
				Confidence: 0.95,
				Reasoning:  "Match",
			}, nil
		},
	}

	cfg := &config.CrossDomainLinksConfig{
		Methods:                []string{"llm"},
		LLmConfidenceThreshold: 0.7,
		BatchSize:              10,
	}

	result, err := relations.BuildEntityLinks(ctx, db, cfg, mockLinker)
	if err != nil {
		t.Fatalf("BuildEntityLinks error = %v", err)
	}
	// First pair fails (error), second succeeds.
	if result.LinksCreated != 1 {
		t.Errorf("expected 1 link created (second pair succeeded), got %d", result.LinksCreated)
	}
	if len(result.Errors) == 0 {
		t.Error("expected error about failed LLM pair")
	}
}

// insertEntityWithTimestamp creates an entity with a specific created_at timestamp.
func insertEntityWithTimestamp(t *testing.T, ctx context.Context, db dao.DBTX, entityType, name, dom string, ts string) int {
	t.Helper()
	result, err := db.ExecContext(ctx,
		`INSERT INTO entities (type, name, domain, confidence, description, metadata_json, created_at) VALUES (?, ?, ?, 0.9, NULL, NULL, ?)`,
		entityType, name, dom, ts,
	)
	if err != nil {
		t.Fatalf("create entity %q: %v", name, err)
	}
	id, _ := result.LastInsertId()
	return int(id)
}

func TestBuildEntityLinksIncremental_FullRebuild(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	insertEntityWithTimestamp(t, ctx, db, "technology", "Golang", "hr", "2024-01-01 10:00:00")
	insertEntityWithTimestamp(t, ctx, db, "technology", "Golang", "it", "2024-01-01 10:00:00")

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{Name: "same-tech", Priority: 10, Where: `A.name == B.name`, RelationType: "same_entity"},
		},
	}

	// Full rebuild (empty since).
	result, err := relations.BuildEntityLinksIncremental(ctx, db, cfg, nil, "")
	if err != nil {
		t.Fatalf("BuildEntityLinksIncremental error = %v", err)
	}
	if result.LinksCreated != 1 {
		t.Errorf("expected 1 link created (full rebuild), got %d", result.LinksCreated)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	links, _ := linkDAO.ListAll(ctx)
	if len(links) != 2 {
		t.Errorf("expected 2 bidirectional links, got %d", len(links))
	}
}

func TestBuildEntityLinksIncremental_OnlyChanged(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	// Old entities (before last linking run).
	insertEntityWithTimestamp(t, ctx, db, "technology", "Python", "hr", "2024-01-01 10:00:00")
	insertEntityWithTimestamp(t, ctx, db, "technology", "Python", "it", "2024-01-01 10:00:00")

	// New entities (after last linking run).
	insertEntityWithTimestamp(t, ctx, db, "technology", "Golang", "hr", "2024-02-01 10:00:00")
	insertEntityWithTimestamp(t, ctx, db, "technology", "Golang", "it", "2024-02-01 10:00:00")

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{Name: "same-tech", Priority: 10, Where: `A.name == B.name`, RelationType: "same_entity"},
		},
	}

	// Incremental linking — only process entities created after Jan 15.
	result, err := relations.BuildEntityLinksIncremental(ctx, db, cfg, nil, "2024-01-15")
	if err != nil {
		t.Fatalf("BuildEntityLinksIncremental error = %v", err)
	}

	// Only Golang pair should be linked (Python is before the since timestamp).
	if result.LinksCreated < 1 {
		t.Errorf("expected at least 1 link created for new entities, got %d", result.LinksCreated)
	}
	linkDAO := dao.NewEntityLinkDAO(db)
	links, _ := linkDAO.ListAll(ctx)

	hasGolang := false
	hasPython := false
	for _, l := range links {
		subjEnt, _ := dao.NewEntityDAO(db).GetByID(ctx, l.SubjectEntityID)
		if subjEnt != nil && subjEnt.Name == "Golang" {
			hasGolang = true
		}
		if subjEnt != nil && subjEnt.Name == "Python" {
			hasPython = true
		}
	}

	if !hasGolang {
		t.Error("expected Golang links (new entity)")
	}
	if hasPython {
		t.Error("should not have Python links (old entity, before since timestamp)")
	}
}

func TestBuildEntityLinksIncremental_NoNewEntities(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	insertEntityWithTimestamp(t, ctx, db, "technology", "Golang", "hr", "2024-01-01 10:00:00")
	insertEntityWithTimestamp(t, ctx, db, "technology", "Golang", "it", "2024-01-01 10:00:00")

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{Name: "same-tech", Priority: 10, Where: `A.name == B.name`, RelationType: "same_entity"},
		},
	}

	// Incremental linking — since is after all entity creation times.
	result, err := relations.BuildEntityLinksIncremental(ctx, db, cfg, nil, "2024-03-01")
	if err != nil {
		t.Fatalf("BuildEntityLinksIncremental error = %v", err)
	}

	if result.LinksCreated != 0 {
		t.Errorf("expected 0 links created (no new entities), got %d", result.LinksCreated)
	}
}

func TestBuildEntityLinksIncremental_DeletesOldLinks(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	// Create two pairs of entities at different times.
	insertEntityWithTimestamp(t, ctx, db, "technology", "Python", "hr", "2024-01-01 10:00:00")
	insertEntityWithTimestamp(t, ctx, db, "technology", "Python", "it", "2024-01-01 10:00:00")

	// Run full linking — Python pair gets linked.
	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{Name: "same-tech", Priority: 10, Where: `A.name == B.name`, RelationType: "same_entity"},
		},
	}

	result1, err := relations.BuildEntityLinksIncremental(ctx, db, cfg, nil, "")
	if err != nil {
		t.Fatalf("first BuildEntityLinksIncremental error = %v", err)
	}
	if result1.LinksCreated != 1 {
		t.Errorf("expected 1 link created in first run, got %d", result1.LinksCreated)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	countBefore, _ := linkDAO.Count(ctx)
	if countBefore < 2 {
		t.Fatalf("expected at least 2 links after full rebuild, got %d", countBefore)
	}

	// Now add a new pair (Golang) and run incremental linking.
	insertEntityWithTimestamp(t, ctx, db, "technology", "Golang", "hr", "2024-02-01 10:00:00")
	insertEntityWithTimestamp(t, ctx, db, "technology", "Golang", "it", "2024-02-01 10:00:00")

	// Incremental run — only Golang entities are after the since timestamp.
	result2, err := relations.BuildEntityLinksIncremental(ctx, db, cfg, nil, "")
	if err != nil {
		t.Fatalf("second BuildEntityLinksIncremental error = %v", err)
	}

	countAfter, _ := linkDAO.Count(ctx)
	// Python links should remain + Golang links added.
	if countAfter <= countBefore {
		t.Errorf("expected more links after incremental run, got %d (before: %d)", countAfter, countBefore)
	}
	if result2.LinksCreated == 0 {
		t.Error("expected some links to be created in incremental run")
	}

	// Verify Python links still exist.
	hasPython := false
	links, _ := linkDAO.ListAll(ctx)
	for _, l := range links {
		subjEnt, _ := dao.NewEntityDAO(db).GetByID(ctx, l.SubjectEntityID)
		if subjEnt != nil && subjEnt.Name == "Python" {
			hasPython = true
			break
		}
	}
	if !hasPython {
		t.Error("expected Python links to remain after incremental run")
	}
}

func TestBuildEntityLinksIncremental_MixedChangedAndStable(t *testing.T) {
	t.Parallel()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	defer cleanFn()

	ctx := context.Background()

	// Stable entities (before since).
	insertEntityWithTimestamp(t, ctx, db, "technology", "Python", "hr", "2024-01-01 10:00:00")
	insertEntityWithTimestamp(t, ctx, db, "technology", "Python", "it", "2024-01-01 10:00:00")

	// Changed entities (after since).
	insertEntityWithTimestamp(t, ctx, db, "technology", "Golang", "hr", "2024-02-01 10:00:00")
	insertEntityWithTimestamp(t, ctx, db, "technology", "Golang", "it", "2024-02-01 10:00:00")

	cfg := &config.CrossDomainLinksConfig{
		Methods: []string{"expression"},
		Expressions: []config.LinkExpression{
			{Name: "same-tech", Priority: 10, Where: `A.name == B.name`, RelationType: "same_entity"},
		},
	}

	// Incremental linking — only process entities created after Jan 15.
	result, err := relations.BuildEntityLinksIncremental(ctx, db, cfg, nil, "2024-01-15")
	if err != nil {
		t.Fatalf("BuildEntityLinksIncremental error = %v", err)
	}

	linkDAO := dao.NewEntityLinkDAO(db)
	links, _ := linkDAO.ListAll(ctx)

	// Only Golang should be linked; Python entities are before the since timestamp.
	hasGolang := false
	hasPython := false
	for _, l := range links {
		subjEnt, _ := dao.NewEntityDAO(db).GetByID(ctx, l.SubjectEntityID)
		if subjEnt != nil && subjEnt.Name == "Golang" {
			hasGolang = true
		}
		if subjEnt != nil && subjEnt.Name == "Python" {
			hasPython = true
		}
	}

	if !hasGolang {
		t.Error("expected Golang links (changed entity)")
	}
	if hasPython {
		t.Error("should not have Python links (stable entity, before since timestamp)")
	}
	if result.LinksCreated != 1 {
		t.Errorf("expected 1 link created (Golang only), got %d", result.LinksCreated)
	}
}

func TestKVKeyLastLinkingRun(t *testing.T) {
	t.Parallel()

	// Verify the constant is exported and usable.
	if relations.KVKeyLastLinkingRun != "last_linking_run" {
		t.Errorf("KVKeyLastLinkingRun = %q, want %q", relations.KVKeyLastLinkingRun, "last_linking_run")
	}
}
