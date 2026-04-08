package webui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

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
