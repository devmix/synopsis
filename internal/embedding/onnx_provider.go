package embedding

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/devmix/synopsis/internal/cache"
	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/logger"
	"github.com/devmix/synopsis/internal/onnx"
	"github.com/yalue/onnxruntime_go"
)

// ONNXProvider generates embeddings using a local ONNX model via onnxruntime.
type ONNXProvider struct {
	session    *onnxruntime_go.DynamicAdvancedSession
	modelName  string
	modelPath  string
	vectorDim  int
	inputNames []string
	cache      *EmbeddingCache
	tokenizer  Tokenizer
	log        *logger.Logger
}

// knownModelInputs lists the input tensors a transformer embedding export may
// declare, in canonical order. token_type_ids is optional (absent in newer
// exports such as BGE-M3).
var knownModelInputs = []string{"input_ids", "attention_mask", "token_type_ids"}

func hasGraphName(graph []onnxruntime_go.InputOutputInfo, name string) bool {
	for _, i := range graph {
		if i.Name == name {
			return true
		}
	}
	return false
}

// inputNamesFor returns the model's declared inputs that are known to the
// provider, in canonical order. It fails if required inputs are missing.
func inputNamesFor(graph []onnxruntime_go.InputOutputInfo) ([]string, error) {
	var names []string
	for _, name := range knownModelInputs {
		if hasGraphName(graph, name) {
			names = append(names, name)
		}
	}
	if !hasGraphName(graph, "input_ids") || !hasGraphName(graph, "attention_mask") {
		return nil, fmt.Errorf("model lacks required inputs input_ids and attention_mask (declared: %v", names)
	}
	return names, nil
}

// selectOutputName picks the embedding output tensor of a model: a pooled
// sentence_embedding when available, otherwise last_hidden_state, otherwise
// the single declared output.
func selectOutputName(graph []onnxruntime_go.InputOutputInfo) (string, error) {
	for _, name := range []string{"sentence_embedding", "last_hidden_state"} {
		if hasGraphName(graph, name) {
			return name, nil
		}
	}
	if len(graph) == 1 {
		return graph[0].Name, nil
	}
	return "", fmt.Errorf("no supported embedding output found (declared: %v", graphNames(graph))
}

func graphNames(graph []onnxruntime_go.InputOutputInfo) []string {
	names := make([]string, len(graph))
	for i, g := range graph {
		names[i] = g.Name
	}
	return names
}

// NewONNXProvider creates an ONNX provider that loads the model at cfg.ModelPath.
// dataDir is used to locate or download the ONNX Runtime library (stored under dataDir/onnxruntime).
func NewONNXProvider(cfg config.LocalEmbedding, dataDir string, log *logger.Logger, onnxCfg *config.ONNXConfig) (*ONNXProvider, error) {
	return NewONNXProviderContext(context.Background(), cfg, dataDir, log, onnxCfg)
}

// NewONNXProviderContext creates an ONNX provider with context support for library download cancellation.
func NewONNXProviderContext(ctx context.Context, cfg config.LocalEmbedding, dataDir string, log *logger.Logger, onnxCfg *config.ONNXConfig) (*ONNXProvider, error) {
	return newONNXProvider(cfg, dataDir, log, nil, onnxCfg)
}

// newONNXProvider is the internal constructor that accepts an optional cache store.
func newONNXProvider(cfg config.LocalEmbedding, dataDir string, log *logger.Logger, store *cache.Store, onnxCfg *config.ONNXConfig) (*ONNXProvider, error) {
	return newONNXProviderContext(context.Background(), cfg, dataDir, log, store, onnxCfg)
}

