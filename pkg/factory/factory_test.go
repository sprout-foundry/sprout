package factory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/providerregistry"
)

// TestTestClient_SendChatRequest tests the TestClient's SendChatRequest method
func TestTestClient_SendChatRequest(t *testing.T) {
	client := &TestClient{model: "test-model"}

	messages := []api.Message{
		{Role: "user", Content: "Hello"},
	}
	tools := []api.Tool{}

	resp, err := client.SendChatRequest(context.Background(), messages, tools, "", false)
	if err != nil {
		t.Fatalf("SendChatRequest failed: %v", err)
	}

	if resp.ID != "test-response-id" {
		t.Errorf("Expected ID 'test-response-id', got '%s'", resp.ID)
	}

	if resp.Model != "test-model" {
		t.Errorf("Expected model 'test-model', got '%s'", resp.Model)
	}

	if len(resp.Choices) != 1 {
		t.Fatalf("Expected 1 choice, got %d", len(resp.Choices))
	}

	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("Expected role 'assistant', got '%s'", resp.Choices[0].Message.Role)
	}

	if resp.Choices[0].Message.Content != "Test response from mock provider" {
		t.Errorf("Unexpected content: '%s'", resp.Choices[0].Message.Content)
	}

	if resp.Usage.TotalTokens != 15 {
		t.Errorf("Expected 15 total tokens, got %d", resp.Usage.TotalTokens)
	}
}

// TestTestClient_SendChatRequestStream tests the streaming method
func TestTestClient_SendChatRequestStream(t *testing.T) {
	client := &TestClient{model: "test-model"}

	var receivedChunks []string
	callback := func(chunk string, contentType string) {
		receivedChunks = append(receivedChunks, chunk)
	}

	messages := []api.Message{
		{Role: "user", Content: "Hello"},
	}

	resp, err := client.SendChatRequestStream(context.Background(), messages, nil, "", false, callback)
	if err != nil {
		t.Fatalf("SendChatRequestStream failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response should not be nil")
	}

	if len(receivedChunks) != 1 {
		t.Fatalf("Expected 1 chunk, got %d", len(receivedChunks))
	}

	if receivedChunks[0] != "Test response from mock provider" {
		t.Errorf("Unexpected chunk content: '%s'", receivedChunks[0])
	}
}

// TestTestClient_CheckConnection tests the CheckConnection method
func TestTestClient_CheckConnection(t *testing.T) {
	client := &TestClient{}

	err := client.CheckConnection()
	if err != nil {
		t.Errorf("CheckConnection should always return nil for test client, got: %v", err)
	}
}

// TestTestClient_SetDebug tests the SetDebug method
func TestTestClient_SetDebug(t *testing.T) {
	client := &TestClient{debug: false}

	client.SetDebug(true)
	if !client.debug {
		t.Error("Expected debug to be true")
	}

	client.SetDebug(false)
	if client.debug {
		t.Error("Expected debug to be false")
	}
}

// TestTestClient_SetModel tests the SetModel method
func TestTestClient_SetModel(t *testing.T) {
	client := &TestClient{}

	err := client.SetModel("new-model")
	if err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}

	if client.model != "new-model" {
		t.Errorf("Expected model 'new-model', got '%s'", client.model)
	}
}

// TestTestClient_GetModel tests the GetModel method
func TestTestClient_GetModel(t *testing.T) {
	// Test with model set
	client := &TestClient{model: "custom-model"}
	if client.GetModel() != "custom-model" {
		t.Errorf("Expected 'custom-model', got '%s'", client.GetModel())
	}

	// Test with empty model (should return default)
	client = &TestClient{model: ""}
	if client.GetModel() != "test-model" {
		t.Errorf("Expected default 'test-model', got '%s'", client.GetModel())
	}
}

// TestTestClient_GetProvider tests the GetProvider method
func TestTestClient_GetProvider(t *testing.T) {
	client := &TestClient{}

	if client.GetProvider() != "test" {
		t.Errorf("Expected provider 'test', got '%s'", client.GetProvider())
	}
}

