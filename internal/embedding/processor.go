package embedding

import (
	"strings"
	"unicode"
)

const defaultMaxTokens = 8192

// Preprocess normalizes a single text for embedding:
// - Unicode normalization (NFC via unicode.Is)
// - Collapse whitespace runs to single spaces
// - Trim leading/trailing whitespace
// - Truncate if the rune count exceeds maxRuneLimit.
func Preprocess(text string, maxRuneLimit int) string {
	if text == "" {
		return text
	}

	// Normalize Unicode by filtering control characters and collapsing whitespace.
	var b strings.Builder
	b.Grow(len(text))
	prevSpace := false
	for _, r := range text {
		if unicode.IsControl(r) && !unicode.IsSpace(r) {
			continue // drop non-whitespace control chars
		}
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteRune(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}

	result := b.String()
	result = strings.TrimSpace(result)

	// Truncate to max rune limit if needed.
	if len([]rune(result)) > maxRuneLimit {
		runes := []rune(result)[:maxRuneLimit]
		result = string(runes)
	}

	return result
}

// PreprocessBatch applies Preprocess to every text in the slice.
func PreprocessBatch(texts []string, maxRuneLimit int) []string {
	if maxRuneLimit <= 0 {
		maxRuneLimit = defaultMaxTokens
	}
	result := make([]string, len(texts))
	for i, t := range texts {
		result[i] = Preprocess(t, maxRuneLimit)
	}
	return result
}

// SplitBatches divides a slice into chunks of at most batchSize elements.
func SplitBatches[T any](slice []T, batchSize int) [][]T {
	if batchSize <= 0 {
		batchSize = 1
	}
	var batches [][]T
	for len(slice) > 0 {
		size := batchSize
		if size > len(slice) {
			size = len(slice)
		}
		batch := slice[:size]
		slice = slice[size:]
		batches = append(batches, batch)
	}
	return batches
}
