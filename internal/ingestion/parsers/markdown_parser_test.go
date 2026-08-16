package parsers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/logger"
)

func testLoggerMD(t *testing.T) *logger.Logger {
	t.Helper()
	l, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create test logger: %v", err)
	}
	return l
}

func TestMarkdownParser_Parse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		files      map[string]string // filename -> content
		wantCount  int
		wantErrors bool
	}{
		{
			name: "single md file",
			files: map[string]string{
				"readme.md": "# Hello\nWorld",
			},
			wantCount: 1,
		},
		{
			name: "multiple md files",
			files: map[string]string{
				"a.md":        "# A",
				"b.md":        "# B",
				"c/nested.md": "# C Nested",
			},
			wantCount: 3,
		},
		{
			name: "skip non-md files",
			files: map[string]string{
				"readme.md": "# Hello",
				"data.json": `{"key": "value"}`,
				"image.png": "binary",
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

			parser := NewMarkdownParser(testLoggerMD(t))
			result := parser.Parse(dir)

			if len(result.Documents) != tt.wantCount {
				t.Errorf("got %d documents, want %d", len(result.Documents), tt.wantCount)
			}

			for _, doc := range result.Documents {
				if doc.Metadata["source_type"] != "markdown" {
					t.Errorf("expected source_type=markdown, got %v", doc.Metadata["source_type"])
				}
				if doc.SourcePath == "" {
					t.Error("SourcePath is empty")
				}
				if doc.Content == "" && tt.wantCount > 0 {
					t.Error("Content is empty for non-empty file")
				}
			}

			if tt.wantErrors && len(result.Errors) == 0 {
				t.Error("expected errors, got none")
			}
		})
	}
}

func TestMarkdownParser_SkipSystemDirs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Create files in system directories that should be skipped.
	for _, subDir := range []string{".git", "node_modules"} {
		path := filepath.Join(dir, subDir, "file.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("# Should be skipped"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create a valid file.
	validPath := filepath.Join(dir, "valid.md")
	if err := os.WriteFile(validPath, []byte("# Valid"), 0o644); err != nil {
		t.Fatal(err)
	}

	parser := NewMarkdownParser(testLoggerMD(t))
	result := parser.Parse(dir)

	if len(result.Documents) != 1 {
		t.Errorf("got %d documents (system dirs should be skipped), want 1", len(result.Documents))
	}
}
