// Package utils provides shared utility functions.
package utils

import "strings"

// EscapeLike escapes %, _ and \ characters in user input for use with SQL LIKE ... ESCAPE '\'.
func EscapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
