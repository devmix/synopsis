package dao_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/database"
	"github.com/devmix/synopsis/internal/database/dao"
)

// migrationsDir resolves the absolute path to the project's migrations directory.
func migrationsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	return dir
}

// setupTestDB creates a temp SQLite database with migrations applied.
func setupTestDB(t *testing.T) (*database.Database, func()) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	d, err := database.Open(dbPath, 1024, database.WithMigrationsDir(migrationsDir(t)))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	if err := d.Migrate(context.Background()); err != nil {
		d.Close() //nolint:errcheck
		t.Fatalf("Migrate() error = %v", err)
	}

	cleanup := func() { _ = d.Close() }
	return d, cleanup
}

// hasFTS5Table checks if the FTS5 virtual table exists (module may be unavailable).
func hasFTS5Table(t *testing.T, d *database.Database) bool {
	t.Helper()
	var count int
	err := d.DB().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chunks_fts';",
	).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// setupVecTableForTest drops the existing chunks_vec (created by migration with config dim)
// and recreates it with FLOAT[dim] for test use.
func setupVecTableForTest(t *testing.T, d *database.Database, ctx context.Context, dim int) {
	t.Helper()
	if err := d.DropVectorTable(ctx); err != nil {
		t.Fatalf("DropVectorTable: %v", err)
	}
	createSQL := fmt.Sprintf(
		"CREATE VIRTUAL TABLE chunks_vec USING vec0(chunk_id INTEGER PRIMARY KEY, vector FLOAT[%d])", dim,
	)
	if _, err := d.DB().Exec(createSQL); err != nil {
		t.Fatalf("create chunks_vec with FLOAT[%d]: %v", dim, err)
	}
}

func TestChunkDAOListAll(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		insertDocs         int
		insertChunksPerDoc int
		wantTotal          int
	}{
		{
			name:               "empty database",
			insertDocs:         0,
			insertChunksPerDoc: 0,
			wantTotal:          0,
		},
		{
			name:               "single document with chunks",
			insertDocs:         1,
			insertChunksPerDoc: 3,
			wantTotal:          3,
		},
		{
			name:               "multiple documents with chunks",
			insertDocs:         2,
			insertChunksPerDoc: 5,
			wantTotal:          10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d, cleanup := setupTestDB(t)
			defer cleanup()

			chunkDAO := dao.NewChunkDAO(d.DB())

			// Insert test documents and chunks.
			for docIdx := 0; docIdx < tt.insertDocs; docIdx++ {
				docResult, err := d.DB().Exec(
					"INSERT INTO documents (source_type, original_path) VALUES ('test', ?);",
					filepath.Join("/test/docs/doc_"+string(rune('A'+docIdx))+".md"),
				)
				if err != nil {
					t.Fatalf("insert document: %v", err)
				}
				docID, err := docResult.LastInsertId()
				if err != nil {
					t.Fatalf("get last insert id: %v", err)
				}

				for chunkIdx := 0; chunkIdx < tt.insertChunksPerDoc; chunkIdx++ {
					_, err := d.DB().Exec(
						"INSERT INTO chunks (doc_id, chunk_text, sequence_num) VALUES (?, ?, ?);",
						docID, "chunk text "+string(rune('A'+docIdx))+"-"+string(rune('0'+chunkIdx)), chunkIdx+1,
					)
					if err != nil {
						t.Fatalf("insert chunk: %v", err)
					}
				}
			}

			chunks, err := chunkDAO.ListAll(context.Background())
			if err != nil {
				t.Fatalf("ListAll() error = %v", err)
			}

			if len(chunks) != tt.wantTotal {
				t.Errorf("ListAll() returned %d chunks, want %d", len(chunks), tt.wantTotal)
			}

			// Verify ordering by id.
			for i := 1; i < len(chunks); i++ {
				if chunks[i].ID <= chunks[i-1].ID {
					t.Errorf("chunks not ordered by id: chunk[%d].id=%d <= chunk[%d].id=%d",
						i, chunks[i].ID, i-1, chunks[i-1].ID)
				}
			}
		})
	}
}

