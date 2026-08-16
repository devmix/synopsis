package onnx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewLibraryCacheManager(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	mgr := NewLibraryCacheManager(dataDir)

	if mgr == nil {
		t.Fatal("expected non-nil cache manager")
	}

	wantCacheDir := filepath.Join(dataDir, "onnxruntime")
	if mgr.cacheDir != wantCacheDir {
		t.Errorf("cacheDir = %q, want %q", mgr.cacheDir, wantCacheDir)
	}

	wantCacheFile := filepath.Join(wantCacheDir, ".cache.json")
	if mgr.cacheFile != wantCacheFile {
		t.Errorf("cacheFile = %q, want %q", mgr.cacheFile, wantCacheFile)
	}
}

func TestLibraryCacheManager_Load_NotExist(t *testing.T) {
	t.Parallel()

	mgr := NewLibraryCacheManager(t.TempDir())

	cache, err := mgr.Load()
	if err != nil {
		t.Errorf("Load() on non-existent cache should return nil error, got: %v", err)
	}
	if cache != nil {
		t.Error("Load() on non-existent cache should return nil cache")
	}
}

func TestLibraryCacheManager_Load_ParseError(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	mgr := NewLibraryCacheManager(dataDir)

	// Create invalid JSON in cache file.
	cacheDir := filepath.Join(dataDir, "onnxruntime")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	if err := os.WriteFile(mgr.cacheFile, []byte("{invalid json}"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cache, err := mgr.Load()
	if err == nil {
		t.Error("Load() with invalid JSON should return an error")
	}
	if cache != nil {
		t.Error("Load() with invalid JSON should return nil cache")
	}
}

func TestLibraryCacheManager_SaveAndLoad(t *testing.T) {
	t.Parallel()

	mgr := NewLibraryCacheManager(t.TempDir())

	cache := &LibraryCache{
		Version:     "1.18.0",
		LibraryPath: "/tmp/libonnxruntime.so.1.18.0",
		InstallTime: "2024-01-01T00:00:00Z",
		Platform:    "linux-amd64",
	}

	if err := mgr.Save(cache); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	loaded, err := mgr.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded == nil {
		t.Fatal("Load() returned nil after Save")
	}

	tests := []struct {
		name  string
		got   string
		want  string
	}{
		{"Version", loaded.Version, cache.Version},
		{"LibraryPath", loaded.LibraryPath, cache.LibraryPath},
		{"InstallTime", loaded.InstallTime, cache.InstallTime},
		{"Platform", loaded.Platform, cache.Platform},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
			}
		})
	}
}

func TestLibraryCacheManager_IsInstalled_NotCached(t *testing.T) {
	t.Parallel()

	mgr := NewLibraryCacheManager(t.TempDir())

	installed, path := mgr.IsInstalled("1.18.0")
	if installed {
		t.Error("IsInstalled should return false when cache is empty")
	}
	if path != "" {
		t.Errorf("IsInstalled path = %q, want empty string", path)
	}
}

func TestLibraryCacheManager_IsInstalled_VersionMismatch(t *testing.T) {
	t.Parallel()

	mgr := NewLibraryCacheManager(t.TempDir())

	cache := &LibraryCache{
		Version:     "1.17.0", // Different version
		LibraryPath: "/tmp/libonnxruntime.so.1.17.0",
	}
	if err := mgr.Save(cache); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	installed, path := mgr.IsInstalled("1.18.0") // Check for different version.
	if installed {
		t.Error("IsInstalled should return false when version doesn't match")
	}
	if path != "" {
		t.Errorf("IsInstalled path = %q, want empty string", path)
	}
}

func TestLibraryCacheManager_IsInstalled_FileMissing(t *testing.T) {
	t.Parallel()

	mgr := NewLibraryCacheManager(t.TempDir())

	cache := &LibraryCache{
		Version:     "1.18.0",
		LibraryPath: "/nonexistent/path/libonnxruntime.so.1.18.0", // File doesn't exist.
	}
	if err := mgr.Save(cache); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	installed, path := mgr.IsInstalled("1.18.0")
	if installed {
		t.Error("IsInstalled should return false when library file doesn't exist")
	}
	if path != "" {
		t.Errorf("IsInstalled path = %q, want empty string", path)
	}
}

func TestLibraryCacheManager_IsInstalled_Valid(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	mgr := NewLibraryCacheManager(dataDir)

	libPath := filepath.Join(dataDir, "onnxruntime", "libonnxruntime.so.1.18.0")
	if err := os.MkdirAll(filepath.Dir(libPath), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(libPath, []byte{}, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cache := &LibraryCache{
		Version:     "1.18.0",
		LibraryPath: libPath,
	}
	if err := mgr.Save(cache); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	installed, path := mgr.IsInstalled("1.18.0")
	if !installed {
		t.Error("IsInstalled should return true for valid installation")
	}
	if path != libPath {
		t.Errorf("IsInstalled path = %q, want %q", path, libPath)
	}
}

func TestLibraryCacheManager_Remove(t *testing.T) {
	t.Parallel()

	mgr := NewLibraryCacheManager(t.TempDir())

	cache := &LibraryCache{Version: "1.18.0"}
	if err := mgr.Save(cache); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if err := mgr.Remove(); err != nil {
		t.Errorf("Remove() error = %v", err)
	}

	// Verify cache file is gone.
	if _, err := os.Stat(mgr.cacheFile); !os.IsNotExist(err) {
		t.Error("cache file should not exist after Remove")
	}
}

func TestLibraryCacheManager_Remove_NotExist(t *testing.T) {
	t.Parallel()

	mgr := NewLibraryCacheManager(t.TempDir())

	err := mgr.Remove()
	if err == nil {
		t.Error("Remove on non-existent cache file should return an error")
	}
}

func TestLibraryCacheManager_GetCacheDir(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	mgr := NewLibraryCacheManager(dataDir)

	got := mgr.GetCacheDir()
	want := filepath.Join(dataDir, "onnxruntime")
	if got != want {
		t.Errorf("GetCacheDir() = %q, want %q", got, want)
	}
}
