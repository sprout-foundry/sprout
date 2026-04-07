package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/credentials"
)

// ---------------------------------------------------------------------------
// Method router — Credentials settings
// ---------------------------------------------------------------------------

// handleAPISettingsCredentials dispatches GET and PUT/DELETE /api/settings/credentials and /api/settings/credentials/{provider}.
// Exact path (/api/settings/credentials) maps here for GET; trailing-slash (/api/settings/credentials/) maps here for PUT/DELETE.
func (ws *ReactWebServer) handleAPISettingsCredentials(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPISettingsCredentialsGet(w, r)
	case http.MethodPut:
		ws.handleAPISettingsCredentialsPut(w, r)
	case http.MethodDelete:
		ws.handleAPISettingsCredentialsDelete(w, r)
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
			maskedValue = credentials.MaskValue(storedValue)
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

func (ws *ReactWebServer) handleAPISettingsCredentialsPut(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	// Extract provider name from URL path
	provider := extractPathSegment(r.URL.Path, "/api/settings/credentials/")
	if provider == "" {
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
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("unknown provider %q", provider))
		return
	}

	// Store the credential
	if err := credentials.SetToActiveBackend(provider, req.Value); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to store credential: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"provider": provider,
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

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	// Delete the credential (idempotent — treat "not found" as success)
	if err := credentials.DeleteFromActiveBackend(provider); err != nil {
		writeJSONError(w, http.StatusInternalServerError, fmt.Sprintf("failed to delete credential: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"provider": provider,
	})
}