package benchmark

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

func TestParseScale(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		wantErr  bool
		wantDocs int
	}{
		{name: "small", wantDocs: Scales["small"].Documents},
		{name: "medium", wantDocs: Scales["medium"].Documents},
		{name: "large", wantDocs: Scales["large"].Documents},
		{name: " SMALL ", wantDocs: Scales["small"].Documents},
		{name: "", wantErr: true},
		{name: "huge", wantErr: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			scale, err := ParseScale(tt.name)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseScale(%q) expected error, got scale %+v", tt.name, scale)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseScale(%q): %v", tt.name, err)
			}
			if scale.Documents != tt.wantDocs {
				t.Errorf("scale.Documents = %d, want %d", scale.Documents, tt.wantDocs)
			}
			if scale.Name == "" || scale.Chunks < scale.Documents {
				t.Errorf("scale %+v is malformed", scale)
			}
		})
	}
}

func TestGenerate_ExactCounts(t *testing.T) {
	t.Parallel()

	for name, scale := range Scales {
		scale := scale
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ds, err := NewGenerator(42).Generate(scale)
			if err != nil {
				t.Fatalf("Generate(%s): %v", name, err)
			}

			counts := map[string]int{
				"documents": len(ds.Documents),
				"chunks":    len(ds.Chunks),
				"entities":  len(ds.Entities),
				"facts":     len(ds.Facts),
			}
			wants := map[string]int{
				"documents": scale.Documents,
				"chunks":    scale.Chunks,
				"entities":  scale.Entities,
				"facts":     scale.Facts,
			}
			for table, want := range wants {
				if got := counts[table]; got != want {
					t.Errorf("%s: got %d rows, want %d", table, got, want)
				}
			}

			if len(ds.FactSources) != len(ds.Facts) {
				t.Errorf("fact_sources = %d, want one per fact (%d)", len(ds.FactSources), len(ds.Facts))
			}
			if ds.Samples == nil {
				t.Fatal("Generate produced no samples")
			}
			for colName, items := range map[string]int{
				"queries":    len(ds.Samples.Queries),
				"doc_ids":    len(ds.Samples.DocIDs),
				"chunk_ids":  len(ds.Samples.ChunkIDs),
				"fact_ids":   len(ds.Samples.FactIDs),
				"entity_ids": len(ds.Samples.EntityIDs),
			} {
				if items != sampleSize {
					t.Errorf("samples.%s = %d, want %d", colName, items, sampleSize)
				}
			}
			if len(ds.Samples.EntityTypes) == 0 {
				t.Error("samples.entity_types is empty")
			}
		})
	}
}

func TestGenerate_DeterministicForSameSeed(t *testing.T) {
	t.Parallel()

	scale := Scales["small"]

	first, err := NewGenerator(42).Generate(scale)
	if err != nil {
		t.Fatalf("first Generate: %v", err)
	}
	second, err := NewGenerator(42).Generate(scale)
	if err != nil {
		t.Fatalf("second Generate: %v", err)
	}

	firstJSON, err := json.Marshal(first)
	if err != nil {
		t.Fatalf("marshal first: %v", err)
	}
	secondJSON, err := json.Marshal(second)
	if err != nil {
		t.Fatalf("marshal second: %v", err)
	}
	if string(firstJSON) != string(secondJSON) {
		t.Fatal("same seed produced different datasets")
	}

	different, err := NewGenerator(43).Generate(scale)
	if err != nil {
		t.Fatalf("third Generate (different seed): %v", err)
	}
	differentJSON, err := json.Marshal(different)
	if err != nil {
		t.Fatalf("marshal third: %v", err)
	}
	if string(firstJSON) == string(differentJSON) {
		t.Fatal("different seeds produced identical datasets")
	}
}

// tinyScale is a custom scale small enough for fast referential-integrity checks.
var tinyScale = Scale{Name: "tiny", Documents: 8, Chunks: 40, Entities: 12, Facts: 6}

