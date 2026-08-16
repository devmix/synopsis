package ner

import (
	"context"
	"testing"

	"github.com/devmix/synopsis/internal/domain"
)

func TestRegexNER_CaptureGroup(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		pattern      string
		entityType   string
		ruleID       string
		content      string
		wantCount    int
		wantEntities []string // expected entity names in order
	}{
		{
			name:         "capture group extracts first group instead of full match",
			pattern:      `([a-zA-Z0-9._%+\-]+)@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`,
			entityType:   "employee",
			ruleID:       "entity_from_email",
			content:      "Contact nikolay.morozov@example.com for details.",
			wantCount:    1,
			wantEntities: []string{"nikolay.morozov"},
		},
		{
			name:         "capture group with simple username",
			pattern:      `([a-zA-Z0-9._%+\-]+)@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`,
			entityType:   "employee",
			ruleID:       "entity_from_email",
			content:      "Email john@company.org for support.",
			wantCount:    1,
			wantEntities: []string{"john"},
		},
		{
			name:         "capture group multiple matches in text",
			pattern:      `([a-zA-Z0-9._%+\-]+)@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`,
			entityType:   "employee",
			ruleID:       "entity_from_email",
			content:      "Reach alice@corp.com or bob.smith@corp.com.",
			wantCount:    2,
			wantEntities: []string{"alice", "bob.smith"},
		},
		{
			name:         "no capture group uses full match (backward compatible)",
			pattern:      `[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`,
			entityType:   "email",
			ruleID:       "full_email_match",
			content:      "Send to test@example.com please.",
			wantCount:    1,
			wantEntities: []string{"test@example.com"},
		},
		{
			name:         "capture group with no match returns empty",
			pattern:      `([a-zA-Z]+)@example\.com`,
			entityType:   "employee",
			ruleID:       "specific_domain",
			content:      "No emails here, just text.",
			wantCount:    0,
			wantEntities: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dc := &domain.DomainConfig{
				Name: "test",
				Extraction: domain.ExtractionDef{
					RegexRules: []domain.RegexRuleDef{
						{
							ID:         tt.ruleID,
							Pattern:    tt.pattern,
							Entity:     tt.entityType,
							Confidence: 0.9,
						},
					},
				},
			}

			ner, err := NewRegexNER([]*domain.DomainConfig{dc}, nil)
			if err != nil {
				t.Fatalf("NewRegexNER() error = %v", err)
			}

			result, err := ner.ExtractEntities(context.Background(), tt.content, nil)
			if err != nil {
				t.Fatalf("ExtractEntities() error = %v", err)
			}

			if result == nil && tt.wantCount > 0 {
				t.Fatal("expected entities but got nil result")
			}

			if result == nil {
				result = &Result{Entities: []Entity{}}
			}

			if len(result.Entities) != tt.wantCount {
				t.Errorf("got %d entities, want %d", len(result.Entities), tt.wantCount)
				for i, e := range result.Entities {
					t.Logf("  entity[%d]: name=%q type=%s", i, e.Name, e.Type)
				}
			}

			for i, wantName := range tt.wantEntities {
				if i >= len(result.Entities) {
					t.Errorf("missing entity at index %d (want name %q)", i, wantName)
					continue
				}
				if result.Entities[i].Name != wantName {
					t.Errorf("entity[%d].name = %q, want %q", i, result.Entities[i].Name, wantName)
				}
			}
		})
	}
}

func TestRegexNER_NilLogger(t *testing.T) {
	t.Parallel()

	dc := &domain.DomainConfig{
		Name: "test",
		Extraction: domain.ExtractionDef{
			RegexRules: []domain.RegexRuleDef{
				{ID: "r1", Pattern: `\btest\b`, Entity: "concept", Confidence: 0.8},
			},
		},
	}

	// nil logger should not cause panic.
	ner, err := NewRegexNER([]*domain.DomainConfig{dc}, nil)
	if err != nil {
		t.Fatalf("NewRegexNER() error = %v", err)
	}

	result, err := ner.ExtractEntities(context.Background(), "this is a test string", nil)
	if err != nil {
		t.Fatalf("ExtractEntities() error = %v", err)
	}

	if result == nil || len(result.Entities) != 1 {
		t.Errorf("expected 1 entity, got %d", func() int {
			if result == nil {
				return 0
			}
			return len(result.Entities)
		}())
	}
}
