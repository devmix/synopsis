// Package benchmark implements deterministic synthetic data generation and
// load testing of the Synopsis MCP tool handlers. The generator produces a
// complete dataset (documents, chunks, entities, facts, cross-domain links)
// with a seeded PRNG so that runs with the same seed are byte-identical.
package benchmark

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"unicode"
)

// Scale defines the size of a generated dataset.
type Scale struct {
	Name      string `json:"name"`
	Documents int    `json:"documents"`
	Chunks    int    `json:"chunks"`
	Entities  int    `json:"entities"`
	Facts     int    `json:"facts"`
}

// Scales holds the predefined dataset sizes.
var Scales = map[string]Scale{
	"small":  {Name: "small", Documents: 500, Chunks: 10_000, Entities: 2_000, Facts: 5_000},
	"medium": {Name: "medium", Documents: 5_000, Chunks: 100_000, Entities: 20_000, Facts: 10_000},
	"large":  {Name: "large", Documents: 10_000, Chunks: 200_000, Entities: 40_000, Facts: 20_000},
}

// ParseScale returns the predefined scale with the given name.
func ParseScale(name string) (Scale, error) {
	s, ok := Scales[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return Scale{}, fmt.Errorf("unknown scale %q, want one of: small, medium, large", name)
	}
	return s, nil
}

// Domains is the ordered list of domains the generated data spans.
var Domains = []string{"hr", "product", "engineering", "finance", "security"}

// entityTypesByDomain lists the entity types created in each domain.
var entityTypesByDomain = map[string][]string{
	"hr":          {"employee", "department", "policy"},
	"product":     {"feature", "release", "requirement"},
	"engineering": {"system", "service", "api"},
	"finance":     {"account", "budget", "vendor"},
	"security":    {"vulnerability", "certificate", "audit_rule"},
}

// predicatesByDomain lists fact predicates per domain.
var predicatesByDomain = map[string][]string{
	"hr":          {"works_in", "manages", "reports_to", "is_governed_by", "complies_with"},
	"product":     {"belongs_to", "depends_on", "blocks", "replaces", "satisfies"},
	"engineering": {"calls", "hosts", "deploys", "integrates_with", "fails_over_to"},
	"finance":     {"charges_to", "budgeted_under", "invoiced_by", "approved_by", "settled_via"},
	"security":    {"affects", "mitigated_by", "detected_by", "audited_by", "scopes_access_to"},
}

// domainVocabulary holds realistic terms per domain used to build chunk text.
var domainVocabulary = map[string][]string{
	"hr":          {"hiring", "vacation", "severance", "onboarding", "performance review", "compensation", "benefits", "leave of absence", "termination notice", "probation period"},
	"product":     {"roadmap", "backlog", "feature flag", "release notes", "user story", "acceptance criteria", "milestone", "sprint planning", "stakeholder review", "deprecation plan"},
	"engineering": {"deployment pipeline", "incident response", "runbook", "service level objective", "load testing", "rollback procedure", "observability stack", "capacity planning", "technical debt", "code review board"},
	"finance":     {"budget approval", "expense report", "quarterly close", "invoice reconciliation", "vendor contract", "forecasting model", "cost center", "payment terms", "audit trail", "reimbursement policy"},
	"security":    {"vulnerability scan", "access control list", "encryption standard", "incident response plan", "penetration test", "compliance audit", "certificate rotation", "data classification", "threat model", "zero trust architecture"},
}

// sentenceTemplates are filled with domain vocabulary terms to build chunk text.
var sentenceTemplates = []string{
	"The %s process must be documented and reviewed by the responsible team before approval.",
	"According to the current policy, %s applies to all departments starting from the next quarter.",
	"Each request related to %s is tracked in the central system until full resolution.",
	"The team is required to complete a training module covering %s within thirty days.",
	"%s remains a priority area, and progress is reported during regular status meetings.",
	"Exceptions for %s require written justification and sign-off from a manager.",
	"All records concerning %s are retained for at least five years in accordance with regulations.",
	"The procedure for handling %s was updated last month to reflect the new requirements.",
}

