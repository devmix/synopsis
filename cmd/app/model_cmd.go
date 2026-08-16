// Package main implements the entry point for the Synopsis RAG service.
package main

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/embedding"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/onnx"
	"github.com/devmix/synopsis/internal/utils"
)

// modelManagerFactory is the factory function for creating ModelManager instances.
// It can be overridden in tests to inject mock managers.
var modelManagerFactory = func(dataDir string, onnxCfg *config.ONNXConfig) (*onnx.ModelManager, error) {
	return onnx.NewModelManager(dataDir, onnxCfg)
}

// PrintModelList outputs a table of available models with installation status.
func printModelList(models []config.ModelInfo, cache *onnx.ModelCache, modelsDir string) {
	fmt.Println("Available Models:")
	fmt.Println(strings.Repeat("-", 90))
	fmt.Printf("%-25s %-10s %-8s %s\n", "NAME", "VERSION", "DIM", "STATUS")
	fmt.Println(strings.Repeat("-", 90))

	for _, m := range models {
		status := "not installed"
		if cache.IsInstalled(m.Name) && utils.DirExists(filepath.Join(modelsDir, m.Name)) {
			status = "installed ✓"
		}
		fmt.Printf("%-25s %-10s %-8d %s\n", m.DisplayName, m.Version, m.VectorDim, status)
	}
	fmt.Println()
}

// printModelInfo outputs detailed information about a single model.
func printModelInfo(m config.ModelInfo, installed bool, cache *onnx.ModelCache, modelsDir string) {
	fmt.Printf("Name:         %s\n", m.Name)
	fmt.Printf("Display Name: %s\n", m.DisplayName)
	fmt.Printf("Description:  %s\n", m.Description)
	fmt.Printf("Version:      %s\n", m.Version)
	fmt.Printf("Vector Dim:   %d\n", m.VectorDim)
	fmt.Printf("Source:       %s\n", m.Source)
	if m.Repo != "" {
		fmt.Printf("Repository:   %s\n", m.Repo)
	}

	status := "Not installed"
	if installed {
		status = "Installed ✓"
		if info, ok := cache.GetModelInfo(m.Name); ok {
			fmt.Printf("Installed At: %s\n", info.InstalledAt.Format("2006-01-02 15:04:05"))
		}
	}
	fmt.Printf("Status:       %s\n", status)

	if len(m.Files) > 0 {
		fmt.Println("\nFiles:")
		for _, f := range m.Files {
			sizeStr := "unknown"
			if f.SizeBytes > 0 {
				sizeStr = "~" + utils.HumanFileSize(f.SizeBytes)
			}
			checksumStr := ""
			if f.Checksum != "" {
				checksumStr = " (checksum verified)"
			}
			fmt.Printf("  - %-25s %s%s\n", f.Name, sizeStr, checksumStr)
		}
	}
	fmt.Println()
}

// runModelList executes the `model list` CLI command.
func runModelList(dataDir string, onnxCfg *config.ONNXConfig) error {
	mm, err := modelManagerFactory(dataDir, onnxCfg)
	if err != nil {
		return fmt.Errorf("init model manager: %w", err)
	}

	models, err := mm.ListModels()
	if err != nil {
		return fmt.Errorf("list models: %w", err)
	}

	printModelList(models, mm.Cache(), mm.GetModelsDir())
	return nil
}

// runModelDownload executes the `model download <name>` CLI command.
func runModelDownload(dataDir string, modelName string, onnxCfg *config.ONNXConfig) error {
	mm, err := modelManagerFactory(dataDir, onnxCfg)
	if err != nil {
		return fmt.Errorf("init model manager: %w", err)
	}

	if modelName == "" {
		modelName = mm.Registry().Default()
		fmt.Printf("No model specified, using default: %s\n\n", modelName)
	}

	info, ok := mm.Registry().Get(modelName)
	if !ok {
		return fmt.Errorf("model %q not found in registry", modelName)
	}

	fmt.Printf("Downloading %s (%s)...\n\n", info.DisplayName, info.Name)

	if err := mm.DownloadModel(modelName); err != nil {
		return fmt.Errorf("download model: %w", err)
	}

	modelDir := filepath.Join(mm.GetModelsDir(), modelName)
	fmt.Printf("\n✓ Model installed at: %s\n", modelDir)
	return nil
}

// runModelDelete executes the `model delete <name>` CLI command.
func runModelDelete(dataDir string, modelName string, onnxCfg *config.ONNXConfig) error {
	mm, err := modelManagerFactory(dataDir, onnxCfg)
	if err != nil {
		return fmt.Errorf("init model manager: %w", err)
	}

	if modelName == "" {
		return fmt.Errorf("model name is required")
	}

	if _, ok := mm.Registry().Get(modelName); !ok {
		return fmt.Errorf("model %q not found in registry", modelName)
	}

	if err := mm.DeleteModel(modelName); err != nil {
		return fmt.Errorf("delete model: %w", err)
	}

	fmt.Printf("✓ Model %s deleted\n", modelName)
	return nil
}

