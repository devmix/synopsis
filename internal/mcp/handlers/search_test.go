package handlers_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/mcp/handlers"
	"github.com/devmix/synopsis/internal/search"

	"github.com/mark3labs/mcp-go/mcp"
)

// mockSearcher implements search.Searcher for unit testing.
type mockSearcher struct {
	results []search.SearchResult
	err     error
}

func (m *mockSearcher) HybridSearch(_ context.Context, _ string, topK int, _ string) ([]search.SearchResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	res := make([]search.SearchResult, 0, len(m.results))
	for i := range m.results {
		if i >= topK {
			break
		}
		res = append(res, m.results[i])
	}
	return res, nil
}

func (m *mockSearcher) LexicalSearch(_ context.Context, _ string, _ int, _ string) ([]search.SearchResult, error) {
	return nil, errors.New("not implemented")
}

func (m *mockSearcher) SemanticSearch(_ context.Context, _ string, _ int, _ string) ([]search.SearchResult, error) {
	return nil, errors.New("not implemented")
}

// mockDomainValidator implements handlers.DomainValidator for unit testing.
type mockDomainValidator struct {
	knownDomains map[string]bool
}

func (v *mockDomainValidator) IsKnownDomain(domain string) bool {
	if v == nil || v.knownDomains == nil {
		return true // nil validator → skip validation
	}
	return v.knownDomains[domain]
}

// buildRequest creates a CallToolRequest with the given arguments.
func buildRequest(name string, args map[string]interface{}) mcp.CallToolRequest {
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Name:      name,
			Arguments: args,
		},
	}
	return req
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// getContentText extracts the text content from a CallToolResult.
func getContentText(result *mcp.CallToolResult) string {
	if result == nil || len(result.Content) == 0 {
		return "<nil>"
	}
	if tc, ok := result.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return "<non-text content>"
}

func TestHandleSearch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         map[string]interface{}
		mockResults  []search.SearchResult
		mockErr      error
		wantIsError  bool
		wantCount    int
		wantContains string // substring expected in response text
	}{
		{
			name:         "empty query returns error",
			args:         map[string]interface{}{"query": ""},
			wantIsError:  true,
			wantContains: "'query' argument is required",
		},
		{
			name:         "missing query returns error",
			args:         map[string]interface{}{},
			wantIsError:  true,
			wantContains: "'query' argument is required",
		},
		{
			name: "successful search with results",
			args: map[string]interface{}{"query": "employee policy", "top_k": 5},
			mockResults: []search.SearchResult{
				{
					ChunkID:      1,
					DocumentID:   10,
					ChunkText:    "The employee signed a confidentiality policy.",
					DocumentPath: "/docs/hr.md",
					Score:        0.92,
					SourceType:   "hybrid",
					Entities: []dao.Entity{
						{ID: 100, Name: "Alice", Type: "employee"},
						{ID: 101, Name: "NDA", Type: "policy"},
					},
				},
				{
					ChunkID:      2,
					DocumentID:   11,
					ChunkText:    "Policies are common documents.",
					DocumentPath: "/docs/policies.md",
					Score:        0.78,
					SourceType:   "hybrid",
				},
			},
			wantIsError:  false,
			wantCount:    2,
			wantContains: `"total_count":2`,
		},
		{
			name:         "searcher returns error propagates to response",
			args:         map[string]interface{}{"query": "test"},
			mockErr:      errors.New("database connection failed"),
			wantIsError:  true,
			wantContains: "Error during search",
		},
		{
			name:         "top_k zero returns error",
			args:         map[string]interface{}{"query": "test", "top_k": float64(0)},
			wantIsError:  true,
			wantContains: "'top_k' must be between 1 and 100, got 0",
		},
		{
			name:         "top_k negative returns error",
			args:         map[string]interface{}{"query": "test", "top_k": float64(-5)},
			wantIsError:  true,
			wantContains: "'top_k' must be between 1 and 100, got -5",
		},
		{
			name:         "top_k over 100 returns error",
			args:         map[string]interface{}{"query": "test", "top_k": float64(500)},
			wantIsError:  true,
			wantContains: "'top_k' must be between 1 and 100, got 500",
		},
		{
			name: "top_k boundary minimum (1) passes",
			args: map[string]interface{}{"query": "test", "top_k": float64(1)},
			mockResults: []search.SearchResult{
				{ChunkID: 1, DocumentID: 1, ChunkText: "result one", DocumentPath: "/a.md", Score: 0.9, SourceType: "hybrid"},
			},
			wantIsError: false,
			wantCount:   1,
		},
		{
			name: "top_k boundary maximum (100) passes",
			args: map[string]interface{}{"query": "test", "top_k": float64(100)},
			mockResults: []search.SearchResult{
				{ChunkID: 1, DocumentID: 1, ChunkText: "result one", DocumentPath: "/a.md", Score: 0.9, SourceType: "hybrid"},
			},
			wantIsError: false,
			wantCount:   1,
		},
		{
			name:         "empty results returns valid response with zero count",
			args:         map[string]interface{}{"query": "nonexistent_xyz"},
			mockResults:  []search.SearchResult{},
			wantIsError:  false,
			wantCount:    0,
			wantContains: `"total_count":0`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			searcher := &mockSearcher{results: tt.mockResults, err: tt.mockErr}
			req := buildRequest("search", tt.args)

			result, err := handlers.HandleSearch(context.Background(), req, searcher, nil)
			if err != nil {
				t.Fatalf("HandleSearch() returned unexpected error: %v", err)
			}

			if result == nil {
				t.Fatal("result is nil")
			}

			if result.IsError != tt.wantIsError {
				t.Errorf("IsError = %v, want %v", result.IsError, tt.wantIsError)
			}

			if len(result.Content) == 0 {
				t.Fatal("response content is empty")
			}

			textContent, ok := result.Content[0].(mcp.TextContent)
			if !ok {
				t.Fatalf("expected TextContent, got %T", result.Content[0])
			}

			if tt.wantContains != "" {
				found := false
				for i := 0; i <= len(textContent.Text)-len(tt.wantContains); i++ {
					if textContent.Text[i:i+len(tt.wantContains)] == tt.wantContains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("response does not contain %q, got: %s", tt.wantContains, textContent.Text)
				}
			}

			if !tt.wantIsError && tt.wantCount >= 0 {
				var resp handlers.SearchResponse
				if err := json.Unmarshal([]byte(textContent.Text), &resp); err != nil {
					t.Fatalf("unmarshal response: %v", err)
				}
				if resp.TotalCount != tt.wantCount {
					t.Errorf("TotalCount = %d, want %d", resp.TotalCount, tt.wantCount)
				}
			}
		})
	}
}