// TestChunkDAOSearchFTS_DomainFilter verifies that SearchFTS filters by domain
// at the SQL level (before LIMIT). Only chunks belonging to documents in the
// specified domain are returned. Multi-domain documents match any of their domains.
func TestChunkDAOSearchFTS_DomainFilter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		domain    string
		wantCount int
	}{
		{
			name:      "domain hr returns only hr chunks",
			domain:    "hr",
			wantCount: 2, // doc1 has domain ["hr"], 2 chunks
		},
		{
			name:      "domain engineering returns only engineering chunks",
			domain:    "engineering",
			wantCount: 3, // doc2 has domain ["engineering"], 3 chunks
		},
		{
			name:      "multi-domain document matches hr",
			domain:    "hr",
			wantCount: 2, // only doc1 is in hr; doc3 is multi-domain but not hr
		},
		{
			name:      "domain policy returns multi-domain chunks",
			domain:    "policy",
			wantCount: 1, // doc3 has domain ["hr","policy"], 1 chunk
		},
		{
			name:      "nonexistent domain returns empty",
			domain:    "product",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d, cleanup := setupTestDB(t)
			defer cleanup()

			if !hasFTS5Table(t, d) {
				t.Skip("FTS5 module not available — skipping FTS domain filter test")
			}

			chunkDAO := dao.NewChunkDAO(d.DB())

			// Insert doc1 with domain ["hr"] in metadata_json, 2 chunks.
			metaJSON1 := `{"domain":"hr"}`
			if _, err := d.DB().Exec(
				`INSERT INTO documents (source_type, original_path, metadata_json) VALUES ('test', ?, ?);`,
				"/test/doc_hr.md", &metaJSON1,
			); err != nil {
				t.Fatalf("insert doc1: %v", err)
			}
			var docID1 int
			if err := d.DB().QueryRow("SELECT last_insert_rowid()").Scan(&docID1); err != nil {
				t.Fatalf("get last insert id: %v", err)
			}

			for i := 0; i < 2; i++ {
				if _, err := d.DB().Exec(
					`INSERT INTO chunks (doc_id, chunk_text, sequence_num) VALUES (?, ?, ?);`,
					docID1, fmt.Sprintf("hr policy document chunk %d", i), i+1,
				); err != nil {
					t.Fatalf("insert chunk: %v", err)
				}
			}

			// Insert doc2 with domain ["engineering"] in metadata_json, 3 chunks.
			metaJSON2 := `{"domain":"engineering"}`
			if _, err := d.DB().Exec(
				`INSERT INTO documents (source_type, original_path, metadata_json) VALUES ('test', ?, ?);`,
				"/test/doc_eng.md", &metaJSON2,
			); err != nil {
				t.Fatalf("insert doc2: %v", err)
			}
			var docID2 int
			if err := d.DB().QueryRow("SELECT last_insert_rowid()").Scan(&docID2); err != nil {
				t.Fatalf("get last insert id: %v", err)
			}

			for i := 0; i < 3; i++ {
				if _, err := d.DB().Exec(
					`INSERT INTO chunks (doc_id, chunk_text, sequence_num) VALUES (?, ?, ?);`,
					docID2, fmt.Sprintf("engineering spec chunk %d", i), i+1,
				); err != nil {
					t.Fatalf("insert chunk: %v", err)
				}
			}

			// Insert doc3 with domain ["hr","policy"] in metadata_json, 1 chunk (multi-domain).
			metaJSON3 := `{"domain":["hr","policy"]}`
			if _, err := d.DB().Exec(
				`INSERT INTO documents (source_type, original_path, metadata_json) VALUES ('test', ?, ?);`,
				"/test/doc_multi.md", &metaJSON3,
			); err != nil {
				t.Fatalf("insert doc3: %v", err)
			}
			var docID3 int
			if err := d.DB().QueryRow("SELECT last_insert_rowid()").Scan(&docID3); err != nil {
				t.Fatalf("get last insert id: %v", err)
			}

			if _, err := d.DB().Exec(
				`INSERT INTO chunks (doc_id, chunk_text, sequence_num) VALUES (?, ?, ?);`,
				docID3, "multi domain policy and hr content", 1,
			); err != nil {
				t.Fatalf("insert chunk: %v", err)
			}

			chunks, err := chunkDAO.SearchFTS(context.Background(), "chunk", 20, tt.domain)
			if err != nil {
				t.Fatalf("SearchFTS() error = %v", err)
			}

			if len(chunks) != tt.wantCount {
				t.Errorf("SearchFTS(domain=%q) returned %d chunks, want %d", tt.domain, len(chunks), tt.wantCount)
			}
		})
	}
}

