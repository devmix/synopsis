// Package sources provides self-contained ingestion units that combine a parser
// and chunker for each supported source type.
package sources

import (
	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/ingestion/chunkers"
)

// Source is a self-sufficient ingestion unit for one source type, combining
// parsing (file discovery + document extraction) and chunking (content splitting).
type Source interface {
	ingestion.Parser // Parse(sourcePath string) ingestion.ParseResult
	chunkers.Chunker // DocumentChunk(content string, metadata map[string]interface{}) ([]DocumentChunk, error)
}