func TestHandleSearch_UnknownDomainWarning(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		args          map[string]interface{}
		knownDomains  map[string]bool
		mockResults   []search.SearchResult
		wantWarning   bool
		warningSubstr string
	}{
		{
			name: "unknown domain produces warning",
			args: map[string]interface{}{"query": "hiring", "domain": "nonexistent-domain"},
			knownDomains: map[string]bool{
				"hr":          true,
				"product":     true,
				"engineering": true,
			},
			mockResults:   []search.SearchResult{},
			wantWarning:   true,
			warningSubstr: "unknown domain",
		},
		{
			name: "known domain produces no warning",
			args: map[string]interface{}{"query": "hiring", "domain": "hr"},
			knownDomains: map[string]bool{
				"hr":          true,
				"product":     true,
				"engineering": true,
			},
			mockResults: []search.SearchResult{
				{ChunkID: 1, DocumentID: 1, ChunkText: "HR policy", Score: 0.9, SourceType: "hybrid"},
			},
			wantWarning: false,
		},
		{
			name: "no domain filter produces no warning",
			args: map[string]interface{}{"query": "hiring"},
			knownDomains: map[string]bool{
				"hr": true,
			},
			mockResults: []search.SearchResult{
				{ChunkID: 1, DocumentID: 1, ChunkText: "result", Score: 0.9, SourceType: "hybrid"},
			},
			wantWarning: false,
		},
		{
			name:         "nil validator skips domain check (no warning even for unknown)",
			args:         map[string]interface{}{"query": "hiring", "domain": "nonexistent-domain"},
			knownDomains: nil, // will cause nil validator to be passed
			mockResults:  []search.SearchResult{},
			wantWarning:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			searcher := &mockSearcher{results: tt.mockResults}
			req := buildRequest("search", tt.args)

			var validator handlers.DomainValidator
			if tt.knownDomains != nil {
				validator = &mockDomainValidator{knownDomains: tt.knownDomains}
			}

			result, err := handlers.HandleSearch(context.Background(), req, searcher, validator)
			if err != nil {
				t.Fatalf("HandleSearch() returned unexpected error: %v", err)
			}

			if result == nil || len(result.Content) == 0 {
				t.Fatal("result or content is nil/empty")
			}

			textContent, ok := result.Content[0].(mcp.TextContent)
			if !ok {
				t.Fatalf("expected TextContent, got %T", result.Content[0])
			}

			var resp handlers.SearchResponse
			if err := json.Unmarshal([]byte(textContent.Text), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			if tt.wantWarning {
				if resp.Warning == "" {
					t.Error("expected warning field to be set, but it is empty")
				} else if tt.warningSubstr != "" && !contains(resp.Warning, tt.warningSubstr) {
					t.Errorf("warning %q does not contain %q", resp.Warning, tt.warningSubstr)
				}
			} else {
				if resp.Warning != "" {
					t.Errorf("expected no warning, got: %q", resp.Warning)
				}
			}
		})
	}
}

