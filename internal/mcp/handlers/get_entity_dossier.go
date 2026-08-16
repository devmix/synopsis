package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/devmix/synopsis/internal/database/dao"
	"github.com/devmix/synopsis/internal/graph"

	"github.com/mark3labs/mcp-go/mcp"
)

// HandleGetEntityDossier processes a get_entity_dossier tool call.
// It retrieves full entity information including facts, sources, related entities, and cross-domain links.
func HandleGetEntityDossier(
	ctx context.Context,
	req mcp.CallToolRequest,
	db *sql.DB,
	g *graph.Graph,
) (*mcp.CallToolResult, error) {
	entityIDStr := req.GetString("entity_id", "")
	entityName := req.GetString("entity_name", "")
	domain := req.GetString("domain", "")

	depth := req.GetInt("depth", 2)
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	includeFacts := req.GetBool("include_facts", true)
	includeSources := req.GetBool("include_sources", true)

	entityDAO := dao.NewEntityDAO(db)
	factDAO := dao.NewFactDAO(db)
	factSourceDAO := dao.NewFactSourceDAO(db)
	entitySourceDAO := dao.NewEntitySourceDAO(db)
	docDAO := dao.NewDocumentDAO(db)
	linkDAO := dao.NewEntityLinkDAO(db)

	ent, errResp := ResolveEntity(ctx, db, entityIDStr, entityName, "", domain)
	if errResp != nil {
		return errResp, nil
	}

	response := EntityDossierResponse{
		Entity: DossierEntityInfo{
			ID:         ent.ID,
			Name:       ent.Name,
			Type:       ent.Type,
			Domain:     ent.Domain,
			Confidence: ent.Confidence,
		},
	}

	if ent.Description != nil {
		response.Entity.Description = *ent.Description
	}
	if ent.MetadataJSON != nil {
		response.Entity.Metadata = *ent.MetadataJSON
	}

	if includeFacts {
		facts, err := factDAO.ListByEntityID(ctx, ent.ID)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Error retrieving facts: %v", err)),
				},
				IsError: true,
			}, nil
		}

		if len(facts) > 100 {
			facts = facts[:100]
		}

		for _, f := range facts {
			factInfo := DossierFact{
				ID:        f.ID,
				Predicate: f.Predicate,
				Domain:    f.Domain,
				Status:    f.Status,
				Weight:    f.Weight,
			}
			if f.SubjectEntityID != 0 {
				factInfo.SubjectEntityID = f.SubjectEntityID
			}
			if f.ObjectEntityID != 0 {
				factInfo.ObjectEntityID = f.ObjectEntityID
			}
			if f.Metadata != nil {
				factInfo.Metadata = *f.Metadata
			}
			if f.ValidFrom != nil {
				factInfo.ValidFrom = *f.ValidFrom
			}
			if f.ValidTo != nil {
				factInfo.ValidTo = *f.ValidTo
			}

			sources, err := factSourceDAO.GetByFactID(ctx, f.ID)
			if err == nil {
				for _, src := range sources {
					fs := FactSourceInfo{
						DocumentID:  src.DocumentID,
						ExtractedAt: src.ExtractedAt,
					}
					if src.Quote != nil {
						fs.Quote = *src.Quote
					}
					factInfo.Sources = append(factInfo.Sources, fs)
				}
			}

			response.Facts = append(response.Facts, factInfo)
		}
	}

	if includeSources {
		docIDs, err := entitySourceDAO.GetDocumentsByEntityID(ctx, ent.ID)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					mcp.NewTextContent(fmt.Sprintf("Error retrieving sources: %v", err)),
				},
				IsError: true,
			}, nil
		}

		if len(docIDs) > 0 {
			docMap, err := docDAO.GetByIDs(ctx, docIDs)
			if err != nil {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						mcp.NewTextContent(fmt.Sprintf("Error retrieving documents: %v", err)),
					},
					IsError: true,
				}, nil
			}

			for _, id := range docIDs {
				doc, ok := docMap[id]
				if !ok {
					continue
				}
				srcInfo := SourceDoc{
					ID:           doc.ID,
					SourceType:   doc.SourceType,
					OriginalPath: doc.OriginalPath,
				}
				if doc.MetadataJSON != nil {
					srcInfo.Metadata = *doc.MetadataJSON
				}
				response.Sources = append(response.Sources, srcInfo)
			}
		}
	}

	// Related entities via BFS in the graph.
	if g != nil {
		bfsCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		opts := graph.BFSOptions{
			MaxDepth:          depth,
			Direction:         graph.DirectionBoth,
			FollowEntityLinks: true,
			MaxNodes:          100,
		}

		result, err := g.BFS(bfsCtx, ent.ID, opts)
		if err == nil && result != nil {
			for _, node := range result.Nodes {
				if node.ID == ent.ID {
					continue
				}
				response.RelatedEntities = append(response.RelatedEntities, RelatedEntity{
					ID:     node.ID,
					Name:   node.Name,
					Type:   node.Type,
					Domain: node.Domain,
				})
			}

			// Build a lookup map for nodes in BFS result.
			nodeMap := make(map[int]*graph.EntityNode, len(result.Nodes))
			for _, n := range result.Nodes {
				nodeMap[n.ID] = n
			}

			// Collect cross-domain links from BFS edges.
			// Only include edges incident to ent.ID where target domain differs from source domain.
			crossLinksByTarget := make(map[int]CrossDomainLink)
			for _, edge := range result.Edges {
				var relType string
				if edge.RelationType != "" {
					relType = edge.RelationType
				}

				// Only process edges incident to ent.ID.
				if edge.SourceID != ent.ID && edge.TargetID != ent.ID {
					continue
				}

				var targetID int
				if edge.SourceID == ent.ID {
					targetID = edge.TargetID
				} else {
					targetID = edge.SourceID
				}

				targetNode, ok := nodeMap[targetID]
				if !ok {
					continue
				}

				// Only cross-domain edges belong in cross_domain_links.
				if targetNode.Domain == ent.Domain {
					continue
				}

				crossLink := CrossDomainLink{
					TargetEntityID: targetID,
					TargetName:     targetNode.Name,
					TargetDomain:   targetNode.Domain,
					RelationTypes:  []string{relType},
					Method:         edge.Method,
					Confidence:     edge.Confidence,
				}
				if edge.Evidence != nil {
					crossLink.Evidence = *edge.Evidence
				}

				// Dedup by target_entity_id; merge relation_types; keep provenance from best entry.
				existing, exists := crossLinksByTarget[targetID]
				if !exists || hasBetterProvenance(crossLink, existing) {
					crossLink.RelationTypes = mergeRelationTypes(existing.RelationTypes, relType)
					crossLinksByTarget[targetID] = crossLink
				} else {
					existing.RelationTypes = mergeRelationTypes(existing.RelationTypes, relType)
					crossLinksByTarget[targetID] = existing
				}
			}

			// Collect cross-domain links from BFS, sorted by target_entity_id for deterministic output.
			sortedTargets := make([]int, 0, len(crossLinksByTarget))
			for id := range crossLinksByTarget {
				sortedTargets = append(sortedTargets, id)
			}
			slices.Sort(sortedTargets)
			for _, id := range sortedTargets {
				response.CrossDomainLinks = append(response.CrossDomainLinks, crossLinksByTarget[id])
			}
		}
	}

	// Direct entity links (cross-domain).
	links, err := linkDAO.ListByEntity(ctx, ent.ID)
	if err == nil {
		for _, link := range links {
			var targetID int
			if link.SubjectEntityID == ent.ID {
				targetID = link.TargetEntityID
			} else {
				targetID = link.SubjectEntityID
			}

			targetEnt, err := entityDAO.GetByID(ctx, targetID)
			if err != nil || targetEnt == nil {
				continue
			}

			crossLink := CrossDomainLink{
				TargetEntityID: targetID,
				TargetName:     targetEnt.Name,
				TargetDomain:   targetEnt.Domain,
				RelationTypes:  []string{link.RelationType},
				Method:         link.Method,
				Confidence:     link.Confidence,
			}
			if link.Evidence != nil {
				crossLink.Evidence = *link.Evidence
			}

			// Deduplicate by target_entity_id only; merge relation_types.
			found := false
			for i, existing := range response.CrossDomainLinks {
				if existing.TargetEntityID == targetID {
					if hasBetterProvenance(crossLink, existing) {
						crossLink.RelationTypes = mergeRelationTypes(existing.RelationTypes, link.RelationType)
						response.CrossDomainLinks[i] = crossLink
					} else {
						existing.RelationTypes = mergeRelationTypes(existing.RelationTypes, link.RelationType)
						response.CrossDomainLinks[i] = existing
					}
					found = true
					break
				}
			}
			if !found {
				response.CrossDomainLinks = append(response.CrossDomainLinks, crossLink)
			}
		}
	}

	// Sort cross_domain_links by target_entity_id for deterministic output.
	slices.SortFunc(response.CrossDomainLinks, func(a, b CrossDomainLink) int {
		return a.TargetEntityID - b.TargetEntityID
	})

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error marshaling response: %v", err)),
			},
			IsError: true,
		}, nil
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(string(jsonBytes)),
		},
	}, nil
}

