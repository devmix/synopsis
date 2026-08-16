package embedding_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devmix/synopsis/internal/embedding"
)

func tokenizerTestPath(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "internal", "embedding", "testdata", "vocab.txt")
}

func TestSugarmeTokenizer_Basic(t *testing.T) {
	t.Parallel()

	path := tokenizerTestPath(t)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("testdata not available: %s", path)
	}

	tests := []struct {
		name       string
		text       string
		wantLen    int
		notWantPad bool // first token should not be [PAD] (Predicate 0)
	}{
		{
			name:       "simple sentence",
			text:       "hello world",
			wantLen:    embedding.DefaultMaxLength,
			notWantPad: true,
		},
		{
			name:       "single word",
			text:       "test",
			wantLen:    embedding.DefaultMaxLength,
			notWantPad: true,
		},
		{
			name:    "empty text returns all padding",
			text:    "",
			wantLen: embedding.DefaultMaxLength,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tz, err := embedding.NewSugarmeTokenizer(path, embedding.DefaultMaxLength)
			if err != nil {
				t.Fatalf("NewSugarmeTokenizer() error = %v", err)
			}

			got, err := tz.Tokenize(tt.text)
			if err != nil {
				t.Fatalf("Tokenize() error = %v", err)
			}

			if len(got) != tt.wantLen {
				t.Errorf("len(tokens) = %d, want %d", len(got), tt.wantLen)
			}

			if tt.notWantPad && got[0] == 0 {
				t.Error("first token should not be [PAD]")
			}
		})
	}
}

func TestSugarmeTokenizer_Truncation(t *testing.T) {
	t.Parallel()

	path := tokenizerTestPath(t)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("testdata not available: %s", path)
	}

	tests := []struct {
		name      string
		maxLength int
		text      string
		wantLen   int
	}{
		{
			name:      "truncate long text",
			maxLength: 5,
			text:      "one two three four five six seven eight nine ten",
			wantLen:   5,
		},
		{
			name:      "no truncation needed",
			maxLength: 20,
			text:      "short text",
			wantLen:   20, // will be padded instead
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tz, err := embedding.NewSugarmeTokenizer(path, tt.maxLength)
			if err != nil {
				t.Fatalf("NewSugarmeTokenizer() error = %v", err)
			}

			got, err := tz.Tokenize(tt.text)
			if err != nil {
				t.Fatalf("Tokenize() error = %v", err)
			}

			if len(got) != tt.wantLen {
				t.Errorf("len(tokens) = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestSugarmeTokenizer_Padding(t *testing.T) {
	t.Parallel()

	path := tokenizerTestPath(t)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("testdata not available: %s", path)
	}

	tests := []struct {
		name      string
		maxLength int
		text      string
		wantLen   int
	}{
		{
			name:      "short text padded",
			maxLength: 10,
			text:      "hi",
			wantLen:   10,
		},
		{
			name:      "empty text fully padded",
			maxLength: 8,
			text:      "",
			wantLen:   8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tz, err := embedding.NewSugarmeTokenizer(path, tt.maxLength)
			if err != nil {
				t.Fatalf("NewSugarmeTokenizer() error = %v", err)
			}

			got, err := tz.Tokenize(tt.text)
			if err != nil {
				t.Fatalf("Tokenize() error = %v", err)
			}

			if len(got) != tt.wantLen {
				t.Errorf("len(tokens) = %d, want %d", len(got), tt.wantLen)
			}
		})
	}
}

func TestSugarmeTokenizer_EmptyText(t *testing.T) {
	t.Parallel()

	path := tokenizerTestPath(t)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("testdata not available: %s", path)
	}

	tz, err := embedding.NewSugarmeTokenizer(path, embedding.DefaultMaxLength)
	if err != nil {
		t.Fatalf("NewSugarmeTokenizer() error = %v", err)
	}

	got, err := tz.Tokenize("")
	if err != nil {
		t.Fatalf("Tokenize(\"\") error = %v", err)
	}

	if len(got) != embedding.DefaultMaxLength {
		t.Errorf("len(tokens) = %d, want %d", len(got), embedding.DefaultMaxLength)
	}
}

func TestSugarmeTokenizer_Decode(t *testing.T) {
	t.Parallel()

	path := tokenizerTestPath(t)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("testdata not available: %s", path)
	}

	tests := []struct {
		name    string
		text    string
		wantSub string // substring that must appear in decoded text
	}{
		{
			name:    "roundtrip simple",
			text:    "hello world",
			wantSub: "hello",
		},
		{
			name:    "empty returns empty",
			text:    "",
			wantSub: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tz, err := embedding.NewSugarmeTokenizer(path, embedding.DefaultMaxLength)
			if err != nil {
				t.Fatalf("NewSugarmeTokenizer() error = %v", err)
			}

			tokens, err := tz.Tokenize(tt.text)
			if err != nil {
				t.Fatalf("Tokenize() error = %v", err)
			}

			decoded, err := tz.Decode(tokens)
			if err != nil {
				t.Fatalf("Decode() error = %v", err)
			}

			if tt.wantSub != "" && !strings.Contains(decoded, tt.wantSub) {
				t.Errorf("decoded text %q does not contain %q", decoded, tt.wantSub)
			}
		})
	}
}

func TestSugarmeTokenizer_MaxLength(t *testing.T) {
	t.Parallel()

	path := tokenizerTestPath(t)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("testdata not available: %s", path)
	}

	tests := []struct {
		name      string
		maxLength int
		want      int
	}{
		{
			name:      "default max length",
			maxLength: 0, // should default to DefaultMaxLength
			want:      embedding.DefaultMaxLength,
		},
		{
			name:      "custom max length",
			maxLength: 128,
			want:      128,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tz, err := embedding.NewSugarmeTokenizer(path, tt.maxLength)
			if err != nil {
				t.Fatalf("NewSugarmeTokenizer() error = %v", err)
			}

			if tz.MaxLength() != tt.want {
				t.Errorf("MaxLength() = %d, want %d", tz.MaxLength(), tt.want)
			}
		})
	}
}

func TestSugarmeTokenizer_Concurrent(t *testing.T) {
	t.Parallel()

	path := tokenizerTestPath(t)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("testdata not available: %s", path)
	}

	texts := []string{
		"concurrent tokenization test one",
		"concurrent tokenization test two",
		"another text for testing",
	}

	tz, err := embedding.NewSugarmeTokenizer(path, 512)
	if err != nil {
		t.Fatalf("NewSugarmeTokenizer() error = %v", err)
	}

	const goroutines = 10
	errCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			text := texts[idx%len(texts)]
			tokens, err := tz.Tokenize(text)
			if err != nil {
				errCh <- err
				return
			}
			if len(tokens) != 512 {
				errCh <- fmt.Errorf("goroutine %d: len = %d, want %d", idx, len(tokens), 512)
				return
			}
			errCh <- nil
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		if err := <-errCh; err != nil {
			t.Errorf("concurrent error: %v", err)
		}
	}
}

func BenchmarkSugarmeTokenizer_Tokenize(b *testing.B) {
	path := filepath.Join("..", "..", "internal", "embedding", "testdata", "tokenizer.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		b.Skipf("testdata not available: %s", path)
	}

	tz, err := embedding.NewSugarmeTokenizer(path, 512)
	if err != nil {
		b.Fatalf("NewSugarmeTokenizer() error = %v", err)
	}

	text := "This is a benchmark sentence with multiple words for testing performance."

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = tz.Tokenize(text)
	}
}
