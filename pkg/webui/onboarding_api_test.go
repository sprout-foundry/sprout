package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/credentials"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test helper — builds a ReactWebServer with a live agent and config manager
// backed by a temporary directory. Reuses the same pattern as settings_api_mcp_test.go
// but with a dedicated name to avoid any future clashes.
// ---------------------------------------------------------------------------

func setupOnboardingTestServer(t *testing.T) (*ReactWebServer, string) {
	t.Helper()

	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmpDir, ".config"))
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")

	// Clear all provider API key environment variables so tests don't
	// inherit credentials from the developer's shell.
	providerEnvVars := []string{
		"OPENROUTER_API_KEY", "OPENAI_API_KEY", "DEEPINFRA_API_KEY",
		"DEEPSEEK_API_KEY", "ZAI_API_KEY", "MINIMAX_API_KEY",
		"CHUTES_API_KEY", "MISTRAL_API_KEY", "GEMINI_API_KEY",
		"GROQ_API_KEY", "CEREBRAS_API_KEY", "OLLAMA_API_KEY",
		"JINA_API_KEY",
	}
	for _, key := range providerEnvVars {
		t.Setenv(key, "")
	}

	// Reset the credential backend so the env var takes effect.
	credentials.ResetStorageBackend()

	daemonRoot := t.TempDir()
	workspaceDir := filepath.Join(daemonRoot, "workspace")
	require.NoError(t, os.MkdirAll(workspaceDir, 0o755))

	ws := NewReactWebServer(nil, events.NewEventBus(), 0)
	ws.daemonRoot = daemonRoot
	ws.workspaceRoot = daemonRoot
	ws.terminalManager = NewTerminalManager(daemonRoot)
	ws.fileConsents = newFileConsentManager()

	clientID := "test-client"
	_, err := ws.setClientWorkspaceRoot(clientID, workspaceDir)
	require.NoError(t, err)

	return ws, tmpDir
}

// storeProviderCredential writes a credential for a provider using the file backend.
func storeProviderCredential(t *testing.T, provider, value string) {
	t.Helper()
	err := credentials.SetToActiveBackend(provider, value)
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// GET /api/onboarding/status — handleAPIOnboardingStatus
// ---------------------------------------------------------------------------

func TestOnboardingStatus_FreshUser_AutoSelectsLocalProvider(t *testing.T) {
	// A fresh server has no provider configured. The handler auto-selects the
	// first available provider that doesn't require an API key (e.g., lmstudio,
	// ollama-local). This means setup_required is false even for a fresh user
	// — because a local provider is available. The "provider_not_configured"
	// reason would only apply if ALL providers required API keys and none had
	// credentials. Here we verify the happy auto-selection path.
	ws, _ := setupOnboardingTestServer(t)

	req := makeCredRequest(t, http.MethodGet, "/api/onboarding/status", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.False(t, resp["setup_required"].(bool), "fresh user should auto-select a local provider, so no setup is required")
	assert.Equal(t, "", resp["reason"])

	// current_provider should be auto-selected (likely a local provider).
	assert.NotEmpty(t, resp["current_provider"], "auto-selected provider should be non-empty")
}

func TestOnboardingStatus_EditorProvider_NotSetupRequired(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	// Set LastUsedProvider to "editor" — editor-only mode means no setup needed.
	cm := getConfigManager(t, ws)
	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = "editor"
		return nil
	})
	require.NoError(t, err)

	req := makeCredRequest(t, http.MethodGet, "/api/onboarding/status", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.False(t, resp["setup_required"].(bool))
	assert.Equal(t, "", resp["reason"])
	assert.Equal(t, "editor", resp["current_provider"])
}

func TestOnboardingStatus_WithConfiguredCredential(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	// Set the provider to openrouter and store a valid credential.
	cm := getConfigManager(t, ws)
	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = "openrouter"
		return nil
	})
	require.NoError(t, err)
	storeProviderCredential(t, "openrouter", "sk-or-test-key-openrouter-minimum-length")

	req := makeCredRequest(t, http.MethodGet, "/api/onboarding/status", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.False(t, resp["setup_required"].(bool), "setup should not be required when provider has a credential")
	assert.Equal(t, "", resp["reason"], "reason should be empty when setup is not required")
	assert.Equal(t, "openrouter", resp["current_provider"])
}

