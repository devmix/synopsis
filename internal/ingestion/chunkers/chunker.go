// Package ingestion provides document parsing, chunking, entity extraction,
// and indexing for the RAG synopsis pipeline.
package chunkers

import (
	"github.com/devmix/synopsis/internal/ingestion/ner"
)

type DocumentChunk struct {
	DocID       int                    // reference to parent document in DB
	Text        string                 // chunk content
	SequenceNum int                    // order within document
	StartOffset int                    // byte offset of start in original text
	EndOffset   int                    // byte offset of end in original text
	Metadata    map[string]interface{} // source_file, image_paths, section_title, etc.
	NerResult   *ner.Result
}

type Chunker interface {
	Chunk(content string, metadata map[string]interface{}) ([]DocumentChunk, error)
}
