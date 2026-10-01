//go:build !js

package webui

// settings_api_credentials.go — the webui settings credentials API
// dispatch + the single-credential lifecycle: handleAPISettingsCredentials,
// the provider / credentials response types, handleAPISettingsCredentialsGet,
// setCredentialRequest / validateAndSetCredential, handleAPISettingsCredentialsPut,
// and the delete handler. The key-pool family lives in
// settings_api_credentials_pool.go; the credential-test (verify) flow in
// settings_api_credentials_verify.go.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/credentials"
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
			writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		}
	case http.MethodDelete:
		if strings.HasSuffix(r.URL.Path, "/pool") {
			ws.handleAPISettingsCredentialsPoolDelete(w, r)
		} else {
			ws.handleAPISettingsCredentialsDelete(w, r)
		}
	default:
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
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
	StorageBackend string                             `json:"storage_backend"`
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
// Returns the number of models available if validation succeeded, or 0 and an error.
func (ws *ReactWebServer) validateAndSetCredential(cm *configuration.Manager, provider, newValue string) (int, error) {
	// Use the shared ValidateAndSaveAPIKey function which handles:
	// - Mutex-protected read-modify-write (prevents race conditions)
	// - Validation via ListModels API call
	// - Restoration of old key on failure
	modelCount, err := configuration.ValidateAndSaveAPIKey(provider, newValue)
	if err != nil {
		return 0, fmt.Errorf("validation failed: %w", err)
	}

	// Sync the Manager's in-memory cache with the backend
	if err := cm.RefreshAPIKeys(); err != nil {
		ws.log().Warn("failed to refresh API key cache", slog.Any("err", err))
		// Continue anyway - the key is saved in backend, just cache is stale
	}

	return modelCount, nil
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

	// Auto-truncate credential values to a reasonable maximum.
	req.Value = truncateString(req.Value, maxSettingGenericLength)

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
			"success":  false,
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

	// Delete the provider's pool (all keys including pool_N entries).
	// DeleteProviderPool holds the internal poolMu for thread safety.
	if err := credentials.DeleteProviderPool(provider); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete credential: %v", err))
		return
	}

	// Reset rotation counter for this provider
	credentials.DefaultRotator.Reset(provider)

	// Sync the Manager's in-memory cache with the backend after deletion
	if err := cm.RefreshAPIKeys(); err != nil {
		ws.log().Warn("failed to refresh API keys after deletion", slog.Any("err", err))
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"provider": provider,
	})
}

// ---------------------------------------------------------------------------
// GET /api/settings/credentials/{provider}/pool
// ---------------------------------------------------------------------------
