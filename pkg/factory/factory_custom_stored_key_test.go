package factory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// TestCreateCustomProvider_StoredKeyIsSentAsBearer pins the "paste the API
// key" setup path: a custom provider saved with no env var authenticates
// chat requests with the key kept in the credential store.
func TestCreateCustomProvider_StoredKeyIsSentAsBearer(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("SPROUT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()
	t.Cleanup(credentials.ResetStorageBackend)

	var mu sync.Mutex
	var chatAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "mock-model"}}})
		case "/v1/chat/completions":
			mu.Lock()
			chatAuth = r.Header.Get("Authorization")
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "chatcmpl-test", "object": "chat.completion", "created": 1, "model": "mock-model",
				"choices": []map[string]any{{
					"index":         0,
					"message":       map[string]any{"role": "assistant", "content": "ok"},
					"finish_reason": "stop",
				}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	if err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "pasted-key-provider",
		Endpoint:       server.URL + "/v1",
		ModelName:      "mock-model",
		RequiresAPIKey: true,
	}); err != nil {
		t.Fatalf("save provider: %v", err)
	}
	if err := credentials.SetToActiveBackend("pasted-key-provider", "sk-stored-123"); err != nil {
		t.Fatalf("store key: %v", err)
	}

	client, err := CreateCustomProvider("pasted-key-provider", "mock-model")
	if err != nil {
		t.Fatalf("CreateCustomProvider: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.SendChatRequest(ctx, []api.Message{{Role: "user", Content: "hi"}}, nil, "", false); err != nil {
		t.Fatalf("SendChatRequest: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if chatAuth != "Bearer sk-stored-123" {
		t.Fatalf("chat Authorization = %q, want the stored key", chatAuth)
	}
}
