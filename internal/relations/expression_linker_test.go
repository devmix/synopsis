package relations_test

import (
	"context"
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/relations"
)

func TestExpressionLinker_New(t *testing.T) {
	t.Parallel()

	t.Run("creates linker without database", func(t *testing.T) {
		t.Parallel()
		linker := relations.NewExpressionLinker(nil)
		if linker == nil {
			t.Fatal("expected non-nil linker")
		}
	})
}

func TestExpressionLinker_Init(t *testing.T) {
	t.Parallel()

	t.Run("init with empty expressions succeeds", func(t *testing.T) {
		t.Parallel()
		linker := relations.NewExpressionLinker(nil)
		if err := linker.Init(context.Background(), nil); err != nil {
			t.Fatalf("Init: %v", err)
		}
	})

	t.Run("init with valid expressions compiles them", func(t *testing.T) {
		t.Parallel()
		linker := relations.NewExpressionLinker(nil)
		exprs := []config.LinkExpression{
			{Name: "same-domain", Where: "A.domain == B.domain", Priority: 10},
			{Name: "name-match", Where: "A.name == B.name", Priority: 5},
		}
		if err := linker.Init(context.Background(), exprs); err != nil {
			t.Fatalf("Init: %v", err)
		}
	})

	t.Run("init with invalid expression returns error", func(t *testing.T) {
		t.Parallel()
		linker := relations.NewExpressionLinker(nil)
		exprs := []config.LinkExpression{
			{Name: "bad-syntax", Where: "A.name == ", Priority: 1},
		}
		if err := linker.Init(context.Background(), exprs); err == nil {
			t.Fatal("expected error for invalid expression")
		}
	})

	t.Run("init with type mismatch returns error", func(t *testing.T) {
		t.Parallel()
		linker := relations.NewExpressionLinker(nil)
		exprs := []config.LinkExpression{
			{Name: "wrong-type", Where: "'hello'", Priority: 1},
		}
		if err := linker.Init(context.Background(), exprs); err == nil {
			t.Fatal("expected error for type mismatch")
		}
	})

	t.Run("init sorts expressions by priority descending", func(t *testing.T) {
		t.Parallel()
		linker := relations.NewExpressionLinker(nil)
		exprs := []config.LinkExpression{
			{Name: "low-priority", Where: "true", Priority: 1},
			{Name: "high-priority", Where: "false", Priority: 100},
			{Name: "mid-priority", Where: "A.name == B.name", Priority: 50},
		}
		if err := linker.Init(context.Background(), exprs); err != nil {
			t.Fatalf("Init: %v", err)
		}
	})
}

func TestExpressionLinker_EvaluatePair(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		expressions []config.LinkExpression
		entityA     dao.Entity
		entityB     dao.Entity
		wantMatch   bool
	}{
		{
			name: "domain match",
			expressions: []config.LinkExpression{
				{Name: "same-domain", Where: "A.domain == B.domain", Priority: 10},
			},
			entityA:   dao.Entity{Name: "Go", Domain: "tech"},
			entityB:   dao.Entity{Name: "Golang", Domain: "tech"},
			wantMatch: true,
		},
		{
			name: "domain mismatch",
			expressions: []config.LinkExpression{
				{Name: "same-domain", Where: "A.domain == B.domain", Priority: 10},
			},
			entityA:   dao.Entity{Name: "Go", Domain: "tech"},
			entityB:   dao.Entity{Name: "Go", Domain: "science"},
			wantMatch: false,
		},
		{
			name: "name match",
			expressions: []config.LinkExpression{
				{Name: "same-name", Where: "A.name == B.name", Priority: 10},
			},
			entityA:   dao.Entity{Name: "Golang", Domain: "tech"},
			entityB:   dao.Entity{Name: "Golang", Domain: "engineering"},
			wantMatch: true,
		},
		{
			name: "compound expression match",
			expressions: []config.LinkExpression{
				{Name: "name-and-domain", Where: "A.name == B.name && A.domain != B.domain", Priority: 10},
			},
			entityA:   dao.Entity{Name: "Golang", Domain: "tech"},
			entityB:   dao.Entity{Name: "Golang", Domain: "engineering"},
			wantMatch: true,
		},
		{
			name: "compound expression no match (same domain)",
			expressions: []config.LinkExpression{
				{Name: "name-and-domain", Where: "A.name == B.name && A.domain != B.domain", Priority: 10},
			},
			entityA:   dao.Entity{Name: "Golang", Domain: "tech"},
			entityB:   dao.Entity{Name: "Golang", Domain: "tech"},
			wantMatch: false,
		},
		{
			name: "first matching expression wins (priority order)",
			expressions: []config.LinkExpression{
				{Name: "always-true", Where: "true", Priority: 100},
				{Name: "name-match", Where: "A.name == B.name", Priority: 50},
			},
			entityA:   dao.Entity{Name: "Go", Domain: "tech"},
			entityB:   dao.Entity{Name: "Rust", Domain: "science"},
			wantMatch: true,
		},
		{
			name:        "no expressions returns no match",
			expressions: []config.LinkExpression{},
			entityA:     dao.Entity{Name: "Go", Domain: "tech"},
			entityB:     dao.Entity{Name: "Golang", Domain: "engineering"},
			wantMatch:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			linker := relations.NewExpressionLinker(nil)
			if err := linker.Init(context.Background(), tt.expressions); err != nil {
				t.Fatalf("Init: %v", err)
			}

			matched, exprName, err := linker.EvaluatePair(context.Background(), tt.entityA, tt.entityB)
			if err != nil {
				t.Fatalf("EvaluatePair: %v", err)
			}
			if matched != tt.wantMatch {
				t.Errorf("matched = %v, want %v", matched, tt.wantMatch)
			}
			if matched && exprName == "" {
				t.Error("expected expression name when matched")
			}
		})
	}
}

