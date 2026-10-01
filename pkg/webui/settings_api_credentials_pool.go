//go:build !js

package webui

// settings_api_credentials_pool.go — the webui settings credentials
// key-pool family: the pool request / response types and the pool get / post /
// delete handlers. Split out of settings_api_credentials.go.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// ---------------------------------------------------------------------------
// Method router — Credentials settings
// ---------------------------------------------------------------------------
// keyPoolResponse is the response for GET /api/settings/credentials/{provider}/pool.
type keyPoolResponse struct {
	Provider   string   `json:"provider"`
	KeyCount   int      `json:"key_count"`
	MaskedKeys []string `json:"masked_keys"`
}

// poolCredentialRequest is the request body for POST/DELETE /api/settings/credentials/{provider}/pool.
type poolCredentialRequest struct {
	Value string `json:"value"`
	Index *int   `json:"index,omitempty"` // For DELETE: remove by index instead of value
}

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
		ws.log().Warn("failed to load credential key pool", slog.String("provider", provider), slog.Any("err", err))
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

	// Auto-truncate credential values to a reasonable maximum.
	req.Value = truncateString(req.Value, maxSettingGenericLength)

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

	// Sync the Manager's in-memory cache with the backend after pool modification
	if err := cm.RefreshAPIKeys(); err != nil {
		ws.log().Warn("failed to refresh API keys after pool addition", slog.Any("err", err))
	}

	// Get the updated pool size
	poolSize, err := credentials.GetPoolSize(provider)
	if err != nil {
		ws.log().Warn("failed to get credential pool size", slog.String("provider", provider), slog.Any("err", err))
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

	// Support two removal modes:
	// 1. Index-based (preferred by UI): { "index": 0 }
	// 2. Value-based (backward compat): { "value": "sk-..." }
	var removeErr error
	if req.Index != nil {
		removeErr = credentials.RemoveKeyFromPoolByIndex(provider, *req.Index)
	} else if strings.TrimSpace(req.Value) != "" {
		removeErr = credentials.RemoveKeyFromPool(provider, req.Value)
	} else {
		writeJSONError(w, http.StatusBadRequest, "provide either \"index\" or \"value\" to identify the key to remove")
		return
	}

	if removeErr != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("failed to remove key from pool: %v", removeErr))
		return
	}

	// Sync the Manager's in-memory cache with the backend after pool modification
	if err := cm.RefreshAPIKeys(); err != nil {
		ws.log().Warn("failed to refresh API keys after pool removal", slog.Any("err", err))
	}

	// Get the updated pool size
	poolSize, err := credentials.GetPoolSize(provider)
	if err != nil {
		ws.log().Warn("failed to get credential pool size", slog.String("provider", provider), slog.Any("err", err))
		poolSize = -1
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"provider":  provider,
		"key_count": poolSize,
	})
}