// TestChunkDAOSearchFTS_NoDomainBehaviorUnchanged verifies that SearchFTS without
// a domain filter returns all matching chunks (backward compatibility).
func TestChunkDAOSearchFTS_NoDomainBehaviorUnchanged(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	if !hasFTS5Table(t, d) {
		t.Skip("FTS5 module not available — skipping FTS no-domain test")
	}

	chunkDAO := dao.NewChunkDAO(d.DB())

	// Insert documents with different domains.
	metaJSON1 := `{"domain":"hr"}`
	if _, err := d.DB().Exec(
		`INSERT INTO documents (source_type, original_path, metadata_json) VALUES ('test', ?, ?);`,
		"/test/doc_hr.md", &metaJSON1,
	); err != nil {
		t.Fatalf("insert doc: %v", err)
	}
	var docID1 int
	if err := d.DB().QueryRow("SELECT last_insert_rowid()").Scan(&docID1); err != nil {
		t.Fatalf("get last insert id: %v", err)
	}

	metaJSON2 := `{"domain":"engineering"}`
	if _, err := d.DB().Exec(
		`INSERT INTO documents (source_type, original_path, metadata_json) VALUES ('test', ?, ?);`,
		"/test/doc_eng.md", &metaJSON2,
	); err != nil {
		t.Fatalf("insert doc: %v", err)
	}
	var docID2 int
	if err := d.DB().QueryRow("SELECT last_insert_rowid()").Scan(&docID2); err != nil {
		t.Fatalf("get last insert id: %v", err)
	}

	// Insert chunks in both documents.
	for i := 0; i < 3; i++ {
		if _, err := d.DB().Exec(
			`INSERT INTO chunks (doc_id, chunk_text, sequence_num) VALUES (?, ?, ?);`,
			docID1, fmt.Sprintf("hr document content %d", i), i+1,
		); err != nil {
			t.Fatalf("insert chunk: %v", err)
		}
		if _, err := d.DB().Exec(
			`INSERT INTO chunks (doc_id, chunk_text, sequence_num) VALUES (?, ?, ?);`,
			docID2, fmt.Sprintf("engineering document content %d", i), i+1,
		); err != nil {
			t.Fatalf("insert chunk: %v", err)
		}
	}

	// Without domain filter, should return all 6 chunks.
	chunks, err := chunkDAO.SearchFTS(context.Background(), "document", 20, "")
	if err != nil {
		t.Fatalf("SearchFTS() error = %v", err)
	}

	if len(chunks) != 6 {
		t.Errorf("SearchFTS(domain='') returned %d chunks, want 6 (all domains)", len(chunks))
	}
}

