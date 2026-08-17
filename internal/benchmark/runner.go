package benchmark

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/devmix/synopsis/internal/graph"
	synmcp "github.com/devmix/synopsis/internal/mcp"
	"github.com/devmix/synopsis/internal/mcp/handlers"
	"github.com/devmix/synopsis/internal/search"

	"github.com/mark3labs/mcp-go/mcp"
)

// DefaultSamplesSize is the number of distinct argument values sampled per collection.
const DefaultSamplesSize = 32

// Samples holds the deterministic argument collections used to build tool calls.
type Samples struct {
	Queries     []string `json:"queries"`
	DocIDs      []int    `json:"doc_ids"`
	ChunkIDs    []int    `json:"chunk_ids"`
	FactIDs     []int    `json:"fact_ids"`
	EntityIDs   []int    `json:"entity_ids"`
	EntityTypes []string `json:"entity_types"`
}

// staticNoFillQueries are generic queries used when benchmarking a pre-existing
// database with --no-fill, where no generated vocabulary is available.
var staticNoFillQueries = []string{
	"hiring process policy review",
	"deployment pipeline incident response runbook",
	"budget approval expense report quarter",
	"vulnerability scan access control audit",
	"release notes feature flag milestone",
	"performance review onboarding probation",
	"invoice reconciliation vendor contract terms",
	"certificate rotation encryption standard compliance",
	"sprint planning backlog user story acceptance criteria",
	"service level objective observability capacity planning",
	"leave of absence compensation benefits severance",
	"penetration test threat model data classification",
	"technical debt code review rollback procedure",
	"forecasting model cost center payment terms audit trail",
	"zero trust architecture incident response plan",
	"stakeholder review deprecation plan roadmap",
}

// LoadSamplesFromDB picks deterministic argument samples from an existing database.
// It is used by the --no-fill mode of the load-test command.
func LoadSamplesFromDB(ctx context.Context, db *sql.DB, n int) (*Samples, error) {
	if n <= 0 {
		n = DefaultSamplesSize
	}

	var docCount, entityCount int64
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM documents").Scan(&docCount); err != nil {
		return nil, fmt.Errorf("count documents: %w", err)
	}
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM entities").Scan(&entityCount); err != nil {
		return nil, fmt.Errorf("count entities: %w", err)
	}
	if docCount == 0 && entityCount == 0 {
		return nil, errors.New("database appears empty; run synopsis load-test without --no-fill to populate it first")
	}

	samples := &Samples{Queries: staticNoFillQueries[:min(n, len(staticNoFillQueries))]}

	strideCollect := func(table string) ([]int, error) {
		var total int64
		if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&total); err != nil {
			return nil, fmt.Errorf("count %s: %w", table, err)
		}
		if total == 0 {
			return nil, nil
		}
		step := int(total) / n
		if step < 1 {
			step = 1
		}

		var ids []int
		for i := 0; len(ids) < n && int64(i*step+1) <= total; i++ {
			var id int
			if err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT id FROM %s ORDER BY id LIMIT 1 OFFSET ?", table), i*step).Scan(&id); err != nil {
				return ids, fmt.Errorf("sample %s: %w", table, err)
			}
			ids = append(ids, id)
		}
		sort.Ints(ids)
		return ids, nil
	}

	var err error
	if samples.DocIDs, err = strideCollect("documents"); err != nil {
		return nil, err
	}
	if samples.ChunkIDs, err = strideCollect("chunks"); err != nil {
		return nil, err
	}
	if samples.FactIDs, err = strideCollect("facts"); err != nil {
		return nil, err
	}
	if samples.EntityIDs, err = strideCollect("entities"); err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, "SELECT DISTINCT type FROM entities ORDER BY type LIMIT ?", n)
	if err != nil {
		return nil, fmt.Errorf("list entity types: %w", err)
	}
	defer rows.Close() //nolint:errcheck
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("scan entity type: %w", err)
		}
		samples.EntityTypes = append(samples.EntityTypes, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entity types: %w", err)
	}

	return samples, nil
}

// Options configures a benchmark run.
type Options struct {
	// Iterations is the number of measured units per tool case (default 100).
	Iterations int `json:"iterations"`

	// PagesPerCall is how many cursor pages each measured unit fetches for
	// paginated tools such as catalog_documents (default 3).
	PagesPerCall int `json:"pages_per_call"`
}

// withDefaults fills zero/negative option fields with their defaults.
func (o Options) WithDefaults() Options {
	if o.Iterations <= 0 {
		o.Iterations = 100
	}
	if o.PagesPerCall <= 0 {
		o.PagesPerCall = 3
	}
	return o
}

