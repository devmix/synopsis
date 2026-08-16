package sources

import (
	"testing"

	"github.com/devmix/synopsis/internal/ingestion/chunkers"
	"github.com/devmix/synopsis/internal/logger"
)

func testLoggerSrc(t *testing.T) *logger.Logger {
	t.Helper()
	l, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		t.Fatalf("create test logger: %v", err)
	}
	return l
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	log := testLoggerSrc(t)
	mdChunker := chunkers.NewMarkdownChunker(chunkers.DefaultMarkdownChunkerConfig(), log)
	jsonChunker := chunkers.NewJSONChunker(chunkers.DefaultJSONChunkerConfig(), log)

	tests := []struct {
		name       string
		sourceType string
		newSource  func() Source
	}{
		{
			name:       "markdown",
			sourceType: "markdown",
			newSource:  func() Source { return NewMarkdownSource(mdChunker, log) },
		},
		{
			name:       "json",
			sourceType: "json",
			newSource:  func() Source { return NewJsonSource(jsonChunker, log) },
		},
		{
			name:       "mediawiki",
			sourceType: "mediawiki",
			newSource:  func() Source { return NewMediawikiSource(mdChunker, log) },
		},
		{
			name:       "unstructured",
			sourceType: "unstructured",
			newSource:  func() Source { return NewUnstructuredSource(mdChunker, jsonChunker, log) },
		},
		{
			name:       "webpages",
			sourceType: "webpages",
			newSource:  func() Source { return NewWebpageSource(mdChunker, log) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewRegistry()

			if err := reg.Register(tt.sourceType, tt.newSource()); err != nil {
				t.Fatalf("Register(%q): %v", tt.sourceType, err)
			}

			src, ok := reg.Get(tt.sourceType)
			if !ok {
				t.Fatalf("GetNerResponse(%q) not found", tt.sourceType)
			}
			if src == nil {
				t.Fatalf("GetNerResponse(%q) returned nil source", tt.sourceType)
			}

			types := reg.Types()
			found := false
			for _, typ := range types {
				if typ == tt.sourceType {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("Types() missing %q", tt.sourceType)
			}
		})
	}

	t.Run("unknown type not found", func(t *testing.T) {
		reg := NewRegistry()
		_, ok := reg.Get("nonexistent")
		if ok {
			t.Fatal("GetNerResponse(nonexistent) should return false")
		}
	})

	t.Run("duplicate registration returns error", func(t *testing.T) {
		reg := NewRegistry()
		if err := reg.Register("markdown", NewMarkdownSource(mdChunker, log)); err != nil {
			t.Fatalf("first Register: %v", err)
		}
		err := reg.Register("markdown", NewMarkdownSource(mdChunker, log))
		if err == nil {
			t.Fatal("duplicate Register should return error")
		}
	})
}
