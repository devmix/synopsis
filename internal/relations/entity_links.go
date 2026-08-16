// Package relations provides cross-domain entity link building as post-ingestion processing.
package relations

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/utils"
)

// KVKeyLastLinkingRun is the app_kv key storing the last linking run timestamp.
const KVKeyLastLinkingRun = "last_linking_run"

// BuildEntityLinksResult holds the outcome of a link-building run.
type BuildEntityLinksResult struct {
	LinksCreated int      `json:"links_created"`
	LinksSkipped int      `json:"links_skipped"`
	Errors       []string `json:"errors,omitempty"`
}

const (
	ruleConfidence   = 1.0
	equalsConfidence = 0.9
	evidenceDescLen  = 500
)

// entityPair holds two entities from different domains that share the same
// type and normalized name, suitable for cross-domain linking.
type entityPair struct{ A, B dao.Entity }

// BuildEntityLinks creates cross-domain entity links using configured methods.
// Returns immediately with an empty result if globalCfg is nil.
func BuildEntityLinks(ctx context.Context, db *sql.DB, globalCfg *config.CrossDomainLinksConfig, linker CrossDomainLinker) (*BuildEntityLinksResult, error) {
	return BuildEntityLinksIncremental(ctx, db, globalCfg, linker, "")
}

// BuildEntityLinksIncremental creates cross-domain entity links with optional
// incremental processing. When since is non-empty, only entities created after
// the given timestamp are processed; existing links for those changed entities
// are deleted and recreated. An empty since triggers a full rebuild of all entities.
func BuildEntityLinksIncremental(ctx context.Context, db *sql.DB, globalCfg *config.CrossDomainLinksConfig, linker CrossDomainLinker, since string) (*BuildEntityLinksResult, error) {
	if globalCfg == nil {
		return &BuildEntityLinksResult{}, nil
	}

	if err := globalCfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate cross-domain links config: %w", err)
	}

	linkDAO := dao.NewEntityLinkDAO(db)

	var entitiesByTypeAndName map[string]map[string][]dao.Entity
	var changedIDs map[int]bool
	var err error

	if since != "" {
		// Incremental mode: load only changed entities and delete their existing links.
		entitiesByTypeAndName, changedIDs, err = loadChangedEntitiesByTypeAndName(ctx, db, since)
		if err != nil {
			return nil, fmt.Errorf("load changed entities: %w", err)
		}
		if len(entitiesByTypeAndName) == 0 {
			return &BuildEntityLinksResult{}, nil // no new entities to link
		}
		// Delete existing links for changed entities before rebuilding.
		if err := deleteLinksForChangedEntities(ctx, linkDAO, changedIDs); err != nil {
			return nil, fmt.Errorf("delete old links for changed entities: %w", err)
		}
	} else {
		// Full rebuild mode.
		entitiesByTypeAndName, err = loadEntitiesByTypeAndName(ctx, db)
		if err != nil {
			return nil, fmt.Errorf("load entities: %w", err)
		}
	}

	result := &BuildEntityLinksResult{}

	for _, method := range globalCfg.Methods {
		switch method {
		case "equals":
			created, skipped, errs := buildEqualsLinks(ctx, linkDAO, globalCfg, entitiesByTypeAndName)
			result.LinksCreated += created
			result.LinksSkipped += skipped
			result.Errors = append(result.Errors, errs...)

		case "llm":
			created, skipped, errs := buildLLMLinks(ctx, linkDAO, globalCfg, entitiesByTypeAndName, linker)
			result.LinksCreated += created
			result.LinksSkipped += skipped
			result.Errors = append(result.Errors, errs...)

		case "expression":
			created, skipped, errs := buildExpressionLinks(ctx, linkDAO, globalCfg, entitiesByTypeAndName)
			result.LinksCreated += created
			result.LinksSkipped += skipped
			result.Errors = append(result.Errors, errs...)
		}
	}

	return result, nil
}

