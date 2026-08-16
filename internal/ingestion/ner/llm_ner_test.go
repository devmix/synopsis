package ner

import (
	"strings"
	"testing"
)

func TestTruncateDescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		maxLen  int    // expected max length of output; 0 means no truncation needed
		wantEnd string // suffix that must appear at end (e.g. "...") or empty for exact match
		notWant bool   // if true, the result should NOT contain wantEnd
	}{
		{
			name:   "short_description_unchanged",
			input:  "Alice is a software engineer.",
			maxLen: 30,
		},
		{
			name:   "empty_description",
			input:  "",
			maxLen: 0,
		},
		{
			name:   "whitespace_only",
			input:  "   ",
			maxLen: 0,
		},
		{
			name:    "long_description_truncated_at_sentence",
			input:   "This is the first sentence. This is the second sentence that goes on and on. This is the third sentence with more content to make it longer than five hundred characters so we can test truncation properly at a sentence boundary.",
			maxLen:  500,
			wantEnd: ".",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateDescription(tt.input)

			runes := []rune(result)
			if len(runes) > 500 {
				t.Errorf("truncateDescription() returned %d runes, max is 500", len(runes))
			}

			if tt.wantEnd != "" && !tt.notWant {
				if !hasSuffix(result, tt.wantEnd) {
					t.Errorf("result should end with %q: %q", tt.wantEnd, result)
				}
			}

			// Verify no trailing whitespace.
			if len(result) > 0 && (result[len(result)-1] == ' ' || result[len(result)-1] == '\n') {
				t.Errorf("result has trailing whitespace: %q", result)
			}
		})
	}
}

func TestTruncateDescription_Exactly500(t *testing.T) {
	t.Parallel()

	// Create a description exactly 500 runes.
	desc := strings.Repeat("a", 499) + "b"
	result := truncateDescription(desc)
	if len([]rune(result)) != 500 {
		t.Errorf("result length = %d, want 500", len([]rune(result)))
	}

	// Create a description 501 runes with no sentence boundary.
	desc2 := strings.Repeat("a", 500) + "b"
	result2 := truncateDescription(desc2)
	if len([]rune(result2)) != 500 {
		t.Errorf("result length = %d, want 500 (hard cap)", len([]rune(result2)))
	}
}

func TestTruncateDescription_SentenceBoundary(t *testing.T) {
	t.Parallel()

	// Description with sentence boundary before limit.
	desc := strings.Repeat("word ", 10) + ". " + strings.Repeat("word ", 100) + "."
	result := truncateDescription(desc)

	// Should cut at the first period, not go to 500 chars.
	if len([]rune(result)) >= 500 {
		t.Errorf("expected truncation at sentence boundary, got %d runes", len([]rune(result)))
	}
	if !hasSuffix(result, ".") {
		t.Errorf("result should end with period: %q", result)
	}
}

func TestTruncateDescription_HardCap_NoSentenceBoundary(t *testing.T) {
	t.Parallel()

	// Text longer than 500 chars with no sentence-ending punctuation.
	desc := strings.Repeat("x", 600)
	result := truncateDescription(desc)

	if len([]rune(result)) != 500 {
		t.Errorf("result length = %d, want 500 (hard cap)", len([]rune(result)))
	}
}

func TestValidateEntityMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    map[string]interface{}
		wantKeys []string // keys expected in output; empty means nil output
		notWant  []string // keys that should NOT be in output
	}{
		{
			name:     "nil_input_returns_nil",
			input:    nil,
			wantKeys: nil,
		},
		{
			name:     "empty_map_returns_nil",
			input:    map[string]interface{}{},
			wantKeys: nil,
		},
		{
			name: "clean_metadata_passes_through",
			input: map[string]interface{}{
				"provider": "internal",
				"score":    0.95,
			},
			wantKeys: []string{"provider", "score"},
		},
		{
			name: "drops_uncertainty_comment_implied",
			input: map[string]interface{}{
				"status":   "implied by context",
				"provider": "internal",
			},
			wantKeys: []string{"provider"},
			notWant:  []string{"status"},
		},
		{
			name: "drops_uncertainty_comment_not_explicitly",
			input: map[string]interface{}{
				"version":  "not explicitly stated",
				"provider": "internal",
			},
			wantKeys: []string{"provider"},
			notWant:  []string{"version"},
		},
		{
			name: "keeps_non_string_values",
			input: map[string]interface{}{
				"count": 42,
				"flag":  true,
			},
			wantKeys: []string{"count", "flag"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateEntityMetadata(tt.input)

			if len(tt.wantKeys) == 0 && result != nil {
				t.Errorf("expected nil result, got %v", result)
				return
			}
			if len(tt.wantKeys) > 0 && result == nil {
				t.Errorf("expected non-nil result with keys %v, got nil", tt.wantKeys)
				return
			}

			for _, k := range tt.wantKeys {
				if _, ok := result[k]; !ok {
					t.Errorf("missing expected key %q in result: %v", k, result)
				}
			}
			for _, k := range tt.notWant {
				if _, ok := result[k]; ok {
					t.Errorf("should not contain dropped key %q in result: %v", k, result)
				}
			}
		})
	}
}

