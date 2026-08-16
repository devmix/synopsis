// Package handlers implements MCP tool handlers for the Synopsis RAG service.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/devmix/synopsis/internal/search"

	"github.com/mark3labs/mcp-go/mcp"
)

// DomainValidator checks whether a domain name is known to the knowledge base.
type DomainValidator interface {
	IsKnownDomain(domain string) bool
}

// HandleSearch processes a search tool call.
// It performs combined lexical and semantic search with RRF fusion.
func HandleSearch(
	ctx context.Context,
	req mcp.CallToolRequest,
	searcher search.Searcher,
	domainValidator DomainValidator,
) (*mcp.CallToolResult, error) {
	query := req.GetString("query", "")
	if query == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent("Error: 'query' argument is required and must not be empty"),
			},
			IsError: true,
		}, nil
	}

	topK := req.GetInt("top_k", 10)
	if topK < 1 || topK > 100 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error: 'top_k' must be between 1 and 100, got %d", topK)),
			},
			IsError: true,
		}, nil
	}

	// Extract optional domain filter.
	domain := req.GetString("domain", "")

	start := time.Now()

	results, err := searcher.HybridSearch(ctx, query, topK, domain)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error during search: %v", err)),
			},
			IsError: true,
		}, nil
	}

	searchTime := time.Since(start).Milliseconds()

	response := SearchResponse{
		Results:      make([]SearchResultItem, 0, len(results)),
		TotalCount:   len(results),
		SearchTimeMs: searchTime,
	}

	// Warn if the requested domain is unknown.
	if domain != "" && domainValidator != nil && !domainValidator.IsKnownDomain(domain) {
		response.Warning = fmt.Sprintf("unknown domain %q; results may be incomplete", domain)
	}

	for _, r := range results {
		item := SearchResultItem{
			DocumentID:  r.DocumentID,
			ChunkID:     r.ChunkID,
			Text:        r.ChunkText,
			SequenceNum: r.SequenceNum,
			Score:       r.Score,
			SourceType:  r.SourceType,
		}
		if r.StartOffset != nil {
			item.StartOffset = *r.StartOffset
		}
		if r.EndOffset != nil {
			item.EndOffset = *r.EndOffset
		}
		if r.DocumentPath != "" {
			item.DocumentPath = r.DocumentPath
		}

		// Include domains in response as []string.
		if domains, ok := r.Metadata["domains"].([]string); ok && len(domains) > 0 {
			item.Domains = domains
		}

		for _, ent := range r.Entities {
			item.Entities = append(item.Entities, EntityRef{
				ID:   ent.ID,
				Name: ent.Name,
				Type: ent.Type,
			})
		}
		response.Results = append(response.Results, item)
	}

	jsonBytes, err := json.Marshal(response)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				mcp.NewTextContent(fmt.Sprintf("Error marshaling results: %v", err)),
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

// SearchResponse is the JSON response format for search.
type SearchResponse struct {
	Results      []SearchResultItem `json:"results"`
	TotalCount   int                `json:"total_count"`
	SearchTimeMs int64              `json:"search_time_ms"`
	Warning      string             `json:"warning,omitempty"`
}

// SearchResultItem represents a single search result chunk.
type SearchResultItem struct {
	DocumentID   int         `json:"document_id"`
	ChunkID      int         `json:"chunk_id"`
	Text         string      `json:"text"`
	SequenceNum  int         `json:"sequence_num"`
	StartOffset  int         `json:"start_offset,omitempty"`
	EndOffset    int         `json:"end_offset,omitempty"`
	DocumentPath string      `json:"document_path"`
	Score        float64     `json:"score"`
	SourceType   string      `json:"source_type"`
	Domains      []string    `json:"domains,omitempty"`
	Entities     []EntityRef `json:"entities,omitempty"`
}

// EntityRef is a lightweight reference to an entity.
type EntityRef struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}
