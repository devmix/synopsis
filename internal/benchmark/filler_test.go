package benchmark

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	"github.com/devmix/synopsis/internal/database/dao"
)

// TestMain registers the sqlite-vec extension before any database handle is
// opened, mirroring the production path (database.Open calls sqlite_vec.Auto()).
// Without this, dao.TestDB connections lack the vec0 module and every fill test
// would skip even under `make test`, where both FTS5 and sqlite-vec are compiled in.
func TestMain(m *testing.M) {
	sqlite_vec.Auto()
	os.Exit(m.Run())
}

// fakeProvider is a deterministic in-process embedding provider used to avoid
// loading an ONNX model in tests. It satisfies both benchmark.Embedder and the
// full embedding.Provider interface (used by runner_test).
type fakeProvider struct {
	dim    int
	calls  int
	maxLen int
}

func newFakeProvider(dim int) *fakeProvider { return &fakeProvider{dim: dim} }

// GenerateEmbeddings returns a deterministic vector per text.
func (f *fakeProvider) GenerateEmbeddings(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	if len(texts) > f.maxLen {
		f.maxLen = len(texts)
	}

	out := make([][]float32, 0, len(texts))
	for i, text := range texts {
		vec := make([]float32, f.dim)
		for d := 0; d < f.dim; d++ {
			// Mix the row index and a cheap hash of the text so distinct chunks
			// get distinct vectors (needed for meaningful KNN results).
			h := uint32(i+1)*2654435761 + uint32(len(text))*97 + uint32(d)*31
			vec[d] = float32(h%1000) / 1000.0
		}
		out = append(out, vec)
	}
	return out, nil
}

func (f *fakeProvider) VectorDim() int { return f.dim }

// Name implements embedding.Provider.
func (f *fakeProvider) Name() string { return "fake" }

// openBenchmarkTestDB opens a migrated test database. The sqlite-vec extension is
// registered process-wide by TestMain; t.Skip remains as a guard for builds where
// FTS5 or the vec0 module are genuinely absent (bare `go test` without the CGO
// flags from AGENTS.md).
func openBenchmarkTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, cleanFn, err := dao.TestDB(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("create test db: %v", err)
	}
	t.Cleanup(cleanFn)

	if !tableExists(t, db, ftsTableName) {
		t.Skip("FTS5 module not available — skipping benchmark fill test")
	}
	if !vecModuleAvailable(t, db) {
		t.Skip("sqlite-vec module not available — skipping benchmark fill test")
	}
	return db
}

// tableExists reports whether a (virtual) table is present in sqlite_master.
func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type IN ('table','view') AND name = ?", name).Scan(&n); err != nil {
		t.Fatalf("query sqlite_master for %s: %v", name, err)
	}
	return n > 0
}

// vecModuleAvailable probes whether the vec0 virtual table module is compiled in.
func vecModuleAvailable(t *testing.T, db *sql.DB) bool {
	t.Helper()
	if _, err := db.Exec("CREATE VIRTUAL TABLE benchmark_vec_probe USING vec0(id INTEGER PRIMARY KEY, v FLOAT[2])"); err != nil {
		return false
	}
	if _, err := db.Exec("DROP TABLE benchmark_vec_probe"); err != nil { //nolint:errcheck
		t.Fatalf("drop vec probe table: %v", err)
	}
	return true
}

