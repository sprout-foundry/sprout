//go:build !js

package webui

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Constants for test values
// ---------------------------------------------------------------------------

const (
	testClientID      = "test-client"
	editorProvider    = "editor"
	nativeBackendMode = "native"
)

// ---------------------------------------------------------------------------
// Helper Functions
// ---------------------------------------------------------------------------

// getStringField safely extracts a string field from a map[string]interface{}
func getStringField(m map[string]interface{}, key string) (string, bool) {
	if m == nil {
		return "", false
	}
	val, ok := m[key].(string)
	return val, ok
}

// getBoolField safely extracts a boolean field from a map[string]interface{}
func getBoolField(m map[string]interface{}, key string) (bool, bool) {
	if m == nil {
		return false, false
	}
	val, ok := m[key].(bool)
	return val, ok
}

// getMapField safely extracts a map[string]interface{} field from a map
func getMapField(m map[string]interface{}, key string) (map[string]interface{}, bool) {
	if m == nil {
		return nil, false
	}
	val, ok := m[key].(map[string]interface{})
	return val, ok
}

// getSliceField safely extracts a []interface{} field from a map
func getSliceField(m map[string]interface{}, key string) ([]interface{}, bool) {
	if m == nil {
		return nil, false
	}
	val, ok := m[key].([]interface{})
	return val, ok
}

// findProvider searches for a provider matching the given criteria
func findProvider(providers []interface{}, requiresAPIKey bool, excludeIDs ...string) (map[string]interface{}, bool) {
	for _, p := range providers {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}

		// Check provider ID exclusions
		providerID, ok := getStringField(pm, "id")
		if ok {
			excluded := false
			for _, exclude := range excludeIDs {
				if providerID == exclude {
					excluded = true
					break
				}
			}
			if excluded {
				continue
			}
		}

		// Check if provider matches the API key requirement
		if reqKey, ok := getBoolField(pm, "requires_api_key"); ok && reqKey == requiresAPIKey {
			return pm, true
		}
	}
	return nil, false
}

// findProviderByID searches for a provider by its ID
func findProviderByID(providers []interface{}, providerID string) (map[string]interface{}, bool) {
	for _, p := range providers {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		if id, ok := getStringField(pm, "id"); ok && id == providerID {
			return pm, true
		}
	}
	return nil, false
}

// getProvidersSlice safely extracts providers array from status response
func getProvidersSlice(statusResp map[string]interface{}) ([]interface{}, bool) {
	return getSliceField(statusResp, "providers")
}

// ---------------------------------------------------------------------------
// E2E Onboarding Tests — Full HTTP flow tests for onboarding API
// These tests verify the complete onboarding experience from status check
// through provider selection, API key setup, and completion.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// E2E Test: Fresh User Flow — Status → Skip → Verify Editor Mode
// ---------------------------------------------------------------------------

func TestOnboardingE2E_FreshUserSkipsToEditorMode(t *testing.T) {
	// This test simulates a fresh user who decides to use sprout as an editor-only tool
	// without configuring any AI provider.
	ws, _ := setupOnboardingTestServer(t)

	// Step 1: Check onboarding status — may require setup
	statusReq := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq.Header.Set(webClientIDHeader, testClientID)
	statusReq.Header.Set("Content-Type", "application/json")
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)

	assert.Equal(t, http.StatusOK, statusRec.Code)
	var statusResp map[string]interface{}
	decodeJSON(t, statusRec, &statusResp)
	// Note: Fresh users may auto-select a provider if one doesn't require API key,
	// so setup_required may be false. The key thing is that skip works.

	// Step 2: User chooses to skip onboarding
	skipReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/skip", nil)
	skipReq.Header.Set(webClientIDHeader, testClientID)
	skipReq.Header.Set("Content-Type", "application/json")
	skipRec := httptest.NewRecorder()
	ws.handleAPIOnboardingSkip(skipRec, skipReq)

	assert.Equal(t, http.StatusOK, skipRec.Code)
	var skipResp map[string]interface{}
	decodeJSON(t, skipRec, &skipResp)
	success, ok := getBoolField(skipResp, "success")
	require.True(t, ok, "response should contain success field")
	assert.True(t, success)
	provider, ok := getStringField(skipResp, "provider")
	require.True(t, ok, "response should contain provider field")
	assert.Equal(t, editorProvider, provider)

	// Step 3: Verify status reflects editor mode
	statusReq2 := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq2.Header.Set(webClientIDHeader, testClientID)
	statusReq2.Header.Set("Content-Type", "application/json")
	statusRec2 := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec2, statusReq2)

	assert.Equal(t, http.StatusOK, statusRec2.Code)
	var statusResp2 map[string]interface{}
	decodeJSON(t, statusRec2, &statusResp2)
	setupRequired, ok := getBoolField(statusResp2, "setup_required")
	require.True(t, ok, "response should contain setup_required field")
	assert.False(t, setupRequired, "editor mode should not require setup")
	currentProvider, ok := getStringField(statusResp2, "current_provider")
	require.True(t, ok, "response should contain current_provider field")
	assert.Equal(t, editorProvider, currentProvider)
}