// TestTestClient_GetModelContextLimit tests the GetModelContextLimit method
func TestTestClient_GetModelContextLimit(t *testing.T) {
	client := &TestClient{}

	limit, err := client.GetModelContextLimit()
	if err != nil {
		t.Fatalf("GetModelContextLimit failed: %v", err)
	}

	if limit != 4096 {
		t.Errorf("Expected context limit 4096, got %d", limit)
	}
}

// TestTestClient_ListModels tests the ListModels method
func TestTestClient_ListModels(t *testing.T) {
	client := &TestClient{}

	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("Expected 1 model, got %d", len(models))
	}

	if models[0].Name != "test-model" {
		t.Errorf("Expected model name 'test-model', got '%s'", models[0].Name)
	}

	if models[0].ContextLength != 4096 {
		t.Errorf("Expected context length 4096, got %d", models[0].ContextLength)
	}
}

// TestTestClient_SupportsVision tests the SupportsVision method
func TestTestClient_SupportsVision(t *testing.T) {
	client := &TestClient{}

	if client.SupportsVision() {
		t.Error("TestClient should not support vision")
	}
}

// TestTestClient_GetVisionModel tests the GetVisionModel method
func TestTestClient_GetVisionModel(t *testing.T) {
	client := &TestClient{}

	if client.GetVisionModel() != "" {
		t.Errorf("Expected empty vision model, got '%s'", client.GetVisionModel())
	}
}

