package embedding

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/yalue/onnxruntime_go"
)

// BenchOptions controls a benchmark run. Zero values use defaults:
// 3 warmup runs, 9 measured runs, sequence lengths 32/128/512.
type BenchOptions struct {
	Warmup  int
	Runs    int
	SeqLens []int
}

func (o *BenchOptions) withDefaults() {
	if o.Warmup <= 0 {
		o.Warmup = 3
	}
	if o.Runs <= 0 {
		o.Runs = 9
	}
	if len(o.SeqLens) == 0 {
		o.SeqLens = []int{32, 128, 512}
	}
}

// BenchStats aggregates measured latencies in milliseconds.
type BenchStats struct {
	MedianMS float64
	MinMS    float64
	MaxMS    float64
	Runs     int
}

func (s BenchStats) String() string {
	return fmt.Sprintf("median %.1f ms/text [min %.1f, max %.1f], %d runs", s.MedianMS, s.MinMS, s.MaxMS, s.Runs)
}

// ModelBenchResult holds the benchmark outcome for a single model.
type ModelBenchResult struct {
	Name      string
	VectorDim int
	// Production is the full provider path used by the service at ingest and
	// search time; the tokenizer pads every input to maxLength (512).
	Production BenchStats
	// Natural maps sequence length -> stats measured through a direct ONNX
	// session without padding, showing how latency scales with real text size.
	Natural map[int]BenchStats
}

func medianMS(xs []float64) float64 {
	s := append([]float64(nil), xs...)
	sort.Float64s(s)
	m := len(s) / 2
	if len(s)%2 == 1 {
		return s[m]
	}
	return (s[m-1] + s[m]) / 2
}

func statsFromMS(xs []float64) BenchStats {
	if len(xs) == 0 {
		return BenchStats{}
	}
	min := xs[0]
	max := xs[0]
	for _, x := range xs[1:] {
		if x < min {
			min = x
		}
		if x > max {
			max = x
		}
	}
	return BenchStats{MedianMS: medianMS(xs), MinMS: min, MaxMS: max, Runs: len(xs)}
}

// RuntimeInfo describes the machine a benchmark ran on.
type RuntimeInfo struct {
	CPUModel           string
	NumCPU             int
	MemTotalBytes      uint64
	ONNXRuntimeVersion string
}

// DetectRuntime fills in CPU model, logical CPU count and total memory from
// /proc (Linux); fields stay zero/empty when unavailable.
func DetectRuntime(dataDir string) RuntimeInfo {
	info := RuntimeInfo{NumCPU: runtime.NumCPU()}

	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok || strings.TrimSpace(key) != "model name" {
				continue
			}
			info.CPUModel = strings.TrimSpace(value)
			break
		}
	}

	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			key, value, ok := strings.Cut(line, ":")
			if !ok || strings.TrimSpace(key) != "MemTotal" {
				continue
			}
			fields := strings.Fields(strings.TrimSpace(value))
			if len(fields) >= 2 && fields[1] == "kB" {
				if kb, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
					info.MemTotalBytes = kb * 1024
				}
			}
			break
		}
	}

	info.ONNXRuntimeVersion = onnxruntimeVersion(dataDir)
	return info
}

func onnxruntimeVersion(dataDir string) string {
	matches, _ := filepath.Glob(filepath.Join(dataDir, "onnxruntime", "onnxruntime-*"))
	for _, m := range matches {
		base := filepath.Base(m)
		if !strings.HasPrefix(base, "onnxruntime-") {
			continue
		}
		fields := strings.Split(strings.TrimPrefix(base, "onnxruntime-"), "-")
		if len(fields) >= 3 {
			return fields[len(fields)-1]
		}
	}
	return ""
}

// benchSampleText is the deterministic text fed to every measured run (~90
// content tokens). A unique suffix per run keeps results out of the embedding
// cache without changing padded length.
const benchSampleText = "The company operates across three regions and provides enterprise analytics tooling to mid-market customers worldwide. The organization was founded in 2014 and has grown steadily through a series of acquisitions, expanding its engineering team and data infrastructure year after year."

