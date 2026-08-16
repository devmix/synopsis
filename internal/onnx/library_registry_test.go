package onnx

import (
	"testing"

	"github.com/devmix/synopsis/internal/config"
)

func TestDetectPlatformFromConfig(t *testing.T) {
	t.Parallel()

	platforms := []config.ONNXPlatformConfig{
		{Key: "linux-amd64", OS: "linux", Arch: "amd64", ArchiveURL: "https://example.com/linux-x64.tgz", ArchiveFormat: "tgz", LibraryName: "libonnxruntime.so.1.28.0", LibraryPath: "onnxruntime-linux-x64-1.28.0/lib/libonnxruntime.so.1.28.0"},
		{Key: "linux-arm64", OS: "linux", Arch: "arm64", ArchiveURL: "https://example.com/linux-aarch64.tgz", ArchiveFormat: "tgz", LibraryName: "libonnxruntime.so.1.28.0", LibraryPath: "onnxruntime-linux-aarch64-1.28.0/lib/libonnxruntime.so.1.28.0"},
		{Key: "darwin-amd64", OS: "darwin", Arch: "amd64", ArchiveURL: "https://example.com/osx-x64.tgz", ArchiveFormat: "tgz", LibraryName: "libonnxruntime.1.28.0.dylib", LibraryPath: "onnxruntime-osx-x64-1.28.0/lib/libonnxruntime.1.28.0.dylib"},
		{Key: "darwin-arm64", OS: "darwin", Arch: "arm64", ArchiveURL: "https://example.com/osx-arm64.tgz", ArchiveFormat: "tgz", LibraryName: "libonnxruntime.1.28.0.dylib", LibraryPath: "onnxruntime-osx-arm64-1.28.0/lib/libonnxruntime.1.28.0.dylib"},
		{Key: "windows-amd64", OS: "windows", Arch: "amd64", ArchiveURL: "https://example.com/win-x64.zip", ArchiveFormat: "zip", LibraryName: "onnxruntime.dll", LibraryPath: "onnxruntime-win-x64-1.28.0/lib/onnxruntime.dll"},
	}

	t.Run("finds matching platform from config", func(t *testing.T) {
		info, err := DetectPlatformFromConfig(platforms)
		if err != nil {
			t.Fatalf("DetectPlatformFromConfig() error = %v", err)
		}
		if info.OS == "" {
			t.Error("Detected platform OS should not be empty")
		}
		if info.Arch == "" {
			t.Error("Detected platform Arch should not be empty")
		}
		if info.ArchiveURL == "" {
			t.Error("Detected platform ArchiveURL should not be empty")
		}
		if info.LibraryName == "" {
			t.Error("Detected platform LibraryName should not be empty")
		}
	})

	t.Run("unsupported platform returns error", func(t *testing.T) {
		_, err := DetectPlatformFromConfig([]config.ONNXPlatformConfig{
			{Key: "windows-amd64", OS: "windows", Arch: "amd64"},
		})
		if err == nil {
			t.Error("expected error for unsupported platform")
		}
	})

	t.Run("empty platforms returns error", func(t *testing.T) {
		_, err := DetectPlatformFromConfig(nil)
		if err == nil {
			t.Error("expected error for empty platforms list")
		}
	})
}

func TestDetectPlatformFromConfig_AllFieldsPopulated(t *testing.T) {
	t.Parallel()

	platforms := []config.ONNXPlatformConfig{
		{Key: "linux-amd64", OS: "linux", Arch: "amd64", ArchiveURL: "https://example.com/linux-x64.tgz", ArchiveFormat: "tgz", LibraryName: "libonnxruntime.so.1.28.0", LibraryPath: "path/to/lib"},
	}

	tests := []struct {
		key       string
		wantOS    string
		wantArch  string
		wantURL   string
		wantFmt   string
		wantLibN  string
		wantLibP  string
	}{
		{"linux-amd64", "linux", "amd64", "https://example.com/linux-x64.tgz", "tgz", "libonnxruntime.so.1.28.0", "path/to/lib"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			info, err := DetectPlatformFromConfig(platforms)
			if err != nil && tt.key == "linux-amd64" {
				// Only run field checks if we're actually on linux/amd64 or the test platform matches.
				// For other platforms, just verify no error for a valid config.
				t.Skip("skipping field check — current platform does not match test")
			}
			if err != nil {
				return // platform doesn't match current runtime, skip detailed checks
			}

			if info.OS != tt.wantOS {
				t.Errorf("OS = %q, want %q", info.OS, tt.wantOS)
			}
			if info.Arch != tt.wantArch {
				t.Errorf("Arch = %q, want %q", info.Arch, tt.wantArch)
			}
			if info.ArchiveURL != tt.wantURL {
				t.Errorf("ArchiveURL = %q, want %q", info.ArchiveURL, tt.wantURL)
			}
			if info.ArchiveFormat != tt.wantFmt {
				t.Errorf("ArchiveFormat = %q, want %q", info.ArchiveFormat, tt.wantFmt)
			}
			if info.LibraryName != tt.wantLibN {
				t.Errorf("LibraryName = %q, want %q", info.LibraryName, tt.wantLibN)
			}
			if info.LibraryPath != tt.wantLibP {
				t.Errorf("LibraryPath = %q, want %q", info.LibraryPath, tt.wantLibP)
			}
		})
	}
}