// hasBetterProvenance returns true if candidate has more complete provenance than existing.
// A link with higher confidence or non-empty method/evidence is preferred.
// On full tie (equal confidence, same method/evidence presence), uses a deterministic
// tiebreaker: compare method strings lexicographically, then evidence strings,
// then target_entity_id numerically. This ensures the choice does not depend on edge processing order.
func hasBetterProvenance(candidate, existing CrossDomainLink) bool {
	if candidate.Confidence > existing.Confidence {
		return true
	}
	if candidate.Confidence < existing.Confidence {
		return false
	}
	// Same confidence: prefer non-empty method over empty.
	if candidate.Method != "" && existing.Method == "" {
		return true
	}
	if candidate.Method == "" && existing.Method != "" {
		return false
	}
	// Both have method or both lack it; compare evidence presence.
	if candidate.Evidence != "" && existing.Evidence == "" {
		return true
	}
	if candidate.Evidence == "" && existing.Evidence != "" {
		return false
	}
	// Full tie on confidence, method presence, evidence presence: deterministic tiebreaker.
	if candidate.Method != existing.Method {
		return candidate.Method < existing.Method // lexicographic
	}
	if candidate.Evidence != existing.Evidence {
		return candidate.Evidence < existing.Evidence // lexicographic
	}
	return candidate.TargetEntityID < existing.TargetEntityID // numeric
}