func TestHandleSearch_ResponseFields(t *testing.T) {
	t.Parallel()

	startOffset := 100
	endOffset := 250

	searcher := &mockSearcher{
		results: []search.SearchResult{
			{
				ChunkID:      42,
				DocumentID:   7,
				ChunkText:    "Test chunk content",
				DocumentPath: "/docs/test.md",
				Score:        0.85,
				SourceType:   "hybrid",
				SequenceNum:  3,
				StartOffset:  &startOffset,
				EndOffset:    &endOffset,
				Metadata: map[string]interface{}{
					"domains": []string{"hr", "policy"},
				},
				Entities: []dao.Entity{
					{ID: 501, Name: "Alice", Type: "employee"},
				},
			},
		},
	}

	req := buildRequest("search", map[string]interface{}{"query": "test"})
	result, err := handlers.HandleSearch(context.Background(), req, searcher, nil)
	if err != nil {
		t.Fatalf("HandleSearch() error = %v", err)
	}

	var resp handlers.SearchResponse
	if err := json.Unmarshal([]byte(result.Content[0].(mcp.TextContent).Text), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}

	r := resp.Results[0]
	if r.DocumentID != 7 {
		t.Errorf("DocumentID = %d, want 7", r.DocumentID)
	}
	if r.ChunkID != 42 {
		t.Errorf("ChunkID = %d, want 42", r.ChunkID)
	}
	if r.Text != "Test chunk content" {
		t.Errorf("Text = %q, want %q", r.Text, "Test chunk content")
	}
	if r.SequenceNum != 3 {
		t.Errorf("SequenceNum = %d, want 3", r.SequenceNum)
	}
	if r.StartOffset != 100 {
		t.Errorf("StartOffset = %d, want 100", r.StartOffset)
	}
	if r.EndOffset != 250 {
		t.Errorf("EndOffset = %d, want 250", r.EndOffset)
	}
	if r.DocumentPath != "/docs/test.md" {
		t.Errorf("DocumentPath = %q, want %q", r.DocumentPath, "/docs/test.md")
	}
	if r.Score != 0.85 {
		t.Errorf("Score = %f, want 0.85", r.Score)
	}
	if r.SourceType != "hybrid" {
		t.Errorf("SourceType = %q, want %q", r.SourceType, "hybrid")
	}
	if len(r.Domains) != 2 || r.Domains[0] != "hr" || r.Domains[1] != "policy" {
		t.Errorf("Domains = %+v, want [hr policy]", r.Domains)
	}
	if len(r.Entities) != 1 || r.Entities[0].Name != "Alice" {
		t.Errorf("Entities mismatch: %+v", r.Entities)
	}
	if r.Entities[0].ID != 501 {
		t.Errorf("Entity Predicate = %d, want 501", r.Entities[0].ID)
	}
	if resp.SearchTimeMs < 0 {
		t.Errorf("SearchTimeMs should be non-negative, got %d", resp.SearchTimeMs)
	}
}

func TestHandleSearch_DomainFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		args      map[string]interface{}
		results   []search.SearchResult
		wantCount int
	}{
		{
			name: "domain filter matches results with string slice domains",
			args: map[string]interface{}{"query": "test", "domain": "hr"},
			results: []search.SearchResult{
				// Mock searcher simulates domain filtering inside HybridSearch.
				// Only hr-domain results are returned (filtering happens before truncation).
				{
					ChunkID:   1,
					ChunkText: "HR policy document",
					Score:     0.9,
					Metadata: map[string]interface{}{
						"domains": []string{"hr", "policy"},
					},
				},
			},
			wantCount: 1,
		},
		{
			name:      "domain filter no match returns empty",
			args:      map[string]interface{}{"query": "test", "domain": "product"},
			results:   []search.SearchResult{}, // mock searcher filtered out all results
			wantCount: 0,
		},
		{
			name: "no domain filter returns all results",
			args: map[string]interface{}{"query": "test"},
			results: []search.SearchResult{
				{
					ChunkID:   1,
					ChunkText: "HR policy document",
					Score:     0.9,
					Metadata: map[string]interface{}{
						"domains": []string{"hr"},
					},
				},
				{
					ChunkID:   2,
					ChunkText: "Engineering spec",
					Score:     0.8,
					Metadata: map[string]interface{}{
						"domains": []string{"engineering"},
					},
				},
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			searcher := &mockSearcher{results: tt.results}
			req := buildRequest("search", tt.args)

			result, err := handlers.HandleSearch(context.Background(), req, searcher, nil)
			if err != nil {
				t.Fatalf("HandleSearch() returned unexpected error: %v", err)
			}

			if result == nil || len(result.Content) == 0 {
				t.Fatal("result or content is nil/empty")
			}

			textContent, ok := result.Content[0].(mcp.TextContent)
			if !ok {
				t.Fatalf("expected TextContent, got %T", result.Content[0])
			}

			var resp handlers.SearchResponse
			if err := json.Unmarshal([]byte(textContent.Text), &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}

			if resp.TotalCount != tt.wantCount {
				t.Errorf("TotalCount = %d, want %d", resp.TotalCount, tt.wantCount)
			}
			if len(resp.Results) != tt.wantCount {
				t.Errorf("Results length = %d, want %d", len(resp.Results), tt.wantCount)
			}
		})
	}
}
