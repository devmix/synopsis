package parsers

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/devmix/synopsis/internal/ingestion"
	"github.com/devmix/synopsis/internal/logger"
)

// sectionHeadingRe matches markdown ATX headings for section grouping.
var sectionHeadingRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)

// UnstructuredParser recursively finds .md files and groups content by sections.
// It also associates images found alongside markdown files with their parent document.
type UnstructuredParser struct {
	log *logger.Logger
}

// NewUnstructuredParser creates a parser with the given logger (required).
func NewUnstructuredParser(log *logger.Logger) *UnstructuredParser {
	return &UnstructuredParser{log: log}
}

// SupportedExtensions returns the file extensions supported by this parser.
func (*UnstructuredParser) SupportedExtensions() []string { return []string{".md"} }

// Parse walks the source path for markdown files, reads them, and returns documents
// grouped by section headings with associated image paths.
func (p *UnstructuredParser) Parse(sourcePath string) ingestion.ParseResult {
	var docs []ingestion.Document
	var errs []error

	err := filepath.WalkDir(sourcePath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err)
			return nil // continue walking despite errors
		}

		if d.IsDir() && skipDirs[d.Name()] {
			return filepath.SkipDir
		}

		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			doc, parseErr := p.parseMarkdownFile(path, sourcePath)
			if parseErr != nil {
				errs = append(errs, parseErr)
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

// parseMarkdownFile reads a markdown file, groups content by sections, and associates images.
func (p *UnstructuredParser) parseMarkdownFile(path, baseDir string) (ingestion.Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ingestion.Document{}, err
	}

	info, err := os.Stat(path)
	if err != nil {
		return ingestion.Document{}, err
	}

	relPath, _ := filepath.Rel(baseDir, path)

	// Collect image paths from the same directory.
	imagePaths := p.collectImages(filepath.Dir(path))

	meta := map[string]interface{}{
		"source_type": "unstructured",
		"source_file": relPath,
		"file_size":   len(data),
		"modified_at": info.ModTime().Format(time.RFC3339),
		"image_paths": imagePaths,
	}

	return ingestion.Document{
		SourcePath: path,
		Content:    string(data),
		Metadata:   meta,
	}, nil
}

// collectImages finds image files in the same directory as a markdown file.
func (p *UnstructuredParser) collectImages(dir string) []string {
	var images []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(entry.Name()))
		if isImageExt(ext) {
			images = append(images, filepath.Base(entry.Name()))
		}
	}

	return images
}

// GroupSections splits a markdown document's content into sections based on headings.
// This can be called by downstream consumers to further split documents.
func (p *UnstructuredParser) GroupSections(content string) []Section {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}

	headings := findHeadingsForGrouping(content)

	if len(headings) == 0 {
		return []Section{
			{Title: "untitled", Content: content, StartOffset: 0, EndOffset: len(content)},
		}
	}

	var sections []Section
	for i, h := range headings {
		endPos := len(content)
		if i+1 < len(headings) {
			endPos = headings[i+1].pos
		}

		text := strings.TrimSpace(content[h.pos:endPos])
		if text == "" {
			continue
		}

		sections = append(sections, Section{
			Title:       h.text,
			Content:     text,
			StartOffset: h.pos,
			EndOffset:   endPos,
		})
	}

	return sections
}

// Section represents a section of a markdown document delimited by headings.
type Section struct {
	Title       string
	Content     string
	StartOffset int
	EndOffset   int
}

// headingInfoForGrouping holds parsed heading data for section grouping.
type headingInfoForGrouping struct {
	level int
	text  string
	pos   int // byte offset in content
}

// findHeadingsForGrouping extracts ATX headings with positions for section grouping.
func findHeadingsForGrouping(content string) []headingInfoForGrouping {
	var result []headingInfoForGrouping
	lines := strings.Split(content, "\n")
	offset := 0

	for _, line := range lines {
		matches := sectionHeadingRe.FindStringSubmatch(line)
		if len(matches) >= 2 {
			hashCount := 0
			for _, r := range line {
				if r == '#' {
					hashCount++
				} else {
					break
				}
			}
			text := strings.TrimSpace(matches[1])
			result = append(result, headingInfoForGrouping{
				level: hashCount,
				text:  text,
				pos:   offset,
			})
		}
		offset += len(line) + 1 // +1 for newline
	}

	return result
}
