package onnx

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/devmix/synopsis/internal/config"
	"github.com/schollz/progressbar/v3"
)

// LibraryManager handles ONNX Runtime library installation.
type LibraryManager struct {
	cacheMgr   *LibraryCacheManager
	platform   PlatformInfo
	version    string
	httpClient *http.Client
}

// NewLibraryManager creates a new library manager with platform info and version from config.
func NewLibraryManager(dataDir string, cfg *config.ONNXConfig) (*LibraryManager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("onnx config is nil")
	}

	platform, err := DetectPlatformFromConfig(cfg.Runtime.Platforms)
	if err != nil {
		return nil, fmt.Errorf("detect platform: %w", err)
	}

	return &LibraryManager{
		cacheMgr: NewLibraryCacheManager(dataDir),
		platform: platform,
		version:  cfg.Runtime.Version,
		httpClient: &http.Client{
			Timeout: 300 * time.Second, // 5 minutes for large downloads
		},
	}, nil
}

// EnsureLibrary ensures ONNX Runtime library is installed.
// Deprecated: Use EnsureLibraryContext instead to support cancellation and timeouts.
func (m *LibraryManager) EnsureLibrary() (string, error) {
	return m.EnsureLibraryContext(context.Background())
}

// EnsureLibraryContext ensures ONNX Runtime library is installed with context support.
func (m *LibraryManager) EnsureLibraryContext(ctx context.Context) (string, error) {
	// Check if already installed.
	if installed, libPath := m.cacheMgr.IsInstalled(m.version); installed {
		return libPath, nil
	}

	// Download and install with context for cancellation/timeout support.
	if err := m.DownloadLibraryContext(ctx); err != nil {
		return "", fmt.Errorf("download library: %w", err)
	}

	// Verify installation.
	if installed, libPath := m.cacheMgr.IsInstalled(m.version); installed {
		return libPath, nil
	}

	return "", fmt.Errorf("library installation verification failed")
}

// DownloadLibrary downloads and extracts ONNX Runtime.
// Deprecated: Use DownloadLibraryContext instead to support cancellation and timeouts.
func (m *LibraryManager) DownloadLibrary() error {
	return m.DownloadLibraryContext(context.Background())
}

// DownloadLibraryContext downloads and extracts ONNX Runtime with context support.
func (m *LibraryManager) DownloadLibraryContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("context must not be nil")
	}

	// Create download directory.
	downloadDir := m.cacheMgr.GetCacheDir()
	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return fmt.Errorf("create download dir: %w", err)
	}

	// Download archive with context for cancellation support.
	archivePath := filepath.Join(downloadDir, "onnxruntime-archive."+m.platform.ArchiveFormat)
	if err := m.downloadArchive(ctx, m.platform.ArchiveURL, archivePath); err != nil {
		return fmt.Errorf("download archive: %w", err)
	}
	defer os.Remove(archivePath) // Clean up archive after extraction.

	// Extract archive.
	if err := m.extractArchive(archivePath, downloadDir); err != nil {
		return fmt.Errorf("extract archive: %w", err)
	}

	// Copy library to cache.
	librarySrc := filepath.Join(downloadDir, m.platform.LibraryPath)
	libraryDst := filepath.Join(downloadDir, m.platform.LibraryName)

	if err := m.copyFile(librarySrc, libraryDst); err != nil {
		return fmt.Errorf("copy library: %w", err)
	}

	// Create symlink for versioned library.
	symlinkPath := filepath.Join(downloadDir, strings.TrimSuffix(m.platform.LibraryName, "."+m.version))
	if _, err := os.Lstat(symlinkPath); os.IsNotExist(err) {
		_ = os.Symlink(m.platform.LibraryName, symlinkPath) // Ignore symlink errors.
	}

	// Update cache.
	cache := &LibraryCache{
		Version:     m.version,
		LibraryPath: libraryDst,
		InstallTime: time.Now().Format(time.RFC3339),
		Platform:    fmt.Sprintf("%s-%s", m.platform.OS, m.platform.Arch),
	}
	if err := m.cacheMgr.Save(cache); err != nil {
		return fmt.Errorf("save cache: %w", err)
	}

	return nil
}

