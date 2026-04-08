package webui

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alantheprice/ledit/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findProviderLoc searches the providers list for a provider with the given name.
// Returns a pointer to the matching entry or fatals the test if not found.
func findProviderLoc(t *testing.T, providers []providerCredentialStatusResponse, name string) *providerCredentialStatusResponse {
	t.Helper()
	for i := range providers {
		if providers[i].Provider == name {
			return &providers[i]
		}
	}
	t.Fatalf("provider %q not found in providers list", name)
	return nil
}

// ---------------------------------------------------------------------------
// GET /api/settings/credentials — handleAPISettingsCredentialsGet
// ---------------------------------------------------------------------------

func TestGetProviderCredentials_ReturnsProviders(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	req := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, rec, &resp)

	// storage_backend should be "stored" (FileBackend.Source() returns "stored")
	assert.Equal(t, "stored", resp.StorageBackend)

	// Providers list should not be empty — at minimum openai and openrouter are built-in.
	assert.NotEmpty(t, resp.Providers, "expected at least one provider")

	// Every provider entry should have the required fields.
	for _, p := range resp.Providers {
		assert.NotEmpty(t, p.Provider, "provider name must be set")
		assert.NotEmpty(t, p.DisplayName, "display_name must be set")
		assert.NotEmpty(t, p.CredentialSource, "credential_source must be one of 'stored'|'environment'|'none'")
		assert.Contains(t, []string{"stored", "environment", "none"}, p.CredentialSource)
	}

	// Providers should be sorted alphabetically.
	for i := 1; i < len(resp.Providers); i++ {
		assert.GreaterOrEqual(t, resp.Providers[i].Provider, resp.Providers[i-1].Provider,
			"providers should be sorted alphabetically")
	}
}

func TestGetProviderCredentials_ShowsStoredCredential(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Clear any ambient OPENAI_API_KEY so we can test stored credentials in isolation.
	t.Setenv("OPENAI_API_KEY", "")

	// Store a credential for openai.
	storeCredential(t, "openai", "sk-test-openai-key-12345678")

	req := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, rec, &resp)

	openai := findProviderLoc(t, resp.Providers, "openai")

	assert.True(t, openai.HasStoredCredential, "openai should have a stored credential")
	assert.Equal(t, "stored", openai.CredentialSource)
	assert.NotEmpty(t, openai.MaskedValue, "masked_value should be non-empty for a stored credential")
	assert.True(t, len(openai.MaskedValue) < len("sk-test-openai-key-12345678"), "masked value should be shorter than actual value")
}

func TestGetProviderCredentials_ShowsEnvCredential(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Set an environment variable for openrouter.
	t.Setenv("OPENROUTER_API_KEY", "sk-or-test-key-12345678")

	req := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, rec, &resp)

	openrouter := findProviderLoc(t, resp.Providers, "openrouter")

	assert.True(t, openrouter.HasEnvCredential, "openrouter should have env credential")
	assert.Equal(t, "environment", openrouter.CredentialSource)
	assert.NotEmpty(t, openrouter.MaskedValue, "masked_value should be non-empty for env credential")
	assert.Equal(t, "OPENROUTER_API_KEY", openrouter.EnvVar)
}

func TestGetProviderCredentials_EnvTakesPrecedenceOverStored(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Store a credential AND set the env var for deepseek.
	storeCredential(t, "deepseek", "sk-deepseek-stored-key-12345678")
	t.Setenv("DEEPSEEK_API_KEY", "sk-deepseek-env-key-abcdefghijklmnop")

	req := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, rec, &resp)

	deepseek := findProviderLoc(t, resp.Providers, "deepseek")

	// Env should win over stored.
	assert.True(t, deepseek.HasEnvCredential, "deepseek should have env credential")
	assert.True(t, deepseek.HasStoredCredential, "deepseek should still have stored credential")
	assert.Equal(t, "environment", deepseek.CredentialSource, "env should take precedence over stored")

	// The masked value should reflect the env value, not the stored one.
	assert.Contains(t, deepseek.MaskedValue, "sk-d", "masked value should prefix from env value")
}