// loadChangedEntitiesByTypeAndName loads only entities created after the given
// timestamp and returns them grouped by (type, normalized_name), plus a set of
// changed entity IDs for link cleanup.
func loadChangedEntitiesByTypeAndName(ctx context.Context, db *sql.DB, since string) (map[string]map[string][]dao.Entity, map[int]bool, error) {
	ents, err := dao.NewEntityDAO(db).ListCreatedSince(ctx, since)
	if err != nil {
		return nil, nil, fmt.Errorf("list entities created since %s: %w", since, err)
	}

	grouped := make(map[string]map[string][]dao.Entity)
	changedIDs := make(map[int]bool)

	for _, ent := range ents {
		key := ent.Type
		nameKey := utils.Normalize(ent.Name)
		if grouped[key] == nil {
			grouped[key] = make(map[string][]dao.Entity)
		}
		grouped[key][nameKey] = append(grouped[key][nameKey], ent)
		changedIDs[ent.ID] = true
	}

	return grouped, changedIDs, nil
}

// deleteLinksForChangedEntities removes all entity_links where either subject or
// target is in the set of changed entity IDs. This ensures links are recreated
// with current data for incremental runs.
func deleteLinksForChangedEntities(ctx context.Context, linkDAO *dao.EntityLinkDAO, changedIDs map[int]bool) error {
	if len(changedIDs) == 0 {
		return nil
	}

	ids := make([]int, 0, len(changedIDs))
	for id := range changedIDs {
		ids = append(ids, id)
	}

	_, err := linkDAO.DeleteByEntityIDs(ctx, ids)
	if err != nil {
		return fmt.Errorf("delete links for changed entities: %w", err)
	}
	return nil
}

// loadEntitiesByTypeAndName loads all entities and groups them by (type, normalized_name).
func loadEntitiesByTypeAndName(ctx context.Context, db *sql.DB) (map[string]map[string][]dao.Entity, error) {
	ents, err := dao.NewEntityDAO(db).List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list entities: %w", err)
	}

	grouped := make(map[string]map[string][]dao.Entity)
	for _, ent := range ents {
		key := ent.Type
		nameKey := utils.Normalize(ent.Name)
		if grouped[key] == nil {
			grouped[key] = make(map[string][]dao.Entity)
		}
		grouped[key][nameKey] = append(grouped[key][nameKey], ent)
	}

	return grouped, nil
}

// crossDomainEntityPairs groups entities by (type, normalized_name), then by domain,
// and produces deterministic pairs between different domains.
func crossDomainEntityPairs(entitiesByTypeAndName map[string]map[string][]dao.Entity) []entityPair {
	type pairKey struct{ A, B int } // entity IDs for dedup

	seen := make(map[pairKey]bool)
	var pairs []entityPair

	// Sort outer keys (entity types) for deterministic iteration order.
	types := make([]string, 0, len(entitiesByTypeAndName))
	for t := range entitiesByTypeAndName {
		types = append(types, t)
	}
	sort.Strings(types)

	for _, entityType := range types {
		nameMap := entitiesByTypeAndName[entityType]

		// Sort inner keys (normalized names) for deterministic iteration order.
		names := make([]string, 0, len(nameMap))
		for n := range nameMap {
			names = append(names, n)
		}
		sort.Strings(names)

		for _, nameKey := range names {
			ents := nameMap[nameKey]
			if len(ents) < 2 {
				continue
			}

			domainGroups := make(map[string][]dao.Entity)
			for _, ent := range ents {
				normDomain := utils.Normalize(ent.Domain)
				domainGroups[normDomain] = append(domainGroups[normDomain], ent)
			}

			if len(domainGroups) < 2 {
				continue
			}

			domains := make([]string, 0, len(domainGroups))
			for d := range domainGroups {
				domains = append(domains, d)
			}
			sort.Strings(domains)

			// Sort entities within each domain group by Predicate for deterministic pair order.
			for _, entsInGroup := range domainGroups {
				sort.Slice(entsInGroup, func(i, j int) bool {
					return entsInGroup[i].ID < entsInGroup[j].ID
				})
			}

			for i := 0; i < len(domains); i++ {
				for j := i + 1; j < len(domains); j++ {
					domA, domB := domains[i], domains[j]
					for _, entA := range domainGroups[domA] {
						for _, entB := range domainGroups[domB] {
							k := pairKey{entA.ID, entB.ID}
							if seen[k] {
								continue
							}
							seen[k] = true
							pairs = append(pairs, entityPair{A: entA, B: entB})
						}
					}
				}
			}
		}
	}

	return pairs
}

