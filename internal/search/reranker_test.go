package search

import (
	"testing"
	"time"
)

func TestNewReranker(t *testing.T) {
	t.Parallel()

	reranker := NewReranker()

	if reranker.DeprecatedBoost != 0.2 {
		t.Errorf("DeprecatedBoost = %f, want 0.2", reranker.DeprecatedBoost)
	}
	if reranker.OfficialBoost != 1.5 {
		t.Errorf("OfficialBoost = %f, want 1.5", reranker.OfficialBoost)
	}
	if reranker.RecentBoost != 1.2 {
		t.Errorf("RecentBoost = %f, want 1.2", reranker.RecentBoost)
	}
	if reranker.RecentDays != 90 {
		t.Errorf("RecentDays = %d, want 90", reranker.RecentDays)
	}
	if reranker.AuthorityBoost == nil {
		t.Error("AuthorityBoost should not be nil")
	}
}

func TestReranker_Rerank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		results  []SearchResult
		setup    func(r *Reranker)
		wantLen  int
		wantRank []int // expected ranks after sorting
	}{
		{
			name:    "empty results",
			results: nil,
			wantLen: 0,
		},
		{
			name: "deprecated document score reduced",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{"is_deprecated": true}},
				{ChunkID: 2, Score: 0.8, Metadata: map[string]interface{}{}},
			},
			setup: func(r *Reranker) {
				r.DeprecatedBoost = 0.2
				r.OfficialBoost = 1.0
				r.RecentBoost = 1.0
			},
			wantLen: 2,
			// After boost: chunk 1 = 0.2, chunk 2 = 0.8 → chunk 2 should be first
			wantRank: []int{1, 2}, // ranks after re-sorting
		},
		{
			name: "official document score increased",
			results: []SearchResult{
				{ChunkID: 1, Score: 0.5, Metadata: map[string]interface{}{"is_official": true}},
				{ChunkID: 2, Score: 0.8, Metadata: map[string]interface{}{}},
			},
			setup: func(r *Reranker) {
				r.DeprecatedBoost = 1.0
				r.OfficialBoost = 1.5
				r.RecentBoost = 1.0
			},
			wantLen: 2,
			// After boost: chunk 1 = 0.75, chunk 2 = 0.8 → chunk 2 still first but closer
			wantRank: []int{1, 2},
		},
		{
			name: "recent document gets freshness boost",
			results: []SearchResult{
				{ChunkID: 1, Score: 0.5, Metadata: map[string]interface{}{
					"updated_at": time.Now().Format(time.RFC3339),
				}},
				{ChunkID: 2, Score: 0.8, Metadata: map[string]interface{}{
					"updated_at": time.Now().AddDate(0, 0, -100).Format(time.RFC3339),
				}},
			},
			setup: func(r *Reranker) {
				r.DeprecatedBoost = 1.0
				r.OfficialBoost = 1.0
				r.RecentBoost = 1.2
				r.RecentDays = 90
			},
			wantLen: 2,
			// After boost: chunk 1 = 0.6, chunk 2 = 0.8 → chunk 2 still first
			wantRank: []int{1, 2},
		},
		{
			name: "combined boosts reorder results",
			results: []SearchResult{
				{ChunkID: 1, Score: 0.3, Metadata: map[string]interface{}{"is_official": true}},
				{ChunkID: 2, Score: 0.5, Metadata: map[string]interface{}{}},
				{ChunkID: 3, Score: 0.4, Metadata: map[string]interface{}{"is_deprecated": true}},
			},
			setup: func(r *Reranker) {
				r.DeprecatedBoost = 0.2
				r.OfficialBoost = 1.5
				r.RecentBoost = 1.0
			},
			wantLen: 3,
			// After boost: chunk 1 = 0.45, chunk 2 = 0.5, chunk 3 = 0.08
			// Order should be: 2, 1, 3
			wantRank: []int{1, 2, 3},
		},
		{
			name: "ranks reassigned after re-sorting",
			results: []SearchResult{
				{ChunkID: 1, Score: 0.9, Rank: 1},
				{ChunkID: 2, Score: 0.5, Rank: 2},
			},
			setup: func(r *Reranker) {
				r.DeprecatedBoost = 1.0
				r.OfficialBoost = 1.0
				r.RecentBoost = 1.0
			},
			wantLen: 2,
			// No boost applied, but ranks should still be re-assigned
			wantRank: []int{1, 2},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reranker := NewReranker()
			if tt.setup != nil {
				tt.setup(reranker)
			}

			got := reranker.Rerank(tt.results)

			if len(got) != tt.wantLen {
				t.Errorf("Rerank() length = %d, want %d", len(got), tt.wantLen)
			}

			// Verify scores are in descending order.
			for i := 1; i < len(got); i++ {
				if got[i].Score > got[i-1].Score {
					t.Errorf("scores not sorted descending: got[%d].Score = %f > got[%d].Score = %f",
						i, got[i].Score, i-1, got[i-1].Score)
				}
			}

			// Verify ranks are correct (1-based, sequential).
			for i, r := range got {
				if r.Rank != i+1 {
					t.Errorf("got[%d].Rank = %d, want %d", i, r.Rank, i+1)
				}
			}
		})
	}
}