// ---------------------------------------------------------------------------
// PUT /api/settings/credentials/{provider} — handleAPISettingsCredentialsPut
// ---------------------------------------------------------------------------

func TestPutProviderCredential_Success(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	body := map[string]interface{}{
		"value": "sk-openai-new-key-12345678",
	}
	req := makeCredRequest(t, http.MethodPut, "/api/settings/credentials/openai/", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.True(t, resp["success"].(bool))
	assert.Equal(t, "openai", resp["provider"])

	// Verify it was stored in the backend.
	val, _, err := credentials.GetFromActiveBackend("openai")
	require.NoError(t, err)
	assert.Equal(t, "sk-openai-new-key-12345678", val)
}

func TestPutProviderCredential_EmptyValue(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	body := map[string]interface{}{
		"value": "",
	}
	req := makeCredRequest(t, http.MethodPut, "/api/settings/credentials/openai/", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "cannot be empty")
}

func TestPutProviderCredential_WhitespaceOnlyValue(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	body := map[string]interface{}{
		"value": "   \t\n  ",
	}
	req := makeCredRequest(t, http.MethodPut, "/api/settings/credentials/openai/", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "cannot be empty")
}

func TestPutProviderCredential_UnknownProvider(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	body := map[string]interface{}{
		"value": "some-key-value",
	}
	req := makeCredRequest(t, http.MethodPut, "/api/settings/credentials/nonexistent_provider_xyz/", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "provider not found")
}

func TestPutProviderCredential_MissingProviderInPath(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	body := map[string]interface{}{
		"value": "some-key-value",
	}
	// Empty provider (trailing slash with nothing after prefix)
	req := makeCredRequest(t, http.MethodPut, "/api/settings/credentials//", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "provider name is required")
}

func TestPutProviderCredential_InvalidJSON(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	body := bytes.NewReader([]byte("not valid json {{{"))
	req := httptest.NewRequest(http.MethodPut, "/api/settings/credentials/openai/", body)
	req.Header.Set(webClientIDHeader, "test-client")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "Invalid JSON")
}

func TestPutProviderCredential_UpdatesGetResponse(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Clear any ambient OPENAI_API_KEY so we can test stored credentials in isolation.
	t.Setenv("OPENAI_API_KEY", "")

	// First verify no stored credential for openai.
	getReq := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	getRec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(getRec, getReq)
	assert.Equal(t, http.StatusOK, getRec.Code)

	var beforeResp getCredentialsResponse
	decodeJSON(t, getRec, &beforeResp)
	openaiBefore := findProviderLoc(t, beforeResp.Providers, "openai")
	assert.False(t, openaiBefore.HasStoredCredential, "no stored credential before PUT")

	// Now PUT a credential.
	putBody := map[string]interface{}{
		"value": "sk-openai-put-key-12345678",
	}
	putReq := makeCredRequest(t, http.MethodPut, "/api/settings/credentials/openai/", putBody)
	putRec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(putRec, putReq)
	assert.Equal(t, http.StatusOK, putRec.Code)

	// Now GET again — should show the stored credential.
	getRec2 := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(getRec2, getReq)
	assert.Equal(t, http.StatusOK, getRec2.Code)

	var afterResp getCredentialsResponse
	decodeJSON(t, getRec2, &afterResp)
	openaiAfter := findProviderLoc(t, afterResp.Providers, "openai")
	assert.True(t, openaiAfter.HasStoredCredential, "should have stored credential after PUT")
	assert.Equal(t, "stored", openaiAfter.CredentialSource)
	assert.NotEmpty(t, openaiAfter.MaskedValue)
}

// ---------------------------------------------------------------------------
// DELETE /api/settings/credentials/{provider} — handleAPISettingsCredentialsDelete
// ---------------------------------------------------------------------------