func TestGenerate_ReferentialIntegrity(t *testing.T) {
	t.Parallel()

	ds, err := NewGenerator(7).Generate(tinyScale)
	if err != nil {
		t.Fatalf("Generate(tiny): %v", err)
	}

	docIDs := make(map[int]bool, len(ds.Documents))
	pathSeen := map[string]int{} // path -> document id
	for i := range ds.Documents {
		d := &ds.Documents[i]
		if d.ID < 1 || docIDs[d.ID] {
			t.Errorf("invalid or duplicate document id %d", d.ID)
		}
		docIDs[d.ID] = true

		if prev, dup := pathSeen[d.OriginalPath]; dup {
			t.Errorf("duplicate original_path %q (docs %d and %d)", d.OriginalPath, prev, d.ID)
		} else if d.OriginalPath == "" {
			t.Errorf("document %d has empty original_path", d.ID)
		}
		pathSeen[d.OriginalPath] = d.ID
	}

	chunkIDs := make(map[int]bool, len(ds.Chunks))
	for i := range ds.Chunks {
		c := &ds.Chunks[i]
		if c.ID < 1 || chunkIDs[c.ID] {
			t.Errorf("invalid or duplicate chunk id %d", c.ID)
		}
		chunkIDs[c.ID] = true

		if !docIDs[c.DocID] {
			t.Errorf("chunk %d references unknown document %d", c.ID, c.DocID)
		}
		if c.SeqNum < 1 {
			t.Errorf("chunk %d has invalid sequence_num %d", c.ID, c.SeqNum)
		}
	}

	entityByID := make(map[int]*Entity, len(ds.Entities))
	entityKeySeen := map[string]int{} // domain|type|name -> entity id
	for i := range ds.Entities {
		e := &ds.Entities[i]
		if e.ID < 1 || entityByID[e.ID] != nil {
			t.Errorf("invalid or duplicate entity id %d", e.ID)
		}
		entityByID[e.ID] = e

		key := e.Domain + "|" + e.Type + "|" + e.Name
		if prev, dup := entityKeySeen[key]; dup {
			t.Errorf("duplicate (domain,type,name) %q (entities %d and %d)", key, prev, e.ID)
		}
		entityKeySeen[key] = e.ID
	}

	for i := range ds.Facts {
		f := &ds.Facts[i]
		subject, okS := entityByID[f.SubjectID]
		object, okO := entityByID[f.ObjectID]
		if !okS || !okO {
			t.Errorf("fact %d references unknown entities (%d -> %d)", f.ID, f.SubjectID, f.ObjectID)
			continue
		}
		if subject.Domain != object.Domain || subject.Domain != f.Domain {
			t.Errorf("fact %d mixes domains: fact=%s subject=%s object=%s",
				f.ID, f.Domain, subject.Domain, object.Domain)
		}
	}

	for i := range ds.FactSources {
		fs := &ds.FactSources[i]
		if !docIDs[fs.DocumentID] {
			t.Errorf("fact_source %d references unknown document %d", fs.ID, fs.DocumentID)
		}
	}

	for i := range ds.EntitySources {
		es := &ds.EntitySources[i]
		if entityByID[es.EntityID] == nil || !docIDs[es.DocumentID] {
			t.Errorf("entity_source %d references unknown row (entity=%d doc=%d)", es.ID, es.EntityID, es.DocumentID)
		}
	}

	for i := range ds.ChunkEntities {
		ce := &ds.ChunkEntities[i]
		if !chunkIDs[ce.ChunkID] {
			t.Errorf("chunk_entity references unknown chunk %d", ce.ChunkID)
		}
		if entityByID[ce.EntityID] == nil {
			t.Errorf("chunk_entity (%d,%d) references unknown entity", ce.ChunkID, ce.EntityID)
		}
	}

	for i := range ds.EntityLinks {
		el := &ds.EntityLinks[i]
		subject, okS := entityByID[el.SubjectID]
		target, okT := entityByID[el.TargetID]
		if !okS || !okT {
			t.Errorf("entity_link %d references unknown entities", el.ID)
			continue
		}
		if subject.Domain == target.Domain {
			t.Errorf("entity_link %d is intra-domain (%s)", el.ID, subject.Domain)
		}
	}

	// Unique fact triples (subject, predicate, object).
	tripleSeen := make(map[string]bool, len(ds.Facts))
	for i := range ds.Facts {
		f := &ds.Facts[i]
		triple := strconv.Itoa(f.SubjectID) + "|" + f.Predicate + "|" + strconv.Itoa(f.ObjectID)
		if tripleSeen[triple] {
			t.Errorf("duplicate fact triple %s", triple)
		}
		tripleSeen[triple] = true
	}
}

func TestFTSSafeQuery(t *testing.T) {
	t.Parallel()

	tests := []struct{ in, want string }{
		{"invoice reconciliation", "invoice reconciliation"},
		{"Policy probation period-10", "Policy probation period"}, // de-dup suffix must not leak a bare number
		{"Alice Smith 3", "Alice Smith"},
		{"feature flag procedure policy review", "feature flag procedure policy review"},
		{"42", ""}, // purely numeric input sanitizes to empty (caller substitutes a fallback)
	}
	for _, tc := range tests {
		if got := ftsSafeQuery(tc.in); got != tc.want {
			t.Errorf("ftsSafeQuery(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestGenerateQueriesAreFTS5Safe asserts no sample query contains a bare numeric token:
// FTS5 would parse such a token as a column reference and fail with 'no such column'.
// Only the small datasets are covered: they have the fewest entities per name pool, so
// de-duplication produces the most "-N" suffixed names — exactly the regression surface.
// The large scales share identical query-generation logic and would only add minutes of
// generation time to CI.
func TestGenerateQueriesAreFTS5Safe(t *testing.T) {
	t.Parallel()

	scales := []Scale{tinyScale, Scales["small"]}
	for _, seed := range []int64{7, 42} {
		for i, scale := range scales {
			ds, err := NewGenerator(seed).Generate(scale)
			if err != nil {
				t.Fatalf("generate (seed=%d idx=%d): %v", seed, i, err)
			}
			if len(ds.Samples.Queries) == 0 {
				t.Fatalf("no sample queries generated (seed=%d)", seed)
			}
			for _, q := range ds.Samples.Queries {
				assertNoBareNumericToken(t, seed, scale.Name, q)
			}
		}
	}
}

// assertNoBareNumericToken fails the test if any token of q is purely numeric.
func assertNoBareNumericToken(t *testing.T, seed int64, scaleName, q string) {
	t.Helper()
	if q == "" {
		t.Errorf("seed=%d %s: empty sample query", seed, scaleName)
		return
	}
	for _, field := range strings.Fields(q) {
		parts := strings.FieldsFunc(field, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		for _, part := range parts {
			if isDigitsOnly(part) {
				t.Errorf("seed=%d %s: query %q contains bare numeric token %q", seed, scaleName, q, part)
			}
		}
	}
}
