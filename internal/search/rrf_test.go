package search

import (
	"testing"
)

func TestReciprocalRankFusion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		lexicalResults  []LexicalSearchResult
		semanticResults []SemanticSearchResult
		k               int
		topN            int
		wantCount       int
		wantFirstID     int     // chunk_id of the top result
		wantTopScoreGT  float64 // minimum expected score for #1
	}{
		{
			name:            "empty inputs",
			lexicalResults:  nil,
			semanticResults: nil,
			k:               60,
			topN:            10,
			wantCount:       0,
		},
		{
			name: "only lexical results",
			lexicalResults: []LexicalSearchResult{
				{ChunkID: 1, ChunkText: "alpha", DocumentID: 1},
				{ChunkID: 2, ChunkText: "beta", DocumentID: 1},
				{ChunkID: 3, ChunkText: "gamma", DocumentID: 2},
			},
			semanticResults: nil,
			k:               60,
			topN:            10,
			wantCount:       3,
			wantFirstID:     1,
			wantTopScoreGT:  0.15, // calibrated: 0.7·rrf + 0.3·bm25_norm; BM25 all zero → norm=0.5
		},
		{
			name:           "only semantic results",
			lexicalResults: nil,
			semanticResults: []SemanticSearchResult{
				{ChunkID: 10, ChunkText: "vector_a", DocumentID: 3, Score: 0.1},
				{ChunkID: 11, ChunkText: "vector_b", DocumentID: 3, Score: 0.2},
			},
			k:              60,
			topN:           10,
			wantCount:      2,
			wantFirstID:    10,
			wantTopScoreGT: 0.14, // calibrated: semantic-only gets BM25 norm=0.5 neutral
		},
		{
			name: "overlapping results — shared chunk gets higher score",
			lexicalResults: []LexicalSearchResult{
				{ChunkID: 1, ChunkText: "shared text", DocumentID: 1},
				{ChunkID: 2, ChunkText: "only lexical", DocumentID: 1},
				{ChunkID: 3, ChunkText: "third lexical", DocumentID: 2},
			},
			semanticResults: []SemanticSearchResult{
				{ChunkID: 1, ChunkText: "shared text", DocumentID: 1, Score: 0.05},
				{ChunkID: 4, ChunkText: "only semantic", DocumentID: 2, Score: 0.1},
			},
			k:              60,
			topN:           10,
			wantCount:      4,
			wantFirstID:    1, // chunk 1 appears in both lists → highest RRF score
			wantTopScoreGT: 0.17, // calibrated: higher RRF + BM25 norm
		},
		{
			name: "topN truncation",
			lexicalResults: func() []LexicalSearchResult {
				var r []LexicalSearchResult
				for i := 0; i < 5; i++ {
					r = append(r, LexicalSearchResult{ChunkID: i + 1, ChunkText: "text", DocumentID: 1})
				}
				return r
			}(),
			semanticResults: nil,
			k:               60,
			topN:            2,
			wantCount:       2,
			wantFirstID:     1,
		},
		{
			name: "custom k parameter",
			lexicalResults: []LexicalSearchResult{
				{ChunkID: 1, ChunkText: "a", DocumentID: 1},
			},
			semanticResults: nil,
			k:               10, // smaller k → higher RRF scores
			topN:            10,
			wantCount:       1,
			wantFirstID:     1,
			wantTopScoreGT:  0.2, // calibrated with BM25 norm; raw RRF ≈ 0.0909
		},
		{
			name: "default k when zero",
			lexicalResults: []LexicalSearchResult{
				{ChunkID: 42, ChunkText: "hello", DocumentID: 5},
			},
			semanticResults: nil,
			k:               0, // should default to 20 (calibrated)
			topN:            10,
			wantCount:       1,
			wantFirstID:     42,
			wantTopScoreGT:  0.15, // calibrated with k=20 and BM25 norm
		},
	}

	for _, tt := range tests {
		tt := tt // capture for parallel subtests
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ReciprocalRankFusion(
				tt.lexicalResults,
				tt.semanticResults,
				tt.k,
				tt.topN,
			)

			if len(got) != tt.wantCount {
				t.Errorf("count = %d, want %d", len(got), tt.wantCount)
			}

			if tt.wantFirstID > 0 && len(got) == 0 {
				t.Error("expected at least one result but got none")
			}

			if tt.wantFirstID > 0 && len(got) > 0 {
				if got[0].ChunkID != tt.wantFirstID {
					t.Errorf("top chunk_id = %d, want %d", got[0].ChunkID, tt.wantFirstID)
				}
			}

			if tt.wantTopScoreGT > 0 && len(got) > 0 {
				if got[0].Score <= tt.wantTopScoreGT {
					t.Errorf("top score = %f, want > %f", got[0].Score, tt.wantTopScoreGT)
				}
			}

			// Verify ranks are assigned correctly (1-based).
			for i, r := range got {
				if r.Rank != i+1 {
					t.Errorf("result[%d].Rank = %d, want %d", i, r.Rank, i+1)
				}
			}

			// Verify scores are in descending order.
			for i := 1; i < len(got); i++ {
				if got[i].Score > got[i-1].Score {
					t.Errorf("scores not sorted: result[%d].score = %f > result[%d].score = %f",
						i, got[i].Score, i-1, got[i-1].Score)
				}
			}

			// Verify source type.
			for _, r := range got {
				switch r.SourceType {
				case "lexical", "semantic", "hybrid":
					// valid
				default:
					t.Errorf("unexpected source_type %q for chunk %d", r.SourceType, r.ChunkID)
				}
			}
		})
	}
}