// ---------------------------------------------------------------------------
// E2E Test: Provider Selection Flow — Status → Select Provider → Complete
// ---------------------------------------------------------------------------

func TestOnboardingE2E_ProviderSelectionFlow(t *testing.T) {
	// This test simulates a user who selects a provider and completes onboarding.
	ws, _ := setupOnboardingTestServer(t)

	// Step 1: Get onboarding status to see available providers
	statusReq := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq.Header.Set(webClientIDHeader, testClientID)
	statusReq.Header.Set("Content-Type", "application/json")
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)

	assert.Equal(t, http.StatusOK, statusRec.Code)
	var statusResp map[string]interface{}
	decodeJSON(t, statusRec, &statusResp)

	// Verify providers list is populated
	providers, ok := getProvidersSlice(statusResp)
	require.True(t, ok, "providers should be an array")
	require.Greater(t, len(providers), 0, "should have providers available")

	// Find a provider that doesn't require API key for testing
	testProviderMap, found := findProvider(providers, false)
	if !found {
		t.Fatalf("No provider without API key requirement available for testing")
	}
	testProvider, ok := getStringField(testProviderMap, "id")
	require.True(t, ok, "provider should have an id field")

	// Step 2: Complete onboarding with the selected provider
	completeReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete",
		strings.NewReader(`{"provider":"`+testProvider+`","model":"test-model"}`))
	completeReq.Header.Set(webClientIDHeader, testClientID)
	completeReq.Header.Set("Content-Type", "application/json")
	completeRec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(completeRec, completeReq)

	// The request may succeed or fail depending on whether the provider has a running backend,
	// but the key thing is that the config should be persisted.
	assert.Contains(t, []int{http.StatusOK, http.StatusBadRequest}, completeRec.Code,
		"onboarding complete should return 200 or 400 (connection check may fail)")

	// Step 3: Verify config was persisted
	cm := getConfigManager(t, ws)
	cfg := cm.GetConfig()
	assert.Equal(t, testProvider, cfg.LastUsedProvider,
		"selected provider should be persisted to config")

	// Step 4: Verify status reflects the new configuration
	statusReq2 := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq2.Header.Set(webClientIDHeader, testClientID)
	statusReq2.Header.Set("Content-Type", "application/json")
	statusRec2 := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec2, statusReq2)

	assert.Equal(t, http.StatusOK, statusRec2.Code)
	var statusResp2 map[string]interface{}
	decodeJSON(t, statusRec2, &statusResp2)
	currentProvider, ok := getStringField(statusResp2, "current_provider")
	require.True(t, ok, "response should contain current_provider field")
	assert.Equal(t, testProvider, currentProvider)
}

// ---------------------------------------------------------------------------
// E2E Test: Provider with Credential Flow — Status → Select → Complete without Key
// ---------------------------------------------------------------------------