func TestDeleteProviderCredential_Success(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Store a credential first.
	storeCredential(t, "openai", "sk-openai-to-delete-12345678")

	// Verify it exists.
	val, _, err := credentials.GetFromActiveBackend("openai")
	require.NoError(t, err)
	assert.Equal(t, "sk-openai-to-delete-12345678", val)

	// Delete it via the API.
	req := makeCredRequest(t, http.MethodDelete, "/api/settings/credentials/openai/", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.True(t, resp["success"].(bool))
	assert.Equal(t, "openai", resp["provider"])

	// Verify it's gone from the backend.
	val, _, err = credentials.GetFromActiveBackend("openai")
	require.NoError(t, err)
	assert.Empty(t, val, "credential should be deleted from backend")
}

func TestDeleteProviderCredential_RemovedFromGet(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Clear any ambient OPENAI_API_KEY so we can test stored credentials in isolation.
	t.Setenv("OPENAI_API_KEY", "")

	// Store a credential.
	storeCredential(t, "openai", "sk-openai-delete-test-12345678")

	// Delete it.
	delReq := makeCredRequest(t, http.MethodDelete, "/api/settings/credentials/openai/", nil)
	delRec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(delRec, delReq)
	assert.Equal(t, http.StatusOK, delRec.Code)

	// GET should no longer show it as stored.
	getReq := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	getRec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(getRec, getReq)
	assert.Equal(t, http.StatusOK, getRec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, getRec, &resp)
	openai := findProviderLoc(t, resp.Providers, "openai")
	assert.False(t, openai.HasStoredCredential, "stored credential should be removed after DELETE")
	assert.Equal(t, "none", openai.CredentialSource)
	assert.Empty(t, openai.MaskedValue)
}

func TestDeleteProviderCredential_Idempotent(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Delete a credential that was never stored — should succeed (200, not 500).
	req := makeCredRequest(t, http.MethodDelete, "/api/settings/credentials/openai/", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.True(t, resp["success"].(bool))
	assert.Equal(t, "openai", resp["provider"])

	// Call DELETE again — still should succeed.
	rec2 := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec2, req)
	assert.Equal(t, http.StatusOK, rec2.Code)

	var resp2 map[string]interface{}
	decodeJSON(t, rec2, &resp2)
	assert.True(t, resp2["success"].(bool))
}

func TestDeleteProviderCredential_MissingProviderInPath(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	req := makeCredRequest(t, http.MethodDelete, "/api/settings/credentials//", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "provider name is required")
}

// ---------------------------------------------------------------------------
// Method routing
// ---------------------------------------------------------------------------

func TestProviderCredentials_MethodNotAllowed(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	req := makeCredRequest(t, http.MethodPost, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestProviderCredentials_MethodNotAllowed_WithProviderPath(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	req := makeCredRequest(t, http.MethodPost, "/api/settings/credentials/openai/", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestProviderCredentials_PatchMethodNotAllowed(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	req := makeCredRequest(t, http.MethodPatch, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

// ---------------------------------------------------------------------------
// POST /api/settings/credentials/{provider}/test — handleAPISettingsCredentialsTest
// ---------------------------------------------------------------------------

func TestTestProviderCredential_RateLimited(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Do NOT reset rate limiter — let any prior test state remain.
	// First call should succeed (test provider mock always succeeds).
	req1 := makeCredRequest(t, http.MethodPost, "/api/settings/credentials/test/test", nil)
	rec1 := httptest.NewRecorder()
	ws.handleAPISettingsCredentialsTest(rec1, req1)
	assert.Equal(t, http.StatusOK, rec1.Code)

	// Immediate second call should be rate limited.
	req2 := makeCredRequest(t, http.MethodPost, "/api/settings/credentials/test/test", nil)
	rec2 := httptest.NewRecorder()
	ws.handleAPISettingsCredentialsTest(rec2, req2)
	assert.Equal(t, http.StatusTooManyRequests, rec2.Code)

	var resp2 testCredentialResponse
	decodeJSON(t, rec2, &resp2)
	assert.False(t, resp2.Success)
	assert.Contains(t, resp2.Error, "wait before testing")
}

func TestTestProviderCredential_TestProvider_ReturnsSuccess(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)
	// Reset rate limiter so this test isn't affected by prior tests
	testLastCalledAt.Delete("test")

	req := makeCredRequest(t, http.MethodPost, "/api/settings/credentials/test/test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentialsTest(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp testCredentialResponse
	decodeJSON(t, rec, &resp)

	assert.True(t, resp.Success, "success should be true for test provider")
	assert.Equal(t, "test", resp.Provider, "provider should be 'test'")
	assert.Equal(t, 1, resp.ModelCount, "model_count should be 1")
	assert.Equal(t, []string{"test-model"}, resp.SampleModels, "sample_models should contain test-model")
}

func TestTestProviderCredential_MissingProvider(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Empty provider (double slash after prefix)
	req := makeCredRequest(t, http.MethodPost, "/api/settings/credentials//test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentialsTest(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], "provider name is required", "error should mention provider name required")
}

func TestTestProviderCredential_UnknownProvider(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	req := makeCredRequest(t, http.MethodPost, "/api/settings/credentials/nonexistent_provider_xyz/test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentialsTest(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]interface{}
	decodeJSON(t, rec, &resp)
	assert.Contains(t, resp["error"], `provider not found`, "error should mention provider not found")
}

func TestTestProviderCredential_MissingTestSuffix_Returns405(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// POST to /api/settings/credentials/openai/ (no /test suffix) should return 405
	req := makeCredRequest(t, http.MethodPost, "/api/settings/credentials/openai/", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentialsTest(rec, req)

	assert.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestTestProviderCredential_ResponseFields(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)
	// Reset rate limiter so this test isn't affected by prior tests
	testLastCalledAt.Delete("test")

	req := makeCredRequest(t, http.MethodPost, "/api/settings/credentials/test/test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentialsTest(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp testCredentialResponse
	decodeJSON(t, rec, &resp)

	// Verify required fields exist and have correct types
	assert.IsType(t, true, resp.Success, "success field should be boolean")
	assert.IsType(t, "", resp.Provider, "provider field should be string")
	assert.NotEmpty(t, resp.Provider, "provider should not be empty")
}

func TestTestProviderCredential_NoCredential_ReturnsFriendlyError(t *testing.T) {
	// Ensure OPENAI_API_KEY is not set in the test environment
	t.Setenv("OPENAI_API_KEY", "")

	ws, _ := setupMCPCredTestServer(t)

	// Test with a provider that requires API key but has no credential set
	// We'll use the test server which doesn't set any credentials for real providers
	req := makeCredRequest(t, http.MethodPost, "/api/settings/credentials/openai/test", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentialsTest(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp testCredentialResponse
	decodeJSON(t, rec, &resp)

	assert.False(t, resp.Success, "success should be false when no credential")
	assert.Equal(t, "openai", resp.Provider, "provider should be 'openai'")
	assert.Contains(t, resp.Error, "No credential configured", "error should be user-friendly")
}

// ---------------------------------------------------------------------------
// Local provider — does not require API key
// ---------------------------------------------------------------------------

func TestGetProviderCredentials_LocalProvider_NoAPIKeyRequired(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	req := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, rec, &resp)

	ollamaLocal := findProviderLoc(t, resp.Providers, "ollama-local")
	assert.False(t, ollamaLocal.RequiresAPIKey, "ollama-local should not require an API key")
	assert.Equal(t, "none", ollamaLocal.CredentialSource)
	assert.Empty(t, ollamaLocal.EnvVar, "ollama-local has no env var")
}

// ---------------------------------------------------------------------------
// Provider metadata fields
// ---------------------------------------------------------------------------

func TestGetProviderCredentials_KnownProviderFields(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	req := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, rec, &resp)

	openai := findProviderLoc(t, resp.Providers, "openai")
	assert.Equal(t, "openai", openai.Provider)
	assert.Equal(t, "OpenAI", openai.DisplayName)
	assert.Equal(t, "OPENAI_API_KEY", openai.EnvVar)
	assert.True(t, openai.RequiresAPIKey)
}

// ---------------------------------------------------------------------------
// PUT – overwriting an existing credential
// ---------------------------------------------------------------------------

func TestPutProviderCredential_OverwriteExisting(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Store initial credential.
	storeCredential(t, "openai", "sk-old-key-12345678")

	// Overwrite via PUT.
	body := map[string]interface{}{
		"value": "sk-new-key-abcdefghijklmnop",
	}
	req := makeCredRequest(t, http.MethodPut, "/api/settings/credentials/openai/", body)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify the new value is stored.
	val, _, err := credentials.GetFromActiveBackend("openai")
	require.NoError(t, err)
	assert.Equal(t, "sk-new-key-abcdefghijklmnop", val, "should have overwritten the old credential")
}

// ---------------------------------------------------------------------------
// Masked value edge cases
// ---------------------------------------------------------------------------

func TestGetProviderCredentials_MaskedValueShortCredential(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Clear any ambient OPENAI_API_KEY so we can test stored credentials in isolation.
	t.Setenv("OPENAI_API_KEY", "")

	// Store a short credential (3 chars) — should mask to "****".
	storeCredential(t, "openai", "abc")

	req := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, rec, &resp)
	openai := findProviderLoc(t, resp.Providers, "openai")
	assert.Equal(t, "****", openai.MaskedValue, "short values (< 4 chars) should mask to ****")
}

func TestGetProviderCredentials_MaskedValueMediumCredential(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Clear any ambient OPENAI_API_KEY so we can test stored credentials in isolation.
	t.Setenv("OPENAI_API_KEY", "")

	// Store a medium credential (6 chars) — should mask to "ab****".
	storeCredential(t, "openai", "abcdef")

	req := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, rec, &resp)
	openai := findProviderLoc(t, resp.Providers, "openai")
	assert.Equal(t, "ab****", openai.MaskedValue, "medium values (4-7 chars) should show first 2 + ****")
}

func TestGetProviderCredentials_MaskedValueLongCredential(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)

	// Clear any ambient OPENAI_API_KEY so we can test stored credentials in isolation.
	t.Setenv("OPENAI_API_KEY", "")

	// Store a long credential (16 chars) — should mask to "sk-t****".
	storeCredential(t, "openai", "sk-test-long-key")

	req := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, rec, &resp)
	openai := findProviderLoc(t, resp.Providers, "openai")
	assert.Equal(t, "sk-t****", openai.MaskedValue, "long values (>= 8 chars) should show first 4 + ****")
}

func TestGetProviderCredentials_MaskedValueEmptyCredential(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)
	t.Setenv("OPENAI_API_KEY", "")

	// Store an empty credential directly via the Store map (bypassing Set validation)
	// The GET handler should not count empty values as stored.
	store, err := credentials.Load()
	require.NoError(t, err)
	store["openai"] = ""
	require.NoError(t, credentials.Save(store))

	req := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, rec, &resp)
	openai := findProviderLoc(t, resp.Providers, "openai")
	assert.False(t, openai.HasStoredCredential, "empty credential should not count as stored")
	assert.Empty(t, openai.MaskedValue)
}

func TestGetProviderCredentials_MaskedValueExactlyFourChars(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)
	t.Setenv("OPENAI_API_KEY", "")

	// 4 chars — boundary between short (****) and medium (ab****)
	storeCredential(t, "openai", "abcd")

	req := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, rec, &resp)
	openai := findProviderLoc(t, resp.Providers, "openai")
	assert.Equal(t, "ab****", openai.MaskedValue, "4-char values should show first 2 + ****")
}

func TestGetProviderCredentials_MaskedValueExactlySevenChars(t *testing.T) {
	ws, _ := setupMCPCredTestServer(t)
	t.Setenv("OPENAI_API_KEY", "")

	// 7 chars — boundary between medium (first 2 + ****) and long (first 4 + ****)
	storeCredential(t, "openai", "abcdefg")

	req := makeCredRequest(t, http.MethodGet, "/api/settings/credentials", nil)
	rec := httptest.NewRecorder()
	ws.handleAPISettingsCredentials(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp getCredentialsResponse
	decodeJSON(t, rec, &resp)
	openai := findProviderLoc(t, resp.Providers, "openai")
	assert.Equal(t, "ab****", openai.MaskedValue, "7-char values should show first 2 + **** (same as 4-7 range)")
}