// newONNXProviderContext creates an ONNX provider with context support and optional cache store.
func newONNXProviderContext(ctx context.Context, cfg config.LocalEmbedding, dataDir string, log *logger.Logger, store *cache.Store, onnxCfg *config.ONNXConfig) (*ONNXProvider, error) {
	if ctx == nil {
		return nil, fmt.Errorf("local embedding: context must not be nil")
	}

	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("local embedding: model_path is required")
	}

	modelName := cfg.ModelName
	if modelName == "" {
		modelName = "default"
	}

	// Ensure ONNX Runtime library is available (stored under dataDir/onnxruntime).
	libManager, err := onnx.NewLibraryManager(dataDir, onnxCfg)
	if err != nil {
		return nil, fmt.Errorf("local embedding: init library manager: %w", err)
	}

	libPath, err := libManager.EnsureLibraryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("local embedding: ensure onnxruntime library: %w\n\n"+
			"Tip: You can also install manually with:\n"+
			"  synopsis onnx-runtime install", err)
	}

	// Verify model file exists before loading.
	if _, err := os.Stat(cfg.ModelPath); err != nil {
		return nil, fmt.Errorf("local embedding: check model file %s: %w", cfg.ModelPath, err)
	}

	// Tell onnxruntime_go where to find the shared library (avoids LD_LIBRARY_PATH).
	onnxruntime_go.SetSharedLibraryPath(libPath)

	// Initialize ONNX Runtime environment if not already initialized.
	if !onnxruntime_go.IsInitialized() {
		if err := onnxruntime_go.InitializeEnvironment(); err != nil {
			return nil, fmt.Errorf("local embedding: initialize ONNX Runtime: %w", err)
		}
	}

	// Detect the model's actual graph I/O — exports differ: BGE-M3 has no
	// token_type_ids input and outputs a pooled sentence_embedding instead of
	// last_hidden_state, while BGE-small uses all three inputs.
	graphInputs, graphOutputs, err := onnxruntime_go.GetInputOutputInfo(cfg.ModelPath)
	if err != nil {
		return nil, fmt.Errorf("local embedding: inspect model I/O %s: %w", cfg.ModelPath, err)
	}

	inputNames, err := inputNamesFor(graphInputs)
	if err != nil {
		return nil, fmt.Errorf("local embedding: unsupported inputs in %s: %w", cfg.ModelPath, err)
	}

	outputName, err := selectOutputName(graphOutputs)
	if err != nil {
		return nil, fmt.Errorf("local embedding: unsupported outputs in %s: %w", cfg.ModelPath, err)
	}

	session, err := onnxruntime_go.NewDynamicAdvancedSession(
		cfg.ModelPath,
		inputNames,
		[]string{outputName},
		nil, // default session options
	)
	if err != nil {
		return nil, fmt.Errorf("local embedding: create ONNX session: %w", err)
	}

	if cfg.VectorDim <= 0 {
		cfg.VectorDim = 1024 // BGE-M3 default
	}

	// Auto-detect tokenizer path if not explicitly set.
	// Try tokenizer.json in the model directory, then fallback to explicit path.
	tokenizerPath := cfg.TokenizerPath
	if tokenizerPath == "" {
		modelDir := filepath.Dir(cfg.ModelPath)
		tjPath := filepath.Join(modelDir, "tokenizer.json")
		if _, err := os.Stat(tjPath); err == nil {
			tokenizerPath = modelDir // NewSugarmeTokenizer will find tokenizer.json inside
		} else {
			tokenizerPath = cfg.ModelPath // fallback to model dir
		}
	}

	tokenizer, err := NewSugarmeTokenizer(tokenizerPath, DefaultMaxLength)
	if err != nil {
		session.Destroy() //nolint:errcheck
		return nil, fmt.Errorf("local embedding: create tokenizer: %w", err)
	}

	var embCache *EmbeddingCache
	if store != nil {
		embCache = NewEmbeddingCacheWithStore(store)
	} else {
		embCache = NewEmbeddingCache()
	}

	return &ONNXProvider{
		session:    session,
		modelName:  modelName,
		modelPath:  cfg.ModelPath,
		vectorDim:  cfg.VectorDim,
		inputNames: inputNames,
		cache:      embCache,
		tokenizer:  tokenizer,
		log:        log,
	}, nil
}

