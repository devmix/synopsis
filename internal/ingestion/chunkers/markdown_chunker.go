package chunkers

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/devmix/synopsis/internal/logger"
)

// MarkdownChunkerConfig configures the markdown chunking strategy.
type MarkdownChunkerConfig struct {
	Strategy       string // "headers", "fixed", "hybrid"
	MaxChunkSize   int    // maximum characters per chunk (default 1000)
	OverlapSize    int    // overlap between adjacent chunks in chars (default 100)
	MinSectionSize int    // minimum section size before splitting (default 500)
}

// DefaultMarkdownChunkerConfig returns a config with recommended defaults.
func DefaultMarkdownChunkerConfig() MarkdownChunkerConfig {
	return MarkdownChunkerConfig{
		Strategy:       "headers",
		MaxChunkSize:   1000,
		OverlapSize:    100,
		MinSectionSize: 500,
	}
}

// MarkdownChunker splits markdown documents into chunks based on heading structure.
type MarkdownChunker struct {
	cfg MarkdownChunkerConfig
	log *logger.Logger
}

// NewMarkdownChunker creates a chunker with the given configuration and logger (required).
func NewMarkdownChunker(cfg MarkdownChunkerConfig, log *logger.Logger) *MarkdownChunker {
	if cfg.Strategy == "" {
		cfg = DefaultMarkdownChunkerConfig()
	}
	return &MarkdownChunker{cfg: cfg, log: log}
}

// headingRe matches markdown ATX headings (# Heading).
var headingRe = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)

// imageRe matches markdown image syntax ![alt](path).
var imageRe = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)

// cleanHeading strips markdown formatting markers from a heading text.
// Removes: leading # characters, **bold**, *italic*, `code` backticks.
// Preserves numbering like "1.1 ", "3.2.4 ".
func cleanHeading(text string) string {
	// Remove bold markers (**text**).
	text = strings.ReplaceAll(text, "**", "")
	// Remove italic markers (*text*).
	text = strings.ReplaceAll(text, "*", "")
	// Remove inline code backticks (`code`).
	text = strings.ReplaceAll(text, "`", "")
	return strings.TrimSpace(text)
}

// buildBreadcrumbs builds the full parent path from heading hierarchy including
// the current heading. For a heading at index i, it collects all ancestor headings
// and appends the current heading. Returns multi-line indented breadcrumb:
//
//	> A
//	 > B
//	  > C
func buildBreadcrumbs(headings []headingInfo, currentIdx int) string {
	if len(headings) == 0 || currentIdx < 0 {
		return ""
	}

	current := headings[currentIdx]
	var path []string

	// Walk backwards to find parent headings at each level.
	for j := currentIdx - 1; j >= 0; j-- {
		if headings[j].level < current.level {
			path = append([]string{cleanHeading(headings[j].text)}, path...)
			// If we found a level-1 heading, no need to go further.
			if headings[j].level == 1 {
				break
			}
			current = headings[j]
		}
	}

	// Append the current heading itself.
	path = append(path, cleanHeading(headings[currentIdx].text))

	var lines []string
	for i, p := range path {
		lines = append(lines, strings.Repeat(" ", i)+"> "+p)
	}
	return strings.Join(lines, "\n")
}

// isHeaderOnly checks if the section text consists only of a heading line with no body.
func isHeaderOnly(text string) bool {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) == 0 {
		return true
	}
	// First line should be a heading; check remaining lines for non-empty content.
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "" {
			return false
		}
	}
	return true
}

// sectionBody extracts the body text after the first line (heading) of a section.
func sectionBody(text string) string {
	lines := strings.SplitN(strings.TrimSpace(text), "\n", 2)
	if len(lines) < 2 {
		return ""
	}
	return strings.TrimSpace(lines[1])
}

// getFileNameFromMetadata extracts the file name from metadata["source_file"].
func getFileNameFromMetadata(metadata map[string]interface{}) string {
	if metadata == nil {
		return ""
	}
	sf, ok := metadata["source_file"].(string)
	if !ok || sf == "" {
		return ""
	}
	return filepath.Base(sf)
}

// DocumentChunk splits the markdown content into chunks according to the configured strategy.
func (m *MarkdownChunker) Chunk(content string, metadata map[string]interface{}) ([]DocumentChunk, error) {
	var chunks []DocumentChunk
	var err error

	switch m.cfg.Strategy {
	case "headers":
		chunks, err = m.chunkByHeaders(content, metadata)
	case "fixed":
		chunks = chunkFixed(content, metadata, m.cfg.MaxChunkSize, m.cfg.OverlapSize)
	case "hybrid":
		chunks, err = m.chunkHybrid(content, metadata)
	default:
		return nil, fmt.Errorf("unknown markdown chunking strategy %q", m.cfg.Strategy)
	}

	if err == nil {
		m.log.Debug("chunk complete", "strategy", m.cfg.Strategy, "max_chunk_size", m.cfg.MaxChunkSize, "overlap_size", m.cfg.OverlapSize, "chunks", len(chunks))
	}
	return chunks, err
}