func TestOnboardingStatus_WithProviderMissingCredential(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	// Set the provider to openrouter but do NOT store a credential.
	cm := getConfigManager(t, ws)
	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = "openrouter"
		return nil
	})
	require.NoError(t, err)

	req := makeCredRequest(t, http.MethodGet, "/api/onboarding/status", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.True(t, resp["setup_required"].(bool), "setup should be required when provider lacks a credential")
	assert.Equal(t, "missing_provider_credential", resp["reason"])
	assert.Equal(t, "openrouter", resp["current_provider"])
}

func TestOnboardingStatus_MethodNotAllowed(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	// POST to the status endpoint should return 405.
	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/status", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestOnboardingStatus_ResponseStructure(t *testing.T) {
	// Verify the response includes all expected top-level fields.
	ws, _ := setupOnboardingTestServer(t)

	req := makeCredRequest(t, http.MethodGet, "/api/onboarding/status", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)

	// Top-level keys that should always be present.
	assert.Contains(t, resp, "setup_required", "response should contain setup_required")
	assert.Contains(t, resp, "reason", "response should contain reason")
	assert.Contains(t, resp, "current_provider", "response should contain current_provider")
	assert.Contains(t, resp, "current_model", "response should contain current_model")
	assert.Contains(t, resp, "providers", "response should contain providers")
	assert.Contains(t, resp, "environment", "response should contain environment")

	// providers should be an array.
	providers, ok := resp["providers"].([]interface{})
	require.True(t, ok, "providers should be an array")
	assert.Greater(t, len(providers), 0, "should have at least one provider listed")

	// environment should be an object with expected keys.
	env, ok := resp["environment"].(map[string]interface{})
	require.True(t, ok, "environment should be an object")
	assert.Contains(t, env, "runtime_platform")
	assert.Contains(t, env, "host_platform")
	assert.Contains(t, env, "backend_mode")
	assert.Contains(t, env, "recommended_terminal")
}

// ---------------------------------------------------------------------------
// POST /api/onboarding/skip — handleAPIOnboardingSkip
// ---------------------------------------------------------------------------

func TestOnboardingSkip_SetsEditorProvider(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/skip", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingSkip(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.True(t, resp["success"].(bool))
	assert.Equal(t, "editor", resp["provider"])
	assert.Equal(t, "", resp["model"])

	// Verify the provider was persisted to config.
	cm := getConfigManager(t, ws)
	cfg := cm.GetConfig()
	assert.Equal(t, "editor", cfg.LastUsedProvider)
}

func TestOnboardingSkip_ThenStatusNotRequired(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	// Skip onboarding first.
	skipReq := makeCredRequest(t, http.MethodPost, "/api/onboarding/skip", nil)
	skipRec := httptest.NewRecorder()
	ws.handleAPIOnboardingSkip(skipRec, skipReq)
	require.Equal(t, http.StatusOK, skipRec.Code)

	// Now check status — should not require setup.
	statusReq := makeCredRequest(t, http.MethodGet, "/api/onboarding/status", nil)
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)

	assert.Equal(t, http.StatusOK, statusRec.Code)

	var resp map[string]interface{}
	decodeJSON(t, statusRec, &resp)
	assert.False(t, resp["setup_required"].(bool))
	assert.Equal(t, "editor", resp["current_provider"])
}