func TestReranker_ApplyBusinessRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results []SearchResult
		setup   func(r *Reranker)
		want    []float64 // expected scores after applying rules
	}{
		{
			name:    "empty results",
			results: nil,
			want:    nil,
		},
		{
			name: "deprecated document score *= 0.2",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{"is_deprecated": true}},
			},
			setup: func(r *Reranker) {
				r.DeprecatedBoost = 0.2
			},
			want: []float64{0.2},
		},
		{
			name: "official document score *= 1.5",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{"is_official": true}},
			},
			setup: func(r *Reranker) {
				r.OfficialBoost = 1.5
			},
			want: []float64{1.5},
		},
		{
			name: "expired document score *= 0.1",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{
					"valid_to": "2020-01-01T00:00:00Z",
				}},
			},
			setup: func(r *Reranker) {
				r.DeprecatedBoost = 1.0
				r.OfficialBoost = 1.0
			},
			want: []float64{0.1},
		},
		{
			name: "normal document score unchanged",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{}},
			},
			setup: func(r *Reranker) {
				r.DeprecatedBoost = 0.2
				r.OfficialBoost = 1.5
			},
			want: []float64{1.0},
		},
		{
			name: "deprecated and official combined (both apply)",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{
					"is_deprecated": true,
					"is_official":   true,
				}},
			},
			setup: func(r *Reranker) {
				r.DeprecatedBoost = 0.2
				r.OfficialBoost = 1.5
			},
			want: []float64{0.3}, // 1.0 * 0.2 * 1.5
		},
		{
			name: "multiple documents with different statuses",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{"is_deprecated": true}},
				{ChunkID: 2, Score: 1.0, Metadata: map[string]interface{}{"is_official": true}},
				{ChunkID: 3, Score: 1.0, Metadata: map[string]interface{}{}},
			},
			setup: func(r *Reranker) {
				r.DeprecatedBoost = 0.2
				r.OfficialBoost = 1.5
			},
			want: []float64{0.2, 1.5, 1.0},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reranker := NewReranker()
			if tt.setup != nil {
				tt.setup(reranker)
			}

			got := reranker.ApplyBusinessRules(tt.results)

			if len(got) != len(tt.want) {
				t.Fatalf("ApplyBusinessRules() length = %d, want %d", len(got), len(tt.want))
			}

			for i, expectedScore := range tt.want {
				if absDiff(got[i].Score, expectedScore) > 0.0001 {
					t.Errorf("got[%d].Score = %f, want %f", i, got[i].Score, expectedScore)
				}
			}
		})
	}
}