// buildEqualsLinks creates links between entities with the same normalized name across domains.
func buildEqualsLinks(ctx context.Context, linkDAO *dao.EntityLinkDAO, cfg *config.CrossDomainLinksConfig, entitiesByTypeAndName map[string]map[string][]dao.Entity) (int, int, []string) {
	created := 0
	skipped := 0
	var errs []string

	minWords := config.DefaultEqualsMinWords
	if cfg.Equals != nil && cfg.Equals.MinWords > 0 {
		minWords = cfg.Equals.MinWords
	}

	pairs := crossDomainEntityPairs(entitiesByTypeAndName)

	for _, p := range pairs {
		nameA := utils.Normalize(p.A.Name)
		parts := strings.Fields(nameA)
		if len(parts) < minWords {
			continue // skip: not enough words in name
		}

		evidence := fmt.Sprintf("equals: %s in %s and %s", nameA, utils.Normalize(p.A.Domain), utils.Normalize(p.B.Domain))

		linkCreated, err := createBidirectionalLink(ctx, linkDAO, p.A.ID, p.B.ID, config.DefaultRelationType, "equals", equalsConfidence, evidence)
		if err != nil {
			errs = append(errs, fmt.Sprintf("create equals link (%d <-> %d): %v", p.A.ID, p.B.ID, err))
			continue
		}
		if linkCreated {
			created++
		} else {
			skipped++
		}
	}

	return created, skipped, errs
}

// buildLLMLinks uses an LLM linker to evaluate candidate entity pairs.
func buildLLMLinks(ctx context.Context, linkDAO *dao.EntityLinkDAO, cfg *config.CrossDomainLinksConfig, entitiesByTypeAndName map[string]map[string][]dao.Entity, linker CrossDomainLinker) (int, int, []string) {
	if linker == nil {
		return 0, 0, []string{"llm method requires a linker"}
	}

	threshold := config.DefaultLLMConfidenceThreshold
	if cfg.LLmConfidenceThreshold > 0 {
		threshold = cfg.LLmConfidenceThreshold
	}

	batchSize := config.DefaultLLMBatchSize
	if cfg.BatchSize > 0 {
		batchSize = cfg.BatchSize
	}

	pairs := crossDomainEntityPairs(entitiesByTypeAndName)

	if len(pairs) == 0 {
		return 0, 0, nil
	}

	created := 0
	skipped := 0
	var errs []string

	for i := 0; i < len(pairs); i += batchSize {
		select {
		case <-ctx.Done():
			return created, skipped, append(errs, fmt.Sprintf("context cancelled after %d pairs", i))
		default:
		}

		end := i + batchSize
		if end > len(pairs) {
			end = len(pairs)
		}

		for _, p := range pairs[i:end] {
			descA := ""
			if p.A.Description != nil {
				descA = *p.A.Description
			}
			descB := ""
			if p.B.Description != nil {
				descB = *p.B.Description
			}

			candidateA := EntityCandidate{
				ID:          p.A.ID,
				Name:        p.A.Name,
				Type:        p.A.Type,
				Domain:      p.A.Domain,
				Description: descA,
			}
			candidateB := EntityCandidate{
				ID:          p.B.ID,
				Name:        p.B.Name,
				Type:        p.B.Type,
				Domain:      p.B.Domain,
				Description: descB,
			}

			decision, err := linker.LinkPair(ctx, candidateA, candidateB)
			if err != nil {
				errs = append(errs, fmt.Sprintf("llm link pair (%d <-> %d): %v", p.A.ID, p.B.ID, err))
				skipped++
				continue
			}

			if !decision.SameEntity || decision.Confidence < threshold {
				skipped++
				continue
			}

			evidence := utils.Truncate(decision.Reasoning, evidenceDescLen)

			linkCreated, err := createBidirectionalLink(ctx, linkDAO, p.A.ID, p.B.ID, config.DefaultRelationType, "llm", decision.Confidence, evidence)
			if err != nil {
				errs = append(errs, fmt.Sprintf("create llm link (%d <-> %d): %v", p.A.ID, p.B.ID, err))
				continue
			}
			if linkCreated {
				created++
			} else {
				skipped++
			}
		}
	}

	return created, skipped, errs
}