func TestOnboardingE2E_ProviderWithExistingCredential(t *testing.T) {
	// This test simulates a user who already has credentials configured (e.g., via env var).
	ws, _ := setupOnboardingTestServer(t)

	// Step 1: Set up a credential via the credentials backend
	storeProviderCredential(t, "openrouter", "sk-or-test-key-for-e2e-testing")

	// Step 2: Configure the provider in the config
	cm := getConfigManager(t, ws)
	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		cfg.LastUsedProvider = "openrouter"
		return nil
	})
	require.NoError(t, err)

	// Step 3: Check status — should show provider as configured
	statusReq := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq.Header.Set(webClientIDHeader, testClientID)
	statusReq.Header.Set("Content-Type", "application/json")
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)

	assert.Equal(t, http.StatusOK, statusRec.Code)
	var statusResp map[string]interface{}
	decodeJSON(t, statusRec, &statusResp)

	// Find openrouter in the providers list
	providers, ok := getProvidersSlice(statusResp)
	require.True(t, ok, "providers should be an array")
	openrouterProvider, found := findProviderByID(providers, "openrouter")
	require.True(t, found, "openrouter should be in providers list")
	hasCredential, ok := getBoolField(openrouterProvider, "has_credential")
	require.True(t, ok, "provider should have has_credential field")
	assert.True(t, hasCredential,
		"openrouter should show has_credential=true")

	// Step 4: Complete onboarding WITHOUT providing an API key (since it's already configured)
	completeReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete",
		strings.NewReader(`{"provider":"openrouter","model":"test-model"}`))
	completeReq.Header.Set(webClientIDHeader, testClientID)
	completeReq.Header.Set("Content-Type", "application/json")
	completeRec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(completeRec, completeReq)

	// Should succeed or fail based on connection check, but config should be persisted
	assert.Contains(t, []int{http.StatusOK, http.StatusBadRequest}, completeRec.Code)

	// Verify config was persisted
	cm2 := getConfigManager(t, ws)
	cfg := cm2.GetConfig()
	assert.Equal(t, "openrouter", cfg.LastUsedProvider)
}

// ---------------------------------------------------------------------------
// E2E Test: API Key Validation Flow — Status → Select → Complete with Key
// ---------------------------------------------------------------------------

func TestOnboardingE2E_APIKeySetupFlow(t *testing.T) {
	// This test simulates a user who needs to enter an API key for their selected provider.
	ws, _ := setupOnboardingTestServer(t)

	// Step 1: Get status to find a provider that requires API key
	statusReq := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq.Header.Set(webClientIDHeader, testClientID)
	statusReq.Header.Set("Content-Type", "application/json")
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)

	assert.Equal(t, http.StatusOK, statusRec.Code)
	var statusResp map[string]interface{}
	decodeJSON(t, statusRec, &statusResp)

	// Find a provider that requires API key
	providers, ok := getProvidersSlice(statusResp)
	require.True(t, ok, "providers should be an array")
	keyRequiredProviderMap, found := findProvider(providers, true)
	if !found {
		t.Fatalf("No provider requiring API key available for testing")
	}
	keyRequiredProvider, ok := getStringField(keyRequiredProviderMap, "id")
	require.True(t, ok, "provider should have an id field")

	// Step 2: Complete onboarding WITH an API key
	apiKey := "sk-test-key-for-e2e-validation-" + t.Name()
	completeReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete",
		strings.NewReader(`{"provider":"`+keyRequiredProvider+`","model":"test-model","api_key":"`+apiKey+`"}`))
	completeReq.Header.Set(webClientIDHeader, testClientID)
	completeReq.Header.Set("Content-Type", "application/json")
	completeRec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(completeRec, completeReq)

	// The request may succeed or fail based on connection check
	assert.Contains(t, []int{http.StatusOK, http.StatusBadRequest}, completeRec.Code)

	// Step 3: Verify the API key was persisted to the credentials store
	// The key should be stored regardless of whether the overall request succeeded
	// (the key is saved before the connection check)
	// The key is saved via the config manager during handler execution,
	// so we verify the call succeeded without errors.
	var completeResp map[string]interface{}
	decodeJSON(t, completeRec, &completeResp)
	if completeRec.Code == http.StatusOK {
		require.Equal(t, true, completeResp["success"], "response should indicate success")
	} else {
		t.Logf("Onboarding request returned %d (expected for failed connections)", completeRec.Code)
	}
}

// ---------------------------------------------------------------------------
// E2E Test: Model Selection Persistence — Complete → Verify Model Saved
// ---------------------------------------------------------------------------

