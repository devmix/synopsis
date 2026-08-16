package embedding_test

import (
	"os"
	"testing"

	"github.com/devmix/synopsis/internal/embedding"
)

func TestNewSugarmeTokenizer_EmptyPath(t *testing.T) {
	t.Parallel()

	_, err := embedding.NewSugarmeTokenizer("", 512)
	if err == nil {
		t.Fatal("expected error for empty path, got nil")
	}
}

func TestNewSugarmeTokenizer_NonexistentFile(t *testing.T) {
	t.Parallel()

	_, err := embedding.NewSugarmeTokenizer("/nonexistent/tokenizer.json", 512)
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestNewSugarmeTokenizer_Load(t *testing.T) {
	t.Parallel()

	path := tokenizerTestPath(t)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("testdata not available: %s", path)
	}

	tz, err := embedding.NewSugarmeTokenizer(path, 512)
	if err != nil {
		t.Fatalf("NewSugarmeTokenizer() error = %v", err)
	}

	if tz == nil {
		t.Fatal("tokenizer is nil")
	}

	if tz.MaxLength() != 512 {
		t.Errorf("MaxLength() = %d, want 512", tz.MaxLength())
	}
}
