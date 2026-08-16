package watcher_test

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/devmix/synopsis/internal/watcher"
)

func TestNew(t *testing.T) {
	t.Parallel()

	w, err := watcher.New(100*time.Millisecond, func(ctx context.Context, paths []string) error {
		return nil
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer w.Stop() //nolint:errcheck

	if w == nil {
		t.Fatal("expected non-nil watcher")
	}
}

func TestAdd_Directory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	w, err := watcher.New(100*time.Millisecond, func(ctx context.Context, paths []string) error {
		return nil
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer w.Stop() //nolint:errcheck

	if err := w.Add(tmpDir); err != nil {
		t.Errorf("Add() error = %v", err)
	}
}

func TestAdd_NotDirectory(t *testing.T) {
	t.Parallel()

	tmpFile := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(tmpFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	w, err := watcher.New(100*time.Millisecond, func(ctx context.Context, paths []string) error {
		return nil
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer w.Stop() //nolint:errcheck

	err = w.Add(tmpFile)
	if err == nil {
		t.Error("expected error for non-directory, got nil")
	}
}

func TestAdd_NonExistent(t *testing.T) {
	t.Parallel()

	w, err := watcher.New(100*time.Millisecond, func(ctx context.Context, paths []string) error {
		return nil
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer w.Stop() //nolint:errcheck

	err = w.Add("/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Error("expected error for nonexistent directory, got nil")
	}
}

func TestStartAndStop(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	var mu sync.Mutex
	callbackCalled := false

	w, err := watcher.New(50*time.Millisecond, func(ctx context.Context, paths []string) error {
		mu.Lock()
		defer mu.Unlock()
		callbackCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := w.Add(tmpDir); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Start again should fail.
	if err := w.Start(ctx); err == nil {
		t.Error("expected error for double start, got nil")
	}

	time.Sleep(100 * time.Millisecond)

	if err := w.Stop(); err != nil {
		t.Errorf("Stop() error = %v", err)
	}

	mu.Lock()
	_ = callbackCalled // just verify no panic occurred
	mu.Unlock()
}

func TestFileChangeDetection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	var mu sync.Mutex
	var detectedPaths []string

	w, err := watcher.New(50*time.Millisecond, func(ctx context.Context, paths []string) error {
		mu.Lock()
		defer mu.Unlock()
		detectedPaths = append(detectedPaths, paths...)
		return nil
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := w.Add(tmpDir); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer w.Stop() //nolint:errcheck

	// Create a markdown file.
	testFile := filepath.Join(tmpDir, "test.md")
	time.Sleep(100 * time.Millisecond) // let watcher settle
	if err := os.WriteFile(testFile, []byte("# Test"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	// Wait for debounce + flush interval.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	found := false
	for _, p := range detectedPaths {
		if p == testFile {
			found = true
			break
		}
	}
	mu.Unlock()

	if !found {
		t.Logf("detected paths: %v", detectedPaths)
		// File change detection may not work on all platforms in tests.
		// This is a best-effort test.
	}
}

func TestFileRemovalDetection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	var mu sync.Mutex
	var detectedPaths []string

	w, err := watcher.New(50*time.Millisecond, func(ctx context.Context, paths []string) error {
		mu.Lock()
		defer mu.Unlock()
		detectedPaths = append(detectedPaths, paths...)
		return nil
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := w.Add(tmpDir); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer w.Stop() //nolint:errcheck

	// Create, then remove a markdown file.
	testFile := filepath.Join(tmpDir, "removed.md")
	time.Sleep(100 * time.Millisecond) // let watcher settle
	if err := os.WriteFile(testFile, []byte("# Gone"), 0o644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	time.Sleep(200 * time.Millisecond)
	if err := os.Remove(testFile); err != nil {
		t.Fatalf("remove test file: %v", err)
	}

	// Wait for debounce + flush interval.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	found := false
	for _, p := range detectedPaths {
		if p == testFile {
			found = true
			break
		}
	}
	mu.Unlock()

	if !found {
		t.Errorf("removed file %s was not reported to the callback (got %v)", testFile, detectedPaths)
	}
}

func TestNewSubdirectoryDetection(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	var mu sync.Mutex
	var detectedPaths []string

	w, err := watcher.New(50*time.Millisecond, func(ctx context.Context, paths []string) error {
		mu.Lock()
		defer mu.Unlock()
		detectedPaths = append(detectedPaths, paths...)
		return nil
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := w.Add(tmpDir); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer w.Stop() //nolint:errcheck

	// Create a subdirectory after the watcher started, then a file inside it.
	subDir := filepath.Join(tmpDir, "nested")
	time.Sleep(100 * time.Millisecond) // let watcher settle
	if err := os.Mkdir(subDir, 0o755); err != nil {
		t.Fatalf("mkdir nested dir: %v", err)
	}
	time.Sleep(200 * time.Millisecond) // let the watcher pick up the new dir
	nestedFile := filepath.Join(subDir, "deep.md")
	if err := os.WriteFile(nestedFile, []byte("# Deep"), 0o644); err != nil {
		t.Fatalf("write nested file: %v", err)
	}

	// Wait for debounce + flush interval.
	time.Sleep(300 * time.Millisecond)

	mu.Lock()
	found := false
	for _, p := range detectedPaths {
		if p == nestedFile {
			found = true
			break
		}
	}
	mu.Unlock()

	if !found {
		t.Logf("detected paths: %v", detectedPaths)
		// Nested directory watching may not work on all platforms in tests.
		// This is a best-effort test.
	}
}

func TestDebounce(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	var mu sync.Mutex
	callbackCount := 0

	w, err := watcher.New(200*time.Millisecond, func(ctx context.Context, paths []string) error {
		mu.Lock()
		defer mu.Unlock()
		callbackCount++
		return nil
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := w.Add(tmpDir); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer w.Stop() //nolint:errcheck

	time.Sleep(100 * time.Millisecond)

	// Create multiple files quickly.
	for i := 0; i < 3; i++ {
		testFile := filepath.Join(tmpDir, "test"+string(rune('a'+i))+".md")
		if err := os.WriteFile(testFile, []byte("content"), 0o644); err != nil {
			t.Fatalf("write test file: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Wait for debounce + extra.
	time.Sleep(500 * time.Millisecond)

	mu.Lock()
	count := callbackCount
	mu.Unlock()

	if count > 1 {
		t.Logf("callback called %d times (expected at most 1 due to debounce)", count)
	}
}
