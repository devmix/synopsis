package parsers

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/logger"
)

// skipDirs lists directory names that should be excluded from recursive walking.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, ".idea": true,
	".vscode": true, "__pycache__": true, ".opencode": true,
}

// MarkdownParser recursively finds and reads .md files from a directory.
type MarkdownParser struct {
	log *logger.Logger
}

// NewMarkdownParser creates a parser with the given logger (required).
func NewMarkdownParser(log *logger.Logger) *MarkdownParser {
	return &MarkdownParser{log: log}
}

// SupportedExtensions returns the file extensions supported by this parser.
func (*MarkdownParser) SupportedExtensions() []string { return []string{".md"} }

// Parse walks the source path for markdown files and returns parsed documents.
func (p *MarkdownParser) Parse(sourcePath string) ingestion.ParseResult {
	var docs []ingestion.Document
	var errs []error

	err := filepath.WalkDir(sourcePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			return nil // continue walking despite errors
		}

		// Skip directories we don't want to traverse.
		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}

		// Only process .md files.
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
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

// parseFile reads a single markdown file and returns a Document.
func (p *MarkdownParser) parseFile(path, baseDir string) (ingestion.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ingestion.Document{}, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return ingestion.Document{}, err
	}

	relPath, _ := filepath.Rel(baseDir, path)

	meta := map[string]interface{}{
		"source_type": "markdown",
		"source_file": relPath,
		"file_size":   len(data),
		"modified_at": info.ModTime().Format(time.RFC3339),
	}

	return ingestion.Document{
		SourcePath: path,
		Content:    string(data),
		Metadata:   meta,
	}, nil
}