var firstNames = []string{
	"Alice", "Bob", "Carol", "David", "Elena", "Frank", "Grace", "Henry", "Irina", "Jack",
	"Kira", "Leo", "Maria", "Nikolai", "Olga", "Peter", "Quinn", "Rosa", "Sergei", "Tina",
	"Umar", "Vera", "Walter", "Xena", "Yuri", "Zoe", "Adam", "Bella", "Cyril", "Diana",
}

var lastNames = []string{
	"Anderson", "Baker", "Chen", "Dmitriev", "Evans", "Fischer", "Garcia", "Hoffman", "Ivanov", "Johnson",
	"Kim", "Larsen", "Morozova", "Novak", "Olsen", "Petrov", "Quist", "Romanov", "Schmidt", "Tanaka",
	"Ueda", "Volkov", "Wagner", "Xu", "Young", "Zaytsev", "Bergman", "Costa", "Dubois", "Eriksen",
}

// personTypes are entity types named like people (First Last).
var personTypes = map[string]bool{"employee": true}

// Document is a generated source document.
type Document struct {
	ID           int      `json:"id"`
	SourceType   string   `json:"source_type"`
	OriginalPath string   `json:"original_path"`
	Domains      []string `json:"domains,omitempty"`
	MetadataJSON string   `json:"metadata_json"`
	ContentHash  string   `json:"content_hash,omitempty"`
}

// Chunk is a generated text chunk belonging to a document.
type Chunk struct {
	ID          int    `json:"id"`
	DocID       int    `json:"doc_id"`
	SeqNum      int    `json:"sequence_num"`
	Text        string `json:"text"`
	StartOffset *int   `json:"start_offset,omitempty"`
	EndOffset   *int   `json:"end_offset,omitempty"`
}

// Entity is a generated entity with a stable 1-based ID.
type Entity struct {
	ID          int     `json:"id"`
	Type        string  `json:"type"`
	Name        string  `json:"name"`
	Domain      string  `json:"domain"`
	Description string  `json:"description,omitempty"`
	Confidence  float64 `json:"confidence"`
}

// Fact is a generated subject-predicate-object fact.
type Fact struct {
	ID        int     `json:"id"`
	SubjectID int     `json:"subject_entity_id"`
	Predicate string  `json:"predicate"`
	ObjectID  int     `json:"object_entity_id"`
	Domain    string  `json:"domain"`
	Status    string  `json:"status"`
	ValidFrom *string `json:"valid_from,omitempty"`
	ValidTo   *string `json:"valid_to,omitempty"`
	Weight    int     `json:"weight"`
	Metadata  *string `json:"metadata,omitempty"`
}

// FactSource links a fact to its source document with an exact quote.
type FactSource struct {
	ID         int    `json:"id"`
	FactID     int    `json:"fact_id"`
	DocumentID int    `json:"document_id"`
	Quote      string `json:"quote,omitempty"`
}

// EntityLink is a cross-domain link between two entities.
type EntityLink struct {
	ID           int     `json:"id"`
	SubjectID    int     `json:"subject_entity_id"`
	TargetID     int     `json:"target_entity_id"`
	RelationType string  `json:"relation_type"`
	Method       string  `json:"method"`
	Confidence   float64 `json:"confidence"`
	Evidence     string  `json:"evidence,omitempty"`
}

// ChunkEntity links a chunk with an entity mentioned in it.
type ChunkEntity struct {
	ChunkID  int `json:"chunk_id"`
	EntityID int `json:"entity_id"`
}

// EntitySource links an entity to the document where it was found.
type EntitySource struct {
	ID         int `json:"id"`
	EntityID   int `json:"entity_id"`
	DocumentID int `json:"document_id"`
}

// Dataset is a complete generated dataset ready for database fill plus
// deterministic samples used by the benchmark runner to pick tool arguments.
type Dataset struct {
	Scale         Scale          `json:"scale"`
	Seed          int64          `json:"seed"`
	Documents     []Document     `json:"documents,omitempty"`
	Chunks        []Chunk        `json:"chunks,omitempty"`
	Entities      []Entity       `json:"entities,omitempty"`
	Facts         []Fact         `json:"facts,omitempty"`
	FactSources   []FactSource   `json:"fact_sources,omitempty"`
	EntityLinks   []EntityLink   `json:"entity_links,omitempty"`
	ChunkEntities []ChunkEntity  `json:"chunk_entities,omitempty"`
	EntitySources []EntitySource `json:"entity_sources,omitempty"`

	// Samples holds the deterministic benchmark argument samples (queries, IDs, types).
	Samples *Samples `json:"samples,omitempty"`
}

