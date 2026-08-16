// Package llm provides a shared HTTP client for OpenAI-compatible LLM APIs.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/logger"
)

// ClientOptions holds parameters for the LLM HTTP client.
type ClientOptions struct {
	Config config.LLMConfig
	Log    *logger.Logger // optional structured logger
}

// Client is a shared HTTP client for OpenAI-compatible LLM APIs.
type Client struct {
	apiBaseURL     string
	modelName      string
	apiKey         string
	temperature    float64
	seed           int
	maxTokens      int
	responseFormat string
	maxRetries     int
	httpClient     *http.Client
	log            *logger.Logger
}

// CallOptions configures a single LLM call.
type CallOptions struct {
	SystemPrompt string
	UserPrompt   string
	JSONSchema   string // optional pre-generated JSON schema for json_schema response format
	SchemaName   string // name for the JSON schema; defaults to "llm_output" if empty
	Attachments  []string
}

type llmRequest struct {
	Model          string          `json:"model"`
	Messages       []llmMessage    `json:"messages"`
	Temperature    float64         `json:"temperature,omitempty"`
	Seed           int             `json:"seed,omitempty"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type llmMessageText struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type llmMessage struct {
	Role    string        `json:"role"`
	Content []interface{} `json:"content"`
}

type responseFormat struct {
	Type       string            `json:"type"`
	JSONSchema *jsonSchemaConfig `json:"json_schema,omitempty"`
}

type jsonSchemaConfig struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
}

type llmResponse struct {
	Choices []llmChoice `json:"choices"`
}

type llmChoice struct {
	Message      llmMessageResp  `json:"message"`
	FinishReason string          `json:"finish_reason,omitempty"`
	Index        int             `json:"index,omitempty"`
	Logprobs     json.RawMessage `json:"logprobs,omitempty"`
}

type llmMessageResp struct {
	Content          string `json:"content"`
	Refusal          string `json:"refusal,omitempty"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type httpError struct {
	StatusCode int
	Message    string
}

func (e *httpError) Error() string { return e.Message }

// emptyContentError is returned when the LLM responds with empty or whitespace-only content.
// It is intentionally non-retryable because retrying will produce the same result.
type emptyContentError struct {
	Message string
}

func (e *emptyContentError) Error() string { return e.Message }

// NewClient creates a new LLM HTTP client from the given config.
func NewClient(cfg ClientOptions) (*Client, error) {
	if cfg.Config.APIBaseURL == "" {
		return nil, fmt.Errorf("llm client: api_base_url is required")
	}
	if cfg.Config.ModelName == "" {
		return nil, fmt.Errorf("llm client: model_name is required")
	}

	temp := cfg.Config.Temperature
	if temp < 0 {
		temp = 0.0
	}

	maxTokens := cfg.Config.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 2048
	}

	responseFormat := cfg.Config.ResponseFormat
	if responseFormat == "" || responseFormat != "json_schema" {
		responseFormat = "json_object"
	}

	timeoutMs := cfg.Config.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = 60000
	}

	maxRetries := cfg.Config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	return &Client{
		apiBaseURL:     cfg.Config.APIBaseURL,
		apiKey:         cfg.Config.APIKey,
		modelName:      cfg.Config.ModelName,
		temperature:    temp,
		seed:           cfg.Config.Seed,
		maxTokens:      maxTokens,
		responseFormat: responseFormat,
		maxRetries:     maxRetries,
		httpClient:     &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond},
		log:            cfg.Log,
	}, nil
}

// Call makes an LLM API call with retry logic. Returns the raw content string from the response.
func (c *Client) Call(ctx context.Context, opts CallOptions) (string, error) {
	if opts.SystemPrompt == "" {
		return "", fmt.Errorf("llm client: system_prompt is required")
	}
	if opts.UserPrompt == "" {
		return "", fmt.Errorf("llm client: user_prompt is required")
	}

	var lastErr error

	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			jitter := time.Duration(rand.Intn(500)) * time.Millisecond //nolint:gosec
			select {
			case <-ctx.Done():
				return "", fmt.Errorf("llm client: cancelled during retry backoff: %w", ctx.Err())
			case <-time.After(backoff + jitter):
			}
		}

		response, err := c.doCall(ctx, opts)
		if response != "" && err == nil {
			return response, nil
		}

		lastErr = err

		if !isRetryableError(err) {
			return "", fmt.Errorf("llm client: %w", lastErr)
		}

		if c.log != nil {
			c.log.Debug("retrying LLM request", "attempt", attempt+1, "max_retries", c.maxRetries, logger.Err(err))
		}
	}

	return "", fmt.Errorf("llm client: exhausted %d retries: %w", c.maxRetries, lastErr)
}

