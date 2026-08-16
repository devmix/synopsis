package graph

import (
	"fmt"
	"strings"

	"github.com/devmix/synopsis/internal/utils"
)

// FindEntityExact performs an O(1) case-insensitive lookup by exact name.
// If domain is non-empty, it matches only within that domain.
func (g *Graph) FindEntityExact(name string, domain string) (*EntityNode, bool) {
	if name == "" {
		return nil, false
	}
	id, ok := g.GetEntityIDByName(name, domain)
	if !ok {
		return nil, false
	}
	return g.GetNode(id)
}

// FindEntityPartial returns all entities whose name contains the pattern (case-insensitive).
// Results are sorted by relevance: exact prefix matches first, then substring matches.
// If domain is non-empty, it matches only within that domain.
func (g *Graph) FindEntityPartial(pattern string, domain string) ([]*EntityNode, error) {
	if pattern == "" {
		return nil, fmt.Errorf("pattern must not be empty")
	}

	lower := strings.ToLower(pattern)
	domName := utils.Normalize(domain)

	var exactPrefix []*EntityNode // name starts with pattern
	var substring []*EntityNode   // name contains pattern

	for _, node := range g.nodes {
		if domName != "" && node.Domain != domName {
			continue
		}
		nameLower := strings.ToLower(node.Name)
		if nameLower == lower {
			continue // skip exact match — caller should use FindEntityExact for that
		}
		if strings.HasPrefix(nameLower, lower) {
			exactPrefix = append(exactPrefix, node)
		} else if strings.Contains(nameLower, lower) {
			substring = append(substring, node)
		}
	}

	// Combine: prefix matches first (higher relevance), then substring.
	result := make([]*EntityNode, 0, len(exactPrefix)+len(substring))
	result = append(result, exactPrefix...)
	result = append(result, substring...)

	return result, nil
}

// FindEntitiesByType returns all entities of the given type, optionally filtered by domain.
func (g *Graph) FindEntitiesByType(entityType string, domain string) ([]*EntityNode, error) {
	if entityType == "" {
		return nil, fmt.Errorf("entity type must not be empty")
	}

	ids := g.NodesByType(entityType, domain)
	if len(ids) == 0 {
		return nil, nil
	}

	nodes := make([]*EntityNode, 0, len(ids))
	for _, id := range ids {
		node, ok := g.GetNode(id)
		if !ok {
			continue
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}