// BenchmarkModel measures embedding inference for one installed model:
// first the full provider path (padded to maxLength), then direct ONNX
// sessions at natural sequence lengths without padding. modelPath must point
// to the model.onnx file; tokenizer.json is expected in the same directory.
func BenchmarkModel(ctx context.Context, modelName, modelPath string, vectorDim int, dataDir string, log *logger.Logger, onnxCfg *config.ONNXConfig, opts BenchOptions) (*ModelBenchResult, error) {
	opts.withDefaults()

	cfg := config.LocalEmbedding{
		ModelName: modelName,
		ModelPath: modelPath,
		VectorDim: vectorDim,
	}
	provider, err := NewONNXProvider(cfg, dataDir, log, onnxCfg)
	if err != nil {
		return nil, fmt.Errorf("create ONNX provider for %s: %w", modelName, err)
	}

	res := &ModelBenchResult{Name: modelName, VectorDim: vectorDim, Natural: map[int]BenchStats{}}

	// 1. Production path — the exact code used by sync and search.
	var prodMS []float64
	for i := 0; i < opts.Warmup+opts.Runs; i++ {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("benchmark %s: cancelled: %w", modelName, ctx.Err())
		default:
		}
		text := fmt.Sprintf("%s [sample %d]", benchSampleText, i)
		start := time.Now()
		if _, err := provider.GenerateEmbeddings(ctx, []string{text}); err != nil {
			return nil, fmt.Errorf("generate embeddings (%s): %w", modelName, err)
		}
		dur := float64(time.Since(start).Microseconds()) / 1000.0
		if i >= opts.Warmup {
			prodMS = append(prodMS, dur)
		}
	}
	res.Production = statsFromMS(prodMS)

	// 2. Direct session at natural lengths (no padding).
	graphInputs, graphOutputs, err := onnxruntime_go.GetInputOutputInfo(modelPath)
	if err != nil {
		return res, fmt.Errorf("inspect model I/O (%s): %w", modelName, err)
	}
	inNames, err := inputNamesFor(graphInputs)
	if err != nil {
		return res, fmt.Errorf("unsupported inputs in %s: %w", modelName, err)
	}
	outName, err := selectOutputName(graphOutputs)
	if err != nil {
		return res, fmt.Errorf("unsupported outputs in %s: %w", modelName, err)
	}
	sess, err := onnxruntime_go.NewDynamicAdvancedSession(modelPath, inNames, []string{outName}, nil)
	if err != nil {
		return res, fmt.Errorf("create ONNX session (%s): %w", modelName, err)
	}
	defer sess.Destroy() //nolint:errcheck

	tokenizer, err := NewSugarmeTokenizer(filepath.Dir(modelPath), DefaultMaxLength)
	if err != nil {
		return res, fmt.Errorf("create tokenizer (%s): %w", modelName, err)
	}

	for _, n := range opts.SeqLens {
		if n > DefaultMaxLength {
			return res, fmt.Errorf("sequence length %d exceeds max %d", n, DefaultMaxLength)
		}
		var ms []float64
		for i := 0; i < opts.Warmup+opts.Runs; i++ {
			select {
			case <-ctx.Done():
				return res, fmt.Errorf("benchmark %s: cancelled: %w", modelName, ctx.Err())
			default:
			}
			full, err := tokenizer.Tokenize(fmt.Sprintf("%s [raw %d %d]", benchSampleText, n, i))
			if err != nil {
				return res, fmt.Errorf("tokenize (%s): %w", modelName, err)
			}
			dur, err := rawInfer(sess, inNames, full[:n])
			if err != nil {
				return res, fmt.Errorf("raw infer n=%d (%s): %w", n, modelName, err)
			}
			if i >= opts.Warmup {
				ms = append(ms, dur)
			}
		}
		res.Natural[n] = statsFromMS(ms)
	}

	return res, nil
}

// rawInfer runs one inference through a pre-built dynamic session with the
// given token IDs (no padding) and returns the latency in milliseconds.
func rawInfer(sess *onnxruntime_go.DynamicAdvancedSession, inNames []string, ids []int32) (float64, error) {
	n := len(ids)

	inputIDs := make([]int64, n)
	for i, v := range ids {
		inputIDs[i] = int64(v)
	}
	mask := make([]int64, n)
	for i := range mask {
		mask[i] = 1
	}

	dataForInput := map[string][]int64{
		"input_ids":      inputIDs,
		"attention_mask": mask,
		"token_type_ids": make([]int64, n), // bound only if the model declares it
	}

	tensors := make([]*onnxruntime_go.Tensor[int64], 0, len(inNames))
	values := make([]onnxruntime_go.Value, 0, len(inNames))
	destroyTensors := func() {
		for _, t := range tensors {
			_ = t.Destroy() //nolint:errcheck
		}
	}

	for _, name := range inNames {
		data, ok := dataForInput[name]
		if !ok {
			destroyTensors()
			return 0, fmt.Errorf("unsupported model input %q", name)
		}
		tensor, err := onnxruntime_go.NewTensor(onnxruntime_go.NewShape(1, int64(n)), data)
		if err != nil {
			destroyTensors()
			return 0, fmt.Errorf("create %s tensor: %w", name, err)
		}
		tensors = append(tensors, tensor)
		values = append(values, tensor)
	}

	outputs := []onnxruntime_go.Value{nil} // runtime-allocated; shape varies by model
	start := time.Now()
	err := sess.Run(values, outputs)
	dur := float64(time.Since(start).Microseconds()) / 1000.0
	destroyTensors()
	return dur, err
}