// GenerateEmbeddings produces normalized embeddings for each text.
func (p *ONNXProvider) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	start := time.Now()
	defer func() {
		p.log.Infow("generated embeddings", "provider", p.Name(), "texts", len(texts), "duration", time.Since(start).Round(time.Millisecond))
	}()

	if len(texts) == 0 {
		return nil, fmt.Errorf("local embedding: texts must not be empty")
	}

	results := make([][]float32, len(texts))

	for i, text := range texts {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("local embedding: cancelled: %w", ctx.Err())
		default:
		}

		// Check cache first.
		if cached, ok := p.cache.Get(p.modelName, p.vectorDim, text); ok {
			results[i] = cached
			continue
		}

		vec, err := p.infer(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("local embedding: infer text %d: %w", i, err)
		}

		p.cache.Set(p.modelName, p.vectorDim, text, vec)
		results[i] = vec
	}

	return results, nil
}

// VectorDim returns the dimensionality of vectors produced by this provider.
func (p *ONNXProvider) VectorDim() int { return p.vectorDim }

// Name returns a human-readable identifier for logging.
func (p *ONNXProvider) Name() string { return "onnx" }

// infer runs ONNX inference on a single text and returns the normalized embedding.
func (p *ONNXProvider) infer(_ context.Context, text string) ([]float32, error) {
	tokenIDs, err := p.tokenizer.Tokenize(text)
	if err != nil {
		return nil, fmt.Errorf("tokenize: %w", err)
	}

	n := len(tokenIDs)

	inputIDs := make([]int64, n)
	for i, id := range tokenIDs {
		inputIDs[i] = int64(id)
	}
	attentionMask := make([]int64, n)
	for i := range attentionMask {
		attentionMask[i] = 1
	}

	dataForInput := map[string][]int64{
		"input_ids":      inputIDs,
		"attention_mask": attentionMask,
		// token_type_ids: zeros (single sequence); only bound if the model declares it.
		"token_type_ids": make([]int64, n),
	}

	tensors := make([]*onnxruntime_go.Tensor[int64], 0, len(p.inputNames))
	values := make([]onnxruntime_go.Value, 0, len(p.inputNames))
	destroyTensors := func() {
		for _, t := range tensors {
			_ = t.Destroy() //nolint:errcheck
		}
	}

	for _, name := range p.inputNames {
		data, ok := dataForInput[name]
		if !ok {
			destroyTensors()
			return nil, fmt.Errorf("infer: unsupported model input %q", name)
		}
		tensor, err := onnxruntime_go.NewTensor(onnxruntime_go.NewShape(1, int64(n)), data)
		if err != nil {
			destroyTensors()
			return nil, fmt.Errorf("create %s tensor: %w", name, err)
		}
		tensors = append(tensors, tensor)
		values = append(values, tensor)
	}

	// Pass a nil output so the runtime allocates it; shape varies by model
	// ([1, seq_len, dim] hidden states vs [1, dim] pooled embedding).
	outputs := []onnxruntime_go.Value{nil}
	if err := p.session.Run(values, outputs); err != nil {
		destroyTensors()
		return nil, fmt.Errorf("ONNX run: %w", err)
	}
	destroyTensors()

	tensor, ok := outputs[0].(*onnxruntime_go.Tensor[float32])
	if !ok {
		return nil, fmt.Errorf("unsupported ONNX output type: %T", outputs[0])
	}
	data := tensor.GetData()

	// Take the [0, 0, :] vector (CLS token representation); for models with a
	// pooled sentence_embedding output this is the single pre-pooled vector.
	if len(data) < p.vectorDim {
		return nil, fmt.Errorf("output tensor too small: expected at least %d values, got %d",
			p.vectorDim, len(data))
	}
	result := make([]float32, p.vectorDim)
	copy(result, data[:p.vectorDim])

	// L2 normalize the vector.
	norm := 0.0
	for _, v := range result {
		norm += float64(v * v)
	}
	norm = math.Sqrt(norm)
	if norm > 1e-9 {
		for i := range result {
			result[i] /= float32(norm)
		}
	}

	return result, nil
}