func TestOnboardingSkip_RepeatedCallsAreIdempotent(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	for i := 0; i < 3; i++ {
		req := makeCredRequest(t, http.MethodPost, "/api/onboarding/skip", nil)
		rec := httptest.NewRecorder()
		ws.handleAPIOnboardingSkip(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "skip call %d should return 200", i+1)

		var resp map[string]interface{}
		decodeJSON(t, rec, &resp)
		assert.True(t, resp["success"].(bool), "skip call %d should return success", i+1)
		assert.Equal(t, "editor", resp["provider"], "skip call %d should return editor provider", i+1)
	}

	// Config should still be "editor" after all calls.
	cm := getConfigManager(t, ws)
	cfg := cm.GetConfig()
	assert.Equal(t, "editor", cfg.LastUsedProvider)
}

func TestOnboardingSkip_MethodNotAllowed(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	// GET to the skip endpoint should return 405.
	req := makeCredRequest(t, http.MethodGet, "/api/onboarding/skip", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingSkip(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// ---------------------------------------------------------------------------
// Integration: skip → status round-trip, plus provider transition
// ---------------------------------------------------------------------------

func TestOnboardingStatus_AfterSkip_EnvironmentStillReported(t *testing.T) {
	// After skipping, the environment info should still be returned correctly.
	ws, _ := setupOnboardingTestServer(t)

	skipReq := makeCredRequest(t, http.MethodPost, "/api/onboarding/skip", nil)
	skipRec := httptest.NewRecorder()
	ws.handleAPIOnboardingSkip(skipRec, skipReq)
	require.Equal(t, http.StatusOK, skipRec.Code)

	statusReq := makeCredRequest(t, http.MethodGet, "/api/onboarding/status", nil)
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)

	var resp map[string]interface{}
	decodeJSON(t, statusRec, &resp)

	env, ok := resp["environment"].(map[string]interface{})
	require.True(t, ok, "environment should be present even after skipping")
	assert.Contains(t, env, "runtime_platform")
	assert.Contains(t, env, "host_platform")
	// Providers list should also still be returned.
	providers, ok := resp["providers"].([]interface{})
	require.True(t, ok)
	assert.Greater(t, len(providers), 0, "providers list should still be populated")
}

func TestOnboardingStatus_MissingClientID_Header(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	// Request without the client ID header — resolveClientID falls back
	// to "default", which triggers lazy agent creation.
	req := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	req.Header.Set("Content-Type", "application/json")
	// Intentionally NOT setting webClientIDHeader.
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(rec, req)

	// Should not panic and should return a valid response via the
	// default ("default") client's auto-created agent.
	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp, "setup_required")
	assert.Contains(t, resp, "current_provider")
}

// ---------------------------------------------------------------------------
// POST /api/onboarding/complete — handleAPIOnboardingComplete
// ---------------------------------------------------------------------------

func TestOnboardingComplete_MethodNotAllowed(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	req := makeCredRequest(t, http.MethodGet, "/api/onboarding/complete", nil)
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestOnboardingComplete_InvalidJSON(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete", strings.NewReader("not json"))
	req.Header.Set(webClientIDHeader, "test-client")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "Invalid JSON")
}

func TestOnboardingComplete_MissingProvider(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/complete", map[string]string{"model": "gpt-4"})
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "provider is required")
}

func TestOnboardingComplete_UnknownProvider(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/complete", map[string]string{
		"provider": "nonexistent_provider_xyz",
		"model":    "m1",
	})
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "unsupported provider")
}

func TestOnboardingComplete_MissingAPIKey(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/complete", map[string]string{
		"provider": "openrouter",
		"model":    "openai/gpt-4",
	})
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "api_key is required")
}

func TestOnboardingComplete_TestProviderRejected(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	storeProviderCredential(t, "test", "test-key")

	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/complete", map[string]string{
		"provider": "test",
		"model":    "test-model",
	})
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "test provider cannot be used")
}

func TestOnboardingComplete_InvalidAPIKey(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/complete", map[string]string{
		"provider": "openrouter",
		"model":    "openai/gpt-4",
		"api_key":  "bad",
	})
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	// Should fail with an error related to the key being invalid.
	assert.Contains(t, resp, "error", "response should have an error field")
}

