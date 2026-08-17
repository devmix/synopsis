package benchmark

import (
	"context"
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/graph"
	"github.com/devmix/synopsis/internal/search"
)

// TestRunner_Smoke exercises the full benchmark path end to end: generated data,
// real FTS5 + sqlite-vec indexes (via Fill), a live searcher and graph, then one
// pass of every tool case. It asserts that each case completes at least one
// successful measured unit against the filled database.
func TestRunner_Smoke(t *testing.T) {
	t.Parallel()

	db := openBenchmarkTestDB(t)
	ctx := context.Background()

	ds, err := NewGenerator(7).Generate(tinyScale)
	if err != nil {
		t.Fatalf("generate tiny dataset: %v", err)
	}
	if _, err := Fill(ctx, db, ds, newFakeProvider(8), FillOptions{}); err != nil {
		t.Fatalf("fill database: %v", err)
	}

	g, gStats, err := graph.NewGraphFromDB(ctx, db)
	if err != nil {
		t.Fatalf("load graph: %v", err)
	}
	if gStats.NodeCount == 0 {
		t.Fatal("graph loaded with no nodes; entity data did not reach the graph")
	}

	searcher := search.NewSearcher(
		db, dao.NewChunkDAO(db), dao.NewDocumentDAO(db), dao.NewChunkEntityDAO(db),
		newFakeProvider(8),
		config.SearchConfig{
			RRFK:           60,
			LexicalTopK:    10,
			SemanticTopK:   10,
			FinalTopK:      5,
			EnableLexical:  true,
			EnableSemantic: true,
			TimeoutMs:      2000,
		},
		config.GraphConfig{}, // graph expansion off; handlers only need the raw graph
		g,
	)

	runner := NewRunner(db, searcher, g, ds.Samples)

	const iterations = 3
	result, err := runner.Run(ctx, Options{Iterations: iterations})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.Tools) != 14 { // 12 MCP tools + search_lexical + search_semantic
		t.Fatalf("got %d tool cases, want 14: %v", len(result.Tools), namesOf(result))
	}

	for _, ts := range result.Tools {
		if ts.Calls+ts.Errors != iterations {
			t.Errorf("%s: calls(%d)+errors(%d) != iterations(%d)", ts.Name, ts.Calls, ts.Errors, iterations)
		}
		if ts.Calls == 0 {
			t.Errorf("%s had no successful call; first error: %s", ts.Name, orNone(ts.FirstError))
			continue
		}
		if ts.AvgMs < 0 || ts.MinMs > ts.MaxMs {
			t.Errorf("%s has inconsistent latency stats: %+v", ts.Name, ts)
		}
	}

	// Paginated tools fetch at least one page per successful unit.
	for _, name := range []string{"catalog_documents", "catalog_entities", "search_entities_by_type", "search_facts"} {
		if ts := findStats(result, name); ts != nil && ts.Calls > 0 && ts.Pages < ts.Calls {
			t.Errorf("%s fetched %d pages for %d successful units (want >= calls)", name, ts.Pages, ts.Calls)
		}
	}

	// The search cases must not lose iterations to FTS5/vec errors: a regression here
	// silently skews latency and QPS metrics (failed units are excluded from them).
	for _, name := range []string{"search", "search_lexical", "search_semantic"} {
		if ts := findStats(result, name); ts != nil && ts.Errors > 0 {
			t.Errorf("%s had %d failed iterations; first error: %s", name, ts.Errors, orNone(ts.FirstError))
		}
	}
}

// TestRunner_AllSampleQueriesParseInFTS5 runs every generated sample query through the
// lexical (FTS5) search path and fails if any of them is rejected. It guards against
// query shapes FTS5 cannot parse — e.g. a bare numeric token from a de-duplicated entity
// name, which FTS5 reads as a column reference ('no such column: 10').
func TestRunner_AllSampleQueriesParseInFTS5(t *testing.T) {
	t.Parallel()

	db := openBenchmarkTestDB(t)
	ctx := context.Background()

	ds, err := NewGenerator(7).Generate(tinyScale)
	if err != nil {
		t.Fatalf("generate tiny dataset: %v", err)
	}
	if _, err := Fill(ctx, db, ds, newFakeProvider(8), FillOptions{}); err != nil {
		t.Fatalf("fill database: %v", err)
	}

	searcher := search.NewSearcher(
		db, dao.NewChunkDAO(db), dao.NewDocumentDAO(db), dao.NewChunkEntityDAO(db),
		newFakeProvider(8),
		config.SearchConfig{RRFK: 60, LexicalTopK: 10, SemanticTopK: 10, FinalTopK: 5, TimeoutMs: 5000},
		config.GraphConfig{},
		nil, // graph expansion off; only the lexical path is exercised here
	)

	for i, q := range ds.Samples.Queries {
		if _, err := searcher.LexicalSearch(ctx, q, 10, ""); err != nil {
			t.Errorf("sample query %d (%q) rejected by FTS5: %v", i, q, err)
		}
	}
}

func namesOf(result *RunResult) []string {
	names := make([]string, 0, len(result.Tools))
	for _, ts := range result.Tools {
		names = append(names, ts.Name)
	}
	return names
}

func findStats(result *RunResult, name string) *ToolStats {
	for i := range result.Tools {
		if result.Tools[i].Name == name {
			return &result.Tools[i]
		}
	}
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "(none recorded)"
	}
	return s
}
