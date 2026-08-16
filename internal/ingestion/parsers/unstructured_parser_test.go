package parsers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/logger"
)

func testLogger(t *testing.T) *logger.Logger {
	t.Helper()
	l, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create test logger: %v", err)
	}
	return l
}

func TestUnstructuredParser_Parse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		files     map[string]string // filename -> content
		wantCount int
	}{
		{
			name: "single md file",
			files: map[string]string{
				"docs/readme.md": "# Hello\nSome text.",
			},
			wantCount: 1,
		},
		{
			name: "multiple md files",
			files: map[string]string{
				"docs/a.md": "# A",
				"docs/b.md": "# B",
				"docs/c.txt": "not markdown",
			},
			wantCount: 2,
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

			parser := NewUnstructuredParser(testLogger(t))
			result := parser.Parse(dir)

			if len(result.Documents) != tt.wantCount {
				t.Errorf("got %d documents, want %d", len(result.Documents), tt.wantCount)
			}

			for _, doc := range result.Documents {
				if doc.Metadata["source_type"] != "unstructured" {
					t.Errorf("expected source_type=unstructured, got %v", doc.Metadata["source_type"])
				}
			}
		})
	}
}

func TestUnstructuredParser_ImageAssociation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	files := map[string]string{
		"docs/article.md": "# Article\n![hero](banner.png)",
		"docs/banner.png":  "image data",
		"docs/logo.svg":    "<svg/>",
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

	parser := NewUnstructuredParser(testLogger(t))
	result := parser.Parse(dir)

	if len(result.Documents) != 1 {
		t.Fatalf("expected 1 document, got %d", len(result.Documents))
	}

	imgPaths, ok := result.Documents[0].Metadata["image_paths"].([]string)
	if !ok {
		t.Fatal("missing image_paths in metadata")
	}

	if len(imgPaths) != 2 {
		t.Errorf("expected 2 images, got %d: %v", len(imgPaths), imgPaths)
	}
}

func TestUnstructuredParser_GroupSections(t *testing.T) {
	t.Parallel()

	parser := NewUnstructuredParser(testLogger(t))

	tests := []struct {
		name      string
		content   string
		wantCount int
	}{
		{
			name: "no headings",
			content: "Just plain text without any headings.",
			wantCount: 1, // single untitled section
		},
		{
			name: "two sections",
			content: "# Section A\nContent A.\n\n# Section B\nContent B.",
			wantCount: 2,
		},
		{
			name:      "empty content",
			content:   "",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sections := parser.GroupSections(tt.content)
			if len(sections) != tt.wantCount {
				t.Errorf("got %d sections, want %d", len(sections), tt.wantCount)
			}
		})
	}
}
