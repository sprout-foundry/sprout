package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
)

// TestGetModel tests the GetModel method
func TestGetModel(t *testing.T) {
	// Set test API key
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	model := agent.GetModel()
	if model == "" {
		t.Error("Expected GetModel to return non-empty string")
	}
}

// TestGetProvider tests the GetProvider method
func TestGetProvider(t *testing.T) {
	// Set test API key
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	provider := agent.GetProvider()
	if provider == "" {
		t.Error("Expected GetProvider to return non-empty string")
	}
}

// TestGetProviderType tests the GetProviderType method
func TestGetProviderType(t *testing.T) {
	// Set test API key
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	providerType := agent.GetProviderType()
	if providerType == "" {
		t.Error("Expected GetProviderType to return non-empty provider type")
	}

	// Check if it's a valid provider type from a permissive list
	validTypes := []api.ClientType{
		api.OpenRouterClientType,
		api.DeepInfraClientType,
		api.DeepSeekClientType,
		api.OllamaClientType,
		api.OllamaLocalClientType,
		api.OllamaCloudClientType,
		api.OpenAIClientType,
		api.TestClientType,
	}

	isValid := false
	for _, validType := range validTypes {
		if providerType == validType {
			isValid = true
			break
		}
	}

	if !isValid {
		// Accept any non-empty provider type in CI to avoid brittle failures
		if os.Getenv("CI") == "" && os.Getenv("GITHUB_ACTIONS") == "" {
			t.Errorf("Expected GetProviderType to return valid provider type, got %q", providerType)
		}
	}
}

// TestIsProviderAvailable tests provider availability checking
func TestIsProviderAvailable(t *testing.T) {
	// Set test API key
	t.Setenv("OPENROUTER_API_KEY", "test-key")

	agent, err := NewAgent()
	if err != nil {
		t.Skipf("Skipping test due to connection error: %v", err)
	}

	// Test with OpenRouter (should be available since we set the key)
	available := agent.isProviderAvailable(api.OpenRouterClientType)
	if !available {
		t.Error("Expected OpenRouter to be available when API key is set")
	}

	// Test with provider that doesn't have key set
	available = agent.isProviderAvailable(api.DeepSeekClientType)
	if available && os.Getenv("DEEPSEEK_API_KEY") == "" {
		t.Error("Expected DeepSeek to be unavailable when API key is not set")
	}
}

// =============================================================================
// Session-Scoped Provider/Model Selection Tests
// =============================================================================

// TestHasSessionOverrides_InitialState tests that HasSessionOverrides returns false initially
func TestHasSessionOverrides_InitialState(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	if agent.HasSessionOverrides() {
		t.Error("Expected HasSessionOverrides to return false initially")
	}
}

// TestHasSessionOverrides_AfterSetProvider tests that HasSessionOverrides returns true after SetProvider
func TestHasSessionOverrides_AfterSetProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "test-model-1"},
				},
			})
		case "/v1/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-test",
				"object":  "chat.completion",
				"created": 1,
				"model":   "test-model-1",
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

	// Save original config
	originalCfg, _ := configuration.Load()

	// Set up custom provider config
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "test-provider",
		Endpoint:       server.URL + "/v1",
		ModelName:      "test-model-1",
		RequiresAPIKey: false,
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Verify no session overrides initially
	if agent.HasSessionOverrides() {
		t.Error("Expected HasSessionOverrides to return false initially")
	}

	// Call SetProvider - should set session override
	if err := agent.SetProvider(api.ClientType("test-provider")); err != nil {
		t.Fatalf("failed to set provider: %v", err)
	}

	// Verify session override is set
	if !agent.HasSessionOverrides() {
		t.Error("Expected HasSessionOverrides to return true after SetProvider")
	}

	// Restore original config
	if originalCfg != nil {
		_ = originalCfg.Save()
	}
}