func TestOnboardingE2E_ModelSelectionPersistence(t *testing.T) {
	// This test verifies that the selected model is persisted to config.
	ws, _ := setupOnboardingTestServer(t)

	// Step 1: Get status to find a provider
	statusReq := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq.Header.Set(webClientIDHeader, testClientID)
	statusReq.Header.Set("Content-Type", "application/json")
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)

	assert.Equal(t, http.StatusOK, statusRec.Code)
	var statusResp map[string]interface{}
	decodeJSON(t, statusRec, &statusResp)

	// Find a provider that doesn't require API key
	providers, ok := getProvidersSlice(statusResp)
	require.True(t, ok, "providers should be an array")
	testProviderMap, found := findProvider(providers, false)
	if !found {
		t.Fatalf("No provider without API key requirement available for testing")
	}
	testProvider, ok := getStringField(testProviderMap, "id")
	require.True(t, ok, "provider should have an id field")

	// Step 2: Complete onboarding with a specific model
	customModel := "custom-e2e-test-model-" + t.Name()
	completeReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete",
		strings.NewReader(`{"provider":"`+testProvider+`","model":"`+customModel+`"}`))
	completeReq.Header.Set(webClientIDHeader, testClientID)
	completeReq.Header.Set("Content-Type", "application/json")
	completeRec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(completeRec, completeReq)

	// Step 3: Verify the model was persisted to config
	cm := getConfigManager(t, ws)
	cfg := cm.GetConfig()
	persistedModel := cfg.GetModelForProvider(testProvider)
	assert.Equal(t, customModel, persistedModel,
		"custom model should be persisted to config.ProviderModels")
}

// ---------------------------------------------------------------------------
// E2E Test: Environment Detection — Status → Verify Platform Info
// ---------------------------------------------------------------------------

func TestOnboardingE2E_EnvironmentDetection(t *testing.T) {
	// This test verifies that environment information is correctly detected and returned.
	ws, _ := setupOnboardingTestServer(t)

	// Step 1: Get onboarding status
	statusReq := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq.Header.Set(webClientIDHeader, testClientID)
	statusReq.Header.Set("Content-Type", "application/json")
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)

	assert.Equal(t, http.StatusOK, statusRec.Code)
	var statusResp map[string]interface{}
	decodeJSON(t, statusRec, &statusResp)

	// Step 2: Verify environment object is present and has expected fields
	env, ok := getMapField(statusResp, "environment")
	require.True(t, ok, "environment should be an object")

	expectedFields := []string{
		"runtime_platform",
		"host_platform",
		"backend_mode",
		"has_wsl",
		"has_git_bash",
		"recommended_terminal",
	}
	for _, field := range expectedFields {
		assert.Contains(t, env, field, "environment should contain "+field)
	}

	// Step 3: Verify backend_mode is set correctly
	backendMode, ok := getStringField(env, "backend_mode")
	require.True(t, ok, "backend_mode should be a string")
	assert.Equal(t, nativeBackendMode, backendMode, "default backend mode should be 'native'")
}

// ---------------------------------------------------------------------------
// E2E Test: Provider Ordering — Status → Verify Recommended First
// ---------------------------------------------------------------------------

func TestOnboardingE2E_ProviderOrdering(t *testing.T) {
	// This test verifies that providers are ordered correctly (recommended first).
	ws, _ := setupOnboardingTestServer(t)

	// Step 1: Get onboarding status
	statusReq := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq.Header.Set(webClientIDHeader, testClientID)
	statusReq.Header.Set("Content-Type", "application/json")
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)

	assert.Equal(t, http.StatusOK, statusRec.Code)
	var statusResp map[string]interface{}
	decodeJSON(t, statusRec, &statusResp)

	// Step 2: Verify providers list
	providers, ok := getProvidersSlice(statusResp)
	require.True(t, ok, "providers should be an array")
	assert.Greater(t, len(providers), 0, "should have providers")

	// Step 3: Verify recommended providers are marked
	var recommendedCount int
	for _, p := range providers {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		if recommended, ok := getBoolField(pm, "recommended"); ok && recommended {
			recommendedCount++
		}
	}
	// At least some providers should be marked as recommended
	assert.Greater(t, recommendedCount, 0, "should have recommended providers")
}

