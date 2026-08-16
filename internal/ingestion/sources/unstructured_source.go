package sources

import (
	"fmt"

	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/ingestion/chunkers"
	"github.com/devmix/synopsis/internal/ingestion/parsers"
	"github.com/devmix/synopsis/internal/logger"
)

// UnstructuredSource merges results from UnstructuredParser (.md files) and JSONParser (.json files).
// Chunk routing is determined by metadata["source_type"]: "unstructured" → MarkdownChunker,
// "json" → JSONChunker. Unknown source_type returns an error (fail loud).
type UnstructuredSource struct {
	mdParser    *parsers.UnstructuredParser
	jsonParser  *parsers.JSONParser
	mdChunker   chunkers.Chunker
	jsonChunker chunkers.Chunker
}

// NewUnstructuredSource creates an UnstructuredSource with the given chunkers and logger (required).
func NewUnstructuredSource(mdChunker, jsonChunker chunkers.Chunker, log *logger.Logger) *UnstructuredSource {
	return &UnstructuredSource{
		mdParser:    parsers.NewUnstructuredParser(log),
		jsonParser:  parsers.NewJSONParser(log),
		mdChunker:   mdChunker,
		jsonChunker: jsonChunker,
	}
}

// Parse merges documents from both UnstructuredParser (.md) and JSONParser (.json).
func (s *UnstructuredSource) Parse(sourcePath string) ingestion.ParseResult {
	mdResult := s.mdParser.Parse(sourcePath)
	jsonResult := s.jsonParser.Parse(sourcePath)

	docs := make([]ingestion.Document, 0, len(mdResult.Documents)+len(jsonResult.Documents))
	docs = append(docs, mdResult.Documents...)
	docs = append(docs, jsonResult.Documents...)

	errs := make([]error, 0, len(mdResult.Errors)+len(jsonResult.Errors))
	errs = append(errs, mdResult.Errors...)
	errs = append(errs, jsonResult.Errors...)

	return ingestion.ParseResult{Documents: docs, Errors: errs}
}

// SupportedExtensions returns the union of extensions from both parsers.
func (s *UnstructuredSource) SupportedExtensions() []string {
	exts := make([]string, 0, len(s.mdParser.SupportedExtensions())+len(s.jsonParser.SupportedExtensions()))
	exts = append(exts, s.mdParser.SupportedExtensions()...)
	exts = append(exts, s.jsonParser.SupportedExtensions()...)
	return exts
}

// Chunk routes to the appropriate chunker based on metadata["source_type"].
func (s *UnstructuredSource) Chunk(content string, metadata map[string]interface{}) ([]chunkers.DocumentChunk, error) {
	if metadata == nil {
		return nil, fmt.Errorf("metadata is required for unstructured source chunk routing")
	}

	sourceType, ok := metadata["source_type"].(string)
	if !ok || sourceType == "" {
		return nil, fmt.Errorf("metadata[\"source_type\"] must be a non-empty string for unstructured source chunk routing")
	}

	switch sourceType {
	case "unstructured":
		return s.mdChunker.Chunk(content, metadata)
	case "json":
		return s.jsonChunker.Chunk(content, metadata)
	default:
		return nil, fmt.Errorf("unknown source_type %q for unstructured chunk routing", sourceType)
	}
}