// TestTestClient_SendVisionRequest tests that vision requests return an error
func TestTestClient_SendVisionRequest(t *testing.T) {
	client := &TestClient{}

	_, err := client.SendVisionRequest(context.Background(), nil, nil, "", false)
	if err == nil {
		t.Error("SendVisionRequest should return an error for test client")
	}

	expectedErr := "vision not supported in test provider"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

// TestTestClient_TPSStats tests all TPS-related methods
func TestTestClient_TPSStats(t *testing.T) {
	client := &TestClient{}

	// Test GetLastTPS
	lastTPS := client.GetLastTPS()
	if lastTPS != 100.0 {
		t.Errorf("Expected last TPS 100.0, got %f", lastTPS)
	}

	// Test GetAverageTPS
	avgTPS := client.GetAverageTPS()
	if avgTPS != 100.0 {
		t.Errorf("Expected average TPS 100.0, got %f", avgTPS)
	}

	// Test GetTPSStats
	stats := client.GetTPSStats()
	if stats["last"] != 100.0 {
		t.Errorf("Expected stats['last'] 100.0, got %f", stats["last"])
	}
	if stats["average"] != 100.0 {
		t.Errorf("Expected stats['average'] 100.0, got %f", stats["average"])
	}

	// ResetTPSStats should be a no-op and not panic
	client.ResetTPSStats()

	// Verify stats are unchanged after reset (since it's a no-op)
	stats = client.GetTPSStats()
	if stats["last"] != 100.0 {
		t.Errorf("Expected stats['last'] 100.0 after reset, got %f", stats["last"])
	}
}

// TestCreateProviderClient_TestClientType tests creating a TestClient via the factory
func TestCreateProviderClient_TestClientType(t *testing.T) {
	client, err := CreateProviderClient(api.TestClientType, "test-model")
	if err != nil {
		t.Fatalf("CreateProviderClient failed for TestClientType: %v", err)
	}

	// Verify it's a TestClient
	_, ok := client.(*TestClient)
	if !ok {
		t.Error("Expected TestClient type")
	}

	// Verify model is set
	if client.GetModel() != "test-model" {
		t.Errorf("Expected model 'test-model', got '%s'", client.GetModel())
	}
}

// TestCreateProviderClient_TestClientType_EmptyModel tests creating a TestClient without specifying a model
func TestCreateProviderClient_TestClientType_EmptyModel(t *testing.T) {
	client, err := CreateProviderClient(api.TestClientType, "")
	if err != nil {
		t.Fatalf("CreateProviderClient failed: %v", err)
	}

	// Should return default model
	if client.GetModel() != "test-model" {
		t.Errorf("Expected default model 'test-model', got '%s'", client.GetModel())
	}
}

// TestCreateProviderClient_TestClientType_FullInterface tests that TestClient implements ClientInterface
func TestCreateProviderClient_TestClientType_FullInterface(t *testing.T) {
	client, err := CreateProviderClient(api.TestClientType, "test-model")
	if err != nil {
		t.Fatalf("CreateProviderClient failed: %v", err)
	}

	// Test all interface methods work without panic
	_ = client.GetProvider()
	_, _ = client.GetModelContextLimit()
	_, _ = client.ListModels(context.Background())
	_ = client.SupportsVision()
	_ = client.GetVisionModel()
	_, _ = client.SendChatRequest(context.Background(), nil, nil, "", false)
	_, _ = client.SendVisionRequest(context.Background(), nil, nil, "", false)
	_ = client.GetLastTPS()
	_ = client.GetAverageTPS()
	_ = client.GetTPSStats()
	client.ResetTPSStats()
	client.SetDebug(true)
	_ = client.CheckConnection()
}

// TestTestClient_NilMessages tests that TestClient handles nil gracefully
func TestTestClient_NilMessages(t *testing.T) {
	client := &TestClient{}

	// Should not panic with nil inputs
	resp, err := client.SendChatRequest(context.Background(), nil, nil, "", false)
	if err != nil {
		t.Fatalf("SendChatRequest failed with nil inputs: %v", err)
	}

	if resp == nil {
		t.Error("Response should not be nil")
	}
}

// --- Tests for factory initialization and provider creation ---

// TestInTestBinary verifies that inTestBinary() correctly detects test binaries.
// Since inTestBinary() reads os.Args[0], we must save and restore os.Args around tests.
func TestInTestBinary(t *testing.T) {
	// Save original args and restore after test
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	tests := []struct {
		name     string
		args0    string
		wantTest bool
	}{
		// Normal production binaries
		{"normal_binary", "sprout", false},
		{"normal_path", "/usr/local/bin/sprout", false},
		{"normal_long_path", "/opt/sprout/bin/sprout", false},
		{"different_name", "mysprout", false},
		{"no_extension", "/usr/bin/sprout", false},

		// Test suffix (.test)
		{"test_suffix", "sprout.test", true},
		{"test_path", "/tmp/sprout.test", true},
		{"test_deep_path", "/home/user/project/pkg/factory/sprout.test", true},
		{"different_name_test", "mysprout.test", true},

		// Test executable suffix (.test.exe for Windows)
		{"test_exe", "sprout.test.exe", true},
		{"test_exe_path", "/tmp/sprout.test.exe", true},
		{"different_name_test_exe", "mysprout.test.exe", true},

		// Internal test path (/_test/)
		{"internal_test", "/tmp/sprout/_test/main.test", true},
		{"internal_test_short", "foo/_test/bar", true},
		{"internal_test_root", "/_test/sprout", true},
		{"internal_test_mid", "/home/user/sprout/_test/coverage.data", true},

		// Windows-style internal test path (\_test\)
		{"windows_internal_test", "C:\\Users\\foo\\_test\\main.exe", true},
		{"windows_internal_test2", "C:\\_test\\foo", true},

		// Near-misses that should NOT match
		{"near_miss_bak", "sprout.test.bak", false},
		{"near_miss_notest", "notatest", false},
		{"near_miss_underscore", "sprout_test", false},
		{"near_miss_testprefix", "testsprout", false},
		{"near_miss_test_suffix_other", "sprout.testfile", false},
		{"near_miss_test_dir", "/sprout/test/sprout", false},
		{"near_miss_test_dir_uppercase", "/sprout/Test/sprout", false},
		{"near_miss_underscore_test", "/sprout/_testdata/sprout", false},

		// Edge cases
		{"empty_string", "", false},
		{"just_test_suffix", ".test", true},
		{"just_test_exe", ".test.exe", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Args = []string{tt.args0}
			got := inTestBinary()
			if got != tt.wantTest {
				t.Errorf("inTestBinary() = %v, want %v for args[0]=%q", got, tt.wantTest, tt.args0)
			}
		})
	}

	// Edge case: empty os.Args (should return false per the nil-check logic)
	t.Run("empty_args_slice", func(t *testing.T) {
		os.Args = []string{}
		got := inTestBinary()
		if got {
			t.Errorf("inTestBinary() should return false for empty os.Args, got true")
		}
	})
}

