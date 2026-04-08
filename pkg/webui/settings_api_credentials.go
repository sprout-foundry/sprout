package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/credentials"
)

// ---------------------------------------------------------------------------
// Method router — Credentials settings
// ---------------------------------------------------------------------------

// handleAPISettingsCredentials dispatches GET, PUT, DELETE, and POST /api/settings/credentials[/...].
// Exact path (/api/settings/credentials) maps here for GET; trailing-slash (/api/settings/credentials/) maps here for PUT/DELETE.
// POST /api/settings/credentials/{provider}/test also routes here for credential validation.
// Pool endpoints: GET/POST/DELETE /api/settings/credentials/{provider}/pool
func (ws *ReactWebServer) handleAPISettingsCredentials(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		if strings.HasSuffix(r.URL.Path, "/pool") {
			ws.handleAPISettingsCredentialsPoolGet(w, r)
		} else {
			ws.handleAPISettingsCredentialsGet(w, r)
		}
	case http.MethodPut:
		ws.handleAPISettingsCredentialsPut(w, r)
	case http.MethodPost:
		if strings.HasSuffix(r.URL.Path, "/pool") {
			ws.handleAPISettingsCredentialsPoolPost(w, r)
		} else if strings.HasSuffix(r.URL.Path, "/test") {
			ws.handleAPISettingsCredentialsTest(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	case http.MethodDelete:
		if strings.HasSuffix(r.URL.Path, "/pool") {
			ws.handleAPISettingsCredentialsPoolDelete(w, r)
		} else {
			ws.handleAPISettingsCredentialsDelete(w, r)
		}
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// GET /api/settings/credentials
// ---------------------------------------------------------------------------

// providerCredentialStatusResponse represents the credential status for a single provider.
type providerCredentialStatusResponse struct {
	Provider            string `json:"provider"`
	DisplayName         string `json:"display_name"`
	EnvVar              string `json:"env_var"`
	RequiresAPIKey      bool   `json:"requires_api_key"`
	HasStoredCredential bool   `json:"has_stored_credential"`
	HasEnvCredential    bool   `json:"has_env_credential"`
	CredentialSource    string `json:"credential_source"` // "stored", "environment", or "none"
	MaskedValue         string `json:"masked_value"`
	KeyPoolSize         int    `json:"key_pool_size"` // Number of keys in the pool
}

// getCredentialsResponse is the response for GET /api/settings/credentials.
type getCredentialsResponse struct {
	StorageBackend string                        `json:"storage_backend"`
	Providers      []providerCredentialStatusResponse `json:"providers"`
}

func (ws *ReactWebServer) handleAPISettingsCredentialsGet(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	// Get the active backend and its source
	backend, err := credentials.GetStorageBackend()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get storage backend: %v", err))
		return
	}

	// Get available providers from config manager
	providerTypes := cm.GetAvailableProviders()

	// Build the response list
	providers := make([]providerCredentialStatusResponse, 0, len(providerTypes))
	for _, providerType := range providerTypes {
		providerStr := string(providerType)

		// Get auth metadata for the provider
		metadata, err := configuration.GetProviderAuthMetadata(providerStr)
		if err != nil {
			// Skip providers with invalid metadata
			continue
		}

		// Check for env var credential
		hasEnvCred := false
		if metadata.EnvVar != "" {
			hasEnvCred = os.Getenv(metadata.EnvVar) != ""
		}

		// Check for stored credential (keyring/file only, not env vars).
		// HasProviderCredential checks env vars too; we want to know if there
		// is a value stored in the active backend.
		var storedValue string
		hasStoredCred := false
		if val, _, err := credentials.GetFromActiveBackend(providerStr); err == nil && strings.TrimSpace(val) != "" {
			storedValue = val
			hasStoredCred = true
		}

		// Get pool size (ignore errors - default to 0)
		poolSize, _ := credentials.GetPoolSize(providerStr)

		// Determine credential source and masked value
		var source string
		var maskedValue string

		if hasEnvCred {
			source = "environment"
			if metadata.EnvVar != "" {
				maskedValue = credentials.MaskValue(os.Getenv(metadata.EnvVar))
			}
		} else if hasStoredCred {
			source = "stored"
			if poolSize > 1 {
				maskedValue = fmt.Sprintf("(%d keys configured)", poolSize)
			} else {
				maskedValue = credentials.MaskValue(storedValue)
			}
		} else {
			source = "none"
			maskedValue = ""
		}

		providers = append(providers, providerCredentialStatusResponse{
			Provider:            providerStr,
			DisplayName:         metadata.DisplayName,
			EnvVar:              metadata.EnvVar,
			RequiresAPIKey:      metadata.RequiresAPIKey,
			HasStoredCredential: hasStoredCred,
			HasEnvCredential:    hasEnvCred,
			CredentialSource:    source,
			MaskedValue:         maskedValue,
			KeyPoolSize:         poolSize,
		})
	}

	// Sort providers alphabetically by provider name
	sort.SliceStable(providers, func(i, j int) bool {
		return providers[i].Provider < providers[j].Provider
	})

	response := getCredentialsResponse{
		StorageBackend: backend.Source(),
		Providers:      providers,
	}

	writeJSON(w, http.StatusOK, response)
}

// ---------------------------------------------------------------------------
// PUT /api/settings/credentials/{provider}
// ---------------------------------------------------------------------------

// setCredentialRequest is the request body for PUT /api/settings/credentials/{provider}.
type setCredentialRequest struct {
	Value string `json:"value"`
}

// validateAndSetCredential validates a new API key before storing it.
// If validation fails, the old key is preserved and an error is returned.
// Returns true if validation succeeded and key was stored, false otherwise.
func (ws *ReactWebServer) validateAndSetCredential(cm *configuration.Manager, provider, newValue string) (bool, error) {
	// Use the shared ValidateAndSaveAPIKey function which handles:
	// - Mutex-protected read-modify-write (prevents race conditions)
	// - Validation via ListModels API call
	// - Restoration of old key on failure
	_, err := configuration.ValidateAndSaveAPIKey(provider, newValue)
	if err != nil {
		return false, fmt.Errorf("validation failed: %w", err)
	}

	// Sync the Manager's in-memory cache with the backend
	if err := cm.RefreshAPIKeys(); err != nil {
		log.Printf("[config] Warning: failed to refresh API keys cache: %v", err)
		// Continue anyway - the key is saved in backend, just cache is stale
	}

	// Validation succeeded - key is already stored in backend via ValidateAndSaveAPIKey
	// (ValidateAndSaveAPIKey logs success with model count, so no duplicate log here)
	return true, nil
}

func (ws *ReactWebServer) handleAPISettingsCredentialsPut(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	// Extract provider name from URL path
	provider := extractPathSegment(r.URL.Path, "/api/settings/credentials/")
	if provider == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	// Sanitize provider name to prevent path traversal attacks
	provider = path.Base(provider)
	if provider == "" || provider == "." {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	var req setCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Validate value is non-empty
	if strings.TrimSpace(req.Value) == "" {
		writeJSONError(w, http.StatusBadRequest, "credential value cannot be empty")
		return
	}

	// Validate provider is in the known providers list
	knownProviders := cm.GetAvailableProviders()
	validProvider := false
	for _, p := range knownProviders {
		if string(p) == provider {
			validProvider = true
			break
		}
	}
	if !validProvider {
		writeJSONError(w, http.StatusBadRequest, "provider not found")
		return
	}

	// Warn if provider has an existing multi-key pool that would be overwritten
	if existingSize, _ := credentials.GetPoolSize(provider); existingSize > 1 {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"success": false,
			"provider": provider,
			"warning": fmt.Sprintf(
				"Provider %q has %d keys in its pool. Use the pool API (POST/DELETE /api/settings/credentials/%s/pool) to manage keys individually.",
				provider, existingSize, provider,
			),
		})
		return
	}

	// Validate the new key BEFORE storing it
	// This ensures we never replace a working key with a broken one
	if _, err := ws.validateAndSetCredential(cm, provider, req.Value); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("API key validation failed: %s", sanitizeTestError(err)))
		return
	}

	// Key validated successfully - the key is already stored by validateAndSetCredential
	// No additional save needed - SetToActiveBackend was called in validation

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"provider": provider,
	})
}