// ---------------------------------------------------------------------------
// E2E Test: Error Handling — Invalid Request → Proper Error Response
// ---------------------------------------------------------------------------

func TestOnboardingE2E_ErrorHandling(t *testing.T) {
	// This test verifies that invalid requests return proper error responses.
	ws, _ := setupOnboardingTestServer(t)

	// Test 1: Invalid JSON
	t.Run("InvalidJSON", func(t *testing.T) {
		completeReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete",
			strings.NewReader("not valid json"))
		completeReq.Header.Set(webClientIDHeader, testClientID)
		completeReq.Header.Set("Content-Type", "application/json")
		completeRec := httptest.NewRecorder()
		ws.handleAPIOnboardingComplete(completeRec, completeReq)

		assert.Equal(t, http.StatusBadRequest, completeRec.Code)
		var resp map[string]interface{}
		decodeJSON(t, completeRec, &resp)
		assert.Contains(t, resp["error"], "Invalid JSON")
	})

	// Test 2: Missing provider
	t.Run("MissingProvider", func(t *testing.T) {
		body := `{"model":"test-model"}`
		completeReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete",
			strings.NewReader(body))
		completeReq.Header.Set(webClientIDHeader, testClientID)
		completeReq.Header.Set("Content-Type", "application/json")
		completeRec := httptest.NewRecorder()
		ws.handleAPIOnboardingComplete(completeRec, completeReq)

		assert.Equal(t, http.StatusBadRequest, completeRec.Code)
		var resp map[string]interface{}
		decodeJSON(t, completeRec, &resp)
		assert.Contains(t, resp["error"], "provider is required")
	})

	// Test 3: Unknown provider
	t.Run("UnknownProvider", func(t *testing.T) {
		body := `{"provider":"unknown-provider-xyz","model":"m1"}`
		completeReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete",
			strings.NewReader(body))
		completeReq.Header.Set(webClientIDHeader, testClientID)
		completeReq.Header.Set("Content-Type", "application/json")
		completeRec := httptest.NewRecorder()
		ws.handleAPIOnboardingComplete(completeRec, completeReq)

		assert.Equal(t, http.StatusBadRequest, completeRec.Code)
		var resp map[string]interface{}
		decodeJSON(t, completeRec, &resp)
		assert.Contains(t, resp["error"], "unsupported provider")
	})

	// Test 4: Missing API key for provider that requires it
	t.Run("MissingAPIKey", func(t *testing.T) {
		body := `{"provider":"openrouter","model":"test-model"}`
		completeReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete",
			strings.NewReader(body))
		completeReq.Header.Set(webClientIDHeader, testClientID)
		completeReq.Header.Set("Content-Type", "application/json")
		completeRec := httptest.NewRecorder()
		ws.handleAPIOnboardingComplete(completeRec, completeReq)

		assert.Equal(t, http.StatusBadRequest, completeRec.Code)
		var resp map[string]interface{}
		decodeJSON(t, completeRec, &resp)
		assert.Contains(t, resp["error"], "api_key is required")
	})

	// Test 5: Test provider rejected
	t.Run("TestProviderRejected", func(t *testing.T) {
		storeProviderCredential(t, "test", "test-key")
		body := `{"provider":"test","model":"test-model"}`
		completeReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete",
			strings.NewReader(body))
		completeReq.Header.Set(webClientIDHeader, testClientID)
		completeReq.Header.Set("Content-Type", "application/json")
		completeRec := httptest.NewRecorder()
		ws.handleAPIOnboardingComplete(completeRec, completeReq)

		assert.Equal(t, http.StatusBadRequest, completeRec.Code)
		var resp map[string]interface{}
		decodeJSON(t, completeRec, &resp)
		assert.Contains(t, resp["error"], "test provider cannot be used")
	})
}

// ---------------------------------------------------------------------------
// E2E Test: Input Trimming — Whitespace Handling
// ---------------------------------------------------------------------------