// ToolStats holds latency statistics for one benchmarked tool case.
type ToolStats struct {
	Name          string  `json:"name"`
	Calls         int     `json:"calls"`                 // measured units executed successfully
	Pages         int     `json:"pages,omitempty"`       // total pages fetched (paginated cases)
	Errors        int     `json:"errors"`                // failed calls (excluded from latency stats)
	FirstError    string  `json:"first_error,omitempty"` // message of the first failure, if any
	TotalMs       float64 `json:"total_ms"`              // sum of successful per-call latencies
	AttemptMs     float64 `json:"attempt_ms"`            // sum of ALL attempted per-iteration latencies, failures included
	AvgMs         float64 `json:"avg_ms"`
	MinMs         float64 `json:"min_ms"`
	MaxMs         float64 `json:"max_ms"`
	P50Ms         float64 `json:"p50_ms"`
	P95Ms         float64 `json:"p95_ms"`
	P99Ms         float64 `json:"p99_ms"`
	ThroughputQPS float64 `json:"throughput_qps"` // attempted units per second over ALL iterations (failures counted as cost)
}

// RunResult is returned by Runner.Run.
type RunResult struct {
	Tools []ToolStats `json:"tools"`
}

// Runner executes the benchmark cases against tool handlers directly (no HTTP).
type Runner struct {
	db        *sql.DB
	searcher  search.Searcher
	graph     *graph.Graph
	samples   *Samples
	validator handlers.DomainValidator
}

// NewRunner creates a Runner. g may be nil only if graph-dependent tools are not benchmarked;
// the standard case list requires it, so pass a loaded graph in practice.
func NewRunner(db *sql.DB, searcher search.Searcher, g *graph.Graph, samples *Samples) *Runner {
	return &Runner{
		db:        db,
		searcher:  searcher,
		graph:     g,
		samples:   samples,
		validator: newDBDomainValidator(context.Background(), db), // re-bound to the run's ctx by Run
	}
}