// Generator produces deterministic synthetic datasets. All randomness flows
// through a single seeded PRNG, so the same seed always yields the same data.
type Generator struct {
	seed int64
	rng  *rand.Rand
}

// NewGenerator creates a Generator with the given PRNG seed.
func NewGenerator(seed int64) *Generator {
	return &Generator{seed: seed, rng: rand.New(rand.NewSource(seed))}
}

const sampleSize = 32 // number of benchmark argument samples per collection

// Generate builds a complete dataset for the given scale.
func (g *Generator) Generate(scale Scale) (*Dataset, error) {
	if scale.Documents <= 0 || scale.Chunks < scale.Documents {
		return nil, fmt.Errorf("invalid scale: need at least one chunk per document (documents=%d, chunks=%d)", scale.Documents, scale.Chunks)
	}
	if scale.Documents < len(Domains) || scale.Entities < len(Domains) {
		return nil, fmt.Errorf("invalid scale %q: need at least one document and one entity per domain (documents=%d, entities=%d, domains=%d)", scale.Name, scale.Documents, scale.Entities, len(Domains))
	}

	ds := &Dataset{Scale: scale, Seed: g.seed}

	if err := g.generateDocuments(ds); err != nil {
		return nil, err
	}
	if err := g.generateChunks(ds); err != nil {
		return nil, err
	}
	if err := g.generateEntities(ds); err != nil {
		return nil, err
	}
	g.generateChunkEntities(ds)
	if err := g.generateFacts(ds); err != nil {
		return nil, err
	}
	g.generateFactSources(ds)
	g.generateEntitySources(ds)
	if err := g.generateEntityLinks(ds); err != nil {
		return nil, err
	}
	g.buildSamples(ds)

	return ds, nil
}

// generateDocuments creates one document per ID with a single primary domain.
// Documents are allocated in contiguous per-domain blocks (like entities) so
// that docRangesByDomain can map domain -> ID range for fact sources and
// entity sources.
func (g *Generator) generateDocuments(ds *Dataset) error {
	ds.Documents = make([]Document, 0, ds.Scale.Documents)

	base := ds.Scale.Documents / len(Domains)
	id := 0
	for di, domain := range Domains {
		count := base
		if di < ds.Scale.Documents%len(Domains) {
			count++
		}

		vocab := domainVocabulary[domain]
		for n := 0; n < count; n++ {
			id++

			ext := "md"
			sourceType := "markdown"
			if (n+di)%5 == 4 { // stable mix of source types across domains
				ext = "json"
				sourceType = "json"
			}

			t1 := vocab[g.rng.Intn(len(vocab))]
			t2 := vocab[g.rng.Intn(len(vocab))]
			title := titleCase(t1) + " and " + t2 + ": internal guidelines"

			ds.Documents = append(ds.Documents, Document{
				ID:           id,
				SourceType:   sourceType,
				OriginalPath: fmt.Sprintf("/synthetic/%s/doc-%06d.%s", domain, id, ext),
				Domains:      []string{domain},
				MetadataJSON: fmt.Sprintf(`{"title":%q,"domain":[%q]}`, title, domain),
			})
		}
	}

	if id != ds.Scale.Documents {
		return fmt.Errorf("generated %d documents, want %d", id, ds.Scale.Documents)
	}
	return nil
}

