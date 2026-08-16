package sources

import (
	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/ingestion/chunkers"
	"github.com/devmix/synopsis/internal/ingestion/parsers"
	"github.com/devmix/synopsis/internal/logger"
)

// MediawikiSource delegates parsing to MediawikiParser and chunking to MarkdownChunker.
// Using MarkdownChunker for wikitext is a graceful degradation: wikitext headings
// partially overlap with markdown ATX syntax, so the chunker produces reasonable
// sections when headings are present, or falls back to fixed-size splitting in hybrid mode.
type MediawikiSource struct {
	parser  *parsers.MediawikiParser
	chunker chunkers.Chunker
}

// NewMediawikiSource creates a MediawikiSource with the given markdown chunker and logger (required).
func NewMediawikiSource(mdChunker chunkers.Chunker, log *logger.Logger) *MediawikiSource {
	return &MediawikiSource{
		parser:  parsers.NewMediawikiParser(log),
		chunker: mdChunker,
	}
}

// Parse delegates to the embedded MediawikiParser.
func (s *MediawikiSource) Parse(sourcePath string) ingestion.ParseResult {
	return s.parser.Parse(sourcePath)
}

// SupportedExtensions delegates to the embedded MediawikiParser.
func (s *MediawikiSource) SupportedExtensions() []string { return s.parser.SupportedExtensions() }

// Chunk delegates to the configured markdown chunker for graceful wikitext degradation.
func (s *MediawikiSource) Chunk(content string, metadata map[string]interface{}) ([]chunkers.DocumentChunk, error) {
	return s.chunker.Chunk(content, metadata)
}