// EntityDossierResponse is the JSON response format for get_entity_dossier.
type EntityDossierResponse struct {
	Entity           DossierEntityInfo `json:"entity"`
	Facts            []DossierFact     `json:"facts,omitempty"`
	Sources          []SourceDoc       `json:"sources,omitempty"`
	RelatedEntities  []RelatedEntity   `json:"related_entities,omitempty"`
	CrossDomainLinks []CrossDomainLink `json:"cross_domain_links,omitempty"`
}

// DossierEntityInfo contains entity data for the dossier.
type DossierEntityInfo struct {
	ID          int     `json:"id"`
	Name        string  `json:"name"`
	Type        string  `json:"type"`
	Domain      string  `json:"domain"`
	Description string  `json:"description,omitempty"`
	Confidence  float64 `json:"confidence,omitempty"`
	Metadata    string  `json:"metadata,omitempty"`
}

// DossierFact contains fact data for the dossier.
type DossierFact struct {
	ID              int              `json:"id"`
	Predicate       string           `json:"predicate"`
	SubjectEntityID int              `json:"subject_entity_id,omitempty"`
	ObjectEntityID  int              `json:"object_entity_id,omitempty"`
	Domain          string           `json:"domain"`
	Metadata        string           `json:"metadata,omitempty"`
	Status          string           `json:"status"`
	ValidFrom       string           `json:"valid_from,omitempty"`
	ValidTo         string           `json:"valid_to,omitempty"`
	Weight          int              `json:"weight"`
	Sources         []FactSourceInfo `json:"sources,omitempty"`
}

// SourceDoc contains document source data.
type SourceDoc struct {
	ID           int    `json:"id"`
	SourceType   string `json:"source_type"`
	OriginalPath string `json:"original_path"`
	Metadata     string `json:"metadata,omitempty"`
}

// RelatedEntity contains related entity data for the dossier.
type RelatedEntity struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Domain string `json:"domain"`
}

// DossierEdge contains edge metadata for related entities.
type DossierEdge struct {
	RelationType string `json:"relation_type,omitempty"`
	Domain       string `json:"domain,omitempty"`
	Metadata     string `json:"metadata,omitempty"`
}

// CrossDomainLink contains cross-domain link data.
type CrossDomainLink struct {
	TargetEntityID int      `json:"target_entity_id"`
	TargetName     string   `json:"target_name"`
	TargetDomain   string   `json:"target_domain"`
	RelationTypes  []string `json:"relation_types,omitempty"`
	Method         string   `json:"method,omitempty"`
	Confidence     float64  `json:"confidence,omitempty"`
	Evidence       string   `json:"evidence,omitempty"`
}

// mergeRelationTypes adds relType to existing types if not already present,
// preserving encounter order and deduplicating within the array.
func mergeRelationTypes(types []string, relType string) []string {
	for _, t := range types {
		if t == relType {
			return types
		}
	}
	return append(types, relType)
}