func TestFill_PopulatesAllTables(t *testing.T) {
	t.Parallel()

	db := openBenchmarkTestDB(t)
	ctx := context.Background()

	ds, err := NewGenerator(7).Generate(tinyScale)
	if err != nil {
		t.Fatalf("generate tiny dataset: %v", err)
	}

	emb := newFakeProvider(8)
	progressCalls := 0
	report, err := Fill(ctx, db, ds, emb, FillOptions{
		BatchSize: 13, // 40 chunks → 4 embedding batches
		Progress: func(done, total int) {
			progressCalls++
			if done > total || (progressCalls == 1 && done != 13) {
				t.Errorf("bad progress callback state: done=%d total=%d call=%d", done, total, progressCalls)
			}
		},
	})
	if err != nil {
		t.Fatalf("Fill: %v", err)
	}

	wantCounts := map[string]int64{
		"documents":      int64(tinyScale.Documents),
		"chunks":         int64(len(ds.Chunks)),
		"entities":       int64(len(ds.Entities)),
		"facts":          int64(len(ds.Facts)),
		"fact_sources":   int64(len(ds.FactSources)),
		"entity_sources": int64(len(ds.EntitySources)),
		"chunk_entities": int64(len(ds.ChunkEntities)),
		"entity_links":   int64(len(ds.EntityLinks)),
		vecTableName:     int64(len(ds.Chunks)),
	}
	for table, want := range wantCounts {
		if got := report.Tables[table]; got != want {
			t.Errorf("table %s: got %d rows, want %d", table, got, want)
		}
	}
	if report.Vectors != len(ds.Chunks) {
		t.Errorf("report.Vectors = %d, want %d", report.Vectors, len(ds.Chunks))
	}

	// Embeddings were requested in batches of BatchSize texts.
	if emb.calls != 4 { // ceil(40/13)
		t.Errorf("embedding provider called %d times, want 4", emb.calls)
	}
	if progressCalls != emb.calls {
		t.Errorf("progress callback fired %d times, want %d (one per batch)", progressCalls, emb.calls)
	}

	// The FTS index must contain exactly one entry per chunk after rebuild.
	var ftsRows int64
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+ftsTableName).Scan(&ftsRows); err != nil {
		t.Fatalf("count fts rows: %v", err)
	}
	if ftsRows != int64(len(ds.Chunks)) {
		t.Errorf("chunks_fts rows = %d, want %d", ftsRows, len(ds.Chunks))
	}

	// KNN queries must work against the freshly built vector index.
	sampleVec := make([]float32, 8)
	for d := range sampleVec {
		sampleVec[d] = float32(d) / 7.0
	}
	var knnHits int64
	if err := db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM chunks_vec WHERE vector MATCH ? AND k = ?",
		dao.FormatVector(sampleVec), 5).Scan(&knnHits); err != nil {
		t.Fatalf("vec0 KNN query: %v", err)
	}
	if knnHits != 5 { // ceil(40/13) batches, 40 chunks → k=5 must return exactly 5 rows
		t.Errorf("KNN hits = %d, want 5", knnHits)
	}

	// AUTOINCREMENT sequences are pinned to the max explicit IDs.
	for table, maxID := range map[string]int{"documents": tinyScale.Documents, "chunks": len(ds.Chunks)} {
		var seq int64
		if err := db.QueryRowContext(ctx, "SELECT seq FROM sqlite_sequence WHERE name = ?", table).Scan(&seq); err != nil {
			t.Fatalf("read sequence for %s: %v", table, err)
		}
		if seq != int64(maxID) {
			t.Errorf("sqlite_sequence.%s = %d, want %d", table, seq, maxID)
		}
	}

	// A second fill must clear and refill cleanly (exercises the DELETE paths).
	report2, err := Fill(ctx, db, ds, emb, FillOptions{})
	if err != nil {
		t.Fatalf("second Fill: %v", err)
	}
	for table, want := range wantCounts {
		if got := report2.Tables[table]; got != want {
			t.Errorf("after refill, table %s: got %d rows, want %d", table, got, want)
		}
	}
}

// TestFill_RestoredTriggersMatchMigration guards against drift between the FTS5 sync
// triggers that Fill() restores (restoreFTSTriggers duplicates their DDL by hand) and
// their canonical definitions in migrations/001_schema.sql, which dao.TestDB has
// already applied to this database before Fill runs.
func TestFill_RestoredTriggersMatchMigration(t *testing.T) {
	t.Parallel()

	db := openBenchmarkTestDB(t)
	ctx := context.Background()

	before := ftsTriggerDefinitions(t, db) // as created by the migrations
	if len(before) != len(ftsTriggerNames) {
		t.Fatalf("expected %d FTS5 sync triggers from migrations, found %d (%v)", len(ftsTriggerNames), len(before), sortedKeys(before))
	}

	ds, err := NewGenerator(7).Generate(tinyScale)
	if err != nil {
		t.Fatalf("generate tiny dataset: %v", err)
	}
	if _, err := Fill(ctx, db, ds, newFakeProvider(8), FillOptions{}); err != nil {
		t.Fatalf("Fill: %v", err)
	}

	after := ftsTriggerDefinitions(t, db) // as restored by restoreFTSTriggers
	for _, name := range ftsTriggerNames {
		want, ok := before[name]
		if !ok {
			t.Errorf("migration did not define trigger %s", name)
			continue
		}
		got, ok := after[name]
		if !ok {
			t.Errorf("Fill did not restore trigger %s", name)
			continue
		}
		if normalizeSQL(got) != normalizeSQL(want) {
			t.Errorf("restored trigger %s drifted from migrations/001_schema.sql:\n got:  %s\n want: %s", name, normalizeSQL(got), normalizeSQL(want))
		}
	}
}

// ftsTriggerDefinitions returns the DDL of every FTS5 sync trigger in sqlite_master.
func ftsTriggerDefinitions(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()

	placeholders := make([]string, len(ftsTriggerNames))
	args := make([]any, len(ftsTriggerNames))
	for i, name := range ftsTriggerNames {
		placeholders[i] = "?"
		args[i] = name
	}
	query := "SELECT name, sql FROM sqlite_master WHERE type = 'trigger' AND name IN (" + strings.Join(placeholders, ",") + ")"

	rows, err := db.Query(query, args...)
	if err != nil {
		t.Fatalf("query trigger definitions: %v", err)
	}
	defer func() { _ = rows.Close() }() //nolint:errcheck

	out := make(map[string]string, len(ftsTriggerNames))
	for rows.Next() {
		var name, def string
		if err := rows.Scan(&name, &def); err != nil {
			t.Fatalf("scan trigger definition: %v", err)
		}
		out[name] = def
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate trigger definitions: %v", err)
	}
	return out
}

// normalizeSQL collapses whitespace so formatting differences cannot mask semantic drift.
func normalizeSQL(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// sortedKeys returns the map keys in stable order for diagnostics.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
