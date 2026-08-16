package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

const (
	// DefaultPageSize is the default number of items per page.
	DefaultPageSize = 20
	// MinPageSize is the minimum allowed page size.
	MinPageSize = 1
	// MaxPageSize is the maximum allowed page size.
	MaxPageSize = 200
)

// Cursor holds pagination state encoded as base64 JSON.
type Cursor struct {
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

// EncodeCursor creates a base64-encoded cursor string from offset and limit.
func EncodeCursor(offset, limit int) string {
	// json.Marshal on Cursor (two ints) never fails — it has no unexported fields,
	// no circular references, and no types that can produce marshal errors.
	data, _ := json.Marshal(Cursor{Offset: offset, Limit: limit}) //nolint:errcheck
	return base64.StdEncoding.EncodeToString(data)
}

// DecodeCursor parses a base64-encoded cursor into offset and limit.
// An empty cursor returns (0, DefaultPageSize, nil).
func DecodeCursor(cursorStr string) (int, int, error) {
	if cursorStr == "" {
		return 0, DefaultPageSize, nil
	}

	data, err := base64.StdEncoding.DecodeString(cursorStr)
	if err != nil {
		return 0, DefaultPageSize, fmt.Errorf("decode cursor: %w", err)
	}

	var c Cursor
	if err := json.Unmarshal(data, &c); err != nil {
		return 0, DefaultPageSize, fmt.Errorf("unmarshal cursor: %w", err)
	}

	limit := NormalizePageSize(c.Limit)
	return c.Offset, limit, nil
}

// NormalizePageSize clamps the page size to [MinPageSize, MaxPageSize].
func NormalizePageSize(size int) int {
	if size < MinPageSize || size > MaxPageSize {
		return DefaultPageSize
	}
	return size
}