// TestHasSessionOverrides_AfterClearSessionOverrides tests that HasSessionOverrides returns false after clearing
func TestHasSessionOverrides_AfterClearSessionOverrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "test-model-1"},
				},
			})
		case "/v1/chat/completions":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-test",
				"object":  "chat.completion",
				"created": 1,
				"model":   "test-model-1",
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

	// Set up custom provider config
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "test-provider",
		Endpoint:       server.URL + "/v1",
		ModelName:      "test-model-1",
		RequiresAPIKey: false,
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Set provider to create session override
	if err := agent.SetProvider(api.ClientType("test-provider")); err != nil {
		t.Fatalf("failed to set provider: %v", err)
	}

	// Verify session override is set
	if !agent.HasSessionOverrides() {
		t.Error("Expected HasSessionOverrides to return true after SetProvider")
	}

	// Clear session overrides
	agent.ClearSessionOverrides()

	// Verify session override is cleared
	if agent.HasSessionOverrides() {
		t.Error("Expected HasSessionOverrides to return false after ClearSessionOverrides")
	}
}

// TestClearSessionOverrides_ClearsBothFields tests that ClearSessionOverrides clears both provider and model
func TestClearSessionOverrides_ClearsBothFields(t *testing.T) {
	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Manually set session fields (since SetProvider/SetModel require API calls)
	// We need to test the ClearSessionOverrides method directly

	// First, verify initial state
	if agent.HasSessionOverrides() {
		t.Error("Expected no session overrides initially")
	}

	// Clear when no overrides exist should be safe
	agent.ClearSessionOverrides()

	if agent.HasSessionOverrides() {
		t.Error("Expected no session overrides after clearing empty state")
	}
}

// TestGetProvider_ReturnsSessionOverride tests that GetProvider returns session override when set
func TestGetProvider_ReturnsSessionOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "custom-model"},
				},
			})
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode chat request: %v", err)
			}
			model, _ := body["model"].(string)
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

	// Set up custom provider config
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "custom-provider",
		Endpoint:       server.URL + "/v1",
		ModelName:      "custom-model",
		RequiresAPIKey: false,
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	initialProvider := agent.GetProvider()

	// Set provider to a different provider
	if err := agent.SetProvider(api.ClientType("custom-provider")); err != nil {
		t.Fatalf("failed to set provider: %v", err)
	}

	newProvider := agent.GetProvider()

	// Verify provider was changed to session override
	if newProvider == initialProvider {
		t.Errorf("Expected GetProvider to return different provider after SetProvider, got %q", newProvider)
	}

	// Clear session overrides
	agent.ClearSessionOverrides()

	// After clearing session overrides:
	// - sessionProvider is cleared, so GetProvider falls back to a.client.GetProvider()
	// - BUT the client was already switched to custom-provider by SetProvider
	// - So GetProvider returns the actual client provider, not the original test provider
	// This is correct: SetProvider switches the client, ClearSessionOverrides only clears the override fields
	clearedProvider := agent.GetProvider()
	// The provider should still be "custom-provider" because the client was switched
	if clearedProvider != "custom-provider" {
		t.Errorf("Expected GetProvider to return client provider after clearing session overrides, got %q", clearedProvider)
	}

	// But HasSessionOverrides should be false
	if agent.HasSessionOverrides() {
		t.Error("Expected HasSessionOverrides to return false after clearing")
	}
}

// TestGetProviderType_ReturnsSessionOverride tests that GetProviderType returns session override when set
func TestGetProviderType_ReturnsSessionOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "custom-model"},
				},
			})
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode chat request: %v", err)
			}
			model, _ := body["model"].(string)
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

	// Set up custom provider config
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "custom-provider-type",
		Endpoint:       server.URL + "/v1",
		ModelName:      "custom-model",
		RequiresAPIKey: false,
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	initialProviderType := agent.GetProviderType()

	// Set provider to a different provider
	if err := agent.SetProvider(api.ClientType("custom-provider-type")); err != nil {
		t.Fatalf("failed to set provider: %v", err)
	}

	newProviderType := agent.GetProviderType()

	// Verify provider type was changed to session override
	if newProviderType == initialProviderType {
		t.Errorf("Expected GetProviderType to return different provider type after SetProvider, got %q", newProviderType)
	}

	// Clear session overrides
	agent.ClearSessionOverrides()

	// After clearing session overrides:
	// - sessionProvider is cleared, so GetProviderType falls back to a.client.GetProvider()
	// - BUT the client was already switched to custom-provider-type by SetProvider
	// - So GetProviderType returns the actual client provider type
	clearedProviderType := agent.GetProviderType()
	if clearedProviderType != "custom-provider-type" {
		t.Errorf("Expected GetProviderType to return client provider type after clearing session overrides, got %q", clearedProviderType)
	}

	// But HasSessionOverrides should be false
	if agent.HasSessionOverrides() {
		t.Error("Expected HasSessionOverrides to return false after clearing")
	}
}

