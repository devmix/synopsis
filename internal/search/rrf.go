// Package search (rrf) implements Reciprocal Rank Fusion for merging ranked lists.
package search

import (
	"math"
	"sort"
)

const defaultRRFK = 20 // Calibrated k value: lower k increases rank sensitivity (~8× vs k=60).

// rrfEntry accumulates scores and metadata for a single chunk across multiple rankers.
type rrfEntry struct {
	ChunkID     int
	ChunkText   string
	DocumentID  int
	SequenceNum int
	StartOffset *int
	EndOffset   *int
	Score       float64 // Sum of RRF scores from all lists (before BM25 calibration).
	BM25Score   float64 // Raw BM25 score from lexical search (0 if not in lexical results).
	Rank        int     // Final rank after fusion (set by caller).
	SourceType  string  // Source type: "lexical", "semantic", or "hybrid" (both).
}

// ReciprocalRankFusion merges two ranked result lists using the RRF algorithm with BM25 calibration.
//
// The formula is: score = sum(1 / (k + rank)) for each list where the item appears,
// then calibrated as: finalScore = 0.7·rrfScore + 0.3·bm25NormScore.
// k defaults to 20 if not specified. Results are sorted by descending fused score,
// with ChunkID ascending as a deterministic tiebreaker for equal scores.
// If topN <= 0, no truncation is applied and all results are returned.
func ReciprocalRankFusion(
	lexicalResults []LexicalSearchResult,
	semanticResults []SemanticSearchResult,
	k int,
	topN int,
) []SearchResult {
	if k <= 0 {
		k = defaultRRFK
	}

	scores := make(map[int]*rrfEntry) // chunk_id -> accumulated entry

	// Process lexical results. BM25 score is stored raw (lower is better in SQLite FTS5).
	for rank, r := range lexicalResults {
		entry, ok := scores[r.ChunkID]
		if !ok {
			entry = &rrfEntry{
				ChunkID:     r.ChunkID,
				ChunkText:   r.ChunkText,
				DocumentID:  r.DocumentID,
				SequenceNum: r.SequenceNum,
				StartOffset: r.StartOffset,
				EndOffset:   r.EndOffset,
				BM25Score:   r.Score, // raw BM25 score (lower is better)
				SourceType:  "lexical",
			}
			scores[r.ChunkID] = entry
		}
		entry.Score += 1.0 / float64(k+(rank+1)) // rank is 1-based
	}

	// Process semantic results.
	for rank, r := range semanticResults {
		entry, ok := scores[r.ChunkID]
		if !ok {
			entry = &rrfEntry{
				ChunkID:     r.ChunkID,
				ChunkText:   r.ChunkText,
				DocumentID:  r.DocumentID,
				SequenceNum: r.SequenceNum,
				StartOffset: r.StartOffset,
				EndOffset:   r.EndOffset,
				BM25Score:   0, // not in lexical results
				SourceType:  "semantic",
			}
			scores[r.ChunkID] = entry
		} else {
			entry.SourceType = "hybrid"
		}
		entry.Score += 1.0 / float64(k+(rank+1)) // rank is 1-based
	}

	// Collect entries for score normalization.
	entries := make([]rrfEntry, 0, len(scores))
	for _, e := range scores {
		entries = append(entries, *e)
	}

	// Min-max normalize BM25 scores over the pool.
	// BM25 in SQLite FTS5: lower is better (distance-like). We invert so higher is better.
	normalizeBM25(entries)

	// Normalize RRF scores to [0, 1] range for fair blending with BM25.
	normalizeRRF(entries)

	// Calibrate final score: 0.7·rrf_norm + 0.3·bm25_norm.
	for i := range entries {
		entries[i].Score = 0.7*entries[i].Score + 0.3*entries[i].BM25Score
	}

	// Sort by descending calibrated score.
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Score != entries[j].Score {
			return entries[i].Score > entries[j].Score // descending score
		}
		return entries[i].ChunkID < entries[j].ChunkID // ascending ChunkID tiebreak
	})

	// Trim to topN (topN <= 0 means no truncation).
	if topN > 0 && len(entries) > topN {
		entries = entries[:topN]
	}

	// Build final SearchResult slice with rank assigned.
	results := make([]SearchResult, 0, len(entries))
	for i, e := range entries {
		results = append(results, SearchResult{
			ChunkID:     e.ChunkID,
			ChunkText:   e.ChunkText,
			DocumentID:  e.DocumentID,
			SequenceNum: e.SequenceNum,
			StartOffset: e.StartOffset,
			EndOffset:   e.EndOffset,
			Score:       e.Score,
			Rank:        i + 1, // 1-based rank
			SourceType:  e.SourceType,
			Metadata:    make(map[string]interface{}),
		})
	}

	return results
}

// normalizeBM25 applies min-max normalization to BM25 scores.
// BM25 in SQLite FTS5 is "lower-is-better" (distance-like). We invert first,
// then apply min-max so the range becomes [0, 1] with higher being better.
func normalizeBM25(entries []rrfEntry) {
	if len(entries) == 0 {
		return
	}

	// Find min and max of raw BM25 scores only over entries that have actual BM25 data.
	// Semantic-only entries (BM25Score=0) are excluded from range computation to avoid
	// skewing normalization in mixed pools.
	minBM25, maxBM25 := math.MaxFloat64, -math.MaxFloat64
	hasBM25Data := false
	for i := range entries {
		if entries[i].SourceType == "lexical" || entries[i].SourceType == "hybrid" {
			if entries[i].BM25Score < minBM25 {
				minBM25 = entries[i].BM25Score
			}
			if entries[i].BM25Score > maxBM25 {
				maxBM25 = entries[i].BM25Score
			}
			hasBM25Data = true
		}
	}

	// If no entry has BM25 data (all semantic-only), give every entry neutral value.
	if !hasBM25Data {
		for i := range entries {
			entries[i].BM25Score = 0.5
		}
		return
	}

	rangeBM25 := maxBM25 - minBM25
	if rangeBM25 == 0 {
		// All BM25 scores are equal (or all zero); set to neutral value.
		for i := range entries {
			entries[i].BM25Score = 0.5
		}
		return
	}

	// Invert (lower-is-better → higher-is-better) and normalize to [0, 1].
	for i := range entries {
		if entries[i].BM25Score == 0 && entries[i].SourceType != "lexical" && entries[i].SourceType != "hybrid" {
			// Semantic-only result: no BM25 data, use neutral value.
			entries[i].BM25Score = 0.5
		} else {
			// Invert: (max - score) / range → higher original BM25 (worse) becomes lower normalized.
			// But we want better BM25 (lower raw) to map to higher normalized.
			entries[i].BM25Score = (maxBM25 - entries[i].BM25Score) / rangeBM25
		}
	}
}

// normalizeRRF applies min-max normalization to RRF scores so they fall in [0, 1]
// for fair blending with normalized BM25 scores.
func normalizeRRF(entries []rrfEntry) {
	if len(entries) == 0 {
		return
	}

	minScore, maxScore := entries[0].Score, entries[0].Score
	for i := 1; i < len(entries); i++ {
		if entries[i].Score < minScore {
			minScore = entries[i].Score
		}
		if entries[i].Score > maxScore {
			maxScore = entries[i].Score
		}
	}

	rangeScore := maxScore - minScore
	if rangeScore == 0 {
		// All RRF scores equal; set to neutral value.
		for i := range entries {
			entries[i].Score = 0.5
		}
		return
	}

	for i := range entries {
		entries[i].Score = (entries[i].Score - minScore) / rangeScore
	}
}
