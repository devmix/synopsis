package parsers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/logger"
)

func testLoggerWP(t *testing.T) *logger.Logger {
	t.Helper()
	l, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create test logger: %v", err)
	}
	return l
}

func TestWebpageParser_Parse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		files     map[string]string // filename -> content
		wantCount int
		checkDocs func(t *testing.T, docs []ingestion.Document)
	}{
		{
			name: "flat md pages produce one document each",
			files: map[string]string{
				"pages/index.md":   "# Home\nWelcome to the site.",
				"pages/about.md":   "# About\nOur story.",
				"pages/contact.md": "# Contact\nEmail us.",
			},
			wantCount: 3,
		},
		{
			name: "html page converted to markdown",
			files: map[string]string{
				"pages/page-1.html": "<h1>Title</h1><p>Body text.</p>",
			},
			wantCount: 1,
			checkDocs: func(t *testing.T, docs []ingestion.Document) {
				t.Helper()
				if docs[0].Content == "" {
					t.Error("expected non-empty converted markdown content")
				}
			},
		},
		{
			name: "md preferred over html for same page name",
			files: map[string]string{
				"pages/page-1.md":   "# MD Content\nThis should win.",
				"pages/page-1.html": "<h1>HTML Content</h1><p>This should lose.</p>",
			},
			wantCount: 1,
			checkDocs: func(t *testing.T, docs []ingestion.Document) {
				t.Helper()
				if docs[0].Content != "# MD Content\nThis should win." {
					t.Errorf("expected md content, got %q", docs[0].Content)
				}
			},
		},
		{
			name: "static directory excluded",
			files: map[string]string{
				"pages/index.md":       "# Home",
				"static/logo.png":      "binary data",
				"static/images/bg.jpg": "more binary",
			},
			wantCount: 1,
		},
		{
			name: "mixed md and html pages",
			files: map[string]string{
				"pages/home.md":       "# Home MD",
				"pages/pricing.html":  "<h1>Pricing</h1><p>Plans below.</p>",
				"pages/docs.md":       "# Docs\nAPI reference.",
			},
			wantCount: 3,
		},
		{
			name: "subdirectory pages collected",
			files: map[string]string{
				"pages/blog/post-1.md":     "# Post One",
				"pages/blog/post-2.html":   "<h1>Post Two</h1>",
			},
			wantCount: 2,
		},
		{
			name: "non-md-html files ignored",
			files: map[string]string{
				"pages/index.md":   "# Home",
				"pages/readme.txt": "not a content file",
				"pages/data.json":  `{"key":"value"}`,
			},
			wantCount: 1,
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

			parser := NewWebpageParser(testLoggerWP(t))
			result := parser.Parse(dir)

			if len(result.Documents) != tt.wantCount {
				t.Errorf("got %d documents, want %d", len(result.Documents), tt.wantCount)
			}

			for _, doc := range result.Documents {
				if doc.Metadata["source_type"] != "webpages" {
					t.Errorf("expected source_type=webpages, got %v", doc.Metadata["source_type"])
				}
				if _, ok := doc.Metadata["source_file"]; !ok {
					t.Error("missing source_file in metadata")
				}
				if _, ok := doc.Metadata["file_size"]; !ok {
					t.Error("missing file_size in metadata")
				}
			}

			if tt.checkDocs != nil && len(result.Documents) > 0 {
				tt.checkDocs(t, result.Documents)
			}
		})
	}
}

func TestWebpageParser_SourceFileMetadata(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	files := map[string]string{
		"pages/index.md":   "# Home",
		"pages/about.html": "<h1>About</h1>",
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

	parser := NewWebpageParser(testLoggerWP(t))
	result := parser.Parse(dir)

	if len(result.Documents) != 2 {
		t.Fatalf("expected 2 documents, got %d", len(result.Documents))
	}

	sourceFiles := make(map[string]bool)
	for _, doc := range result.Documents {
		sf, ok := doc.Metadata["source_file"].(string)
		if !ok {
			t.Fatal("source_file not a string")
		}
		sourceFiles[sf] = true
	}

	if !sourceFiles["pages/index.md"] {
		t.Error("expected pages/index.md in source_files")
	}
	if !sourceFiles["pages/about.html"] {
		t.Error("expected pages/about.html in source_files")
	}
}

func TestWebpageParser_HTMLConversion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Write a valid HTML file.
	htmlPath := filepath.Join(dir, "pages/broken.html")
	if err := os.MkdirAll(filepath.Dir(htmlPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(htmlPath, []byte("<h1>Valid HTML</h1><p>Content.</p>"), 0o644); err != nil {
		t.Fatal(err)
	}

	parser := NewWebpageParser(testLoggerWP(t))
	result := parser.Parse(dir)

	if len(result.Documents) != 1 {
		t.Fatalf("expected 1 document, got %d", len(result.Documents))
	}
	if len(result.Errors) > 0 {
		t.Errorf("expected no errors for valid HTML, got %v", result.Errors)
	}
}

func TestWebpageParser_SkipDirs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	files := map[string]string{
		"pages/index.md":      "# Home",
		".git/config":         "[core]",
		"node_modules/pkg.js": "module.exports = {}",
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

	parser := NewWebpageParser(testLoggerWP(t))
	result := parser.Parse(dir)

	if len(result.Documents) != 1 {
		t.Errorf("expected 1 document (skipDirs respected), got %d", len(result.Documents))
	}
}
