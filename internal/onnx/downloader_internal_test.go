// Package onnx contains internal tests with access to unexported symbols.
package onnx

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devmix/synopsis/internal/utils"
)

func TestIsPrivateHost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		host     string
		wantTrue bool
	}{
		{
			name:     "localhost is private",
			host:     "localhost",
			wantTrue: true,
		},
		{
			name:     "127.0.0.1 is private",
			host:     "127.0.0.1",
			wantTrue: true,
		},
		{
			name:     "empty host returns false",
			host:     "",
			wantTrue: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPrivateHost(tt.host)
			if got != tt.wantTrue {
				t.Errorf("isPrivateHost(%q) = %v, want %v", tt.host, got, tt.wantTrue)
			}
		})
	}
}

func TestDirExists(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	if !utils.DirExists(tmpDir) {
		t.Error("expected tmpDir to exist")
	}

	if utils.DirExists(filepath.Join(tmpDir, "nonexistent")) {
		t.Error("expected nonexistent path to return false")
	}

	filePath := filepath.Join(tmpDir, "file.txt")
	if err := os.WriteFile(filePath, []byte("test"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if utils.DirExists(filePath) {
		t.Error("expected file path to return false for dirExists")
	}
}

func TestDownloadFile_URLValidation(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "test.bin")

	cfg := DefaultDownloadConfig()
	dl := NewModelDownloader(cfg)

	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "file scheme rejected",
			url:     "file:///etc/passwd",
			wantErr: true,
		},
		{
			name:    "ftp scheme rejected",
			url:     "ftp://example.com/file.bin",
			wantErr: true,
		},
		{
			name:    "empty URL rejected",
			url:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := dl.DownloadFile(tt.url, destPath, "", 0)
			if (err != nil) != tt.wantErr {
				t.Errorf("DownloadFile(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}