// TestChunkDAOSearchVector_DomainFilter verifies that SearchVector filters by domain
// at the SQL level. Only chunks belonging to documents in the specified domain are returned.
func TestChunkDAOSearchVector_DomainFilter(t *testing.T) {
	t.Parallel()

	d, cleanup := setupTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// Drop the 1024-dim chunks_vec created by migration and recreate with FLOAT[4].
	setupVecTableForTest(t, d, ctx, 4)

	chunkDAO := dao.NewChunkDAO(d.DB())

	// Insert doc1 with domain ["hr"], 2 chunks.
	metaJSON1 := `{"domain":"hr"}`
	if _, err := d.DB().Exec(
		`INSERT INTO documents (source_type, original_path, metadata_json) VALUES ('test', ?, ?);`,
		"/test/doc_hr.md", &metaJSON1,
	); err != nil {
		t.Fatalf("insert doc: %v", err)
	}
	var docID1 int
	if err := d.DB().QueryRow("SELECT last_insert_rowid()").Scan(&docID1); err != nil {
		t.Fatalf("get last insert id: %v", err)
	}

	var chunkIDs []int
	for i := 0; i < 2; i++ {
		chunkID, err := chunkDAO.Create(ctx, dao.Chunk{
			DocID:       docID1,
			ChunkText:   fmt.Sprintf("hr policy content %d", i),
			SequenceNum: i + 1,
		})
		if err != nil {
			t.Fatalf("create chunk: %v", err)
		}
		chunkIDs = append(chunkIDs, chunkID)
	}

	// Insert doc2 with domain ["engineering"], 3 chunks.
	metaJSON2 := `{"domain":"engineering"}`
	if _, err := d.DB().Exec(
		`INSERT INTO documents (source_type, original_path, metadata_json) VALUES ('test', ?, ?);`,
		"/test/doc_eng.md", &metaJSON2,
	); err != nil {
		t.Fatalf("insert doc: %v", err)
	}
	var docID2 int
	if err := d.DB().QueryRow("SELECT last_insert_rowid()").Scan(&docID2); err != nil {
		t.Fatalf("get last insert id: %v", err)
	}

	for i := 0; i < 3; i++ {
		chunkID, err := chunkDAO.Create(ctx, dao.Chunk{
			DocID:       docID2,
			ChunkText:   fmt.Sprintf("engineering spec content %d", i),
			SequenceNum: i + 1,
		})
		if err != nil {
			t.Fatalf("create chunk: %v", err)
		}
		chunkIDs = append(chunkIDs, chunkID)
	}

	// Insert vectors for all chunks.
	vector := []float32{0.1, 0.2, 0.3, 0.4}
	for _, id := range chunkIDs {
		if err := chunkDAO.UpsertVector(ctx, id, vector); err != nil {
			t.Fatalf("upsert vector: %v", err)
		}
	}

	tests := []struct {
		name      string
		domain    string
		wantCount int
	}{
		{
			name:      "domain hr returns only hr chunks",
			domain:    "hr",
			wantCount: 2,
		},
		{
			name:      "domain engineering returns only engineering chunks",
			domain:    "engineering",
			wantCount: 3,
		},
		{
			name:      "no domain returns all chunks",
			domain:    "",
			wantCount: 5,
		},
		{
			name:      "nonexistent domain returns empty",
			domain:    "product",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks, err := chunkDAO.SearchVector(ctx, vector, 10, tt.domain)
			if err != nil {
				t.Fatalf("SearchVector() error = %v", err)
			}

			if len(chunks) != tt.wantCount {
				t.Errorf("SearchVector(domain=%q) returned %d chunks, want %d", tt.domain, len(chunks), tt.wantCount)
			}
		})
	}
}

