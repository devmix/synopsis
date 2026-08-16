package onnx

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/devmix/synopsis/internal/config"
)

func testONNXConfig() config.ONNXConfig {
	return config.ONNXConfig{
		Runtime: config.ONNXRuntimeConfig{
			Version: "1.20.0",
			Platforms: []config.ONNXPlatformConfig{
				{Key: "linux-amd64", OS: "linux", Arch: "amd64", ArchiveURL: "https://example.com/test.zip", LibraryName: "libonnxruntime.so"},
			},
		},
	}
}

func TestNewLibraryManager(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		dataDir string
		wantErr bool
	}{
		{
			name:    "valid data dir",
			dataDir: t.TempDir(),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := testONNXConfig()
			mgr, err := NewLibraryManager(tt.dataDir, &cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewLibraryManager() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if mgr == nil {
				t.Fatal("expected non-nil manager")
			}

			if mgr.httpClient == nil {
				t.Error("httpClient should not be nil after construction")
			}

			if mgr.version != cfg.Runtime.Version {
				t.Errorf("version = %q, want %q", mgr.version, cfg.Runtime.Version)
			}

			if mgr.cacheMgr == nil {
				t.Error("cacheMgr should not be nil")
			}

			if mgr.platform.OS == "" {
				t.Error("platform OS should not be empty")
			}
			if mgr.platform.Arch == "" {
				t.Error("platform Arch should not be empty")
			}
		})
	}
}

func TestLibraryManager_DownloadLibraryContext_NilContext(t *testing.T) {
	t.Parallel()

	cfg := testONNXConfig()
	mgr, err := NewLibraryManager(t.TempDir(), &cfg)
	if err != nil {
		t.Fatalf("NewLibraryManager() error = %v", err)
	}

	err = mgr.DownloadLibraryContext(nil) //nolint:staticcheck // intentionally testing nil context rejection.
	if err == nil {
		t.Error("DownloadLibraryContext(nil) should return an error")
	} else if got := err.Error(); got != "context must not be nil" {
		t.Errorf("error = %q, want %q", got, "context must not be nil")
	}
}

func TestLibraryManager_downloadArchive_NilContext(t *testing.T) {
	t.Parallel()

	cfg := testONNXConfig()
	mgr, err := NewLibraryManager(t.TempDir(), &cfg)
	if err != nil {
		t.Fatalf("NewLibraryManager() error = %v", err)
	}

	err = mgr.downloadArchive(nil, "http://example.com/test.zip", "/tmp/test.zip") //nolint:staticcheck // intentionally testing nil context rejection.
	if err == nil {
		t.Error("downloadArchive with nil context should return an error")
	} else if got := err.Error(); got != "context must not be nil" {
		t.Errorf("error = %q, want %q", got, "context must not be nil")
	}
}

func TestLibraryManager_downloadArchive_NilHTTPClient(t *testing.T) {
	t.Parallel()

	cfg := testONNXConfig()
	mgr, err := NewLibraryManager(t.TempDir(), &cfg)
	if err != nil {
		t.Fatalf("NewLibraryManager() error = %v", err)
	}

	mgr.httpClient = nil

	ctx := context.Background()
	err = mgr.downloadArchive(ctx, "http://example.com/test.zip", "/tmp/test.zip")
	if err == nil {
		t.Error("downloadArchive with nil httpClient should return an error")
	} else if got := err.Error(); got != "http client is not initialized" {
		t.Errorf("error = %q, want %q", got, "http client is not initialized")
	}
}