func TestReciprocalRankFusion_ScoreCalculation(t *testing.T) {
	t.Parallel()

	// Verify RRF score calculation with BM25 calibration.
	k := 60

	lexicalResults := []LexicalSearchResult{
		{ChunkID: 1, ChunkText: "a", DocumentID: 1}, // rank 1 in lexical → rrf = 1/61
		{ChunkID: 2, ChunkText: "b", DocumentID: 1}, // rank 2 in lexical → rrf = 1/62
	}

	semanticResults := []SemanticSearchResult{
		{ChunkID: 2, ChunkText: "b", DocumentID: 1, Score: 0.1}, // rank 1 in semantic → rrf += 1/61
		{ChunkID: 3, ChunkText: "c", DocumentID: 2, Score: 0.2}, // rank 2 in semantic → rrf = 1/62
	}

	got := ReciprocalRankFusion(lexicalResults, semanticResults, k, 10)

	if len(got) != 3 {
		t.Fatalf("count = %d, want 3", len(got))
	}

	// Chunk 2 appears in both lists: rrfScore = 1/62 + 1/61 ≈ 0.0321
	// With BM25 calibration (all BM25=0 → norm=0.5): final = 0.7*rrf + 0.3*0.5
	var chunk2Score float64
	for _, r := range got {
		if r.ChunkID == 2 {
			chunk2Score = r.Score
			break
		}
	}

	rawRRF := 1.0/float64(k+2) + 1.0/float64(k+1) // chunk 2: rank 2 in lexical, rank 1 in semantic
	expectedMin := 0.7*rawRRF + 0.3*0.5            // calibrated score
	if chunk2Score < expectedMin-0.0001 {
		t.Errorf("chunk 2 score = %f, want >= %f (calibrated: 0.7·rrf(%f) + 0.3·bm25_norm(0.5))",
			chunk2Score, expectedMin, rawRRF)
	}

	// Chunk 2 should be ranked #1 (highest combined score).
	if got[0].ChunkID != 2 {
		t.Errorf("top result chunk_id = %d, want 2", got[0].ChunkID)
	}
}

func TestReciprocalRankFusion_SourceType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		lexicalResults  []LexicalSearchResult
		semanticResults []SemanticSearchResult
		chunkID         int
		wantSourceType  string
	}{
		{
			name:            "chunk only in lexical",
			lexicalResults:  []LexicalSearchResult{{ChunkID: 1, ChunkText: "a", DocumentID: 1}},
			semanticResults: nil,
			chunkID:         1,
			wantSourceType:  "lexical",
		},
		{
			name:            "chunk only in semantic",
			lexicalResults:  nil,
			semanticResults: []SemanticSearchResult{{ChunkID: 2, ChunkText: "b", DocumentID: 1, Score: 0.1}},
			chunkID:         2,
			wantSourceType:  "semantic",
		},
		{
			name:            "chunk in both lists",
			lexicalResults:  []LexicalSearchResult{{ChunkID: 3, ChunkText: "c", DocumentID: 1}},
			semanticResults: []SemanticSearchResult{{ChunkID: 3, ChunkText: "c", DocumentID: 1, Score: 0.05}},
			chunkID:         3,
			wantSourceType:  "hybrid",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ReciprocalRankFusion(tt.lexicalResults, tt.semanticResults, 60, 10)

			var found bool
			for _, r := range got {
				if r.ChunkID == tt.chunkID {
					found = true
					if r.SourceType != tt.wantSourceType {
						t.Errorf("source_type for chunk %d = %q, want %q", tt.chunkID, r.SourceType, tt.wantSourceType)
					}
					break
				}
			}
			if !found {
				t.Errorf("chunk %d not found in results", tt.chunkID)
			}
		})
	}
}

