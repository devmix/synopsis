package parsers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/logger"
)

// JSONParser recursively finds and reads .json files from a directory.
type JSONParser struct {
	log *logger.Logger
}

// NewJSONParser creates a parser with the given logger (required).
func NewJSONParser(log *logger.Logger) *JSONParser {
	return &JSONParser{log: log}
}

// SupportedExtensions returns the file extensions supported by this parser.
func (*JSONParser) SupportedExtensions() []string { return []string{".json"} }

// Parse walks the source path for JSON files and returns parsed documents.
func (p *JSONParser) Parse(sourcePath string) ingestion.ParseResult {
	var docs []ingestion.Document
	var errs []error

	err := filepath.WalkDir(sourcePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			return nil
		}

		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}

		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			doc, err := p.parseFile(path, sourcePath)
			if err != nil {
				errs = append(errs, err)
				return nil
			}
			docs = append(docs, doc)
		}

		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}

	p.log.Debug("parse complete", "source_path", sourcePath, "files_found", len(docs), "errors", len(errs))
	for _, e := range errs {
		p.log.Warn("parse error", logger.Err(e))
	}

	return ingestion.ParseResult{Documents: docs, Errors: errs}
}

// parseFile reads a single JSON file and returns a Document.
func (p *JSONParser) parseFile(path, baseDir string) (ingestion.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ingestion.Document{}, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return ingestion.Document{}, err
	}

	// Validate JSON and detect structure type.
	structureType := p.detectStructure(data)

	relPath, _ := filepath.Rel(baseDir, path)

	meta := map[string]interface{}{
		"source_type": "json",
		"source_file": relPath,
		"file_size":   len(data),
		"modified_at": info.ModTime().Format(time.RFC3339),
		"structure":   structureType,
	}

	return ingestion.Document{
		SourcePath: path,
		Content:    string(data),
		Metadata:   meta,
	}, nil
}

// detectStructure returns the top-level JSON type: "array", "object", or "unknown".
func (p *JSONParser) detectStructure(data []byte) string {
	var raw json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return "unknown"
	}

	trimmed := strings.TrimSpace(string(raw))
	if len(trimmed) == 0 {
		return "unknown"
	}

	switch trimmed[0] {
	case '[':
		return "array"
	case '{':
		return "object"
	default:
		return "unknown"
	}
}