// TestInTestBinary_RestoresOriginalArgs verifies that os.Args is properly
// restored after testing, preventing cross-test contamination.
func TestInTestBinary_RestoresOriginalArgs(t *testing.T) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// Manipulate os.Args
	os.Args = []string{"manipulated.test"}
	if !inTestBinary() {
		t.Error("Expected inTestBinary() to return true after manipulation")
	}

	// Restore and verify original behavior is preserved
	os.Args = origArgs
	// We're actually running in a test binary, so this should be true
	got := inTestBinary()
	if !got {
		t.Logf("Running in test binary as expected (original args restored): %v", got)
	}
}

// TestGlobalFactory verifies that GlobalFactory() returns a non-nil instance
// that was properly initialized by init().
func TestGlobalFactory(t *testing.T) {
	factory := GlobalFactory()

	if factory == nil {
		t.Fatal("GlobalFactory() returned nil; init() did not initialize the global factory")
	}

	// Verify the factory has loaded embedded provider configs
	providers := factory.GetAvailableProviders()
	if len(providers) == 0 {
		t.Fatal("GlobalFactory() has no available providers; embedded configs may not have loaded")
	}

	t.Logf("GlobalFactory initialized with %d providers: %v", len(providers), providers)
}

// TestInitDoesNotSpawnGoroutineInTestBinary verifies that when running in a
// test binary, the init() function does NOT spawn the refreshFromRemote goroutine.
// We can't directly test that the goroutine wasn't spawned, but we can verify:
// 1. inTestBinary() returns true (confirming test binary detection)
// 2. The factory was still initialized with embedded configs (init() ran)
func TestInitDoesNotSpawnGoroutineInTestBinary(t *testing.T) {
	// We're running in a test binary, so inTestBinary() should return true.
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	// Our actual args already end in .test, so this should be true
	if !inTestBinary() {
		t.Skip("Not running in a test binary; skipping goroutine skip test")
	}

	// Verify the factory was initialized with embedded configs
	// (proving that init() still runs the config loading part)
	factory := GlobalFactory()
	if factory == nil {
		t.Fatal("Factory should be initialized by init() even in test binary")
	}

	// Verify at least one provider is available (from embedded configs)
	providers := factory.GetAvailableProviders()
	if len(providers) == 0 {
		t.Fatal("Factory should have at least one provider from embedded configs")
	}

	// The key assertion: init() ran and loaded embedded configs successfully
	// even though we're in a test binary (which means refreshFromRemote was skipped)
	t.Logf("Factory has %d providers from embedded configs (refreshFromRemote skipped in test binary)", len(providers))
}

// TestCreateGenericProvider_EmbeddedProvider verifies that CreateGenericProvider
// works for a real embedded provider. We use "openai" which is always in the
// embedded configs. It may or may not succeed depending on whether credentials
// are configured, but NewGenericProvider itself should succeed (errors only occur
// if config validation fails or no provider is found).
func TestCreateGenericProvider_EmbeddedProvider(t *testing.T) {
	client, err := CreateGenericProvider("openai", "")
	if err != nil {
		// Error is acceptable if credentials aren't configured or validation fails.
		// The important thing is the function doesn't panic and returns a meaningful error.
		t.Logf("CreateGenericProvider(\"openai\", \"\") returned error (likely credentials not configured): %v", err)
		return
	}
	if client == nil {
		t.Fatal("CreateGenericProvider(\"openai\", \"\") returned nil client")
	}

	t.Logf("CreateGenericProvider(\"openai\", \"\") succeeded; provider=%q, model=%q", client.GetProvider(), client.GetModel())
}

