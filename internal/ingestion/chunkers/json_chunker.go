package chunkers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/devmix/synopsis/internal/logger"
)

// JSONChunkerConfig configures the JSON chunking strategy.
type JSONChunkerConfig struct {
	TextFields    []string // fields to extract text from; empty means all string fields
	CombineFields bool     // if true, merge all text fields into a single chunk per object
	MaxObjects    int      // maximum number of objects to process (0 = unlimited)
}

// DefaultJSONChunkerConfig returns recommended defaults.
func DefaultJSONChunkerConfig() JSONChunkerConfig {
	return JSONChunkerConfig{
		TextFields:    []string{"description", "title", "wikitext", "html"},
		CombineFields: true,
		MaxObjects:    0,
	}
}

// JSONChunker splits JSON content into chunks by extracting text from objects.
type JSONChunker struct {
	cfg JSONChunkerConfig
	log *logger.Logger
}

// NewJSONChunker creates a chunker with the given configuration and logger (required).
func NewJSONChunker(cfg JSONChunkerConfig, log *logger.Logger) *JSONChunker {
	if len(cfg.TextFields) == 0 && !cfg.CombineFields {
		def := DefaultJSONChunkerConfig()
		cfg.TextFields = def.TextFields
	}
	return &JSONChunker{cfg: cfg, log: log}
}

// DocumentChunk parses JSON content and produces chunks. Supports both arrays of objects
// and single objects. For arrays, each object becomes one or more chunks depending on config.
func (j *JSONChunker) Chunk(content string, metadata map[string]interface{}) ([]DocumentChunk, error) {
	// Try parsing as array first.
	var arr []json.RawMessage
	if unmarshalErr := json.Unmarshal([]byte(content), &arr); unmarshalErr == nil {
		chunks, err := j.chunkArray(arr, metadata)
		if err == nil {
			j.log.Debug("chunk complete", "combine_fields", j.cfg.CombineFields, "max_objects", j.cfg.MaxObjects, "chunks", len(chunks))
		}
		return chunks, err
	}

	// Fall back to single object.
	var obj map[string]interface{}
	if parseErr := json.Unmarshal([]byte(content), &obj); parseErr != nil {
		return nil, fmt.Errorf("parse JSON: %w", parseErr)
	}
	chunks, err := j.chunkObject(obj, 0, metadata)
	if err == nil {
		j.log.Debug("chunk complete", "combine_fields", j.cfg.CombineFields, "max_objects", j.cfg.MaxObjects, "chunks", len(chunks))
	}
	return chunks, err
}

// chunkArray processes a JSON array of objects.
func (j *JSONChunker) chunkArray(arr []json.RawMessage, metadata map[string]interface{}) ([]DocumentChunk, error) {
	limit := j.cfg.MaxObjects
	if limit > 0 && len(arr) > limit {
		arr = arr[:limit]
	}

	var chunks []DocumentChunk
	for i, raw := range arr {
		var obj map[string]interface{}
		if err := json.Unmarshal(raw, &obj); err != nil {
			continue // skip unparseable objects
		}
		objChunks, err := j.chunkObject(obj, i, metadata)
		if err != nil {
			continue
		}
		chunks = append(chunks, objChunks...)
	}

	return chunks, nil
}

// chunkObject extracts text fields from a single JSON object and produces chunks.
func (j *JSONChunker) chunkObject(obj map[string]interface{}, index int, metadata map[string]interface{}) ([]DocumentChunk, error) {
	if j.cfg.CombineFields {
		return j.combineFields(obj, index, metadata)
	}
	return j.perFieldChunks(obj, index, metadata)
}

// combineFields merges all text fields into a single chunk per object.
func (j *JSONChunker) combineFields(obj map[string]interface{}, index int, metadata map[string]interface{}) ([]DocumentChunk, error) {
	var parts []string
	for _, field := range j.cfg.TextFields {
		if val, ok := obj[field]; ok {
			if s, ok := val.(string); ok && s != "" {
				parts = append(parts, fmt.Sprintf("**%s**: %s", field, s))
			}
		}
	}

	if len(parts) == 0 {
		// Fallback: use all string fields.
		for k, v := range obj {
			if s, ok := v.(string); ok && s != "" {
				parts = append(parts, fmt.Sprintf("**%s**: %s", k, s))
			}
		}
	}

	if len(parts) == 0 {
		return nil, nil // no text content in this object
	}

	meta := copyMap(metadata)
	if meta == nil {
		meta = make(map[string]interface{})
	}
	meta["object_index"] = index

	return []DocumentChunk{
		{
			Text:        joinStrings(parts, "\n\n"),
			SequenceNum: index,
			Metadata:    meta,
		},
	}, nil
}

// perFieldChunks creates a separate chunk for each text field.
func (j *JSONChunker) perFieldChunks(obj map[string]interface{}, index int, metadata map[string]interface{}) ([]DocumentChunk, error) {
	var chunks []DocumentChunk
	fieldIndex := 0

	for _, field := range j.cfg.TextFields {
		if val, ok := obj[field]; ok {
			if s, ok := val.(string); ok && s != "" {
				meta := copyMap(metadata)
				if meta == nil {
					meta = make(map[string]interface{})
				}
				meta["object_index"] = index
				meta["field_name"] = field
				chunks = append(chunks, DocumentChunk{
					Text:        s,
					SequenceNum: index,
					Metadata:    meta,
				})
				fieldIndex++
			}
		}
	}

	return chunks, nil
}

// joinStrings joins a slice of strings with the given separator using strings.Builder.
func joinStrings(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}

	var sb strings.Builder
	for i, p := range parts {
		if i > 0 {
			sb.WriteString(sep)
		}
		sb.WriteString(p)
	}
	return sb.String()
}