// ---------------------------------------------------------------------------
// POST /api/settings/credentials/{provider}/test
// ---------------------------------------------------------------------------

// testCredentialResponse is the response for POST /api/settings/credentials/{provider}/test.
type testCredentialResponse struct {
	Success      bool     `json:"success"`
	Provider     string   `json:"provider"`
	ModelCount   int      `json:"model_count,omitempty"`
	SampleModels []string `json:"sample_models,omitempty"`
	Error        string   `json:"error,omitempty"`
}

// keyPoolResponse is the response for GET /api/settings/credentials/{provider}/pool.
type keyPoolResponse struct {
	Provider   string   `json:"provider"`
	KeyCount   int      `json:"key_count"`
	MaskedKeys []string `json:"masked_keys"`
}

// poolCredentialRequest is the request body for POST/DELETE /api/settings/credentials/{provider}/pool.
type poolCredentialRequest struct {
	Value string `json:"value"`
}

// Per-provider rate limiter for the test-connection endpoint.
var testLastCalledAt sync.Map // map[string]time.Time

const testCooldown = 5 * time.Second

// sanitizeTestError maps internal API errors to user-friendly messages.
// Raw error strings may contain server-internal details (filesystem paths,
// stack traces) that should not be exposed to the browser.
func sanitizeTestError(err error) string {
	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "status 401") || strings.Contains(lower, "unauthorized"):
		return "API key is invalid or expired"
	case strings.Contains(lower, "status 403") || strings.Contains(lower, "forbidden"):
		return "API key does not have permission to list models"
	case strings.Contains(lower, "status 429") || strings.Contains(lower, "rate limit"):
		return "Rate limited — please wait a moment and try again"
	case strings.Contains(lower, "no such host") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "network is unreachable"):
		return "Unable to reach provider API. Check your network connection"
	case strings.Contains(lower, "tls") || strings.Contains(lower, "certificate"):
		return "TLS/SSL error connecting to provider API"
	case strings.Contains(lower, "context canceled"):
		return "Connection test was canceled"
	default:
		return "Connection test failed. Check your API key and network connection."
	}
}