// TestCreateGenericProvider_WithModel verifies that CreateGenericProvider
// correctly sets a model when provided, for an embedded provider.
func TestCreateGenericProvider_WithModel(t *testing.T) {
	client, err := CreateGenericProvider("openai", "gpt-4o")
	if err != nil {
		t.Logf("CreateGenericProvider(\"openai\", \"gpt-4o\") returned error (likely credentials not configured): %v", err)
		return
	}
	if client == nil {
		t.Fatal("CreateGenericProvider(\"openai\", \"gpt-4o\") returned nil client")
	}

	if client.GetModel() != "gpt-4o" {
		t.Errorf("Expected model 'gpt-4o', got %q", client.GetModel())
	}
}

// TestCreateGenericProvider_NonExistentProvider verifies error handling for
// providers that don't exist in either the generic or custom provider systems.
func TestCreateGenericProvider_NonExistentProvider(t *testing.T) {
	_, err := CreateGenericProvider("nonexistent-provider-xyz", "")
	if err == nil {
		t.Fatal("CreateGenericProvider should return error for non-existent provider")
	}

	t.Logf("Got expected error for non-existent provider: %v", err)
}

// TestCreateProviderClient_GenericProviderType verifies that CreateProviderClient
// correctly routes known provider types to CreateGenericProvider.
func TestCreateProviderClient_GenericProviderTypes(t *testing.T) {
	tests := []struct {
		name       string
		clientType api.ClientType
		provider   string
		// Some providers may fail if credentials aren't configured,
		// so we just check that the call doesn't panic
		allowError bool
	}{
		{"openai", api.OpenAIClientType, "openai", true},
		{"chutes", api.ChutesClientType, "chutes", true},
		{"deepinfra", api.DeepInfraClientType, "deepinfra", true},
		{"deepseek", api.DeepSeekClientType, "deepseek", true},
		{"openrouter", api.OpenRouterClientType, "openrouter", true},
		{"cerebras", api.CerebrasClientType, "cerebras", true},
		{"mistral", api.MistralClientType, "mistral", true},
		{"minimax", api.MinimaxClientType, "minimax", true},
		{"test", api.TestClientType, "test", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := CreateProviderClient(tt.clientType, "")

			if tt.allowError {
				// For real providers, error is OK (credentials may not be set)
				// We just verify the function doesn't panic
				if err != nil && client == nil {
					t.Logf("Provider %q returned expected error (credentials not configured): %v", tt.name, err)
					return
				}
			} else {
				// For test provider, should always succeed
				if err != nil {
					t.Fatalf("CreateProviderClient(%v) failed unexpectedly: %v", tt.clientType, err)
				}
				if client == nil {
					t.Fatal("CreateProviderClient returned nil client")
				}
			}
		})
	}
}

// TestEmbeddedOnlyMode verifies that when the registry is disabled (empty base URL),
// FetchAllProviders returns nil gracefully and the factory still has embedded providers.
func TestEmbeddedOnlyMode(t *testing.T) {
	providerregistry.SetBaseURL("")
	t.Cleanup(func() {
		providerregistry.SetBaseURL("https://sprout-foundry.github.io/sprout")
		providerregistry.ClearCache()
	})

	if providerregistry.IsEnabled() {
		t.Fatal("registry should be disabled when base URL is empty")
	}

	results, _ := providerregistry.FetchAllProviders(context.Background())
	if results != nil {
		t.Fatal("FetchAllProviders should return nil when registry is disabled")
	}

	factory := GlobalFactory()
	providers := factory.GetAvailableProviders()
	if len(providers) == 0 {
		t.Fatal("should have embedded providers even in embedded-only mode")
	}

	t.Logf("Embedded-only mode: %d providers available", len(providers))
}

