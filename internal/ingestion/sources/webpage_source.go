package sources

import (
	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/ingestion/chunkers"
	"github.com/devmix/synopsis/internal/ingestion/parsers"
	"github.com/devmix/synopsis/internal/logger"
)

// WebpageSource delegates parsing to WebpageParser and chunking to MarkdownChunker.
type WebpageSource struct {
	parser  *parsers.WebpageParser
	chunker chunkers.Chunker
}

// NewWebpageSource creates a WebpageSource with the given markdown chunker and logger (required).
func NewWebpageSource(mdChunker chunkers.Chunker, log *logger.Logger) *WebpageSource {
	return &WebpageSource{
		parser:  parsers.NewWebpageParser(log),
		chunker: mdChunker,
	}
}

// Parse delegates to the embedded WebpageParser.
func (s *WebpageSource) Parse(sourcePath string) ingestion.ParseResult {
	return s.parser.Parse(sourcePath)
}

// SupportedExtensions delegates to the embedded WebpageParser.
func (s *WebpageSource) SupportedExtensions() []string { return s.parser.SupportedExtensions() }

// Chunk delegates to the configured markdown chunker.
func (s *WebpageSource) Chunk(content string, metadata map[string]interface{}) ([]chunkers.DocumentChunk, error) {
	return s.chunker.Chunk(content, metadata)
}