func TestLibraryManager_downloadArchive_ContextCancellation(t *testing.T) {
	t.Parallel()

	cfg := testONNXConfig()
	mgr, err := NewLibraryManager(t.TempDir(), &cfg)
	if err != nil {
		t.Fatalf("NewLibraryManager() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = mgr.downloadArchive(ctx, "http://example.com/test.zip", "/tmp/test-cancelled.zip")
	if err == nil {
		t.Error("downloadArchive with cancelled context should return an error")
	}
}

func TestLibraryManager_GetVersion(t *testing.T) {
	t.Parallel()

	cfg := testONNXConfig()
	mgr, err := NewLibraryManager(t.TempDir(), &cfg)
	if err != nil {
		t.Fatalf("NewLibraryManager() error = %v", err)
	}

	got := mgr.GetVersion()
	if got != cfg.Runtime.Version {
		t.Errorf("GetVersion() = %q, want %q", got, cfg.Runtime.Version)
	}
}

func TestLibraryManager_GetCacheDir(t *testing.T) {
	t.Parallel()

	cfg := testONNXConfig()
	dataDir := t.TempDir()
	mgr, err := NewLibraryManager(dataDir, &cfg)
	if err != nil {
		t.Fatalf("NewLibraryManager() error = %v", err)
	}

	got := mgr.GetCacheDir()
	wantSuffix := "onnxruntime"
	if filepath.Base(got) != wantSuffix {
		t.Errorf("GetCacheDir() base = %q, want suffix %q", filepath.Base(got), wantSuffix)
	}
}

func TestLibraryManager_GetLibraryPath_NotInstalled(t *testing.T) {
	t.Parallel()

	cfg := testONNXConfig()
	mgr, err := NewLibraryManager(t.TempDir(), &cfg)
	if err != nil {
		t.Fatalf("NewLibraryManager() error = %v", err)
	}

	path, ok := mgr.GetLibraryPath()
	if ok {
		t.Error("GetLibraryPath() should return false when library is not installed")
	}
	if path != "" {
		t.Errorf("GetLibraryPath() = %q, want empty string", path)
	}
}

func TestLibraryManager_UninstallLibrary_NotInstalled(t *testing.T) {
	t.Parallel()

	cfg := testONNXConfig()
	mgr, err := NewLibraryManager(t.TempDir(), &cfg)
	if err != nil {
		t.Fatalf("NewLibraryManager() error = %v", err)
	}

	err = mgr.UninstallLibrary()
	if err != nil {
		t.Errorf("UninstallLibrary() on non-installed library should return nil, got: %v", err)
	}
}

func TestLibraryManager_UninstallLibrary_Installed(t *testing.T) {
	t.Parallel()

	cfg := testONNXConfig()
	dataDir := t.TempDir()
	mgr, err := NewLibraryManager(dataDir, &cfg)
	if err != nil {
		t.Fatalf("NewLibraryManager() error = %v", err)
	}

	libPath := filepath.Join(mgr.GetCacheDir(), "libonnxruntime.so")
	if err := os.MkdirAll(filepath.Dir(libPath), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(libPath, []byte("fake"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	leftover := filepath.Join(mgr.GetCacheDir(), "leftover-dir", "junk.txt")
	if err := os.MkdirAll(filepath.Dir(leftover), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(leftover, []byte("junk"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cache := &LibraryCache{Version: cfg.Runtime.Version, LibraryPath: libPath}
	if err := mgr.cacheMgr.Save(cache); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if _, ok := mgr.GetLibraryPath(); !ok {
		t.Fatal("test setup failed: library should be installed")
	}

	if err := mgr.UninstallLibrary(); err != nil {
		t.Errorf("UninstallLibrary() on installed library should return nil, got: %v", err)
	}

	if _, ok := mgr.GetLibraryPath(); ok {
		t.Error("GetLibraryPath() should report not installed after uninstall")
	}

	if _, err := os.Stat(mgr.GetCacheDir()); !os.IsNotExist(err) {
		t.Errorf("cache directory should be removed entirely, stat error = %v", err)
	}
}

func TestLibraryManager_EnsureLibrary_BackwardCompatible(t *testing.T) {
	t.Parallel()

	cfg := testONNXConfig()
	mgr, err := NewLibraryManager(t.TempDir(), &cfg)
	if err != nil {
		t.Fatalf("NewLibraryManager() error = %v", err)
	}

	mgr.SetHTTPClient(&http.Client{
		Timeout: 5 * time.Second,
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				Status:     "404 Not Found",
				StatusCode: http.StatusNotFound,
				Body:       http.NoBody,
			}, nil
		}),
	})

	_, err = mgr.EnsureLibrary()
	if err != nil {
		errMsg := err.Error()
		if errMsg == "download library: download archive: create request: net/http: nil Context" {
			t.Errorf("EnsureLibrary returned nil context error (the original bug): %v", err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestLibraryManager_downloadArchive_InvalidURL(t *testing.T) {
	t.Parallel()

	cfg := testONNXConfig()
	mgr, err := NewLibraryManager(t.TempDir(), &cfg)
	if err != nil {
		t.Fatalf("NewLibraryManager() error = %v", err)
	}

	ctx := context.Background()
	err = mgr.downloadArchive(ctx, "://invalid-url", "/tmp/test.zip")
	if err == nil {
		t.Error("downloadArchive with invalid URL should return an error")
	}
}

func TestLibraryManager_downloadArchive_HTTPError(t *testing.T) {
	t.Parallel()

	server := http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}),
	}

	listener, err := tcpListener()
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}

	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Close() //nolint:errcheck

	cfg := testONNXConfig()
	mgr, err := NewLibraryManager(t.TempDir(), &cfg)
	if err != nil {
		t.Fatalf("NewLibraryManager() error = %v", err)
	}

	ctx := context.Background()
	destPath := filepath.Join(t.TempDir(), "test.zip")
	err = mgr.downloadArchive(ctx, "http://"+listener.Addr().String()+"/notfound", destPath)
	if err == nil {
		t.Error("downloadArchive with 404 response should return an error")
	}
}

func tcpListener() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}