func TestOnboardingComplete_InvalidAPIKey_ErrorResponseBody(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/complete", map[string]string{
		"provider": "openrouter",
		"model":    "openai/gpt-4",
		"api_key":  "bad-key",
	})
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp, "error", "error response should have an error field")
}

func TestOnboardingComplete_LocalProviderPersistsConfig(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	// Find a provider that doesn't require an API key.
	statusReq := makeCredRequest(t, http.MethodGet, "/api/onboarding/status", nil)
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)
	require.Equal(t, http.StatusOK, statusRec.Code)

	var statusResp map[string]interface{}
	decodeJSON(t, statusRec, &statusResp)
	providers := statusResp["providers"].([]interface{})

	var localProvider string
	for _, p := range providers {
		pm := p.(map[string]interface{})
		if requires, ok := pm["requires_api_key"].(bool); ok && !requires {
			localProvider = pm["id"].(string)
			break
		}
	}
	if localProvider == "" {
		t.Skip("No local/no-key provider available in test environment")
	}

	// Note: agent creation calls getClientAgent() which may block for up to 30s
	// on its internal connection check. httptest.NewRecorder does not propagate
	// context cancellation into the handler, so a request-context timeout is
	// ineffective here. The test is tolerant of any response code.
	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/complete", map[string]string{
		"provider": localProvider,
		"model":    "test-model",
	})
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	// Verify the config was updated with the new provider, even if the overall
	// request returned an error (e.g., agent connection check failed).
	cm := getConfigManager(t, ws)
	cfg := cm.GetConfig()
	assert.Equal(t, localProvider, cfg.LastUsedProvider,
		"provider should be persisted to config even if handler returns error")
}

func TestOnboardingComplete_TrimsInputFields(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/complete", map[string]string{
		"provider": "  openrouter  ",
		"model":    "  test-model  ",
		"api_key":  "  bad  ",
	})
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	// Should NOT fail with "provider is required" — provider is trimmed.
	// It should fail at the validation or agent creation step.
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.NotContains(t, resp["error"], "provider is required")
}

func TestOnboardingComplete_EmptyProvider(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/complete", map[string]string{
		"provider": "  ",
		"model":    "m1",
	})
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "provider is required")
}

func TestOnboardingComplete_EmptyBody(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/complete", map[string]string{})
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "provider is required")
}

func TestOnboardingComplete_ModelPersistedBeforeAgentCreation(t *testing.T) {
	ws, _ := setupOnboardingTestServer(t)

	// Find a provider that doesn't require an API key.
	statusReq := makeCredRequest(t, http.MethodGet, "/api/onboarding/status", nil)
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)
	require.Equal(t, http.StatusOK, statusRec.Code)

	var statusResp map[string]interface{}
	decodeJSON(t, statusRec, &statusResp)
	providers := statusResp["providers"].([]interface{})

	var localProvider string
	for _, p := range providers {
		pm := p.(map[string]interface{})
		if requires, ok := pm["requires_api_key"].(bool); ok && !requires {
			localProvider = pm["id"].(string)
			break
		}
	}
	if localProvider == "" {
		t.Skip("No local/no-key provider available in test environment")
	}

	testModel := "onboarding-persist-test-model-" + time.Now().Format("150405")

	req := makeCredRequest(t, http.MethodPost, "/api/onboarding/complete", map[string]string{
		"provider": localProvider,
		"model":    testModel,
	})
	rec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(rec, req)

	// Regardless of whether the overall request succeeded or failed (e.g. agent
	// connection check timed out), the model should be persisted to config.
	cm := getConfigManager(t, ws)
	cfg := cm.GetConfig()

	// The provider should be persisted
	assert.Equal(t, localProvider, cfg.LastUsedProvider,
		"provider should be persisted to config")

	// The model should be persisted in ProviderModels, even if agent creation failed
	persistedModel := cfg.GetModelForProvider(localProvider)
	assert.Equal(t, testModel, persistedModel,
		"model should be persisted to config ProviderModels even if agent creation fails")
}
