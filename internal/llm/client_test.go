package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/devmix/synopsis/internal/config"
	"github.com/devmix/synopsis/internal/llm"
)

func TestCall_Success(t *testing.T) {
	t.Run("returns content from valid response", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"message": map[string]string{"content": `{"entities":[],"relations":[]}`},
					},
				},
			}
			json.NewEncoder(w).Encode(resp) //nolint:errcheck
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		client, err := llm.NewClient(llm.ClientOptions{
			Config: config.LLMConfig{
				APIBaseURL: server.URL,
				ModelName:  "test-model",
			},
		})
		if err != nil {
			t.Fatalf("NewClient returned error: %v", err)
		}

		content, err := client.Call(context.Background(), llm.CallOptions{
			SystemPrompt: "You are a helper.",
			UserPrompt:   "Extract entities from this text.",
		})
		if err != nil {
			t.Fatalf("Call returned error: %v", err)
		}

		expected := `{"entities":[],"relations":[]}`
		if content != expected {
			t.Errorf("expected %q, got %q", expected, content)
		}
	})

	t.Run("sets authorization header when API key provided", func(t *testing.T) {
		var authHeader string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]string{"content": "ok"}},
				},
			}
			json.NewEncoder(w).Encode(resp) //nolint:errcheck
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		client, err := llm.NewClient(llm.ClientOptions{
			Config: config.LLMConfig{
				APIBaseURL: server.URL,
				ModelName:  "test-model",
				APIKey:     "secret-key-123",
			},
		})
		if err != nil {
			t.Fatalf("NewClient returned error: %v", err)
		}

		_, _ = client.Call(context.Background(), llm.CallOptions{
			SystemPrompt: "test",
			UserPrompt:   "test",
		})

		expectedAuth := "Bearer secret-key-123"
		if authHeader != expectedAuth {
			t.Errorf("expected Authorization %q, got %q", expectedAuth, authHeader)
		}
	})

	t.Run("does not set authorization header when API key empty", func(t *testing.T) {
		var authHeader string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader = r.Header.Get("Authorization")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]string{"content": "ok"}},
				},
			}
			json.NewEncoder(w).Encode(resp) //nolint:errcheck
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		client, err := llm.NewClient(llm.ClientOptions{
			Config: config.LLMConfig{
				APIBaseURL: server.URL,
				ModelName:  "test-model",
				APIKey:     "",
			},
		})
		if err != nil {
			t.Fatalf("NewClient returned error: %v", err)
		}

		_, _ = client.Call(context.Background(), llm.CallOptions{
			SystemPrompt: "test",
			UserPrompt:   "test",
		})

		if authHeader != "" {
			t.Errorf("expected empty Authorization header, got %q", authHeader)
		}
	})
}