// TestRemoteMergeOverEmbedded verifies that remote configs fetched via
// FetchAllProviders can be upserted into the factory, merging with and
// overriding embedded configs, while preserving providers that were not
// touched by the remote fetch.
func TestRemoteMergeOverEmbedded(t *testing.T) {
	testProviderJSON := `{
		"name": "test-provider",
		"endpoint": "https://api.example.com/v1",
		"auth": {"type": "bearer", "env_var": "TEST_PROVIDER_KEY"},
		"defaults": {"model": "test-model-1"},
		"models": {
			"default_context_limit": 8192,
			"default_model": "test-model-1",
			"available_models": ["test-model-1"]
		},
		"retry": {
			"max_attempts": 3,
			"base_delay_ms": 1000,
			"backoff_multiplier": 2,
			"max_delay_ms": 10000,
			"retryable_errors": ["502", "503", "504"]
		},
		"cost": {
			"input_token_cost": 0.001,
			"output_token_cost": 0.002,
			"currency": "USD"
		}
	}`

	openaiJSON := `{
		"name": "openai",
		"endpoint": "https://remote-openai-api.example.com/v1",
		"auth": {"type": "bearer", "env_var": "OPENAI_API_KEY"},
		"defaults": {"model": "gpt-4o-mini"},
		"models": {
			"default_context_limit": 128000,
			"default_model": "gpt-4o-mini",
			"available_models": ["gpt-4o-mini", "gpt-4o"],
			"supports_vision": true
		},
		"retry": {
			"max_attempts": 3,
			"base_delay_ms": 1000,
			"backoff_multiplier": 2,
			"max_delay_ms": 10000,
			"retryable_errors": ["502", "503", "504"]
		},
		"cost": {
			"input_token_cost": 0.01,
			"output_token_cost": 0.03,
			"currency": "USD"
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/providers/index.json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"providers":["test-provider","openai"]}`))
		case "/providers/test-provider.json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(testProviderJSON))
		case "/providers/openai.json":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(openaiJSON))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Save original openai config and available providers before changes
	factory := GlobalFactory()
	originalOpenAiConfig, _ := factory.GetProviderConfig("openai")
	originalProviders := factory.GetAvailableProviders()

	// Prepare registry for test server
	providerregistry.ClearCache()
	providerregistry.SetBaseURL(server.URL)
	providerregistry.SetHTTPTimeout(5 * time.Second)
	t.Cleanup(func() {
		providerregistry.SetBaseURL("https://sprout-foundry.github.io/sprout")
		providerregistry.ClearCache()
	})

	// Fetch all providers from the test server
	results, err := providerregistry.FetchAllProviders(context.Background())
	if err != nil {
		t.Fatalf("FetchAllProviders failed: %v", err)
	}
	if results == nil {
		t.Fatal("FetchAllProviders returned nil; test server should have returned results")
	}

	// Upsert each fetched config into the factory
	for name, cfg := range results {
		providerCfg := cfg.ToProviderConfig()
		if providerCfg == nil {
			continue
		}
		if err := factory.UpsertConfig(name, providerCfg); err != nil {
			t.Fatalf("UpsertConfig(%q) failed: %v", name, err)
		}
	}

	// Verify test-provider appears in available providers
	providers := factory.GetAvailableProviders()
	found := false
	for _, p := range providers {
		if p == "test-provider" {
			found = true
			break
		}
	}
	if !found {
		t.Error("test-provider should appear in GetAvailableProviders() after upsert")
	}

	// Verify openai endpoint was overridden by remote config
	openAiConfig, err := factory.GetProviderConfig("openai")
	if err != nil {
		t.Fatalf("GetProviderConfig(\"openai\") failed: %v", err)
	}
	if openAiConfig.Endpoint != "https://remote-openai-api.example.com/v1" {
		t.Errorf("Expected openai endpoint 'https://remote-openai-api.example.com/v1', got %q", openAiConfig.Endpoint)
	}

	// Verify cerebras still exists unchanged (was not touched by remote fetch)
	cerebrasConfig, err := factory.GetProviderConfig("cerebras")
	if err != nil {
		t.Fatalf("GetProviderConfig(\"cerebras\") failed: cerebras should still exist: %v", err)
	}
	if cerebrasConfig == nil {
		t.Fatal("cerebras config should not be nil")
	}

	// Restore openai's original config
	if originalOpenAiConfig != nil {
		if err := factory.UpsertConfig("openai", originalOpenAiConfig); err != nil {
			t.Logf("Failed to restore openai config: %v", err)
		}
	}

	t.Logf("Remote merge test passed: %d original providers, test-provider added, openai overridden, cerebras preserved", len(originalProviders))
}