// TestReciprocalRankFusion_StableTiebreak verifies that equal-score chunks are
// ordered by ascending ChunkID (deterministic tiebreaker).
func TestReciprocalRankFusion_StableTiebreak(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		lexical      []LexicalSearchResult
		semantic     []SemanticSearchResult
		k            int
		topN         int
		wantChunkIDs []int // expected chunk IDs in order
	}{
		{
			name: "equal scores — lower ChunkID first",
			// Both chunks appear at rank 1 in their respective lists, so both get equal RRF + BM25 norm.
			lexical:      []LexicalSearchResult{{ChunkID: 5, ChunkText: "a", DocumentID: 1}},
			semantic:     []SemanticSearchResult{{ChunkID: 3, ChunkText: "b", DocumentID: 1, Score: 0.1}},
			k:            60,
			topN:         10,
			wantChunkIDs: []int{3, 5}, // chunk 3 < chunk 5, equal scores → 3 first
		},
		{
			name: "three chunks — two tied, one lower",
			// chunk 10 and 20 both at rank 1 in their lists → equal calibrated score.
			// chunk 5 at semantic rank 2 → lower RRF + same BM25 norm → lower total.
			lexical: []LexicalSearchResult{
				{ChunkID: 10, ChunkText: "a", DocumentID: 1}, // rank 1 in lexical
			},
			semantic: []SemanticSearchResult{
				{ChunkID: 20, ChunkText: "b", DocumentID: 1, Score: 0.1}, // rank 1 in semantic (tied with chunk 10)
				{ChunkID: 5, ChunkText: "c", DocumentID: 1, Score: 0.1},  // rank 2 in semantic (lower RRF)
			},
			k:            60,
			topN:         10,
			wantChunkIDs: []int{10, 20, 5}, // tied chunks 10 & 20 sorted by ChunkID asc, then chunk 5 (lower score)
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := ReciprocalRankFusion(tt.lexical, tt.semantic, tt.k, tt.topN)

			if len(got) != len(tt.wantChunkIDs) {
				t.Fatalf("got %d results, want %d", len(got), len(tt.wantChunkIDs))
			}

			for i, wantID := range tt.wantChunkIDs {
				if got[i].ChunkID != wantID {
					t.Errorf("result[%d].ChunkID = %d, want %d", i, got[i].ChunkID, wantID)
				}
			}

			// Run again to verify determinism.
			got2 := ReciprocalRankFusion(tt.lexical, tt.semantic, tt.k, tt.topN)
			for i := range got {
				if got[i].ChunkID != got2[i].ChunkID {
					t.Errorf("non-deterministic: call1[%d]=%d, call2[%d]=%d",
						i, got[i].ChunkID, i, got2[i].ChunkID)
				}
			}
		})
	}
}

// TestReciprocalRankFusion_TopNGuard verifies that topN <= 0 does not panic
// and returns all results without truncation.
func TestReciprocalRankFusion_TopNGuard(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		topN    int
		wantAll bool // true means no truncation should happen
	}{
		{
			name:    "topN zero — return all",
			topN:    0,
			wantAll: true,
		},
		{
			name:    "topN negative — return all",
			topN:    -1,
			wantAll: true,
		},
		{
			name:    "topN positive — truncate normally",
			topN:    2,
			wantAll: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			lexicalResults := []LexicalSearchResult{
				{ChunkID: 1, ChunkText: "a", DocumentID: 1},
				{ChunkID: 2, ChunkText: "b", DocumentID: 1},
				{ChunkID: 3, ChunkText: "c", DocumentID: 1},
			}

			got := ReciprocalRankFusion(lexicalResults, nil, 60, tt.topN)

			if tt.wantAll {
				if len(got) != 3 {
					t.Errorf("topN=%d: got %d results, want all 3", tt.topN, len(got))
				}
			} else {
				if len(got) != tt.topN {
					t.Errorf("topN=%d: got %d results, want %d", tt.topN, len(got), tt.topN)
				}
			}
		})
	}
}

