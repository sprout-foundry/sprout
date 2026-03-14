package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func TestLMStudioConnectionNoAuth(t *testing.T) {
	// Skip this test in CI environments since LM Studio won't be running
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		t.Skip("Skipping LM Studio connection test in CI environment")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if authHeader := strings.TrimSpace(r.Header.Get("Authorization")); authHeader != "" {
			t.Fatalf("expected no authorization header for local LM Studio test, got %q", authHeader)
		}

		if r.URL.Path != "/" {
			t.Fatalf("expected request path /, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "test-chatcmpl",
			"object":  "chat.completion",
			"created": 1234567890,
			"model":   "qwen3-coder:30b",
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": "ok",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer server.Close()

	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("failed to parse test server URL: %v", err)
	}

	testCases := []struct {
		name     string
		endpoint string
	}{
		{"127.0.0.1", fmt.Sprintf("http://127.0.0.1:%s", parsed.Port())},
		{"localhost", fmt.Sprintf("http://localhost:%s", parsed.Port())},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := &ProviderConfig{
				Name:     "lmstudio",
				Endpoint: tc.endpoint,
				Defaults: RequestDefaults{
					Model: "qwen3-coder:30b",
				},
				Models: ModelConfig{
					DefaultModel:        "qwen3-coder:30b",
					DefaultContextLimit: 4096,
					AvailableModels:     []string{"qwen3-coder:30b"},
				},
				Auth: AuthConfig{
					Type:   "bearer",
					EnvVar: "",
				},
			}

			provider, err := NewGenericProvider(config)
			if err != nil {
				t.Fatalf("failed to create provider: %v", err)
			}

			if err := provider.CheckConnection(); err != nil {
				t.Fatalf("expected local LM Studio connection check to succeed without auth, got: %v", err)
			}
		})
	}
}
