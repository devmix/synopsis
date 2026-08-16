package parsers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/logger"
)

func testLoggerMW(t *testing.T) *logger.Logger {
	t.Helper()
	l, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create test logger: %v", err)
	}
	return l
}

func TestMediawikiParser_Parse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		files     map[string]string // filename -> content
		wantCount int
	}{
		{
			name: "single page",
			files: map[string]string{
				"space/wiki-type/by-type/services/api_gateway.json": `{
					"title": "API Gateway",
					"url": "https://example.com/API_Gateway",
					"wikitext": "== API Gateway ==\nA service mesh component.",
					"html": "<div>HTML content</div>",
					"images": ["gateway.png"],
					"links": ["Service Catalog"],
					"categories": ["Services", "Networking"],
					"entity_type": "service"
				}`,
			},
			wantCount: 1,
		},
		{
			name: "skip graph.json",
			files: map[string]string{
				"space/wiki-type/graph.json": `{"page1": ["page2"]}`,
				"space/wiki-type/by-type/systems/database.json": `{
					"title": "Database",
					"wikitext": "A data storage system."
				}`,
			},
			wantCount: 1, // graph.json should be skipped
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for name, content := range tt.files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			parser := NewMediawikiParser(testLoggerMW(t))
			result := parser.Parse(dir)

			if len(result.Documents) != tt.wantCount {
				t.Errorf("got %d documents, want %d", len(result.Documents), tt.wantCount)
			}

			for _, doc := range result.Documents {
				if doc.Metadata["source_type"] != "mediawiki" {
					t.Errorf("expected source_type=mediawiki, got %v", doc.Metadata["source_type"])
				}
				if title, ok := doc.Metadata["title"].(string); !ok || title == "" {
					t.Error("missing or empty title in metadata")
				}
			}
		})
	}
}

func TestMediawikiParser_ExtractContent(t *testing.T) {
	t.Parallel()

	p := NewMediawikiParser(testLoggerMW(t))

	tests := []struct {
		name string
		page MediawikiPage
		want string
	}{
		{
			name: "wikitext priority",
			page: MediawikiPage{
				Wikitext: "== Wiki Content ==",
				HTML:     "<div>HTML</div>",
				Title:    "Title",
			},
			want: "== Wiki Content ==",
		},
		{
			name: "html fallback",
			page: MediawikiPage{
				HTML:  "<p>Fallback HTML</p>",
				Title: "Title",
			},
			want: "<p>Fallback HTML</p>",
		},
		{
			name: "title + description last resort",
			page: MediawikiPage{
				Title:       "Just Title",
				Description: "With description.",
			},
			want: "Just Title\n\nWith description.",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := p.extractContent(tt.page)
			if got != tt.want {
				t.Errorf("extractContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMediawikiParser_ExtractPathComponents(t *testing.T) {
	t.Parallel()

	p := NewMediawikiParser(testLoggerMW(t))

	tests := []struct {
		name         string
		path         string
		baseDir      string
		wantSpace    string
		wantWikiType string
		wantEntity   string
	}{
		{
			name:         "full path",
			path:         "/data/devmix/internal/by-type/services/api_gateway.json",
			baseDir:      "/data",
			wantSpace:    "devmix",
			wantWikiType: "internal",
			wantEntity:   "services",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			space, wikiType, entityType := p.extractPathComponents(tt.path, tt.baseDir)
			if space != tt.wantSpace {
				t.Errorf("space = %q, want %q", space, tt.wantSpace)
			}
			if wikiType != tt.wantWikiType {
				t.Errorf("wiki_type = %q, want %q", wikiType, tt.wantWikiType)
			}
			if entityType != tt.wantEntity {
				t.Errorf("entity_type = %q, want %q", entityType, tt.wantEntity)
			}
		})
	}
}

func TestMediawikiParser_GraphJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	files := map[string]string{
		"space/wiki-type/graph.json": `{
			"API Gateway": ["Service Catalog", "Load Balancer"],
			"Database": ["Storage"]
		}`,
		"space/wiki-type/by-type/services/api_gateway.json": `{
			"title": "API Gateway",
			"wikitext": "== API Gateway ==",
			"entity_type": "service"
		}`,
		"space/wiki-type/by-type/systems/database.json": `{
			"title": "Database",
			"wikitext": "== Database ==",
			"entity_type": "system"
		}`,
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	parser := NewMediawikiParser(testLoggerMW(t))
	result := parser.Parse(dir)

	if len(result.Documents) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(result.Documents))
	}

	// Check that graph relations are stored in metadata.
	for _, doc := range result.Documents {
		title, _ := doc.Metadata["title"].(string)
		rels, hasRels := doc.Metadata["graph_relations"]

		switch title {
		case "API Gateway":
			if !hasRels {
				t.Error("API Gateway should have graph_relations")
			} else {
				relSlice, ok := rels.([]string)
				if !ok || len(relSlice) != 2 {
					t.Errorf("API Gateway relations = %v, want [Service Catalog, Load Balancer]", relSlice)
				}
			}
		case "Database":
			if !hasRels {
				t.Error("Database should have graph_relations")
			} else {
				relSlice, ok := rels.([]string)
				if !ok || len(relSlice) != 1 {
					t.Errorf("Database relations = %v, want [Storage]", relSlice)
				}
			}
		default:
			t.Errorf("unexpected title: %s", title)
		}
	}
}

func TestMediawikiParser_LoadGraphJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	files := map[string]string{
		"space/wiki-type/graph.json": `{"A": ["B", "C"], "D": ["E"]}`,
	}

	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	parser := NewMediawikiParser(testLoggerMW(t))
	graph := parser.loadGraphJSON(dir)

	if len(graph["A"]) != 2 {
		t.Errorf("graph[A] has %d entries, want 2", len(graph["A"]))
	}
	if len(graph["D"]) != 1 {
		t.Errorf("graph[D] has %d entries, want 1", len(graph["D"]))
	}
}

func TestMediawikiParser_LoadGraphJSON_Missing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// No graph.json file.

	parser := NewMediawikiParser(testLoggerMW(t))
	graph := parser.loadGraphJSON(dir)

	if len(graph) != 0 {
		t.Errorf("expected empty graph, got %d entries", len(graph))
	}
}