// headingInfo holds parsed heading data.
type headingInfo struct {
	level int
	text  string
	pos   int // byte offset in content
}

// findHeadings extracts all ATX headings from markdown content with their positions.
func findHeadings(content string) []headingInfo {
	var result []headingInfo
	lines := strings.Split(content, "\n")
	offset := 0

	for _, line := range lines {
		matches := headingRe.FindStringSubmatch(line)
		if len(matches) >= 2 {
			// Count # characters for level.
			hashCount := 0
			for _, r := range line {
				if r == '#' {
					hashCount++
				} else {
					break
				}
			}
			text := strings.TrimSpace(matches[1])
			result = append(result, headingInfo{
				level: hashCount,
				text:  text,
				pos:   offset,
			})
		}
		offset += len(line) + 1 // +1 for newline
	}

	return result
}

// chunkByHeaders splits content at heading boundaries (h1-h6).
func (m *MarkdownChunker) chunkByHeaders(content string, metadata map[string]interface{}) ([]DocumentChunk, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil
	}

	headings := findHeadings(content)

	if len(headings) == 0 {
		// No headings — entire content is one section (preamble).
		imgs := imageRe.FindAllStringSubmatch(content, -1)
		meta := copyMap(metadata)
		if meta == nil {
			meta = make(map[string]interface{})
		}
		for k, v := range extractImageMetadata(imgs) {
			meta[k] = v
		}
		chunkText := strings.TrimSpace(content)
		fileName := getFileNameFromMetadata(metadata)
		if fileName != "" {
			chunkText = fileName + "\n\n" + chunkText
		}
		return []DocumentChunk{
			{
				Text:        chunkText,
				StartOffset: 0,
				EndOffset:   len(content),
				Metadata:    meta,
			},
		}, nil
	}

	chunks := make([]DocumentChunk, 0, len(headings))

	// Handle preamble (text before the first heading).
	if headings[0].pos > 0 {
		preambleText := strings.TrimSpace(content[:headings[0].pos])
		if preambleText != "" {
			meta := copyMap(metadata)
			if meta == nil {
				meta = make(map[string]interface{})
			}
			fileName := getFileNameFromMetadata(metadata)
			chunkText := preambleText
			if fileName != "" {
				chunkText = fileName + "\n\n" + chunkText
			}
			imgs := imageRe.FindAllStringSubmatch(preambleText, -1)
			if imgMeta := extractImageMetadata(imgs); len(imgMeta) > 0 {
				for k, v := range imgMeta {
					meta[k] = v
				}
			}
			chunks = append(chunks, DocumentChunk{
				Text:        chunkText,
				StartOffset: 0,
				EndOffset:   headings[0].pos,
				Metadata:    meta,
			})
		}
	}

	for i, h := range headings {
		endPos := len(content)
		if i+1 < len(headings) {
			endPos = headings[i+1].pos
		}

		text := content[h.pos:endPos]
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}

		// Skip header-only chunks (no body text).
		if isHeaderOnly(text) {
			continue
		}

		meta := copyMap(metadata)
		if meta == nil {
			meta = make(map[string]interface{})
		}
		meta["section_title"] = h.text
		meta["heading_level"] = h.level

		// Build breadcrumb from heading hierarchy.
		breadcrumb := buildBreadcrumbs(headings, i)
		if breadcrumb != "" {
			meta["breadcrumb"] = breadcrumb
		}

		// Extract images from this section body.
		body := sectionBody(text)
		imgs := imageRe.FindAllStringSubmatch(body, -1)
		if imgMeta := extractImageMetadata(imgs); len(imgMeta) > 0 {
			for k, v := range imgMeta {
				meta[k] = v
			}
		}

		chunkText := body
		if breadcrumb != "" {
			chunkText = breadcrumb + "\n\n" + chunkText
		}

		chunks = append(chunks, DocumentChunk{
			Text:        chunkText,
			SequenceNum: i,
			StartOffset: h.pos,
			EndOffset:   endPos,
			Metadata:    meta,
		})
	}

	return chunks, nil
}

