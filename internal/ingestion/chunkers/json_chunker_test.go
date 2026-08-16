package chunkers

import (
	"testing"

	"github.com/devmix/synopsis/internal/logger"
)

func testLoggerChunk(t *testing.T) *logger.Logger {
	t.Helper()
	l, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create test logger: %v", err)
	}
	return l
}

func TestJSONChunker_ChunkArray(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		config    JSONChunkerConfig
		wantCount int
	}{
		{
			name: "array of objects combined",
			content: `[
				{"title": "First", "description": "Desc 1"},
				{"title": "Second", "description": "Desc 2"}
			]`,
			config: JSONChunkerConfig{
				TextFields:    []string{"title", "description"},
				CombineFields: true,
			},
			wantCount: 2,
		},
		{
			name: "array of objects per field",
			content: `[
				{"title": "First", "description": "Desc 1"}
			]`,
			config: JSONChunkerConfig{
				TextFields:    []string{"title", "description"},
				CombineFields: false,
			},
			wantCount: 2, // title + description = 2 chunks for 1 object
		},
		{
			name:    "empty array",
			content: `[]`,
			config: JSONChunkerConfig{
				TextFields:    []string{"title"},
				CombineFields: true,
			},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			chunker := NewJSONChunker(tt.config, testLoggerChunk(t))
			got, err := chunker.Chunk(tt.content, map[string]interface{}{"source_file": "test.json"})
			if err != nil {
				t.Fatalf("ChunkObject() error = %v", err)
			}

			if len(got) != tt.wantCount {
				t.Errorf("got %d chunks, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestJSONChunker_ChunkObject(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		config    JSONChunkerConfig
		wantCount int
	}{
		{
			name:    "single object combined",
			content: `{"title": "Page Title", "description": "Full description here."}`,
			config: JSONChunkerConfig{
				TextFields:    []string{"title", "description"},
				CombineFields: true,
			},
			wantCount: 1,
		},
		{
			name:    "single object per field",
			content: `{"title": "Page Title", "description": "Full description here."}`,
			config: JSONChunkerConfig{
				TextFields:    []string{"title", "description"},
				CombineFields: false,
			},
			wantCount: 2,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			chunker := NewJSONChunker(tt.config, testLoggerChunk(t))
			got, err := chunker.Chunk(tt.content, nil)
			if err != nil {
				t.Fatalf("ChunkObject() error = %v", err)
			}

			if len(got) != tt.wantCount {
				t.Errorf("got %d chunks, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestJSONChunker_InvalidJSON(t *testing.T) {
	t.Parallel()

	chunker := NewJSONChunker(DefaultJSONChunkerConfig(), testLoggerChunk(t))
	_, err := chunker.Chunk(`{invalid json}`, nil)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestJSONChunker_MaxObjects(t *testing.T) {
	t.Parallel()

	content := `[
		{"title": "A"}, {"title": "B"}, {"title": "C"}
	]`

	chunker := NewJSONChunker(JSONChunkerConfig{
		TextFields:    []string{"title"},
		CombineFields: true,
		MaxObjects:    2,
	}, testLoggerChunk(t))

	got, err := chunker.Chunk(content, nil)
	if err != nil {
		t.Fatalf("ChunkObject() error = %v", err)
	}

	if len(got) != 2 {
		t.Errorf("got %d chunks with MaxObjects=2, want 2", len(got))
	}
}

func TestJoinStrings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		parts []string
		sep   string
		want  string
	}{
		{
			name:  "empty parts",
			parts: []string{},
			sep:   ",",
			want:  "",
		},
		{
			name:  "single part",
			parts: []string{"hello"},
			sep:   ",",
			want:  "hello",
		},
		{
			name:  "multiple parts with newline sep",
			parts: []string{"a", "b", "c"},
			sep:   "\n\n",
			want:  "a\n\nb\n\nc",
		},
		{
			name:  "two parts with comma sep",
			parts: []string{"first", "second"},
			sep:   ", ",
			want:  "first, second",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := joinStrings(tt.parts, tt.sep)
			if got != tt.want {
				t.Errorf("joinStrings(%v, %q) = %q, want %q", tt.parts, tt.sep, got, tt.want)
			}
		})
	}
}