func (c *Client) doCall(ctx context.Context, opts CallOptions) (string, error) {
	reqBody := c.buildRequestBody(opts.SystemPrompt, opts.UserPrompt, opts.JSONSchema, opts.SchemaName, opts.Attachments)

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("llm client: marshal request: %w", err)
	}

	url := c.apiBaseURL + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("llm client: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("llm client: HTTP request: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("llm client: read response: %w", err)
	}

	switch resp.StatusCode {
	case http.StatusOK:
		var apiResp llmResponse
		if err := json.Unmarshal(body, &apiResp); err != nil {
			return "", fmt.Errorf("llm client: parse response: %w", err)
		}

		if len(apiResp.Choices) == 0 {
			return "", fmt.Errorf("llm client: no choices in response")
		}

		choice := apiResp.Choices[0]
		content := strings.TrimSpace(choice.Message.Content)

		if content == "" {
			reasoningLen := len(choice.Message.ReasoningContent)
			finishReason := choice.FinishReason
			if finishReason == "" {
				finishReason = "(none)"
			}
			msg := fmt.Sprintf(
				"llm client: empty response from model (finish_reason=%q, reasoning_content_length=%d)",
				finishReason, reasoningLen,
			)
			if finishReason == "length" {
				msg += "; likely cause: thinking mode consumed all max_tokens on reasoning_content; disable thinking or increase max_tokens"
			} else {
				msg += "; model returned empty response"
			}
			return "", &emptyContentError{Message: msg}
		}

		return content, nil

	case http.StatusTooManyRequests:
		return "", &httpError{StatusCode: 429, Message: "llm client: rate limited (429)"}

	default:
		if resp.StatusCode >= 500 {
			return "", &httpError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("llm client: server error %d: %s", resp.StatusCode, string(body))}
		}
		return "", &httpError{StatusCode: resp.StatusCode, Message: fmt.Sprintf("llm client: HTTP %d: %s", resp.StatusCode, string(body))}
	}
}

func (c *Client) buildRequestBody(systemPrompt, userPrompt, jsonSchema, schemaName string, attachments []string) llmRequest {
	userMessages := []interface{}{llmMessageText{Type: "text", Text: userPrompt}}

	if len(attachments) > 0 {
		for _, attachment := range attachments {
			userMessages = append(userMessages, llmMessageText{Type: "text", Text: attachment})
		}
	}

	messages := []llmMessage{
		{Role: "system", Content: []interface{}{llmMessageText{Type: "text", Text: systemPrompt}}},
		{Role: "user", Content: userMessages},
	}

	reqBody := llmRequest{
		Model:       c.modelName,
		Messages:    messages,
		Temperature: c.temperature,
		Seed:        c.seed,
		MaxTokens:   c.maxTokens,
	}

	switch c.responseFormat {
	case "json_schema":
		if jsonSchema != "" {
			name := schemaName
			if name == "" {
				name = "llm_output"
			}
			reqBody.ResponseFormat = &responseFormat{
				Type: "json_schema",
				JSONSchema: &jsonSchemaConfig{
					Name:   name,
					Schema: json.RawMessage(jsonSchema),
				},
			}
			return reqBody
		}
		fallthrough
	default: // json_object or fallback when schema is empty
		reqBody.ResponseFormat = &responseFormat{Type: "json_object"}
	}

	return reqBody
}

func (c *Client) IsRequiresSchema() bool {
	return c.responseFormat == "json_schema"
}

func isRetryableError(err error) bool {
	// Do not retry on explicit context cancellation.
	if errors.Is(err, context.Canceled) {
		return false
	}

	// Do not retry on empty content — retrying produces the same result.
	var emptyErr *emptyContentError
	if errors.As(err, &emptyErr) {
		return false
	}

	// Retry on HTTP deadline exceeded (transient timeout).
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// Retry on network timeout errors (*url.Error with Timeout()).
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
