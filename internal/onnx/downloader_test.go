package onnx_test

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/onnx"
)

func TestModelDownloader(t *testing.T) {
	t.Parallel()

	// Helper: config that skips SSRF check for httptest (localhost servers).
	testConfig := func() onnx.DownloadConfig {
		cfg := onnx.DefaultDownloadConfig()
		cfg.SkipSSRFCheck = true // allow localhost in tests
		return cfg
	}

	t.Run("DownloadFile succeeds with valid server", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "testfile.bin")

		serverContent := []byte("hello world test data for download")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(serverContent)))
			w.WriteHeader(http.StatusOK)
			w.Write(serverContent) //nolint:errcheck
		}))
		defer srv.Close()

		cfg := testConfig()
		dl := onnx.NewModelDownloader(cfg)

		if err := dl.DownloadFile(srv.URL, destPath, "", int64(len(serverContent))); err != nil {
			t.Fatalf("DownloadFile: %v", err)
		}

		data, err := os.ReadFile(destPath)
		if err != nil {
			t.Fatalf("read downloaded file: %v", err)
		}
		if string(data) != string(serverContent) {
			t.Errorf("downloaded content = %q, want %q", data, serverContent)
		}
	})

	t.Run("DownloadFile fails on 404", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "missing.bin")

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		cfg := testConfig()
		cfg.MaxRetries = 0 // no retries for faster test
		dl := onnx.NewModelDownloader(cfg)

		err := dl.DownloadFile(srv.URL, destPath, "", 0)
		if err == nil {
			t.Error("expected error on 404 response")
		}
	})

	t.Run("DownloadFile creates parent directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "subdir", "nested", "file.bin")

		serverContent := []byte("nested file content")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write(serverContent) //nolint:errcheck
		}))
		defer srv.Close()

		cfg := testConfig()
		dl := onnx.NewModelDownloader(cfg)

		if err := dl.DownloadFile(srv.URL, destPath, "", int64(len(serverContent))); err != nil {
			t.Fatalf("DownloadFile: %v", err)
		}

		if _, err := os.Stat(destPath); err != nil {
			t.Errorf("expected file to exist at nested path: %v", err)
		}
	})

	t.Run("DefaultDownloadConfig has sensible values", func(t *testing.T) {
		cfg := onnx.DefaultDownloadConfig()
		if cfg.MaxRetries < 1 {
			t.Error("MaxRetries should be at least 1")
		}
		if cfg.Timeout <= 0 {
			t.Error("Timeout should be positive")
		}
		if cfg.SkipSSRFCheck {
			t.Error("SkipSSRFCheck should be false by default (production safety)")
		}
	})

	t.Run("Checksum verification succeeds", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "checksummed.bin")

		serverContent := []byte("content with known checksum")
		h := sha256.New()
		h.Write(serverContent) //nolint:errcheck
		expectedChecksum := fmt.Sprintf("sha256:%x", h.Sum(nil))

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write(serverContent) //nolint:errcheck
		}))
		defer srv.Close()

		cfg := testConfig()
		dl := onnx.NewModelDownloader(cfg)

		if err := dl.DownloadFile(srv.URL, destPath, expectedChecksum, int64(len(serverContent))); err != nil {
			t.Fatalf("DownloadFile with valid checksum: %v", err)
		}
	})

	t.Run("Checksum verification fails on mismatch", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "badchecksum.bin")

		serverContent := []byte("content that does not match checksum")
		badChecksum := "sha256:0000000000000000000000000000000000000000000000000000000000000000"

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write(serverContent) //nolint:errcheck
		}))
		defer srv.Close()

		cfg := testConfig()
		dl := onnx.NewModelDownloader(cfg)

		err := dl.DownloadFile(srv.URL, destPath, badChecksum, int64(len(serverContent)))
		if err == nil {
			t.Error("expected checksum mismatch error")
		}
	})

	t.Run("Retry logic retries on failure", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "retry.bin")

		attempts := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts++
			if attempts < 3 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success after retries")) //nolint:errcheck
		}))
		defer srv.Close()

		cfg := testConfig()
		cfg.MaxRetries = 3
		cfg.RetryDelay = 0 // no delay for faster test
		dl := onnx.NewModelDownloader(cfg)

		if err := dl.DownloadFile(srv.URL, destPath, "", 25); err != nil {
			t.Fatalf("DownloadFile should succeed after retries: %v", err)
		}
		if attempts < 3 {
			t.Errorf("expected at least 3 attempts, got %d", attempts)
		}
	})

	t.Run("Retry logic fails after max retries exceeded", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "fail.bin")

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer srv.Close()

		cfg := testConfig()
		cfg.MaxRetries = 2
		cfg.RetryDelay = 0 // no delay for faster test
		dl := onnx.NewModelDownloader(cfg)

		err := dl.DownloadFile(srv.URL, destPath, "", 0)
		if err == nil {
			t.Error("expected error after max retries exceeded")
		}
	})

	t.Run("SSRF prevention rejects file:// URL", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "ssrf.bin")

		cfg := onnx.DefaultDownloadConfig() // SSRF check enabled by default
		dl := onnx.NewModelDownloader(cfg)

		err := dl.DownloadFile("file:///etc/passwd", destPath, "", 0)
		if err == nil {
			t.Error("expected error for file:// URL scheme")
		}
	})

	t.Run("SSRF prevention rejects localhost when enabled", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "ssrf.bin")

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, "should not reach here") //nolint:errcheck
		}))
		defer srv.Close()

		cfg := onnx.DefaultDownloadConfig() // SSRF check enabled by default
		dl := onnx.NewModelDownloader(cfg)

		err := dl.DownloadFile(srv.URL, destPath, "", 0)
		if err == nil {
			t.Error("expected error for localhost URL (SSRF prevention)")
		}
	})

	t.Run("Download allows external URLs when SSRF check is off", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "external.bin")

		serverContent := []byte("external content")
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write(serverContent) //nolint:errcheck
		}))
		defer srv.Close()

		cfg := testConfig() // SSRF check disabled for localhost testing
		dl := onnx.NewModelDownloader(cfg)

		if err := dl.DownloadFile(srv.URL, destPath, "", int64(len(serverContent))); err != nil {
			t.Fatalf("DownloadFile should allow URLs when SSRF check is off: %v", err)
		}
	})
}
