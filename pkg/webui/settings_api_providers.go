package webui

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/alantheprice/ledit/pkg/configuration"
)

// ---------------------------------------------------------------------------
// Method router — Provider settings
// ---------------------------------------------------------------------------

// handleAPISettingsProviders dispatches all methods for /api/settings/providers and /api/settings/providers/{name}.
// Exact path (/api/settings/providers) maps here for GET/POST; trailing-slash (/api/settings/providers/) maps here for PUT/DELETE.
func (ws *ReactWebServer) handleAPISettingsProviders(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleAPISettingsProvidersGet(w, r)
	case http.MethodPost:
		ws.handleAPISettingsProvidersPost(w, r)
	case http.MethodPut:
		ws.handleAPISettingsProvidersPut(w, r)
	case http.MethodDelete:
		ws.handleAPISettingsProvidersDelete(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------------------------------------------------------------------------
// GET /api/settings/providers
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsProvidersGet(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	cfg := cm.GetConfig()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"custom_providers": sanitizedCustomProviders(cfg.CustomProviders),
	})
}

// ---------------------------------------------------------------------------
// POST /api/settings/providers
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsProvidersPost(w http.ResponseWriter, r *http.Request) {
	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var provider configuration.CustomProviderConfig
	if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	if provider.Name == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required")
		return
	}

	// Strip any incoming API key — never accept from the browser.
	provider.APIKey = ""

	if err := validateCustomProvider(provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	key := provider.Name
	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
		}
		if _, exists := cfg.CustomProviders[key]; exists {
			return fmt.Errorf("custom provider %q already exists (use PUT to update)", key)
		}
		cfg.CustomProviders[key] = provider
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusConflict, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":  true,
		"provider": provider,
	})
}

// ---------------------------------------------------------------------------
// PUT /api/settings/providers/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsProvidersPut(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/providers/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxSettingsBodyBytes)

	var provider configuration.CustomProviderConfig
	if err := json.NewDecoder(r.Body).Decode(&provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, fmt.Sprintf("Invalid JSON: %v", err))
		return
	}

	// Ensure name in body matches URL
	provider.Name = name
	// Strip incoming API key
	provider.APIKey = ""

	if err := validateCustomProvider(provider); err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			cfg.CustomProviders = make(map[string]configuration.CustomProviderConfig)
		}
		if _, exists := cfg.CustomProviders[name]; !exists {
			return fmt.Errorf("custom provider %q not found (use POST to create)", name)
		}
		// Preserve the existing API key if stored
		existing := cfg.CustomProviders[name]
		provider.APIKey = existing.APIKey
		cfg.CustomProviders[name] = provider
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"provider": provider,
	})
}

// ---------------------------------------------------------------------------
// DELETE /api/settings/providers/{name}
// ---------------------------------------------------------------------------

func (ws *ReactWebServer) handleAPISettingsProvidersDelete(w http.ResponseWriter, r *http.Request) {
	name := extractPathSegment(r.URL.Path, "/api/settings/providers/")
	if name == "" {
		writeJSONError(w, http.StatusBadRequest, "provider name is required in URL path")
		return
	}

	cm := ws.getConfigManager(r, w)
	if cm == nil {
		return
	}

	err := cm.UpdateConfig(func(cfg *configuration.Config) error {
		if cfg.CustomProviders == nil {
			return fmt.Errorf("custom provider %q not found", name)
		}
		if _, exists := cfg.CustomProviders[name]; !exists {
			return fmt.Errorf("custom provider %q not found", name)
		}
		delete(cfg.CustomProviders, name)
		return nil
	})
	if err != nil {
		writeJSONError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"deleted": name,
	})
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func validateCustomProvider(p configuration.CustomProviderConfig) error {
	if p.Name == "" {
		return fmt.Errorf("provider name is required")
	}
	if p.Endpoint == "" {
		return fmt.Errorf("provider endpoint is required")
	}
	if p.ContextSize < 0 {
		return fmt.Errorf("context_size must be non-negative")
	}
	return nil
}