// generateChunks fills chunks for every document and computes realistic
// start/end offsets within each document's full text.
func (g *Generator) generateChunks(ds *Dataset) error {
	base := ds.Scale.Chunks / ds.Scale.Documents
	remainder := ds.Scale.Chunks % ds.Scale.Documents

	chunkID := 0
	for i := range ds.Documents {
		doc := &ds.Documents[i]
		count := base + 1
		if i >= remainder {
			count = base
		}
		if count <= 0 {
			return fmt.Errorf("document %d got a non-positive chunk count", doc.ID)
		}

		fullText := strings.Builder{}
		for seq := 1; seq <= count; seq++ {
			chunkID++
			text := g.chunkText(doc.Domains[0])

			start := fullText.Len()
			if start > 0 {
				fullText.WriteString("\n\n")
				start += 2
			}
			fullText.WriteString(text)
			end := fullText.Len()

			s, e := start, end
			ds.Chunks = append(ds.Chunks, Chunk{
				ID:          chunkID,
				DocID:       doc.ID,
				SeqNum:      seq,
				Text:        text,
				StartOffset: &s,
				EndOffset:   &e,
			})
		}

		sum := sha256.Sum256([]byte(fullText.String()))
		doc.ContentHash = hex.EncodeToString(sum[:])
	}

	if chunkID != ds.Scale.Chunks {
		return fmt.Errorf("generated %d chunks, want %d", chunkID, ds.Scale.Chunks)
	}
	return nil
}

// generateEntities creates entities distributed evenly across domains.
func (g *Generator) generateEntities(ds *Dataset) error {
	base := ds.Scale.Entities / len(Domains)
	id := 0
	usedNames := make(map[string]bool, ds.Scale.Entities) // key: domain|type|name

	for di, domain := range Domains {
		count := base
		if di < ds.Scale.Entities%len(Domains) {
			count++
		}

		types := entityTypesByDomain[domain]
		vocab := domainVocabulary[domain]
		for n := 0; n < count; n++ {
			id++
			entityType := types[n%len(types)]
			keyPrefix := domain + "|" + entityType

			var name string
			if personTypes[entityType] {
				name = g.personName(usedNames, keyPrefix)
			} else {
				baseName := titleCase(entityType) + " " + vocab[g.rng.Intn(len(vocab))]
				name = baseName
				for k := 2; usedNames[keyPrefix+"|"+name]; k++ {
					name = fmt.Sprintf("%s-%d", baseName, k)
				}
			}

			key := keyPrefix + "|" + name
			if usedNames[key] {
				return fmt.Errorf("duplicate entity generated: %s", key)
			}
			usedNames[key] = true

			ds.Entities = append(ds.Entities, Entity{
				ID:          id,
				Type:        entityType,
				Name:        name,
				Domain:      domain,
				Description: fmt.Sprintf("%s entity in the %s domain covering %s.", titleCase(entityType), domain, vocab[(n+3)%len(vocab)]),
				Confidence:  round4(0.7 + g.rng.Float64()*0.29),
			})
		}
	}

	if id != ds.Scale.Entities {
		return fmt.Errorf("generated %d entities, want %d", id, ds.Scale.Entities)
	}

	return nil
}

// personName builds a unique "First Last" name for the given key prefix
// (domain|type), using the shared used-set.
func (g *Generator) personName(usedNames map[string]bool, prefix string) string {
	for k := 0; ; k++ {
		name := firstNames[g.rng.Intn(len(firstNames))] + " " + lastNames[g.rng.Intn(len(lastNames))]
		if k > 0 {
			name = fmt.Sprintf("%s %d", name, k)
		}
		if !usedNames[prefix+"|"+name] {
			return name
		}
	}
}

// generateFacts creates facts using a mixed-radix enumeration over the
// (subject, predicate, object) space per domain. The stride keeps subjects and
// objects spread out while guaranteeing the unique constraint
// UNIQUE(subject_entity_id, object_entity_id, predicate).
func (g *Generator) generateFacts(ds *Dataset) error {
	entityRange := idRangesByDomain(ds.Entities, func(e Entity) string { return e.Domain })

	ds.Facts = make([]Fact, 0, ds.Scale.Facts)
	factIndexInDomain := make(map[string]int, len(Domains))

	for i := 1; i <= ds.Scale.Facts; i++ {
		domain := Domains[(i-1)%len(Domains)]
		erange := entityRange[domain]
		eCount := erange[1] - erange[0]
		preds := predicatesByDomain[domain]

		j := factIndexInDomain[domain]
		factIndexInDomain[domain] = j + 1

		combinations := eCount * len(preds) * eCount
		if combinations <= 0 {
			return fmt.Errorf("no entity combinations available in domain %q", domain)
		}
		idx := (j * 7919) % combinations

		subjectOffset := idx % eCount
		t := idx / eCount
		predIdx := t % len(preds)
		objectOffset := (t / len(preds)) % eCount

		status := "approved"
		switch r := g.rng.Float64(); {
		case r >= 0.97:
			status = "rejected"
		case r >= 0.87:
			status = "draft"
		case r >= 0.72:
			status = "pending"
		}

		fact := Fact{
			ID:        i,
			SubjectID: erange[0] + subjectOffset,
			Predicate: preds[predIdx],
			ObjectID:  erange[0] + objectOffset,
			Domain:    domain,
			Status:    status,
			Weight:    g.rng.Intn(3) + 1,
		}
		if g.rng.Float64() < 0.5 {
			validFrom := fmt.Sprintf("20%d-%02d-15", g.rng.Intn(4)+3, g.rng.Intn(12)+1)
			fact.ValidFrom = &validFrom
		}
		if fact.ValidFrom != nil && g.rng.Float64() < 0.3 {
			validTo := fmt.Sprintf("20%d-%02d-30", g.rng.Intn(4)+5, g.rng.Intn(12)+1)
			fact.ValidTo = &validTo
		}

		ds.Facts = append(ds.Facts, fact)
	}

	return nil
}