func (ws *ReactWebServer) handleAPISettingsCredentialsTest(w http.ResponseWriter, r *http.Request) {
	// Only handle paths ending with /test
	if !strings.HasSuffix(r.URL.Path, "/test") {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract provider name from URL path: /api/settings/credentials/{provider}/test
	provider := extractPathSegment(r.URL.Path, "/api/settings/credentials/")
	if provider == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	// Trim trailing /test from the extracted segment
	provider = strings.TrimSuffix(provider, "/test")

	// Sanitize: take only the base name to prevent path traversal
	provider = path.Base(provider)

	if provider == "" || provider == "." {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	// Validate provider is known
	knownProviders := cm.GetAvailableProviders()
	validProvider := false
	for _, p := range knownProviders {
		if string(p) == provider {
			validProvider = true
			break
		}
	}
	// Also accept "test" as a valid provider (mock provider for testing)
	if provider == "test" {
		validProvider = true
	}
	if !validProvider {
		writeJSONError(w, http.StatusBadRequest, "provider not found")
		return
	}

	// Rate limiting: allow at most one test per provider every 5 seconds.
	if last, ok := testLastCalledAt.Load(provider); ok {
		if t, ok := last.(time.Time); ok {
			if time.Since(t) < testCooldown {
				writeJSON(w, http.StatusTooManyRequests, testCredentialResponse{
					Success:  false,
					Provider: provider,
					Error:    "Please wait before testing again",
				})
				return
			}
		}
	}
	testLastCalledAt.Store(provider, time.Now())

	// For the "test" mock provider, return success immediately
	if provider == "test" {
		writeJSON(w, http.StatusOK, testCredentialResponse{
			Success:      true,
			Provider:     provider,
			ModelCount:   1,
			SampleModels: []string{"test-model"},
		})
		return
	}

	// Check if credentials exist before making API call
	if !configuration.HasProviderAuth(provider) {
		writeJSON(w, http.StatusOK, testCredentialResponse{
			Success:  false,
			Provider: provider,
			Error:    "No credential configured. Save an API key first.",
		})
		return
	}

	// Parse provider name to ClientType
	clientType, err := api.ParseProviderName(provider)
	if err != nil {
		writeJSON(w, http.StatusOK, testCredentialResponse{
			Success:  false,
			Provider: provider,
			Error:    fmt.Sprintf("unsupported provider: %s", provider),
		})
		return
	}

	// Use ListModels (GET /models) to validate the credential — free, no tokens consumed.
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	models, err := api.GetModelsForProviderCtx(ctx, clientType)
	if err != nil {
		// Check if this was a timeout
		if ctx.Err() == context.DeadlineExceeded {
			writeJSON(w, http.StatusGatewayTimeout, testCredentialResponse{
				Success:  false,
				Provider: provider,
				Error:    "connection test timed out (20s)",
			})
			return
		}

		writeJSON(w, http.StatusOK, testCredentialResponse{
			Success:  false,
			Provider: provider,
			Error:    sanitizeTestError(err),
		})
		return
	}

	// Build sample model list (first 5)
	sampleModels := make([]string, 0, 5)
	for i, m := range models {
		if i >= 5 {
			break
		}
		sampleModels = append(sampleModels, m.ID)
	}

	writeJSON(w, http.StatusOK, testCredentialResponse{
		Success:      true,
		Provider:     provider,
		ModelCount:   len(models),
		SampleModels: sampleModels,
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/settings/credentials/{provider}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsCredentialsDelete(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	// Extract provider name from URL path
	provider := extractPathSegment(r.URL.Path, "/api/settings/credentials/")
	if provider == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	// Sanitize provider name to prevent path traversal attacks
	provider = path.Base(provider)
	if provider == "" || provider == "." {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	// Validate provider is known before allowing deletion
	knownProviders := cm.GetAvailableProviders()
	validProvider := false
	for _, p := range knownProviders {
		if string(p) == provider {
			validProvider = true
			break
		}
	}
	// Also accept "test" as a valid provider (mock provider for testing)
	if provider == "test" {
		validProvider = true
	}
	if !validProvider {
		writeJSONError(w, http.StatusBadRequest, "provider not found")
		return
	}

	// Delete the credential (idempotent — treat "not found" as success)
	if err := credentials.DeleteFromActiveBackend(provider); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete credential: %v", err))
		return
	}

	// Clean up any pool entries (provider__pool_N) that may exist for keyring backend
	backend, _ := credentials.GetStorageBackend()
	if _, isKeyring := backend.(*credentials.OSKeyringBackend); isKeyring {
		for i := 1; i < credentials.MaxPoolEntries; i++ {
			poolKey := fmt.Sprintf("%s__pool_%d", provider, i)
			_ = credentials.DeleteFromActiveBackend(poolKey)
		}
	}

	// Reset rotation counter for this provider
	credentials.DefaultRotator.Reset(provider)

	// Sync the Manager's in-memory cache with the backend after deletion
	if err := cm.RefreshAPIKeys(); err != nil {
		log.Printf("[config] Warning: failed to refresh API keys after deletion: %v", err)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"provider": provider,
	})
}

// ---------------------------------------------------------------------------
// GET /api/settings/credentials/{provider}/pool
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsCredentialsPoolGet(w http.ResponseWriter, r *http.Request) {
	// Extract provider name from URL path: /api/settings/credentials/{provider}/pool
	provider := extractPathSegment(r.URL.Path, "/api/settings/credentials/")
	if provider == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	// Trim trailing /pool from the extracted segment
	provider = strings.TrimSuffix(provider, "/pool")

	// Sanitize: take only the base name to prevent path traversal
	provider = path.Base(provider)

	if provider == "" || provider == "." {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	// Validate provider is known
	knownProviders := cm.GetAvailableProviders()
	validProvider := false
	for _, p := range knownProviders {
		if string(p) == provider {
			validProvider = true
			break
		}
	}
	if !validProvider {
		writeJSONError(w, http.StatusBadRequest, "provider not found")
		return
	}

	// Load the key pool
	result, err := credentials.LoadKeyPool(provider)
	if err != nil {
		log.Printf("[credentials] Warning: failed to load key pool for %q: %v", provider, err)
		result = &credentials.KeyPoolResult{Pool: &credentials.KeyPool{Keys: []string{}}}
	}

	// Mask each key
	maskedKeys := make([]string, 0, len(result.Pool.Keys))
	for _, key := range result.Pool.Keys {
		maskedKeys = append(maskedKeys, credentials.MaskValue(key))
	}

	writeJSON(w, http.StatusOK, keyPoolResponse{
		Provider:   provider,
		KeyCount:   len(result.Pool.Keys),
		MaskedKeys: maskedKeys,
	})
}

// ---------------------------------------------------------------------------
// POST /api/settings/credentials/{provider}/pool
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsCredentialsPoolPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	// Extract provider name from URL path: /api/settings/credentials/{provider}/pool
	provider := extractPathSegment(r.URL.Path, "/api/settings/credentials/")
	if provider == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	// Trim trailing /pool from the extracted segment
	provider = strings.TrimSuffix(provider, "/pool")

	// Sanitize: take only the base name to prevent path traversal
	provider = path.Base(provider)

	if provider == "" || provider == "." {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	var req poolCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Validate value is non-empty
	if strings.TrimSpace(req.Value) == "" {
		writeJSONError(w, http.StatusBadRequest, "key value cannot be empty")
		return
	}

	// Validate provider is known
	knownProviders := cm.GetAvailableProviders()
	validProvider := false
	for _, p := range knownProviders {
		if string(p) == provider {
			validProvider = true
			break
		}
	}
	if !validProvider {
		writeJSONError(w, http.StatusBadRequest, "provider not found")
		return
	}

	// Add the key to the pool
	if err := credentials.AddKeyToPool(provider, req.Value); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("failed to add key to pool: %v", err))
		return
	}

	// Get the updated pool size
	poolSize, err := credentials.GetPoolSize(provider)
	if err != nil {
		log.Printf("[credentials] Warning: failed to get pool size for %q: %v", provider, err)
		poolSize = -1
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"provider":  provider,
		"key_count": poolSize,
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/settings/credentials/{provider}/pool
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsCredentialsPoolDelete(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	// Extract provider name from URL path: /api/settings/credentials/{provider}/pool
	provider := extractPathSegment(r.URL.Path, "/api/settings/credentials/")
	if provider == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	// Trim trailing /pool from the extracted segment
	provider = strings.TrimSuffix(provider, "/pool")

	// Sanitize: take only the base name to prevent path traversal
	provider = path.Base(provider)

	if provider == "" || provider == "." {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	var req poolCredentialRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Validate value is non-empty
	if strings.TrimSpace(req.Value) == "" {
		writeJSONError(w, http.StatusBadRequest, "key value cannot be empty")
		return
	}

	// Validate provider is known
	knownProviders := cm.GetAvailableProviders()
	validProvider := false
	for _, p := range knownProviders {
		if string(p) == provider {
			validProvider = true
			break
		}
	}
	if !validProvider {
		writeJSONError(w, http.StatusBadRequest, "provider not found")
		return
	}

	// Remove the key from the pool
	if err := credentials.RemoveKeyFromPool(provider, req.Value); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("failed to remove key from pool: %v", err))
		return
	}

	// Get the updated pool size
	poolSize, err := credentials.GetPoolSize(provider)
	if err != nil {
		log.Printf("[credentials] Warning: failed to get pool size for %q: %v", provider, err)
		poolSize = -1
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"provider":  provider,
		"key_count": poolSize,
	})
}