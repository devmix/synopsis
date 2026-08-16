package entities

import "strings"

// normalizeName prepares an entity name for matching:
// trims surrounding whitespace, lowercases, and collapses internal whitespace runs.
func normalizeName(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// getBigrams returns the character bigrams of a normalized name.
// Rune-aware so Cyrillic (multi-byte UTF-8) names are handled correctly.
// Names shorter than two runes are represented by themselves.
func getBigrams(s string) map[string]struct{} {
	runes := []rune(normalizeName(s))
	if len(runes) < 2 {
		return map[string]struct{}{s: {}}
	}
	bigrams := make(map[string]struct{}, len(runes)-1)
	for i := 0; i < len(runes)-1; i++ {
		bigrams[string(runes[i:i+2])] = struct{}{}
	}
	return bigrams
}

// jaroWinkler returns the Jaro-Winkler similarity of two names in [0.0, 1.0].
// Prefix matches earn a bonus (up to 4 runes, scale factor 0.1).
func jaroWinkler(s1, s2 string) float64 {
	s1 = normalizeName(s1)
	s2 = normalizeName(s2)
	if s1 == s2 {
		return 1.0
	}

	r1 := []rune(s1)
	r2 := []rune(s2)
	len1, len2 := len(r1), len(r2)
	if len1 == 0 || len2 == 0 {
		return 0.0
	}

	matchDistance := max(len1, len2)/2 - 1
	if matchDistance < 0 {
		matchDistance = 0
	}

	matches1 := make([]bool, len1)
	matches2 := make([]bool, len2)
	matches := 0

	for i := 0; i < len1; i++ {
		start := i - matchDistance
		if start < 0 {
			start = 0
		}
		end := i + matchDistance
		if end >= len2 {
			end = len2 - 1
		}
		for j := start; j <= end; j++ {
			if matches2[j] || r1[i] != r2[j] {
				continue
			}
			matches1[i] = true
			matches2[j] = true
			matches++
			break
		}
	}

	if matches == 0 {
		return 0.0
	}

	transpositions := 0
	k := 0
	for i := 0; i < len1; i++ {
		if !matches1[i] {
			continue
		}
		for !matches2[k] {
			k++
		}
		if r1[i] != r2[k] {
			transpositions++
		}
		k++
	}

	m := float64(matches)
	t := float64(transpositions) / 2.0
	jaro := (m/float64(len1) + m/float64(len2) + (m-t)/m) / 3.0

	prefix := 0
	maxPrefix := 4
	if len1 < maxPrefix {
		maxPrefix = len1
	}
	if len2 < maxPrefix {
		maxPrefix = len2
	}
	for i := 0; i < maxPrefix; i++ {
		if r1[i] == r2[i] {
			prefix++
		} else {
			break
		}
	}

	return jaro + float64(prefix)*0.1*(1.0-jaro)
}