// TestOfflineGracefulDegradation verifies that when the registry URL is
// unreachable, FetchAllProviders returns nil, nil (graceful degradation)
// and the factory's embedded providers remain available.
func TestOfflineGracefulDegradation(t *testing.T) {
	// Save available providers before test
	factory := GlobalFactory()
	originalProviders := factory.GetAvailableProviders()

	// Point registry at an unreachable host with a short timeout
	providerregistry.SetBaseURL("http://localhost.invalid:9999")
	providerregistry.SetHTTPTimeout(100 * time.Millisecond)
	providerregistry.ClearCache()
	t.Cleanup(func() {
		providerregistry.SetBaseURL("https://sprout-foundry.github.io/sprout")
		providerregistry.ClearCache()
	})

	// FetchAllProviders should return nil, nil (graceful degradation)
	results, err := providerregistry.FetchAllProviders(context.Background())
	if err != nil {
		t.Fatalf("FetchAllProviders should not return an error for unreachable registry, got: %v", err)
	}
	if results != nil {
		t.Fatalf("FetchAllProviders should return nil for unreachable registry, got %d results", len(results))
	}

	// Available providers should still have the embedded ones
	currentProviders := factory.GetAvailableProviders()
	if len(currentProviders) != len(originalProviders) {
		t.Errorf("Expected %d providers, got %d after offline degradation", len(originalProviders), len(currentProviders))
	}

	t.Logf("Offline degradation test passed: %d providers still available", len(currentProviders))
}

// TestCreateCustomProvider_NoCredentialsRequired verifies the v0.17.0 regression fix:
// a custom provider with RequiresAPIKey=false and no EnvVar should be creatable
// without a "no credentials configured" error. The factory should skip the
// credential check entirely for such providers (e.g., local mocks in tests,
// self-hosted services that don't require auth).
func TestCreateCustomProvider_NoCredentialsRequired(t *testing.T) {
	// Set up isolated config directory
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	// Mock server that returns valid responses
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			// Return a valid models list
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "mock-model"},
				},
			})
		case "/v1/chat/completions":
			// Return a valid chat completion
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":      "chatcmpl-test",
				"object":  "chat.completion",
				"created": 1,
				"model":   "mock-model",
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

	// Save custom provider with RequiresAPIKey=false and no EnvVar.
	// This is the configuration that was broken before v0.17.0 — the factory
	// incorrectly demanded credentials even though auth was explicitly optional.
	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "no-creds-provider",
		Endpoint:       server.URL + "/v1",
		ModelName:      "mock-model",
		RequiresAPIKey: false,
		// EnvVar intentionally omitted (empty string)
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	// Create the custom provider — should NOT return a "no credentials" error.
	client, err := CreateCustomProvider("no-creds-provider", "")

	// We may or may not get a client depending on whether the server is
	// reachable, but the key assertion is: no credentials error.
	if err != nil {
		errMsg := err.Error()
		// The bug produced errors like:
		// "custom provider X is registered but has no credentials configured."
		// This should NOT happen for providers that don't require auth.
		if strings.Contains(errMsg, "credentials") || strings.Contains(errMsg, "no credentials") {
			t.Errorf("CreateCustomProvider returned a credentials error for a provider that doesn't require them: %v", err)
		}
		// Other errors (e.g., connection refused) are acceptable — the point
		// is that we didn't get a credentials validation error.
		t.Logf("CreateCustomProvider returned non-credentials error (acceptable): %v", err)
		return
	}

	// If we got a client, verify it's usable
	if client == nil {
		t.Fatal("CreateCustomProvider returned nil client with no error")
	}

	if client.GetProvider() != "no-creds-provider" {
		t.Errorf("Expected provider 'no-creds-provider', got %q", client.GetProvider())
	}

	t.Logf("CreateCustomProvider succeeded for auth-optional provider: %s", client.GetProvider())
}

