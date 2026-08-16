package prompts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileSuccess(t *testing.T) {
	dir := t.TempDir()
	content := "hello {{ .Name }}"
	if err := os.WriteFile(filepath.Join(dir, "greet.tmpl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	loader, err := NewLoader(dir, nil)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	info, err := loader.Load("greet")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if info.Template == nil {
		t.Fatal("expected non-nil template")
	}
	if info.Hash == "" {
		t.Error("expected non-empty hash")
	}

	var buf bytes.Buffer
	err = info.Template.Execute(&buf, map[string]string{"Name": "World"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if buf.String() != "hello World" {
		t.Errorf("rendered = %q, want %q", buf.String(), "hello World")
	}
}

func TestLoadNonexistentNoFallback(t *testing.T) {
	dir := t.TempDir()

	loader, err := NewLoader(dir, nil)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	_, err = loader.Load("nonexistent-template")
	if err == nil {
		t.Fatal("expected error for nonexistent template with no embedded default")
	}
}

func TestLoadInvalidTemplateSyntax(t *testing.T) {
	dir := t.TempDir()
	content := "hello {{ .Name" // unmatched delimiter
	if err := os.WriteFile(filepath.Join(dir, "bad.tmpl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	loader, err := NewLoader(dir, nil)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	_, err = loader.Load("bad")
	if err == nil {
		t.Fatal("expected error for invalid template syntax")
	}
}

func TestHashCorrectness(t *testing.T) {
	dir := t.TempDir()
	content := "known content for hash test"
	if err := os.WriteFile(filepath.Join(dir, "hash.tmpl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	loader, err := NewLoader(dir, nil)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	info, err := loader.Load("hash")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Verify hash matches SHA-256 of content.
	hash := sha256.Sum256([]byte(content))
	expectedHash := hex.EncodeToString(hash[:])
	if info.Hash != expectedHash {
		t.Errorf("hash = %q, want %q", info.Hash, expectedHash)
	}
}

func TestFuncMapJoin(t *testing.T) {
	dir := t.TempDir()
	content := "{{ join \", \" .Items }}"
	if err := os.WriteFile(filepath.Join(dir, "join.tmpl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	loader, err := NewLoader(dir, nil)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	info, err := loader.Load("join")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	err = info.Template.Execute(&buf, map[string][]string{"Items": {"a", "b", "c"}})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if buf.String() != "a, b, c" {
		t.Errorf("rendered = %q, want %q", buf.String(), "a, b, c")
	}
}

func TestFuncMapTruncate(t *testing.T) {
	dir := t.TempDir()
	content := "{{ truncate .Text 5 }}"
	if err := os.WriteFile(filepath.Join(dir, "trunc.tmpl"), []byte(content), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	loader, err := NewLoader(dir, nil)
	if err != nil {
		t.Fatalf("NewLoader: %v", err)
	}

	info, err := loader.Load("trunc")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	var buf bytes.Buffer
	err = info.Template.Execute(&buf, map[string]string{"Text": "hello world"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if buf.String() != "hello..." {
		t.Errorf("rendered = %q, want %q", buf.String(), "hello...")
	}
}

func TestNewLoaderEmptyPath(t *testing.T) {
	_, err := NewLoader("", nil)
	if err == nil {
		t.Fatal("expected error for empty base path")
	}
}