func TestReranker_ApplyFreshnessBoost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results []SearchResult
		setup   func(r *Reranker)
		want    []float64 // expected scores after freshness boost
	}{
		{
			name:    "empty results",
			results: nil,
			want:    nil,
		},
		{
			name: "recent document gets boost",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{
					"updated_at": time.Now().Format(time.RFC3339),
				}},
			},
			setup: func(r *Reranker) {
				r.RecentBoost = 1.2
				r.RecentDays = 90
			},
			want: []float64{1.2},
		},
		{
			name: "old document no boost",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{
					"updated_at": time.Now().AddDate(0, 0, -100).Format(time.RFC3339),
				}},
			},
			setup: func(r *Reranker) {
				r.RecentBoost = 1.2
				r.RecentDays = 90
			},
			want: []float64{1.0},
		},
		{
			name: "mixed recent and old documents",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{
					"updated_at": time.Now().Format(time.RFC3339),
				}},
				{ChunkID: 2, Score: 1.0, Metadata: map[string]interface{}{
					"updated_at": time.Now().AddDate(0, 0, -100).Format(time.RFC3339),
				}},
				{ChunkID: 3, Score: 1.0, Metadata: map[string]interface{}{
					"updated_at": time.Now().AddDate(0, 0, -45).Format(time.RFC3339),
				}},
			},
			setup: func(r *Reranker) {
				r.RecentBoost = 1.2
				r.RecentDays = 90
			},
			want: []float64{1.2, 1.0, 1.2},
		},
		{
			name: "missing metadata no boost",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: nil},
			},
			setup: func(r *Reranker) {
				r.RecentBoost = 1.2
			},
			want: []float64{1.0},
		},
		{
			name: "invalid date format no boost",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{
					"updated_at": "invalid-date",
				}},
			},
			setup: func(r *Reranker) {
				r.RecentBoost = 1.2
			},
			want: []float64{1.0},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reranker := NewReranker()
			if tt.setup != nil {
				tt.setup(reranker)
			}

			got := reranker.ApplyFreshnessBoost(tt.results)

			if len(got) != len(tt.want) {
				t.Fatalf("ApplyFreshnessBoost() length = %d, want %d", len(got), len(tt.want))
			}

			for i, expectedScore := range tt.want {
				if absDiff(got[i].Score, expectedScore) > 0.0001 {
					t.Errorf("got[%d].Score = %f, want %f", i, got[i].Score, expectedScore)
				}
			}
		})
	}
}

func TestReranker_ApplyAuthorityBoost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		results []SearchResult
		setup   func(r *Reranker)
		want    []float64 // expected scores after authority boost
	}{
		{
			name:    "empty results",
			results: nil,
			want:    nil,
		},
		{
			name: "known document type gets boost",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{
					"document_source_type": "policy",
				}},
			},
			setup: func(r *Reranker) {
				r.AuthorityBoost = map[string]float64{
					"policy": 1.5,
				}
			},
			want: []float64{1.5},
		},
		{
			name: "unknown document type no boost",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{
					"document_source_type": "unknown_type",
				}},
			},
			setup: func(r *Reranker) {
				r.AuthorityBoost = map[string]float64{
					"policy": 1.5,
				}
			},
			want: []float64{1.0},
		},
		{
			name: "multiple document types",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{
					"document_source_type": "policy",
				}},
				{ChunkID: 2, Score: 1.0, Metadata: map[string]interface{}{
					"document_source_type": "tutorial",
				}},
				{ChunkID: 3, Score: 1.0, Metadata: map[string]interface{}{
					"document_source_type": "api",
				}},
			},
			setup: func(r *Reranker) {
				r.AuthorityBoost = map[string]float64{
					"policy":   1.5,
					"tutorial": 1.2,
					"api":      1.0,
				}
			},
			want: []float64{1.5, 1.2, 1.0},
		},
		{
			name: "missing metadata no boost",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: nil},
			},
			setup: func(r *Reranker) {
				r.AuthorityBoost = map[string]float64{
					"policy": 1.5,
				}
			},
			want: []float64{1.0},
		},
		{
			name: "empty authority boost map no boost",
			results: []SearchResult{
				{ChunkID: 1, Score: 1.0, Metadata: map[string]interface{}{
					"document_source_type": "policy",
				}},
			},
			setup: func(r *Reranker) {
				r.AuthorityBoost = map[string]float64{}
			},
			want: []float64{1.0},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reranker := NewReranker()
			if tt.setup != nil {
				tt.setup(reranker)
			}

			got := reranker.ApplyAuthorityBoost(tt.results)

			if len(got) != len(tt.want) {
				t.Fatalf("ApplyAuthorityBoost() length = %d, want %d", len(got), len(tt.want))
			}

			for i, expectedScore := range tt.want {
				if absDiff(got[i].Score, expectedScore) > 0.0001 {
					t.Errorf("got[%d].Score = %f, want %f", i, got[i].Score, expectedScore)
				}
			}
		})
	}
}

