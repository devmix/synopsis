package sources

import (
	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/ingestion/chunkers"
	"github.com/devmix/synopsis/internal/ingestion/parsers"
	"github.com/devmix/synopsis/internal/logger"
)

// JsonSource delegates parsing to JSONParser and chunking to JSONChunker.
type JsonSource struct {
	parser  *parsers.JSONParser
	chunker chunkers.Chunker
}

// NewJsonSource creates a JsonSource with the given json chunker and logger (required).
func NewJsonSource(jsonChunker chunkers.Chunker, log *logger.Logger) *JsonSource {
	return &JsonSource{
		parser:  parsers.NewJSONParser(log),
		chunker: jsonChunker,
	}
}

// Parse delegates to the embedded JSONParser.
func (s *JsonSource) Parse(sourcePath string) ingestion.ParseResult {
	return s.parser.Parse(sourcePath)
}

// SupportedExtensions delegates to the embedded JSONParser.
func (s *JsonSource) SupportedExtensions() []string { return s.parser.SupportedExtensions() }

// Chunk delegates to the configured json chunker.
func (s *JsonSource) Chunk(content string, metadata map[string]interface{}) ([]chunkers.DocumentChunk, error) {
	return s.chunker.Chunk(content, metadata)
}
