package parsers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/logger"
)

func testLoggerJSON(t *testing.T) *logger.Logger {
	t.Helper()
	l, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create test logger: %v", err)
	}
	return l
}

func TestJSONParser_Parse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		files     map[string]string // filename -> content
		wantCount int
	}{
		{
			name: "single json file",
			files: map[string]string{
				"data.json": `{"title": "Test"}`,
			},
			wantCount: 1,
		},
		{
			name: "array json file",
			files: map[string]string{
				"items.json": `[{"id": 1}, {"id": 2}]`,
			},
			wantCount: 1,
		},
		{
			name: "skip non-json files",
			files: map[string]string{
				"data.json": `{"key": "value"}`,
				"readme.md": "# Hello",
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

			parser := NewJSONParser(testLoggerJSON(t))
			result := parser.Parse(dir)

			if len(result.Documents) != tt.wantCount {
				t.Errorf("got %d documents, want %d", len(result.Documents), tt.wantCount)
			}

			for _, doc := range result.Documents {
				if doc.Metadata["source_type"] != "json" {
					t.Errorf("expected source_type=json, got %v", doc.Metadata["source_type"])
				}
			}
		})
	}
}

func TestJSONParser_DetectStructure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
		want string
	}{
		{"object", `{"key": "value"}`, "object"},
		{"array", `[1, 2, 3]`, "array"},
		{"empty object", `{}`, "object"},
		{"empty array", `[]`, "array"},
		{"invalid", `not json`, "unknown"},
	}

	p := NewJSONParser(testLoggerJSON(t))
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := p.detectStructure([]byte(tt.json))
			if got != tt.want {
				t.Errorf("detectStructure(%q) = %q, want %q", tt.json, got, tt.want)
			}
		})
	}
}