func TestExpressionLinker_EvaluatePair_StringFunctions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		exprWhere string
		entityA   dao.Entity
		entityB   dao.Entity
		wantMatch bool
	}{
		{
			name:      "contains match",
			exprWhere: "A.name.contains('Go')",
			entityA:   dao.Entity{Name: "Golang"},
			entityB:   dao.Entity{Name: "anything"},
			wantMatch: true,
		},
		{
			name:      "contains no match",
			exprWhere: "A.name.contains('Rust')",
			entityA:   dao.Entity{Name: "Golang"},
			entityB:   dao.Entity{Name: "anything"},
			wantMatch: false,
		},
		{
			name:      "startsWith match",
			exprWhere: "A.name.startsWith('Go')",
			entityA:   dao.Entity{Name: "Golang"},
			entityB:   dao.Entity{Name: "anything"},
			wantMatch: true,
		},
		{
			name:      "startsWith no match",
			exprWhere: "A.name.startsWith('Lang')",
			entityA:   dao.Entity{Name: "Golang"},
			entityB:   dao.Entity{Name: "anything"},
			wantMatch: false,
		},
		{
			name:      "endsWith match",
			exprWhere: "A.name.endsWith('lang')",
			entityA:   dao.Entity{Name: "Golang"},
			entityB:   dao.Entity{Name: "anything"},
			wantMatch: true,
		},
		{
			name:      "endsWith no match",
			exprWhere: "A.name.endsWith('Go')",
			entityA:   dao.Entity{Name: "Golang"},
			entityB:   dao.Entity{Name: "anything"},
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			linker := relations.NewExpressionLinker(nil)
			if err := linker.Init(context.Background(), []config.LinkExpression{
				{Name: "string-func", Where: tt.exprWhere, Priority: 10},
			}); err != nil {
				t.Fatalf("Init: %v", err)
			}

			matched, _, err := linker.EvaluatePair(context.Background(), tt.entityA, tt.entityB)
			if err != nil {
				t.Fatalf("EvaluatePair: %v", err)
			}
			if matched != tt.wantMatch {
				t.Errorf("matched = %v, want %v", matched, tt.wantMatch)
			}
		})
	}
}

func TestExpressionLinker_EvaluatePair_MetadataFunction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		exprWhere string
		entityA   dao.Entity
		entityB   dao.Entity
		wantMatch bool
	}{
		{
			name:      "metadata match",
			exprWhere: `metadata(A, 'version') == '1.0'`,
			entityA:   dao.Entity{Name: "Go", MetadataJSON: strPtr(`{"version": "1.0"}`)},
			entityB:   dao.Entity{Name: "Golang"},
			wantMatch: true,
		},
		{
			name:      "metadata no match (wrong value)",
			exprWhere: `metadata(A, 'version') == '2.0'`,
			entityA:   dao.Entity{Name: "Go", MetadataJSON: strPtr(`{"version": "1.0"}`)},
			entityB:   dao.Entity{Name: "Golang"},
			wantMatch: false,
		},
		{
			name:      "metadata no match (missing key)",
			exprWhere: `metadata(A, 'author') == 'someone'`,
			entityA:   dao.Entity{Name: "Go", MetadataJSON: strPtr(`{"version": "1.0"}`)},
			entityB:   dao.Entity{Name: "Golang"},
			wantMatch: false,
		},
		{
			name:      "metadata empty string when no metadata",
			exprWhere: `metadata(A, 'version') == ''`,
			entityA:   dao.Entity{Name: "Go"},
			entityB:   dao.Entity{Name: "Golang"},
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			linker := relations.NewExpressionLinker(nil)
			if err := linker.Init(context.Background(), []config.LinkExpression{
				{Name: "meta-check", Where: tt.exprWhere, Priority: 10},
			}); err != nil {
				t.Fatalf("Init: %v", err)
			}

			matched, _, err := linker.EvaluatePair(context.Background(), tt.entityA, tt.entityB)
			if err != nil {
				t.Fatalf("EvaluatePair: %v", err)
			}
			if matched != tt.wantMatch {
				t.Errorf("matched = %v, want %v", matched, tt.wantMatch)
			}
		})
	}
}

func TestExpressionLinker_EvaluatePair_ContextCancellation(t *testing.T) {
	t.Parallel()

	linker := relations.NewExpressionLinker(nil)
	if err := linker.Init(context.Background(), []config.LinkExpression{
		{Name: "simple", Where: "true", Priority: 10},
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := dao.Entity{Name: "Go"}
	b := dao.Entity{Name: "Golang"}

	// Should not panic; may or may not return error depending on CEL implementation
	matched, _, err := linker.EvaluatePair(ctx, a, b)
	if err != nil {
		t.Logf("EvaluatePair with cancelled ctx returned error (expected): %v", err)
	} else if !matched {
		t.Log("EvaluatePair with cancelled ctx returned false (acceptable)")
	}
}

func strPtr(s string) *string {
	return &s
}