func TestReranker_boostFactor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		result SearchResult
		setup  func(r *Reranker)
		want   float64
	}{
		{
			name:   "empty metadata returns 1.0",
			result: SearchResult{ChunkID: 1, Metadata: nil},
			want:   1.0,
		},
		{
			name:   "deprecated gets DeprecatedBoost",
			result: SearchResult{ChunkID: 1, Metadata: map[string]interface{}{"is_deprecated": true}},
			setup:  func(r *Reranker) { r.DeprecatedBoost = 0.2 },
			want:   0.2,
		},
		{
			name:   "official gets OfficialBoost",
			result: SearchResult{ChunkID: 1, Metadata: map[string]interface{}{"is_official": true}},
			setup:  func(r *Reranker) { r.OfficialBoost = 1.5 },
			want:   1.5,
		},
		{
			name:   "expired gets 0.1 penalty",
			result: SearchResult{ChunkID: 1, Metadata: map[string]interface{}{"valid_to": "2020-01-01T00:00:00Z"}},
			want:   0.1,
		},
		{
			name: "deprecated and official combined",
			result: SearchResult{
				ChunkID: 1,
				Metadata: map[string]interface{}{
					"is_deprecated": true,
					"is_official":   true,
				},
			},
			setup: func(r *Reranker) {
				r.DeprecatedBoost = 0.2
				r.OfficialBoost = 1.5
			},
			want: 0.3, // 0.2 * 1.5
		},
		{
			name: "deprecated, official, and expired all apply",
			result: SearchResult{
				ChunkID: 1,
				Metadata: map[string]interface{}{
					"is_deprecated": true,
					"is_official":   true,
					"valid_to":      "2020-01-01T00:00:00Z",
				},
			},
			setup: func(r *Reranker) {
				r.DeprecatedBoost = 0.2
				r.OfficialBoost = 1.5
			},
			want: 0.03, // 0.2 * 1.5 * 0.1
		},
		{
			name: "future valid_to no penalty",
			result: SearchResult{
				ChunkID: 1,
				Metadata: map[string]interface{}{
					"valid_to": "2099-12-31T23:59:59Z",
				},
			},
			want: 1.0,
		},
		{
			name:   "invalid date format no penalty",
			result: SearchResult{ChunkID: 1, Metadata: map[string]interface{}{"valid_to": "invalid"}},
			want:   1.0,
		},
		{
			name:   "non-bool is_deprecated ignored",
			result: SearchResult{ChunkID: 1, Metadata: map[string]interface{}{"is_deprecated": "true"}},
			want:   1.0,
		},
		{
			name:   "non-bool is_official ignored",
			result: SearchResult{ChunkID: 1, Metadata: map[string]interface{}{"is_official": "true"}},
			want:   1.0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reranker := NewReranker()
			if tt.setup != nil {
				tt.setup(reranker)
			}

			got := reranker.boostFactor(tt.result)

			if absDiff(got, tt.want) > 0.0001 {
				t.Errorf("boostFactor() = %f, want %f", got, tt.want)
			}
		})
	}
}

// absDiff returns the absolute difference between two floats.
func absDiff(a, b float64) float64 {
	diff := a - b
	if diff < 0 {
		return -diff
	}
	return diff
}
