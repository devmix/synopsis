package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/onnx"
)

func runONNXRuntimeCommand(cfgPath string, subArgs []string) {
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
	}

	dataDir := cfg.Paths.DataDir
	if dataDir == "" {
		dataDir = "data"
	}

	onnxCfg, err := config.LoadONNXConfig(cfg.Paths.ONNXConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load ONNX config %s: %v\n", cfg.Paths.ONNXConfigPath, err)
	}

	if len(subArgs) == 0 {
		printONNXRuntimeUsage()
		return
	}

	subCmd := subArgs[0]

	switch subCmd {
	case "install":
		runONNXRuntimeInstall(dataDir, &onnxCfg)
	case "status":
		runONNXRuntimeStatus(dataDir, &onnxCfg)
	case "uninstall":
		runONNXRuntimeUninstall(dataDir, &onnxCfg)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", subCmd)
		printONNXRuntimeUsage()
		os.Exit(1)
	}
}

func loadConfig(cfgPath string) (*config.Config, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()
	return &cfg, nil
}

func runONNXRuntimeInstall(dataDir string, onnxCfg *config.ONNXConfig) {
	fmt.Println("Installing ONNX Runtime library...")

	manager, err := onnx.NewLibraryManager(dataDir, onnxCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if libPath, ok := manager.GetLibraryPath(); ok {
		fmt.Printf("ONNX Runtime %s is already installed at: %s\n", manager.GetVersion(), libPath)
		return
	}

	libPath, err := manager.EnsureLibrary()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ ONNX Runtime %s installed successfully\n", manager.GetVersion())
	fmt.Printf("  Library: %s\n", libPath)
	fmt.Printf("  Cache: %s\n", manager.GetCacheDir())
}

func runONNXRuntimeStatus(dataDir string, onnxCfg *config.ONNXConfig) {
	manager, err := onnx.NewLibraryManager(dataDir, onnxCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("ONNX Runtime Status")
	fmt.Println("===================")
	fmt.Printf("Version:     %s\n", manager.GetVersion())

	libPath, installed := manager.GetLibraryPath()
	if installed {
		fmt.Printf("Status:      Installed\n")
		fmt.Printf("Library:     %s\n", libPath)
		fmt.Printf("Cache:       %s\n", manager.GetCacheDir())
	} else {
		fmt.Printf("Status:      Not installed\n")
		fmt.Printf("Cache:       %s\n", manager.GetCacheDir())
		fmt.Printf("Run:         synopsis onnx-runtime install\n")
	}

	fmt.Println("\nSupported Platforms:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, p := range onnxCfg.Runtime.Platforms {
		fmt.Fprintf(w, "  %s\t%s\n", p.Key, p.LibraryName) //nolint:errcheck
	}
	w.Flush() //nolint:errcheck
}

func runONNXRuntimeUninstall(dataDir string, onnxCfg *config.ONNXConfig) {
	manager, err := onnx.NewLibraryManager(dataDir, onnxCfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	_, installed := manager.GetLibraryPath()
	if !installed {
		fmt.Println("ONNX Runtime is not installed")
		return
	}

	if err := manager.UninstallLibrary(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ ONNX Runtime uninstalled successfully")
}

func printONNXRuntimeUsage() {
	fmt.Println(`ONNX Runtime Management

Usage:
  synopsis onnx-runtime <command>

Commands:
  install     Install ONNX Runtime library
  status      Show installation status
  uninstall   Remove installed library

Examples:
  synopsis onnx-runtime install
  synopsis onnx-runtime status
  synopsis onnx-runtime uninstall`)
}