// generateFactSources links every fact to one document of its domain.
func (g *Generator) generateFactSources(ds *Dataset) {
	docRange := docRangesByDomain(ds)

	ds.FactSources = make([]FactSource, 0, len(ds.Facts))
	for i := range ds.Facts {
		fact := &ds.Facts[i]
		drange := docRange[fact.Domain]
		vocab := domainVocabulary[fact.Domain]
		quote := fmt.Sprintf("The %s procedure must be documented and reviewed.", vocab[g.rng.Intn(len(vocab))])

		ds.FactSources = append(ds.FactSources, FactSource{
			ID:         i + 1,
			FactID:     fact.ID,
			DocumentID: drange[0] + g.rng.Intn(drange[1]-drange[0]),
			Quote:      quote,
		})
	}
}

// generateEntitySources links every entity to one or two documents of its domain.
func (g *Generator) generateEntitySources(ds *Dataset) {
	docRange := docRangesByDomain(ds)

	id := 0
	for i := range ds.Entities {
		ent := &ds.Entities[i]
		drange := docRange[ent.Domain]
		nLinks := g.rng.Intn(2) + 1 // 1..2 documents per entity
		seen := make(map[int]bool, nLinks)
		for k := 0; k < nLinks; k++ {
			docID := drange[0] + g.rng.Intn(drange[1]-drange[0])
			if seen[docID] {
				continue
			}
			seen[docID] = true
			id++
			ds.EntitySources = append(ds.EntitySources, EntitySource{ID: id, EntityID: ent.ID, DocumentID: docID})
		}
	}
}

// generateEntityLinks creates cross-domain same_entity links between entities
// of different domains.
func (g *Generator) generateEntityLinks(ds *Dataset) error {
	target := ds.Scale.Entities / 25
	if target == 0 && len(ds.Entities) >= 10 {
		target = 1 // keep at least one cross-domain link for tiny datasets
	}

	entityRange := idRangesByDomain(ds.Entities, func(e Entity) string { return e.Domain })
	methods := []string{"rule", "equals", "llm"}
	usedPairs := make(map[[2]int]bool)

	for i := 0; i < target; i++ {
		domA := Domains[i%len(Domains)]
		domB := Domains[(i+1)%len(Domains)]
		if domA == domB {
			continue
		}
		rA, rB := entityRange[domA], entityRange[domB]

		var subjectID, targetID int
		ok := false
		for attempt := 0; attempt < 64 && !ok; attempt++ {
			subjectID = rA[0] + g.rng.Intn(rA[1]-rA[0])
			targetID = rB[0] + g.rng.Intn(rB[1]-rB[0])
			if subjectID != targetID && !usedPairs[[2]int{subjectID, targetID}] {
				ok = true
			}
		}
		if !ok {
			continue // extremely unlikely; skip to stay within the unique constraint
		}
		usedPairs[[2]int{subjectID, targetID}] = true

		ds.EntityLinks = append(ds.EntityLinks, EntityLink{
			ID:           len(ds.EntityLinks) + 1,
			SubjectID:    subjectID,
			TargetID:     targetID,
			RelationType: "same_entity",
			Method:       methods[g.rng.Intn(len(methods))],
			Confidence:   round4(0.7 + g.rng.Float64()*0.29),
			Evidence:     fmt.Sprintf("Cross-domain match between %q and %q generated by the benchmark loader.", domA, domB),
		})
	}

	return nil
}