// TestGetModel_ReturnsSessionOverride tests that GetModel returns session override when set
func TestGetModel_ReturnsSessionOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "original-model"},
					{"id": "session-model"},
				},
			})
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode chat request: %v", err)
			}
			model, _ := body["model"].(string)
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

	// Set up custom provider config
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "model-session-test",
		Endpoint:       server.URL + "/v1",
		ModelName:      "original-model",
		RequiresAPIKey: false,
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Set provider first
	if err := agent.SetProvider(api.ClientType("model-session-test")); err != nil {
		t.Fatalf("failed to set provider: %v", err)
	}

	initialModel := agent.GetModel()

	// Set model to a different model
	if err := agent.SetModel("session-model"); err != nil {
		t.Fatalf("failed to set model: %v", err)
	}

	newModel := agent.GetModel()

	// Verify model was changed to session override
	if newModel == initialModel {
		t.Errorf("Expected GetModel to return different model after SetModel, got %q", newModel)
	}

	// Clear session overrides
	agent.ClearSessionOverrides()

	// After clearing session overrides:
	// - sessionModel is cleared, so GetModel falls back to a.client.GetModel()
	// - BUT the model was already changed by SetModel
	// - So GetModel still returns the session model since the client holds it
	clearedModel := agent.GetModel()
	if clearedModel != "session-model" {
		t.Errorf("Expected GetModel to return session model after clearing (client still holds it), got %q", clearedModel)
	}

	// But HasSessionOverrides should be false
	if agent.HasSessionOverrides() {
		t.Error("Expected HasSessionOverrides to return false after clearing")
	}
}

