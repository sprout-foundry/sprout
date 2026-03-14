package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
)

func TestSetProviderFallsBackWhenConfiguredCustomModelIsInvalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "qwen3.5-4b"},
					{"id": "qwen3.5-35-A3B"},
				},
			})
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode chat request: %v", err)
			}

			model, _ := body["model"].(string)
			if model != "qwen3.5-4b" {
				http.Error(w, "error code: 502", http.StatusBadGateway)
				return
			}

			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-test",
				"object":  "chat.completion",
				"created": 1,
				"model":   model,
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
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "ai-worker",
		Endpoint:       server.URL + "/v1",
		ModelName:      "2",
		RequiresAPIKey: false,
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	if err := agent.SetProvider(api.ClientType("ai-worker")); err != nil {
		t.Fatalf("expected provider switch to recover from invalid configured model, got error: %v", err)
	}

	if got := agent.GetModel(); got != "qwen3.5-4b" {
		t.Fatalf("expected fallback model to be persisted on switch, got %q", got)
	}

	savedCfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}
	if got := savedCfg.GetModelForProvider("ai-worker"); got != "qwen3.5-4b" {
		t.Fatalf("expected saved provider model to be updated, got %q", got)
	}
}