// generateChunkEntities links each chunk to one or three entities of its document's domain.
func (g *Generator) generateChunkEntities(ds *Dataset) {
	entityRange := idRangesByDomain(ds.Entities, func(e Entity) string { return e.Domain })

	docDomain := make(map[int]string, len(ds.Documents))
	for i := range ds.Documents {
		docDomain[ds.Documents[i].ID] = ds.Documents[i].Domains[0]
	}

	for i := range ds.Chunks {
		chunk := &ds.Chunks[i]
		domain := docDomain[chunk.DocID]
		erange := entityRange[domain]
		if erange[0] == erange[1] {
			continue // no entities in this domain (should not happen)
		}
		count := g.rng.Intn(3) + 1 // 1..3 entities per chunk
		picked := make(map[int]bool, count)
		for k := 0; k < count; k++ {
			entityID := erange[0] + g.rng.Intn(erange[1]-erange[0])
			if picked[entityID] {
				continue
			}
			picked[entityID] = true
			ds.ChunkEntities = append(ds.ChunkEntities, ChunkEntity{ChunkID: chunk.ID, EntityID: entityID})
		}
	}
}

// buildSamples picks deterministic benchmark argument samples and attaches them to the dataset.
func (g *Generator) buildSamples(ds *Dataset) {
	samples := &Samples{
		DocIDs:   sampleIDs(g.rng, len(ds.Documents), sampleSize),
		ChunkIDs: sampleIDs(g.rng, len(ds.Chunks), sampleSize),
		FactIDs:  sampleIDs(g.rng, len(ds.Facts), sampleSize),
	}

	// Entity samples prefer entities that actually participate in facts or links.
	candidates := make(map[int]bool)
	for i := range ds.Facts {
		candidates[ds.Facts[i].SubjectID] = true
		candidates[ds.Facts[i].ObjectID] = true
	}
	for i := range ds.EntityLinks {
		candidates[ds.EntityLinks[i].SubjectID] = true
		candidates[ds.EntityLinks[i].TargetID] = true
	}

	ordered := make([]int, 0, len(candidates))
	for id := range candidates {
		ordered = append(ordered, id)
	}
	sort.Ints(ordered)

	if len(ordered) < sampleSize && len(ds.Entities) > 0 {
		extra := make(map[int]bool, len(ordered))
		for _, id := range ordered {
			extra[id] = true
		}
		for i := range ds.Entities {
			if !extra[ds.Entities[i].ID] {
				ordered = append(ordered, ds.Entities[i].ID)
				if len(ordered) >= sampleSize*2 {
					break
				}
			}
		}
		sort.Ints(ordered)
	}

	samples.EntityIDs = shuffleSample(g.rng, ordered, sampleSize)
	samples.Queries = g.generateQueries(ds)

	// Entity types actually present in the dataset (stable first-seen order).
	var entityTypes []string
	seenTypes := make(map[string]bool)
	for i := range ds.Entities {
		t := ds.Entities[i].Type
		if !seenTypes[t] {
			seenTypes[t] = true
			entityTypes = append(entityTypes, t)
		}
	}
	samples.EntityTypes = entityTypes

	ds.Samples = samples
}

// generateQueries builds search queries from the domain vocabulary and entity names.
// Every query passes through ftsSafeQuery so that FTS5 can parse it unconditionally,
// regardless of how entity names are de-duplicated.
func (g *Generator) generateQueries(ds *Dataset) []string {
	queries := make([]string, 0, sampleSize)
	for i := 0; i < sampleSize; i++ {
		domain := Domains[g.rng.Intn(len(Domains))]
		vocab := domainVocabulary[domain]

		var raw string
		switch g.rng.Intn(4) {
		case 0: // two terms from the same domain
			raw = vocab[g.rng.Intn(len(vocab))] + " " + vocab[g.rng.Intn(len(vocab))]
		case 1: // single term
			raw = vocab[g.rng.Intn(len(vocab))]
		case 2: // entity name (exercises result enrichment)
			if len(ds.Entities) > 0 {
				raw = ds.Entities[g.rng.Intn(len(ds.Entities))].Name
			} else {
				raw = vocab[0]
			}
		default: // term plus generic business words
			raw = vocab[g.rng.Intn(len(vocab))] + " procedure policy review"
		}

		if safe := ftsSafeQuery(raw); safe != "" {
			queries = append(queries, safe)
		} else {
			// Generated names always contain letters, so this is defensive only.
			queries = append(queries, "review")
		}
	}
	return queries
}

