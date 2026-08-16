// Package parsers provides document source parsers for different data formats.
package parsers

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/logger"
)

// WebpageParser parses webpage dataset directories.
// Expected structure:
//
//	sourcePath/
//	  pages/
//	    [page-1].md, [page-2].md, ...   (preferred)
//	    [page-1].html, [page-2].html, ... (fallback, converted to Markdown)
//	  static/                            (skipped)
type WebpageParser struct {
	log *logger.Logger
}

// NewWebpageParser creates a parser with the given logger (required).
func NewWebpageParser(log *logger.Logger) *WebpageParser {
	return &WebpageParser{log: log}
}

// SupportedExtensions returns the file extensions supported by this parser.
func (*WebpageParser) SupportedExtensions() []string { return []string{".md", ".html"} }

// Parse walks the webpage source path recursively, collects *.md and *.html files,
// groups them by page name (basename without extension), prefers .md over .html,
// converts .html to Markdown, and returns parsed documents.
func (p *WebpageParser) Parse(sourcePath string) ingestion.ParseResult {
	var docs []ingestion.Document
	var errs []error

	// Collect all candidate files grouped by page name within each directory.
	pages := p.collectPages(sourcePath)

	// Sort keys for deterministic output order.
	keys := make([]string, 0, len(pages))
	for k := range pages {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	conv := converter.NewConverter(
		converter.WithPlugins(base.NewBasePlugin(), commonmark.NewCommonmarkPlugin()),
	)

	for _, key := range keys {
		info := pages[key]
		doc, err := p.parsePage(sourcePath, info, conv)
		if err != nil {
			errs = append(errs, err)
			p.log.Warn("parse error", logger.Err(err))
			continue
		}
		docs = append(docs, doc)
	}

	p.log.Debug("parse complete", "source_path", sourcePath, "files_found", len(docs), "errors", len(errs))

	return ingestion.ParseResult{Documents: docs, Errors: errs}
}

// pageFiles holds collected files for a single directory.
type pageFiles struct {
	md   string // path to .md file if present
	html string // path to .html file if present
	dir  string // directory containing the files
}

// collectPages walks the source tree and groups *.md/*.html files by directory,
// preferring .md over .html for the same page name.
func (p *WebpageParser) collectPages(sourcePath string) map[string]pageFiles {
	pages := make(map[string]pageFiles)

	err := filepath.WalkDir(sourcePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}

		if d.IsDir() && (skipDirs[d.Name()] || d.Name() == "static") {
			return filepath.SkipDir
		}

		if d.IsDir() {
			return nil
		}

		name := strings.ToLower(d.Name())
		ext := filepath.Ext(name)

		if ext != ".md" && ext != ".html" {
			return nil
		}

		dir := filepath.Dir(path)
		pageName := strings.TrimSuffix(filepath.Base(name), ext)
		key := dir + "/" + pageName

		info, ok := pages[key]
		if !ok {
			info = pageFiles{dir: dir}
		}

		if ext == ".md" {
			info.md = path
		} else if ext == ".html" {
			info.html = path
		}

		pages[key] = info
		return nil
	})
	_ = err // walk errors are non-fatal; caller sees partial results

	return pages
}

// parsePage reads content for a single page, preferring .md over .html,
// and converts .html to Markdown if needed.
func (p *WebpageParser) parsePage(baseDir string, info pageFiles, conv *converter.Converter) (ingestion.Document, error) {
	var filePath string
	var content []byte

	if info.md != "" {
		filePath = info.md
		var err error
		content, err = os.ReadFile(filePath)
		if err != nil {
			return ingestion.Document{}, err
		}
	} else if info.html != "" {
		filePath = info.html
		htmlData, readErr := os.ReadFile(filePath)
		if readErr != nil {
			return ingestion.Document{}, readErr
		}
		mdContent, convertErr := conv.ConvertString(string(htmlData))
		if convertErr != nil {
			return ingestion.Document{}, convertErr
		}
		content = []byte(mdContent)
	} else {
		return ingestion.Document{}, nil // should not happen
	}

	relPath, _ := filepath.Rel(baseDir, filePath)

	meta := map[string]interface{}{
		"source_type": "webpages",
		"source_file": relPath,
		"file_size":   len(content),
	}

	return ingestion.Document{
		SourcePath: filePath,
		Content:    string(content),
		Metadata:   meta,
	}, nil
}

// isImageExt returns true if the extension is a common image format.
func isImageExt(ext string) bool {
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg", ".bmp":
		return true
	default:
		return false
	}
}