// TestNormalizeBM25_MixedPool verifies that semantic-only entries (BM25Score=0) do not
// skew min/max range computation in normalizeBM25. In a mixed pool, only lexical/hybrid
// entries participate in the range; semantic-only entries receive neutral value 0.5.
func TestNormalizeBM25_MixedPool(t *testing.T) {
	t.Parallel()

	// Build a mixed pool: two lexical entries with different BM25 scores and one semantic-only entry.
	entries := []rrfEntry{
		{ChunkID: 1, BM25Score: 0.5, SourceType: "lexical"},   // best BM25 (lower is better)
		{ChunkID: 2, BM25Score: 4.0, SourceType: "lexical"},   // worse BM25
		{ChunkID: 3, BM25Score: 0, SourceType: "semantic"},     // no BM25 data — should get 0.5 neutral
	}

	normalizeBM25(entries)

	// Semantic-only entry must be exactly 0.5 (neutral).
	if entries[2].BM25Score != 0.5 {
		t.Errorf("semantic-only BM25 norm = %f, want 0.5", entries[2].BM25Score)
	}

	// Lexical entry with best BM25 (0.5) should normalize to 1.0 (highest).
	if entries[0].BM25Score != 1.0 {
		t.Errorf("best lexical BM25 norm = %f, want 1.0", entries[0].BM25Score)
	}

	// Lexical entry with worst BM25 (4.0) should normalize to 0.0 (lowest).
	if entries[1].BM25Score != 0.0 {
		t.Errorf("worst lexical BM25 norm = %f, want 0.0", entries[1].BM25Score)
	}

	// Verify that the semantic-only entry did NOT pull minBM25 to 0.
	// If it had, the range would be [0, 4] and best BM25 (0.5) would normalize to:
	//   (4 - 0.5) / 4 = 0.875 instead of 1.0.
	if entries[0].BM25Score < 0.99 {
		t.Errorf("best lexical BM25 norm (%f) is too low — semantic-only entry likely skewed the range", entries[0].BM25Score)
	}
}

// TestReciprocalRankFusion_BM25Calibration verifies that BM25 scores are properly
// normalized and blended into the final calibrated score.
func TestReciprocalRankFusion_BM25Calibration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		lexicalResults  []LexicalSearchResult
		semanticResults []SemanticSearchResult
		k               int
		topN            int
		wantCount       int
		check           func(t *testing.T, results []SearchResult)
	}{
		{
			name: "BM25 differentiation — lower BM25 (better) ranks higher",
			lexicalResults: []LexicalSearchResult{
				// In SQLite FTS5, lower score is better. Chunk 1 has best BM25.
				{ChunkID: 1, ChunkText: "best match", DocumentID: 1, Score: 0.5},
				{ChunkID: 2, ChunkText: "ok match", DocumentID: 1, Score: 2.0},
				{ChunkID: 3, ChunkText: "weak match", DocumentID: 1, Score: 5.0},
			},
			semanticResults: nil,
			k:               60,
			topN:            10,
			wantCount:       3,
			check: func(t *testing.T, results []SearchResult) {
				// All at same RRF rank position but different BM25.
				// After calibration, chunk 1 (best BM25=0.5) should have highest score.
				if results[0].ChunkID != 1 {
					t.Errorf("expected chunk 1 (best BM25) first, got chunk %d", results[0].ChunkID)
				}
				if results[1].ChunkID != 2 {
					t.Errorf("expected chunk 2 second, got chunk %d", results[1].ChunkID)
				}
				if results[2].ChunkID != 3 {
					t.Errorf("expected chunk 3 third, got chunk %d", results[2].ChunkID)
				}
				// Verify score differentiation: best BM25 should have noticeably higher score.
				if results[0].Score <= results[1].Score {
					t.Errorf("chunk 1 score (%f) should be > chunk 2 score (%f)",
						results[0].Score, results[1].Score)
				}
			},
		},
		{
			name: "semantic-only gets neutral BM25",
			lexicalResults: nil,
			semanticResults: []SemanticSearchResult{
				{ChunkID: 10, ChunkText: "vector result", DocumentID: 3, Score: 0.1},
			},
			k:         60,
			topN:      10,
			wantCount: 1,
			check: func(t *testing.T, results []SearchResult) {
				// Semantic-only result should have score > 0 (from RRF + neutral BM25).
				if results[0].Score <= 0 {
					t.Errorf("semantic-only score should be positive, got %f", results[0].Score)
				}
			},
		},
		{
			name: "hybrid result combines RRF and BM25",
			lexicalResults: []LexicalSearchResult{
				{ChunkID: 1, ChunkText: "shared", DocumentID: 1, Score: 1.0},
			},
			semanticResults: []SemanticSearchResult{
				{ChunkID: 1, ChunkText: "shared", DocumentID: 1, Score: 0.5},
			},
			k:         60,
			topN:      10,
			wantCount: 1,
			check: func(t *testing.T, results []SearchResult) {
				if results[0].SourceType != "hybrid" {
					t.Errorf("expected hybrid source type, got %q", results[0].SourceType)
				}
				// Score should be higher than pure RRF due to BM25 contribution.
				pureRRF := 1.0/61 + 1.0/61 // both at rank 1
				if results[0].Score <= pureRRF {
					t.Errorf("calibrated score (%f) should exceed pure RRF (%f)",
						results[0].Score, pureRRF)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := ReciprocalRankFusion(
				tt.lexicalResults,
				tt.semanticResults,
				tt.k,
				tt.topN,
			)

			if len(got) != tt.wantCount {
				t.Fatalf("count = %d, want %d", len(got), tt.wantCount)
			}

			if tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}