// runModelInfo executes the `model info <name>` CLI command.
func runModelInfo(dataDir string, modelName string, onnxCfg *config.ONNXConfig) error {
	mm, err := modelManagerFactory(dataDir, onnxCfg)
	if err != nil {
		return fmt.Errorf("init model manager: %w", err)
	}

	if modelName == "" {
		modelName = mm.Registry().Default()
	}

	info, ok := mm.Registry().Get(modelName)
	if !ok {
		return fmt.Errorf("model %q not found in registry", modelName)
	}

	_, installed := mm.GetModelPath(modelName)
	printModelInfo(info, installed, mm.Cache(), mm.GetModelsDir())
	return nil
}

// runModelBenchmark executes the `model benchmark [<name>]` CLI command.
// With no name it benchmarks every installed model.
func runModelBenchmark(dataDir string, modelName string, onnxCfg *config.ONNXConfig) error {
	mm, err := modelManagerFactory(dataDir, onnxCfg)
	if err != nil {
		return fmt.Errorf("init model manager: %w", err)
	}

	var targets []config.ModelInfo
	if modelName == "" {
		models, err := mm.ListModels()
		if err != nil {
			return fmt.Errorf("list models: %w", err)
		}
		for _, m := range models {
			if mm.Cache().IsInstalled(m.Name) && utils.DirExists(filepath.Join(mm.GetModelsDir(), m.Name)) {
				targets = append(targets, m)
			}
		}
		if len(targets) == 0 {
			return fmt.Errorf("no installed models found; run 'synopsis model download <name>' first")
		}
	} else {
		info, ok := mm.Registry().Get(modelName)
		if !ok {
			return fmt.Errorf("model %q not found in registry", modelName)
		}
		targets = []config.ModelInfo{info}
	}

	log, err := logger.New(logger.Options{Level: "error"})
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}

	fmt.Println("Embedding Benchmark")
	fmt.Println(strings.Repeat("-", 70))
	rt := embedding.DetectRuntime(dataDir)
	cpu := rt.CPUModel
	if cpu == "" {
		cpu = "unknown CPU"
	}
	mem := ""
	if rt.MemTotalBytes > 0 {
		mem = fmt.Sprintf(", ~%s RAM", utils.HumanFileSize(int64(rt.MemTotalBytes)))
	}
	fmt.Printf("Hardware: %s, %d logical CPUs%s\n", cpu, rt.NumCPU, mem)
	if rt.ONNXRuntimeVersion != "" {
		fmt.Println("ONNX Runtime:", rt.ONNXRuntimeVersion)
	}

	ctx := context.Background()
	for _, m := range targets {
		modelDir, installed := mm.GetModelPath(m.Name)
		if !installed {
			return fmt.Errorf("model %s is not installed; run 'synopsis model download %s' first", m.Name, m.Name)
		}
		modelPath := filepath.Join(modelDir, "model.onnx")

		res, err := embedding.BenchmarkModel(ctx, m.Name, modelPath, m.VectorDim, dataDir, log, onnxCfg, embedding.BenchOptions{})
		if err != nil {
			return fmt.Errorf("benchmark %s: %w", m.Name, err)
		}

		fmt.Printf("\n%s (dim=%d)\n", m.DisplayName, res.VectorDim)
		fmt.Printf("  production (padded to seq=512): %s\n", res.Production)
		lengths := make([]int, 0, len(res.Natural))
		for n := range res.Natural {
			lengths = append(lengths, n)
		}
		sort.Ints(lengths)
		for _, n := range lengths {
			st := res.Natural[n]
			fmt.Printf("  natural seq=%-4d: %-28s (%.0f tok/s)\n", n, st, float64(n)/(st.MedianMS/1000.0))
		}
	}
	fmt.Println()
	return nil
}

// printModelUsage outputs help text for the model subcommand.
func printModelUsage() {
	fmt.Println("Usage: synopsis model <subcommand> [args]")
	fmt.Println()
	fmt.Println("Subcommands:")
	fmt.Println("  list                  List available and installed models")
	fmt.Println("  download [<name>]     Download a model (default: bge-m3-int8)")
	fmt.Println("  delete <name>         Delete an installed model")
	fmt.Println("  info [<name>]         Show model details (default: bge-m3-int8)")
	fmt.Println("  benchmark [<name>]    Measure embedding speed of installed models (all if omitted)")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  synopsis model list")
	fmt.Println("  synopsis model download bge-m3-int8")
	fmt.Println("  synopsis model delete bge-small-en-v1.5")
	fmt.Println("  synopsis model info paraphrase-multilingual-MiniLM-L12-v2")
	fmt.Println("  synopsis model benchmark bge-m3-int8")
}