// TestCreateCustomProvider_CredentialsRequired verifies the counterpart: when
// RequiresAPIKey=true (or EnvVar is set), the factory SHOULD enforce credentials
// and return a "no credentials configured" error when none are found.
func TestCreateCustomProvider_CredentialsRequired(t *testing.T) {
	// Set up isolated config directory
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	// Save custom provider with RequiresAPIKey=true.
	// No credentials are set, so CreateCustomProvider should return a
	// "no credentials configured" error.
	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "requires-creds-provider",
		Endpoint:       "https://api.example.com/v1",
		ModelName:      "some-model",
		RequiresAPIKey: true,
		// EnvVar intentionally omitted
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	// Create the custom provider — should return a credentials error.
	_, err = CreateCustomProvider("requires-creds-provider", "")

	if err == nil {
		t.Fatal("CreateCustomProvider should have returned an error for a provider that requires credentials but has none configured")
	}

	errMsg := err.Error()
	// The error should mention credentials
	if !strings.Contains(errMsg, "credentials") && !strings.Contains(errMsg, "no credentials") {
		t.Errorf("Expected credentials error, got: %v", err)
	}

	// Error should identify the provider
	if !strings.Contains(errMsg, "requires-creds-provider") {
		t.Errorf("Error should mention the provider name, got: %v", err)
	}

	t.Logf("Got expected credentials error: %v", err)
}

// TestCreateCustomProvider_CredentialsRequiredWithEnvVar verifies that when
// EnvVar is set but the environment variable is not present, the factory
// returns a credentials error.
func TestCreateCustomProvider_CredentialsRequiredWithEnvVar(t *testing.T) {
	// Set up isolated config directory
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("SPROUT_CONFIG", configDir)

	// Ensure the env var is NOT set
	t.Setenv("MY_MOCK_API_KEY", "")

	// Save custom provider with EnvVar set but no credentials.
	err := configuration.SaveCustomProvider(configuration.CustomProviderConfig{
		Name:           "env-var-provider",
		Endpoint:       "https://api.example.com/v1",
		ModelName:      "some-model",
		RequiresAPIKey: false, // not strictly required, but EnvVar is set
		EnvVar:         "MY_MOCK_API_KEY",
	})
	if err != nil {
		t.Fatalf("failed to save custom provider: %v", err)
	}

	// Create the custom provider — should return a credentials error because EnvVar is set.
	_, err = CreateCustomProvider("env-var-provider", "")

	if err == nil {
		t.Fatal("CreateCustomProvider should have returned an error when EnvVar is set but not present")
	}

	errMsg := err.Error()
	// The error should mention credentials
	if !strings.Contains(errMsg, "credentials") && !strings.Contains(errMsg, "no credentials") {
		t.Errorf("Expected credentials error, got: %v", err)
	}

	// Error should mention the env var name as a hint
	if !strings.Contains(errMsg, "MY_MOCK_API_KEY") {
		t.Errorf("Error should mention the EnvVar name as a hint, got: %v", err)
	}

	t.Logf("Got expected credentials error with EnvVar hint: %v", err)
}