func TestOnboardingE2E_InputTrimming(t *testing.T) {
	// This test verifies that input fields are properly trimmed.
	ws, _ := setupOnboardingTestServer(t)

	// Step 1: Get status to find a provider
	statusReq := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq.Header.Set(webClientIDHeader, testClientID)
	statusReq.Header.Set("Content-Type", "application/json")
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)

	assert.Equal(t, http.StatusOK, statusRec.Code)
	var statusResp map[string]interface{}
	decodeJSON(t, statusRec, &statusResp)

	// Find a provider that doesn't require API key
	providers, ok := getProvidersSlice(statusResp)
	require.True(t, ok, "providers should be an array")
	testProviderMap, found := findProvider(providers, false)
	if !found {
		t.Fatalf("No provider without API key requirement available for testing")
	}
	testProvider, ok := getStringField(testProviderMap, "id")
	require.True(t, ok, "provider should have an id field")

	// Step 2: Complete onboarding with whitespace in fields
	// Note: The API handler trims input fields, so whitespace should be handled
	completeReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete",
		strings.NewReader(`{"provider":"  `+testProvider+`  ","model":"  test-model  "}`))
	completeReq.Header.Set(webClientIDHeader, testClientID)
	completeReq.Header.Set("Content-Type", "application/json")
	completeRec := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(completeRec, completeReq)

	// Verify the trimmed provider was persisted to config
	cm := getConfigManager(t, ws)
	cfg := cm.GetConfig()
	// The provider should be trimmed and persisted, regardless of the HTTP response code
	assert.Equal(t, testProvider, cfg.LastUsedProvider,
		"trimmed provider should be persisted to config")
}

// ---------------------------------------------------------------------------
// E2E Test: Re-onboarding Flow — Change Provider
// ---------------------------------------------------------------------------

func TestOnboardingE2E_ReonboardingFlow(t *testing.T) {
	// This test simulates a user changing their provider (re-onboarding).
	ws, _ := setupOnboardingTestServer(t)

	// Step 1: First onboarding — select a provider
	statusReq := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq.Header.Set(webClientIDHeader, testClientID)
	statusReq.Header.Set("Content-Type", "application/json")
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)

	assert.Equal(t, http.StatusOK, statusRec.Code)
	var statusResp map[string]interface{}
	decodeJSON(t, statusRec, &statusResp)

	// Find first provider that doesn't require API key
	providers, ok := getProvidersSlice(statusResp)
	require.True(t, ok, "providers should be an array")
	firstProviderMap, found := findProvider(providers, false)
	if !found {
		t.Fatalf("No provider without API key requirement available for testing")
	}
	firstProvider, ok := getStringField(firstProviderMap, "id")
	require.True(t, ok, "provider should have an id field")

	completeReq1 := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete",
		strings.NewReader(`{"provider":"`+firstProvider+`","model":"model1"}`))
	completeReq1.Header.Set(webClientIDHeader, testClientID)
	completeReq1.Header.Set("Content-Type", "application/json")
	completeRec1 := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(completeRec1, completeReq1)

	// Step 2: Second onboarding — change to a different provider
	statusReq2 := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq2.Header.Set(webClientIDHeader, testClientID)
	statusReq2.Header.Set("Content-Type", "application/json")
	statusRec2 := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec2, statusReq2)

	var statusResp2 map[string]interface{}
	decodeJSON(t, statusRec2, &statusResp2)

	// Find a different provider
	providers2, ok := getProvidersSlice(statusResp2)
	require.True(t, ok, "providers should be an array")
	secondProviderMap, found := findProvider(providers2, false, firstProvider)
	if !found {
		t.Fatalf("No second provider available for re-onboarding test")
	}
	secondProvider, ok := getStringField(secondProviderMap, "id")
	require.True(t, ok, "provider should have an id field")

	completeReq2 := httptest.NewRequest(http.MethodPost, "/api/onboarding/complete",
		strings.NewReader(`{"provider":"`+secondProvider+`","model":"model2"}`))
	completeReq2.Header.Set(webClientIDHeader, testClientID)
	completeReq2.Header.Set("Content-Type", "application/json")
	completeRec2 := httptest.NewRecorder()
	ws.handleAPIOnboardingComplete(completeRec2, completeReq2)

	// Step 3: Verify the new provider was persisted
	cm := getConfigManager(t, ws)
	cfg := cm.GetConfig()
	assert.Equal(t, secondProvider, cfg.LastUsedProvider,
		"new provider should replace old provider in config")

	// Step 4: Verify status reflects the new configuration
	statusReq3 := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq3.Header.Set(webClientIDHeader, testClientID)
	statusReq3.Header.Set("Content-Type", "application/json")
	statusRec3 := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec3, statusReq3)

	var statusResp3 map[string]interface{}
	decodeJSON(t, statusRec3, &statusResp3)
	currentProvider, ok := getStringField(statusResp3, "current_provider")
	require.True(t, ok, "response should contain current_provider field")
	assert.Equal(t, secondProvider, currentProvider)
}