// Run executes every benchmark case for the configured number of iterations.
func (r *Runner) Run(ctx context.Context, opts Options) (*RunResult, error) {
	opts = opts.WithDefaults()
	r.validator = newDBDomainValidator(ctx, r.db) // domain validation shares the run's ctx

	var missing []string
	if len(r.samples.Queries) == 0 {
		missing = append(missing, "queries")
	}
	for name, ids := range map[string][]int{
		"doc_ids":    r.samples.DocIDs,
		"chunk_ids":  r.samples.ChunkIDs,
		"fact_ids":   r.samples.FactIDs,
		"entity_ids": r.samples.EntityIDs,
	} {
		if len(ids) == 0 {
			missing = append(missing, name)
		}
	}
	if len(r.samples.EntityTypes) == 0 {
		missing = append(missing, "entity_types")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("insufficient samples to benchmark all tools: missing %v", missing)
	}

	cases := []toolCase{
		{name: synmcp.ToolSearch, call: func(i int) (int, error) {
			return r.once(ctx, synmcp.ToolSearch, map[string]interface{}{
				"query": r.samples.Queries[i%len(r.samples.Queries)],
				"top_k": 10,
			})
		}},
		{name: "search_lexical", call: func(i int) (int, error) {
			if _, err := r.searcher.LexicalSearch(ctx, r.samples.Queries[i%len(r.samples.Queries)], 10, ""); err != nil {
				return 0, err
			}
			return 1, nil
		}},
		{name: "search_semantic", call: func(i int) (int, error) {
			if _, err := r.searcher.SemanticSearch(ctx, r.samples.Queries[i%len(r.samples.Queries)], 10, ""); err != nil {
				return 0, err
			}
			return 1, nil
		}},
		{name: synmcp.ToolCatalogOverview, call: func(int) (int, error) {
			return r.once(ctx, synmcp.ToolCatalogOverview, map[string]interface{}{})
		}},
		{name: synmcp.ToolCatalogDocuments, paginated: true, call: func(int) (int, error) {
			return r.fetchPages(ctx, synmcp.ToolCatalogDocuments, pageArgs(), opts.PagesPerCall)
		}},
		{name: synmcp.ToolCatalogEntities, paginated: true, call: func(int) (int, error) {
			return r.fetchPages(ctx, synmcp.ToolCatalogEntities, pageArgs(), opts.PagesPerCall)
		}},
		{name: synmcp.ToolSearchEntitiesByType, paginated: true, call: func(i int) (int, error) {
			args := copyArgs(pageArgs())
			args["entity_type"] = r.samples.EntityTypes[i%len(r.samples.EntityTypes)]
			return r.fetchPages(ctx, synmcp.ToolSearchEntitiesByType, args, opts.PagesPerCall)
		}},
		{name: synmcp.ToolSearchFacts, paginated: true, call: func(int) (int, error) {
			args := copyArgs(pageArgs())
			args["status"] = "approved"
			return r.fetchPages(ctx, synmcp.ToolSearchFacts, args, opts.PagesPerCall)
		}},
		{name: synmcp.ToolGetDocumentContext, call: func(i int) (int, error) {
			return r.once(ctx, synmcp.ToolGetDocumentContext, map[string]interface{}{
				"document_id":      strconv.Itoa(r.samples.DocIDs[i%len(r.samples.DocIDs)]),
				"include_chunks":   true,
				"include_entities": true,
				"include_facts":    true,
			})
		}},
		{name: synmcp.ToolGetChunkByID, call: func(i int) (int, error) {
			return r.once(ctx, synmcp.ToolGetChunkByID, map[string]interface{}{
				"chunk_id": strconv.Itoa(r.samples.ChunkIDs[i%len(r.samples.ChunkIDs)]),
			})
		}},
		{name: synmcp.ToolGetFactByID, call: func(i int) (int, error) {
			return r.once(ctx, synmcp.ToolGetFactByID, map[string]interface{}{
				"fact_id": strconv.Itoa(r.samples.FactIDs[i%len(r.samples.FactIDs)]),
			})
		}},
		{name: synmcp.ToolGetEntityDossier, call: func(i int) (int, error) {
			return r.once(ctx, synmcp.ToolGetEntityDossier, map[string]interface{}{
				"entity_id": strconv.Itoa(r.samples.EntityIDs[i%len(r.samples.EntityIDs)]),
			})
		}},
		{name: synmcp.ToolGetEntityRelations, call: func(i int) (int, error) {
			return r.once(ctx, synmcp.ToolGetEntityRelations, map[string]interface{}{
				"entity_id":            strconv.Itoa(r.samples.EntityIDs[i%len(r.samples.EntityIDs)]),
				"include_cross_domain": true,
			})
		}},
		{name: synmcp.ToolGetEntityLinks, call: func(i int) (int, error) {
			return r.once(ctx, synmcp.ToolGetEntityLinks, map[string]interface{}{
				"entity_id": strconv.Itoa(r.samples.EntityIDs[i%len(r.samples.EntityIDs)]),
			})
		}},
	}

	result := &RunResult{Tools: make([]ToolStats, 0, len(cases))}
	for _, tc := range cases {
		stats := ToolStats{Name: tc.name}
		latencies := make([]float64, 0, opts.Iterations)

		for i := 0; i < opts.Iterations; i++ {
			start := time.Now()
			pages, err := tc.call(i)
			elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0
			stats.AttemptMs += elapsedMs // failed attempts consume wall time too and must count against throughput
			if err != nil {
				stats.Errors++
				if stats.FirstError == "" {
					stats.FirstError = err.Error()
				}
				continue
			}
			latencies = append(latencies, elapsedMs)
			stats.Pages += pages
		}

		stats.Calls = len(latencies)
		for _, l := range latencies {
			stats.TotalMs += l
		}
		if stats.Calls > 0 {
			sort.Float64s(latencies)
			stats.AvgMs = round3(stats.TotalMs / float64(stats.Calls))
			stats.MinMs = round3(latencies[0])
			stats.MaxMs = round3(latencies[len(latencies)-1])
			stats.P50Ms = percentile(latencies, 50)
			stats.P95Ms = percentile(latencies, 95)
			stats.P99Ms = percentile(latencies, 99)
		}
		if stats.AttemptMs > 0 {
			stats.ThroughputQPS = round3(float64(opts.Iterations) * 1000.0 / stats.AttemptMs)
		}

		result.Tools = append(result.Tools, stats)
	}

	return result, nil
}

// toolCase describes one benchmark case: a named unit of work executed repeatedly.
type toolCase struct {
	name      string
	paginated bool
	call      func(iteration int) (pages int, err error)
}

// once invokes a tool and counts it as a single measured unit on success.
func (r *Runner) once(ctx context.Context, name string, args map[string]interface{}) (int, error) {
	if _, err := r.invoke(ctx, name, args); err != nil {
		return 0, err
	}
	return 1, nil
}

// pageArgs returns a fresh argument set with the default page size.
func pageArgs() map[string]interface{} {
	return map[string]interface{}{"page_size": handlers.DefaultPageSize}
}

