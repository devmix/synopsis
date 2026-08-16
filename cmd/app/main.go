// Package main implements the entry point for the Synopsis RAG service.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/devmix/synopsis/internal/config"
)

const version = "0.1.0-dev"

// defaultPreset is the default configuration preset name (config.{preset}.yaml).
const defaultPreset = "default"

// resolveConfigPath returns the effective config path:
//   - if --config is set, return it as-is
//   - otherwise search relative to the executable, then CWD using the preset name
func resolveConfigPath(cliPath string, preset string) string {
	if cliPath != "" {
		return cliPath
	}

	exe := ""
	exePath, err := os.Executable()
	if err == nil {
		resolved, err2 := filepath.EvalSymlinks(exePath)
		if err2 == nil {
			exe = resolved
		}
	}

	return resolveConfigCandidates(exe, preset)
}

// resolveConfigCandidates searches for a config file using the given executable path and preset.
// Candidate order: exeDir/configs → exeDir → parent(configs) → parent → CWD/configs → CWD → fallback.
func resolveConfigCandidates(exe string, preset string) string {
	configName := fmt.Sprintf("config.%s.yaml", preset)

	// Try relative to executable first.
	if exe != "" {
		exeDir := filepath.Dir(exe)
		candidates := []string{
			filepath.Join(exeDir, "configs", configName),
			filepath.Join(exeDir, configName),
			filepath.Join(filepath.Dir(exeDir), configName),
			filepath.Join(filepath.Dir(exeDir), "configs", configName),
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}

	// Fallback to CWD.
	cwd, err := os.Getwd()
	if err == nil {
		cwdCandidates := []string{
			filepath.Join(cwd, "configs", configName),
			filepath.Join(cwd, configName),
		}
		for _, candidate := range cwdCandidates {
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}

	// Ultimate fallback — let config.Load handle the error.
	return fmt.Sprintf("configs/config.%s.yaml", preset)
}

func main() {
	// Global flags must appear before the subcommand.
	cliConfig := flag.String("config", "", "path to configuration file (default: auto-search)")
	preset := flag.String("preset", defaultPreset, "configuration preset name (config.{preset}.yaml)")
	dbPath := flag.String("db", "", "path to SQLite database file (overrides config)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("synopsis %s\n", version)
		os.Exit(0)
	}

	// Resolve config path: explicit --config > preset-based auto-search.
	cfgPath := resolveConfigPath(*cliConfig, *preset)

	// Subcommand dispatch.
	if flag.NArg() == 0 {
		printUsage()
		os.Exit(1)
	}

	subCmd := flag.Arg(0)
	subArgs := flag.Args()[1:]

	switch subCmd {
	case "sync":
		var rebuild bool
		var autoRebuildVectors bool
		cmdFlags := flag.NewFlagSet("sync", flag.ExitOnError)
		cmdFlags.BoolVar(&rebuild, "rebuild", false, "clear all existing data before re-indexing")
		cmdFlags.BoolVar(&autoRebuildVectors, "auto-rebuild-vectors", false, "automatically rebuild vectors on dimension mismatch")
		cmdFlags.Usage = func() {
			fmt.Fprintln(os.Stderr, "usage: synopsis sync [--rebuild] [--auto-rebuild-vectors]")
			cmdFlags.PrintDefaults()
		}
		if err := cmdFlags.Parse(subArgs); err != nil {
			os.Exit(1)
		}
		runSync(cfgPath, *dbPath, rebuild, autoRebuildVectors)
	case "serve":
		noInitialSync := false
		port := 0
		autoRebuildVectors := false
		cmdFlags := flag.NewFlagSet("serve", flag.ExitOnError)
		cmdFlags.BoolVar(&noInitialSync, "no-initial-sync", false, "skip the full source scan on startup")
		cmdFlags.IntVar(&port, "port", 0, "HTTP listen port (overrides server.port config; default 8080)")
		cmdFlags.BoolVar(&autoRebuildVectors, "auto-rebuild-vectors", false, "automatically rebuild vectors on dimension mismatch")
		cmdFlags.Usage = func() {
			fmt.Fprintln(os.Stderr, "usage: synopsis serve [--no-initial-sync] [--port N] [--auto-rebuild-vectors]")
			cmdFlags.PrintDefaults()
		}
		if err := cmdFlags.Parse(subArgs); err != nil {
			os.Exit(1)
		}
		runServe(cfgPath, *dbPath, noInitialSync, port, autoRebuildVectors)
	case "model":
		runModelCommand(cfgPath, subArgs)
	case "onnx-runtime":
		runONNXRuntimeCommand(cfgPath, subArgs)
	default:
		fmt.Fprintf(os.Stderr, "error: unknown subcommand %q\n", subCmd)
		printUsage()
		os.Exit(1)
	}
}

// printUsage prints the top-level usage message.
func printUsage() {
	fmt.Fprintln(os.Stderr, "Synopsis RAG service")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  synopsis [--config PATH] [--preset NAME] [--db PATH] <command>")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  serve          start MCP server with auto-update (initial sync + file watching)")
	fmt.Fprintln(os.Stderr, "  sync           force a full re-index of all sources and exit")
	fmt.Fprintln(os.Stderr, "  model          manage embedding models (list, download, delete, info, benchmark)")
	fmt.Fprintln(os.Stderr, "  onnx-runtime   manage the ONNX runtime bundled with the binary")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Flags:")
	flag.PrintDefaults()
}

func runModelCommand(cfgPath string, subArgs []string) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config %s: %v\n", cfgPath, err)
		os.Exit(1)
	}
	cfg.ApplyDefaults()

	dataDir := cfg.Paths.DataDir
	if dataDir == "" {
		dataDir = "data"
	}

	onnxCfg, err := config.LoadONNXConfig(cfg.Paths.ONNXConfigPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load ONNX config %s: %v\n", cfg.Paths.ONNXConfigPath, err)
		os.Exit(1)
	}

	if len(subArgs) == 0 {
		printModelUsage()
		return
	}

	subCmd := subArgs[0]
	modelName := ""
	if len(subArgs) > 1 {
		modelName = subArgs[1]
	}

	switch subCmd {
	case "list":
		if err := runModelList(dataDir, &onnxCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "download":
		if err := runModelDownload(dataDir, modelName, &onnxCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "delete":
		if err := runModelDelete(dataDir, modelName, &onnxCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "info":
		if err := runModelInfo(dataDir, modelName, &onnxCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "benchmark":
		if err := runModelBenchmark(dataDir, modelName, &onnxCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown model subcommand: %s\n", subCmd)
		printModelUsage()
		os.Exit(1)
	}
}