// downloadArchive downloads a file with progress bar.
func (m *LibraryManager) downloadArchive(ctx context.Context, url, dest string) error {
	if ctx == nil {
		return fmt.Errorf("context must not be nil")
	}

	if m.httpClient == nil {
		return fmt.Errorf("http client is not initialized")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	// Create file.
	file, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer file.Close()

	// Create progress bar.
	bar := progressbar.DefaultBytes(
		resp.ContentLength,
		"Downloading ONNX Runtime",
	)

	// Download with progress.
	if _, err := io.Copy(io.MultiWriter(file, bar), resp.Body); err != nil {
		return fmt.Errorf("download: %w", err)
	}

	return nil
}

// extractArchive extracts ZIP or TAR.GZ archive.
func (m *LibraryManager) extractArchive(archivePath, destDir string) error {
	switch m.platform.ArchiveFormat {
	case "zip":
		return m.extractZip(archivePath, destDir)
	case "tgz":
		return m.extractTgz(archivePath, destDir)
	default:
		return fmt.Errorf("unsupported archive format: %s", m.platform.ArchiveFormat)
	}
}

// extractZip extracts a ZIP archive.
func (m *LibraryManager) extractZip(archivePath, destDir string) error {
	zipReader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open zip: %w", err)
	}
	defer zipReader.Close()

	for _, f := range zipReader.File {
		// Skip directories.
		if f.FileInfo().IsDir() {
			continue
		}

		// Extract file.
		destPath := filepath.Join(destDir, f.Name)
		if err := m.extractZipFile(f, destPath); err != nil {
			return fmt.Errorf("extract file %s: %w", f.Name, err)
		}
	}

	return nil
}

func (m *LibraryManager) extractZipFile(f *zip.File, destPath string) error {
	srcFile, err := f.Open()
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer srcFile.Close()

	// Create destination directory.
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, srcFile)
	return err
}

// extractTgz extracts a TAR.GZ archive.
func (m *LibraryManager) extractTgz(archivePath, destDir string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open tgz: %w", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("create gzip reader: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar: %w", err)
		}

		// Skip directories.
		if header.Typeflag == tar.TypeDir {
			continue
		}

		// Extract file.
		destPath := filepath.Join(destDir, header.Name)
		if err := m.extractTarFile(tarReader, destPath); err != nil {
			return fmt.Errorf("extract file %s: %w", header.Name, err)
		}
	}

	return nil
}

func (m *LibraryManager) extractTarFile(src io.Reader, destPath string) error {
	// Create destination directory.
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	destFile, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, src)
	return err
}

func (m *LibraryManager) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// GetLibraryPath returns the path to installed library.
func (m *LibraryManager) GetLibraryPath() (string, bool) {
	installed, libPath := m.cacheMgr.IsInstalled(m.version)
	if !installed {
		return "", false
	}
	return libPath, true
}

// UninstallLibrary removes the installed library and its metadata.
func (m *LibraryManager) UninstallLibrary() error {
	installed, _ := m.cacheMgr.IsInstalled(m.version)
	if !installed {
		return nil // Already uninstalled
	}
	// The library files and .cache.json live in the cache directory; remove it entirely.
	if err := os.RemoveAll(m.cacheMgr.GetCacheDir()); err != nil {
		return fmt.Errorf("remove library: %w", err)
	}
	return nil
}

// GetCacheDir returns the cache directory.
func (m *LibraryManager) GetCacheDir() string {
	return m.cacheMgr.GetCacheDir()
}

// GetVersion returns the ONNX Runtime version.
func (m *LibraryManager) GetVersion() string {
	return m.version
}

// SetHTTPClient replaces the HTTP client used for downloads. Primarily useful for testing with mock servers.
func (m *LibraryManager) SetHTTPClient(client *http.Client) {
	m.httpClient = client
}
