package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/devmix/synopsis/internal/cache"
	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/logger"
)

const defaultMaxRetries = 3
const defaultTimeoutMs = 30_000

// APIProvider generates embeddings by calling an OpenAI-compatible HTTP API.
type APIProvider struct {
	baseURL    string
	apiKey     string
	modelName  string
	vectorDim  int
	httpClient *http.Client
	maxRetries int
	cache      *EmbeddingCache
	log        *logger.Logger
}

// NewAPIProvider creates an API provider configured for the given endpoint.
func NewAPIProvider(cfg config.APIEmbedding, log *logger.Logger) (*APIProvider, error) {
	return newAPIProvider(cfg, log, nil)
}

// newAPIProvider is the internal constructor that accepts an optional cache store.
func newAPIProvider(cfg config.APIEmbedding, log *logger.Logger, store *cache.Store) (*APIProvider, error) {
	if cfg.BaseURL == "" {
		return nil, fmt.Errorf("api embedding: base_url is required")
	}
	if cfg.ModelName == "" {
		return nil, fmt.Errorf("api embedding: model_name is required")
	}
	if cfg.VectorDim <= 0 {
		cfg.VectorDim = 1536 // OpenAI text-embedding-ada-002 default
	}

	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	timeoutMs := cfg.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = defaultTimeoutMs
	}

	var embCache *EmbeddingCache
	if store != nil {
		embCache = NewEmbeddingCacheWithStore(store)
	} else {
		embCache = NewEmbeddingCache()
	}

	return &APIProvider{
		baseURL:    cfg.BaseURL,
		apiKey:     cfg.APIKey,
		modelName:  cfg.ModelName,
		vectorDim:  cfg.VectorDim,
		httpClient: &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond},
		maxRetries: maxRetries,
		cache:      embCache,
		log:        log,
	}, nil
}

// GenerateEmbeddings produces embeddings for each text via the remote API.
func (p *APIProvider) GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	start := time.Now()
	defer func() {
		p.log.Infow("generated embeddings", "provider", p.Name(), "texts", len(texts), "duration", time.Since(start).Round(time.Millisecond))
	}()

	if len(texts) == 0 {
		return nil, fmt.Errorf("api embedding: texts must not be empty")
	}

	results := make([][]float32, len(texts))

	for i, text := range texts {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("api embedding: cancelled: %w", ctx.Err())
		default:
		}

		if cached, ok := p.cache.Get(p.modelName, p.vectorDim, text); ok {
			results[i] = cached
			continue
		}

		vec, err := p.requestWithRetries(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("api embedding: request for text %d: %w", i, err)
		}

		p.cache.Set(p.modelName, p.vectorDim, text, vec)
		results[i] = vec
	}

	return results, nil
}

// VectorDim returns the dimensionality of vectors produced by this provider.
func (p *APIProvider) VectorDim() int { return p.vectorDim }

// Name returns a human-readable identifier for logging.
func (p *APIProvider) Name() string { return "api" }

// --- request helpers ---

// httpError wraps an HTTP error with the status code for retry classification.
type httpError struct {
	StatusCode int
	Message    string
}

func (e *httpError) Error() string { return e.Message }

type embeddingRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type embeddingResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
}

func (p *APIProvider) requestWithRetries(ctx context.Context, text string) ([]float32, error) {
	var lastErr error
	for attempt := 0; attempt <= p.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("api embedding: cancelled during backoff: %w", ctx.Err())
			case <-time.After(backoff):
			}
		}

		vec, err := p.doRequest(ctx, text)
		if err == nil {
			return vec, nil
		}

		lastErr = err

		// Retry only on transient errors.
		if !isRetryableError(err, attempt, p.maxRetries) {
			break
		}
	}
	return nil, fmt.Errorf("api embedding: exhausted %d retries: %w", p.maxRetries, lastErr)
}

func (p *APIProvider) doRequest(ctx context.Context, text string) ([]float32, error) {
	reqBody := embeddingRequest{
		Input: []string{text},
		Model: p.modelName,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := p.baseURL + "/embeddings"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.apiKey)
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var emResp embeddingResponse
		if err := json.Unmarshal(body, &emResp); err != nil {
			return nil, fmt.Errorf("parse API response: %w", err)
		}
		if len(emResp.Data) == 0 || len(emResp.Data[0].Embedding) == 0 {
			return nil, fmt.Errorf("api embedding: empty embedding in response")
		}

		f64vec := emResp.Data[0].Embedding
		vec := make([]float32, len(f64vec))
		for i, v := range f64vec {
			vec[i] = float32(v)
		}
		return vec, nil

	case http.StatusTooManyRequests:
		return nil, &httpError{StatusCode: 429, Message: "api embedding: rate limited (429)"}

	default:
		if resp.StatusCode >= 500 {
			return nil, &httpError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("api embedding: server error %d: %s", resp.StatusCode, string(body))}
		}
		return nil, &httpError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("api embedding: HTTP %d: %s", resp.StatusCode, string(body))}
	}
}

// isRetryableError returns true if the error warrants a retry attempt.
func isRetryableError(err error, attempt int, maxRetries int) bool {
	if attempt >= maxRetries {
		return false
	}

	// Do not retry on explicit context timeout or cancellation.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}

	// Retry on network timeout errors.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}

	// Retry on 429 and 5xx HTTP errors.
	var httpErr *httpError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == 429 || httpErr.StatusCode >= 500
	}

	return false
}
