package parsers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/logger"
)

// MediawikiPage represents a parsed mediawiki page JSON structure.
type MediawikiPage struct {
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	Wikitext    string   `json:"wikitext"`
	HTML        string   `json:"html"`
	Images      []string `json:"images"`
	Links       []string `json:"links"`
	Categories  []string `json:"categories"`
	EntityType  string   `json:"entity_type"`
	Description string   `json:"description,omitempty"`
}

// GraphEdge represents a single relationship between two pages in graph.json.
type GraphEdge struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Type   string `json:"type,omitempty"`
}

// MediawikiParser parses mediawiki dataset directories.
// Expected structure:
//
//	sourcePath/
//	  <space>/
//	    <wiki-type>/
//	      by-type/
//	        <entity-type>/
//	          <page-name>.json
//	      graph.json (optional) — parsed and relations stored in document metadata
type MediawikiParser struct {
	log *logger.Logger
}

// NewMediawikiParser creates a parser with the given logger (required).
func NewMediawikiParser(log *logger.Logger) *MediawikiParser {
	return &MediawikiParser{log: log}
}

// SupportedExtensions returns the file extensions supported by this parser.
func (*MediawikiParser) SupportedExtensions() []string { return []string{".json"} }

// Parse walks the mediawiki source path and returns parsed documents.
func (p *MediawikiParser) Parse(sourcePath string) ingestion.ParseResult {
	var docs []ingestion.Document
	var errs []error

	// First pass: collect graph.json data if present.
	graphRelations := p.loadGraphJSON(sourcePath)

	// Walk through space/wiki-type/by-type/entity-type/page.json structure.
	err := filepath.WalkDir(sourcePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			return nil
		}

		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}

		// Process page JSON files (not graph.json).
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".json") &&
			!strings.EqualFold(d.Name(), "graph.json") {
			doc, err := p.parsePageFile(path, sourcePath, graphRelations)
			if err != nil {
				errs = append(errs, err)
				return nil
			}
			docs = append(docs, doc)
		}

		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}

	p.log.Debug("parse complete", "source_path", sourcePath, "files_found", len(docs), "errors", len(errs))
	for _, e := range errs {
		p.log.Warn("parse error", logger.Err(e))
	}

	return ingestion.ParseResult{Documents: docs, Errors: errs}
}

// loadGraphJSON finds and parses graph.json files in the source tree.
// Returns a map from page title to list of related page titles.
func (p *MediawikiParser) loadGraphJSON(sourcePath string) map[string][]string {
	result := make(map[string][]string)

	err := filepath.WalkDir(sourcePath, func(path string, d os.DirEntry, err error) error {
		if err != nil || !strings.EqualFold(d.Name(), "graph.json") {
			return err
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil // skip unreadable graph files
		}

		var graph map[string][]string
		if unmarshalErr := json.Unmarshal(data, &graph); unmarshalErr != nil {
			return nil // skip malformed graph files
		}

		for source, targets := range graph {
			result[source] = append(result[source], targets...)
		}

		return nil
	})
	_ = err // walk errors are non-fatal; return partial results

	return result
}

// parsePageFile reads a single mediawiki page JSON and returns a Document.
func (p *MediawikiParser) parsePageFile(path, baseDir string, graphRelations map[string][]string) (ingestion.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ingestion.Document{}, err
	}

	var page MediawikiPage
	if err := json.Unmarshal(data, &page); err != nil {
		return ingestion.Document{}, err
	}

	// Determine content: wikitext > html > title + description.
	content := p.extractContent(page)

	relPath, _ := filepath.Rel(baseDir, path)

	// Extract space and wiki-type from path structure.
	space, wikiType, entityType := p.extractPathComponents(path, baseDir)

	meta := map[string]interface{}{
		"source_type": "mediawiki",
		"source_file": relPath,
		"title":       page.Title,
		"url":         page.URL,
		"space":       space,
		"wiki_type":   wikiType,
		"entity_type": entityType,
		"page_links":  page.Links,
		"image_paths": page.Images,
		"categories":  page.Categories,
	}

	// Enrich with graph.json relations if available.
	if rels, ok := graphRelations[page.Title]; ok {
		meta["graph_relations"] = rels
	}

	return ingestion.Document{
		SourcePath: path,
		Content:    content,
		Metadata:   meta,
	}, nil
}

// extractContent selects the best available text content from a mediawiki page.
func (p *MediawikiParser) extractContent(page MediawikiPage) string {
	if page.Wikitext != "" {
		return page.Wikitext
	}
	if page.HTML != "" {
		return page.HTML
	}
	parts := []string{page.Title, page.Description}
	var nonEmpty []string
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	return strings.Join(nonEmpty, "\n\n")
}

// extractPathComponents derives space, wiki_type, and entity_type from the file path.
func (p *MediawikiParser) extractPathComponents(path, baseDir string) (space, wikiType, entityType string) {
	rel, _ := filepath.Rel(baseDir, path)
	parts := strings.Split(rel, string(filepath.Separator))

	if len(parts) >= 2 {
		space = parts[0]
	}
	if len(parts) >= 3 {
		wikiType = parts[1]
	}
	if len(parts) >= 5 && strings.EqualFold(parts[len(parts)-3], "by-type") {
		entityType = parts[len(parts)-2]
	}

	return space, wikiType, entityType
}