// buildExpressionLinks creates links using CEL expressions from the configuration.
func buildExpressionLinks(ctx context.Context, linkDAO *dao.EntityLinkDAO, cfg *config.CrossDomainLinksConfig, entitiesByTypeAndName map[string]map[string][]dao.Entity) (int, int, []string) {
	created := 0
	skipped := 0
	var errs []string

	if len(cfg.Expressions) == 0 {
		return 0, 0, nil
	}

	// Build entity lookup for expression evaluation.
	// We need to iterate all cross-domain pairs.
	pairs := crossDomainEntityPairs(entitiesByTypeAndName)
	if len(pairs) == 0 {
		return 0, 0, nil
	}

	// Create expression linker (db not needed for basic expression evaluation;
	// scope loaders are lazy-loaded and only used when expressions call facts/chunks/neighbors).
	linker := NewExpressionLinker(nil)
	if err := linker.Init(ctx, cfg.Expressions); err != nil {
		return 0, 0, []string{fmt.Sprintf("init expression linker: %v", err)}
	}

	for _, p := range pairs {
		matched, exprName, err := linker.EvaluatePair(ctx, p.A, p.B)
		if err != nil {
			errs = append(errs, fmt.Sprintf("expression pair (%d <-> %d): %v", p.A.ID, p.B.ID, err))
			skipped++
			continue
		}

		if !matched {
			continue
		}

		// Find the expression to get relation type.
		relationType := config.DefaultRelationType
		for _, expr := range cfg.Expressions {
			if expr.Name == exprName {
				relationType = expr.RelationType
				break
			}
		}

		evidence := fmt.Sprintf("expression: %s", exprName)

		linkCreated, err := createBidirectionalLink(ctx, linkDAO, p.A.ID, p.B.ID, relationType, "expression", ruleConfidence, evidence)
		if err != nil {
			errs = append(errs, fmt.Sprintf("create expression link (%d <-> %d): %v", p.A.ID, p.B.ID, err))
			continue
		}
		if linkCreated {
			created++
		} else {
			skipped++
		}
	}

	return created, skipped, errs
}

// createEntityLink creates a single directed entity link.
// Returns (true, nil) if the link was newly inserted, (false, nil) if it already existed.
func createEntityLink(ctx context.Context, linkDAO *dao.EntityLinkDAO, subjectID, targetID int, relationType, method string, confidence float64, evidence string) (bool, error) {
	link := dao.EntityLink{
		SubjectEntityID: subjectID,
		TargetEntityID:  targetID,
		RelationType:    relationType,
		Method:          method,
		Confidence:      confidence,
		Evidence:        &evidence,
	}

	return linkDAO.Create(ctx, link)
}

// createBidirectionalLink creates links in both directions between two entities.
// Returns (true, nil) if at least one new link was inserted, (false, nil) if both already existed.
func createBidirectionalLink(ctx context.Context, linkDAO *dao.EntityLinkDAO, aID, bID int, relationType, method string, confidence float64, evidence string) (bool, error) {
	createdAB, err := createEntityLink(ctx, linkDAO, aID, bID, relationType, method, confidence, evidence)
	if err != nil {
		return false, fmt.Errorf("create link A->B: %w", err)
	}
	createdBA, err := createEntityLink(ctx, linkDAO, bID, aID, relationType, method, confidence, evidence)
	if err != nil {
		return false, fmt.Errorf("create link B->A: %w", err)
	}
	return createdAB || createdBA, nil
}
