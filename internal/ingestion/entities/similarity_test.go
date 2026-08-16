package entities

import "testing"

func TestNormalizeName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "trims whitespace", in: "  Apple Inc.  ", want: "apple inc."},
		{name: "collapses internal spaces", in: "Стив    Джобс", want: "стив джобс"},
		{name: "lowercases", in: "GOOGLE", want: "google"},
		{name: "empty", in: "   ", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeName(tt.in); got != tt.want {
				t.Errorf("normalizeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestGetBigrams(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "two runes", in: "ab", want: []string{"ab"}},
		{name: "single rune kept whole", in: "a", want: []string{"a"}},
		{name: "cyrillic runes", in: "привет", want: []string{"пр", "ри", "ив", "ве", "ет"}},
		{name: "normalizes before splitting", in: "AB cd", want: []string{"ab", "b ", " c", "cd"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getBigrams(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("getBigrams(%q) = %v, want %v", tt.in, got, tt.want)
			}
			for _, w := range tt.want {
				if _, ok := got[w]; !ok {
					t.Errorf("getBigrams(%q) missing %q, got %v", tt.in, w, got)
				}
			}
		})
	}
}

func TestJaroWinkler(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want float64
	}{
		{name: "identical", a: "Apple", b: "Apple", want: 1.0},
		{name: "case and whitespace insensitive", a: "  APPLE  ", b: "apple", want: 1.0},
		{name: "empty strings", a: "", b: "", want: 1.0},
		{name: "one empty", a: "apple", b: "", want: 0.0},
		{name: "prefix bonus substring", a: "apple", b: "apple inc.", want: 0.9},
		{name: "cyrillic initials", a: "Стив Джобс", b: "С. Джобс", want: 0.873},
		{name: "transposition", a: "martha", b: "marhta", want: 0.961},
		{name: "unrelated", a: "apple", b: "xyz", want: 0.0},
	}
	const eps = 1e-3
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jaroWinkler(tt.a, tt.b)
			if got < tt.want-eps || got > tt.want+eps {
				t.Errorf("jaroWinkler(%q, %q) = %.3f, want %.3f", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestJaroWinklerThresholdCases(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{name: "synonym merges", a: "Apple", b: "Apple Inc.", want: true},
		{name: "cyrillic initials merge", a: "Стив Джобс", b: "С. Джобс", want: true},
		{name: "different names stay apart", a: "Иван Иванов", b: "Петр Петров", want: false},
		{name: "different short names stay apart", a: "Варя", b: "Вера", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const threshold = 0.8
			got := jaroWinkler(tt.a, tt.b) >= threshold
			if got != tt.want {
				t.Errorf("jaroWinkler(%q, %q) >= %v = %v, want %v", tt.a, tt.b, threshold, got, tt.want)
			}
		})
	}
}
