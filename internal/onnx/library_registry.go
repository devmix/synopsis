// Package onnx provides ONNX Runtime library management for auto-download and installation.
package onnx

import (
	"fmt"
	"runtime"

	"github.com/devmix/synopsis/internal/config"
)

// PlatformInfo contains platform-specific information for ONNX Runtime.
type PlatformInfo struct {
	OS            string
	Arch          string
	ArchiveURL    string
	ArchiveFormat string // "zip" or "tgz"
	LibraryName   string
	LibraryPath   string // path within archive to the library
}

// DetectPlatformFromConfig returns the PlatformInfo for the current system from config platforms.
func DetectPlatformFromConfig(platforms []config.ONNXPlatformConfig) (PlatformInfo, error) {
	key := fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
	for _, p := range platforms {
		if p.Key == key {
			return PlatformInfo{
				OS:            p.OS,
				Arch:          p.Arch,
				ArchiveURL:    p.ArchiveURL,
				ArchiveFormat: p.ArchiveFormat,
				LibraryName:   p.LibraryName,
				LibraryPath:   p.LibraryPath,
			}, nil
		}
	}
	return PlatformInfo{}, fmt.Errorf("unsupported platform: %s-%s", runtime.GOOS, runtime.GOARCH)
}
