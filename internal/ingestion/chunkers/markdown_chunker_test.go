package chunkers

import (
	"strings"
	"testing"
)

func TestMarkdownChunker_HeaderOnlySkip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantCount int
	}{
		{
			name: "h1 without body skipped, two h2 with body kept",
			content: `# A

## A.1
text under a1

## A.2
text under a2`,
			wantCount: 2,
		},
		{
			name: "all headers have bodies",
			content: `# Title
some intro text

## Section 1
body one

## Section 2
body two`,
			wantCount: 3, // preamble + 2 sections
		},
		{
			name: "only header-only headings produce zero chunks",
			content: `# A

## B

### C`,
			wantCount: 0,
		},
		{
			name: "header with body followed by header without body",
			content: `# Title
intro text

## Section 1
body here

## Empty Section`,
			wantCount: 2, // preamble + section 1 (empty section skipped)
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			chunker := NewMarkdownChunker(DefaultMarkdownChunkerConfig(), testLoggerChunk(t))
			got, err := chunker.Chunk(tt.content, nil)
			if err != nil {
				t.Fatalf("Chunk() error = %v", err)
			}

			if len(got) != tt.wantCount {
				t.Errorf("got %d chunks, want %d", len(got), tt.wantCount)
				for i, c := range got {
					t.Logf("chunk[%d]: %q", i, c.Text[:min(len(c.Text), 60)])
				}
			}
		})
	}
}

func TestMarkdownChunker_Breadcrumbs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		content        string
		wantCount      int
		wantBreadCrumb []string // expected breadcrumb values for each non-preamble chunk
	}{
		{
			name: "two-level hierarchy",
			content: `# A

## A.1
text under a1

## A.2
text under a2`,
			wantCount:      2,
			wantBreadCrumb: []string{"> A\n > A.1", "> A\n > A.2"},
		},
		{
			name: "three-level hierarchy",
			content: `# Chapter

## Section 1

### Subsection 1.1
deep content here`,
			wantCount:      1,
			wantBreadCrumb: []string{"> Chapter\n > Section 1\n  > Subsection 1.1"},
		},
		{
			name: "mixed levels with multiple children",
			content: `# Root

## Child A
content a

## Child B

### Grandchild B.1
content b1

### Grandchild B.2
content b2`,
			wantCount:      3, // child A + grandchild B.1 + grandchild B.2 (Child B skipped)
			wantBreadCrumb: []string{"> Root\n > Child A", "> Root\n > Child B\n  > Grandchild B.1", "> Root\n > Child B\n  > Grandchild B.2"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			chunker := NewMarkdownChunker(DefaultMarkdownChunkerConfig(), testLoggerChunk(t))
			got, err := chunker.Chunk(tt.content, nil)
			if err != nil {
				t.Fatalf("Chunk() error = %v", err)
			}

			if len(got) != tt.wantCount {
				t.Errorf("got %d chunks, want %d", len(got), tt.wantCount)
			}

			for i, wantBC := range tt.wantBreadCrumb {
				if i >= len(got) {
					break
				}
				gotBC, ok := got[i].Metadata["breadcrumb"].(string)
				if !ok {
					t.Errorf("chunk[%d]: missing breadcrumb in metadata", i)
					continue
				}
				if gotBC != wantBC {
					t.Errorf("chunk[%d] breadcrumb = %q, want %q", i, gotBC, wantBC)
				}
				// Also verify chunk_text starts with breadcrumb.
				if !strings.HasPrefix(got[i].Text, wantBC+"\n\n") {
					t.Errorf("chunk[%d] text does not start with breadcrumb prefix; got prefix %q", i, got[i].Text[:min(len(got[i].Text), len(wantBC)+10)])
				}
			}
		})
	}
}

