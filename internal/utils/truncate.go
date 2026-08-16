// Package utils provides shared utility functions.
package utils

const ellipsis = "..."

// Truncate shortens s to at most max runes, appending "..." if truncated.
// If max <= 0 it returns an empty string. It is rune-safe and never splits
// a multi-byte UTF-8 character.
func Truncate(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + ellipsis
}