// chunkHybrid first splits by headers, then applies fixed-size splitting to large sections.
func (m *MarkdownChunker) chunkHybrid(content string, metadata map[string]interface{}) ([]DocumentChunk, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil
	}

	headings := findHeadings(content)

	if len(headings) == 0 {
		// No headings — treat as preamble with file name prefix.
		meta := copyMap(metadata)
		if meta == nil {
			meta = make(map[string]interface{})
		}
		fileName := getFileNameFromMetadata(metadata)
		chunkText := strings.TrimSpace(content)
		if fileName != "" {
			chunkText = fileName + "\n\n" + chunkText
		}
		fixedChunks := chunkFixed(chunkText, meta, m.cfg.MaxChunkSize, m.cfg.OverlapSize)
		return fixedChunks, nil
	}

	var chunks []DocumentChunk

	// Handle preamble (text before the first heading).
	if headings[0].pos > 0 {
		preambleText := strings.TrimSpace(content[:headings[0].pos])
		if preambleText != "" {
			meta := copyMap(metadata)
			if meta == nil {
				meta = make(map[string]interface{})
			}
			fileName := getFileNameFromMetadata(metadata)
			chunkText := preambleText
			if fileName != "" {
				chunkText = fileName + "\n\n" + chunkText
			}
			subChunks := chunkFixed(chunkText, meta, m.cfg.MaxChunkSize, m.cfg.OverlapSize)
			for _, sc := range subChunks {
				sc.DocID = 0
				chunks = append(chunks, sc)
			}
		}
	}

	for i, h := range headings {
		endPos := len(content)
		if i+1 < len(headings) {
			endPos = headings[i+1].pos
		}

		text := strings.TrimSpace(content[h.pos:endPos])
		if text == "" {
			continue
		}

		// Skip header-only chunks (no body text).
		if isHeaderOnly(text) {
			continue
		}

		meta := copyMap(metadata)
		if meta == nil {
			meta = make(map[string]interface{})
		}
		meta["section_title"] = h.text
		meta["heading_level"] = h.level

		// Build breadcrumb from heading hierarchy.
		breadcrumb := buildBreadcrumbs(headings, i)
		if breadcrumb != "" {
			meta["breadcrumb"] = breadcrumb
		}

		body := sectionBody(text)

		imgs := imageRe.FindAllStringSubmatch(body, -1)
		if imgMeta := extractImageMetadata(imgs); len(imgMeta) > 0 {
			for k, v := range imgMeta {
				meta[k] = v
			}
		}

		chunkText := body
		if breadcrumb != "" {
			chunkText = breadcrumb + "\n\n" + chunkText
		}

		var subChunks []DocumentChunk
		if len(chunkText) > m.cfg.MaxChunkSize && m.cfg.MinSectionSize > 0 {
			subChunks = chunkFixed(chunkText, meta, m.cfg.MaxChunkSize, m.cfg.OverlapSize)
		} else {
			subChunks = []DocumentChunk{
				{
					Text:        chunkText,
					SequenceNum: i,
					StartOffset: h.pos,
					EndOffset:   endPos,
					Metadata:    meta,
				},
			}
		}

		for _, sc := range subChunks {
			sc.DocID = 0
			chunks = append(chunks, sc)
		}
	}

	return chunks, nil
}

// extractImageMetadata returns metadata map with image paths if found.
func extractImageMetadata(matches [][]string) map[string]interface{} {
	meta := make(map[string]interface{})
	if len(matches) == 0 {
		return meta
	}
	var paths []string
	for _, m := range matches {
		if len(m) >= 3 {
			paths = append(paths, m[2])
		}
	}
	meta["image_paths"] = paths
	return meta
}

// chunkFixed splits text into fixed-size chunks with overlap.
func chunkFixed(content string, metadata map[string]interface{}, maxSize, overlap int) []DocumentChunk {
	if len(content) <= maxSize {
		meta := copyMap(metadata)
		if meta == nil {
			meta = make(map[string]interface{})
		}
		return []DocumentChunk{
			{
				Text:        content,
				StartOffset: 0,
				EndOffset:   len(content),
				Metadata:    meta,
			},
		}
	}

	var chunks []DocumentChunk
	step := maxSize - overlap
	if step <= 0 {
		step = maxSize // no overlap if overlap >= max
	}

	for start := 0; start < len(content); start += step {
		end := start + maxSize
		if end > len(content) {
			end = len(content)
		}
		meta := copyMap(metadata)
		if meta == nil {
			meta = make(map[string]interface{})
		}
		chunks = append(chunks, DocumentChunk{
			Text:        content[start:end],
			StartOffset: start,
			EndOffset:   end,
			Metadata:    meta,
		})
		if end >= len(content) {
			break
		}
	}

	return chunks
}

// copyMap returns a shallow copy of the map.
func copyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	cp := make(map[string]interface{}, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
