package sources

import (
	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/ingestion/chunkers"
	"github.com/devmix/synopsis/internal/ingestion/parsers"
	"github.com/devmix/synopsis/internal/logger"
)

// MarkdownSource delegates parsing to MarkdownParser and chunking to MarkdownChunker.
type MarkdownSource struct {
	parser  *parsers.MarkdownParser
	chunker chunkers.Chunker
}

// NewMarkdownSource creates a MarkdownSource with the given markdown chunker and logger (required).
func NewMarkdownSource(mdChunker chunkers.Chunker, log *logger.Logger) *MarkdownSource {
	return &MarkdownSource{
		parser:  parsers.NewMarkdownParser(log),
		chunker: mdChunker,
	}
}

// Parse delegates to the embedded MarkdownParser.
func (s *MarkdownSource) Parse(sourcePath string) ingestion.ParseResult {
	return s.parser.Parse(sourcePath)
}

// SupportedExtensions delegates to the embedded MarkdownParser.
func (s *MarkdownSource) SupportedExtensions() []string { return s.parser.SupportedExtensions() }

// Chunk delegates to the configured markdown chunker.
func (s *MarkdownSource) Chunk(content string, metadata map[string]interface{}) ([]chunkers.DocumentChunk, error) {
	return s.chunker.Chunk(content, metadata)
}