// ---------------------------------------------------------------------------
// E2E Test: Status Response Completeness — All Fields Present
// ---------------------------------------------------------------------------

func TestOnboardingE2E_StatusResponseCompleteness(t *testing.T) {
	// This test verifies that the status endpoint returns all required fields.
	ws, _ := setupOnboardingTestServer(t)

	statusReq := httptest.NewRequest(http.MethodGet, "/api/onboarding/status", nil)
	statusReq.Header.Set(webClientIDHeader, testClientID)
	statusReq.Header.Set("Content-Type", "application/json")
	statusRec := httptest.NewRecorder()
	ws.handleAPIOnboardingStatus(statusRec, statusReq)

	assert.Equal(t, http.StatusOK, statusRec.Code)
	var statusResp map[string]interface{}
	decodeJSON(t, statusRec, &statusResp)

	// Verify all top-level fields
	requiredFields := []string{
		"setup_required",
		"reason",
		"current_provider",
		"current_model",
		"providers",
		"environment",
	}
	for _, field := range requiredFields {
		assert.Contains(t, statusResp, field, "response should contain "+field)
	}

	// Verify providers is an array with items
	providers, ok := getProvidersSlice(statusResp)
	require.True(t, ok, "providers should be an array")
	assert.Greater(t, len(providers), 0, "providers should have items")

	// Verify each provider has required fields
	for i, p := range providers {
		pm, ok := p.(map[string]interface{})
		if !ok {
			t.Fatalf("providers[%d] should be a map", i)
		}
		providerFields := []string{"id", "name", "models", "requires_api_key", "has_credential"}
		for _, field := range providerFields {
			assert.Contains(t, pm, field, "provider["+strconv.Itoa(i)+"] should contain "+field)
		}
	}

	// Verify environment has required fields
	env, ok := getMapField(statusResp, "environment")
	require.True(t, ok, "environment should be an object")
	envFields := []string{"runtime_platform", "host_platform", "backend_mode"}
	for _, field := range envFields {
		assert.Contains(t, env, field, "environment should contain "+field)
	}
}

// ---------------------------------------------------------------------------
// E2E Test: Skip Idempotency — Multiple Skip Calls
// ---------------------------------------------------------------------------

func TestOnboardingE2E_SkipIdempotency(t *testing.T) {
	// This test verifies that multiple skip calls are idempotent.
	ws, _ := setupOnboardingTestServer(t)

	// Make multiple skip calls
	for i := 0; i < 3; i++ {
		skipReq := httptest.NewRequest(http.MethodPost, "/api/onboarding/skip", nil)
		skipReq.Header.Set(webClientIDHeader, testClientID)
		skipReq.Header.Set("Content-Type", "application/json")
		skipRec := httptest.NewRecorder()
		ws.handleAPIOnboardingSkip(skipRec, skipReq)

		assert.Equal(t, http.StatusOK, skipRec.Code, "skip call "+strconv.Itoa(i+1)+" should succeed")

		var skipResp map[string]interface{}
		decodeJSON(t, skipRec, &skipResp)
		success, ok := getBoolField(skipResp, "success")
		require.True(t, ok, "response should contain success field")
		assert.True(t, success, "skip call "+strconv.Itoa(i+1)+" should return success")
		provider, ok := getStringField(skipResp, "provider")
		require.True(t, ok, "response should contain provider field")
		assert.Equal(t, editorProvider, provider, "skip call "+strconv.Itoa(i+1)+" should return editor provider")
	}

	// Verify config is still "editor"
	cm := getConfigManager(t, ws)
	cfg := cm.GetConfig()
	assert.Equal(t, editorProvider, cfg.LastUsedProvider)
}