func TestValidateEntityMetadata_VersionField(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		want    bool // true if version should be kept
	}{
		// Valid versions: stdlib regexp ^[vV]?[0-9]+([._-][a-zA-Z0-9]+)*$ matches these.
		{"version_1", "1", true},
		{"version_2.3", "2.3", true},
		{"version_v1", "v1", true},
		{"version_V2", "V2", true},
		{"version_10", "10", true},
		{"version_1.0", "1.0", true},
		{"version_v2.3.1", "v2.3.1", true},
		{"version_1_beta", "1-beta", true},
		// These are rejected by rejectPatterns:
		{"reject_0-2_years", "0-2 years", false},
		{"reject_approximately", "approximately 3", false},
		{"reject_about", "about 2.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := map[string]interface{}{
				"version":  tt.version,
				"provider": "internal",
			}
			result := validateEntityMetadata(input)

			if tt.want {
				if _, ok := result["version"]; !ok {
					t.Errorf("version %q should be kept, but was dropped: %v", tt.version, result)
				}
			} else {
				if _, ok := result["version"]; ok {
					t.Errorf("version %q should be dropped, but was kept: %v", tt.version, result)
				}
			}
			// Provider should always survive.
			if _, ok := result["provider"]; !ok {
				t.Error("provider key should always be present")
			}
		})
	}
}

func TestIsValidVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bool
	}{
		// Valid versions: stdlib regexp ^[vV]?[0-9]+([._-][a-zA-Z0-9]+)*$ matches these.
		{"1", true},
		{"2.3", true},
		{"v1", true},
		{"V2", true},
		{"10", true},
		{"1.0", true},
		{"v2.3.1", true},
		{"1-beta", true},
		{"2_5", true},
		// Invalid: empty string.
		{"", false},
		// These are rejected by rejectPatterns:
		{"0-2 years", false},
		{"approximately 3", false},
		{"about 2.0", false},
		{"estimated version", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isValidVersion(tt.input)
			if got != tt.want {
				t.Errorf("isValidVersion(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseLLMResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		raw             string
		wantEntities    int
		wantFacts       int
		descTruncated   bool // if true, check that description was truncated
		metadataCleaned bool // if true, check that metadata was cleaned
	}{
		{
			name: "valid_response",
			raw: `{
				"entities": [
					{"name": "Alice", "type": "PERSON", "description": "Software engineer at Acme Corp.", "attributes": {"confidence": 0.9}}
				],
				"relations": [
					{"subject_name": "Alice", "subject_type": "PERSON", "predicate": "works_at", "object_name": "Acme Corp", "object_type": "ORGANIZATION", "attributes": {}}
				]
			}`,
			wantEntities: 1,
			wantFacts:    1,
		},
		{
			name: "skips_empty_entity_names",
			raw: `{
				"entities": [
					{"name": "", "type": "PERSON"},
					{"name": "Bob", "type": "PERSON"}
				],
				"relations": []
			}`,
			wantEntities: 1,
			wantFacts:    0,
		},
		{
			name: "skips_invalid_facts",
			raw: `{
				"entities": [],
				"relations": [
					{"subject_name": "", "subject_type": "PERSON", "predicate": "works_at", "object_name": "Acme Corp", "object_type": "ORGANIZATION"},
					{"subject_name": "Alice", "subject_type": "PERSON", "predicate": "works_at", "object_name": "Acme Corp", "object_type": "ORGANIZATION"}
				]
			}`,
			wantEntities: 0,
			wantFacts:    1,
		},
		{
			name: "truncates_long_description",
			raw: `{
				"entities": [
					{"name": "Alice", "type": "PERSON", "description": "` + strings.Repeat("word ", 100) + `. End.", "attributes": {}}
				],
				"relations": []
			}`,
			wantEntities:  1,
			descTruncated: true,
		},
		{
			name: "cleans_uncertainty_metadata",
			raw: `{
				"entities": [
					{"name": "Alice", "type": "PERSON", "description": "", "attributes": {"status": "implied by context", "provider": "internal"}}
				],
				"relations": []
			}`,
			wantEntities:    1,
			metadataCleaned: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseLLMResponse(tt.raw)
			if err != nil {
				t.Fatalf("parseLLMResponse() error = %v", err)
			}

			if len(result.Entities) != tt.wantEntities {
				t.Errorf("entities count = %d, want %d", len(result.Entities), tt.wantEntities)
			}
			if len(result.Facts) != tt.wantFacts {
				t.Errorf("facts count = %d, want %d", len(result.Facts), tt.wantFacts)
			}

			if tt.descTruncated && len(result.Entities) > 0 {
				descLen := len([]rune(result.Entities[0].Description))
				if descLen > 500 {
					t.Errorf("description not truncated: %d runes", descLen)
				}
			}

			if tt.metadataCleaned && len(result.Entities) > 0 {
				meta := result.Entities[0].Metadata
				if _, ok := meta["status"]; ok {
					t.Error("uncertainty metadata should have been cleaned")
				}
				if _, ok := meta["provider"]; !ok {
					t.Error("valid metadata should be preserved")
				}
			}
		})
	}
}

func TestParseLLMResponse_InvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parseLLMResponse(`{invalid json}`)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}