func TestMarkdownChunker_CleanHeading(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "bold markers removed",
			input: "**Bold Heading**",
			want:  "Bold Heading",
		},
		{
			name:  "italic markers removed",
			input: "*Italic Heading*",
			want:  "Italic Heading",
		},
		{
			name:  "backticks removed",
			input: "`code` heading",
			want:  "code heading",
		},
		{
			name:  "numbering preserved",
			input: "**1.1 Section**",
			want:  "1.1 Section",
		},
		{
			name:  "mixed formatting removed",
			input: "*`code`* and **bold** in *heading*",
			want:  "code and bold in heading",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := cleanHeading(tt.input)
			if got != tt.want {
				t.Errorf("cleanHeading(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestMarkdownChunker_PreambleWithFileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		content    string
		sourceFile string
		wantPrefix string
		wantCount  int
	}{
		{
			name:       "preamble with file name prefix",
			content:    "This is intro text before any heading.\n\n## Section\nbody",
			sourceFile: "docs/guide.md",
			wantPrefix: "guide.md\n\nThis is intro text before any heading.",
			wantCount:  2, // preamble + section
		},
		{
			name:       "no headings entire content gets file name prefix",
			content:    "Just plain text without headers.",
			sourceFile: "readme.md",
			wantPrefix: "readme.md\n\nJust plain text without headers.",
			wantCount:  1,
		},
		{
			name:       "no source_file metadata no prefix added",
			content:    "Plain text.",
			sourceFile: "",
			wantPrefix: "Plain text.",
			wantCount:  1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			meta := make(map[string]interface{})
			if tt.sourceFile != "" {
				meta["source_file"] = tt.sourceFile
			}

			chunker := NewMarkdownChunker(DefaultMarkdownChunkerConfig(), testLoggerChunk(t))
			got, err := chunker.Chunk(tt.content, meta)
			if err != nil {
				t.Fatalf("Chunk() error = %v", err)
			}

			if len(got) != tt.wantCount {
				t.Errorf("got %d chunks, want %d", len(got), tt.wantCount)
			}

			if len(got) > 0 && !strings.HasPrefix(got[0].Text, tt.wantPrefix) {
				t.Errorf("first chunk text does not have expected prefix.\ngot:  %q\nwant: %q", got[0].Text[:min(len(got[0].Text), 80)], tt.wantPrefix)
			}
		})
	}
}

func TestMarkdownChunker_TextWithoutHeaders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantCount int
	}{
		{
			name:      "plain text single chunk",
			content:   "This is just plain text with no headers at all.",
			wantCount: 1,
		},
		{
			name:      "empty content returns nil",
			content:   "",
			wantCount: 0,
		},
		{
			name:      "whitespace only returns nil",
			content:   "   \n\n  ",
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			chunker := NewMarkdownChunker(DefaultMarkdownChunkerConfig(), testLoggerChunk(t))
			got, err := chunker.Chunk(tt.content, nil)
			if err != nil {
				t.Fatalf("Chunk() error = %v", err)
			}

			if len(got) != tt.wantCount {
				t.Errorf("got %d chunks, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestMarkdownChunker_FixedStrategy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		maxSize   int
		wantCount int
	}{
		{
			name:      "small content single chunk",
			content:   "Short text.",
			maxSize:   100,
			wantCount: 1,
		},
		{
			name:      "large content multiple chunks",
			content:   strings.Repeat("A", 300),
			maxSize:   100,
			wantCount: 3, // 300 chars / 100 per chunk with no overlap = 3 chunks
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := MarkdownChunkerConfig{
				Strategy:     "fixed",
				MaxChunkSize: tt.maxSize,
				OverlapSize:  0, // no overlap for predictable test results
			}
			chunker := NewMarkdownChunker(cfg, testLoggerChunk(t))
			got, err := chunker.Chunk(tt.content, nil)
			if err != nil {
				t.Fatalf("Chunk() error = %v", err)
			}

			if len(got) != tt.wantCount {
				t.Errorf("got %d chunks, want %d", len(got), tt.wantCount)
			}
		})
	}
}

func TestMarkdownChunker_HybridStrategy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		content   string
		wantCount int
	}{
		{
			name: "hybrid skips header-only sections",
			content: `# Root

## Section 1
body text here`,
			wantCount: 1, // root skipped (header only), section 1 kept
		},
		{
			name: "hybrid adds breadcrumbs",
			content: `# Chapter

## Section A
content a`,
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := MarkdownChunkerConfig{
				Strategy:       "hybrid",
				MaxChunkSize:   1000,
				OverlapSize:    100,
				MinSectionSize: 500,
			}
			chunker := NewMarkdownChunker(cfg, testLoggerChunk(t))
			got, err := chunker.Chunk(tt.content, nil)
			if err != nil {
				t.Fatalf("Chunk() error = %v", err)
			}

			if len(got) != tt.wantCount {
				t.Errorf("got %d chunks, want %d", len(got), tt.wantCount)
			}

			// Check breadcrumb exists for non-preamble chunks.
			for i, ch := range got {
				bc, ok := ch.Metadata["breadcrumb"].(string)
				if !ok || bc == "" {
					t.Errorf("chunk[%d]: expected non-empty breadcrumb", i)
				}
			}
		})
	}
}