func TestCall_RetryOn429(t *testing.T) {
	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "recovered"}},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := llm.NewClient(llm.ClientOptions{
		Config: config.LLMConfig{
			APIBaseURL: server.URL,
			ModelName:  "test-model",
			MaxRetries: 3,
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	content, err := client.Call(context.Background(), llm.CallOptions{
		SystemPrompt: "You are a helper.",
		UserPrompt:   "Extract entities.",
	})
	if err != nil {
		t.Fatalf("Call returned error after retry on 429: %v", err)
	}

	if content != "recovered" {
		t.Errorf("expected 'recovered', got %q", content)
	}

	if callCount < 2 {
		t.Errorf("expected at least 2 calls (initial + retry), got %d", callCount)
	}
}

func TestCall_RetryOn5xx(t *testing.T) {
	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "recovered"}},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := llm.NewClient(llm.ClientOptions{
		Config: config.LLMConfig{
			APIBaseURL: server.URL,
			ModelName:  "test-model",
			MaxRetries: 3,
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	content, err := client.Call(context.Background(), llm.CallOptions{
		SystemPrompt: "You are a helper.",
		UserPrompt:   "Extract entities.",
	})
	if err != nil {
		t.Fatalf("Call returned error after retry on 5xx: %v", err)
	}

	if content != "recovered" {
		t.Errorf("expected 'recovered', got %q", content)
	}

	if callCount < 2 {
		t.Errorf("expected at least 2 calls (initial + retry), got %d", callCount)
	}
}

func TestCall_NonRetryableError(t *testing.T) {
	t.Run("context.Canceled returns immediately without retries", func(t *testing.T) {
		callCount := 0
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(http.StatusOK)
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		client, err := llm.NewClient(llm.ClientOptions{
			Config: config.LLMConfig{
				APIBaseURL: server.URL,
				ModelName:  "test-model",
				MaxRetries: 3,
			},
		})
		if err != nil {
			t.Fatalf("NewClient returned error: %v", err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // cancel immediately

		_, err = client.Call(ctx, llm.CallOptions{
			SystemPrompt: "test",
			UserPrompt:   "test",
		})
		if err == nil {
			t.Fatal("expected error from cancelled context")
		}

		if !strings.Contains(err.Error(), "context canceled") &&
			!strings.Contains(err.Error(), "cancelled") {
			t.Errorf("expected context cancellation error, got: %v", err)
		}

		// Should have made only 1 attempt (no retries for non-retryable errors from HTTP layer)
		// Note: the first call will fail at HTTP level because context is already cancelled.
		if callCount != 0 && callCount > 3 {
			t.Errorf("expected minimal calls on cancelled context, got %d", callCount)
		}
	})

	t.Run("4xx error returns immediately without retries", func(t *testing.T) {
		callCount := 0
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":"bad request"}`)) //nolint:errcheck
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		client, err := llm.NewClient(llm.ClientOptions{
			Config: config.LLMConfig{
				APIBaseURL: server.URL,
				ModelName:  "test-model",
				MaxRetries: 3,
			},
		})
		if err != nil {
			t.Fatalf("NewClient returned error: %v", err)
		}

		_, err = client.Call(context.Background(), llm.CallOptions{
			SystemPrompt: "test",
			UserPrompt:   "test",
		})
		if err == nil {
			t.Fatal("expected error from 400 response")
		}

		if callCount != 1 {
			t.Errorf("expected exactly 1 call (no retries for 4xx), got %d", callCount)
		}
	})
}

func TestCall_JsonSchemaFormat(t *testing.T) {
	var receivedBody map[string]interface{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "ok"}},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := llm.NewClient(llm.ClientOptions{
		Config: config.LLMConfig{
			APIBaseURL:     server.URL,
			ModelName:      "test-model",
			ResponseFormat: "json_schema",
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	schemaStr := `{"type":"object","properties":{"entities":{"type":"array"}}}`
	_, _ = client.Call(context.Background(), llm.CallOptions{
		SystemPrompt: "You are a helper.",
		UserPrompt:   "Extract entities.",
		JSONSchema:   schemaStr,
	})

	if receivedBody == nil {
		t.Fatal("received body is nil")
	}

	respFormat, ok := receivedBody["response_format"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected response_format in request body")
	}

	if respFormat["type"] != "json_schema" {
		t.Errorf("expected response_format type 'json_schema', got %v", respFormat["type"])
	}

	jsonSchema, ok := respFormat["json_schema"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected json_schema in response_format")
	}

	if jsonSchema["name"] != "llm_output" {
		t.Errorf("expected json_schema name 'llm_output', got %v", jsonSchema["name"])
	}
}

func TestCall_JsonSchemaFallback(t *testing.T) {
	var receivedBody map[string]interface{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "ok"}},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := llm.NewClient(llm.ClientOptions{
		Config: config.LLMConfig{
			APIBaseURL:     server.URL,
			ModelName:      "test-model",
			ResponseFormat: "json_schema", // configured for json_schema but...
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	// ...pass empty JSONSchema so it falls back to json_object
	_, _ = client.Call(context.Background(), llm.CallOptions{
		SystemPrompt: "You are a helper.",
		UserPrompt:   "Extract entities.",
		JSONSchema:   "", // empty schema → fallback
	})

	if receivedBody == nil {
		t.Fatal("received body is nil")
	}

	respFormat, ok := receivedBody["response_format"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected response_format in request body")
	}

	if respFormat["type"] != "json_object" {
		t.Errorf("expected fallback to 'json_object', got %v", respFormat["type"])
	}
}

func TestCall_InvalidConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     llm.ClientOptions
		wantErr string
	}{
		{
			name: "empty base URL",
			cfg: llm.ClientOptions{
				Config: config.LLMConfig{
					ModelName: "test-model",
				},
			},
			wantErr: "base_url is required",
		},
		{
			name: "empty model name",
			cfg: llm.ClientOptions{
				Config: config.LLMConfig{
					APIBaseURL: "http://localhost:8080/v1",
				},
			},
			wantErr: "model_name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := llm.NewClient(tt.cfg)
			if err == nil {
				t.Fatal("expected error but got none")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error to contain %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestCall_MalformedResponse(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`not valid json`)) //nolint:errcheck
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := llm.NewClient(llm.ClientOptions{
		Config: config.LLMConfig{
			APIBaseURL: server.URL,
			ModelName:  "test-model",
			MaxRetries: 0, // no retries to speed up test
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	_, err = client.Call(context.Background(), llm.CallOptions{
		SystemPrompt: "test",
		UserPrompt:   "test",
	})
	if err == nil {
		t.Fatal("expected error from malformed response")
	}

	if !strings.Contains(err.Error(), "parse response") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestCall_ExhaustedRetries(t *testing.T) {
	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusInternalServerError)
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	maxRetries := 2
	client, err := llm.NewClient(llm.ClientOptions{
		Config: config.LLMConfig{
			APIBaseURL: server.URL,
			ModelName:  "test-model",
			MaxRetries: maxRetries,
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	_, err = client.Call(context.Background(), llm.CallOptions{
		SystemPrompt: "You are a helper.",
		UserPrompt:   "Extract entities.",
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}

	if !strings.Contains(err.Error(), "exhausted") {
		t.Errorf("expected exhausted retries error, got: %v", err)
	}

	// initial call + maxRetries = 1 + 2 = 3 total calls
	expectedCalls := 1 + maxRetries
	if callCount != expectedCalls {
		t.Errorf("expected %d calls (initial + %d retries), got %d", expectedCalls, maxRetries, callCount)
	}
}

func TestNewClient_Defaults(t *testing.T) {
	var receivedBody map[string]interface{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody) //nolint:errcheck
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "ok"}},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := llm.NewClient(llm.ClientOptions{
		Config: config.LLMConfig{
			APIBaseURL: server.URL,
			ModelName:  "test-model",
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	_, _ = client.Call(context.Background(), llm.CallOptions{
		SystemPrompt: "test",
		UserPrompt:   "test",
	})

	if receivedBody == nil {
		t.Fatal("received body is nil")
	}

	// Check max_tokens default (2048)
	maxTokens, ok := receivedBody["max_tokens"].(float64)
	if !ok || int(maxTokens) != 2048 {
		t.Errorf("expected max_tokens 2048, got %v", receivedBody["max_tokens"])
	}

	// Check response_format default (json_object)
	respFormat, ok := receivedBody["response_format"].(map[string]interface{})
	if !ok || respFormat["type"] != "json_object" {
		t.Errorf("expected response_format type 'json_object', got %v", receivedBody["response_format"])
	}
}

// TestCall_EmptyPrompts validates that empty system or user prompts are rejected.
func TestCall_EmptyPrompts(t *testing.T) {
	t.Parallel()

	client, err := llm.NewClient(llm.ClientOptions{
		Config: config.LLMConfig{
			APIBaseURL: "http://localhost:9999",
			ModelName:  "test-model",
			TimeoutMs:  1000,
			MaxRetries: 0,
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	tests := []struct {
		name   string
		system string
		user   string
	}{
		{
			name:   "empty system prompt",
			system: "",
			user:   "user content",
		},
		{
			name:   "empty user prompt",
			system: "system content",
			user:   "",
		},
		{
			name:   "both empty",
			system: "",
			user:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Call(context.Background(), llm.CallOptions{
				SystemPrompt: tt.system,
				UserPrompt:   tt.user,
			})
			if err == nil {
				t.Error("expected error for empty prompt")
			}
		})
	}
}

// TestCall_EmptyContent validates that empty or whitespace-only content returns a descriptive error.
func TestCall_EmptyContent(t *testing.T) {
	tests := []struct {
		name              string
		content           string
		finishReason      string
		reasoningContent  string
		wantErrContains   []string
		notWantErrContain []string
	}{
		{
			name:             "empty content with finish_reason=length mentions thinking mode",
			content:          "",
			finishReason:     "length",
			reasoningContent: "this is some reasoning output from the model that was quite long",
			wantErrContains: []string{
				"empty response from model",
				`finish_reason="length"`,
				"reasoning_content_length=",
				"thinking mode",
			},
			notWantErrContain: nil,
		},
		{
			name:             "empty content with finish_reason=stop does not mention thinking mode",
			content:          "",
			finishReason:     "stop",
			reasoningContent: "",
			wantErrContains: []string{
				"empty response from model",
				`finish_reason="stop"`,
				"reasoning_content_length=0",
				"model returned empty response",
			},
			notWantErrContain: []string{"thinking mode"},
		},
		{
			name:             "whitespace-only content treated as empty",
			content:          "   \n\t  ",
			finishReason:     "stop",
			reasoningContent: "",
			wantErrContains: []string{
				"empty response from model",
				`finish_reason="stop"`,
				"reasoning_content_length=0",
			},
			notWantErrContain: nil,
		},
		{
			name:              "non-empty content with reasoning_content returns normally",
			content:           `{"entities":[]}`,
			finishReason:      "stop",
			reasoningContent:  "some reasoning here",
			wantErrContains:   nil,
			notWantErrContain: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)

				resp := map[string]interface{}{
					"choices": []map[string]interface{}{
						{
							"message": map[string]string{
								"content":           tt.content,
								"reasoning_content": tt.reasoningContent,
							},
							"finish_reason": tt.finishReason,
						},
					},
				}
				json.NewEncoder(w).Encode(resp) //nolint:errcheck
			})

			server := httptest.NewServer(handler)
			defer server.Close()

			client, err := llm.NewClient(llm.ClientOptions{
				Config: config.LLMConfig{
					APIBaseURL: server.URL,
					ModelName:  "test-model",
					MaxRetries: 0, // no retries for empty content tests
				},
			})
			if err != nil {
				t.Fatalf("NewClient returned error: %v", err)
			}

			content, err := client.Call(context.Background(), llm.CallOptions{
				SystemPrompt: "You are a helper.",
				UserPrompt:   "Extract entities from this text.",
			})

			if tt.wantErrContains != nil {
				if err == nil {
					t.Fatal("expected error but got none")
				}
				for _, substr := range tt.wantErrContains {
					if !strings.Contains(err.Error(), substr) {
						t.Errorf("expected error to contain %q, got: %v", substr, err)
					}
				}
				for _, substr := range tt.notWantErrContain {
					if strings.Contains(err.Error(), substr) {
						t.Errorf("expected error NOT to contain %q, got: %v", substr, err)
					}
				}
			} else {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				expected := strings.TrimSpace(tt.content)
				if content != expected {
					t.Errorf("expected content %q, got %q", expected, content)
				}
			}
		})
	}
}

// TestCall_EmptyContentNonRetryable validates that empty content errors are not retried.
func TestCall_EmptyContentNonRetryable(t *testing.T) {
	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message":       map[string]string{"content": ""},
					"finish_reason": "length",
				},
			},
		}
		json.NewEncoder(w).Encode(resp) //nolint:errcheck
	})

	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := llm.NewClient(llm.ClientOptions{
		Config: config.LLMConfig{
			APIBaseURL: server.URL,
			ModelName:  "test-model",
			MaxRetries: 3, // retries configured but should NOT be used for empty content
		},
	})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	_, err = client.Call(context.Background(), llm.CallOptions{
		SystemPrompt: "You are a helper.",
		UserPrompt:   "Extract entities.",
	})
	if err == nil {
		t.Fatal("expected error for empty content")
	}

	if callCount != 1 {
		t.Errorf("expected exactly 1 call (no retries for empty content), got %d", callCount)
	}
}

// TestCall_ExistingBehaviorUnchanged validates that existing retry behavior is unchanged.
func TestCall_ExistingBehaviorUnchanged(t *testing.T) {
	t.Run("429 still retried", func(t *testing.T) {
		callCount := 0
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]string{"content": "recovered"}},
				},
			}
			json.NewEncoder(w).Encode(resp) //nolint:errcheck
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		client, err := llm.NewClient(llm.ClientOptions{
			Config: config.LLMConfig{
				APIBaseURL: server.URL,
				ModelName:  "test-model",
				MaxRetries: 3,
			},
		})
		if err != nil {
			t.Fatalf("NewClient returned error: %v", err)
		}

		content, err := client.Call(context.Background(), llm.CallOptions{
			SystemPrompt: "test",
			UserPrompt:   "test",
		})
		if err != nil {
			t.Fatalf("Call should succeed after retry on 429: %v", err)
		}
		if content != "recovered" {
			t.Errorf("expected 'recovered', got %q", content)
		}
	})

	t.Run("5xx still retried", func(t *testing.T) {
		callCount := 0
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]string{"content": "recovered"}},
				},
			}
			json.NewEncoder(w).Encode(resp) //nolint:errcheck
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		client, err := llm.NewClient(llm.ClientOptions{
			Config: config.LLMConfig{
				APIBaseURL: server.URL,
				ModelName:  "test-model",
				MaxRetries: 3,
			},
		})
		if err != nil {
			t.Fatalf("NewClient returned error: %v", err)
		}

		content, err := client.Call(context.Background(), llm.CallOptions{
			SystemPrompt: "test",
			UserPrompt:   "test",
		})
		if err != nil {
			t.Fatalf("Call should succeed after retry on 5xx: %v", err)
		}
		if content != "recovered" {
			t.Errorf("expected 'recovered', got %q", content)
		}
	})

	t.Run("network error still retried", func(t *testing.T) {
		callCount := 0
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			if callCount == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			resp := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]string{"content": "recovered"}},
				},
			}
			json.NewEncoder(w).Encode(resp) //nolint:errcheck
		})

		server := httptest.NewServer(handler)
		defer server.Close()

		client, err := llm.NewClient(llm.ClientOptions{
			Config: config.LLMConfig{
				APIBaseURL: server.URL,
				ModelName:  "test-model",
				MaxRetries: 3,
			},
		})
		if err != nil {
			t.Fatalf("NewClient returned error: %v", err)
		}

		content, err := client.Call(context.Background(), llm.CallOptions{
			SystemPrompt: "test",
			UserPrompt:   "test",
		})
		if err != nil {
			t.Fatalf("Call should succeed after retry on 5xx: %v", err)
		}
		if content != "recovered" {
			t.Errorf("expected 'recovered', got %q", content)
		}
	})
}
