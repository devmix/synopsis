package onnx

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/schollz/progressbar/v3"
)

// DownloadConfig holds settings for model file downloading.
type DownloadConfig struct {
	MaxRetries       int           // number of retry attempts on failure, default 3
	RetryDelay       time.Duration // delay between retries, default 2s
	Timeout          time.Duration // HTTP request timeout, default 10m
	UserAgent        string        // custom User-Agent header
	SkipSSRFCheck    bool          // disable SSRF protection (for testing only)
}

// DefaultDownloadConfig returns a DownloadConfig with sensible defaults.
func DefaultDownloadConfig() DownloadConfig {
	return DownloadConfig{
		MaxRetries: 3,
		RetryDelay: 2 * time.Second,
		Timeout:    10 * time.Minute,
		UserAgent:  "synopsis/0.1.0",
	}
}

// ModelDownloader handles downloading model files with progress reporting and checksum verification.
type ModelDownloader struct {
	cfg DownloadConfig
}

// NewModelDownloader creates a downloader with the given configuration.
func NewModelDownloader(cfg DownloadConfig) *ModelDownloader {
	return &ModelDownloader{cfg: cfg}
}

// DownloadFile downloads a file from url to destPath, showing a progress bar.
// It verifies the checksum if one is provided in expectedChecksum (format "sha256:hex").
func (d *ModelDownloader) DownloadFile(urlStr, destPath, expectedChecksum string, totalSize int64) error {
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return fmt.Errorf("create download dir %s: %w", filepath.Dir(destPath), err)
	}

	for attempt := 0; attempt <= d.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(d.cfg.RetryDelay)
		}

		err := d.doDownload(urlStr, destPath, totalSize)
		if err == nil {
			break
		}

		if attempt == d.cfg.MaxRetries {
			return fmt.Errorf("download %s after %d attempts: %w", urlStr, d.cfg.MaxRetries+1, err)
		}
	}

	if expectedChecksum != "" {
		if err := d.verifyChecksum(destPath, expectedChecksum); err != nil {
			return fmt.Errorf("checksum verify %s: %w", destPath, err)
		}
	}

	return nil
}

func (d *ModelDownloader) doDownload(rawURL, destPath string, totalSize int64) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	if parsedURL.Scheme != "https" && parsedURL.Scheme != "http" {
		return fmt.Errorf("invalid URL scheme %q (only http/https allowed)", parsedURL.Scheme)
	}

	if !d.cfg.SkipSSRFCheck && isPrivateHost(parsedURL.Hostname()) {
		return fmt.Errorf("access to private address denied: %s", parsedURL.Hostname())
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", d.cfg.UserAgent)

	client := &http.Client{Timeout: d.cfg.Timeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP GET: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %s for %s", resp.Status, rawURL)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file %s: %w", destPath, err)
	}
	defer f.Close() //nolint:errcheck

	actualSize := resp.ContentLength
	if actualSize <= 0 && totalSize > 0 {
		actualSize = totalSize
	}

	bar := progressbar.DefaultBytes(actualSize, "downloading")
	writer := io.MultiWriter(f, bar)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		f.Close() //nolint:errcheck
		os.Remove(destPath) //nolint:errcheck
		return fmt.Errorf("copy body: %w", err)
	}

	return f.Close()
}

// isPrivateHost returns true if the hostname resolves to a private, loopback, or link-local address.
func isPrivateHost(hostname string) bool {
	if hostname == "" {
		return false
	}

	ips, err := net.LookupIP(hostname)
	if err != nil {
		return false // can't resolve, allow (may be external DNS)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

func (d *ModelDownloader) verifyChecksum(path, expected string) error {
	if len(expected) < 8 || expected[:7] != "sha256:" {
		return nil // unknown format, skip verification
	}
	expectedHex := expected[7:]

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close() //nolint:errcheck

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	actualHex := fmt.Sprintf("%x", h.Sum(nil))
	if actualHex != expectedHex {
		return fmt.Errorf("sha256 mismatch: got %s, want %s", actualHex, expectedHex)
	}
	return nil
}