// invoke builds an MCP request, dispatches it to the matching handler directly,
// and returns the first text content of the result ("" on failure).
func (r *Runner) invoke(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	req := mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: args}}

	var res *mcp.CallToolResult
	var err error
	switch name {
	case synmcp.ToolSearch:
		res, err = handlers.HandleSearch(ctx, req, r.searcher, r.validator)
	case synmcp.ToolCatalogOverview:
		res, err = handlers.HandleCatalogOverview(ctx, req, r.db)
	case synmcp.ToolCatalogDocuments:
		res, err = handlers.HandleCatalogDocuments(ctx, req, r.db)
	case synmcp.ToolCatalogEntities:
		res, err = handlers.HandleCatalogEntities(ctx, req, r.db)
	case synmcp.ToolSearchEntitiesByType:
		res, err = handlers.HandleSearchEntitiesByType(ctx, req, r.db)
	case synmcp.ToolSearchFacts:
		res, err = handlers.HandleSearchFacts(ctx, req, r.db)
	case synmcp.ToolGetDocumentContext:
		res, err = handlers.HandleGetDocumentContext(ctx, req, r.db)
	case synmcp.ToolGetChunkByID:
		res, err = handlers.HandleGetChunkByID(ctx, req, r.db)
	case synmcp.ToolGetFactByID:
		res, err = handlers.HandleGetFactByID(ctx, req, r.db)
	case synmcp.ToolGetEntityDossier:
		res, err = handlers.HandleGetEntityDossier(ctx, req, r.db, r.graph)
	case synmcp.ToolGetEntityRelations:
		if r.graph == nil {
			return "", errors.New("knowledge graph is not loaded")
		}
		res, err = handlers.HandleGetEntityRelations(ctx, req, r.db, r.graph)
	case synmcp.ToolGetEntityLinks:
		res, err = handlers.HandleGetEntityLinks(ctx, req, r.db)
	default:
		return "", fmt.Errorf("unknown tool %q", name)
	}

	if err != nil {
		return "", err
	}
	text := textOf(res)
	if res.IsError {
		return "", errors.New(text)
	}
	return text, nil
}

// fetchPages executes a paginated tool, following next_cursor for up to maxPages pages.
func (r *Runner) fetchPages(ctx context.Context, name string, baseArgs map[string]interface{}, maxPages int) (int, error) {
	cursor := ""
	fetched := 0
	for p := 0; p < maxPages; p++ {
		args := copyArgs(baseArgs)
		if cursor != "" {
			args["cursor"] = cursor
		}

		text, err := r.invoke(ctx, name, args)
		if err != nil {
			return fetched, fmt.Errorf("page %d of %s: %w", p+1, name, err)
		}

		var out struct {
			NextCursor *string `json:"next_cursor"`
		}
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			return fetched, fmt.Errorf("page %d of %s: parse response: %w", p+1, name, err)
		}

		fetched++
		if out.NextCursor == nil || *out.NextCursor == "" {
			break
		}
		cursor = *out.NextCursor
	}
	return fetched, nil
}

// dbDomainValidator validates domain names against the knowledge base using the
// same query as the MCP server (documents.metadata_json $.domain). The handlers.
// DomainValidator interface has no context parameter, so the validator captures a
// context at construction time; Runner.Run re-binds it to the run's ctx so that
// validation queries honor cancellation and deadlines.
type dbDomainValidator struct {
	db  *sql.DB
	ctx context.Context // falls back to Background when constructed without one
}

func newDBDomainValidator(ctx context.Context, db *sql.DB) handlers.DomainValidator {
	if ctx == nil {
		ctx = context.Background()
	}
	return &dbDomainValidator{db: db, ctx: ctx}
}

func (v *dbDomainValidator) IsKnownDomain(domain string) bool {
	if v.db == nil || domain == "" {
		return true // no DB or empty domain → skip validation
	}
	const query = `SELECT 1 FROM documents, json_each(metadata_json, '$.domain') WHERE metadata_json IS NOT NULL AND json_valid(metadata_json) AND json_each.value = ? LIMIT 1`
	var dummy int
	return v.db.QueryRowContext(v.ctx, query, domain).Scan(&dummy) == nil
}

// textOf extracts the first text content of a tool result ("" if none).
func textOf(res *mcp.CallToolResult) string {
	if res == nil || len(res.Content) == 0 {
		return ""
	}
	if tc, ok := res.Content[0].(mcp.TextContent); ok {
		return tc.Text
	}
	return ""
}

// copyArgs returns a shallow copy of args.
func copyArgs(args map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	return out
}

// percentile returns the nearest-rank percentile (p in 0..100) of a sorted slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p / 100.0 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return round3(sorted[rank-1])
}

// round3 rounds to 3 decimal places.
func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}