// TestFormatVector verifies that FormatVector preserves full float32 precision
// using strconv.FormatFloat with 'g' format instead of fixed-point "%f".
func TestFormatVector(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		vector   []float32
		wantJSON []any // expected JSON array elements (numbers or strings for scientific notation)
	}{
		{
			name:   "empty vector",
			vector: []float32{},
			wantJSON: nil,
		},
		{
			name:     "zero value",
			vector:   []float32{0},
			wantJSON: []any{json.Number("0")},
		},
		{
			name:     "negative zero",
			vector:   []float32{-0},
			wantJSON: []any{json.Number("0")},
		},
		{
			name:     "positive one",
			vector:   []float32{1.0},
			wantJSON: []any{json.Number("1")},
		},
		{
			name:     "negative half",
			vector:   []float32{-0.5},
			wantJSON: []any{json.Number("-0.5")},
		},
		{
			name:     "small value 1e-7 must not round to zero",
			vector:   []float32{1e-7},
			wantJSON: nil, // we check it's non-zero in the subtest
		},
		{
			name:     "high precision value preserves significant digits",
			vector:   []float32{0.12345678},
			wantJSON: nil, // checked by round-trip below
		},
		{
			name:     "max float32",
			vector:   []float32{math.MaxFloat32},
			wantJSON: nil,
		},
		{
			name:     "smallest positive normal float32",
			vector:   []float32{math.SmallestNonzeroFloat32},
			wantJSON: nil,
		},
		{
			name:     "negative max float32",
			vector:   []float32{-math.MaxFloat32},
			wantJSON: nil,
		},
		{
			name:     "mixed values including small and large",
			vector:   []float32{0.12345678, 1e-7, -0.5, 1.0, 0, math.MaxFloat32},
			wantJSON: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := dao.FormatVector(tt.vector)

			// Verify result is valid JSON array.
			var parsed []json.RawMessage
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("FormatVector(%v) produced invalid JSON %q: %v", tt.vector, result, err)
			}

			if len(parsed) != len(tt.vector) {
				t.Errorf("FormatVector(%v) = %s (len=%d), want length %d",
					tt.vector, result, len(parsed), len(tt.vector))
			}

			// For cases with explicit expected values.
			if tt.wantJSON != nil {
				var got []any
				if err := json.Unmarshal([]byte(result), &got); err != nil {
					t.Fatalf("parse result: %v", err)
				}
				for i, want := range tt.wantJSON {
					if i >= len(got) {
						break
					}
					switch w := want.(type) {
					case json.Number:
						gStr := fmt.Sprintf("%v", got[i])
						if gStr != w.String() {
							t.Errorf("element[%d] = %s, want %s", i, gStr, w)
						}
					default:
						if got[i] != w {
							t.Errorf("element[%d] = %v, want %v", i, got[i], w)
						}
					}
				}
			}

			// Special check: 1e-7 must not be serialized as "0".
			if tt.name == "small value 1e-7 must not round to zero" {
				var nums []float64
				if err := json.Unmarshal([]byte(result), &nums); err != nil {
					t.Fatalf("parse result: %v", err)
				}
				if len(nums) > 0 && nums[0] == 0 {
					t.Errorf("1e-7 was serialized as zero in %q", result)
				}
			}

			// Round-trip check for all values.
			var roundTripped []float32
			if err := json.Unmarshal([]byte(result), &roundTripped); err != nil {
				t.Fatalf("round-trip parse failed: %v", err)
			}
			for i, want := range tt.vector {
				if roundTripped[i] != want {
					t.Errorf("round-trip element[%d]: got %v (bit=%08x), want %v (bit=%08x)",
						i, roundTripped[i], math.Float32bits(roundTripped[i]),
						want, math.Float32bits(want))
				}
			}
		})
	}
}

// TestFormatVector_JSONCompatibility ensures output is parseable as a JSON array of numbers.
func TestFormatVector_JSONCompatibility(t *testing.T) {
	t.Parallel()

	vectors := [][]float32{
		{},
		{0},
		{1, 2, 3},
		{-0.5, 0.5},
		{math.MaxFloat32, -math.MaxFloat32},
		{1e-7, 0.12345678, math.SmallestNonzeroFloat32},
	}

	for i, vec := range vectors {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			result := dao.FormatVector(vec)

			var parsed []float64
			if err := json.Unmarshal([]byte(result), &parsed); err != nil {
				t.Fatalf("FormatVector produced invalid JSON %q: %v", result, err)
			}

			if len(parsed) != len(vec) {
				t.Errorf("length mismatch: got %d, want %d", len(parsed), len(vec))
			}
		})
	}
}