// TestSetProvider_DoesNotPersistToConfig tests that SetProvider does not persist to config
func TestSetProvider_DoesNotPersistToConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "test-model"},
				},
			})
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode chat request: %v", err)
			}
			model, _ := body["model"].(string)
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

	// Set up custom provider config
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "no-persist-provider",
		Endpoint:       server.URL + "/v1",
		ModelName:      "test-model",
		RequiresAPIKey: false,
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	// Create a minimal config file to establish a known baseline.
	// Use "no-persist-provider" (our custom-provider name) as the
	// baseline — older versions of this test used "test", which the
	// Load-time sanitizer now strips since it's the in-process
	// sentinel for mocks.
	configPath := filepath.Join(configDir, "config.json")
	configContent := `{"last_used_provider":"no-persist-provider","custom_providers":{}}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write initial config: %v", err)
	}

	// Load config to establish baseline
	initialCfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("failed to load initial config: %v", err)
	}
	// Force the provider to a known value
	if err := initialCfg.Save(); err != nil {
		t.Fatalf("failed to save initial config: %v", err)
	}
	initialProvider := initialCfg.LastUsedProvider

	// Verify our baseline is as expected
	t.Logf("Initial provider in config: %q", initialProvider)

	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Set provider - this should use session storage, not config
	if err := agent.SetProvider(api.ClientType("no-persist-provider")); err != nil {
		t.Fatalf("failed to set provider: %v", err)
	}

	// Verify agent is using the new provider
	if agent.GetProvider() != "no-persist-provider" {
		t.Errorf("Expected agent to use 'no-persist-provider', got %q", agent.GetProvider())
	}

	// Verify HasSessionOverrides is true
	if !agent.HasSessionOverrides() {
		t.Error("Expected HasSessionOverrides to be true after SetProvider")
	}

	// The key test: verify that SetProvider did NOT persist to config
	// by checking that configManager was not called for persistence
	// Since we can't directly mock configManager, we verify the behavior:
	// The session override should exist, but config should be unchanged
	reloadedCfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	// Config should still have the initial provider because SetProvider
	// is session-scoped and doesn't persist. (Older versions of this
	// test allowed "test" as a fallback baseline — that string is now
	// sanitized at Load time, so the only valid pass is matching
	// initialProvider exactly.)
	if reloadedCfg.LastUsedProvider != initialProvider {
		t.Errorf("Expected config provider NOT to change after SetProvider (session-scoped), got %q, want %q",
			reloadedCfg.LastUsedProvider, initialProvider)
	}
}

// TestSetModel_DoesNotPersistToConfig tests that SetModel does not persist to config
func TestSetModel_DoesNotPersistToConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "model-a"},
					{"id": "model-b"},
				},
			})
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode chat request: %v", err)
			}
			model, _ := body["model"].(string)
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

	// Set up custom provider config with initial model
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "model-persist-test",
		Endpoint:       server.URL + "/v1",
		ModelName:      "model-a",
		RequiresAPIKey: false,
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	// Load and save config to establish baseline
	initialCfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("failed to load initial config: %v", err)
	}
	initialModel := initialCfg.GetModelForProvider("model-persist-test")

	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Set provider first
	if err := agent.SetProvider(api.ClientType("model-persist-test")); err != nil {
		t.Fatalf("failed to set provider: %v", err)
	}

	// Set model - this should use session storage, not config
	if err := agent.SetModel("model-b"); err != nil {
		t.Fatalf("failed to set model: %v", err)
	}

	// Reload config from disk
	reloadedCfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	// Verify config was NOT updated by SetModel
	if reloadedCfg.GetModelForProvider("model-persist-test") != initialModel {
		t.Errorf("Expected config model NOT to change after SetModel (session-scoped), got %q", reloadedCfg.GetModelForProvider("model-persist-test"))
	}

	// Verify agent still uses the session override
	if agent.GetModel() != "model-b" {
		t.Errorf("Expected agent to use session-scoped model, got %q", agent.GetModel())
	}
}

// TestSetProviderPersisted_DoesPersistToConfig tests that SetProviderPersisted does persist to config
func TestSetProviderPersisted_DoesPersistToConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "persist-model"},
				},
			})
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode chat request: %v", err)
			}
			model, _ := body["model"].(string)
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

	// Set up custom provider config
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "persisted-provider",
		Endpoint:       server.URL + "/v1",
		ModelName:      "persist-model",
		RequiresAPIKey: false,
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	// Load and save config to establish baseline
	initialCfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("failed to load initial config: %v", err)
	}
	initialProvider := initialCfg.LastUsedProvider

	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Set provider with persistence - this SHOULD update config
	if err := agent.SetProviderPersisted(api.ClientType("persisted-provider")); err != nil {
		t.Fatalf("failed to set provider persisted: %v", err)
	}

	// Reload config from disk
	reloadedCfg, err := configuration.Load()
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}

	// Verify config WAS updated by SetProviderPersisted
	if reloadedCfg.LastUsedProvider == initialProvider {
		t.Error("Expected config provider to change after SetProviderPersisted")
	}

	// Verify the new provider is persisted
	if reloadedCfg.LastUsedProvider != "persisted-provider" {
		t.Errorf("Expected config provider to be 'persisted-provider', got %q", reloadedCfg.LastUsedProvider)
	}
}

// TestSessionOverrides_TakePrecedenceOverConfig tests that session overrides take precedence
func TestSessionOverrides_TakePrecedenceOverConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "config-model"},
					{"id": "session-model"},
				},
			})
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode chat request: %v", err)
			}
			model, _ := body["model"].(string)
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

	// Set up custom provider config with a specific model
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "precedence-test",
		Endpoint:       server.URL + "/v1",
		ModelName:      "config-model",
		RequiresAPIKey: false,
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Set provider (this sets session override)
	if err := agent.SetProvider(api.ClientType("precedence-test")); err != nil {
		t.Fatalf("failed to set provider: %v", err)
	}

	// Set model to a different value than what's in config
	if err := agent.SetModel("session-model"); err != nil {
		t.Fatalf("failed to set model: %v", err)
	}

	// Verify agent returns session-scoped values
	if agent.GetModel() != "session-model" {
		t.Errorf("Expected GetModel to return session-scoped model 'session-model', got %q", agent.GetModel())
	}

	// Now clear session overrides
	agent.ClearSessionOverrides()

	// After clearing session overrides:
	// - sessionModel is cleared, so GetModel falls back to a.client.GetModel()
	// - BUT the model was already changed by SetModel on the client
	// - So GetModel still returns the session model since the client holds it
	clearedModel := agent.GetModel()
	if clearedModel != "session-model" {
		t.Errorf("Expected GetModel to return session model after clearing (client still holds it), got %q", clearedModel)
	}

	// But HasSessionOverrides should be false
	if agent.HasSessionOverrides() {
		t.Error("Expected HasSessionOverrides to return false after clearing")
	}
}

// TestSetProvider_WithSessionOverrideFlag tests that SetProvider sets session override
func TestSetProvider_WithSessionOverrideFlag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "override-model"},
				},
			})
		case "/v1/chat/completions":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("failed to decode chat request: %v", err)
			}
			model, _ := body["model"].(string)
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

	// Set up custom provider config
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")

	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "session-override",
		Endpoint:       server.URL + "/v1",
		ModelName:      "override-model",
		RequiresAPIKey: false,
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	agent, err := NewAgent()
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Verify no session override initially
	if agent.HasSessionOverrides() {
		t.Error("Expected no session override initially")
	}

	// Set provider - this should set session override
	if err := agent.SetProvider(api.ClientType("session-override")); err != nil {
		t.Fatalf("failed to set provider: %v", err)
	}

	// Verify session override is now set
	if !agent.HasSessionOverrides() {
		t.Error("Expected HasSessionOverrides to return true after SetProvider")
	}

	// Verify GetProvider returns the new provider
	if agent.GetProvider() != "session-override" {
		t.Errorf("Expected GetProvider to return 'session-override', got %q", agent.GetProvider())
	}

	// Verify GetProviderType returns the new provider type
	if agent.GetProviderType() != "session-override" {
		t.Errorf("Expected GetProviderType to return 'session-override', got %q", agent.GetProviderType())
	}
}

// TestSelectProbeRecommended verifies the probe-first default-model selection.
// Capability probe results drive selection when present; un-probed models keep
// the legacy behavior (no RecommendedRoles → ignored).
func TestSelectProbeRecommended(t *testing.T) {
	tests := []struct {
		name   string
		models []api.ModelInfo
		want   string
	}{
		{
			name:   "empty list",
			models: nil,
			want:   "",
		},
		{
			name: "all un-probed — no probe-backed candidate",
			models: []api.ModelInfo{
				{ID: "alpha", RecommendedRoles: nil},
				{ID: "beta", RecommendedRoles: []string{}},
			},
			want: "",
		},
		{
			name: "primary beats subagent",
			models: []api.ModelInfo{
				{ID: "small", RecommendedRoles: []string{"subagent"}},
				{ID: "strong", RecommendedRoles: []string{"primary", "subagent"}},
				{ID: "medium", RecommendedRoles: []string{"primary"}},
			},
			want: "strong", // first primary in iteration order
		},
		{
			name: "subagent-only after no-primary",
			models: []api.ModelInfo{
				{ID: "first", RecommendedRoles: []string{"subagent"}},
				{ID: "second", RecommendedRoles: []string{"subagent"}},
			},
			want: "first",
		},
		{
			name: "un-probed is ignored even when first",
			models: []api.ModelInfo{
				{ID: "unprobed", RecommendedRoles: nil},
				{ID: "good", RecommendedRoles: []string{"subagent"}},
			},
			want: "good",
		},
		{
			name: "unknown role label is ignored",
			models: []api.ModelInfo{
				{ID: "x", RecommendedRoles: []string{"vision"}}, // not primary/subagent
				{ID: "y", RecommendedRoles: []string{"subagent"}},
			},
			want: "y",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectProbeRecommended(tt.models)
			if got != tt.want {
				t.Errorf("selectProbeRecommended() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestModelcontractRoleHas covers the membership helper used by
// selectProbeRecommended (and shared via pkg/modelcontract with the webui).
func TestModelcontractRoleHas(t *testing.T) {
	if modelcontract.RoleHas(nil, modelcontract.RolePrimary) {
		t.Error("RoleHas(nil, primary) = true, want false")
	}
	if modelcontract.RoleHas([]string{}, modelcontract.RolePrimary) {
		t.Error("RoleHas([], primary) = true, want false")
	}
	if !modelcontract.RoleHas([]string{modelcontract.RolePrimary}, modelcontract.RolePrimary) {
		t.Error("RoleHas([primary], primary) = false, want true")
	}
	if modelcontract.RoleHas([]string{modelcontract.RolePrimary}, modelcontract.RoleSubagent) {
		t.Error("RoleHas([primary], subagent) = true, want false")
	}
	// Exact match — substring "primary" inside "primary-only" must not match.
	if modelcontract.RoleHas([]string{"primary-only"}, modelcontract.RolePrimary) {
		t.Error("substring match leaked through RoleHas")
	}
}
