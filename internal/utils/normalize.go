package utils

import "strings"

// Normalize prepares arbitrary text for matching: trims surrounding
// whitespace, lowercases, and collapses internal whitespace runs.
func Normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}
