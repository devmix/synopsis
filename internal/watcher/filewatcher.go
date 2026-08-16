// Package watcher provides a filesystem watcher that monitors source directories
// and triggers callbacks when files change, with configurable debounce intervals.
package watcher

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/devmix/synopsis/internal/logger"
)

// FileWatcher watches a set of directories for file changes and triggers
// a callback after a debounce period.
type FileWatcher struct {
	watcher     *fsnotify.Watcher
	callback    func(ctx context.Context, changedPaths []string) error
	debounce    time.Duration
	mu          sync.Mutex
	pending     map[string]time.Time // path -> last change time
	lastRun     time.Time
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	done        chan struct{}
	log         *logger.Logger
}

// WatcherOption configures the FileWatcher during construction.
type WatcherOption func(*FileWatcher)

// WithLogger sets the structured logger for the file watcher.
func WithLogger(l *logger.Logger) WatcherOption {
	return func(fw *FileWatcher) { fw.log = l }
}

// New creates a new FileWatcher with the given debounce interval and callback.
func New(debounce time.Duration, cb func(ctx context.Context, changedPaths []string) error, opts ...WatcherOption) (*FileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create fsnotify watcher: %w", err)
	}

	fw := &FileWatcher{
		watcher:  w,
		callback: cb,
		debounce: debounce,
		pending:  make(map[string]time.Time),
		done:     make(chan struct{}),
	}

	for _, opt := range opts {
		opt(fw)
	}

	return fw, nil
}

// Add adds a directory to the watch list. It watches recursively by watching
// all existing subdirectories and new directories as they appear.
func (fw *FileWatcher) Add(dir string) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("resolve path %s: %w", dir, err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("stat directory %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	if err := fw.watcher.Add(dir); err != nil {
		return fmt.Errorf("watch directory %s: %w", dir, err)
	}

	// Also watch existing subdirectories.
	if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		if path == dir {
			return nil // already added
		}
		return fw.watcher.Add(path)
	}); err != nil {
		return fmt.Errorf("walk subdirectories of %s: %w", dir, err)
	}

	return nil
}

// Start begins watching for file changes in a background goroutine.
func (fw *FileWatcher) Start(ctx context.Context) error {
	if fw.cancel != nil {
		return fmt.Errorf("watcher already started")
	}

	wctx, cancel := context.WithCancel(ctx)
	fw.cancel = cancel
	fw.wg.Add(1)

	go func() {
		defer fw.wg.Done()
		fw.run(wctx)
	}()

	return nil
}

// Stop closes the watcher and waits for the background goroutine to finish.
func (fw *FileWatcher) Stop() error {
	if fw.cancel == nil {
		return nil // not started
	}

	fw.cancel()

	// Close the fsnotify watcher to unblock the event loop.
	if err := fw.watcher.Close(); err != nil {
		return fmt.Errorf("close fsnotify watcher: %w", err)
	}

	// Wait for goroutine to finish with a timeout.
	done := make(chan struct{})
	go func() {
		fw.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("watcher stop timed out after 5s")
	}
}

// run is the main event loop for the file watcher.
func (fw *FileWatcher) run(ctx context.Context) {
	ticker := time.NewTicker(fw.debounce / 2)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-fw.watcher.Events:
			if !ok {
				return // channel closed
			}
			fw.handleEvent(event)
		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return // channel closed
			}
			if fw.log != nil {
				fw.log.Error("file watcher error", "error", err)
			}
		case <-ticker.C:
			fw.flushPending(ctx)
		}
	}
}

// handleEvent processes a single filesystem event.
func (fw *FileWatcher) handleEvent(event fsnotify.Event) {
	// Watch new directories as they appear so files created inside them are
	// covered too. This must run before the extension filter: directories
	// have no extension of their own.
	info, err := os.Stat(event.Name)
	if err == nil && info.IsDir() {
		if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) {
			if err := fw.watcher.Add(event.Name); err != nil {
				if fw.log != nil {
					fw.log.Error("add new directory to watch", "path", event.Name, "error", err)
				}
			}
		}
		return
	}

	// Only care about regular files with relevant extensions.
	ext := filepath.Ext(event.Name)
	if ext != ".md" && ext != ".json" {
		return
	}

	fw.mu.Lock()
	defer fw.mu.Unlock()

	// Normalize the path.
	absPath, err := filepath.Abs(event.Name)
	if err != nil {
		return
	}

	switch {
	case event.Has(fsnotify.Create):
		fallthrough
	case event.Has(fsnotify.Write):
		fallthrough
	case event.Has(fsnotify.Chmod):
		fallthrough
	case event.Has(fsnotify.Remove):
		fallthrough
	case event.Has(fsnotify.Rename):
		// Removals and renames are recorded too so the callback can prune
		// deleted documents and re-index renamed files.
		fw.pending[absPath] = time.Now()
	}
}

// flushPending checks if enough time has passed since the last change and triggers callback.
func (fw *FileWatcher) flushPending(ctx context.Context) {
	fw.mu.Lock()

	now := time.Now()
	if len(fw.pending) == 0 {
		fw.mu.Unlock()
		return
	}

	// Check if debounce period has elapsed since the last change.
	var latestChange time.Time
	for _, t := range fw.pending {
		if t.After(latestChange) {
			latestChange = t
		}
	}

	if now.Sub(latestChange) < fw.debounce {
		fw.mu.Unlock()
		return // not enough time has passed
	}

	// Check if debounce period has elapsed since the last run.
	if now.Sub(fw.lastRun) < fw.debounce {
		fw.mu.Unlock()
		return
	}

	// Collect pending paths and clear the map.
	paths := make([]string, 0, len(fw.pending))
	for p := range fw.pending {
		paths = append(paths, p)
	}
	fw.pending = make(map[string]time.Time)
	fw.lastRun = now

	fw.mu.Unlock()

	// Trigger callback outside the lock.
	if err := fw.callback(ctx, paths); err != nil {
		if fw.log != nil {
			fw.log.Error("reindex callback error", "error", err)
		}
	}
}