// ftsSafeQuery rewrites q into a plain FTS5 term expression. A purely numeric
// token (e.g. the "10" in a de-duplicated entity name such as
// "Policy probation period-10") would be parsed by FTS5 as a column reference
// and fail with 'no such column: 10', so digit-only fragments are dropped while
// all other fragments keep the query realistic.
func ftsSafeQuery(q string) string {
	kept := make([]string, 0, 8)
	for _, field := range strings.Fields(q) {
		parts := strings.FieldsFunc(field, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		for _, part := range parts {
			if isDigitsOnly(part) {
				continue
			}
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

// isDigitsOnly reports whether s is non-empty and consists solely of digit runes.
func isDigitsOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// chunkText builds a realistic paragraph of 3..6 sentences for the domain.
func (g *Generator) chunkText(domain string) string {
	vocab := domainVocabulary[domain]
	nSentences := g.rng.Intn(4) + 3 // 3..6
	sentences := make([]string, nSentences)

	for i := range sentences {
		tpl := sentenceTemplates[g.rng.Intn(len(sentenceTemplates))]
		sentences[i] = fmt.Sprintf(tpl, vocab[g.rng.Intn(len(vocab))])
	}

	return strings.Join(sentences, " ")
}

// idRangesByDomain computes 1-based ID ranges [lo, hi) per domain for items
// stored in slice order.
func idRangesByDomain[T any](items []T, domainOf func(T) string) map[string][2]int {
	counts := make(map[string]int, len(Domains))
	for _, it := range items {
		counts[domainOf(it)]++
	}

	ranges := make(map[string][2]int, len(Domains))
	pos := 0
	for _, d := range Domains {
		lo := pos + 1
		ranges[d] = [2]int{lo, lo + counts[d]} // half-open [lo, hi) over contiguous per-domain IDs
		pos += counts[d]
	}
	return ranges
}

// docRangesByDomain computes 1-based document ID ranges per primary domain.
func docRangesByDomain(ds *Dataset) map[string][2]int {
	counts := make(map[string]int, len(Domains))
	for i := range ds.Documents {
		counts[ds.Documents[i].Domains[0]]++
	}

	ranges := make(map[string][2]int, len(Domains))
	pos := 0
	for _, d := range Domains {
		lo := pos + 1
		ranges[d] = [2]int{lo, lo + counts[d]} // half-open [lo, hi) over contiguous per-domain IDs
		pos += counts[d]
	}
	return ranges
}

// sampleIDs picks n distinct 1-based IDs from [1..count] deterministically.
func sampleIDs(rng *rand.Rand, count, n int) []int {
	if n > count {
		n = count
	}
	idx := make([]int, count)
	for i := range idx {
		idx[i] = i + 1 // 1-based IDs
	}
	rng.Shuffle(count, func(i, j int) { idx[i], idx[j] = idx[j], idx[i] })
	return idx[:n]
}

// shuffleSample picks n values from a pre-sorted slice deterministically.
func shuffleSample(rng *rand.Rand, src []int, n int) []int {
	if n > len(src) {
		n = len(src)
	}
	cp := make([]int, len(src))
	copy(cp, src)
	rng.Shuffle(len(cp), func(i, j int) { cp[i], cp[j] = cp[j], cp[i] })
	return cp[:n]
}

// titleCase upper-cases the first rune of s.
func titleCase(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	return string(unicodeToUpper(r[0])) + string(r[1:])
}

// unicodeToUpper upper-cases a single ASCII-compatible rune (identity for the rest).
func unicodeToUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 32
	}
	return r
}

// round4 rounds a float to 4 decimal places.
func round4(v float64) float64 {
	return float64(int64(v*10000+0.5)) / 10000
}
