package utils_test

import (
	"testing"

	"github.com/devmix/synopsis/internal/utils"
)

func TestTruncate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{
			name: "empty string",
			s:    "",
			max:  10,
			want: "",
		},
		{
			name: "max zero returns empty",
			s:    "hello",
			max:  0,
			want: "",
		},
		{
			name: "max negative returns empty",
			s:    "hello",
			max:  -5,
			want: "",
		},
		{
			name: "exact max length no suffix",
			s:    "hello",
			max:  5,
			want: "hello",
		},
		{
			name: "max greater than length no suffix",
			s:    "hi",
			max:  10,
			want: "hi",
		},
		{
			name: "truncate with ellipsis",
			s:    "hello world",
			max:  5,
			want: "hello...",
		},
		{
			name: "utf-8 multibyte emoji not split",
			s:    "\u2603\u2744\ufe0f", // snowman + snowflake with variation selector (multi-byte)
			max:  1,
			want: "\u2603...",
		},
		{
			name: "utf-8 multibyte emoji two runes",
			s:    "\u2603\u2744\ufe0f",
			max:  2,
			want: "\u2603\u2744...",
		},
		{
			name: "cyrillic multibyte not split",
			s:    "\u041f\u0440\u0438\u0432\u0435\u0442", // Привет (6 runes, 12 bytes)
			max:  3,
			want: "\u041f\u0440\u0438...",
		},
		{
			name: "single rune truncation",
			s:    "abc",
			max:  1,
			want: "a...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := utils.Truncate(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}