func TestMarkdownChunker_IsHeaderOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "heading only",
			input: "# Title",
			want:  true,
		},
		{
			name:  "heading with empty lines after",
			input: "# Title\n\n\n",
			want:  true,
		},
		{
			name:  "heading with body",
			input: "# Title\n\nSome body text.",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isHeaderOnly(tt.input)
			if got != tt.want {
				t.Errorf("isHeaderOnly(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestMarkdownChunker_BuildBreadcrumbs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		idx     int // which heading index to build breadcrumbs for
		wantBC  string
	}{
		{
			name: "first heading is its own breadcrumb",
			content: `# A
text`,
			idx:    0,
			wantBC: "> A",
		},
		{
			name: "second level under first",
			content: `# A

## B
text`,
			idx:    1,
			wantBC: "> A\n > B",
		},
		{
			name: "third level full path",
			content: `# A

## B

### C
text`,
			idx:    2,
			wantBC: "> A\n > B\n  > C",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			headings := findHeadings(tt.content)
			got := buildBreadcrumbs(headings, tt.idx)
			if got != tt.wantBC {
				t.Errorf("buildBreadcrumbs(idx=%d) = %q, want %q", tt.idx, got, tt.wantBC)
			}
		})
	}
}

func TestMarkdownChunker_AcceptanceCriteria(t *testing.T) {
	// Acceptance criterion: Document "# A\n## A.1\ntext\n## A.2\ntext" produces exactly 2 chunks
	// with breadcrumbs "> A\n > A.1" and "> A\n > A.2".
	t.Parallel()

	content := `# A

## A.1
text

## A.2
text`

	chunker := NewMarkdownChunker(DefaultMarkdownChunkerConfig(), testLoggerChunk(t))
	got, err := chunker.Chunk(content, nil)
	if err != nil {
		t.Fatalf("Chunk() error = %v", err)
	}

	if len(got) != 2 {
		t.Errorf("got %d chunks, want exactly 2", len(got))
	}

	wantBreadcrumbs := []string{"> A\n > A.1", "> A\n > A.2"}
	for i, wantBC := range wantBreadcrumbs {
		if i >= len(got) {
			break
		}
		gotBC, ok := got[i].Metadata["breadcrumb"].(string)
		if !ok {
			t.Errorf("chunk[%d]: missing breadcrumb", i)
			continue
		}
		if gotBC != wantBC {
			t.Errorf("chunk[%d] breadcrumb = %q, want %q", i, gotBC, wantBC)
		}
		// Verify chunk_text starts with breadcrumb.
		if !strings.HasPrefix(got[i].Text, wantBC+"\n\n") {
			t.Errorf("chunk[%d] text does not start with breadcrumb prefix", i)
		}
	}

	// Verify no chunk contains only "# A" without body.
	for _, ch := range got {
		if strings.HasPrefix(ch.Text, "# A\n") || ch.Text == "# A" {
			t.Error("found a header-only chunk that should have been skipped")
		}
	}
}
